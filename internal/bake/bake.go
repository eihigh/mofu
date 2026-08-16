// Package bake converts a Cubism model export into the .mofu container.
//
// The conversion is a sampling job: every motion is stepped at a fixed rate,
// the Cubism Core is asked to deform the model at each step, and the resulting
// vertex positions and per-drawable channels are recorded. Nothing about the
// deformer survives into the output, which is the whole point -- the runtime
// then needs no Core.
package bake

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/eihigh/mofu/internal/core"
	"github.com/eihigh/mofu/internal/cubism"
	"github.com/eihigh/mofu/internal/physics"
	"github.com/eihigh/mofu/mofufmt"
)

// settleSeconds is how long physics is run with frozen parameters before an
// animation starts recording, so pendulums begin from equilibrium instead of
// from their reset pose.
const settleSeconds = 2.0

// RestAnimation is the name given to the single-frame animation holding the
// model's default pose. It is always emitted first.
const RestAnimation = "@rest"

// Options configures a bake run.
type Options struct {
	// CorePath is the Cubism Core library to load. Empty means autodetect.
	CorePath string
	// FPS is the sampling rate. Zero means "use each motion's own Meta.Fps".
	FPS float64
	// Motions are extra .motion3.json paths to bake in addition to the ones
	// the model3.json references.
	Motions []string
	// SkipPhysics leaves physics out even when the model has a physics3.json.
	SkipPhysics bool
	// SkipExpressions leaves expressions out instead of baking them as
	// overlays.
	SkipExpressions bool
	// Uncompressed leaves the output body uncompressed.
	Uncompressed bool
	// Logf, when set, receives progress messages.
	Logf func(format string, args ...any)
}

func (o *Options) logf(format string, args ...any) {
	if o.Logf != nil {
		o.Logf(format, args...)
	}
}

// Result is a baked model plus the notes worth showing the user.
type Result struct {
	File     *mofufmt.File
	Warnings []string
}

// Run bakes the model3.json at path.
func Run(path string, opts Options) (*Result, error) {
	m3, err := cubism.LoadModel3(path)
	if err != nil {
		return nil, err
	}

	c, err := core.Load(opts.CorePath)
	if err != nil {
		return nil, err
	}
	defer c.Close()
	opts.logf("cubism core %s (%s)", c.Version(), c.Path())
	c.SetLogFunction(func(msg string) { opts.logf("core: %s", strings.TrimRight(msg, "\r\n")) })

	moc3, err := os.ReadFile(m3.Resolve(m3.FileReferences.Moc))
	if err != nil {
		return nil, err
	}
	model, err := c.NewModel(moc3)
	if err != nil {
		return nil, err
	}

	b := &baker{model: model, m3: m3, opts: &opts}
	b.loadPhysics()
	return b.run()
}

// loadPhysics wires up the physics3.json referenced by the model, if any.
func (b *baker) loadPhysics() {
	rel := b.m3.FileReferences.Physics
	if rel == "" {
		return
	}
	if b.opts.SkipPhysics {
		b.warn("physics (%s) skipped (-physics=false)", rel)
		return
	}
	data, err := os.ReadFile(b.m3.Resolve(rel))
	if err != nil {
		b.warn("physics (%s) skipped: %v", rel, err)
		return
	}
	sim, err := physics.New(data)
	if err != nil {
		b.warn("physics (%s) skipped: %v", rel, err)
		return
	}
	b.sim = sim
	b.opts.logf("physics %s: %d pendulum chains, simulated offline", rel, sim.SettingCount())
	b.warn("physics is baked from motion parameters only (approximation); live input such as dragging is not reproduced")
}

// physicsParams adapts the Core's parameter storage to physics.Params.
type physicsParams struct{ b *baker }

func (p physicsParams) Get(id string) (float32, bool) {
	i, ok := p.b.paramIndex[id]
	if !ok {
		return 0, false
	}
	return p.b.paramValues[i], true
}

func (p physicsParams) Set(id string, v float32) {
	if i, ok := p.b.paramIndex[id]; ok {
		p.b.paramValues[i] = v
	}
}

