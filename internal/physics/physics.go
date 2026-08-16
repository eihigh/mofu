// Package physics simulates Cubism physics3.json settings offline.
//
// Cubism physics is a set of pendulum chains: input parameters tilt each
// chain's frame of reference, the pendulum swings after it, and the pendulum's
// segment angles are written back to output parameters. Nothing in it is
// random, so given the same parameter inputs it always produces the same
// outputs -- which is what lets a baking tool run it ahead of time with a
// motion's parameters as the only input.
//
// The maths follows the publicly documented physics3.json semantics: a
// verlet-style particle chain with per-particle mobility, delay, acceleration
// and radius, inputs normalised through the setting's piecewise-linear
// normalisation, and outputs taken from the angle or translation of a chain
// segment. It is an independent implementation, close enough to the official
// runtime for baked output, but not bit-identical to it.
package physics

import (
	"encoding/json"
	"fmt"
	"math"
)

// Params is the parameter store the simulation reads inputs from and writes
// outputs to.
type Params interface {
	// Get returns the current value of a parameter, or ok=false if the model
	// does not have it.
	Get(id string) (value float32, ok bool)
	// Set overwrites a parameter's value.
	Set(id string, value float32)
	// Range returns a parameter's bounds and default.
	Range(id string) (min, max, def float32, ok bool)
}

// vec2 is a 2D vector in physics space (Y up, like the Cubism canvas).
type vec2 struct{ x, y float64 }

func (a vec2) add(b vec2) vec2      { return vec2{a.x + b.x, a.y + b.y} }
func (a vec2) sub(b vec2) vec2      { return vec2{a.x - b.x, a.y - b.y} }
func (a vec2) scale(s float64) vec2 { return vec2{a.x * s, a.y * s} }
func (a vec2) length() float64      { return math.Hypot(a.x, a.y) }

func (a vec2) normalized() vec2 {
	l := a.length()
	if l == 0 {
		return vec2{0, -1}
	}
	return a.scale(1 / l)
}

func (a vec2) rotated(radian float64) vec2 {
	sin, cos := math.Sincos(radian)
	return vec2{a.x*cos - a.y*sin, a.x*sin + a.y*cos}
}

// signedAngle is the angle that rotates from onto the direction of to.
func signedAngle(from, to vec2) float64 {
	return math.Atan2(from.x*to.y-from.y*to.x, from.x*to.x+from.y*to.y)
}

// The JSON shape of a physics3.json file.
type file struct {
	Version int `json:"Version"`
	Meta    struct {
		PhysicsSettingCount int     `json:"PhysicsSettingCount"`
		Fps                 float64 `json:"Fps"`
		EffectiveForces     struct {
			Gravity jsonVec `json:"Gravity"`
			Wind    jsonVec `json:"Wind"`
		} `json:"EffectiveForces"`
	} `json:"Meta"`
	PhysicsSettings []jsonSetting `json:"PhysicsSettings"`
}

type jsonVec struct{ X, Y float64 }

type jsonSetting struct {
	Id    string `json:"Id"`
	Input []struct {
		Source  struct{ Target, Id string } `json:"Source"`
		Weight  float64                     `json:"Weight"`
		Type    string                      `json:"Type"`
		Reflect bool                        `json:"Reflect"`
	} `json:"Input"`
	Output []struct {
		Destination struct{ Target, Id string } `json:"Destination"`
		VertexIndex int                         `json:"VertexIndex"`
		Scale       float64                     `json:"Scale"`
		Weight      float64                     `json:"Weight"`
		Type        string                      `json:"Type"`
		Reflect     bool                        `json:"Reflect"`
	} `json:"Output"`
	Vertices []struct {
		Position     jsonVec `json:"Position"`
		Mobility     float64 `json:"Mobility"`
		Delay        float64 `json:"Delay"`
		Acceleration float64 `json:"Acceleration"`
		Radius       float64 `json:"Radius"`
	} `json:"Vertices"`
	Normalization struct {
		Position norm `json:"Position"`
		Angle    norm `json:"Angle"`
	} `json:"Normalization"`
}

type norm struct {
	Minimum float64 `json:"Minimum"`
	Default float64 `json:"Default"`
	Maximum float64 `json:"Maximum"`
}

