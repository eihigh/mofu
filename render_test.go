package mofu

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"testing"

	"github.com/eihigh/mofu/mofufmt"
	"github.com/hajimehoshi/ebiten/v2"
)

func TestShaderCompiles(t *testing.T) {
	if _, err := ebiten.NewShader(shaderSrc); err != nil {
		t.Fatalf("the embedded Kage shader does not compile: %v", err)
	}
}

// solidPNG encodes a w*h image of a single colour.
func solidPNG(t *testing.T, w, h int, c color.NRGBA) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for i := 0; i < len(img.Pix); i += 4 {
		img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3] = c.R, c.G, c.B, c.A
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// quadFile builds a one-mesh model: a quad spanning the whole canvas, drawn
// from a solid texture.
func quadFile(t *testing.T) *mofufmt.File {
	t.Helper()
	const q = 65535
	return &mofufmt.File{
		Canvas: mofufmt.Canvas{Width: 64, Height: 64, OriginX: 32, OriginY: 32, PixelsPerUnit: 32},
		Textures: []mofufmt.Texture{
			{Name: "solid.png", Data: solidPNG(t, 8, 4, color.NRGBA{0xff, 0, 0, 0xff})},
		},
		Meshes: []mofufmt.Mesh{{
			ID:      "quad",
			UVs:     []float32{0, 0, 1, 0, 1, 1, 0, 1},
			Indices: []uint16{0, 1, 2, 0, 2, 3},
			MinX:    -1, MinY: -1, MaxX: 1, MaxY: 1,
		}},
		Animations: []mofufmt.Animation{{
			Name: "@rest", FPS: 30, FrameCount: 1,
			Tracks: []mofufmt.Track{{
				Positions: []uint16{0, 0, q, 0, q, q, 0, q},
				Opacity:   []float32{1},
				Order:     []int32{0},
				Visible:   []uint8{1},
			}},
		}},
	}
}

func TestNewModel(t *testing.T) {
	m, err := New(quadFile(t))
	if err != nil {
		t.Fatal(err)
	}
	defer m.Dispose()

	if w, h := m.CanvasSize(); w != 64 || h != 64 {
		t.Errorf("CanvasSize = (%v, %v), want (64, 64)", w, h)
	}
	if names := m.AnimationNames(); len(names) != 1 || names[0] != "@rest" {
		t.Errorf("AnimationNames = %v", names)
	}
	if _, ok := m.Animation("@rest"); !ok {
		t.Error("Animation(@rest) not found")
	}
	if _, ok := m.Animation("nope"); ok {
		t.Error("Animation(nope) unexpectedly found")
	}

	// Cubism UVs start at the bottom left of the texture; Ebitengine's source
	// coordinates start at the top left, so V must be flipped and both scaled
	// to the texture's pixel size (8x4 here).
	verts := m.templates[0]
	want := [][2]float32{{0, 4}, {8, 4}, {8, 0}, {0, 0}}
	for i, w := range want {
		if verts[i].SrcX != w[0] || verts[i].SrcY != w[1] {
			t.Errorf("vertex %d src = (%v, %v), want (%v, %v)", i, verts[i].SrcX, verts[i].SrcY, w[0], w[1])
		}
	}
}

// newGeometryPlayer builds a Player over f without allocating GPU resources.
func newGeometryPlayer(f *mofufmt.File) *Player {
	m := &Model{file: f, byName: map[string]int{}}
	for i := range f.Animations {
		m.byName[f.Animations[i].Name] = i
	}
	p := &Player{
		model: m,
		anim:  0,
		speed: 1,
		verts: make([][]ebiten.Vertex, len(f.Meshes)),
		order: make([]int, len(f.Meshes)),
		state: make([]meshState, len(f.Meshes)),
	}
	for i := range f.Meshes {
		p.verts[i] = make([]ebiten.Vertex, f.Meshes[i].VertexCount())
	}
	return p
}

