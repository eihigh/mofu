package mofu

import (
	"github.com/eihigh/mofu/mofufmt"
	"github.com/hajimehoshi/ebiten/v2"
)

// Clipping masks are rendered the way the official Cubism renderers do it:
// each unique mask combination is drawn once per frame into a small
// off-screen buffer, and clipped meshes then sample that buffer from inside
// the fragment shader, so a clipped mesh costs one draw call like any other.
// The buffer covers the combination's canvas-space bounding box of that
// frame, so its resolution goes where the mask actually is.
const (
	// maskBufferSize is the resolution of one mask buffer. Masks are smooth
	// silhouettes, so a modest fixed resolution sampled bilinearly holds up
	// well past 1:1 zoom.
	maskBufferSize = 512
	// maskPad keeps one empty texel around the silhouette so bilinear
	// sampling fades cleanly to zero at the buffer's edge.
	maskPad = 1
)

// render draws every visible mesh in render order. The pose and the vertex
// buffers must be built, and prepareFrame must have run for this Draw.
func (p *Player) render(dst *ebiten.Image) {
	for _, i := range p.order {
		s := &p.pose.states[i]
		if !s.visible || s.opacity <= 0 {
			continue
		}
		mesh := &p.model.file.Meshes[i]
		gi := p.model.maskGroupOf[i]
		if gi < 0 || !p.maskUsable {
			p.drawMesh(dst, i, nil, nil)
			continue
		}
		p.ensureMask(gi)
		inverted := mesh.Flags.Has(mofufmt.MeshInvertedMask)
		if p.maskEmpty[gi] {
			// No mask coverage anywhere: a normal mask clips the mesh out
			// entirely, an inverted one leaves it untouched.
			if inverted {
				p.drawMesh(dst, i, nil, nil)
			}
			continue
		}
		p.drawMesh(dst, i, p.maskBufs[gi], p.maskUniforms(gi, inverted))
	}
}

// drawMesh issues one mesh as a single shader draw, clipped by mask when one
// is given.
func (p *Player) drawMesh(dst *ebiten.Image, i int, mask *ebiten.Image, uniforms map[string]any) {
	mesh := &p.model.file.Meshes[i]
	tex := p.model.texture(mesh.TextureIndex)
	if tex == nil {
		return
	}
	s := &p.pose.states[i]
	verts := p.verts[i]
	for v := range verts {
		verts[v].ColorR = s.multiply[0]
		verts[v].ColorG = s.multiply[1]
		verts[v].ColorB = s.multiply[2]
		verts[v].ColorA = s.opacity
		verts[v].Custom0 = s.screen[0]
		verts[v].Custom1 = s.screen[1]
		verts[v].Custom2 = s.screen[2]
	}
	op := &ebiten.DrawTrianglesShaderOptions{Blend: blendFor(mesh.Flags)}
	op.Images[0] = tex
	shader := p.model.shader
	if mask != nil {
		op.Images[1] = mask
		op.Uniforms = uniforms
		shader = p.model.maskShader
	}
	dst.DrawTrianglesShader(verts, mesh.Indices, shader, op)
}

// maskUniforms returns the mask shader's uniforms for one combination. The
// maps and the slices inside them are cached and mutated in place each frame;
// that is safe because DrawTrianglesShader copies uniform values out at call
// time.
func (p *Player) maskUniforms(gi int, inverted bool) map[string]any {
	key := gi << 1
	var inv float32
	if inverted {
		key |= 1
		inv = 1
	}
	u, ok := p.uniforms[key]
	if !ok {
		u = map[string]any{
			"InvGeoM":      p.invGeoM,
			"MaskRect":     p.maskRects[gi],
			"MaskInverted": inv,
		}
		p.uniforms[key] = u
	}
	return u
}

