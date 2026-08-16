// Package core is a thin binding to the proprietary Live2D Cubism Core
// dynamic library.
//
// The library is not redistributable, so it is never linked at build time:
// it is looked up at run time with dlopen/LoadLibrary, next to the running
// executable by default. Everything here mirrors Live2DCubismCore.h.
package core

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"unsafe"

	"github.com/ebitengine/purego"
)

// ErrLibraryNotFound is returned when no Cubism Core library could be located.
var ErrLibraryNotFound = errors.New("core: Live2D Cubism Core library not found")

// Core is a loaded Cubism Core library.
type Core struct {
	handle uintptr
	path   string

	// Required entry points.
	csmGetVersion             func() uint32
	csmGetLatestMocVersion    func() uint32
	csmGetMocVersion          func(uintptr, uint32) uint32
	csmReviveMocInPlace       func(uintptr, uint32) uintptr
	csmGetSizeofModel         func(uintptr) uint32
	csmInitializeModelInPlace func(uintptr, uintptr, uint32) uintptr
	csmUpdateModel            func(uintptr)
	csmReadCanvasInfo         func(uintptr, uintptr, uintptr, uintptr)

	csmGetParameterCount         func(uintptr) int32
	csmGetParameterIds           func(uintptr) uintptr
	csmGetParameterMinimumValues func(uintptr) uintptr
	csmGetParameterMaximumValues func(uintptr) uintptr
	csmGetParameterDefaultValues func(uintptr) uintptr
	csmGetParameterValues        func(uintptr) uintptr

	csmGetPartCount     func(uintptr) int32
	csmGetPartIds       func(uintptr) uintptr
	csmGetPartOpacities func(uintptr) uintptr

	csmGetDrawableCount           func(uintptr) int32
	csmGetDrawableIds             func(uintptr) uintptr
	csmGetDrawableConstantFlags   func(uintptr) uintptr
	csmGetDrawableDynamicFlags    func(uintptr) uintptr
	csmGetDrawableTextureIndices  func(uintptr) uintptr
	csmGetDrawableRenderOrders    func(uintptr) uintptr
	csmGetDrawableOpacities       func(uintptr) uintptr
	csmGetDrawableMaskCounts      func(uintptr) uintptr
	csmGetDrawableMasks           func(uintptr) uintptr
	csmGetDrawableVertexCounts    func(uintptr) uintptr
	csmGetDrawableVertexPositions func(uintptr) uintptr
	csmGetDrawableVertexUvs       func(uintptr) uintptr
	csmGetDrawableIndexCounts     func(uintptr) uintptr
	csmGetDrawableIndices         func(uintptr) uintptr
	csmResetDrawableDynamicFlags  func(uintptr)

	// Optional entry points. Availability depends on the Core version, so
	// every use must nil-check first.
	csmHasMocConsistency            func(uintptr, uint32) int32
	csmSetLogFunction               func(uintptr)
	csmGetParameterTypes            func(uintptr) uintptr
	csmGetParameterRepeats          func(uintptr) uintptr
	csmGetPartParentPartIndices     func(uintptr) uintptr
	csmGetDrawableDrawOrders        func(uintptr) uintptr
	csmGetDrawableParentPartIndices func(uintptr) uintptr
	csmGetDrawableMultiplyColors    func(uintptr) uintptr
	csmGetDrawableScreenColors      func(uintptr) uintptr
}

// binding describes one entry point to resolve.
type binding struct {
	fn       any
	name     string
	optional bool
}

