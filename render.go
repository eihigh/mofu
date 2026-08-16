package mofu

import (
	"github.com/eihigh/mofu/mofufmt"
	"github.com/hajimehoshi/ebiten/v2"
)

// render draws every visible mesh in render order.
func (p *Player) render(dst *ebiten.Image) {
	for _, i := range p.order {
		s := &p.pose.states[i]
		if !s.visible || s.opacity <= 0 {
			continue
		}
		mesh := &p.model.file.Meshes[i]
		if len(mesh.Masks) == 0 {
			p.drawMesh(dst, i, blendFor(mesh.Flags))
			continue
		}
		p.drawMasked(dst, i)
	}
}

// drawMesh writes one mesh straight into dst.
func (p *Player) drawMesh(dst *ebiten.Image, i int, blend ebiten.Blend) {
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
	op := &ebiten.DrawTrianglesShaderOptions{Blend: blend}
	op.Images[0] = tex
	dst.DrawTrianglesShader(verts, mesh.Indices, p.model.shader, op)
}

// drawMasked renders a clipped mesh.
//
// Ebitengine has no second sampler to reach for here, so the clip is applied
// with blend factors instead: the mesh is rendered into a scratch buffer, the
// mask buffer is drawn over it with dst = dst*srcAlpha, and the result is
// composited into dst with the mesh's own blend mode. Fully transparent
// pixels are a no-op under all three Cubism blend modes, so compositing the
// whole rectangle is safe.
func (p *Player) drawMasked(dst *ebiten.Image, i int) {
	mesh := &p.model.file.Meshes[i]
	p.ensureBuffers(dst)

	p.maskBuf.Clear()
	for _, mi := range mesh.Masks {
		if mi < 0 || int(mi) >= len(p.model.file.Meshes) {
			continue
		}
		p.drawMaskShape(p.maskBuf, int(mi))
	}

	p.partBuf.Clear()
	p.drawMesh(p.partBuf, i, blendNormal)

	clip := blendMaskApply
	if mesh.Flags.Has(mofufmt.MeshInvertedMask) {
		clip = blendMaskApplyInverted
	}
	op := &ebiten.DrawImageOptions{Blend: clip}
	p.partBuf.DrawImage(p.maskBuf, op)

	out := &ebiten.DrawImageOptions{Blend: blendFor(mesh.Flags)}
	dst.DrawImage(p.partBuf, out)
}

// drawMaskShape renders a mask drawable's silhouette. Only its alpha matters,
// and its own opacity and colours are deliberately ignored: a mask defines a
// region, not an appearance.
func (p *Player) drawMaskShape(dst *ebiten.Image, i int) {
	mesh := &p.model.file.Meshes[i]
	tex := p.model.texture(mesh.TextureIndex)
	if tex == nil {
		return
	}
	verts := p.verts[i]
	for v := range verts {
		verts[v].ColorR = 1
		verts[v].ColorG = 1
		verts[v].ColorB = 1
		verts[v].ColorA = 1
		verts[v].Custom0 = 0
		verts[v].Custom1 = 0
		verts[v].Custom2 = 0
	}
	op := &ebiten.DrawTrianglesShaderOptions{Blend: blendNormal}
	op.Images[0] = tex
	dst.DrawTrianglesShader(verts, mesh.Indices, p.model.shader, op)
}

// ensureBuffers (re)allocates the scratch buffers to match dst.
func (p *Player) ensureBuffers(dst *ebiten.Image) {
	b := dst.Bounds()
	w, h := b.Dx(), b.Dy()
	if p.maskBuf != nil && p.maskBuf.Bounds().Dx() == w && p.maskBuf.Bounds().Dy() == h {
		return
	}
	if p.maskBuf != nil {
		p.maskBuf.Deallocate()
		p.partBuf.Deallocate()
	}
	p.maskBuf = ebiten.NewImage(w, h)
	p.partBuf = ebiten.NewImage(w, h)
}

// Dispose releases the player's scratch buffers.
func (p *Player) Dispose() {
	if p.maskBuf != nil {
		p.maskBuf.Deallocate()
		p.partBuf.Deallocate()
		p.maskBuf, p.partBuf = nil, nil
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
