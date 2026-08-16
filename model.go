// Package mofu plays back baked Live2D models with Ebitengine.
//
// A .mofu file is produced by the `mofu` command from a Cubism model export.
// Every deformation has already been evaluated at bake time, so playing one
// back is ordinary keyframed mesh animation: interpolate vertex positions
// between two frames, sort by render order, draw. No Cubism Core is involved
// and nothing proprietary is linked in.
package mofu

import (
	"bytes"
	_ "embed"
	"fmt"
	"image"
	_ "image/png"
	"io"
	"os"
	"sort"

	"github.com/eihigh/mofu/mofufmt"
	"github.com/hajimehoshi/ebiten/v2"
)

//go:embed shader.kage
var shaderSrc []byte

// Model is a loaded .mofu file: textures, meshes and animations. It is
// immutable once loaded and can back any number of Players.
type Model struct {
	file     *mofufmt.File
	textures []*ebiten.Image
	shader   *ebiten.Shader

	// templates hold the parts of each mesh's vertices that never change,
	// namely the texture coordinates. Players copy from these.
	templates [][]ebiten.Vertex

	byName    map[string]int
	byOverlay map[string]int
	// overlayDeltas is the overlays' position deltas converted to canvas
	// pixels, indexed [overlay][mesh]; a nil inner slice means no movement.
	overlayDeltas [][][]float32
}

// LoadFile reads a .mofu file from disk.
func LoadFile(path string) (*Model, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return Load(f)
}

// Load reads a .mofu file from r.
func Load(r io.Reader) (*Model, error) {
	f, err := mofufmt.Decode(r)
	if err != nil {
		return nil, err
	}
	return New(f)
}

// New builds a playable model from already-decoded data.
func New(f *mofufmt.File) (*Model, error) {
	m := newModelCommon(f)

	shader, err := ebiten.NewShader(shaderSrc)
	if err != nil {
		return nil, fmt.Errorf("mofu: compiling shader: %w", err)
	}
	m.shader = shader

	m.textures = make([]*ebiten.Image, len(f.Textures))
	for i, t := range f.Textures {
		img, _, err := image.Decode(bytes.NewReader(t.Data))
		if err != nil {
			return nil, fmt.Errorf("mofu: texture %q: %w", t.Name, err)
		}
		m.textures[i] = ebiten.NewImageFromImage(img)
	}

	m.templates = make([][]ebiten.Vertex, len(f.Meshes))
	for i := range f.Meshes {
		mesh := &f.Meshes[i]
		tex := m.texture(mesh.TextureIndex)
		var tw, th float32 = 1, 1
		if tex != nil {
			b := tex.Bounds()
			tw, th = float32(b.Dx()), float32(b.Dy())
		}
		verts := make([]ebiten.Vertex, mesh.VertexCount())
		for v := range verts {
			// Cubism UVs have their origin at the bottom left.
			verts[v].SrcX = mesh.UVs[v*2] * tw
			verts[v].SrcY = (1 - mesh.UVs[v*2+1]) * th
		}
		m.templates[i] = verts
	}

	return m, nil
}

// newModelCommon builds the GPU-free part of a Model: the lookup tables and
// the overlay deltas in canvas space. Tests use it to exercise playback
// without a graphics context.
func newModelCommon(f *mofufmt.File) *Model {
	m := &Model{file: f}
	m.byName = make(map[string]int, len(f.Animations))
	for i := range f.Animations {
		m.byName[f.Animations[i].Name] = i
	}
	m.byOverlay = make(map[string]int, len(f.Overlays))
	ppu := f.Canvas.PixelsPerUnit
	if ppu == 0 {
		ppu = 1
	}
	m.overlayDeltas = make([][][]float32, len(f.Overlays))
	for oi := range f.Overlays {
		m.byOverlay[f.Overlays[oi].Name] = oi
		tracks := f.Overlays[oi].Tracks
		deltas := make([][]float32, len(f.Meshes))
		for mi := range f.Meshes {
			if mi >= len(tracks) || tracks[mi].DeltaPositions == nil {
				continue
			}
			src := tracks[mi].DeltaPositions
			d := make([]float32, len(src))
			for v := 0; v+1 < len(src); v += 2 {
				// Model units to canvas pixels; canvas Y points down.
				d[v] = src[v] * ppu
				d[v+1] = -src[v+1] * ppu
			}
			deltas[mi] = d
		}
		m.overlayDeltas[oi] = deltas
	}
	return m
}

func (m *Model) texture(i int32) *ebiten.Image {
	if i < 0 || int(i) >= len(m.textures) {
		return nil
	}
	return m.textures[i]
}

// CanvasSize is the model's canvas in pixels. Vertex positions produced by
// Player.Draw live in a rectangle of this size with its origin at the top
// left, so this is the box to scale and centre with DrawOptions.GeoM.
func (m *Model) CanvasSize() (w, h float64) {
	return float64(m.file.Canvas.Width), float64(m.file.Canvas.Height)
}

// AnimationNames returns every animation in the file, sorted.
func (m *Model) AnimationNames() []string {
	names := make([]string, 0, len(m.file.Animations))
	for i := range m.file.Animations {
		names = append(names, m.file.Animations[i].Name)
	}
	sort.Strings(names)
	return names
}

// OverlayNames returns every overlay (baked expression), sorted.
func (m *Model) OverlayNames() []string {
	names := make([]string, 0, len(m.file.Overlays))
	for i := range m.file.Overlays {
		names = append(names, m.file.Overlays[i].Name)
	}
	sort.Strings(names)
	return names
}

// HitAreaNames returns every hit area, in file order.
func (m *Model) HitAreaNames() []string {
	names := make([]string, 0, len(m.file.HitAreas))
	for _, h := range m.file.HitAreas {
		names = append(names, h.Name)
	}
	return names
}

// Animation looks an animation up by name.
func (m *Model) Animation(name string) (*mofufmt.Animation, bool) {
	i, ok := m.byName[name]
	if !ok {
		return nil, false
	}
	return &m.file.Animations[i], true
}

// File exposes the decoded container, for tools that want to inspect it.
func (m *Model) File() *mofufmt.File { return m.file }

// Dispose releases the GPU resources held by the model.
func (m *Model) Dispose() {
	for _, t := range m.textures {
		if t != nil {
			t.Deallocate()
		}
	}
	m.textures = nil
}