func (c *Core) bindings() []binding {
	return []binding{
		{&c.csmGetVersion, "csmGetVersion", false},
		{&c.csmGetLatestMocVersion, "csmGetLatestMocVersion", false},
		{&c.csmGetMocVersion, "csmGetMocVersion", false},
		{&c.csmReviveMocInPlace, "csmReviveMocInPlace", false},
		{&c.csmGetSizeofModel, "csmGetSizeofModel", false},
		{&c.csmInitializeModelInPlace, "csmInitializeModelInPlace", false},
		{&c.csmUpdateModel, "csmUpdateModel", false},
		{&c.csmReadCanvasInfo, "csmReadCanvasInfo", false},

		{&c.csmGetParameterCount, "csmGetParameterCount", false},
		{&c.csmGetParameterIds, "csmGetParameterIds", false},
		{&c.csmGetParameterMinimumValues, "csmGetParameterMinimumValues", false},
		{&c.csmGetParameterMaximumValues, "csmGetParameterMaximumValues", false},
		{&c.csmGetParameterDefaultValues, "csmGetParameterDefaultValues", false},
		{&c.csmGetParameterValues, "csmGetParameterValues", false},

		{&c.csmGetPartCount, "csmGetPartCount", false},
		{&c.csmGetPartIds, "csmGetPartIds", false},
		{&c.csmGetPartOpacities, "csmGetPartOpacities", false},

		{&c.csmGetDrawableCount, "csmGetDrawableCount", false},
		{&c.csmGetDrawableIds, "csmGetDrawableIds", false},
		{&c.csmGetDrawableConstantFlags, "csmGetDrawableConstantFlags", false},
		{&c.csmGetDrawableDynamicFlags, "csmGetDrawableDynamicFlags", false},
		{&c.csmGetDrawableTextureIndices, "csmGetDrawableTextureIndices", false},
		{&c.csmGetDrawableRenderOrders, "csmGetDrawableRenderOrders", false},
		{&c.csmGetDrawableOpacities, "csmGetDrawableOpacities", false},
		{&c.csmGetDrawableMaskCounts, "csmGetDrawableMaskCounts", false},
		{&c.csmGetDrawableMasks, "csmGetDrawableMasks", false},
		{&c.csmGetDrawableVertexCounts, "csmGetDrawableVertexCounts", false},
		{&c.csmGetDrawableVertexPositions, "csmGetDrawableVertexPositions", false},
		{&c.csmGetDrawableVertexUvs, "csmGetDrawableVertexUvs", false},
		{&c.csmGetDrawableIndexCounts, "csmGetDrawableIndexCounts", false},
		{&c.csmGetDrawableIndices, "csmGetDrawableIndices", false},
		{&c.csmResetDrawableDynamicFlags, "csmResetDrawableDynamicFlags", false},

		{&c.csmHasMocConsistency, "csmHasMocConsistency", true},
		{&c.csmSetLogFunction, "csmSetLogFunction", true},
		{&c.csmGetParameterTypes, "csmGetParameterTypes", true},
		{&c.csmGetParameterRepeats, "csmGetParameterRepeats", true},
		{&c.csmGetPartParentPartIndices, "csmGetPartParentPartIndices", true},
		{&c.csmGetDrawableDrawOrders, "csmGetDrawableDrawOrders", true},
		{&c.csmGetDrawableParentPartIndices, "csmGetDrawableParentPartIndices", true},
		{&c.csmGetDrawableMultiplyColors, "csmGetDrawableMultiplyColors", true},
		{&c.csmGetDrawableScreenColors, "csmGetDrawableScreenColors", true},
	}
}

// Load opens the Cubism Core library at path. When path is empty the library
// is searched for with Find.
func Load(path string) (*Core, error) {
	if path == "" {
		p, err := Find()
		if err != nil {
			return nil, err
		}
		path = p
	}
	h, err := openLibrary(path)
	if err != nil {
		return nil, err
	}
	c := &Core{handle: h, path: path}
	for _, b := range c.bindings() {
		p, err := lookup(h, b.name)
		if err != nil {
			if b.optional {
				continue
			}
			closeLibrary(h)
			return nil, err
		}
		purego.RegisterFunc(b.fn, p)
	}
	return c, nil
}

// Find locates a Cubism Core library. Search order:
//
//  1. $MOFU_CUBISM_CORE, if set (a full path to the library).
//  2. The directory holding the running executable.
//  3. The current working directory.
//  4. The bare library name, letting the OS loader search its own paths.
func Find() (string, error) {
	if p := os.Getenv("MOFU_CUBISM_CORE"); p != "" {
		if _, err := os.Stat(p); err != nil {
			return "", fmt.Errorf("core: MOFU_CUBISM_CORE=%s: %w", p, err)
		}
		return p, nil
	}
	var dirs []string
	if exe, err := os.Executable(); err == nil {
		if exe, err := filepath.EvalSymlinks(exe); err == nil {
			dirs = append(dirs, filepath.Dir(exe))
		}
	}
	if wd, err := os.Getwd(); err == nil {
		dirs = append(dirs, wd)
	}
	for _, dir := range dirs {
		for _, name := range defaultLibNames() {
			p := filepath.Join(dir, name)
			if _, err := os.Stat(p); err == nil {
				return p, nil
			}
		}
	}
	// Last resort: let the loader resolve it from the system search path.
	return defaultLibNames()[0], nil
}

// Close unloads the library. Every Model created from it must be released
// first.
func (c *Core) Close() error { return closeLibrary(c.handle) }

// Path is the file the library was loaded from.
func (c *Core) Path() string { return c.path }