func TestBuildVerticesMapsCanvasToScreen(t *testing.T) {
	f := quadFile(t)
	p := newGeometryPlayer(f)
	a := &f.Animations[0]

	var g ebiten.GeoM
	p.buildVertices(a, 0, 0, 0, &g)

	// Model space has Y up with the origin at the canvas centre; the
	// destination has Y down with the origin at the top left.
	want := [][2]float32{{0, 64}, {64, 64}, {64, 0}, {0, 0}}
	for i, w := range want {
		v := p.verts[0][i]
		if v.DstX != w[0] || v.DstY != w[1] {
			t.Errorf("vertex %d dst = (%v, %v), want (%v, %v)", i, v.DstX, v.DstY, w[0], w[1])
		}
	}

	// A caller's GeoM applies on top of the canvas mapping.
	g.Scale(0.5, 0.5)
	g.Translate(10, 20)
	p.buildVertices(a, 0, 0, 0, &g)
	if v := p.verts[0][1]; v.DstX != 42 || v.DstY != 52 {
		t.Errorf("transformed vertex = (%v, %v), want (42, 52)", v.DstX, v.DstY)
	}
}

func TestBuildVerticesInterpolates(t *testing.T) {
	const q = 65535
	f := quadFile(t)
	f.Animations = []mofufmt.Animation{{
		Name: "move", FPS: 2, FrameCount: 2,
		Tracks: []mofufmt.Track{{
			Flags: mofufmt.TrackPositionsAnimated,
			// Vertex 0 travels from the canvas centre to its top right;
			// the other three stay put. Frames are laid out back to back.
			Positions: []uint16{
				q / 2, q / 2, 0, 0, 0, 0, 0, 0, // frame 0
				q, q, 0, 0, 0, 0, 0, 0, // frame 1
			},
		}},
	}}
	f.Meshes[0].UVs = []float32{0, 0, 1, 0, 1, 1, 0, 1}
	p := newGeometryPlayer(f)

	var g ebiten.GeoM
	p.buildVertices(&f.Animations[0], 0, 1, 0.5, &g)
	// Frame 0 puts the vertex at (32, 32), frame 1 at (64, 0); halfway is
	// (48, 16).
	// The tolerance covers the 16-bit position quantisation.
	v := p.verts[0][0]
	if !within(v.DstX, 48, 0.01) || !within(v.DstY, 16, 0.01) {
		t.Errorf("interpolated vertex = (%v, %v), want (48, 16)", v.DstX, v.DstY)
	}
}

func TestSampleAndSortOrder(t *testing.T) {
	f := quadFile(t)
	f.Meshes = append(f.Meshes, f.Meshes[0], f.Meshes[0])
	tr := f.Animations[0].Tracks[0]
	back := tr
	back.Order = []int32{5}
	mid := tr
	mid.Order = []int32{1}
	mid.Visible = []uint8{0}
	front := tr
	front.Order = []int32{3}
	front.Opacity = []float32{0.5}
	f.Animations[0].Tracks = []mofufmt.Track{back, mid, front}

	p := newGeometryPlayer(f)
	p.sample(&f.Animations[0], 0, 0, 0, 0.5)
	p.sortOrder()

	if got := p.order; got[0] != 1 || got[1] != 2 || got[2] != 0 {
		t.Errorf("draw order = %v, want [1 2 0]", got)
	}
	if p.state[1].visible {
		t.Error("mesh 1 should be invisible")
	}
	// Opacity 0.5 scaled by the draw call's alpha of 0.5.
	if !closeTo(p.state[2].opacity, 0.25) {
		t.Errorf("mesh 2 opacity = %v, want 0.25", p.state[2].opacity)
	}
}

func TestBlendFor(t *testing.T) {
	if blendFor(0) != blendNormal {
		t.Error("no flags should select normal blending")
	}
	if blendFor(mofufmt.MeshBlendAdditive) != blendAdditive {
		t.Error("the additive flag should select additive blending")
	}
	if blendFor(mofufmt.MeshBlendMultiplicative) != blendMultiplicative {
		t.Error("the multiplicative flag should select multiplicative blending")
	}
	// Masking flags must not disturb the blend choice.
	if blendFor(mofufmt.MeshInvertedMask|mofufmt.MeshDoubleSided) != blendNormal {
		t.Error("mask flags should not affect the blend mode")
	}
}

func within(got, want, tol float32) bool {
	d := got - want
	return d < tol && d > -tol
}
