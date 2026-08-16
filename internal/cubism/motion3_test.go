package cubism

import (
	"math"
	"testing"
)

// curveJSON wraps a Segments array in the smallest valid motion3.json.
func parseCurve(t *testing.T, segments string) *Curve {
	t.Helper()
	src := `{"Version":3,"Meta":{"Duration":10,"Fps":30,"Loop":true,"AreBeziersRestricted":true},
	         "Curves":[{"Target":"Parameter","Id":"P","Segments":[` + segments + `]}]}`
	m, err := ParseMotion3([]byte(src), "test")
	if err != nil {
		t.Fatalf("ParseMotion3: %v", err)
	}
	return &m.Curves[0]
}

func near(t *testing.T, got, want float64, what string) {
	t.Helper()
	if math.Abs(got-want) > 1e-4 {
		t.Errorf("%s = %v, want %v", what, got, want)
	}
}

func TestLinearSegment(t *testing.T) {
	c := parseCurve(t, "0,0, 0, 2,10")
	near(t, c.Evaluate(-1, true), 0, "before the start")
	near(t, c.Evaluate(0, true), 0, "at the start")
	near(t, c.Evaluate(1, true), 5, "midway")
	near(t, c.Evaluate(2, true), 10, "at the end")
	near(t, c.Evaluate(99, true), 10, "past the end")
	near(t, c.Duration(), 2, "Duration")
}

func TestSteppedSegments(t *testing.T) {
	stepped := parseCurve(t, "0,0, 2, 2,10")
	near(t, stepped.Evaluate(1.999, true), 0, "stepped holds the left value")
	near(t, stepped.Evaluate(2, true), 10, "stepped at the end")

	inverse := parseCurve(t, "0,0, 3, 2,10")
	near(t, inverse.Evaluate(0.001, true), 10, "inverse stepped takes the right value")
	near(t, inverse.Evaluate(2, true), 10, "inverse stepped at the end")
}

func TestBezierSegment(t *testing.T) {
	// Control points on a straight line: the curve must be the line.
	c := parseCurve(t, "0,0, 1, 1,1, 2,2, 3,3")
	for _, tt := range []float64{0, 0.5, 1, 1.5, 2.25, 3} {
		near(t, c.Evaluate(tt, true), tt, "linear-shaped bezier")
	}
	// The unrestricted path solves for the parameter instead of assuming it.
	for _, tt := range []float64{0.5, 1.5, 2.25} {
		near(t, c.Evaluate(tt, false), tt, "unrestricted bezier")
	}
}

func TestBezierEaseIsMonotonic(t *testing.T) {
	// An ease-in-out from (0,0) to (1,1).
	c := parseCurve(t, "0,0, 1, 0.5,0, 0.5,1, 1,1")
	prev := math.Inf(-1)
	for i := 0; i <= 20; i++ {
		x := float64(i) / 20
		v := c.Evaluate(x, false)
		if v < prev-1e-6 {
			t.Fatalf("not monotonic at %v: %v after %v", x, v, prev)
		}
		if v < -1e-6 || v > 1+1e-6 {
			t.Fatalf("out of range at %v: %v", x, v)
		}
		prev = v
	}
	near(t, c.Evaluate(0.5, false), 0.5, "symmetric ease midpoint")
}

func TestMultipleSegments(t *testing.T) {
	// Linear up, hold, linear down.
	c := parseCurve(t, "0,0, 0, 1,10, 2, 2,10, 0, 3,0")
	near(t, c.Evaluate(0.5, true), 5, "first segment")
	near(t, c.Evaluate(1.5, true), 10, "stepped hold")
	near(t, c.Evaluate(2.5, true), 5, "last segment")
	if n := len(c.segs); n != 3 {
		t.Errorf("decoded %d segments, want 3", n)
	}
}

func TestMalformedSegments(t *testing.T) {
	for _, bad := range []string{
		`{"Version":3,"Curves":[{"Id":"P","Segments":[0]}]}`,         // no first point
		`{"Version":3,"Curves":[{"Id":"P","Segments":[0,0,9,1,1]}]}`, // unknown type
		`{"Version":3,"Curves":[{"Id":"P","Segments":[0,0,1,1,1]}]}`, // truncated bezier
	} {
		if _, err := ParseMotion3([]byte(bad), "test"); err == nil {
			t.Errorf("ParseMotion3(%s) succeeded, want an error", bad)
		}
	}
}

func TestEmptyCurveIsSafe(t *testing.T) {
	var c Curve
	if got := c.Evaluate(1, true); got != 0 {
		t.Errorf("Evaluate on an empty curve = %v, want 0", got)
	}
	if got := c.Duration(); got != 0 {
		t.Errorf("Duration on an empty curve = %v, want 0", got)
	}
}

func TestFpsDefault(t *testing.T) {
	m, err := ParseMotion3([]byte(`{"Version":3,"Meta":{"Duration":1},"Curves":[]}`), "test")
	if err != nil {
		t.Fatal(err)
	}
	if m.Meta.Fps != 30 {
		t.Errorf("Fps = %v, want the 30 fallback", m.Meta.Fps)
	}
}
