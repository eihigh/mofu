// Package mofufmt defines the .mofu container: the baked, Core-free form of a
// Live2D model.
//
// A .mofu file is deliberately dumb. All deformation has already happened at
// bake time, so what remains is a set of static meshes plus, per animation,
// keyframed vertex positions and a handful of per-drawable channels. That is
// the classic vertex-animation ("morph target") layout, with the 2D extras a
// Live2D model needs bolted on: a texture index, clipping masks, a render
// order and Cubism's multiply/screen colours.
//
// The package has no dependency on the Cubism Core or on Ebitengine, so both
// the converter and the runtime can share it.
package mofufmt

import "math"

// Magic identifies a .mofu file.
var Magic = [4]byte{'M', 'O', 'F', 'U'}

// Version is the container version this package reads and writes.
const Version uint32 = 1

// Container flags.
const (
	// FlagCompressed marks the body as gzip-compressed.
	FlagCompressed uint32 = 1 << 0
)

// File is a whole baked model.
type File struct {
	Canvas     Canvas
	Textures   []Texture
	Meshes     []Mesh
	Animations []Animation
}

// Canvas describes the model's coordinate system, copied from
// csmReadCanvasInfo. Vertex positions are in model units; multiplying by
// PixelsPerUnit and offsetting by the origin puts them in canvas pixels.
type Canvas struct {
	Width         float32
	Height        float32
	OriginX       float32
	OriginY       float32
	PixelsPerUnit float32
}

// Texture is one texture atlas, stored as the original encoded image bytes so
// the runtime can decode it with any image codec it already links.
type Texture struct {
	Name string
	Data []byte
}

// MeshFlags mirror the Cubism constant flags that survive baking.
type MeshFlags uint8

// Mesh flag bits.
const (
	// MeshBlendAdditive selects additive blending.
	MeshBlendAdditive MeshFlags = 1 << 0
	// MeshBlendMultiplicative selects multiplicative blending.
	MeshBlendMultiplicative MeshFlags = 1 << 1
	// MeshDoubleSided disables back-face culling.
	MeshDoubleSided MeshFlags = 1 << 2
	// MeshInvertedMask inverts the clipping mask.
	MeshInvertedMask MeshFlags = 1 << 3
)

// Has reports whether all bits of f are set.
func (m MeshFlags) Has(f MeshFlags) bool { return m&f == f }

// Mesh is the time-invariant part of one drawable: its topology, its UVs and
// the bounding box its animated positions are quantised against.
type Mesh struct {
	ID           string
	TextureIndex int32
	Flags        MeshFlags

	// Masks lists the mesh indices that clip this mesh. Empty means unclipped.
	Masks []int32

	// UVs are interleaved u,v pairs; len(UVs) == 2*VertexCount.
	UVs []float32
	// Indices are triangle indices into the mesh's vertices.
	Indices []uint16

	// MinX..MaxY bound every position this mesh takes in every animation.
	// Positions are stored as uint16 fractions of this box.
	MinX, MinY, MaxX, MaxY float32
}

// VertexCount is the number of vertices of the mesh.
func (m *Mesh) VertexCount() int { return len(m.UVs) / 2 }

// DequantX converts a stored X sample back to model units.
func (m *Mesh) DequantX(q uint16) float32 {
	return m.MinX + (m.MaxX-m.MinX)*float32(q)/65535
}

// DequantY converts a stored Y sample back to model units.
func (m *Mesh) DequantY(q uint16) float32 {
	return m.MinY + (m.MaxY-m.MinY)*float32(q)/65535
}

// QuantX converts an X position in model units to its stored form.
func (m *Mesh) QuantX(v float32) uint16 { return quant(v, m.MinX, m.MaxX) }

// QuantY converts a Y position in model units to its stored form.
func (m *Mesh) QuantY(v float32) uint16 { return quant(v, m.MinY, m.MaxY) }

func quant(v, lo, hi float32) uint16 {
	if hi <= lo {
		return 0
	}
	u := float64(v-lo) / float64(hi-lo)
	q := math.Round(u * 65535)
	switch {
	case q < 0:
		return 0
	case q > 65535:
		return 65535
	}
	return uint16(q)
}

// Animation is one baked motion, sampled at a fixed rate.
type Animation struct {
	Name       string
	FPS        float32
	FrameCount int32
	Loop       bool
	FadeIn     float32
	FadeOut    float32

	// Tracks is parallel to File.Meshes.
	Tracks []Track
}

