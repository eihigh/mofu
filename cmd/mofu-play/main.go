// Command mofu-play previews a baked .mofu file in a window.
//
// It is a separate binary from `mofu` on purpose: it links Ebitengine, which
// needs a display, while the converter must stay usable on headless build
// machines.
package main

import (
	"flag"
	"fmt"
	"image/color"
	"os"

	"github.com/eihigh/mofu"
	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/inpututil"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "mofu-play:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	fs := flag.NewFlagSet("mofu-play", flag.ExitOnError)
	width := fs.Int("w", 720, "window width")
	height := fs.Int("h", 1080, "window height")
	anim := fs.String("a", "", "animation to start with (default: the first one)")
	scale := fs.Float64("scale", 1, "extra zoom on top of fitting the window")
	rest, err := parseFlags(fs, args)
	if err != nil {
		return err
	}
	if len(rest) != 1 {
		fs.Usage()
		return fmt.Errorf("expected exactly one .mofu file")
	}
	path := rest[0]

	ebiten.SetWindowSize(*width, *height)
	ebiten.SetWindowTitle("mofu - " + path)
	ebiten.SetWindowResizingMode(ebiten.WindowResizingModeEnabled)

	g := &viewer{path: path, startAnim: *anim, zoom: *scale}
	return ebiten.RunGame(g)
}

// parseFlags parses args, tolerating flags that appear after the positional
// argument.
func parseFlags(fs *flag.FlagSet, args []string) ([]string, error) {
	var positional []string
	for {
		if err := fs.Parse(args); err != nil {
			return nil, err
		}
		if fs.NArg() == 0 {
			return positional, nil
		}
		positional = append(positional, fs.Arg(0))
		args = fs.Args()[1:]
	}
}

// viewer is a minimal harness: it exists to exercise the runtime and to give
// a quick look at a baked file.
type viewer struct {
	path      string
	startAnim string
	zoom      float64

	model  *mofu.Model
	player *mofu.Player
	names  []string
	cur    int
}

func (v *viewer) load() error {
	m, err := mofu.LoadFile(v.path)
	if err != nil {
		return err
	}
	v.model = m
	v.player = m.NewPlayer()
	v.names = m.AnimationNames()
	if v.startAnim != "" {
		if err := v.player.Play(v.startAnim); err != nil {
			return err
		}
		for i, n := range v.names {
			if n == v.startAnim {
				v.cur = i
			}
		}
	}
	return nil
}

func (v *viewer) Update() error {
	if v.model == nil {
		if err := v.load(); err != nil {
			return err
		}
	}
	if inpututil.IsKeyJustPressed(ebiten.KeyRight) || inpututil.IsKeyJustPressed(ebiten.KeySpace) {
		v.step(1)
	}
	if inpututil.IsKeyJustPressed(ebiten.KeyLeft) {
		v.step(-1)
	}
	if inpututil.IsKeyJustPressed(ebiten.KeyEscape) {
		return ebiten.Termination
	}
	v.player.Update()
	return nil
}

func (v *viewer) step(d int) {
	if len(v.names) == 0 {
		return
	}
	v.cur = (v.cur + d + len(v.names)) % len(v.names)
	if err := v.player.Play(v.names[v.cur]); err != nil {
		fmt.Fprintln(os.Stderr, err)
	}
}

func (v *viewer) Draw(screen *ebiten.Image) {
	screen.Fill(color.RGBA{0x1e, 0x1e, 0x22, 0xff})
	if v.player == nil {
		return
	}
	cw, ch := v.model.CanvasSize()
	sw, sh := float64(screen.Bounds().Dx()), float64(screen.Bounds().Dy())
	s := min(sw/cw, sh/ch) * v.zoom

	var op mofu.DrawOptions
	op.Alpha = 1
	op.GeoM.Translate(-cw/2, -ch/2)
	op.GeoM.Scale(s, s)
	op.GeoM.Translate(sw/2, sh/2)
	v.player.Draw(screen, &op)

	if len(v.names) > 0 {
		ebiten.SetWindowTitle(fmt.Sprintf("mofu - %s [%s]  (left/right to switch)", v.path, v.names[v.cur]))
	}
}

func (v *viewer) Layout(w, h int) (int, int) { return w, h }
