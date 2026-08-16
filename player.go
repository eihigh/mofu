package mofu

import (
	"fmt"
	"math"
	"sort"

	"github.com/eihigh/mofu/mofufmt"
	"github.com/hajimehoshi/ebiten/v2"
)

// Event is a timed user-data entry that playback has crossed. Motions carry
// these for things like footsteps and lip-flap cues.
type Event struct {
	// Time is the position within the animation, in seconds.
	Time float64
	// Value is the user data string from the motion.
	Value string
}

// playhead is one animation being played.
type playhead struct {
	anim int
	time float64
	loop bool
	// eventMark is the playback position events have been collected up to.
	eventMark float64
}

// pose is the model sampled at one instant: canvas-space vertex positions and
// the scalar state of every mesh.
type pose struct {
	// positions is x,y per vertex per mesh, in canvas pixels with the origin
	// at the top left.
	positions [][]float32
	states    []meshState
}

// meshState is one mesh's scalar channels at one instant.
type meshState struct {
	visible  bool
	opacity  float32
	order    int32
	multiply [4]float32
	screen   [4]float32
}

func newPose(f *mofufmt.File) pose {
	p := pose{
		positions: make([][]float32, len(f.Meshes)),
		states:    make([]meshState, len(f.Meshes)),
	}
	for i := range f.Meshes {
		p.positions[i] = make([]float32, f.Meshes[i].VertexCount()*2)
	}
	return p
}

// Player is one playing instance of a Model. Models are shared; Players are
// not.
type Player struct {
	model *Model

	cur  playhead
	prev *playhead // the animation fading out, if any

	fadeDur     float64
	fadeElapsed float64

	speed  float64
	paused bool

	overlays map[string]float32
	events   []Event

	pose      pose // the blended result of the last Draw
	scratch   pose // prev's sample during a fade
	poseValid bool

	verts [][]ebiten.Vertex
	order []int

	// Per-Draw mask state; see render.go.
	maskUsable bool
	invGeoM    [6]float32
	maskReady  []bool
	maskEmpty  []bool
	maskRects  [][4]float32
	maskBufs   []*ebiten.Image
	maskVerts  []ebiten.Vertex
}

// NewPlayer creates a player positioned at the start of the first animation.
func (m *Model) NewPlayer() *Player {
	p := &Player{
		model:    m,
		cur:      playhead{anim: -1},
		speed:    1,
		overlays: map[string]float32{},
		pose:     newPose(m.file),
		scratch:  newPose(m.file),
		verts:    make([][]ebiten.Vertex, len(m.file.Meshes)),
		order:    make([]int, len(m.file.Meshes)),
	}
	for i := range m.file.Meshes {
		p.verts[i] = make([]ebiten.Vertex, m.file.Meshes[i].VertexCount())
		if i < len(m.templates) {
			copy(p.verts[i], m.templates[i])
		}
	}
	if len(m.file.Animations) > 0 {
		p.start(0, 0)
	}
	return p
}

// Model returns the model this player draws.
func (p *Player) Model() *Model { return p.model }

// Play switches to the named animation, cross-fading over the animation's
// baked FadeInTime. Playing the animation that is already current restarts
// it. With a zero fade time the switch is instant.
func (p *Player) Play(name string) error {
	i, ok := p.model.byName[name]
	if !ok {
		return fmt.Errorf("mofu: no animation named %q", name)
	}
	p.start(i, float64(p.model.file.Animations[i].FadeIn))
	return nil
}

// PlayWithFade is Play with an explicit cross-fade duration in seconds,
// overriding the animation's own FadeInTime.
func (p *Player) PlayWithFade(name string, fade float64) error {
	i, ok := p.model.byName[name]
	if !ok {
		return fmt.Errorf("mofu: no animation named %q", name)
	}
	p.start(i, fade)
	return nil
}

func (p *Player) start(i int, fade float64) {
	if fade > 0 && p.cur.anim >= 0 {
		prev := p.cur
		p.prev = &prev
		p.fadeDur = fade
		p.fadeElapsed = 0
	} else {
		p.prev = nil
		p.fadeDur = 0
	}
	p.cur = playhead{
		anim:      i,
		loop:      p.model.file.Animations[i].Loop,
		eventMark: -1e-9, // so an event at exactly t=0 fires
	}
}

