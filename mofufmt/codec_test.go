package mofufmt

import (
	"bytes"
	"math"
	"reflect"
	"testing"
)

func sample() *File {
	return &File{
		Canvas: Canvas{Width: 1200, Height: 2400, OriginX: 600, OriginY: 1200, PixelsPerUnit: 600},
		Textures: []Texture{
			{Name: "tex/00.png", Data: []byte{0x89, 'P', 'N', 'G', 0, 1, 2, 3}},
			{Name: "tex/01.png", Data: nil},
		},
		Meshes: []Mesh{
			{
				ID:           "ArtMesh1",
				TextureIndex: 0,
				Flags:        MeshBlendAdditive | MeshInvertedMask,
				Masks:        []int32{1, 2},
				UVs:          []float32{0, 0, 1, 0, 1, 1, 0, 1},
				Indices:      []uint16{0, 1, 2, 0, 2, 3},
				MinX:         -1, MinY: -2, MaxX: 3, MaxY: 4,
			},
			{
				ID:           "ArtMesh2",
				TextureIndex: 1,
				UVs:          []float32{0, 0, 1, 1},
				Indices:      []uint16{0, 1, 1},
			},
		},
		HitAreas: []HitArea{{Name: "Body", Mesh: 0}, {Name: "Head", Mesh: 1}},
		Overlays: []Overlay{
			{
				Name: "smile",
				Tracks: []OverlayTrack{
					{DeltaPositions: []float32{0.1, -0.2, 0, 0, 0, 0, 0.3, 0.4}, DeltaOpacity: 0},
					{DeltaOpacity: -0.5},
				},
			},
		},
		Animations: []Animation{
			{
				Name: "@rest", FPS: 30, FrameCount: 1, Loop: false,
				Tracks: []Track{
					{Positions: []uint16{0, 0, 65535, 0, 65535, 65535, 0, 65535}, Opacity: []float32{1}, Order: []int32{0}, Visible: []uint8{1}},
					{Positions: []uint16{7, 8, 9, 10}, Opacity: []float32{0.5}, Order: []int32{1}, Visible: []uint8{0}},
				},
			},
			{
				Name: "Idle", Sound: "sounds/idle.wav", FPS: 60, FrameCount: 3, Loop: true, FadeIn: 0.5, FadeOut: 0.25,
				Events: []Event{{Time: 0.5, Value: "blink"}, {Time: 1.5, Value: "step"}},
				Tracks: []Track{
					{
						Flags:     TrackPositionsAnimated | TrackHasColors | TrackColorsAnimated,
						Positions: make([]uint16, 3*8),
						Opacity:   []float32{1},
						Order:     []int32{-3},
						Visible:   []uint8{1},
						Colors:    make([]float32, 3*8),
					},
					{Positions: []uint16{1, 2, 3, 4}, Opacity: []float32{1, 0.5, 0}, Flags: TrackOpacityAnimated},
				},
			},
		},
	}
}

func TestRoundTrip(t *testing.T) {
	for _, uncompressed := range []bool{false, true} {
		in := sample()
		var buf bytes.Buffer
		if err := Encode(&buf, in, EncodeOptions{Uncompressed: uncompressed}); err != nil {
			t.Fatalf("Encode(uncompressed=%v): %v", uncompressed, err)
		}
		out, err := Decode(bytes.NewReader(buf.Bytes()))
		if err != nil {
			t.Fatalf("Decode(uncompressed=%v): %v", uncompressed, err)
		}
		if !reflect.DeepEqual(in, out) {
			t.Errorf("round trip mismatch (uncompressed=%v)", uncompressed)
		}
	}
}

func TestDecodeRejectsForeignData(t *testing.T) {
	if _, err := Decode(bytes.NewReader([]byte("not a mofu file at all"))); err != ErrBadMagic {
		t.Errorf("Decode(garbage) = %v, want ErrBadMagic", err)
	}
	if _, err := Decode(bytes.NewReader(nil)); err != ErrBadMagic {
		t.Errorf("Decode(empty) = %v, want ErrBadMagic", err)
	}
}

func TestDecodeRejectsTruncation(t *testing.T) {
	var buf bytes.Buffer
	if err := Encode(&buf, sample(), EncodeOptions{Uncompressed: true}); err != nil {
		t.Fatal(err)
	}
	b := buf.Bytes()
	if _, err := Decode(bytes.NewReader(b[:len(b)/2])); err == nil {
		t.Error("Decode(truncated) succeeded, want an error")
	}
}

func TestQuantizationRoundTrip(t *testing.T) {
	m := Mesh{MinX: -10, MaxX: 10, MinY: 0, MaxY: 1}
	for _, v := range []float32{-10, -5, 0, 3.25, 10} {
		got := m.DequantX(m.QuantX(v))
		if diff := got - v; diff > 1e-3 || diff < -1e-3 {
			t.Errorf("DequantX(QuantX(%v)) = %v", v, got)
		}
	}
	// Out-of-range values clamp rather than wrap.
	if got := m.DequantX(m.QuantX(1000)); got != 10 {
		t.Errorf("QuantX above range = %v, want clamp to 10", got)
	}
	if got := m.DequantX(m.QuantX(-1000)); got != -10 {
		t.Errorf("QuantX below range = %v, want clamp to -10", got)
	}
	// A degenerate box must not divide by zero.
	flat := Mesh{MinX: 5, MaxX: 5}
	if got := flat.DequantX(flat.QuantX(5)); got != 5 {
		t.Errorf("flat DequantX = %v, want 5", got)
	}
}