// prepareFrame resets the per-Draw mask state and captures the inverse of the
// caller's GeoM, which the mask shader uses to map fragments back to canvas
// space.
func (p *Player) prepareFrame(geom *ebiten.GeoM) {
	if len(p.model.maskGroups) == 0 {
		p.maskUsable = false
		return
	}
	if p.maskReady == nil {
		n := len(p.model.maskGroups)
		p.maskReady = make([]bool, n)
		p.maskEmpty = make([]bool, n)
		p.invGeoM = make([]float32, 6)
		p.maskRects = make([][]float32, n)
		for i := range p.maskRects {
			p.maskRects[i] = make([]float32, 4)
		}
		p.maskBufs = make([]*ebiten.Image, n)
		p.uniforms = make(map[int]map[string]any)
	}
	for i := range p.maskReady {
		p.maskReady[i] = false
	}
	g := *geom
	p.maskUsable = g.IsInvertible()
	if !p.maskUsable {
		// A degenerate transform collapses the model anyway; draw whatever
		// remains unclipped rather than guessing at a mask mapping.
		return
	}
	g.Invert()
	p.invGeoM[0] = float32(g.Element(0, 0))
	p.invGeoM[1] = float32(g.Element(0, 1))
	p.invGeoM[2] = float32(g.Element(0, 2))
	p.invGeoM[3] = float32(g.Element(1, 0))
	p.invGeoM[4] = float32(g.Element(1, 1))
	p.invGeoM[5] = float32(g.Element(1, 2))
}

// ensureMask renders a mask combination's silhouette for this frame, once.
func (p *Player) ensureMask(gi int) {
	if p.maskReady[gi] {
		return
	}
	p.maskReady[gi] = true

	group := p.model.maskGroups[gi]
	rect, ok := maskBounds(&p.pose, group)
	p.maskEmpty[gi] = !ok
	if !ok {
		return
	}
	if p.maskBufs[gi] == nil {
		p.maskBufs[gi] = ebiten.NewImage(maskBufferSize, maskBufferSize)
	}
	buf := p.maskBufs[gi]
	buf.Clear()

	// Map the canvas-space rect onto the buffer, leaving maskPad empty
	// texels, and remember the canvas rect the whole buffer corresponds to.
	const inner = maskBufferSize - 2*maskPad
	sx := inner / (rect[2] - rect[0])
	sy := inner / (rect[3] - rect[1])
	r := p.maskRects[gi]
	r[0] = rect[0] - maskPad/sx
	r[1] = rect[1] - maskPad/sy
	r[2] = rect[2] + maskPad/sx
	r[3] = rect[3] + maskPad/sy

	for _, mi := range group {
		mesh := &p.model.file.Meshes[mi]
		tex := p.model.texture(mesh.TextureIndex)
		if tex == nil {
			continue
		}
		pos := p.pose.positions[mi]
		src := p.verts[mi]
		if cap(p.maskVerts) < len(src) {
			p.maskVerts = make([]ebiten.Vertex, len(src))
		}
		verts := p.maskVerts[:len(src)]
		for v := range verts {
			verts[v] = ebiten.Vertex{
				DstX:   (pos[v*2]-rect[0])*sx + maskPad,
				DstY:   (pos[v*2+1]-rect[1])*sy + maskPad,
				SrcX:   src[v].SrcX,
				SrcY:   src[v].SrcY,
				ColorR: 1, ColorG: 1, ColorB: 1, ColorA: 1,
			}
		}
		// A mask is a silhouette: only the texture's alpha matters, and the
		// mask mesh's own opacity and visibility are ignored, as in the
		// official renderer (mask sources are usually invisible drawables).
		buf.DrawTriangles(verts, mesh.Indices, tex, &ebiten.DrawTrianglesOptions{})
	}
}

// maskBounds is the canvas-space bounding box of a mask combination's
// current silhouette. ok is false when the box is degenerate, meaning the
// mask covers nothing.
func maskBounds(pose *pose, group []int32) (rect [4]float32, ok bool) {
	first := true
	for _, mi := range group {
		for v, x := range pose.positions[mi] {
			if v%2 == 0 {
				if first || x < rect[0] {
					rect[0] = x
				}
				if first || x > rect[2] {
					rect[2] = x
				}
			} else {
				if first || x < rect[1] {
					rect[1] = x
				}
				if first || x > rect[3] {
					rect[3] = x
				}
				first = false
			}
		}
	}
	if first || rect[2]-rect[0] <= 0 || rect[3]-rect[1] <= 0 {
		return rect, false
	}
	return rect, true
}

// Dispose releases the player's mask buffers.
func (p *Player) Dispose() {
	for i, b := range p.maskBufs {
		if b != nil {
			b.Deallocate()
			p.maskBufs[i] = nil
		}
	}
}

func blendFor(f mofufmt.MeshFlags) ebiten.Blend {
	switch {
	case f.Has(mofufmt.MeshBlendAdditive):
		return blendAdditive
	case f.Has(mofufmt.MeshBlendMultiplicative):
		return blendMultiplicative
	}
	return blendNormal
}
