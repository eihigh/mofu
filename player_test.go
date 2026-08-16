package mofu

import (
	"testing"

	"github.com/eihigh/mofu/mofufmt"
)

// newTestPlayer builds a Player without touching the GPU, so the timing logic
// can be tested without a graphics context.
func newTestPlayer(a mofufmt.Animation) *Player {
	m := &Model{
		file:   &mofufmt.File{Animations: []mofufmt.Animation{a}},
		byName: map[string]int{a.Name: 0},
	}
	return &Player{model: m, anim: 0, speed: 1, loop: a.Loop}
}

func TestFramesLooping(t *testing.T) {
	p := newTestPlayer(mofufmt.Animation{Name: "a", FPS: 10, FrameCount: 4, Loop: true})

	for _, tc := range []struct {
		time   float64
		f0, f1 int
		t      float32
	}{
		{0, 0, 1, 0},
		{0.05, 0, 1, 0.5},
		{0.3, 3, 0, 0},    // the last frame blends back into the first
		{0.35, 3, 0, 0.5}, // ...halfway through the wrap
		{0.4, 0, 1, 0},    // one full cycle later
		{0.45, 0, 1, 0.5},
	} {
		p.SetTime(tc.time)
		f0, f1, tt := p.frames(p.Animation())
		if f0 != tc.f0 || f1 != tc.f1 || !closeTo(tt, tc.t) {
			t.Errorf("at t=%v: frames = (%d, %d, %v), want (%d, %d, %v)", tc.time, f0, f1, tt, tc.f0, tc.f1, tc.t)
		}
	}
}

func TestFramesClampedWhenNotLooping(t *testing.T) {
	p := newTestPlayer(mofufmt.Animation{Name: "a", FPS: 10, FrameCount: 4})

	p.SetTime(0.25)
	f0, f1, tt := p.frames(p.Animation())
	if f0 != 2 || f1 != 3 || !closeTo(tt, 0.5) {
		t.Errorf("mid animation: (%d, %d, %v), want (2, 3, 0.5)", f0, f1, tt)
	}

	p.SetTime(100)
	f0, f1, tt = p.frames(p.Animation())
	if f0 != 3 || f1 != 3 || tt != 0 {
		t.Errorf("past the end: (%d, %d, %v), want (3, 3, 0)", f0, f1, tt)
	}

	p.SetTime(-5)
	f0, f1, tt = p.frames(p.Animation())
	if f0 != 0 || f1 != 1 || tt != 0 {
		t.Errorf("before the start: (%d, %d, %v), want (0, 1, 0)", f0, f1, tt)
	}
}

func TestFramesSingleFrame(t *testing.T) {
	p := newTestPlayer(mofufmt.Animation{Name: "@rest", FPS: 30, FrameCount: 1})
	p.SetTime(12.5)
	f0, f1, tt := p.frames(p.Animation())
	if f0 != 0 || f1 != 0 || tt != 0 {
		t.Errorf("single frame animation: (%d, %d, %v), want (0, 0, 0)", f0, f1, tt)
	}
}

func TestAdvanceAndFinished(t *testing.T) {
	p := newTestPlayer(mofufmt.Animation{Name: "a", FPS: 10, FrameCount: 5})
	p.Advance(0.25)
	if p.Time() != 0.25 {
		t.Errorf("Time = %v, want 0.25", p.Time())
	}
	if p.Finished() {
		t.Error("Finished before the end")
	}
	p.Advance(0.3)
	if !p.Finished() {
		t.Error("not Finished past the end")
	}

	p.SetPaused(true)
	before := p.Time()
	p.Advance(1)
	if p.Time() != before {
		t.Error("Advance moved a paused player")
	}

	p.SetPaused(false)
	p.SetSpeed(2)
	p.SetTime(0)
	p.Advance(0.1)
	if !closeTo(float32(p.Time()), 0.2) {
		t.Errorf("Time with speed 2 = %v, want 0.2", p.Time())
	}

	// A looping player is never finished.
	l := newTestPlayer(mofufmt.Animation{Name: "b", FPS: 10, FrameCount: 5, Loop: true})
	l.SetTime(1000)
	if l.Finished() {
		t.Error("a looping player reported Finished")
	}
}

func TestPlayUnknownAnimation(t *testing.T) {
	p := newTestPlayer(mofufmt.Animation{Name: "a", FPS: 10, FrameCount: 2})
	if err := p.Play("nope"); err == nil {
		t.Error("Play of an unknown animation succeeded")
	}
	if err := p.Play("a"); err != nil {
		t.Errorf("Play(a) = %v", err)
	}
}

func closeTo(a, b float32) bool {
	d := a - b
	return d < 1e-5 && d > -1e-5
}
