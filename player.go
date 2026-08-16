package mofu

import (
	"fmt"
	"math"
	"sort"

	"github.com/eihigh/mofu/mofufmt"
	"github.com/hajimehoshi/ebiten/v2"
)

// Player is one playing instance of a Model. Models are shared; Players are
// not.
type Player struct {
	model *Model

	anim  int
	time  float64
	speed float64
	loop  bool
	// paused freezes Advance without losing the current time.
	paused bool

	// verts is the working vertex buffer, one slice per mesh.
	verts [][]ebiten.Vertex
	// order is the mesh draw order for the frame being drawn.
	order []int
	// state is the per-mesh sample of the frame being drawn.
	state []meshState

	maskBuf *ebiten.Image
	partBuf *ebiten.Image
}

// meshState is one mesh sampled at one instant.
type meshState struct {
	visible  bool
	opacity  float32
	order    int32
	multiply [4]float32
	screen   [4]float32
}

// NewPlayer creates a player positioned at the start of the first animation.
func (m *Model) NewPlayer() *Player {
	p := &Player{
		model: m,
		anim:  -1,
		speed: 1,
		verts: make([][]ebiten.Vertex, len(m.templates)),
		order: make([]int, len(m.templates)),
		state: make([]meshState, len(m.templates)),
	}
	for i, t := range m.templates {
		p.verts[i] = append([]ebiten.Vertex(nil), t...)
	}
	if len(m.file.Animations) > 0 {
		p.setAnimation(0)
	}
	return p
}

// Model returns the model this player draws.
func (p *Player) Model() *Model { return p.model }

// Play switches to the named animation and rewinds to its start. Playing the
// animation that is already current restarts it.
func (p *Player) Play(name string) error {
	i, ok := p.model.byName[name]
	if !ok {
		return fmt.Errorf("mofu: no animation named %q", name)
	}
	p.setAnimation(i)
	return nil
}

func (p *Player) setAnimation(i int) {
	p.anim = i
	p.time = 0
	p.loop = p.model.file.Animations[i].Loop
}

// Animation returns the current animation, or nil if the model has none.
func (p *Player) Animation() *mofufmt.Animation {
	if p.anim < 0 {
		return nil
	}
	return &p.model.file.Animations[p.anim]
}

// SetSpeed multiplies the playback rate. The default is 1.
func (p *Player) SetSpeed(s float64) { p.speed = s }

// SetLoop overrides the animation's own loop flag.
func (p *Player) SetLoop(loop bool) { p.loop = loop }

// SetPaused stops and resumes playback without rewinding.
func (p *Player) SetPaused(paused bool) { p.paused = paused }

// Time is the playback position in seconds.
func (p *Player) Time() float64 { return p.time }

// SetTime seeks to t seconds.
func (p *Player) SetTime(t float64) { p.time = t }

// Finished reports whether a non-looping animation has run past its end.
func (p *Player) Finished() bool {
	a := p.Animation()
	if a == nil || p.loop {
		return false
	}
	return p.time >= a.Duration()
}

// Update advances playback by one tick at the current TPS. Call it from your
// game's Update.
func (p *Player) Update() {
	p.Advance(1 / float64(ebiten.TPS()))
}

// Advance moves playback forward by dt seconds.
func (p *Player) Advance(dt float64) {
	if p.paused || p.anim < 0 {
		return
	}
	p.time += dt * p.speed
	if p.time < 0 {
		p.time = 0
	}
}

// DrawOptions controls where and how a player is drawn.
type DrawOptions struct {
	// GeoM maps canvas pixels (see Model.CanvasSize) to the destination.
	GeoM ebiten.GeoM
	// Alpha scales the whole model's opacity, for fades. The zero value is
	// treated as 1, so a freshly declared DrawOptions draws the model fully
	// opaque; to hide a model, skip the Draw call instead.
	Alpha float32
}

// Draw renders the current frame into dst.
func (p *Player) Draw(dst *ebiten.Image, opts *DrawOptions) {
	a := p.Animation()
	if a == nil || dst == nil {
		return
	}
	var o DrawOptions
	if opts != nil {
		o = *opts
	}
	if o.Alpha == 0 {
		o.Alpha = 1
	}

	f0, f1, t := p.frames(a)
	p.sample(a, f0, f1, t, o.Alpha)
	p.buildVertices(a, f0, f1, t, &o.GeoM)
	p.sortOrder()
	p.render(dst)
}

