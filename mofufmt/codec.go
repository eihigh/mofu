package mofufmt

import (
	"bufio"
	"compress/gzip"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
)

// ErrBadMagic is returned when the input is not a .mofu file.
var ErrBadMagic = errors.New("mofufmt: not a mofu file")

// EncodeOptions tunes Encode.
type EncodeOptions struct {
	// Uncompressed writes the body verbatim instead of gzipping it.
	Uncompressed bool
}

// Encode writes f to w.
func Encode(w io.Writer, f *File, opts EncodeOptions) error {
	var flags uint32
	if !opts.Uncompressed {
		flags |= FlagCompressed
	}
	head := make([]byte, 0, 16)
	head = append(head, Magic[:]...)
	head = binary.LittleEndian.AppendUint32(head, Version)
	head = binary.LittleEndian.AppendUint32(head, flags)
	head = binary.LittleEndian.AppendUint32(head, 0) // reserved
	if _, err := w.Write(head); err != nil {
		return err
	}

	body := w
	var gz *gzip.Writer
	if flags&FlagCompressed != 0 {
		var err error
		gz, err = gzip.NewWriterLevel(w, gzip.BestCompression)
		if err != nil {
			return err
		}
		body = gz
	}
	bw := bufio.NewWriterSize(body, 1<<16)
	e := &encoder{w: bw}
	e.file(f)
	if e.err != nil {
		return e.err
	}
	if err := bw.Flush(); err != nil {
		return err
	}
	if gz != nil {
		return gz.Close()
	}
	return nil
}

// Decode reads a whole .mofu file from r.
func Decode(r io.Reader) (*File, error) {
	var head [16]byte
	if _, err := io.ReadFull(r, head[:]); err != nil {
		if errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, io.EOF) {
			return nil, ErrBadMagic
		}
		return nil, err
	}
	if [4]byte(head[0:4]) != Magic {
		return nil, ErrBadMagic
	}
	if v := binary.LittleEndian.Uint32(head[4:8]); v != Version {
		return nil, fmt.Errorf("mofufmt: unsupported version %d (want %d)", v, Version)
	}
	flags := binary.LittleEndian.Uint32(head[8:12])

	body := r
	if flags&FlagCompressed != 0 {
		gz, err := gzip.NewReader(r)
		if err != nil {
			return nil, err
		}
		defer gz.Close()
		body = gz
	}
	d := &decoder{r: bufio.NewReaderSize(body, 1<<16)}
	f := d.file()
	if d.err != nil {
		return nil, d.err
	}
	return f, nil
}

// encoder writes primitives, latching the first error.
type encoder struct {
	w   *bufio.Writer
	err error
	buf [8]byte
}

func (e *encoder) u8(v uint8) {
	if e.err == nil {
		e.err = e.w.WriteByte(v)
	}
}

func (e *encoder) uvar(v uint64) {
	if e.err != nil {
		return
	}
	var tmp [binary.MaxVarintLen64]byte
	n := binary.PutUvarint(tmp[:], v)
	_, e.err = e.w.Write(tmp[:n])
}

func (e *encoder) svar(v int64) { e.uvar(uint64(v<<1) ^ uint64(v>>63)) }

func (e *encoder) u16(v uint16) {
	binary.LittleEndian.PutUint16(e.buf[:2], v)
	e.write(e.buf[:2])
}

func (e *encoder) f32(v float32) {
	binary.LittleEndian.PutUint32(e.buf[:4], math.Float32bits(v))
	e.write(e.buf[:4])
}

func (e *encoder) write(b []byte) {
	if e.err == nil {
		_, e.err = e.w.Write(b)
	}
}

func (e *encoder) str(s string) {
	e.uvar(uint64(len(s)))
	if e.err == nil {
		_, e.err = e.w.WriteString(s)
	}
}

func (e *encoder) bytes(b []byte) {
	e.uvar(uint64(len(b)))
	e.write(b)
}