// Version is the Core version encoded as major<<24 | minor<<16 | patch.
func (c *Core) Version() Version { return Version(c.csmGetVersion()) }

// LatestMocVersion is the newest .moc3 version this Core can load.
func (c *Core) LatestMocVersion() uint32 { return c.csmGetLatestMocVersion() }

// SetLogFunction installs a callback for Core diagnostics. It is a no-op on
// Core builds that do not export csmSetLogFunction.
func (c *Core) SetLogFunction(fn func(string)) {
	if c.csmSetLogFunction == nil || fn == nil {
		return
	}
	cb := purego.NewCallback(func(msg uintptr) {
		fn(goString(msg))
	})
	c.csmSetLogFunction(cb)
}

// Version is a packed Cubism version number.
type Version uint32

// String formats the version as "major.minor.patch".
func (v Version) String() string {
	return fmt.Sprintf("%d.%d.%d", v>>24, (v>>16)&0xff, v&0xffff)
}

// Model is a live Cubism model instance together with the moc it was built
// from. Both live in Go memory that must stay pinned for the model's whole
// lifetime, which is why the backing slices are kept in the struct.
type Model struct {
	core *Core

	mocBuf   []byte // aligned window into a larger allocation
	modelBuf []byte // ditto
	moc      uintptr
	model    uintptr

	parameterCount int
	partCount      int
	drawableCount  int
}

// NewModel revives moc3 data and instantiates a model from it. The moc3 bytes
// are copied into a suitably aligned buffer, so the caller keeps ownership of
// the input.
func (c *Core) NewModel(moc3 []byte) (*Model, error) {
	if len(moc3) == 0 {
		return nil, errors.New("core: empty moc3 data")
	}
	m := &Model{core: c}
	m.mocBuf = alignedCopy(moc3, alignofMoc)
	mocPtr := uintptr(unsafe.Pointer(&m.mocBuf[0]))

	if c.csmHasMocConsistency != nil {
		if c.csmHasMocConsistency(mocPtr, uint32(len(m.mocBuf))) != 1 {
			return nil, errors.New("core: moc3 failed the consistency check")
		}
	}
	if v := c.csmGetMocVersion(mocPtr, uint32(len(m.mocBuf))); v == 0 {
		return nil, errors.New("core: unrecognised moc3 version")
	} else if latest := c.csmGetLatestMocVersion(); v > latest {
		return nil, fmt.Errorf("core: moc3 version %d is newer than this Core supports (%d)", v, latest)
	}

	m.moc = c.csmReviveMocInPlace(mocPtr, uint32(len(m.mocBuf)))
	if m.moc == 0 {
		return nil, errors.New("core: csmReviveMocInPlace failed")
	}
	size := c.csmGetSizeofModel(m.moc)
	if size == 0 {
		return nil, errors.New("core: csmGetSizeofModel returned 0")
	}
	m.modelBuf = alignedBuf(int(size), alignofModel)
	m.model = c.csmInitializeModelInPlace(m.moc, uintptr(unsafe.Pointer(&m.modelBuf[0])), size)
	if m.model == 0 {
		return nil, errors.New("core: csmInitializeModelInPlace failed")
	}
	m.parameterCount = int(c.csmGetParameterCount(m.model))
	m.partCount = int(c.csmGetPartCount(m.model))
	m.drawableCount = int(c.csmGetDrawableCount(m.model))
	runtime.KeepAlive(m.mocBuf)
	return m, nil
}

// Update recomputes the model from the current parameter and part values. The
// dynamic flags are reset first so that the *DidChange bits describe exactly
// this update.
func (m *Model) Update() {
	m.core.csmResetDrawableDynamicFlags(m.model)
	m.core.csmUpdateModel(m.model)
	runtime.KeepAlive(m.modelBuf)
	runtime.KeepAlive(m.mocBuf)
}

// CanvasInfo returns the canvas size in pixels, the origin in pixels and the
// pixels-per-unit factor set when the model was exported.
func (m *Model) CanvasInfo() (size, origin Vec2, pixelsPerUnit float32) {
	m.core.csmReadCanvasInfo(m.model,
		uintptr(unsafe.Pointer(&size)),
		uintptr(unsafe.Pointer(&origin)),
		uintptr(unsafe.Pointer(&pixelsPerUnit)))
	return
}

// Counts.

// ParameterCount is the number of parameters of the model.
func (m *Model) ParameterCount() int { return m.parameterCount }

// PartCount is the number of parts of the model.
func (m *Model) PartCount() int { return m.partCount }