func (p physicsParams) Range(id string) (min, max, def float32, ok bool) {
	i, ok := p.b.paramIndex[id]
	if !ok {
		return 0, 0, 0, false
	}
	return p.b.paramMins[i], p.b.paramMaxs[i], p.b.paramDefaults[i], true
}

// baker holds the state shared by both sampling passes.
type baker struct {
	model *core.Model
	m3    *cubism.Model3
	opts  *Options
	sim   *physics.Simulator

	warnings []string

	// Pointers into Core memory. They stay valid for the model's lifetime,
	// so they are resolved once.
	paramValues   []float32
	paramDefaults []float32
	paramMins     []float32
	paramMaxs     []float32
	paramIndex    map[string]int
	partOpacities []float32
	partIndex     map[string]int

	positions  [][]core.Vec2
	opacities  []float32
	orders     []int32
	dynFlags   []core.DynamicFlags
	multiplies []core.Vec4
	screens    []core.Vec4

	meshes  []mofufmt.Mesh
	sources []source
}

// source is one animation to bake.
type source struct {
	name    string
	motion  *cubism.Motion3 // nil for the rest pose
	sound   string
	loop    bool
	fadeIn  float32
	fadeOut float32
	fps     float64
	frames  int
}

func (b *baker) warn(format string, args ...any) {
	b.warnings = append(b.warnings, fmt.Sprintf(format, args...))
}

func (b *baker) run() (*Result, error) {
	b.bind()
	if err := b.buildMeshes(); err != nil {
		return nil, err
	}
	b.collectSources()
	b.measure()

	f := &mofufmt.File{Meshes: b.meshes}
	size, origin, ppu := b.model.CanvasInfo()
	f.Canvas = mofufmt.Canvas{
		Width:         size.X,
		Height:        size.Y,
		OriginX:       origin.X,
		OriginY:       origin.Y,
		PixelsPerUnit: ppu,
	}
	textures, err := b.loadTextures()
	if err != nil {
		return nil, err
	}
	f.Textures = textures

	for i := range b.sources {
		f.Animations = append(f.Animations, *b.bakeAnimation(&b.sources[i]))
	}
	b.bakeHitAreas(f)
	b.bakeOverlays(f)
	b.noteUnbakeables()
	return &Result{File: f, Warnings: b.warnings}, nil
}

// bakeHitAreas maps the model3.json hit areas onto mesh indices.
func (b *baker) bakeHitAreas(f *mofufmt.File) {
	byID := make(map[string]int, len(f.Meshes))
	for i := range f.Meshes {
		byID[f.Meshes[i].ID] = i
	}
	for _, h := range b.m3.HitAreas {
		mi, ok := byID[h.Id]
		if !ok {
			b.warn("hit area %q points at unknown drawable %q, skipped", h.Name, h.Id)
			continue
		}
		name := h.Name
		if name == "" {
			name = h.Id
		}
		f.HitAreas = append(f.HitAreas, mofufmt.HitArea{Name: name, Mesh: int32(mi)})
	}
}

// poseSnapshot is one captured deformation, used to diff expressions against
// the rest pose.
type poseSnapshot struct {
	positions [][]core.Vec2
	opacities []float32
}

// capturePose poses the model at its defaults, lets mutate adjust the
// parameters, runs physics to equilibrium, updates and deep-copies the result.
func (b *baker) capturePose(mutate func()) poseSnapshot {
	rest := &b.sources[0]
	b.applyMotion(rest, 0)
	if mutate != nil {
		mutate()
	}
	if b.sim != nil {
		b.sim.Reset()
		for i := 0; i < int(settleSeconds*30); i++ {
			b.sim.Update(1.0/30, physicsParams{b})
		}
	}
	b.model.Update()
	snap := poseSnapshot{
		positions: make([][]core.Vec2, len(b.positions)),
		opacities: append([]float32(nil), b.opacities...),
	}
	for i, ps := range b.positions {
		snap.positions[i] = append([]core.Vec2(nil), ps...)
	}
	return snap
}

