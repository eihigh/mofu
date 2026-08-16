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

	e.uvar(uint64(len(f.Animations)))
	for i := range f.Animations {
		e.animation(&f.Animations[i])
	}
}

func (e *encoder) animation(a *Animation) {
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
		e.uvar(uint64(len(t.Positions)))
		e.u16s(t.Positions)
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
// input cannot make the decoder allocate wildly.
const maxCount = 1 << 28

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
	n := d.count()
	if d.err != nil || n == 0 {
		return ""
	}
	b := make([]byte, n)
	d.read(b)
	return string(b)
}

func (d *decoder) bytes() []byte {
	n := d.count()
	if d.err != nil || n == 0 {
		return nil
	}
	b := make([]byte, n)
	d.read(b)
	return b
}

func (d *decoder) f32s(n int) []float32 {
	if d.err != nil || n == 0 {
		return nil
	}
	v := make([]float32, n)
	for i := range v {
		v[i] = d.f32()
		if d.err != nil {
			return nil
		}
	}
	return v
}

func (d *decoder) u16s(n int) []uint16 {
	if d.err != nil || n == 0 {
		return nil
	}
	v := make([]uint16, n)
	for i := range v {
		v[i] = d.u16()
		if d.err != nil {
			return nil
		}
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
		f.Textures = make([]Texture, n)
		for i := range f.Textures {
			f.Textures[i].Name = d.str()
			f.Textures[i].Data = d.bytes()
		}
	}

	if n := d.count(); n > 0 {
		f.Meshes = make([]Mesh, n)
		for i := range f.Meshes {
			m := &f.Meshes[i]
			m.ID = d.str()
			m.TextureIndex = int32(d.svar())
			m.Flags = MeshFlags(d.u8())
			if n := d.count(); n > 0 {
				m.Masks = make([]int32, n)
				for j := range m.Masks {
					m.Masks[j] = int32(d.svar())
				}
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
		f.HitAreas = make([]HitArea, n)
		for i := range f.HitAreas {
			f.HitAreas[i].Name = d.str()
			f.HitAreas[i].Mesh = int32(d.svar())
		}
	}

	if n := d.count(); n > 0 {
		f.Overlays = make([]Overlay, n)
		for i := range f.Overlays {
			o := &f.Overlays[i]
			o.Name = d.str()
			if n := d.count(); n > 0 {
				o.Tracks = make([]OverlayTrack, n)
				for j := range o.Tracks {
					o.Tracks[j].DeltaPositions = d.f32s(d.count())
					o.Tracks[j].DeltaOpacity = d.f32()
				}
			}
			if d.err != nil {
				return f
			}
		}
	}

	if n := d.count(); n > 0 {
		f.Animations = make([]Animation, n)
		for i := range f.Animations {
			d.animation(&f.Animations[i])
			if d.err != nil {
				return f
			}
		}
	}
	return f
}

func (d *decoder) animation(a *Animation) {
	a.Name = d.str()
	a.Sound = d.str()
	a.FPS = d.f32()
	a.FrameCount = int32(d.count())
	a.Loop = d.u8() != 0
	a.FadeIn = d.f32()
	a.FadeOut = d.f32()
	if n := d.count(); n > 0 {
		a.Events = make([]Event, n)
		for i := range a.Events {
			a.Events[i].Time = d.f32()
			a.Events[i].Value = d.str()
		}
	}
	if n := d.count(); n > 0 {
		a.Tracks = make([]Track, n)
		for i := range a.Tracks {
			t := &a.Tracks[i]
			t.Flags = TrackFlags(d.u8())
			t.Positions = d.u16s(d.count())
			t.Opacity = d.f32s(d.count())
			if n := d.count(); n > 0 {
				t.Order = make([]int32, n)
				for j := range t.Order {
					t.Order[j] = int32(d.svar())
				}
			}
			if n := d.count(); n > 0 {
				t.Visible = make([]uint8, n)
				d.read(t.Visible)
			}
			t.Colors = d.f32s(d.count())
			if d.err != nil {
				return
			}
		}
	}
}