func (e *encoder) f32s(v []float32) {
	for _, x := range v {
		e.f32(x)
	}
}

func (e *encoder) u16s(v []uint16) {
	for _, x := range v {
		e.u16(x)
	}
}

// positions writes a quantised position stream as second-order deltas.
//
// Vertex animation is smooth, so the change of the per-frame change (the
// acceleration) is near zero for almost every sample; stored as zigzag
// varints it costs a byte or two each and compresses far better than the raw
// stream. Layout: the first frame is a spatial delta chain, the second frame
// deltas against the first, and every later sample stores the change of its
// component's temporal delta. stride is the number of values per frame.
func (e *encoder) positions(v []uint16, stride int) {
	e.uvar(uint64(len(v)))
	s := stride
	if s <= 0 || s > len(v) {
		s = len(v)
	}
	for i, x := range v {
		var d int64
		switch {
		case i == 0:
			d = int64(x)
		case i < s:
			d = int64(x) - int64(v[i-1])
		case i < 2*s:
			d = int64(x) - int64(v[i-s])
		default:
			d = (int64(x) - int64(v[i-s])) - (int64(v[i-s]) - int64(v[i-2*s]))
		}
		e.svar(d)
	}
}

func (e *encoder) file(f *File) {
	e.f32(f.Canvas.Width)
	e.f32(f.Canvas.Height)
	e.f32(f.Canvas.OriginX)
	e.f32(f.Canvas.OriginY)
	e.f32(f.Canvas.PixelsPerUnit)

	e.uvar(uint64(len(f.Textures)))
	for i := range f.Textures {
		e.str(f.Textures[i].Name)
		e.bytes(f.Textures[i].Data)
	}

	e.uvar(uint64(len(f.Meshes)))
	for i := range f.Meshes {
		m := &f.Meshes[i]
		e.str(m.ID)
		e.svar(int64(m.TextureIndex))
		e.u8(uint8(m.Flags))
		e.uvar(uint64(len(m.Masks)))
		for _, x := range m.Masks {
			e.svar(int64(x))
		}
		e.uvar(uint64(m.VertexCount()))
		e.f32s(m.UVs)
		e.uvar(uint64(len(m.Indices)))
		e.u16s(m.Indices)
		e.f32(m.MinX)
		e.f32(m.MinY)
		e.f32(m.MaxX)
		e.f32(m.MaxY)
	}

	e.uvar(uint64(len(f.HitAreas)))
	for i := range f.HitAreas {
		e.str(f.HitAreas[i].Name)
		e.svar(int64(f.HitAreas[i].Mesh))
	}

	e.uvar(uint64(len(f.Overlays)))
	for i := range f.Overlays {
		o := &f.Overlays[i]
		e.str(o.Name)
		e.uvar(uint64(len(o.Tracks)))
		for j := range o.Tracks {
			e.uvar(uint64(len(o.Tracks[j].DeltaPositions)))
			e.f32s(o.Tracks[j].DeltaPositions)
			e.f32(o.Tracks[j].DeltaOpacity)
		}
	}

	strides := make([]int, len(f.Meshes))
	for i := range f.Meshes {
		strides[i] = f.Meshes[i].VertexCount() * 2
	}
	e.uvar(uint64(len(f.Animations)))
	for i := range f.Animations {
		e.animation(&f.Animations[i], strides)
	}
}

func (e *encoder) animation(a *Animation, strides []int) {
	e.str(a.Name)
	e.str(a.Sound)
	e.f32(a.FPS)
	e.uvar(uint64(a.FrameCount))
	if a.Loop {
		e.u8(1)
	} else {
		e.u8(0)
	}
	e.f32(a.FadeIn)
	e.f32(a.FadeOut)
	e.uvar(uint64(len(a.Events)))
	for i := range a.Events {
		e.f32(a.Events[i].Time)
		e.str(a.Events[i].Value)
	}
	e.uvar(uint64(len(a.Tracks)))
	for i := range a.Tracks {
		t := &a.Tracks[i]
		e.u8(uint8(t.Flags))
		stride := 0
		if i < len(strides) {
			stride = strides[i]
		}
		e.positions(t.Positions, stride)
		e.uvar(uint64(len(t.Opacity)))
		e.f32s(t.Opacity)
		e.uvar(uint64(len(t.Order)))
		for _, x := range t.Order {
			e.svar(int64(x))
		}
		e.uvar(uint64(len(t.Visible)))
		e.write(t.Visible)
		e.uvar(uint64(len(t.Colors)))
		e.f32s(t.Colors)
	}
}

