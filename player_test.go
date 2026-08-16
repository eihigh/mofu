package mofu

import (
	"testing"

	"github.com/eihigh/mofu/mofufmt"
)

// newTestPlayer builds a Player over f without touching the GPU, so playback
// logic can be tested without a graphics context.
func newTestPlayer(anims ...mofufmt.Animation) *Player {
	return newModelCommon(&mofufmt.File{Animations: anims}).NewPlayer()
}

func TestFramesLooping(t *testing.T) {
	a := &mofufmt.Animation{Name: "a", FPS: 10, FrameCount: 4, Loop: true}

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
		f0, f1, tt := frameAt(a, tc.time, true)
		if f0 != tc.f0 || f1 != tc.f1 || !closeTo(tt, tc.t) {
			t.Errorf("at t=%v: frames = (%d, %d, %v), want (%d, %d, %v)", tc.time, f0, f1, tt, tc.f0, tc.f1, tc.t)
		}
	}
}

func TestFramesClampedWhenNotLooping(t *testing.T) {
	a := &mofufmt.Animation{Name: "a", FPS: 10, FrameCount: 4}

	f0, f1, tt := frameAt(a, 0.25, false)
	if f0 != 2 || f1 != 3 || !closeTo(tt, 0.5) {
		t.Errorf("mid animation: (%d, %d, %v), want (2, 3, 0.5)", f0, f1, tt)
	}
	f0, f1, tt = frameAt(a, 100, false)
	if f0 != 3 || f1 != 3 || tt != 0 {
		t.Errorf("past the end: (%d, %d, %v), want (3, 3, 0)", f0, f1, tt)
	}
	f0, f1, tt = frameAt(a, -5, false)
	if f0 != 0 || f1 != 1 || tt != 0 {
		t.Errorf("before the start: (%d, %d, %v), want (0, 1, 0)", f0, f1, tt)
	}
}

func TestFramesSingleFrame(t *testing.T) {
	a := &mofufmt.Animation{Name: "@rest", FPS: 30, FrameCount: 1}
	f0, f1, tt := frameAt(a, 12.5, false)
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

func TestCrossFadeState(t *testing.T) {
	p := newTestPlayer(
		mofufmt.Animation{Name: "a", FPS: 10, FrameCount: 10, Loop: true},
		mofufmt.Animation{Name: "b", FPS: 10, FrameCount: 10, Loop: true, FadeIn: 0.5},
	)
	p.Advance(0.3)

	// Play("b") starts a fade using b's baked FadeInTime.
	if err := p.Play("b"); err != nil {
		t.Fatal(err)
	}
	if p.prev == nil || p.fadeDur != 0.5 {
		t.Fatalf("fade not started: prev=%v dur=%v", p.prev, p.fadeDur)
	}
	if p.Animation().Name != "b" || p.Time() != 0 {
		t.Errorf("current after Play = %s at %v, want b at 0", p.Animation().Name, p.Time())
	}

	// Both heads advance during the fade.
	p.Advance(0.2)
	if !closeTo(float32(p.prev.time), 0.5) {
		t.Errorf("previous head time = %v, want 0.5", p.prev.time)
	}
	if !closeTo(float32(p.Time()), 0.2) {
		t.Errorf("current head time = %v, want 0.2", p.Time())
	}

	// The fade ends after its duration.
	p.Advance(0.4)
	if p.prev != nil {
		t.Error("previous head survived past the fade")
	}

	// An animation without FadeIn switches instantly.
	if err := p.Play("a"); err != nil {
		t.Fatal(err)
	}
	if p.prev != nil {
		t.Error("Play without FadeIn started a fade")
	}

	// PlayWithFade overrides the baked time.
	if err := p.PlayWithFade("b", 2); err != nil {
		t.Fatal(err)
	}
	if p.prev == nil || p.fadeDur != 2 {
		t.Errorf("PlayWithFade: prev=%v dur=%v, want a 2s fade", p.prev, p.fadeDur)
	}
}

func TestPollEvents(t *testing.T) {
	p := newTestPlayer(mofufmt.Animation{
		Name: "a", FPS: 10, FrameCount: 11, // 1 second, one-shot
		Events: []mofufmt.Event{{Time: 0, Value: "start"}, {Time: 0.5, Value: "mid"}, {Time: 1.0, Value: "end"}},
	})

	p.Advance(0.25)
	evs := p.PollEvents()
	if len(evs) != 1 || evs[0].Value != "start" {
		t.Fatalf("events after 0.25s = %+v, want [start]", evs)
	}
	// The queue clears once polled.
	if evs := p.PollEvents(); evs != nil {
		t.Errorf("second poll = %+v, want nil", evs)
	}

	p.Advance(0.5)
	evs = p.PollEvents()
	if len(evs) != 1 || evs[0].Value != "mid" {
		t.Fatalf("events after 0.75s = %+v, want [mid]", evs)
	}

	p.Advance(0.5)
	evs = p.PollEvents()
	if len(evs) != 1 || evs[0].Value != "end" {
		t.Fatalf("events at the end = %+v, want [end]", evs)
	}

	// Nothing more fires past the end of a one-shot.
	p.Advance(1)
	if evs := p.PollEvents(); evs != nil {
		t.Errorf("events past the end = %+v, want nil", evs)
	}
}

func TestPollEventsLoops(t *testing.T) {
	p := newTestPlayer(mofufmt.Animation{
		Name: "a", FPS: 10, FrameCount: 10, Loop: true, // 1 second loop
		Events: []mofufmt.Event{{Time: 0.5, Value: "tick"}},
	})
	// Crossing the event three times over three loops fires it three times.
	total := 0
	for i := 0; i < 30; i++ {
		p.Advance(0.1)
		total += len(p.PollEvents())
	}
	if total != 3 {
		t.Errorf("fired %d times over 3 loops, want 3", total)
	}

	// A big step over several loops still fires a bounded number of times.
	p.SetTime(0)
	p.Advance(5)
	if n := len(p.PollEvents()); n != 5 {
		t.Errorf("a 5-loop step fired %d times, want 5", n)
	}

	// Seeking does not fire events.
	p.SetTime(10)
	if evs := p.PollEvents(); evs != nil {
		t.Errorf("SetTime fired %+v", evs)
	}
}

func closeTo(a, b float32) bool {
	d := a - b
	return d < 1e-5 && d > -1e-5
}
