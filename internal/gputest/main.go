// Command gputest validates the mask render path on a real GPU: it draws a
// clipped and an inverted-clipped mesh, at 1x and under a scaled GeoM, and
// reads the pixels back.
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

// maskedFile: mesh 0 is an invisible mask shape covering the canvas's left
// half; mesh 1 is a full-canvas red quad clipped by it.
func maskedFile(inverted bool) *mofufmt.File {
	const q = 65535
	flags := mofufmt.MeshFlags(0)
	if inverted {
		flags = mofufmt.MeshInvertedMask
	}
	return &mofufmt.File{
		Canvas: mofufmt.Canvas{Width: 64, Height: 64, OriginX: 32, OriginY: 32, PixelsPerUnit: 32},
		Textures: []mofufmt.Texture{
			{Name: "white.png", Data: solidPNG(color.NRGBA{0xff, 0xff, 0xff, 0xff})},
			{Name: "red.png", Data: solidPNG(color.NRGBA{0xff, 0x00, 0x00, 0xff})},
		},
		Meshes: []mofufmt.Mesh{
			{ID: "maskShape", TextureIndex: 0,
				UVs: []float32{0, 0, 1, 0, 1, 1, 0, 1}, Indices: []uint16{0, 1, 2, 0, 2, 3},
				MinX: -1, MinY: -1, MaxX: 1, MaxY: 1},
			{ID: "clipped", TextureIndex: 1, Flags: flags, Masks: []int32{0},
				UVs: []float32{0, 0, 1, 0, 1, 1, 0, 1}, Indices: []uint16{0, 1, 2, 0, 2, 3},
				MinX: -1, MinY: -1, MaxX: 1, MaxY: 1},
		},
		Animations: []mofufmt.Animation{{
			Name: "@rest", FPS: 30, FrameCount: 1,
			Tracks: []mofufmt.Track{
				// The mask shape spans the left half (model x in [-1, 0]) and
				// is itself invisible, as real mask sources usually are.
				{Positions: []uint16{0, 0, q / 2, 0, q / 2, q, 0, q},
					Opacity: []float32{1}, Order: []int32{0}, Visible: []uint8{0}},
				{Positions: []uint16{0, 0, q, 0, q, q, 0, q},
					Opacity: []float32{1}, Order: []int32{1}, Visible: []uint8{1}},
			},
		}},
	}
}

type check struct {
	name    string
	x, y    int
	wantRed bool
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

	run := func(label string, inverted bool, scale float64, checks []check) {
		m, err := mofu.New(maskedFile(inverted))
		if err != nil {
			g.fails = append(g.fails, label+": "+err.Error())
			return
		}
		defer m.Dispose()
		p := m.NewPlayer()
		defer p.Dispose()

		size := int(64 * scale)
		dst := ebiten.NewImage(size, size)
		defer dst.Deallocate()
		var op mofu.DrawOptions
		op.GeoM.Scale(scale, scale)
		p.Draw(dst, &op)

		pix := make([]byte, size*size*4)
		dst.ReadPixels(pix)
		for _, c := range checks {
			i := (c.y*size + c.x) * 4
			r, a := pix[i], pix[i+3]
			isRed := r > 0xc0 && a > 0xc0
			isClear := a < 0x30
			switch {
			case c.wantRed && !isRed:
				g.fails = append(g.fails, fmt.Sprintf("%s/%s: got r=%d a=%d, want red", label, c.name, r, a))
			case !c.wantRed && !isClear:
				g.fails = append(g.fails, fmt.Sprintf("%s/%s: got r=%d a=%d, want transparent", label, c.name, r, a))
			}
		}
	}

	run("normal", false, 1, []check{
		{"inside-mask", 16, 32, true},
		{"outside-mask", 48, 32, false},
	})
	run("inverted", true, 1, []check{
		{"inside-mask", 16, 32, false},
		{"outside-mask", 48, 32, true},
	})
	// A scaled GeoM exercises the InvGeoM path in the shader.
	run("scaled-normal", false, 2, []check{
		{"inside-mask", 32, 64, true},
		{"outside-mask", 96, 64, false},
	})
	run("scaled-inverted", true, 2, []check{
		{"inside-mask", 32, 64, false},
		{"outside-mask", 96, 64, true},
	})
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
	fmt.Println("PASS: all mask pixel checks")
}
