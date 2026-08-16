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
    "Expressions": [{"Name": "f01", "File": "exp/f01.exp3.json"}],
    "Motions": {
      "Idle": [{"File": "motions/idle.motion3.json", "FadeInTime": 0.5, "FadeOutTime": 0.25}],
      "Tap": [
        {"File": "motions/tap0.motion3.json"},
        {"File": "motions/tap1.motion3.json"}
      ]
    }
  }
}`

// idleMotion sweeps ParamAngleX across its range and blinks the eye shut
// halfway through, so both the position and the scalar channels move.
const idleMotion = `{
  "Version": 3,
  "Meta": {"Duration": 1.0, "Fps": 30.0, "Loop": true, "AreBeziersRestricted": true, "CurveCount": 2},
  "Curves": [
    {"Target": "Parameter", "Id": "ParamAngleX", "Segments": [0, 0, 0, 1.0, 30]},
    {"Target": "Parameter", "Id": "ParamEyeLOpen", "Segments": [0, 1, 2, 0.5, 0, 2, 1.0, 1]}
  ]
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
	for _, want := range []string{"physics", "expression"} {
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
