// Command gputest validates the render path on a real GPU, pixel by pixel:
// the colour maths of the shaders (multiply, screen, opacity), the three
// Cubism blend modes, clipping masks (normal, inverted, and under a scaled
// GeoM), cross-fading, overlays, and whole-model colour scaling.
//
// It is a program rather than a go test because Ebitengine's game loop needs
// the process's main thread, which test binaries do not hand over. Run it
// wherever a GL context exists; on a headless machine:
//
//	xvfb-run -a go run ./internal/gputest
package main

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"

	"github.com/eihigh/mofu"
	"github.com/eihigh/mofu/mofufmt"
	"github.com/hajimehoshi/ebiten/v2"
)

func solidPNG(c color.NRGBA) []byte {
	img := image.NewNRGBA(image.Rect(0, 0, 8, 8))
	for i := 0; i < len(img.Pix); i += 4 {
		img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3] = c.R, c.G, c.B, c.A
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		panic(err)
	}
	return buf.Bytes()
}

const q = 65535

// canvas is the 64x64 test canvas every scene uses.
var canvas = mofufmt.Canvas{Width: 64, Height: 64, OriginX: 32, OriginY: 32, PixelsPerUnit: 32}

// fullQuad returns quantised positions for a quad covering the whole canvas.
func fullQuad() []uint16 { return []uint16{0, 0, q, 0, q, q, 0, q} }

// leftQuad covers the canvas's left half, rightQuad its right half.
func leftQuad() []uint16  { return []uint16{0, 0, q / 2, 0, q / 2, q, 0, q} }
func rightQuad() []uint16 { return []uint16{q / 2, 0, q, 0, q, q, q / 2, q} }

func quadMesh(texIndex int32, flags mofufmt.MeshFlags, masks []int32) mofufmt.Mesh {
	return mofufmt.Mesh{
		ID: "quad", TextureIndex: texIndex, Flags: flags, Masks: masks,
		UVs:     []float32{0, 0, 1, 0, 1, 1, 0, 1},
		Indices: []uint16{0, 1, 2, 0, 2, 3},
		MinX:    -1, MinY: -1, MaxX: 1, MaxY: 1,
	}
}

func track(pos []uint16, opacity float32, visible uint8) mofufmt.Track {
	return mofufmt.Track{
		Positions: pos, Opacity: []float32{opacity}, Order: []int32{0}, Visible: []uint8{visible},
	}
}

// oneQuad is a single full-canvas quad of the given texture colour, with
// optional flags, opacity and multiply/screen colours.
func oneQuad(tex color.NRGBA, flags mofufmt.MeshFlags, opacity float32, colors []float32) *mofufmt.File {
	tr := track(fullQuad(), opacity, 1)
	if colors != nil {
		tr.Colors = colors
		tr.Flags |= mofufmt.TrackHasColors
	}
	return &mofufmt.File{
		Canvas:   canvas,
		Textures: []mofufmt.Texture{{Name: "t.png", Data: solidPNG(tex)}},
		Meshes:   []mofufmt.Mesh{quadMesh(0, flags, nil)},
		Animations: []mofufmt.Animation{{
			Name: "@rest", FPS: 30, FrameCount: 1, Tracks: []mofufmt.Track{tr},
		}},
	}
}

// maskedFile: mesh 0 is an invisible mask shape covering the canvas's left
// half; mesh 1 is a full-canvas red quad clipped by it.
func maskedFile(inverted bool) *mofufmt.File {
	flags := mofufmt.MeshFlags(0)
	if inverted {
		flags = mofufmt.MeshInvertedMask
	}
	return &mofufmt.File{
		Canvas: canvas,
		Textures: []mofufmt.Texture{
			{Name: "white.png", Data: solidPNG(color.NRGBA{0xff, 0xff, 0xff, 0xff})},
			{Name: "red.png", Data: solidPNG(color.NRGBA{0xff, 0x00, 0x00, 0xff})},
		},
		Meshes: []mofufmt.Mesh{
			quadMesh(0, 0, nil),
			quadMesh(1, flags, []int32{0}),
		},
		Animations: []mofufmt.Animation{{
			Name: "@rest", FPS: 30, FrameCount: 1,
			Tracks: []mofufmt.Track{
				track(leftQuad(), 1, 0), // the mask shape itself is invisible
				track(fullQuad(), 1, 1),
			},
		}},
	}
}

