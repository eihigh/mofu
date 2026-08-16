package mofu

import (
	"math/rand"
	"testing"

	"github.com/eihigh/mofu/mofufmt"
	"github.com/hajimehoshi/ebiten/v2"
)

// benchFile builds a synthetic model in the shape of a real Live2D export:
// many meshes, most channels animated, one overlay.
func benchFile(meshCount, vertexCount, frames int) *mofufmt.File {
	rng := rand.New(rand.NewSource(1))
	f := &mofufmt.File{
		Canvas: mofufmt.Canvas{Width: 2048, Height: 2048, OriginX: 1024, OriginY: 1024, PixelsPerUnit: 1024},
	}
	for m := 0; m < meshCount; m++ {
		mesh := mofufmt.Mesh{
			ID:   "mesh",
			UVs:  make([]float32, vertexCount*2),
			MinX: -1, MinY: -1, MaxX: 1, MaxY: 1,
		}
		for t := 0; t+2 < vertexCount; t++ {
			mesh.Indices = append(mesh.Indices, uint16(t), uint16(t+1), uint16(t+2))
		}
		f.Meshes = append(f.Meshes, mesh)
	}
	anim := func(name string) mofufmt.Animation {
		a := mofufmt.Animation{Name: name, FPS: 30, FrameCount: int32(frames), Loop: true}
		if name != "a" {
			a.FadeIn = 1
		}
		for m := 0; m < meshCount; m++ {
			tr := mofufmt.Track{
				Flags:     mofufmt.TrackPositionsAnimated | mofufmt.TrackOpacityAnimated,
				Positions: make([]uint16, frames*vertexCount*2),
				Opacity:   make([]float32, frames),
				Order:     []int32{int32(m)},
				Visible:   []uint8{1},
			}
			for i := range tr.Positions {
				tr.Positions[i] = uint16(rng.Intn(65536))
			}
			for i := range tr.Opacity {
				tr.Opacity[i] = rng.Float32()
			}
			a.Tracks = append(a.Tracks, tr)
		}
		return a
	}
	f.Animations = []mofufmt.Animation{anim("a"), anim("b")}

	ov := mofufmt.Overlay{Name: "exp", Tracks: make([]mofufmt.OverlayTrack, meshCount)}
	for m := range ov.Tracks {
		ov.Tracks[m].DeltaPositions = make([]float32, vertexCount*2)
	}
	f.Overlays = []mofufmt.Overlay{ov}
	return f
}

// BenchmarkFrameCPU measures the whole per-frame CPU side of Draw short of
// the GPU calls: advancing, sampling both fade heads, blending, overlays,
// transforming and sorting. 64 meshes x 96 vertices approximates a mid-size
// Live2D model.
func BenchmarkFrameCPU(b *testing.B) {
	m := newModelCommon(benchFile(64, 96, 60))
	p := m.NewPlayer()
	if err := p.SetOverlay("exp", 0.5); err != nil {
		b.Fatal(err)
	}
	// Keep a crossfade permanently active: worst case samples two poses.
	if err := p.Play("b"); err != nil {
		b.Fatal(err)
	}
	var g ebiten.GeoM
	g.Scale(0.5, 0.5)
	g.Translate(100, 50)
	var cs ebiten.ColorScale
	cs.ScaleAlpha(0.9)

	b.ReportAllocs()
	for b.Loop() {
		p.Advance(1.0 / 240) // small steps keep the 1s fade alive
		if p.fadeElapsed > 0.9 {
			p.fadeElapsed = 0
		}
		p.samplePose(&p.cur, &p.pose)
		p.samplePose(p.prev, &p.scratch)
		mixPose(&p.pose, &p.scratch, 0.5)
		p.applyOverlays()
		p.applyColorScale(&cs)
		p.buildVerts(&g)
		p.sortOrder()
	}
}

// BenchmarkSamplePose isolates the innermost cost: one pose sample.
func BenchmarkSamplePose(b *testing.B) {
	m := newModelCommon(benchFile(64, 96, 60))
	p := m.NewPlayer()
	b.ReportAllocs()
	for b.Loop() {
		p.cur.time += 1.0 / 240
		p.samplePose(&p.cur, &p.pose)
	}
}