// DrawableCount is the number of drawables of the model.
func (m *Model) DrawableCount() int { return m.drawableCount }

// Parameters.

// ParameterIDs returns the parameter identifiers, indexed by parameter index.
func (m *Model) ParameterIDs() []string {
	return stringSlice(m.core.csmGetParameterIds(m.model), m.parameterCount)
}

// ParameterMinimums returns the per-parameter lower bounds.
func (m *Model) ParameterMinimums() []float32 {
	return valueSlice[float32](m.core.csmGetParameterMinimumValues(m.model), m.parameterCount)
}

// ParameterMaximums returns the per-parameter upper bounds.
func (m *Model) ParameterMaximums() []float32 {
	return valueSlice[float32](m.core.csmGetParameterMaximumValues(m.model), m.parameterCount)
}

// ParameterDefaults returns the per-parameter default values.
func (m *Model) ParameterDefaults() []float32 {
	return valueSlice[float32](m.core.csmGetParameterDefaultValues(m.model), m.parameterCount)
}

// ParameterValues aliases the Core's mutable parameter storage: writing to the
// returned slice is how parameters are driven.
func (m *Model) ParameterValues() []float32 {
	return valueSlice[float32](m.core.csmGetParameterValues(m.model), m.parameterCount)
}

// ParameterTypes returns the parameter types, or nil on Cores that predate
// csmGetParameterTypes.
func (m *Model) ParameterTypes() []ParameterType {
	if m.core.csmGetParameterTypes == nil {
		return nil
	}
	return valueSlice[ParameterType](m.core.csmGetParameterTypes(m.model), m.parameterCount)
}

// Parts.

// PartIDs returns the part identifiers, indexed by part index.
func (m *Model) PartIDs() []string {
	return stringSlice(m.core.csmGetPartIds(m.model), m.partCount)
}

// PartOpacities aliases the Core's mutable part opacity storage.
func (m *Model) PartOpacities() []float32 {
	return valueSlice[float32](m.core.csmGetPartOpacities(m.model), m.partCount)
}

// PartParents returns each part's parent index, or -1 for roots. It is nil on
// Cores that predate csmGetPartParentPartIndices.
func (m *Model) PartParents() []int32 {
	if m.core.csmGetPartParentPartIndices == nil {
		return nil
	}
	return valueSlice[int32](m.core.csmGetPartParentPartIndices(m.model), m.partCount)
}

// Drawables.

// DrawableIDs returns the drawable identifiers, indexed by drawable index.
func (m *Model) DrawableIDs() []string {
	return stringSlice(m.core.csmGetDrawableIds(m.model), m.drawableCount)
}

// ConstantFlags returns the per-drawable constant flags.
func (m *Model) ConstantFlags() []ConstantFlags {
	return valueSlice[ConstantFlags](m.core.csmGetDrawableConstantFlags(m.model), m.drawableCount)
}

// DynamicFlags returns the per-drawable flags produced by the last Update.
func (m *Model) DynamicFlags() []DynamicFlags {
	return valueSlice[DynamicFlags](m.core.csmGetDrawableDynamicFlags(m.model), m.drawableCount)
}

// TextureIndices returns the texture each drawable samples.
func (m *Model) TextureIndices() []int32 {
	return valueSlice[int32](m.core.csmGetDrawableTextureIndices(m.model), m.drawableCount)
}

// RenderOrders returns the per-drawable render order; drawing the drawables
// sorted by this value ascending produces the intended layering.
func (m *Model) RenderOrders() []int32 {
	return valueSlice[int32](m.core.csmGetDrawableRenderOrders(m.model), m.drawableCount)
}

// DrawOrders returns the per-drawable draw order, or nil when unavailable.
func (m *Model) DrawOrders() []int32 {
	if m.core.csmGetDrawableDrawOrders == nil {
		return nil
	}
	return valueSlice[int32](m.core.csmGetDrawableDrawOrders(m.model), m.drawableCount)
}

// Opacities returns the per-drawable opacity.
func (m *Model) Opacities() []float32 {
	return valueSlice[float32](m.core.csmGetDrawableOpacities(m.model), m.drawableCount)
}

// VertexCounts returns the number of vertices of each drawable.
func (m *Model) VertexCounts() []int32 {
	return valueSlice[int32](m.core.csmGetDrawableVertexCounts(m.model), m.drawableCount)
}

// VertexPositions returns the deformed vertex positions of each drawable, in
// model units. The slices alias Core memory and are invalidated by Update.
func (m *Model) VertexPositions() [][]Vec2 {
	return jaggedSlice[Vec2](m.core.csmGetDrawableVertexPositions(m.model), m.VertexCounts())
}