// Duration is the animation's length in seconds.
//
// A looping animation is sampled over the half-open interval [0, duration):
// its last frame blends back into its first, so all FrameCount frames span
// the duration. A one-shot animation instead includes both endpoints, so its
// FrameCount frames span FrameCount-1 intervals.
func (a *Animation) Duration() float64 {
	if a.FPS <= 0 || a.FrameCount <= 0 {
		return 0
	}
	frames := float64(a.FrameCount)
	if !a.Loop {
		frames--
	}
	return frames / float64(a.FPS)
}

// TrackFlags records which channels of a track actually vary over time.
// A channel that does not vary is stored once instead of once per frame,
// which is what makes the format small: in a typical model most drawables sit
// still for most motions.
type TrackFlags uint8

// Track flag bits.
const (
	// TrackPositionsAnimated marks per-frame vertex positions.
	TrackPositionsAnimated TrackFlags = 1 << 0
	// TrackOpacityAnimated marks per-frame opacity.
	TrackOpacityAnimated TrackFlags = 1 << 1
	// TrackOrderAnimated marks per-frame render order.
	TrackOrderAnimated TrackFlags = 1 << 2
	// TrackVisibilityAnimated marks per-frame visibility.
	TrackVisibilityAnimated TrackFlags = 1 << 3
	// TrackHasColors marks the presence of multiply/screen colour data.
	TrackHasColors TrackFlags = 1 << 4
	// TrackColorsAnimated marks per-frame multiply/screen colour.
	TrackColorsAnimated TrackFlags = 1 << 5
)

// Has reports whether all bits of f are set.
func (t TrackFlags) Has(f TrackFlags) bool { return t&f == f }

// Track holds the animated channels of one mesh. Each channel is either one
// sample (constant) or FrameCount samples, as recorded in Flags.
type Track struct {
	Flags TrackFlags

	// Positions is quantised x,y per vertex, laid out frame-major.
	Positions []uint16
	// Opacity is the drawable opacity per frame.
	Opacity []float32
	// Order is the render order per frame; lower draws first.
	Order []int32
	// Visible is 0 or 1 per frame.
	Visible []uint8
	// Colors is multiply RGBA followed by screen RGBA, 8 floats per frame.
	Colors []float32
}

// PositionFrame returns the quantised positions of frame f, a slice of
// 2*vertexCount values. Out-of-range frames clamp.
func (t *Track) PositionFrame(f, vertexCount int) []uint16 {
	stride := vertexCount * 2
	if stride == 0 || len(t.Positions) == 0 {
		return nil
	}
	return t.Positions[clampOffset(f, stride, len(t.Positions)):][:stride]
}

// OpacityAt returns the opacity of frame f.
func (t *Track) OpacityAt(f int) float32 {
	if len(t.Opacity) == 0 {
		return 1
	}
	return t.Opacity[clampIndex(f, len(t.Opacity))]
}

// OrderAt returns the render order of frame f.
func (t *Track) OrderAt(f int) int32 {
	if len(t.Order) == 0 {
		return 0
	}
	return t.Order[clampIndex(f, len(t.Order))]
}

// VisibleAt reports whether the mesh is visible on frame f.
func (t *Track) VisibleAt(f int) bool {
	if len(t.Visible) == 0 {
		return true
	}
	return t.Visible[clampIndex(f, len(t.Visible))] != 0
}

// MultiplyAt returns the multiply colour of frame f.
func (t *Track) MultiplyAt(f int) [4]float32 {
	c := t.colorFrame(f)
	if c == nil {
		return [4]float32{1, 1, 1, 1}
	}
	return [4]float32{c[0], c[1], c[2], c[3]}
}

// ScreenAt returns the screen colour of frame f.
func (t *Track) ScreenAt(f int) [4]float32 {
	c := t.colorFrame(f)
	if c == nil {
		return [4]float32{0, 0, 0, 1}
	}
	return [4]float32{c[4], c[5], c[6], c[7]}
}

func (t *Track) colorFrame(f int) []float32 {
	if len(t.Colors) < 8 {
		return nil
	}
	return t.Colors[clampOffset(f, 8, len(t.Colors)):][:8]
}

func clampIndex(i, n int) int {
	switch {
	case n <= 0:
		return 0
	case i < 0:
		return 0
	case i >= n:
		return n - 1
	}
	return i
}

func clampOffset(frame, stride, total int) int {
	frames := total / stride
	return clampIndex(frame, frames) * stride
}
