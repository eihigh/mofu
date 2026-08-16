package bake_test

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/eihigh/mofu/internal/bake"
	"github.com/eihigh/mofu/internal/fakecore"
	"github.com/eihigh/mofu/mofufmt"
)

const model3JSON = `{
  "Version": 3,
  "FileReferences": {
    "Moc": "test.moc3",
    "Textures": ["test.2048/texture_00.png"],
    "Physics": "test.physics3.json",
    "Expressions": [
      {"Name": "f01", "File": "exp/f01.exp3.json"},
      {"Name": "broken", "File": "exp/missing.exp3.json"}
    ],
    "Motions": {
      "Idle": [{"File": "motions/idle.motion3.json", "Sound": "sounds/idle.wav", "FadeInTime": 0.5, "FadeOutTime": 0.25}],
      "Tap": [
        {"File": "motions/tap0.motion3.json"},
        {"File": "motions/tap1.motion3.json"}
      ]
    }
  },
  "HitAreas": [
    {"Id": "body", "Name": "Body"},
    {"Id": "no_such_drawable", "Name": "Ghost"}
  ]
}`

// exp3JSON slides the model sideways by driving ParamAngleX to its maximum.
const exp3JSON = `{
  "Type": "Live2D Expression",
  "Parameters": [{"Id": "ParamAngleX", "Value": 30, "Blend": "Add"}]
}`

// idleMotion sweeps ParamAngleX across its range and blinks the eye shut
// halfway through, so both the position and the scalar channels move.
const idleMotion = `{
  "Version": 3,
  "Meta": {"Duration": 1.0, "Fps": 30.0, "Loop": true, "AreBeziersRestricted": true, "CurveCount": 2, "UserDataCount": 1},
  "Curves": [
    {"Target": "Parameter", "Id": "ParamAngleX", "Segments": [0, 0, 0, 1.0, 30]},
    {"Target": "Parameter", "Id": "ParamEyeLOpen", "Segments": [0, 1, 2, 0.5, 0, 2, 1.0, 1]}
  ],
  "UserData": [{"Time": 0.5, "Value": "blink"}]
}`

// stillMotion leaves everything at its default, so every channel should fold
// down to a single sample.
const stillMotion = `{
  "Version": 3,
  "Meta": {"Duration": 0.5, "Fps": 20.0, "Loop": false, "AreBeziersRestricted": true},
  "Curves": [{"Target": "PartOpacity", "Id": "PartBody", "Segments": [0, 1, 0, 0.5, 1]}]
}`

// rampMotion is a one-shot that only reaches its final value at the very end
// of its duration, which is what pins the endpoint-sampling behaviour.
const rampMotion = `{
  "Version": 3,
  "Meta": {"Duration": 1.0, "Fps": 10.0, "Loop": false, "AreBeziersRestricted": true},
  "Curves": [{"Target": "Parameter", "Id": "ParamEyeLOpen", "Segments": [0, 1, 0, 1.0, 0]}]
}`

