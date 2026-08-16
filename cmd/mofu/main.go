// Command mofu converts Live2D Cubism model exports into .mofu files and
// inspects the result.
//
// Baking needs the proprietary Live2D Cubism Core dynamic library, which is
// not redistributable and is therefore not part of this repository. Download
// the Cubism SDK for Native from live2d.com and drop the library for your
// platform next to this executable:
//
//	Linux    libLive2DCubismCore.so
//	macOS    libLive2DCubismCore.dylib
//	Windows  Live2DCubismCore.dll
//
// or point MOFU_CUBISM_CORE at it. Playback never needs the library.
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "mofu:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		usage()
		return nil
	}
	switch args[0] {
	case "bake":
		return bakeCmd(args[1:])
	case "info":
		return infoCmd(args[1:])
	case "core":
		return coreCmd(args[1:])
	case "help", "-h", "--help":
		usage()
		return nil
	default:
		usage()
		return fmt.Errorf("unknown command %q", args[0])
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `mofu converts Live2D Cubism exports into a self-contained playback format.

usage:
  mofu bake <model.model3.json> [flags]   convert a model into a .mofu file
  mofu info <model.mofu>                  describe a baked file
  mofu core                               report the Cubism Core in use

Use the separate mofu-play command to preview a baked file in a window;
it is kept apart so that this tool stays runnable without a display.

Baking requires the Live2D Cubism Core dynamic library next to this
executable, or at $MOFU_CUBISM_CORE. Playback requires nothing.
`)
}

// parseFlags parses args, tolerating flags that appear after the positional
// arguments. Go's flag package stops at the first non-flag word, which makes
// the natural `mofu bake model.model3.json -o out.mofu` fail; this keeps
// parsing after each positional instead.
func parseFlags(fs *flag.FlagSet, args []string) ([]string, error) {
	var positional []string
	for {
		if err := fs.Parse(args); err != nil {
			return nil, err
		}
		if fs.NArg() == 0 {
			return positional, nil
		}
		positional = append(positional, fs.Arg(0))
		args = fs.Args()[1:]
	}
}

// stringList collects a repeatable flag.
type stringList []string

func (s *stringList) String() string     { return strings.Join(*s, ",") }
func (s *stringList) Set(v string) error { *s = append(*s, v); return nil }

func defaultOutput(in, ext string) string {
	base := filepath.Base(in)
	base = strings.TrimSuffix(base, ".json")
	base = strings.TrimSuffix(base, ".model3")
	return filepath.Join(filepath.Dir(in), base+ext)
}