// bakeOverlays turns each expression into an additive pose delta against the
// rest pose.
func (b *baker) bakeOverlays(f *mofufmt.File) {
	refs := b.m3.FileReferences.Expressions
	if len(refs) == 0 {
		return
	}
	if b.opts.SkipExpressions {
		b.warn("%d expression(s) skipped (-expressions=false)", len(refs))
		return
	}
	base := b.capturePose(nil)
	seen := map[string]bool{}
	for _, ref := range refs {
		name := ref.Name
		if name == "" {
			name = strings.TrimSuffix(filepath.Base(ref.File), ".exp3.json")
		}
		if seen[name] {
			b.warn("duplicate expression name %q, skipped", name)
			continue
		}
		e, err := cubism.LoadExpression3(b.m3.Resolve(ref.File))
		if err != nil {
			b.warn("skipping expression %s: %v", name, err)
			continue
		}
		seen[name] = true
		snap := b.capturePose(func() { b.applyExpression(e) })

		ov := mofufmt.Overlay{Name: name, Tracks: make([]mofufmt.OverlayTrack, len(f.Meshes))}
		for mi := range f.Meshes {
			tr := &ov.Tracks[mi]
			moved := false
			deltas := make([]float32, 0, len(base.positions[mi])*2)
			for v := range base.positions[mi] {
				dx := snap.positions[mi][v].X - base.positions[mi][v].X
				dy := snap.positions[mi][v].Y - base.positions[mi][v].Y
				if dx != 0 || dy != 0 {
					moved = true
				}
				deltas = append(deltas, dx, dy)
			}
			if moved {
				tr.DeltaPositions = deltas
			}
			tr.DeltaOpacity = snap.opacities[mi] - base.opacities[mi]
		}
		f.Overlays = append(f.Overlays, ov)
		b.opts.logf("expression %-21s baked as an overlay", name)
	}
}

// applyExpression layers an expression's parameter adjustments over the
// current parameter values.
func (b *baker) applyExpression(e *cubism.Expression3) {
	for i := range e.Parameters {
		p := &e.Parameters[i]
		if idx, ok := b.paramIndex[p.Id]; ok {
			b.paramValues[idx] = float32(p.Apply(float64(b.paramValues[idx])))
		}
	}
}

// bind resolves the Core-owned slices and the id lookup tables.
func (b *baker) bind() {
	m := b.model
	b.paramValues = m.ParameterValues()
	b.paramDefaults = append([]float32(nil), m.ParameterDefaults()...)
	b.paramMins = append([]float32(nil), m.ParameterMinimums()...)
	b.paramMaxs = append([]float32(nil), m.ParameterMaximums()...)
	b.paramIndex = indexOf(m.ParameterIDs())
	b.partOpacities = m.PartOpacities()
	b.partIndex = indexOf(m.PartIDs())

	b.positions = m.VertexPositions()
	b.opacities = m.Opacities()
	b.orders = m.RenderOrders()
	b.dynFlags = m.DynamicFlags()
	b.multiplies = m.MultiplyColors()
	b.screens = m.ScreenColors()
	if b.multiplies == nil || b.screens == nil {
		b.warn("this Cubism Core does not expose multiply/screen colours; they are baked as identity")
	}
}

func indexOf(ids []string) map[string]int {
	m := make(map[string]int, len(ids))
	for i, id := range ids {
		m[id] = i
	}
	return m
}

// buildMeshes captures everything about a drawable that cannot change.
func (b *baker) buildMeshes() error {
	m := b.model
	n := m.DrawableCount()
	if n == 0 {
		return fmt.Errorf("bake: model has no drawables")
	}
	ids := m.DrawableIDs()
	flags := m.ConstantFlags()
	texIdx := m.TextureIndices()
	uvs := m.VertexUVs()
	indices := m.Indices()
	masks := m.Masks()

	b.meshes = make([]mofufmt.Mesh, n)
	for i := 0; i < n; i++ {
		mesh := &b.meshes[i]
		mesh.ID = ids[i]
		mesh.TextureIndex = texIdx[i]
		mesh.Flags = convertFlags(flags[i])
		if len(masks[i]) > 0 {
			mesh.Masks = append([]int32(nil), masks[i]...)
		}
		mesh.UVs = make([]float32, 0, len(uvs[i])*2)
		for _, uv := range uvs[i] {
			mesh.UVs = append(mesh.UVs, uv.X, uv.Y)
		}
		mesh.Indices = append([]uint16(nil), indices[i]...)
		mesh.MinX, mesh.MinY = float32(math.Inf(1)), float32(math.Inf(1))
		mesh.MaxX, mesh.MaxY = float32(math.Inf(-1)), float32(math.Inf(-1))
	}
	return nil
}

