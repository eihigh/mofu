package main

import (
	"flag"
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/eihigh/mofu/mofufmt"
)

func infoCmd(args []string) error {
	fs := flag.NewFlagSet("info", flag.ExitOnError)
	verbose := fs.Bool("v", false, "list every mesh")
	rest, err := parseFlags(fs, args)
	if err != nil {
		return err
	}
	if len(rest) != 1 {
		return fmt.Errorf("info: expected exactly one .mofu file")
	}
	f, err := os.Open(rest[0])
	if err != nil {
		return err
	}
	defer f.Close()
	file, err := mofufmt.Decode(f)
	if err != nil {
		return err
	}

	c := file.Canvas
	fmt.Printf("canvas    %.0fx%.0f px, origin (%.1f, %.1f), %.1f px/unit\n",
		c.Width, c.Height, c.OriginX, c.OriginY, c.PixelsPerUnit)

	var verts, tris, masked int
	for i := range file.Meshes {
		m := &file.Meshes[i]
		verts += m.VertexCount()
		tris += len(m.Indices) / 3
		if len(m.Masks) > 0 {
			masked++
		}
	}
	fmt.Printf("meshes    %d (%d vertices, %d triangles, %d clipped)\n", len(file.Meshes), verts, tris, masked)

	fmt.Printf("textures  %d\n", len(file.Textures))
	for _, t := range file.Textures {
		fmt.Printf("          %s (%s)\n", t.Name, humanBytes(int64(len(t.Data))))
	}

	fmt.Printf("animations %d\n", len(file.Animations))
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "  name\tframes\tfps\tseconds\tloop\tmoving meshes")
	for i := range file.Animations {
		a := &file.Animations[i]
		moving := 0
		for j := range a.Tracks {
			if a.Tracks[j].Flags.Has(mofufmt.TrackPositionsAnimated) {
				moving++
			}
		}
		fmt.Fprintf(w, "  %s\t%d\t%g\t%.2f\t%v\t%d/%d\n",
			a.Name, a.FrameCount, a.FPS, a.Duration(), a.Loop, moving, len(a.Tracks))
	}
	w.Flush()

	if *verbose {
		fmt.Println("\nmeshes:")
		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "  #\tid\ttex\tverts\ttris\tmasks\tflags")
		for i := range file.Meshes {
			m := &file.Meshes[i]
			fmt.Fprintf(w, "  %d\t%s\t%d\t%d\t%d\t%d\t%s\n",
				i, m.ID, m.TextureIndex, m.VertexCount(), len(m.Indices)/3, len(m.Masks), flagNames(m.Flags))
		}
		w.Flush()
	}
	return nil
}

func flagNames(f mofufmt.MeshFlags) string {
	s := ""
	for _, e := range []struct {
		f mofufmt.MeshFlags
		n string
	}{
		{mofufmt.MeshBlendAdditive, "add"},
		{mofufmt.MeshBlendMultiplicative, "mul"},
		{mofufmt.MeshDoubleSided, "double"},
		{mofufmt.MeshInvertedMask, "invmask"},
	} {
		if f.Has(e.f) {
			if s != "" {
				s += ","
			}
			s += e.n
		}
	}
	if s == "" {
		return "-"
	}
	return s
}