// VertexUVs returns the texture coordinates of each drawable. They are
// constant for the lifetime of the model.
func (m *Model) VertexUVs() [][]Vec2 {
	return jaggedSlice[Vec2](m.core.csmGetDrawableVertexUvs(m.model), m.VertexCounts())
}

// Indices returns the triangle indices of each drawable.
func (m *Model) Indices() [][]uint16 {
	counts := valueSlice[int32](m.core.csmGetDrawableIndexCounts(m.model), m.drawableCount)
	return jaggedSlice[uint16](m.core.csmGetDrawableIndices(m.model), counts)
}

// Masks returns, for each drawable, the indices of the drawables that clip it.
func (m *Model) Masks() [][]int32 {
	counts := valueSlice[int32](m.core.csmGetDrawableMaskCounts(m.model), m.drawableCount)
	return jaggedSlice[int32](m.core.csmGetDrawableMasks(m.model), counts)
}

// MultiplyColors returns the per-drawable multiply colour, or nil when the
// Core does not expose it.
func (m *Model) MultiplyColors() []Vec4 {
	if m.core.csmGetDrawableMultiplyColors == nil {
		return nil
	}
	return valueSlice[Vec4](m.core.csmGetDrawableMultiplyColors(m.model), m.drawableCount)
}

// ScreenColors returns the per-drawable screen colour, or nil when the Core
// does not expose it.
func (m *Model) ScreenColors() []Vec4 {
	if m.core.csmGetDrawableScreenColors == nil {
		return nil
	}
	return valueSlice[Vec4](m.core.csmGetDrawableScreenColors(m.model), m.drawableCount)
}

// DrawableParents returns each drawable's owning part index, or nil when the
// Core does not expose it.
func (m *Model) DrawableParents() []int32 {
	if m.core.csmGetDrawableParentPartIndices == nil {
		return nil
	}
	return valueSlice[int32](m.core.csmGetDrawableParentPartIndices(m.model), m.drawableCount)
}

// Helpers over the C memory layout.

// ptr reinterprets a pointer value that came out of the Cubism Core as an
// unsafe.Pointer. The pointee is either Core-owned memory or one of the Go
// buffers Model keeps pinned for its whole lifetime, so this is not the
// uintptr round-trip that unsafe.Pointer's rules forbid.
func ptr(p uintptr) unsafe.Pointer {
	return *(*unsafe.Pointer)(unsafe.Pointer(&p))
}

// valueSlice views n consecutive T values starting at p.
func valueSlice[T any](p uintptr, n int) []T {
	if p == 0 || n <= 0 {
		return nil
	}
	return unsafe.Slice((*T)(ptr(p)), n)
}

// jaggedSlice views a `T**` as one slice per entry of counts.
func jaggedSlice[T any](p uintptr, counts []int32) [][]T {
	if p == 0 || len(counts) == 0 {
		return nil
	}
	ptrs := unsafe.Slice((*uintptr)(ptr(p)), len(counts))
	out := make([][]T, len(counts))
	for i, n := range counts {
		out[i] = valueSlice[T](ptrs[i], int(n))
	}
	return out
}

// stringSlice views a `const char**` as Go strings.
func stringSlice(p uintptr, n int) []string {
	if p == 0 || n <= 0 {
		return nil
	}
	ptrs := unsafe.Slice((*uintptr)(ptr(p)), n)
	out := make([]string, n)
	for i, q := range ptrs {
		out[i] = goString(q)
	}
	return out
}

// goString copies a NUL terminated C string.
func goString(p uintptr) string {
	if p == 0 {
		return ""
	}
	base := ptr(p)
	var n int
	for *(*byte)(unsafe.Add(base, n)) != 0 {
		n++
	}
	return string(unsafe.Slice((*byte)(base), n))
}

// alignedBuf allocates n bytes whose first element is aligned to align. The
// returned slice keeps the whole allocation reachable, so the alignment holds
// for as long as the slice is alive.
func alignedBuf(n, align int) []byte {
	buf := make([]byte, n+align)
	off := 0
	if rem := int(uintptr(unsafe.Pointer(&buf[0])) % uintptr(align)); rem != 0 {
		off = align - rem
	}
	return buf[off : off+n : off+n]
}

// alignedCopy copies src into a buffer aligned to align.
func alignedCopy(src []byte, align int) []byte {
	b := alignedBuf(len(src), align)
	copy(b, src)
	return b
}