// Animation returns the current animation, or nil if the model has none.
func (p *Player) Animation() *mofufmt.Animation {
	if p.cur.anim < 0 {
		return nil
	}
	return &p.model.file.Animations[p.cur.anim]
}

// SetSpeed multiplies the playback rate. The default is 1.
func (p *Player) SetSpeed(s float64) { p.speed = s }

// SetLoop overrides the current animation's loop flag.
func (p *Player) SetLoop(loop bool) { p.cur.loop = loop }

// SetPaused stops and resumes playback without rewinding.
func (p *Player) SetPaused(paused bool) { p.paused = paused }

// Paused reports whether playback is paused.
func (p *Player) Paused() bool { return p.paused }

// Time is the playback position in seconds.
func (p *Player) Time() float64 { return p.cur.time }

// SetTime seeks to t seconds. Events between the old and new position are
// skipped, not fired.
func (p *Player) SetTime(t float64) {
	p.cur.time = t
	p.cur.eventMark = t
}

// Finished reports whether a non-looping animation has run past its end.
func (p *Player) Finished() bool {
	a := p.Animation()
	if a == nil || p.cur.loop {
		return false
	}
	return p.cur.time >= a.Duration()
}

// SetOverlay layers a baked expression over playback with the given weight.
// Weight 1 applies it fully, 0 removes it; animate the weight over a few
// frames for a fade. Multiple overlays stack additively.
func (p *Player) SetOverlay(name string, weight float32) error {
	if _, ok := p.model.byOverlay[name]; !ok {
		return fmt.Errorf("mofu: no overlay named %q", name)
	}
	if weight == 0 {
		delete(p.overlays, name)
	} else {
		p.overlays[name] = weight
	}
	return nil
}

// Update advances playback by one tick at the current TPS. Call it from your
// game's Update.
func (p *Player) Update() {
	p.Advance(1 / float64(ebiten.TPS()))
}

// Advance moves playback forward by dt seconds.
func (p *Player) Advance(dt float64) {
	if p.paused || p.cur.anim < 0 {
		return
	}
	step := dt * p.speed
	if p.prev != nil {
		p.prev.time += step
		p.fadeElapsed += step
		if p.fadeElapsed >= p.fadeDur {
			p.prev = nil
		}
	}
	p.cur.time += step
	if p.cur.time < 0 {
		p.cur.time = 0
	}
	p.collectEvents()
}

// collectEvents moves the event cursor up to the current time, queueing every
// event occurrence in between.
func (p *Player) collectEvents() {
	a := p.Animation()
	t0, t1 := p.cur.eventMark, p.cur.time
	p.cur.eventMark = t1
	if len(a.Events) == 0 || t1 <= t0 {
		return
	}
	d := a.Duration()
	for _, e := range a.Events {
		et := float64(e.Time)
		if !p.cur.loop {
			if et > t0 && et <= t1 {
				p.events = append(p.events, Event{Time: et, Value: e.Value})
			}
			continue
		}
		if d <= 0 {
			continue
		}
		// The k-th loop repeats the event at et + k*d; queue every
		// occurrence in (t0, t1], capped in case of a huge dt.
		n := math.Floor((t0-et)/d) + 1
		if n < 0 {
			n = 0
		}
		for t, count := et+n*d, 0; t <= t1 && count < 16; t, count = t+d, count+1 {
			p.events = append(p.events, Event{Time: et, Value: e.Value})
		}
	}
}

// PollEvents returns the events crossed since the last call and clears the
// queue. Poll once per frame after Update.
func (p *Player) PollEvents() []Event {
	if len(p.events) == 0 {
		return nil
	}
	out := p.events
	p.events = nil
	return out
}

