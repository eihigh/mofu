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
	"strings"

	"github.com/eihigh/mofu"
	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/ebitenutil"
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

	model    *mofu.Model
	player   *mofu.Player
	names    []string
	overlays []string
	active   map[string]bool
	cur      int
	geom     ebiten.GeoM
	lastHit  string
	events   []string
}

func (v *viewer) load() error {
	m, err := mofu.LoadFile(v.path)
	if err != nil {
		return err
	}
	v.model = m
	v.player = m.NewPlayer()
	v.names = m.AnimationNames()
	v.overlays = m.OverlayNames()
	v.active = map[string]bool{}
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
	switch {
	case inpututil.IsKeyJustPressed(ebiten.KeyRight):
		v.step(1)
	case inpututil.IsKeyJustPressed(ebiten.KeyLeft):
		v.step(-1)
	case inpututil.IsKeyJustPressed(ebiten.KeySpace):
		v.player.SetPaused(!v.player.Paused())
	case inpututil.IsKeyJustPressed(ebiten.KeyR):
		v.player.SetTime(0)
	case inpututil.IsKeyJustPressed(ebiten.KeyEscape):
		return ebiten.Termination
	}
	// Number keys toggle overlays (baked expressions).
	for i, name := range v.overlays {
		if i >= 9 {
			break
		}
		if inpututil.IsKeyJustPressed(ebiten.Key1 + ebiten.Key(i)) {
			v.active[name] = !v.active[name]
			w := float32(0)
			if v.active[name] {
				w = 1
			}
			if err := v.player.SetOverlay(name, w); err != nil {
				fmt.Fprintln(os.Stderr, err)
			}
		}
	}
	// Hit testing under the cursor.
	mx, my := ebiten.CursorPosition()
	g := v.geom
	if g.IsInvertible() {
		g.Invert()
		cx, cy := g.Apply(float64(mx), float64(my))
		v.lastHit = strings.Join(v.player.HitTest(cx, cy), ", ")
	}

	v.player.Update()
	for _, e := range v.player.PollEvents() {
		v.events = append(v.events, fmt.Sprintf("%.2fs %s", e.Time, e.Value))
		if len(v.events) > 5 {
			v.events = v.events[1:]
		}
	}
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
	op.GeoM.Translate(-cw/2, -ch/2)
	op.GeoM.Scale(s, s)
	op.GeoM.Translate(sw/2, sh/2)
	v.geom = op.GeoM
	v.player.Draw(screen, &op)

	var b strings.Builder
	name := "-"
	if len(v.names) > 0 {
		name = v.names[v.cur]
	}
	fmt.Fprintf(&b, "%s  %.2fs", name, v.player.Time())
	if v.player.Paused() {
		b.WriteString("  [paused]")
	}
	b.WriteString("\nleft/right: switch  space: pause  r: rewind")
	for i, o := range v.overlays {
		if i >= 9 {
			break
		}
		mark := " "
		if v.active[o] {
			mark = "*"
		}
		fmt.Fprintf(&b, "\n%d:%s%s", i+1, mark, o)
	}
	if v.lastHit != "" {
		fmt.Fprintf(&b, "\nhit: %s", v.lastHit)
	}
	for _, e := range v.events {
		fmt.Fprintf(&b, "\nevent %s", e)
	}
	ebitenutil.DebugPrint(screen, b.String())
}

func (v *viewer) Layout(w, h int) (int, int) { return w, h }
