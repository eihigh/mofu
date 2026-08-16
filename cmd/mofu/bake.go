package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/eihigh/mofu/internal/bake"
	"github.com/eihigh/mofu/internal/core"
	"github.com/eihigh/mofu/mofufmt"
)

func bakeCmd(args []string) error {
	fs := flag.NewFlagSet("bake", flag.ExitOnError)
	out := fs.String("o", "", "output path (default: alongside the input, with a .mofu extension)")
	corePath := fs.String("core", "", "path to the Cubism Core library (default: autodetect)")
	fps := fs.Float64("fps", 0, "sampling rate; 0 keeps each motion's own Meta.Fps")
	raw := fs.Bool("raw", false, "skip gzip compression of the body")
	quiet := fs.Bool("q", false, "only report errors")
	var motions stringList
	fs.Var(&motions, "motion", "extra .motion3.json to bake; repeatable")
	rest, err := parseFlags(fs, args)
	if err != nil {
		return err
	}
	if len(rest) != 1 {
		return fmt.Errorf("bake: expected exactly one .model3.json, got %d arguments", len(rest))
	}
	in := rest[0]
	dst := *out
	if dst == "" {
		dst = defaultOutput(in, ".mofu")
	}

	opts := bake.Options{
		CorePath:     *corePath,
		FPS:          *fps,
		Motions:      motions,
		Uncompressed: *raw,
	}
	if !*quiet {
		opts.Logf = func(format string, args ...any) {
			fmt.Fprintf(os.Stderr, format+"\n", args...)
		}
	}

	res, err := bake.Run(in, opts)
	if err != nil {
		return err
	}

	f, err := os.Create(dst)
	if err != nil {
		return err
	}
	if err := mofufmt.Encode(f, res.File, mofufmt.EncodeOptions{Uncompressed: *raw}); err != nil {
		f.Close()
		os.Remove(dst)
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}

	for _, w := range res.Warnings {
		fmt.Fprintln(os.Stderr, "warning:", w)
	}
	if !*quiet {
		st, err := os.Stat(dst)
		if err != nil {
			return err
		}
		fmt.Fprintf(os.Stderr, "wrote %s (%s): %d meshes, %d textures, %d animations\n",
			dst, humanBytes(st.Size()), len(res.File.Meshes), len(res.File.Textures), len(res.File.Animations))
	}
	return nil
}

func coreCmd(args []string) error {
	fs := flag.NewFlagSet("core", flag.ExitOnError)
	corePath := fs.String("core", "", "path to the Cubism Core library (default: autodetect)")
	if _, err := parseFlags(fs, args); err != nil {
		return err
	}
	c, err := core.Load(*corePath)
	if err != nil {
		return err
	}
	defer c.Close()
	fmt.Printf("library:            %s\n", c.Path())
	fmt.Printf("version:            %s\n", c.Version())
	fmt.Printf("latest moc version: %d\n", c.LatestMocVersion())
	return nil
}

func humanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for v := n / unit; v >= unit; v /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGT"[exp])
}
