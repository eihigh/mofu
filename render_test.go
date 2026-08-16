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
	return newModelCommon(f).NewPlayer()
}

// sampleAndTransform runs the sampling half of Draw: pose, overlays, then the
// GeoM transform into the vertex buffers.
func (p *Player) sampleAndTransform(geom *ebiten.GeoM) {
	p.samplePose(&p.cur, &p.pose)
	p.applyOverlays()
	p.poseValid = true
	p.buildVerts(geom)
}

func TestBuildVerticesMapsCanvasToScreen(t *testing.T) {
	f := quadFile(t)
	p := newGeometryPlayer(f)

	var g ebiten.GeoM
	p.sampleAndTransform(&g)

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
	p.sampleAndTransform(&g)
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
	p.SetTime(0.25) // frame 0.5 at 2 fps: halfway between frames 0 and 1
	p.sampleAndTransform(&g)
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
	p.samplePose(&p.cur, &p.pose)
	for i := range p.pose.states {
		p.pose.states[i].opacity *= 0.5 // the draw call's Alpha
	}
	p.sortOrder()

	if got := p.order; got[0] != 1 || got[1] != 2 || got[2] != 0 {
		t.Errorf("draw order = %v, want [1 2 0]", got)
	}
	if p.pose.states[1].visible {
		t.Error("mesh 1 should be invisible")
	}
	// Opacity 0.5 scaled by the draw call's alpha of 0.5.
	if !closeTo(p.pose.states[2].opacity, 0.25) {
		t.Errorf("mesh 2 opacity = %v, want 0.25", p.pose.states[2].opacity)
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

// twoPoseFile builds a model with two single-frame animations: "left" pins a
// one-triangle mesh to the canvas's left half, "right" to its right half.
func twoPoseFile() *mofufmt.File {
	const q = 65535
	// Base and apex y differ, so the triangle has real area: base at model
	// y=-1 (canvas y=64), apex at model y=1 (canvas y=0).
	track := func(xs ...uint16) []mofufmt.Track {
		pos := []uint16{xs[0], 0, xs[1], 0, xs[2], q}
		return []mofufmt.Track{{
			Positions: pos, Opacity: []float32{1}, Order: []int32{0}, Visible: []uint8{1},
		}}
	}
	return &mofufmt.File{
		Canvas: mofufmt.Canvas{Width: 64, Height: 64, OriginX: 32, OriginY: 32, PixelsPerUnit: 32},
		Meshes: []mofufmt.Mesh{{
			ID:      "tri",
			UVs:     []float32{0, 0, 1, 0, 0.5, 1},
			Indices: []uint16{0, 1, 2},
			MinX:    -1, MinY: -1, MaxX: 1, MaxY: 1,
		}},
		HitAreas: []mofufmt.HitArea{{Name: "Tri", Mesh: 0}},
		Overlays: []mofufmt.Overlay{{
			Name: "shift",
			// Push every vertex +0.5 model units in x (16 canvas pixels).
			Tracks: []mofufmt.OverlayTrack{{
				DeltaPositions: []float32{0.5, 0, 0.5, 0, 0.5, 0},
			}},
		}},
		Animations: []mofufmt.Animation{
			{Name: "left", FPS: 30, FrameCount: 1, Tracks: track(0, q/2, q/4)},
			{Name: "right", FPS: 30, FrameCount: 1, FadeIn: 1, Tracks: track(q/2, q, 3*q/4)},
		},
	}
}

func TestCrossFadeBlendsPositions(t *testing.T) {
	p := newGeometryPlayer(twoPoseFile())
	var g ebiten.GeoM

	// "left" alone: vertex 0 sits at canvas x=0.
	p.sampleAndTransform(&g)
	if got := p.verts[0][0].DstX; !within(got, 0, 0.01) {
		t.Fatalf("left pose vertex x = %v, want 0", got)
	}

	// Halfway through a 1s fade to "right", vertex 0 is halfway between its
	// left-pose x=0 and right-pose x=32.
	if err := p.Play("right"); err != nil {
		t.Fatal(err)
	}
	p.Advance(0.5)
	p.samplePose(&p.cur, &p.pose)
	p.samplePose(p.prev, &p.scratch)
	mixPose(&p.pose, &p.scratch, 1-float32(p.fadeElapsed/p.fadeDur))
	p.buildVerts(&g)
	if got := p.verts[0][0].DstX; !within(got, 16, 0.01) {
		t.Errorf("mid-fade vertex x = %v, want 16", got)
	}

	// After the fade only the new pose remains.
	p.Advance(0.6)
	p.sampleAndTransform(&g)
	if got := p.verts[0][0].DstX; !within(got, 32, 0.01) {
		t.Errorf("post-fade vertex x = %v, want 32", got)
	}
}

func TestOverlayShiftsPose(t *testing.T) {
	p := newGeometryPlayer(twoPoseFile())
	var g ebiten.GeoM

	if err := p.SetOverlay("nope", 1); err == nil {
		t.Error("SetOverlay with an unknown name succeeded")
	}
	if err := p.SetOverlay("shift", 1); err != nil {
		t.Fatal(err)
	}
	p.sampleAndTransform(&g)
	// 0.5 model units * 32 px/unit = 16 pixels right of the plain pose.
	if got := p.verts[0][0].DstX; !within(got, 16, 0.01) {
		t.Errorf("overlaid vertex x = %v, want 16", got)
	}

	// Half weight halves the shift.
	if err := p.SetOverlay("shift", 0.5); err != nil {
		t.Fatal(err)
	}
	p.sampleAndTransform(&g)
	if got := p.verts[0][0].DstX; !within(got, 8, 0.01) {
		t.Errorf("half-weight vertex x = %v, want 8", got)
	}

	// Weight zero removes it.
	if err := p.SetOverlay("shift", 0); err != nil {
		t.Fatal(err)
	}
	p.sampleAndTransform(&g)
	if got := p.verts[0][0].DstX; !within(got, 0, 0.01) {
		t.Errorf("removed overlay vertex x = %v, want 0", got)
	}
}

func TestHitTest(t *testing.T) {
	p := newGeometryPlayer(twoPoseFile())

	// The "left" pose is a triangle with its base spanning canvas x in
	// [0, 32] at y=64 and its apex at (16, 0); its centroid is inside.
	if hits := p.HitTest(16, 32); len(hits) != 1 || hits[0] != "Tri" {
		t.Errorf("HitTest inside = %v, want [Tri]", hits)
	}
	if hits := p.HitTest(60, 32); hits != nil {
		t.Errorf("HitTest outside = %v, want none", hits)
	}
	// An invisible mesh cannot be hit.
	p.pose.states[0].visible = false
	if hits := p.HitTest(16, 32); hits != nil {
		t.Errorf("HitTest on an invisible mesh = %v, want none", hits)
	}
}

func TestModelNames(t *testing.T) {
	m := newModelCommon(twoPoseFile())
	if got := m.OverlayNames(); len(got) != 1 || got[0] != "shift" {
		t.Errorf("OverlayNames = %v", got)
	}
	if got := m.HitAreaNames(); len(got) != 1 || got[0] != "Tri" {
		t.Errorf("HitAreaNames = %v", got)
	}
}