func convertFlags(f core.ConstantFlags) mofufmt.MeshFlags {
	var out mofufmt.MeshFlags
	if f.Has(core.BlendAdditive) {
		out |= mofufmt.MeshBlendAdditive
	}
	if f.Has(core.BlendMultiplicative) {
		out |= mofufmt.MeshBlendMultiplicative
	}
	if f.Has(core.DoubleSided) {
		out |= mofufmt.MeshDoubleSided
	}
	if f.Has(core.InvertedMask) {
		out |= mofufmt.MeshInvertedMask
	}
	return out
}

// collectSources gathers the rest pose plus every motion to bake.
func (b *baker) collectSources() {
	b.sources = append(b.sources, source{name: RestAnimation, fps: 30, frames: 1})

	type job struct {
		name string
		path string
		ref  cubism.MotionRef
	}
	var jobs []job
	for _, group := range b.m3.MotionGroups() {
		refs := b.m3.FileReferences.Motions[group]
		for i, ref := range refs {
			name := group
			if len(refs) > 1 {
				name = fmt.Sprintf("%s.%d", group, i)
			}
			jobs = append(jobs, job{name: name, path: b.m3.Resolve(ref.File), ref: ref})
		}
	}
	for _, p := range b.opts.Motions {
		name := strings.TrimSuffix(filepath.Base(p), ".motion3.json")
		jobs = append(jobs, job{name: name, path: p})
	}

	seen := map[string]bool{RestAnimation: true}
	for _, j := range jobs {
		if seen[j.name] {
			b.warn("duplicate animation name %q, skipped", j.name)
			continue
		}
		mo, err := cubism.LoadMotion3(j.path)
		if err != nil {
			b.warn("skipping %s: %v", j.name, err)
			continue
		}
		seen[j.name] = true
		fps := b.opts.FPS
		if fps <= 0 {
			fps = mo.Meta.Fps
		}
		frames := int(math.Round(mo.Meta.Duration * fps))
		// A looping motion is sampled over [0, duration): its last frame
		// blends back into its first, so sampling the endpoint would
		// duplicate frame 0. A one-shot motion needs that endpoint, or its
		// final pose is never reached.
		if !mo.Meta.Loop {
			frames++
		}
		if frames < 1 {
			frames = 1
		}
		b.sources = append(b.sources, source{
			name:    j.name,
			motion:  mo,
			sound:   j.ref.Sound,
			loop:    mo.Meta.Loop,
			fadeIn:  float32(j.ref.FadeInTime),
			fadeOut: float32(j.ref.FadeOutTime),
			fps:     fps,
			frames:  frames,
		})
		b.opts.logf("motion %-24s %6.2fs  %d frames @ %gfps", j.name, mo.Meta.Duration, frames, fps)
	}
}

// applyMotion sets every parameter and part opacity to the state of src at
// frame f, without running physics or updating the model.
func (b *baker) applyMotion(src *source, f int) {
	copy(b.paramValues, b.paramDefaults)
	for i := range b.partOpacities {
		b.partOpacities[i] = 1
	}
	if src.motion != nil {
		t := float64(f) / src.fps
		restricted := src.motion.Meta.AreBeziersRestricted
		for i := range src.motion.Curves {
			c := &src.motion.Curves[i]
			v := float32(c.Evaluate(t, restricted))
			switch c.Target {
			case cubism.TargetParameter:
				if idx, ok := b.paramIndex[c.Id]; ok {
					b.paramValues[idx] = v
				}
			case cubism.TargetPartOpacity:
				if idx, ok := b.partIndex[c.Id]; ok {
					b.partOpacities[idx] = v
				}
			}
		}
	}
}