// frameAt resolves a playback time into a pair of frames and the blend factor
// between them.
func frameAt(a *mofufmt.Animation, time float64, loop bool) (f0, f1 int, t float32) {
	n := int(a.FrameCount)
	if n <= 1 {
		return 0, 0, 0
	}
	pos := time * float64(a.FPS)
	if loop {
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

// samplePose fills dst with the model's state under h, in canvas space.
func (p *Player) samplePose(h *playhead, dst *pose) {
	f := p.model.file
	a := &f.Animations[h.anim]
	f0, f1, t := frameAt(a, h.time, h.loop)

	c := f.Canvas
	ppu := c.PixelsPerUnit
	if ppu == 0 {
		ppu = 1
	}

	for i := range f.Meshes {
		mesh := &f.Meshes[i]
		vc := mesh.VertexCount()
		out := dst.positions[i]
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
		for v := 0; v < vc; v++ {
			x := lerp(mesh.DequantX(q0[v*2]), mesh.DequantX(q1[v*2]), t)
			y := lerp(mesh.DequantY(q0[v*2+1]), mesh.DequantY(q1[v*2+1]), t)
			// Model space has Y up with the origin at the canvas centre; the
			// canvas has Y down with the origin at the top left.
			out[v*2] = x*ppu + c.OriginX
			out[v*2+1] = c.Height - (y*ppu + c.OriginY)
		}
	}

	for i := range dst.states {
		tr := &a.Tracks[i]
		s := &dst.states[i]
		s.visible = tr.VisibleAt(f0)
		s.opacity = lerp(tr.OpacityAt(f0), tr.OpacityAt(f1), t)
		s.order = tr.OrderAt(f0)
		s.multiply = lerp4(tr.MultiplyAt(f0), tr.MultiplyAt(f1), t)
		s.screen = lerp4(tr.ScreenAt(f0), tr.ScreenAt(f1), t)
	}
}

// mixPose blends from into dst: dst = dst*(1-w) + from*w.
func mixPose(dst, from *pose, w float32) {
	for i := range dst.positions {
		a, b := dst.positions[i], from.positions[i]
		for v := range a {
			a[v] = lerp(a[v], b[v], w)
		}
	}
	for i := range dst.states {
		s, o := &dst.states[i], &from.states[i]
		s.opacity = lerp(s.opacity, o.opacity, w)
		s.multiply = lerp4(s.multiply, o.multiply, w)
		s.screen = lerp4(s.screen, o.screen, w)
		if w > 0.5 {
			s.visible = o.visible
			s.order = o.order
		}
	}
}

// applyOverlays adds the active baked expressions to the pose.
func (p *Player) applyOverlays() {
	for name, w := range p.overlays {
		oi := p.model.byOverlay[name]
		deltas := p.model.overlayDeltas[oi]
		tracks := p.model.file.Overlays[oi].Tracks
		for mi := range p.pose.positions {
			if d := deltas[mi]; d != nil {
				out := p.pose.positions[mi]
				for v := range out {
					out[v] += d[v] * w
				}
			}
			if mi < len(tracks) {
				s := &p.pose.states[mi]
				s.opacity = clamp01(s.opacity + tracks[mi].DeltaOpacity*w)
			}
		}
	}
}

// DrawOptions controls where and how a player is drawn.
type DrawOptions struct {
	// GeoM maps canvas pixels (see Model.CanvasSize) to the destination.
	GeoM ebiten.GeoM
	// ColorScale scales the whole model's colours and opacity, for tints and
	// fades. Its zero value is the identity, matching Ebitengine's own draw
	// options.
	ColorScale ebiten.ColorScale
}

// Draw renders the current frame into dst.
func (p *Player) Draw(dst *ebiten.Image, opts *DrawOptions) {
	if p.cur.anim < 0 || dst == nil {
		return
	}
	var o DrawOptions
	if opts != nil {
		o = *opts
	}

	p.samplePose(&p.cur, &p.pose)
	if p.prev != nil && p.fadeDur > 0 {
		p.samplePose(p.prev, &p.scratch)
		w := 1 - float32(p.fadeElapsed/p.fadeDur)
		if w > 0 {
			mixPose(&p.pose, &p.scratch, w)
		}
	}
	p.applyOverlays()
	p.applyColorScale(&o.ColorScale)
	p.poseValid = true

	p.buildVerts(&o.GeoM)
	p.prepareFrame(&o.GeoM)
	p.sortOrder()
	p.render(dst)
}

// applyColorScale folds a whole-model colour scale into the pose: alpha into
// each mesh's opacity, RGB into its multiply colour.
func (p *Player) applyColorScale(cs *ebiten.ColorScale) {
	r, g, b, a := cs.R(), cs.G(), cs.B(), cs.A()
	if r == 1 && g == 1 && b == 1 && a == 1 {
		return
	}
	for i := range p.pose.states {
		s := &p.pose.states[i]
		s.opacity *= a
		s.multiply[0] *= r
		s.multiply[1] *= g
		s.multiply[2] *= b
	}
}

// buildVerts transforms the pose's canvas-space positions through geom into
// the vertex buffers.
func (p *Player) buildVerts(geom *ebiten.GeoM) {
	a := geom.Element(0, 0)
	b := geom.Element(0, 1)
	tx := geom.Element(0, 2)
	c := geom.Element(1, 0)
	d := geom.Element(1, 1)
	ty := geom.Element(1, 2)
	for i := range p.verts {
		pos := p.pose.positions[i]
		verts := p.verts[i]
		for v := range verts {
			x, y := float64(pos[v*2]), float64(pos[v*2+1])
			verts[v].DstX = float32(a*x + b*y + tx)
			verts[v].DstY = float32(c*x + d*y + ty)
		}
	}
}

// sortOrder sorts mesh indices by the render order of the current pose.
func (p *Player) sortOrder() {
	for i := range p.order {
		p.order[i] = i
	}
	sort.SliceStable(p.order, func(a, b int) bool {
		return p.pose.states[p.order[a]].order < p.pose.states[p.order[b]].order
	})
}

// HitTest returns the names of the hit areas containing the given canvas-space
// point, testing against the pose of the most recent Draw. To convert a
// screen-space point, invert the GeoM you draw with:
//
//	g := op.GeoM
//	g.Invert()
//	cx, cy := g.Apply(mouseX, mouseY)
//	hits := player.HitTest(cx, cy)
func (p *Player) HitTest(x, y float64) []string {
	if !p.poseValid {
		if p.cur.anim < 0 {
			return nil
		}
		p.samplePose(&p.cur, &p.pose)
		p.poseValid = true
	}
	var hits []string
	for _, h := range p.model.file.HitAreas {
		mi := int(h.Mesh)
		if mi < 0 || mi >= len(p.pose.positions) {
			continue
		}
		if !p.pose.states[mi].visible {
			continue
		}
		if meshContains(p.pose.positions[mi], p.model.file.Meshes[mi].Indices, float32(x), float32(y)) {
			hits = append(hits, h.Name)
		}
	}
	return hits
}

// meshContains reports whether any triangle of the mesh contains the point.
func meshContains(pos []float32, indices []uint16, x, y float32) bool {
	for i := 0; i+2 < len(indices); i += 3 {
		a, b, c := int(indices[i])*2, int(indices[i+1])*2, int(indices[i+2])*2
		if c+1 >= len(pos) {
			continue
		}
		if pointInTriangle(x, y,
			pos[a], pos[a+1], pos[b], pos[b+1], pos[c], pos[c+1]) {
			return true
		}
	}
	return false
}

// pointInTriangle uses the same-side sign test, tolerant of either winding.
func pointInTriangle(px, py, ax, ay, bx, by, cx, cy float32) bool {
	d1 := (px-bx)*(ay-by) - (ax-bx)*(py-by)
	d2 := (px-cx)*(by-cy) - (bx-cx)*(py-cy)
	d3 := (px-ax)*(cy-ay) - (cx-ax)*(py-ay)
	hasNeg := d1 < 0 || d2 < 0 || d3 < 0
	hasPos := d1 > 0 || d2 > 0 || d3 > 0
	return !(hasNeg && hasPos)
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

func clamp01(v float32) float32 {
	switch {
	case v < 0:
		return 0
	case v > 1:
		return 1
	}
	return v
}
