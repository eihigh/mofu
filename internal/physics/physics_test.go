package physics

import (
	"math"
	"testing"
)

// mapParams is an in-memory Params for tests.
type mapParams struct {
	values map[string]float32
	ranges map[string][3]float32 // min, max, def
}

func newMapParams() *mapParams {
	return &mapParams{values: map[string]float32{}, ranges: map[string][3]float32{}}
}

func (m *mapParams) define(id string, min, max, def float32) {
	m.ranges[id] = [3]float32{min, max, def}
	m.values[id] = def
}

func (m *mapParams) Get(id string) (float32, bool) { v, ok := m.values[id]; return v, ok }
func (m *mapParams) Set(id string, v float32)      { m.values[id] = v }
func (m *mapParams) Range(id string) (float32, float32, float32, bool) {
	r, ok := m.ranges[id]
	return r[0], r[1], r[2], ok
}

const pendulumJSON = `{
  "Version": 3,
  "Meta": {
    "PhysicsSettingCount": 1,
    "EffectiveForces": {"Gravity": {"X": 0, "Y": -1}, "Wind": {"X": 0, "Y": 0}}
  },
  "PhysicsSettings": [{
    "Id": "Setting1",
    "Input": [{"Source": {"Target": "Parameter", "Id": "In"}, "Weight": 100, "Type": "Angle", "Reflect": false}],
    "Output": [{"Destination": {"Target": "Parameter", "Id": "Out"}, "VertexIndex": 1, "Scale": 10, "Weight": 100, "Type": "Angle", "Reflect": false}],
    "Vertices": [
      {"Position": {"X": 0, "Y": 0}, "Mobility": 1, "Delay": 1, "Acceleration": 1, "Radius": 0},
      {"Position": {"X": 0, "Y": 3}, "Mobility": 0.95, "Delay": 0.8, "Acceleration": 1.5, "Radius": 3}
    ],
    "Normalization": {
      "Position": {"Minimum": -10, "Default": 0, "Maximum": 10},
      "Angle": {"Minimum": -30, "Default": 0, "Maximum": 30}
    }
  }]
}`

func pendulum(t *testing.T) (*Simulator, *mapParams) {
	t.Helper()
	s, err := New([]byte(pendulumJSON))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	p := newMapParams()
	p.define("In", -30, 30, 0)
	p.define("Out", -30, 30, 0)
	return s, p
}

func TestRestStaysAtRest(t *testing.T) {
	s, p := pendulum(t)
	for i := 0; i < 120; i++ {
		s.Update(1.0/30, p)
	}
	if out := p.values["Out"]; math.Abs(float64(out)) > 1e-6 {
		t.Errorf("output after 4s at rest = %v, want 0", out)
	}
}

func TestStepInputSettlesToTilt(t *testing.T) {
	s, p := pendulum(t)
	p.Set("In", 30) // full tilt: normalised to 30 degrees
	var out float32
	for i := 0; i < 600; i++ {
		s.Update(1.0/30, p)
		out = p.values["Out"]
		if math.IsNaN(float64(out)) || math.IsInf(float64(out), 0) {
			t.Fatalf("output went non-finite at step %d", i)
		}
	}
	// At equilibrium the pendulum hangs along the tilted gravity, 30 degrees
	// from the base gravity the output is measured against:
	// 30deg in radians * scale 10 = 5.236.
	want := 30 * math.Pi / 180 * 10
	if math.Abs(float64(out)-want) > 0.5 {
		t.Errorf("settled output = %v, want about %.3f", out, want)
	}
}

func TestOutputIsDelayed(t *testing.T) {
	s, p := pendulum(t)
	p.Set("In", 30)
	s.Update(1.0/30, p)
	first := float64(p.values["Out"])
	want := 30 * math.Pi / 180 * 10
	// After a single frame the pendulum must not have reached equilibrium:
	// physics exists to lag behind the input.
	if math.Abs(first) >= want*0.9 {
		t.Errorf("output after one frame = %v, expected well short of %.3f", first, want)
	}
}

func TestOutputClampsToParameterRange(t *testing.T) {
	s, err := New([]byte(pendulumJSON))
	if err != nil {
		t.Fatal(err)
	}
	p := newMapParams()
	p.define("In", -30, 30, 0)
	p.define("Out", 0, 1, 0) // a tiny output range
	p.Set("In", 30)
	for i := 0; i < 300; i++ {
		s.Update(1.0/30, p)
		if out := p.values["Out"]; out < 0 || out > 1 {
			t.Fatalf("output %v escaped [0, 1]", out)
		}
	}
	if out := p.values["Out"]; out != 1 {
		t.Errorf("clamped output = %v, want pinned at 1", out)
	}
}

func TestDeterminism(t *testing.T) {
	run := func() []float32 {
		s, p := pendulum(t)
		var trace []float32
		for i := 0; i < 90; i++ {
			p.Set("In", float32(30*math.Sin(float64(i)/10)))
			s.Update(1.0/30, p)
			trace = append(trace, p.values["Out"])
		}
		return trace
	}
	a, b := run(), run()
	for i := range a {
		if a[i] != b[i] {
			t.Fatalf("diverged at step %d: %v vs %v", i, a[i], b[i])
		}
	}
}

func TestResetRestoresRest(t *testing.T) {
	s, p := pendulum(t)
	p.Set("In", 30)
	for i := 0; i < 60; i++ {
		s.Update(1.0/30, p)
	}
	s.Reset()
	p.Set("In", 0)
	s.Update(1.0/30, p)
	if out := p.values["Out"]; math.Abs(float64(out)) > 1e-6 {
		t.Errorf("output after Reset = %v, want 0", out)
	}
}

func TestNormalize(t *testing.T) {
	n := norm{Minimum: -10, Default: 0, Maximum: 10}
	for _, tc := range []struct{ v, want float64 }{
		{0, 0}, {30, 10}, {-30, -10}, {15, 5}, {-15, -5}, {100, 10}, {-100, -10},
	} {
		if got := normalize(tc.v, -30, 30, 0, n); math.Abs(got-tc.want) > 1e-9 {
			t.Errorf("normalize(%v) = %v, want %v", tc.v, got, tc.want)
		}
	}
	// An asymmetric default splits the mapping piecewise.
	if got := normalize(0.5, 0, 1, 0.5, norm{Minimum: -10, Default: 2, Maximum: 10}); got != 2 {
		t.Errorf("default maps to normalised default, got %v", got)
	}
}

func TestRejectsUselessFiles(t *testing.T) {
	for _, bad := range []string{
		`{}`,
		`{"PhysicsSettings":[]}`,
		`{"PhysicsSettings":[{"Vertices":[{"Radius":1}]}]}`, // one-particle chain
		`not json`,
	} {
		if _, err := New([]byte(bad)); err == nil {
			t.Errorf("New(%q) succeeded, want an error", bad)
		}
	}
}