// pose drives the model to the state of src at frame f, runs physics if the
// model has it, and updates the Core. Frames must be visited in order from 0;
// both baking passes do, which is what keeps the physics identical between
// them.
func (b *baker) pose(src *source, f int) {
	b.applyMotion(src, f)
	if b.sim != nil {
		dt := 1 / src.fps
		if f == 0 {
			b.sim.Reset()
			// Settle into equilibrium under the first frame's parameters.
			for i := 0; i < int(settleSeconds*src.fps); i++ {
				b.sim.Update(dt, physicsParams{b})
			}
			// For a looping motion, additionally run one unrecorded loop so
			// the recorded first frame already carries the state the last
			// frame hands back to it. The physics is not periodic, so the
			// seam is only approximate, but this keeps it small.
			if src.loop && src.frames > 1 {
				for g := 0; g < src.frames; g++ {
					b.applyMotion(src, g)
					b.sim.Update(dt, physicsParams{b})
				}
				b.applyMotion(src, 0)
			}
		}
		b.sim.Update(dt, physicsParams{b})
	}
	b.model.Update()
}

// measure is the first pass: it only grows each mesh's bounding box, so that
// the second pass can quantise positions without buffering every frame.
func (b *baker) measure() {
	for i := range b.sources {
		src := &b.sources[i]
		for f := 0; f < src.frames; f++ {
			b.pose(src, f)
			for d := range b.meshes {
				mesh := &b.meshes[d]
				for _, p := range b.positions[d] {
					if p.X < mesh.MinX {
						mesh.MinX = p.X
					}
					if p.X > mesh.MaxX {
						mesh.MaxX = p.X
					}
					if p.Y < mesh.MinY {
						mesh.MinY = p.Y
					}
					if p.Y > mesh.MaxY {
						mesh.MaxY = p.Y
					}
				}
			}
		}
	}
	for i := range b.meshes {
		mesh := &b.meshes[i]
		if math.IsInf(float64(mesh.MinX), 1) { // no vertices at all
			mesh.MinX, mesh.MinY, mesh.MaxX, mesh.MaxY = 0, 0, 0, 0
		}
	}
}

// bakeAnimation is the second pass for one animation.
func (b *baker) bakeAnimation(src *source) *mofufmt.Animation {
	a := &mofufmt.Animation{
		Name:       src.name,
		Sound:      filepath.ToSlash(src.sound),
		FPS:        float32(src.fps),
		FrameCount: int32(src.frames),
		Loop:       src.loop,
		FadeIn:     src.fadeIn,
		FadeOut:    src.fadeOut,
		Tracks:     make([]mofufmt.Track, len(b.meshes)),
	}
	if src.motion != nil {
		for _, u := range src.motion.UserData {
			a.Events = append(a.Events, mofufmt.Event{Time: float32(u.Time), Value: u.Value})
		}
		sort.SliceStable(a.Events, func(i, j int) bool { return a.Events[i].Time < a.Events[j].Time })
	}

	pos := make([]channel[uint16], len(b.meshes))
	opa := make([]channel[float32], len(b.meshes))
	ord := make([]channel[int32], len(b.meshes))
	vis := make([]channel[uint8], len(b.meshes))
	col := make([]channel[float32], len(b.meshes))
	for d := range b.meshes {
		pos[d].stride = b.meshes[d].VertexCount() * 2
		opa[d].stride = 1
		ord[d].stride = 1
		vis[d].stride = 1
		col[d].stride = 8
	}

	scratch := make([]uint16, maxStride(pos))
	var one [8]float32

	for f := 0; f < src.frames; f++ {
		b.pose(src, f)
		for d := range b.meshes {
			mesh := &b.meshes[d]
			q := scratch[:pos[d].stride]
			for v, p := range b.positions[d] {
				q[v*2] = mesh.QuantX(p.X)
				q[v*2+1] = mesh.QuantY(p.Y)
			}
			pos[d].push(q)

			one[0] = roundChannel(b.opacities[d])
			opa[d].push(one[:1])

			ord[d].pushOne(b.orders[d])

			var visible uint8
			if b.dynFlags[d].Has(core.IsVisible) {
				visible = 1
			}
			vis[d].pushOne(visible)

			mul := core.Vec4{X: 1, Y: 1, Z: 1, W: 1}
			scr := core.Vec4{W: 1}
			if b.multiplies != nil {
				mul = b.multiplies[d]
			}
			if b.screens != nil {
				scr = b.screens[d]
			}
			one = [8]float32{
				roundChannel(mul.X), roundChannel(mul.Y), roundChannel(mul.Z), roundChannel(mul.W),
				roundChannel(scr.X), roundChannel(scr.Y), roundChannel(scr.Z), roundChannel(scr.W),
			}
			col[d].push(one[:])
		}
	}

	for d := range b.meshes {
		t := &a.Tracks[d]
		t.Positions = pos[d].data
		t.Opacity = opa[d].data
		t.Order = ord[d].data
		t.Visible = vis[d].data
		if pos[d].animated {
			t.Flags |= mofufmt.TrackPositionsAnimated
		}
		if opa[d].animated {
			t.Flags |= mofufmt.TrackOpacityAnimated
		}
		if ord[d].animated {
			t.Flags |= mofufmt.TrackOrderAnimated
		}
		if vis[d].animated {
			t.Flags |= mofufmt.TrackVisibilityAnimated
		}
		// Identity colours are the overwhelmingly common case; drop them
		// rather than spending 32 bytes per mesh per animation on them.
		if col[d].animated || !isIdentityColor(col[d].data) {
			t.Colors = col[d].data
			t.Flags |= mofufmt.TrackHasColors
			if col[d].animated {
				t.Flags |= mofufmt.TrackColorsAnimated
			}
		}
	}
	return a
}