// Simulator runs every setting of one physics3.json.
type Simulator struct {
	gravity  vec2 // normalised
	wind     vec2
	settings []setting
}

type setting struct {
	inputs    []input
	outputs   []output
	particles []particle
	// movementThreshold snaps tiny X displacement to zero so a settled
	// pendulum actually comes to rest instead of drifting in the last bits
	// of a float.
	movementThreshold  float64
	normPos, normAngle norm
}

type input struct {
	id      string
	weight  float64 // already divided by 100
	typ     string  // "X", "Y" or "Angle"
	reflect bool
}

type output struct {
	id          string
	vertexIndex int
	scale       float64
	weight      float64 // already divided by 100
	typ         string
	reflect     bool
}

type particle struct {
	pos, lastPos, velocity, force vec2
	lastGravity                   vec2
	mobility, delay, acceleration float64
	radius                        float64
}

// airResistance matches the constant the official runtime divides frame
// rotation by.
const airResistance = 5.0

// New parses physics3.json bytes into a ready simulator.
func New(data []byte) (*Simulator, error) {
	var f file
	if err := json.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("physics: %w", err)
	}
	if len(f.PhysicsSettings) == 0 {
		return nil, fmt.Errorf("physics: no settings")
	}
	s := &Simulator{
		gravity: vec2{f.Meta.EffectiveForces.Gravity.X, f.Meta.EffectiveForces.Gravity.Y}.normalized(),
		wind:    vec2{f.Meta.EffectiveForces.Wind.X, f.Meta.EffectiveForces.Wind.Y},
	}
	for _, js := range f.PhysicsSettings {
		if len(js.Vertices) < 2 {
			continue // a chain needs an anchor and at least one moving particle
		}
		st := setting{
			normPos:           js.Normalization.Position,
			normAngle:         js.Normalization.Angle,
			movementThreshold: math.Abs(js.Normalization.Position.Maximum) * 0.001,
		}
		for _, in := range js.Input {
			if in.Source.Target != "" && in.Source.Target != "Parameter" {
				continue
			}
			st.inputs = append(st.inputs, input{
				id: in.Source.Id, weight: in.Weight / 100,
				typ: in.Type, reflect: in.Reflect,
			})
		}
		for _, out := range js.Output {
			if out.Destination.Target != "" && out.Destination.Target != "Parameter" {
				continue
			}
			st.outputs = append(st.outputs, output{
				id: out.Destination.Id, vertexIndex: out.VertexIndex,
				scale: out.Scale, weight: out.Weight / 100,
				typ: out.Type, reflect: out.Reflect,
			})
		}
		for _, v := range js.Vertices {
			st.particles = append(st.particles, particle{
				mobility: v.Mobility, delay: v.Delay,
				acceleration: v.Acceleration, radius: v.Radius,
			})
		}
		s.settings = append(s.settings, st)
	}
	if len(s.settings) == 0 {
		return nil, fmt.Errorf("physics: no usable settings")
	}
	s.Reset()
	return s, nil
}

// SettingCount is the number of active pendulum chains.
func (s *Simulator) SettingCount() int { return len(s.settings) }

// Reset hangs every chain straight down its gravity and zeroes all motion.
func (s *Simulator) Reset() {
	for i := range s.settings {
		st := &s.settings[i]
		p := st.particles
		p[0].pos = vec2{}
		p[0].lastPos = vec2{}
		p[0].lastGravity = s.gravity
		for j := 1; j < len(p); j++ {
			p[j].pos = p[j-1].pos.add(s.gravity.scale(p[j].radius))
			p[j].lastPos = p[j].pos
			p[j].velocity = vec2{}
			p[j].force = vec2{}
			p[j].lastGravity = s.gravity
		}
	}
}

// Update advances every chain by dt seconds, reading inputs from and writing
// outputs to params. Settings run in order, so one setting's output can feed
// the next setting's input, as in the official runtime.
func (s *Simulator) Update(dt float64, params Params) {
	if dt <= 0 {
		return
	}
	for i := range s.settings {
		st := &s.settings[i]
		totalAngle, totalX, totalY := st.readInputs(params)
		s.updateParticles(st, vec2{totalX, totalY}, totalAngle, dt)
		st.writeOutputs(params, s.gravity)
	}
}

