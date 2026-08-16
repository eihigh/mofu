package mofu

import "github.com/hajimehoshi/ebiten/v2"

// The three Cubism blend modes, transcribed from the blend functions the
// official renderers install (glBlendFuncSeparate arguments in
// CubismRenderer_OpenGLES2).
var (
	// blendNormal is source-over: src + dst*(1-srcA).
	blendNormal = ebiten.Blend{
		BlendFactorSourceRGB:        ebiten.BlendFactorOne,
		BlendFactorSourceAlpha:      ebiten.BlendFactorOne,
		BlendFactorDestinationRGB:   ebiten.BlendFactorOneMinusSourceAlpha,
		BlendFactorDestinationAlpha: ebiten.BlendFactorOneMinusSourceAlpha,
		BlendOperationRGB:           ebiten.BlendOperationAdd,
		BlendOperationAlpha:         ebiten.BlendOperationAdd,
	}

	// blendAdditive adds colour and leaves destination alpha alone.
	blendAdditive = ebiten.Blend{
		BlendFactorSourceRGB:        ebiten.BlendFactorOne,
		BlendFactorSourceAlpha:      ebiten.BlendFactorZero,
		BlendFactorDestinationRGB:   ebiten.BlendFactorOne,
		BlendFactorDestinationAlpha: ebiten.BlendFactorOne,
		BlendOperationRGB:           ebiten.BlendOperationAdd,
		BlendOperationAlpha:         ebiten.BlendOperationAdd,
	}

	// blendMultiplicative multiplies with the destination.
	blendMultiplicative = ebiten.Blend{
		BlendFactorSourceRGB:        ebiten.BlendFactorDestinationColor,
		BlendFactorSourceAlpha:      ebiten.BlendFactorZero,
		BlendFactorDestinationRGB:   ebiten.BlendFactorOneMinusSourceAlpha,
		BlendFactorDestinationAlpha: ebiten.BlendFactorOne,
		BlendOperationRGB:           ebiten.BlendOperationAdd,
		BlendOperationAlpha:         ebiten.BlendOperationAdd,
	}

	// blendMaskApply multiplies the destination by the source's alpha:
	// dst = dst * srcA. Drawing the mask buffer over a rendered drawable with
	// this blend is how clipping is applied without a second sampler.
	blendMaskApply = ebiten.Blend{
		BlendFactorSourceRGB:        ebiten.BlendFactorZero,
		BlendFactorSourceAlpha:      ebiten.BlendFactorZero,
		BlendFactorDestinationRGB:   ebiten.BlendFactorSourceAlpha,
		BlendFactorDestinationAlpha: ebiten.BlendFactorSourceAlpha,
		BlendOperationRGB:           ebiten.BlendOperationAdd,
		BlendOperationAlpha:         ebiten.BlendOperationAdd,
	}

	// blendMaskApplyInverted is blendMaskApply for inverted masks:
	// dst = dst * (1 - srcA).
	blendMaskApplyInverted = ebiten.Blend{
		BlendFactorSourceRGB:        ebiten.BlendFactorZero,
		BlendFactorSourceAlpha:      ebiten.BlendFactorZero,
		BlendFactorDestinationRGB:   ebiten.BlendFactorOneMinusSourceAlpha,
		BlendFactorDestinationAlpha: ebiten.BlendFactorOneMinusSourceAlpha,
		BlendOperationRGB:           ebiten.BlendOperationAdd,
		BlendOperationAlpha:         ebiten.BlendOperationAdd,
	}
)