// roundChannel snaps a scalar channel sample to 1/4096 steps. The step is far
// below anything visible, and it keeps channels foldable: without it, the
// asymptotic tail of a settling physics chain leaves every frame differing in
// the last few bits and defeats the constant-channel optimisation.
func roundChannel(v float32) float32 {
	return float32(math.Round(float64(v)*4096) / 4096)
}

func isIdentityColor(c []float32) bool {
	if len(c) < 8 {
		return true
	}
	want := [8]float32{1, 1, 1, 1, 0, 0, 0, 1}
	for i := 0; i < 8; i++ {
		if c[i] != want[i] {
			return false
		}
	}
	return true
}

func maxStride(cs []channel[uint16]) int {
	n := 0
	for i := range cs {
		if cs[i].stride > n {
			n = cs[i].stride
		}
	}
	return n
}

// channel accumulates one per-frame value stream, collapsing it to a single
// sample for as long as it stays constant. The expansion only happens on the
// frame where the value first moves, so a mesh that is still for a whole
// animation costs one frame of storage.
type channel[T comparable] struct {
	stride   int
	data     []T
	frames   int
	animated bool
}

func (c *channel[T]) push(v []T) {
	if c.frames == 0 {
		c.data = append(c.data, v...)
		c.frames = 1
		return
	}
	if !c.animated {
		if equal(c.data[:c.stride], v) {
			c.frames++
			return
		}
		first := append(make([]T, 0, c.stride), c.data[:c.stride]...)
		for i := 1; i < c.frames; i++ {
			c.data = append(c.data, first...)
		}
		c.animated = true
	}
	c.data = append(c.data, v...)
	c.frames++
}

func (c *channel[T]) pushOne(v T) { c.push([]T{v}) }

func equal[T comparable](a, b []T) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// loadTextures copies the texture files in verbatim.
func (b *baker) loadTextures() ([]mofufmt.Texture, error) {
	var out []mofufmt.Texture
	for _, rel := range b.m3.FileReferences.Textures {
		p := b.m3.Resolve(rel)
		data, err := os.ReadFile(p)
		if err != nil {
			return nil, err
		}
		out = append(out, mofufmt.Texture{Name: filepath.ToSlash(rel), Data: data})
	}
	if len(out) == 0 {
		b.warn("model3.json lists no textures")
	}
	return out, nil
}

// noteUnbakeables reports the parts of a Cubism export that a baked format
// cannot represent, so the user is not left wondering where they went.
func (b *baker) noteUnbakeables() {
	if fr := &b.m3.FileReferences; fr.Pose != "" {
		b.warn("pose (%s) resolves part visibility at run time and is not baked", fr.Pose)
	}
}