// maxCount bounds every length read from a file so that a corrupt or hostile
// input cannot claim absurd sizes outright.
const maxCount = 1 << 28

// allocChunk caps how much any decoder allocation may run ahead of the bytes
// actually read. Lengths in the file are attacker-controlled; memory is only
// ever grown after the data backing it has arrived, so a tiny input claiming
// a huge array fails at the read, not at an allocation.
const allocChunk = 1 << 16

// decoder reads primitives, latching the first error.
type decoder struct {
	r   *bufio.Reader
	err error
	buf [8]byte
}

func (d *decoder) fail(err error) {
	if d.err == nil {
		d.err = err
	}
}

func (d *decoder) u8() uint8 {
	if d.err != nil {
		return 0
	}
	v, err := d.r.ReadByte()
	if err != nil {
		d.fail(err)
	}
	return v
}

func (d *decoder) uvar() uint64 {
	if d.err != nil {
		return 0
	}
	v, err := binary.ReadUvarint(d.r)
	if err != nil {
		d.fail(err)
	}
	return v
}

func (d *decoder) svar() int64 {
	u := d.uvar()
	return int64(u>>1) ^ -int64(u&1)
}

// count reads a length and rejects implausible ones.
func (d *decoder) count() int {
	v := d.uvar()
	if v > maxCount {
		d.fail(fmt.Errorf("mofufmt: implausible length %d", v))
		return 0
	}
	return int(v)
}

func (d *decoder) read(b []byte) {
	if d.err != nil {
		return
	}
	if _, err := io.ReadFull(d.r, b); err != nil {
		d.fail(err)
	}
}

func (d *decoder) u16() uint16 {
	d.read(d.buf[:2])
	return binary.LittleEndian.Uint16(d.buf[:2])
}

func (d *decoder) f32() float32 {
	d.read(d.buf[:4])
	return math.Float32frombits(binary.LittleEndian.Uint32(d.buf[:4]))
}

func (d *decoder) str() string {
	return string(d.bytes())
}

func (d *decoder) bytes() []byte {
	n := d.count()
	if d.err != nil || n == 0 {
		return nil
	}
	b := make([]byte, 0, min(n, allocChunk))
	for len(b) < n {
		m := min(n-len(b), allocChunk)
		start := len(b)
		b = append(b, make([]byte, m)...)
		d.read(b[start:])
		if d.err != nil {
			return nil
		}
	}
	return b
}

func (d *decoder) f32s(n int) []float32 {
	if d.err != nil || n == 0 {
		return nil
	}
	v := make([]float32, 0, min(n, allocChunk/4))
	for i := 0; i < n; i++ {
		x := d.f32()
		if d.err != nil {
			return nil
		}
		v = append(v, x)
	}
	return v
}

// positions reverses the second-order delta stream written by
// encoder.positions.
func (d *decoder) positions(stride int) []uint16 {
	n := d.count()
	if d.err != nil || n == 0 {
		return nil
	}
	s := stride
	if s <= 0 || s > n {
		s = n
	}
	v := make([]uint16, 0, min(n, allocChunk/2))
	for i := 0; i < n; i++ {
		dd := d.svar()
		if d.err != nil {
			return nil
		}
		var x int64
		switch {
		case i == 0:
			x = dd
		case i < s:
			x = int64(v[i-1]) + dd
		case i < 2*s:
			x = int64(v[i-s]) + dd
		default:
			x = 2*int64(v[i-s]) - int64(v[i-2*s]) + dd
		}
		v = append(v, uint16(x))
	}
	return v
}