// fadeFile has two single-frame animations, "left" and "right", with the quad
// pinned to each half; "right" fades in over one second.
func fadeFile() *mofufmt.File {
	return &mofufmt.File{
		Canvas:   canvas,
		Textures: []mofufmt.Texture{{Name: "red.png", Data: solidPNG(color.NRGBA{0xff, 0, 0, 0xff})}},
		Meshes:   []mofufmt.Mesh{quadMesh(0, 0, nil)},
		Animations: []mofufmt.Animation{
			{Name: "left", FPS: 30, FrameCount: 1, Tracks: []mofufmt.Track{track(leftQuad(), 1, 1)}},
			{Name: "right", FPS: 30, FrameCount: 1, FadeIn: 1, Tracks: []mofufmt.Track{track(rightQuad(), 1, 1)}},
		},
	}
}

// overlayFile is a left-half quad plus an overlay that shifts it one model
// unit (half the canvas) to the right.
func overlayFile() *mofufmt.File {
	f := &mofufmt.File{
		Canvas:   canvas,
		Textures: []mofufmt.Texture{{Name: "red.png", Data: solidPNG(color.NRGBA{0xff, 0, 0, 0xff})}},
		Meshes:   []mofufmt.Mesh{quadMesh(0, 0, nil)},
		Animations: []mofufmt.Animation{{
			Name: "@rest", FPS: 30, FrameCount: 1, Tracks: []mofufmt.Track{track(leftQuad(), 1, 1)},
		}},
	}
	f.Overlays = []mofufmt.Overlay{{
		Name: "shift",
		Tracks: []mofufmt.OverlayTrack{{
			DeltaPositions: []float32{1, 0, 1, 0, 1, 0, 1, 0},
		}},
	}}
	return f
}

// check compares one pixel against an expected colour within a tolerance.
type check struct {
	name string
	x, y int
	want color.RGBA
	tol  int
}

type scenario struct {
	name   string
	file   *mofufmt.File
	bg     color.RGBA                 // destination fill before drawing
	scale  float64                    // GeoM scale; 0 means 1
	setup  func(p *mofu.Player) error // play/advance/overlays
	opts   func(op *mofu.DrawOptions) // extra draw options
	checks []check
}

func scenarios() []scenario {
	red := color.RGBA{0xff, 0, 0, 0xff}
	clear := color.RGBA{}
	return []scenario{
		{
			name: "plain", file: oneQuad(color.NRGBA{0xff, 0, 0, 0xff}, 0, 1, nil),
			checks: []check{{"center", 32, 32, red, 12}},
		},
		{
			// Multiply colour halves the red channel of a straight red.
			name: "multiply-color", file: oneQuad(color.NRGBA{0xff, 0, 0, 0xff}, 0, 1,
				[]float32{0.5, 1, 1, 1, 0, 0, 0, 1}),
			checks: []check{{"center", 32, 32, color.RGBA{0x80, 0, 0, 0xff}, 12}},
		},
		{
			// Screen colour: red + (0, 0.5, 0) - red*(0, 0.5, 0) = (1, 0.5, 0).
			name: "screen-color", file: oneQuad(color.NRGBA{0xff, 0, 0, 0xff}, 0, 1,
				[]float32{1, 1, 1, 1, 0, 0.5, 0, 1}),
			checks: []check{{"center", 32, 32, color.RGBA{0xff, 0x80, 0, 0xff}, 12}},
		},
		{
			// Track opacity premultiplies into the output.
			name: "opacity", file: oneQuad(color.NRGBA{0xff, 0, 0, 0xff}, 0, 0.5, nil),
			checks: []check{{"center", 32, 32, color.RGBA{0x80, 0, 0, 0x80}, 12}},
		},
		{
			// Additive red over a green ground gives yellow.
			name: "blend-additive", file: oneQuad(color.NRGBA{0xff, 0, 0, 0xff}, mofufmt.MeshBlendAdditive, 1, nil),
			bg:     color.RGBA{0, 0xff, 0, 0xff},
			checks: []check{{"center", 32, 32, color.RGBA{0xff, 0xff, 0, 0xff}, 12}},
		},
		{
			// Multiplicative grey over a white ground stays grey.
			name: "blend-multiplicative", file: oneQuad(color.NRGBA{0x80, 0x80, 0x80, 0xff}, mofufmt.MeshBlendMultiplicative, 1, nil),
			bg:     color.RGBA{0xff, 0xff, 0xff, 0xff},
			checks: []check{{"center", 32, 32, color.RGBA{0x80, 0x80, 0x80, 0xff}, 12}},
		},
		{
			// ColorScale fades the whole model.
			name: "color-scale", file: oneQuad(color.NRGBA{0xff, 0, 0, 0xff}, 0, 1, nil),
			opts:   func(op *mofu.DrawOptions) { op.ColorScale.ScaleAlpha(0.5) },
			checks: []check{{"center", 32, 32, color.RGBA{0x80, 0, 0, 0x80}, 12}},
		},
		{
			// Halfway through a fade from the left pose to the right pose,
			// the quad sits centred.
			name: "crossfade", file: fadeFile(),
			setup: func(p *mofu.Player) error {
				if err := p.Play("right"); err != nil {
					return err
				}
				p.Advance(0.5)
				return nil
			},
			checks: []check{
				{"center", 32, 32, red, 12},
				{"far-left", 8, 32, clear, 12},
				{"far-right", 56, 32, clear, 12},
			},
		},
		{
			// A full-weight overlay shifts the left-half quad to the right half.
			name: "overlay", file: overlayFile(),
			setup: func(p *mofu.Player) error { return p.SetOverlay("shift", 1) },
			checks: []check{
				{"shifted-into", 48, 32, red, 12},
				{"shifted-out-of", 16, 32, clear, 12},
			},
		},
		{
			name: "mask-normal", file: maskedFile(false),
			checks: []check{
				{"inside-mask", 16, 32, red, 12},
				{"outside-mask", 48, 32, clear, 12},
			},
		},
		{
			name: "mask-inverted", file: maskedFile(true),
			checks: []check{
				{"inside-mask", 16, 32, clear, 12},
				{"outside-mask", 48, 32, red, 12},
			},
		},
		{
			// A scaled GeoM exercises the shader's inverse-transform path.
			name: "mask-normal-scaled", file: maskedFile(false), scale: 2,
			checks: []check{
				{"inside-mask", 32, 64, red, 12},
				{"outside-mask", 96, 64, clear, 12},
			},
		},
		{
			name: "mask-inverted-scaled", file: maskedFile(true), scale: 2,
			checks: []check{
				{"inside-mask", 32, 64, clear, 12},
				{"outside-mask", 96, 64, red, 12},
			},
		},
	}
}

