package cubism

import (
	"encoding/json"
	"fmt"
	"os"
)

// Curve target kinds used by motion3.json.
const (
	TargetModel       = "Model"
	TargetParameter   = "Parameter"
	TargetPartOpacity = "PartOpacity"
)

// SegmentType identifies how a motion segment interpolates.
type SegmentType int

// Segment types as encoded in motion3.json.
const (
	SegmentLinear         SegmentType = 0
	SegmentBezier         SegmentType = 1
	SegmentStepped        SegmentType = 2
	SegmentInverseStepped SegmentType = 3
)

// Motion3 is the contents of a .motion3.json file.
type Motion3 struct {
	Version  int            `json:"Version"`
	Meta     MotionMeta     `json:"Meta"`
	Curves   []Curve        `json:"Curves"`
	UserData []UserDataItem `json:"UserData"`

	// Name is the motion's display name, filled in by LoadMotion3 from the
	// file name.
	Name string `json:"-"`
}

// MotionMeta is the Meta block of a motion3.json.
type MotionMeta struct {
	Duration             float64 `json:"Duration"`
	Fps                  float64 `json:"Fps"`
	Loop                 bool    `json:"Loop"`
	AreBeziersRestricted bool    `json:"AreBeziersRestricted"`
	CurveCount           int     `json:"CurveCount"`
	TotalSegmentCount    int     `json:"TotalSegmentCount"`
	TotalPointCount      int     `json:"TotalPointCount"`
	UserDataCount        int     `json:"UserDataCount"`
	TotalUserDataSize    int     `json:"TotalUserDataSize"`
}

// UserDataItem is one timed user-data event.
type UserDataItem struct {
	Time  float64 `json:"Time"`
	Value string  `json:"Value"`
}

// Curve animates one parameter, one part opacity, or one model-level value.
type Curve struct {
	Target      string    `json:"Target"`
	Id          string    `json:"Id"`
	FadeInTime  float64   `json:"FadeInTime"`
	FadeOutTime float64   `json:"FadeOutTime"`
	Segments    []float64 `json:"Segments"`

	// points and segs are the decoded form of Segments, built by decode.
	points []point
	segs   []segment
}

type point struct{ t, v float64 }

type segment struct {
	kind SegmentType
	// base is the index into points of the segment's first point.
	base int
}

// LoadMotion3 reads, parses and decodes a .motion3.json file.
func LoadMotion3(path string) (*Motion3, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return ParseMotion3(b, path)
}

// ParseMotion3 parses motion3.json bytes. name is only used in error messages.
func ParseMotion3(b []byte, name string) (*Motion3, error) {
	var m Motion3
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	for i := range m.Curves {
		if err := m.Curves[i].decode(); err != nil {
			return nil, fmt.Errorf("%s: curve %q: %w", name, m.Curves[i].Id, err)
		}
	}
	if m.Meta.Fps <= 0 {
		m.Meta.Fps = 30
	}
	return &m, nil
}

// decode expands the flat Segments array into points and segments.
//
// The layout is: two floats for the very first point, then repeatedly a
// segment type followed by that type's control points (one point for linear,
// stepped and inverse-stepped, three for bezier).
func (c *Curve) decode() error {
	s := c.Segments
	if len(s) < 2 {
		return fmt.Errorf("need at least one point, got %d values", len(s))
	}
	c.points = append(c.points[:0], point{s[0], s[1]})
	i := 2
	for i < len(s) {
		kind := SegmentType(s[i])
		i++
		var n int
		switch kind {
		case SegmentLinear, SegmentStepped, SegmentInverseStepped:
			n = 1
		case SegmentBezier:
			n = 3
		default:
			return fmt.Errorf("unknown segment type %v", kind)
		}
		if i+n*2 > len(s) {
			return fmt.Errorf("truncated segment of type %v", kind)
		}
		c.segs = append(c.segs, segment{kind: kind, base: len(c.points) - 1})
		for k := 0; k < n; k++ {
			c.points = append(c.points, point{s[i], s[i+1]})
			i += 2
		}
	}
	return nil
}

// Duration is the time of the curve's last point.
func (c *Curve) Duration() float64 {
	if len(c.points) == 0 {
		return 0
	}
	return c.points[len(c.points)-1].t
}

// Evaluate samples the curve at time t (seconds), clamping outside its range.
// restricted mirrors Meta.AreBeziersRestricted: when set, a bezier's control
// points are known to be evenly spaced in time and the parameter can be taken
// directly from t, otherwise it is recovered by bisection.
func (c *Curve) Evaluate(t float64, restricted bool) float64 {
	if len(c.points) == 0 {
		return 0
	}
	if t <= c.points[0].t || len(c.segs) == 0 {
		return c.points[0].v
	}
	last := c.points[len(c.points)-1]
	if t >= last.t {
		return last.v
	}

	// Find the last segment starting at or before t.
	lo, hi := 0, len(c.segs)-1
	for lo < hi {
		mid := (lo + hi + 1) / 2
		if c.points[c.segs[mid].base].t <= t {
			lo = mid
		} else {
			hi = mid - 1
		}
	}
	seg := c.segs[lo]
	p0 := c.points[seg.base]

	switch seg.kind {
	case SegmentStepped:
		return p0.v
	case SegmentInverseStepped:
		return c.points[seg.base+1].v
	case SegmentLinear:
		p1 := c.points[seg.base+1]
		return lerp(p0.v, p1.v, normalize(t, p0.t, p1.t))
	case SegmentBezier:
		p1, p2, p3 := c.points[seg.base+1], c.points[seg.base+2], c.points[seg.base+3]
		var u float64
		if restricted {
			u = normalize(t, p0.t, p3.t)
		} else {
			u = solveBezierT(p0.t, p1.t, p2.t, p3.t, t)
		}
		return bezier(p0.v, p1.v, p2.v, p3.v, u)
	}
	return p0.v
}

func normalize(t, a, b float64) float64 {
	if b <= a {
		return 0
	}
	u := (t - a) / (b - a)
	switch {
	case u < 0:
		return 0
	case u > 1:
		return 1
	}
	return u
}

func lerp(a, b, u float64) float64 { return a + (b-a)*u }

func bezier(a, b, c, d, u float64) float64 {
	ab := lerp(a, b, u)
	bc := lerp(b, c, u)
	cd := lerp(c, d, u)
	return lerp(lerp(ab, bc, u), lerp(bc, cd, u), u)
}

// solveBezierT recovers the curve parameter whose x equals t. The Cubism
// editor guarantees x is monotonic along a segment, so bisection converges.
func solveBezierT(x0, x1, x2, x3, t float64) float64 {
	lo, hi := 0.0, 1.0
	for i := 0; i < 32; i++ {
		mid := (lo + hi) / 2
		if bezier(x0, x1, x2, x3, mid) < t {
			lo = mid
		} else {
			hi = mid
		}
	}
	return (lo + hi) / 2
}