func (d *decoder) u16s(n int) []uint16 {
	if d.err != nil || n == 0 {
		return nil
	}
	v := make([]uint16, 0, min(n, allocChunk/2))
	for i := 0; i < n; i++ {
		x := d.u16()
		if d.err != nil {
			return nil
		}
		v = append(v, x)
	}
	return v
}

func (d *decoder) file() *File {
	f := new(File)
	f.Canvas.Width = d.f32()
	f.Canvas.Height = d.f32()
	f.Canvas.OriginX = d.f32()
	f.Canvas.OriginY = d.f32()
	f.Canvas.PixelsPerUnit = d.f32()

	if n := d.count(); n > 0 {
		for i := 0; i < n && d.err == nil; i++ {
			f.Textures = append(f.Textures, Texture{Name: d.str(), Data: d.bytes()})
		}
	}

	if n := d.count(); n > 0 {
		for i := 0; i < n && d.err == nil; i++ {
			f.Meshes = append(f.Meshes, Mesh{})
			m := &f.Meshes[i]
			m.ID = d.str()
			m.TextureIndex = int32(d.svar())
			m.Flags = MeshFlags(d.u8())
			for j, jn := 0, d.count(); j < jn && d.err == nil; j++ {
				m.Masks = append(m.Masks, int32(d.svar()))
			}
			vc := d.count()
			m.UVs = d.f32s(vc * 2)
			m.Indices = d.u16s(d.count())
			m.MinX = d.f32()
			m.MinY = d.f32()
			m.MaxX = d.f32()
			m.MaxY = d.f32()
			if d.err != nil {
				return f
			}
		}
	}

	if n := d.count(); n > 0 {
		for i := 0; i < n && d.err == nil; i++ {
			f.HitAreas = append(f.HitAreas, HitArea{Name: d.str(), Mesh: int32(d.svar())})
		}
	}

	if n := d.count(); n > 0 {
		for i := 0; i < n && d.err == nil; i++ {
			f.Overlays = append(f.Overlays, Overlay{Name: d.str()})
			o := &f.Overlays[i]
			for j, jn := 0, d.count(); j < jn && d.err == nil; j++ {
				o.Tracks = append(o.Tracks, OverlayTrack{
					DeltaPositions: d.f32s(d.count()),
					DeltaOpacity:   d.f32(),
				})
			}
		}
	}

	strides := make([]int, len(f.Meshes))
	for i := range f.Meshes {
		strides[i] = f.Meshes[i].VertexCount() * 2
	}
	if n := d.count(); n > 0 {
		for i := 0; i < n && d.err == nil; i++ {
			f.Animations = append(f.Animations, Animation{})
			d.animation(&f.Animations[i], strides)
		}
	}
	return f
}

func (d *decoder) animation(a *Animation, strides []int) {
	a.Name = d.str()
	a.Sound = d.str()
	a.FPS = d.f32()
	a.FrameCount = int32(d.count())
	a.Loop = d.u8() != 0
	a.FadeIn = d.f32()
	a.FadeOut = d.f32()
	for i, n := 0, d.count(); i < n && d.err == nil; i++ {
		a.Events = append(a.Events, Event{Time: d.f32(), Value: d.str()})
	}
	for i, n := 0, d.count(); i < n && d.err == nil; i++ {
		a.Tracks = append(a.Tracks, Track{})
		t := &a.Tracks[i]
		t.Flags = TrackFlags(d.u8())
		stride := 0
		if i < len(strides) {
			stride = strides[i]
		}
		t.Positions = d.positions(stride)
		t.Opacity = d.f32s(d.count())
		for j, jn := 0, d.count(); j < jn && d.err == nil; j++ {
			t.Order = append(t.Order, int32(d.svar()))
		}
		t.Visible = d.bytes()
		t.Colors = d.f32s(d.count())
	}
}