func writeModel(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	write := func(rel, content string) {
		p := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("test.model3.json", model3JSON)
	write("test.moc3", "fake moc3 payload for the stub core")
	write("test.2048/texture_00.png", "not really a png, just bytes to copy")
	write("exp/f01.exp3.json", exp3JSON)
	write("motions/idle.motion3.json", idleMotion)
	write("motions/tap0.motion3.json", stillMotion)
	write("motions/tap1.motion3.json", rampMotion)
	return filepath.Join(dir, "test.model3.json")
}

func runBake(t *testing.T, opts bake.Options) *bake.Result {
	t.Helper()
	opts.CorePath = fakecore.Build(t)
	res, err := bake.Run(writeModel(t), opts)
	if err != nil {
		t.Fatalf("bake.Run: %v", err)
	}
	return res
}

func TestBakeProducesExpectedStructure(t *testing.T) {
	res := runBake(t, bake.Options{})
	f := res.File

	c := f.Canvas
	if c.Width != 512 || c.Height != 1024 || c.PixelsPerUnit != 256 {
		t.Errorf("canvas = %+v", c)
	}
	if len(f.Textures) != 1 || f.Textures[0].Name != "test.2048/texture_00.png" {
		t.Fatalf("textures = %+v", f.Textures)
	}
	if got := string(f.Textures[0].Data); got != "not really a png, just bytes to copy" {
		t.Errorf("texture bytes were not copied verbatim: %q", got)
	}

	if len(f.Meshes) != 2 {
		t.Fatalf("got %d meshes, want 2", len(f.Meshes))
	}
	body, eye := &f.Meshes[0], &f.Meshes[1]
	if body.ID != "body" || eye.ID != "eye" {
		t.Errorf("mesh ids = %q, %q", body.ID, eye.ID)
	}
	if !eye.Flags.Has(mofufmt.MeshBlendAdditive) {
		t.Error("the eye should carry the additive flag")
	}
	if len(eye.Masks) != 1 || eye.Masks[0] != 0 {
		t.Errorf("eye masks = %v, want [0]", eye.Masks)
	}
	if body.VertexCount() != 4 || len(body.Indices) != 6 {
		t.Errorf("body has %d vertices and %d indices", body.VertexCount(), len(body.Indices))
	}

	// The bounding box has to span every pose in every animation: the stub
	// slides x by up to 0.2 when ParamAngleX reaches 30.
	if body.MinX < -1.001 || body.MinX > -0.999 {
		t.Errorf("body.MinX = %v, want -1", body.MinX)
	}
	if body.MaxX < 1.19 || body.MaxX > 1.21 {
		t.Errorf("body.MaxX = %v, want about 1.2", body.MaxX)
	}

	names := map[string]*mofufmt.Animation{}
	for i := range f.Animations {
		names[f.Animations[i].Name] = &f.Animations[i]
	}
	for _, want := range []string{bake.RestAnimation, "Idle", "Tap.0", "Tap.1"} {
		if _, ok := names[want]; !ok {
			t.Errorf("missing animation %q; got %v", want, keys(names))
		}
	}
	if f.Animations[0].Name != bake.RestAnimation {
		t.Errorf("the rest pose should come first, got %q", f.Animations[0].Name)
	}

	rest := names[bake.RestAnimation]
	if rest.FrameCount != 1 {
		t.Errorf("rest frame count = %d, want 1", rest.FrameCount)
	}

	idle := names["Idle"]
	if idle.FrameCount != 30 || idle.FPS != 30 || !idle.Loop {
		t.Errorf("Idle = %d frames @ %v fps, loop=%v", idle.FrameCount, idle.FPS, idle.Loop)
	}
	if idle.FadeIn != 0.5 || idle.FadeOut != 0.25 {
		t.Errorf("Idle fades = %v / %v, want 0.5 / 0.25", idle.FadeIn, idle.FadeOut)
	}
}

func TestBakeFoldsConstantChannels(t *testing.T) {
	res := runBake(t, bake.Options{})
	var idle, still *mofufmt.Animation
	for i := range res.File.Animations {
		switch res.File.Animations[i].Name {
		case "Idle":
			idle = &res.File.Animations[i]
		case "Tap.0":
			still = &res.File.Animations[i]
		}
	}
	if idle == nil || still == nil {
		t.Fatal("expected both Idle and Tap.0")
	}

	// Nothing moves in the still motion, so every channel collapses to one
	// sample no matter how many frames were sampled.
	for i := range still.Tracks {
		tr := &still.Tracks[i]
		if tr.Flags != 0 {
			t.Errorf("Tap.0 track %d flags = %v, want none", i, tr.Flags)
		}
		if want := res.File.Meshes[i].VertexCount() * 2; len(tr.Positions) != want {
			t.Errorf("Tap.0 track %d stored %d position values, want %d", i, len(tr.Positions), want)
		}
		if len(tr.Opacity) != 1 || len(tr.Order) != 1 || len(tr.Visible) != 1 {
			t.Errorf("Tap.0 track %d did not fold its scalar channels", i)
		}
	}

	// Idle moves everything.
	for i := range idle.Tracks {
		tr := &idle.Tracks[i]
		if !tr.Flags.Has(mofufmt.TrackPositionsAnimated) {
			t.Errorf("Idle track %d should have animated positions", i)
		}
		if want := res.File.Meshes[i].VertexCount() * 2 * int(idle.FrameCount); len(tr.Positions) != want {
			t.Errorf("Idle track %d stored %d position values, want %d", i, len(tr.Positions), want)
		}
	}
	// The eye blinks shut and its render order follows.
	eye := &idle.Tracks[1]
	if !eye.Flags.Has(mofufmt.TrackOpacityAnimated) {
		t.Error("the eye's opacity should be animated")
	}
	if !eye.Flags.Has(mofufmt.TrackVisibilityAnimated) {
		t.Error("the eye's visibility should be animated")
	}
	if !eye.Flags.Has(mofufmt.TrackOrderAnimated) {
		t.Error("the eye's render order should be animated")
	}
	// Identity colours are dropped rather than stored.
	if eye.Flags.Has(mofufmt.TrackHasColors) || eye.Colors != nil {
		t.Error("identity colours should not be stored")
	}
}

func TestBakeRestPoseMatchesDefaults(t *testing.T) {
	res := runBake(t, bake.Options{})
	rest := &res.File.Animations[0]
	body := &res.File.Meshes[0]
	// With ParamAngleX at its default of 0 the quad sits at x = -1 and 1.
	q := rest.Tracks[0].PositionFrame(0, body.VertexCount())
	if got := body.DequantX(q[0]); got < -1.001 || got > -0.999 {
		t.Errorf("rest vertex 0 x = %v, want -1", got)
	}
	if got := body.DequantX(q[2]); got < 0.999 || got > 1.001 {
		t.Errorf("rest vertex 1 x = %v, want 1", got)
	}
	if got := rest.Tracks[1].OpacityAt(0); got != 1 {
		t.Errorf("rest eye opacity = %v, want 1", got)
	}
}

func TestBakeFpsOverride(t *testing.T) {
	res := runBake(t, bake.Options{FPS: 10})
	for i := range res.File.Animations {
		a := &res.File.Animations[i]
		if a.Name == bake.RestAnimation {
			continue
		}
		if a.FPS != 10 {
			t.Errorf("%s fps = %v, want 10", a.Name, a.FPS)
		}
	}
}

func TestBakeWarnsAboutUnbakeables(t *testing.T) {
	res := runBake(t, bake.Options{})
	joined := ""
	for _, w := range res.Warnings {
		joined += w + "\n"
	}
	for _, want := range []string{"physics", "hit area", "expression"} {
		if !bytes.Contains([]byte(joined), []byte(want)) {
			t.Errorf("warnings do not mention %q; got:\n%s", want, joined)
		}
	}
}

func TestBakedFileSurvivesEncoding(t *testing.T) {
	res := runBake(t, bake.Options{})
	var buf bytes.Buffer
	if err := mofufmt.Encode(&buf, res.File, mofufmt.EncodeOptions{}); err != nil {
		t.Fatal(err)
	}
	got, err := mofufmt.Decode(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Meshes) != len(res.File.Meshes) || len(got.Animations) != len(res.File.Animations) {
		t.Fatalf("decoded %d meshes / %d animations, want %d / %d",
			len(got.Meshes), len(got.Animations), len(res.File.Meshes), len(res.File.Animations))
	}
	for i := range got.Animations {
		a, b := &got.Animations[i], &res.File.Animations[i]
		if a.Name != b.Name || a.FrameCount != b.FrameCount || a.Loop != b.Loop {
			t.Errorf("animation %d differs: %+v vs %+v", i, a, b)
		}
	}
}

func TestBakeMissingCore(t *testing.T) {
	if _, err := bake.Run(writeModel(t), bake.Options{CorePath: "/definitely/not/a/library.so"}); err == nil {
		t.Error("bake.Run with a bogus Core path succeeded")
	}
}

func keys(m map[string]*mofufmt.Animation) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func TestBakeFrameCounts(t *testing.T) {
	res := runBake(t, bake.Options{})
	byName := map[string]*mofufmt.Animation{}
	for i := range res.File.Animations {
		byName[res.File.Animations[i].Name] = &res.File.Animations[i]
	}

	// A looping motion is sampled over [0, duration): 1.0s at 30fps is 30
	// frames, and its stored duration is the motion's own.
	idle := byName["Idle"]
	if idle.FrameCount != 30 {
		t.Errorf("Idle frame count = %d, want 30", idle.FrameCount)
	}
	if d := idle.Duration(); d < 0.999 || d > 1.001 {
		t.Errorf("Idle duration = %v, want 1", d)
	}

	// A one-shot includes its endpoint: 1.0s at 10fps is 11 frames spanning
	// 10 intervals, so the duration still comes back as 1 second.
	ramp := byName["Tap.1"]
	if ramp.FrameCount != 11 {
		t.Errorf("Tap.1 frame count = %d, want 11", ramp.FrameCount)
	}
	if d := ramp.Duration(); d < 0.999 || d > 1.001 {
		t.Errorf("Tap.1 duration = %v, want 1", d)
	}

	// The ramp drives the eye's opacity from 1 down to 0; the final pose has
	// to actually be present.
	eye := &ramp.Tracks[1]
	if got := eye.OpacityAt(0); got != 1 {
		t.Errorf("Tap.1 eye opacity at the first frame = %v, want 1", got)
	}
	if got := eye.OpacityAt(int(ramp.FrameCount) - 1); got != 0 {
		t.Errorf("Tap.1 eye opacity at the last frame = %v, want 0", got)
	}
}

// physics3JSON hangs a pendulum off ParamAngleX and writes its swing back to
// ParamEyeLOpen, so the stub model's eye opacity reveals the simulation.
const physics3JSON = `{
  "Version": 3,
  "Meta": {
    "PhysicsSettingCount": 1,
    "EffectiveForces": {"Gravity": {"X": 0, "Y": -1}, "Wind": {"X": 0, "Y": 0}}
  },
  "PhysicsSettings": [{
    "Id": "Setting1",
    "Input": [{"Source": {"Target": "Parameter", "Id": "ParamAngleX"}, "Weight": 100, "Type": "Angle", "Reflect": false}],
    "Output": [{"Destination": {"Target": "Parameter", "Id": "ParamEyeLOpen"}, "VertexIndex": 1, "Scale": 1, "Weight": 100, "Type": "Angle", "Reflect": false}],
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

// writePhysicsModel is writeModel plus an actual physics3.json on disk.
func writePhysicsModel(t *testing.T) string {
	t.Helper()
	path := writeModel(t)
	full := filepath.Join(filepath.Dir(path), "test.physics3.json")
	if err := os.WriteFile(full, []byte(physics3JSON), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestBakeHitAreas(t *testing.T) {
	res := runBake(t, bake.Options{})
	if len(res.File.HitAreas) != 1 {
		t.Fatalf("hit areas = %+v, want just Body", res.File.HitAreas)
	}
	h := res.File.HitAreas[0]
	if h.Name != "Body" || h.Mesh != 0 {
		t.Errorf("hit area = %+v, want {Body 0}", h)
	}
}

func TestBakeSoundAndEvents(t *testing.T) {
	res := runBake(t, bake.Options{})
	var idle *mofufmt.Animation
	for i := range res.File.Animations {
		if res.File.Animations[i].Name == "Idle" {
			idle = &res.File.Animations[i]
		}
	}
	if idle == nil {
		t.Fatal("no Idle animation")
	}
	if idle.Sound != "sounds/idle.wav" {
		t.Errorf("Idle sound = %q, want sounds/idle.wav", idle.Sound)
	}
	if len(idle.Events) != 1 || idle.Events[0].Value != "blink" || idle.Events[0].Time != 0.5 {
		t.Errorf("Idle events = %+v, want [{0.5 blink}]", idle.Events)
	}
}

func TestBakeExpressionOverlay(t *testing.T) {
	res := runBake(t, bake.Options{})
	if len(res.File.Overlays) != 1 {
		t.Fatalf("overlays = %d, want 1 (the broken one skipped)", len(res.File.Overlays))
	}
	ov := &res.File.Overlays[0]
	if ov.Name != "f01" {
		t.Errorf("overlay name = %q, want f01", ov.Name)
	}
	if len(ov.Tracks) != len(res.File.Meshes) {
		t.Fatalf("overlay has %d tracks for %d meshes", len(ov.Tracks), len(res.File.Meshes))
	}
	// ParamAngleX at 30 slides every vertex +0.2 in x in the stub core, so
	// the delta must be (0.2, 0) at each vertex of the body mesh.
	tr := &ov.Tracks[0]
	if tr.DeltaPositions == nil {
		t.Fatal("body overlay stored no position deltas")
	}
	for v := 0; v < len(tr.DeltaPositions); v += 2 {
		dx, dy := tr.DeltaPositions[v], tr.DeltaPositions[v+1]
		if dx < 0.19 || dx > 0.21 || dy != 0 {
			t.Errorf("vertex %d delta = (%v, %v), want about (0.2, 0)", v/2, dx, dy)
		}
	}

	// Skipping expressions leaves overlays out and says so.
	skipped := runBake(t, bake.Options{SkipExpressions: true})
	if len(skipped.File.Overlays) != 0 {
		t.Error("SkipExpressions still produced overlays")
	}
}

func TestBakePhysicsChangesOutput(t *testing.T) {
	lib := fakecore.Build(t)
	path := writePhysicsModel(t)

	with, err := bake.Run(path, bake.Options{CorePath: lib})
	if err != nil {
		t.Fatal(err)
	}
	without, err := bake.Run(path, bake.Options{CorePath: lib, SkipPhysics: true})
	if err != nil {
		t.Fatal(err)
	}

	find := func(r *bake.Result) *mofufmt.Animation {
		for i := range r.File.Animations {
			if r.File.Animations[i].Name == "Idle" {
				return &r.File.Animations[i]
			}
		}
		t.Fatal("no Idle")
		return nil
	}
	a, b := find(with), find(without)

	// Physics writes the pendulum's swing into ParamEyeLOpen, which the stub
	// maps straight to the eye's opacity: with physics on, the idle motion's
	// opacity curve has to differ somewhere.
	differs := false
	for f := 0; f < int(a.FrameCount); f++ {
		if a.Tracks[1].OpacityAt(f) != b.Tracks[1].OpacityAt(f) {
			differs = true
			break
		}
	}
	if !differs {
		t.Error("physics on and off baked identical eye opacity")
	}

	// Determinism: two runs with physics agree exactly.
	again, err := bake.Run(path, bake.Options{CorePath: lib})
	if err != nil {
		t.Fatal(err)
	}
	c := find(again)
	for f := 0; f < int(a.FrameCount); f++ {
		if a.Tracks[1].OpacityAt(f) != c.Tracks[1].OpacityAt(f) {
			t.Fatalf("physics bake is not deterministic at frame %d", f)
		}
	}
}

func TestBakePhysicsStillMotionStaysConstant(t *testing.T) {
	lib := fakecore.Build(t)
	res, err := bake.Run(writePhysicsModel(t), bake.Options{CorePath: lib})
	if err != nil {
		t.Fatal(err)
	}
	// The still motion holds every input at its default; after the settling
	// pre-roll the pendulum is at rest, so channels must still fold down to
	// single samples even with physics running.
	for i := range res.File.Animations {
		a := &res.File.Animations[i]
		if a.Name != "Tap.0" {
			continue
		}
		for j := range a.Tracks {
			if a.Tracks[j].Flags != 0 {
				t.Errorf("physics broke channel folding on still motion, track %d flags = %v", j, a.Tracks[j].Flags)
			}
		}
	}
}