// frames resolves the playback time into a pair of frames and the blend
// factor between them.
func (p *Player) frames(a *mofufmt.Animation) (f0, f1 int, t float32) {
	n := int(a.FrameCount)
	if n <= 1 {
		return 0, 0, 0
	}
	pos := p.time * float64(a.FPS)
	if p.loop {
		pos = math.Mod(pos, float64(n))
		if pos < 0 {
			pos += float64(n)
		}
		f0 = int(pos)
		// The bake samples a looping motion over [0, duration), so the last
		// frame blends back into the first.
		f1 = (f0 + 1) % n
	} else {
		if pos < 0 {
			pos = 0
		}
		if pos > float64(n-1) {
			pos = float64(n - 1)
		}
		f0 = int(pos)
		f1 = f0 + 1
		if f1 > n-1 {
			f1 = n - 1
		}
	}
	return f0, f1, float32(pos - math.Floor(pos))
}

// sample reads the scalar channels of every mesh for the current instant.
func (p *Player) sample(a *mofufmt.Animation, f0, f1 int, t, alpha float32) {
	for i := range p.state {
		tr := &a.Tracks[i]
		s := &p.state[i]
		s.visible = tr.VisibleAt(f0)
		s.opacity = lerp(tr.OpacityAt(f0), tr.OpacityAt(f1), t) * alpha
		s.order = tr.OrderAt(f0)
		s.multiply = lerp4(tr.MultiplyAt(f0), tr.MultiplyAt(f1), t)
		s.screen = lerp4(tr.ScreenAt(f0), tr.ScreenAt(f1), t)
	}
}

// buildVertices interpolates and transforms the positions of every mesh.
func (p *Player) buildVertices(a *mofufmt.Animation, f0, f1 int, t float32, geom *ebiten.GeoM) {
	c := p.model.file.Canvas
	ppu := c.PixelsPerUnit
	if ppu == 0 {
		ppu = 1
	}
	// Cubism's canvas has Y pointing up from the bottom left; Ebitengine's
	// destination has Y pointing down from the top left.
	a32, b32, tx := geom.Element(0, 0), geom.Element(0, 1), geom.Element(0, 2)
	c32, d32, ty := geom.Element(1, 0), geom.Element(1, 1), geom.Element(1, 2)

	for i := range p.model.file.Meshes {
		mesh := &p.model.file.Meshes[i]
		vc := mesh.VertexCount()
		if vc == 0 {
			continue
		}
		q0 := a.Tracks[i].PositionFrame(f0, vc)
		q1 := a.Tracks[i].PositionFrame(f1, vc)
		if q0 == nil {
			continue
		}
		if q1 == nil {
			q1 = q0
		}
		verts := p.verts[i]
		for v := 0; v < vc; v++ {
			x := lerp(mesh.DequantX(q0[v*2]), mesh.DequantX(q1[v*2]), t)
			y := lerp(mesh.DequantY(q0[v*2+1]), mesh.DequantY(q1[v*2+1]), t)
			px := float64(x*ppu + c.OriginX)
			py := float64(c.Height - (y*ppu + c.OriginY))
			verts[v].DstX = float32(a32*px + b32*py + tx)
			verts[v].DstY = float32(c32*px + d32*py + ty)
		}
	}
}

// sortOrder sorts mesh indices by the render order of the current frame.
func (p *Player) sortOrder() {
	for i := range p.order {
		p.order[i] = i
	}
	sort.SliceStable(p.order, func(a, b int) bool {
		return p.state[p.order[a]].order < p.state[p.order[b]].order
	})
}

func lerp(a, b, t float32) float32 { return a + (b-a)*t }

func lerp4(a, b [4]float32, t float32) [4]float32 {
	return [4]float32{
		lerp(a[0], b[0], t),
		lerp(a[1], b[1], t),
		lerp(a[2], b[2], t),
		lerp(a[3], b[3], t),
	}
}
