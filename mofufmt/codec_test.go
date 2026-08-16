package mofufmt

import (
	"bytes"
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
		Animations: []Animation{
			{
				Name: "@rest", FPS: 30, FrameCount: 1, Loop: false,
				Tracks: []Track{
					{Positions: []uint16{0, 0, 65535, 0, 65535, 65535, 0, 65535}, Opacity: []float32{1}, Order: []int32{0}, Visible: []uint8{1}},
					{Positions: []uint16{7, 8, 9, 10}, Opacity: []float32{0.5}, Order: []int32{1}, Visible: []uint8{0}},
				},
			},
			{
				Name: "Idle", FPS: 60, FrameCount: 3, Loop: true, FadeIn: 0.5, FadeOut: 0.25,
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