type game struct {
	frame int
	fails []string
	done  bool
}

func (g *game) Update() error {
	g.frame++
	if g.done {
		return ebiten.Termination
	}
	return nil
}

func (g *game) Draw(screen *ebiten.Image) {
	if g.frame < 2 || g.done {
		return
	}
	g.done = true
	for _, sc := range scenarios() {
		g.run(sc)
	}
}

func (g *game) run(sc scenario) {
	fail := func(format string, args ...any) {
		g.fails = append(g.fails, sc.name+": "+fmt.Sprintf(format, args...))
	}
	m, err := mofu.New(sc.file)
	if err != nil {
		fail("%v", err)
		return
	}
	defer m.Dispose()
	p := m.NewPlayer()
	defer p.Dispose()
	if sc.setup != nil {
		if err := sc.setup(p); err != nil {
			fail("setup: %v", err)
			return
		}
	}

	scale := sc.scale
	if scale == 0 {
		scale = 1
	}
	size := int(64 * scale)
	dst := ebiten.NewImage(size, size)
	defer dst.Deallocate()
	if sc.bg.A > 0 {
		dst.Fill(sc.bg)
	}
	var op mofu.DrawOptions
	op.GeoM.Scale(scale, scale)
	if sc.opts != nil {
		sc.opts(&op)
	}
	p.Draw(dst, &op)

	pix := make([]byte, size*size*4)
	dst.ReadPixels(pix)
	for _, c := range sc.checks {
		i := (c.y*size + c.x) * 4
		got := color.RGBA{pix[i], pix[i+1], pix[i+2], pix[i+3]}
		if !within(got, c.want, c.tol) {
			fail("%s at (%d,%d): got %v, want %v ±%d", c.name, c.x, c.y, got, c.want, c.tol)
		}
	}
}

func within(got, want color.RGBA, tol int) bool {
	d := func(a, b uint8) int {
		if a > b {
			return int(a - b)
		}
		return int(b - a)
	}
	return d(got.R, want.R) <= tol && d(got.G, want.G) <= tol &&
		d(got.B, want.B) <= tol && d(got.A, want.A) <= tol
}

func (g *game) Layout(w, h int) (int, int) { return 64, 64 }

func main() {
	g := &game{}
	if err := ebiten.RunGame(g); err != nil && err != ebiten.Termination {
		fmt.Fprintln(os.Stderr, "run:", err)
		os.Exit(2)
	}
	if len(g.fails) > 0 {
		for _, f := range g.fails {
			fmt.Println("FAIL:", f)
		}
		os.Exit(1)
	}
	fmt.Printf("PASS: %d scenarios, all pixel checks\n", len(scenarios()))
}