// readInputs folds the input parameters into a frame tilt (degrees) and a
// root translation.
func (st *setting) readInputs(params Params) (angle, x, y float64) {
	for _, in := range st.inputs {
		v, ok := params.Get(in.id)
		if !ok {
			continue
		}
		pmin, pmax, pdef, ok := params.Range(in.id)
		if !ok {
			continue
		}
		var n float64
		switch in.typ {
		case "X", "Y":
			n = normalize(float64(v), float64(pmin), float64(pmax), float64(pdef), st.normPos)
		default: // Angle
			n = normalize(float64(v), float64(pmin), float64(pmax), float64(pdef), st.normAngle)
		}
		if in.reflect {
			n = -n
		}
		switch in.typ {
		case "X":
			x += n * in.weight
		case "Y":
			y += n * in.weight
		default:
			angle += n * in.weight
		}
	}
	return angle, x, y
}

// normalize maps a parameter value through the piecewise-linear
// [min, default, max] -> [nmin, ndefault, nmax] mapping physics settings use.
func normalize(v, pmin, pmax, pdef float64, n norm) float64 {
	lo, hi := math.Min(pmin, pmax), math.Max(pmin, pmax)
	v = math.Min(math.Max(v, lo), hi)
	nlo, nhi := math.Min(n.Minimum, n.Maximum), math.Max(n.Minimum, n.Maximum)
	if v < pdef {
		span := pdef - lo
		if span == 0 {
			return n.Default
		}
		return n.Default + (v-pdef)*(n.Default-nlo)/span
	}
	span := hi - pdef
	if span == 0 {
		return n.Default
	}
	return n.Default + (v-pdef)*(nhi-n.Default)/span
}

// updateParticles steps the pendulum chain.
func (s *Simulator) updateParticles(st *setting, translation vec2, angleDeg, dt float64) {
	p := st.particles
	p[0].pos = translation

	// The input angle tilts the whole frame: gravity swings with it.
	gravity := s.gravity.rotated(angleDeg * math.Pi / 180).normalized()
	delayScale := dt * 30

	for i := 1; i < len(p); i++ {
		pt := &p[i]
		pt.force = gravity.scale(pt.acceleration).add(s.wind)
		pt.lastPos = pt.pos
		delay := pt.delay * delayScale

		// Rotate the segment a fraction of the way toward the new gravity,
		// damped by air resistance.
		direction := pt.pos.sub(p[i-1].pos)
		radian := signedAngle(pt.lastGravity, gravity) / airResistance
		direction = direction.rotated(radian)
		pt.pos = p[i-1].pos.add(direction)

		// Integrate velocity and force, then pin the segment back to its
		// length.
		pt.pos = pt.pos.add(pt.velocity.scale(delay)).add(pt.force.scale(delay * delay))
		seg := pt.pos.sub(p[i-1].pos).normalized()
		pt.pos = p[i-1].pos.add(seg.scale(pt.radius))

		if math.Abs(pt.pos.x) < st.movementThreshold {
			pt.pos.x = 0
		}
		if delay != 0 {
			pt.velocity = pt.pos.sub(pt.lastPos).scale(pt.mobility / delay)
		}
		pt.lastGravity = gravity
	}
}

// writeOutputs converts chain segments back into parameter values.
func (st *setting) writeOutputs(params Params, baseGravity vec2) {
	p := st.particles
	for _, out := range st.outputs {
		i := out.vertexIndex
		if i < 1 || i >= len(p) {
			continue
		}
		translation := p[i].pos.sub(p[i-1].pos)
		var value float64
		switch out.typ {
		case "X":
			value = translation.x
		case "Y":
			value = translation.y
		default: // Angle: relative to the parent segment (or gravity at the root)
			parent := baseGravity
			if i >= 2 {
				parent = p[i-1].pos.sub(p[i-2].pos)
			}
			value = signedAngle(parent, translation)
		}
		if out.reflect {
			value = -value
		}
		value *= out.scale

		pmin, pmax, _, ok := params.Range(out.id)
		if !ok {
			continue
		}
		cur, _ := params.Get(out.id)
		w := math.Min(out.weight, 1)
		v := float64(cur)*(1-w) + value*w
		v = math.Min(math.Max(v, float64(pmin)), float64(pmax))
		params.Set(out.id, float32(v))
	}
}
