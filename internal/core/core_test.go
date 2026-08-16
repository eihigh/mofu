package core_test

import (
	"testing"

	"github.com/eihigh/mofu/internal/core"
	"github.com/eihigh/mofu/internal/fakecore"
)

// load opens the stub Core and instantiates its model.
func load(t *testing.T) (*core.Core, *core.Model) {
	t.Helper()
	c, err := core.Load(fakecore.Build(t))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	t.Cleanup(func() { c.Close() })
	// The stub only checks that the moc bytes are non-empty and correctly
	// aligned; the real Core parses them.
	m, err := c.NewModel([]byte("fake moc3 payload"))
	if err != nil {
		t.Fatalf("NewModel: %v", err)
	}
	return c, m
}

func TestVersion(t *testing.T) {
	c, _ := load(t)
	if got := c.Version().String(); got != "5.0.0" {
		t.Errorf("Version = %s, want 5.0.0", got)
	}
	if got := c.LatestMocVersion(); got != 6 {
		t.Errorf("LatestMocVersion = %d, want 6", got)
	}
}

func TestNewModelRejectsEmptyMoc(t *testing.T) {
	c, _ := load(t)
	if _, err := c.NewModel(nil); err == nil {
		t.Error("NewModel(nil) succeeded, want an error")
	}
}

func TestCanvasInfo(t *testing.T) {
	_, m := load(t)
	size, origin, ppu := m.CanvasInfo()
	if size.X != 512 || size.Y != 1024 {
		t.Errorf("size = %v, want (512, 1024)", size)
	}
	if origin.X != 256 || origin.Y != 512 {
		t.Errorf("origin = %v, want (256, 512)", origin)
	}
	if ppu != 256 {
		t.Errorf("pixelsPerUnit = %v, want 256", ppu)
	}
}

func TestParametersAndParts(t *testing.T) {
	_, m := load(t)
	if got := m.ParameterCount(); got != 2 {
		t.Fatalf("ParameterCount = %d, want 2", got)
	}
	if got := m.ParameterIDs(); got[0] != "ParamAngleX" || got[1] != "ParamEyeLOpen" {
		t.Errorf("ParameterIDs = %v", got)
	}
	if got := m.ParameterMinimums(); got[0] != -30 {
		t.Errorf("ParameterMinimums[0] = %v, want -30", got[0])
	}
	if got := m.ParameterMaximums(); got[0] != 30 {
		t.Errorf("ParameterMaximums[0] = %v, want 30", got[0])
	}
	if got := m.ParameterDefaults(); got[1] != 1 {
		t.Errorf("ParameterDefaults[1] = %v, want 1", got[1])
	}
	if got := m.ParameterTypes(); len(got) != 2 {
		t.Errorf("ParameterTypes = %v", got)
	}
	if got := m.PartIDs(); got[0] != "PartBody" || got[1] != "PartEye" {
		t.Errorf("PartIDs = %v", got)
	}
	if got := m.PartParents(); got[0] != -1 || got[1] != 0 {
		t.Errorf("PartParents = %v", got)
	}
}

func TestDrawableStaticData(t *testing.T) {
	_, m := load(t)
	if got := m.DrawableCount(); got != 2 {
		t.Fatalf("DrawableCount = %d, want 2", got)
	}
	if got := m.DrawableIDs(); got[0] != "body" || got[1] != "eye" {
		t.Errorf("DrawableIDs = %v", got)
	}
	if got := m.ConstantFlags(); !got[1].Has(core.BlendAdditive) || got[0].Has(core.BlendAdditive) {
		t.Errorf("ConstantFlags = %v", got)
	}
	uvs := m.VertexUVs()
	if len(uvs) != 2 || len(uvs[0]) != 4 {
		t.Fatalf("VertexUVs shape = %d x %d", len(uvs), len(uvs[0]))
	}
	if uvs[0][2] != (core.Vec2{X: 1, Y: 1}) {
		t.Errorf("uv[0][2] = %v, want (1, 1)", uvs[0][2])
	}
	idx := m.Indices()
	if len(idx[0]) != 6 || idx[0][3] != 0 {
		t.Errorf("Indices[0] = %v", idx[0])
	}
	masks := m.Masks()
	if len(masks[0]) != 0 {
		t.Errorf("masks[0] = %v, want none", masks[0])
	}
	if len(masks[1]) != 1 || masks[1][0] != 0 {
		t.Errorf("masks[1] = %v, want [0]", masks[1])
	}
	if got := m.MultiplyColors(); got[0] != (core.Vec4{X: 1, Y: 1, Z: 1, W: 1}) {
		t.Errorf("MultiplyColors[0] = %v", got[0])
	}
	if got := m.ScreenColors(); got[0] != (core.Vec4{W: 1}) {
		t.Errorf("ScreenColors[0] = %v", got[0])
	}
}

func TestUpdateReflectsParameters(t *testing.T) {
	_, m := load(t)
	values := m.ParameterValues()
	positions := m.VertexPositions()

	copy(values, m.ParameterDefaults())
	m.Update()
	restX := positions[0][0].X

	// Driving ParamAngleX to its maximum slides the mesh by 0.2 units.
	values[0] = 30
	m.Update()
	if got := positions[0][0].X - restX; got < 0.19 || got > 0.21 {
		t.Errorf("vertex moved by %v, want about 0.2", got)
	}

	// Closing the eye drops its opacity and clears its visible flag.
	values[1] = 0
	m.Update()
	if got := m.Opacities()[1]; got != 0 {
		t.Errorf("eye opacity = %v, want 0", got)
	}
	if m.DynamicFlags()[1].Has(core.IsVisible) {
		t.Error("the eye is still flagged visible")
	}
	if !m.DynamicFlags()[0].Has(core.IsVisible) {
		t.Error("the body should stay visible")
	}

	// Part opacity multiplies into the drawable's.
	values[1] = 1
	m.PartOpacities()[1] = 0.25
	m.Update()
	if got := m.Opacities()[1]; got != 0.25 {
		t.Errorf("eye opacity with part opacity = %v, want 0.25", got)
	}

	// Render order is dynamic in the stub, which is what the baker relies on.
	if got := m.RenderOrders()[1]; got != 1 {
		t.Errorf("RenderOrders[1] = %d, want 1", got)
	}
	values[1] = 0.1
	m.Update()
	if got := m.RenderOrders()[1]; got != -1 {
		t.Errorf("RenderOrders[1] after closing = %d, want -1", got)
	}
}

func TestLogFunction(t *testing.T) {
	c, _ := load(t)
	// Only checks that installing a callback does not blow up; the stub never
	// calls it.
	c.SetLogFunction(func(string) {})
	c.SetLogFunction(nil)
}
