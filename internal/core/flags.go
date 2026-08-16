package core

// ConstantFlags are the per-drawable flags that never change during the
// lifetime of a model (Live2DCubismCore.h: csmBlendAdditive .. csmIsInvertedMask).
type ConstantFlags uint8

const (
	// BlendAdditive selects additive blending for the drawable.
	BlendAdditive ConstantFlags = 1 << 0
	// BlendMultiplicative selects multiplicative blending for the drawable.
	BlendMultiplicative ConstantFlags = 1 << 1
	// DoubleSided marks the drawable as not back-face culled.
	DoubleSided ConstantFlags = 1 << 2
	// InvertedMask inverts the clipping mask of the drawable.
	InvertedMask ConstantFlags = 1 << 3
)

// Has reports whether all bits of f are set.
func (c ConstantFlags) Has(f ConstantFlags) bool { return c&f == f }

// DynamicFlags are the per-drawable flags refreshed by every csmUpdateModel
// call (Live2DCubismCore.h: csmIsVisible .. csmBlendColorDidChange).
type DynamicFlags uint8

const (
	// IsVisible reports that the drawable is currently visible.
	IsVisible DynamicFlags = 1 << 0
	// VisibilityDidChange reports that visibility changed in the last update.
	VisibilityDidChange DynamicFlags = 1 << 1
	// OpacityDidChange reports that opacity changed in the last update.
	OpacityDidChange DynamicFlags = 1 << 2
	// DrawOrderDidChange reports that the draw order changed in the last update.
	DrawOrderDidChange DynamicFlags = 1 << 3
	// RenderOrderDidChange reports that the render order changed in the last update.
	RenderOrderDidChange DynamicFlags = 1 << 4
	// VertexPositionsDidChange reports that vertices moved in the last update.
	VertexPositionsDidChange DynamicFlags = 1 << 5
	// BlendColorDidChange reports that the blend color changed in the last update.
	BlendColorDidChange DynamicFlags = 1 << 6
)

// Has reports whether all bits of f are set.
func (d DynamicFlags) Has(f DynamicFlags) bool { return d&f == f }

// ParameterType mirrors csmParameterType.
type ParameterType int32

const (
	// ParameterTypeNormal is a plain scalar parameter.
	ParameterTypeNormal ParameterType = 0
	// ParameterTypeBlendShape is a blend shape parameter.
	ParameterTypeBlendShape ParameterType = 1
)

// Alignment requirements documented in Live2DCubismCore.h.
const (
	alignofMoc   = 64
	alignofModel = 16
)

// Vec2 mirrors csmVector2.
type Vec2 struct{ X, Y float32 }

// Vec4 mirrors csmVector4.
type Vec4 struct{ X, Y, Z, W float32 }