func TestChannelAccessorsClamp(t *testing.T) {
	tr := Track{
		Positions: []uint16{1, 2, 3, 4, 5, 6},
		Opacity:   []float32{0.25},
		Order:     []int32{7, 8},
		Visible:   []uint8{0},
	}
	// Positions hold three frames of one vertex.
	if got := tr.PositionFrame(1, 1); !reflect.DeepEqual(got, []uint16{3, 4}) {
		t.Errorf("PositionFrame(1) = %v", got)
	}
	if got := tr.PositionFrame(99, 1); !reflect.DeepEqual(got, []uint16{5, 6}) {
		t.Errorf("PositionFrame(99) = %v, want the last frame", got)
	}
	if got := tr.OpacityAt(50); got != 0.25 {
		t.Errorf("OpacityAt on a constant channel = %v", got)
	}
	if got := tr.OrderAt(50); got != 8 {
		t.Errorf("OrderAt(50) = %v, want 8", got)
	}
	if tr.VisibleAt(3) {
		t.Error("VisibleAt = true, want false")
	}
	// Absent channels fall back to sensible defaults.
	var empty Track
	if empty.OpacityAt(0) != 1 || !empty.VisibleAt(0) {
		t.Error("empty track should read as opaque and visible")
	}
	if empty.MultiplyAt(0) != [4]float32{1, 1, 1, 1} {
		t.Error("empty track should read as identity multiply")
	}
	if empty.ScreenAt(0) != [4]float32{0, 0, 0, 1} {
		t.Error("empty track should read as identity screen")
	}
}

// TestPositionDeltaRoundTrip drives the second-order delta codec through the
// shapes that exercise each of its branches: single frame, two frames, many
// frames, streams shorter than a frame, and no stride information at all.
func TestPositionDeltaRoundTrip(t *testing.T) {
	smooth := func(frames, stride int) []uint16 {
		v := make([]uint16, frames*stride)
		for f := 0; f < frames; f++ {
			for i := 0; i < stride; i++ {
				v[f*stride+i] = uint16(30000 + 5000*f/(frames+1) + i*13)
			}
		}
		return v
	}
	cases := []struct {
		name   string
		values []uint16
		stride int
	}{
		{"one frame", smooth(1, 8), 8},
		{"two frames", smooth(2, 8), 8},
		{"many frames", smooth(50, 8), 8},
		{"extremes", []uint16{0, 65535, 65535, 0, 0, 65535, 65535, 0}, 4},
		{"no stride", smooth(3, 4), 0},
		{"stride larger than data", smooth(1, 4), 100},
		{"empty", nil, 8},
	}
	for _, tc := range cases {
		f := &File{
			Meshes: []Mesh{{UVs: make([]float32, tc.stride)}}, // VertexCount*2 == stride
			Animations: []Animation{{
				Name: "a", FPS: 30, FrameCount: 1,
				Tracks: []Track{{Positions: tc.values}},
			}},
		}
		if tc.stride == 0 {
			f.Meshes[0].UVs = nil
		}
		var buf bytes.Buffer
		if err := Encode(&buf, f, EncodeOptions{Uncompressed: true}); err != nil {
			t.Fatalf("%s: encode: %v", tc.name, err)
		}
		got, err := Decode(bytes.NewReader(buf.Bytes()))
		if err != nil {
			t.Fatalf("%s: decode: %v", tc.name, err)
		}
		if !reflect.DeepEqual(got.Animations[0].Tracks[0].Positions, tc.values) {
			t.Errorf("%s: positions did not round trip", tc.name)
		}
	}
}

// TestSmoothMotionCompresses pins the point of the delta encoding: a smooth
// 100-vertex, 300-frame motion (117 KiB of raw quantised samples) must land
// far below what storing the samples verbatim under gzip achieves (~113 KiB).
func TestSmoothMotionCompresses(t *testing.T) {
	const verts, frames = 100, 300
	stride := verts * 2
	f := &File{
		Meshes: []Mesh{{UVs: make([]float32, stride), MinX: -1, MinY: -1, MaxX: 1, MaxY: 1}},
	}
	tr := Track{Flags: TrackPositionsAnimated, Positions: make([]uint16, frames*stride)}
	for fr := 0; fr < frames; fr++ {
		tt := float64(fr) / 30
		for i := 0; i < stride; i++ {
			phase := float64(i) * 0.37
			v := 32767 + 6000*math.Sin(2*math.Pi*tt/4+phase)
			tr.Positions[fr*stride+i] = uint16(v)
		}
	}
	f.Animations = []Animation{{Name: "a", FPS: 30, FrameCount: frames, Tracks: []Track{tr}}}

	var buf bytes.Buffer
	if err := Encode(&buf, f, EncodeOptions{}); err != nil {
		t.Fatal(err)
	}
	if buf.Len() > 40<<10 {
		t.Errorf("smooth motion encoded to %d KiB; the delta coding should stay well under 40 KiB", buf.Len()>>10)
	}
	// And it still round-trips exactly.
	got, err := Decode(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got.Animations[0].Tracks[0].Positions, tr.Positions) {
		t.Error("smooth motion did not round trip")
	}
	t.Logf("raw samples: %d KiB, encoded file: %d KiB", frames*stride*2>>10, buf.Len()>>10)
}
