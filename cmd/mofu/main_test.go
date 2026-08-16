package main

import (
	"bytes"
	"flag"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eihigh/mofu/internal/fakecore"
)

// writeModel lays out a minimal but complete Cubism export for the stub core.
func writeModel(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	write := func(rel, content string) {
		p := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("m.model3.json", `{
	  "Version": 3,
	  "FileReferences": {
	    "Moc": "m.moc3",
	    "Textures": ["tex.png"],
	    "Motions": {"Idle": [{"File": "idle.motion3.json"}]}
	  },
	  "HitAreas": [{"Id": "body", "Name": "Body"}]
	}`)
	write("m.moc3", "fake moc3 payload")
	write("tex.png", "fake texture bytes")
	write("idle.motion3.json", `{
	  "Version": 3,
	  "Meta": {"Duration": 0.5, "Fps": 30, "Loop": true, "AreBeziersRestricted": true},
	  "Curves": [{"Target": "Parameter", "Id": "ParamAngleX", "Segments": [0, 0, 0, 0.5, 30]}]
	}`)
	return filepath.Join(dir, "m.model3.json")
}

// capture runs fn with os.Stdout redirected and returns what it printed.
func capture(t *testing.T, fn func() error) (string, error) {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	old := os.Stdout
	os.Stdout = w
	defer func() { os.Stdout = old }()
	fnErr := fn()
	w.Close()
	var buf bytes.Buffer
	if _, err := io.Copy(&buf, r); err != nil {
		t.Fatal(err)
	}
	return buf.String(), fnErr
}

func TestBakeAndInfo(t *testing.T) {
	lib := fakecore.Build(t)
	model := writeModel(t)
	out := filepath.Join(t.TempDir(), "m.mofu")

	// Flags after the positional argument must work.
	if err := run([]string{"bake", model, "-o", out, "-core", lib, "-q"}); err != nil {
		t.Fatalf("bake: %v", err)
	}
	if st, err := os.Stat(out); err != nil || st.Size() == 0 {
		t.Fatalf("bake produced nothing: %v", err)
	}

	got, err := capture(t, func() error { return run([]string{"info", out, "-v"}) })
	if err != nil {
		t.Fatalf("info: %v", err)
	}
	for _, want := range []string{"canvas", "@rest", "Idle", "Body -> mesh 0", "body"} {
		if !strings.Contains(got, want) {
			t.Errorf("info output lacks %q:\n%s", want, got)
		}
	}
}

func TestBakeRefusesToClobberOnError(t *testing.T) {
	// A bake that fails must not leave a partial output file behind.
	out := filepath.Join(t.TempDir(), "x.mofu")
	err := run([]string{"bake", "/no/such/model3.json", "-o", out, "-core", fakecore.Build(t)})
	if err == nil {
		t.Fatal("bake of a missing model succeeded")
	}
	if _, statErr := os.Stat(out); statErr == nil {
		t.Error("a failed bake left an output file")
	}
}

func TestCoreCommand(t *testing.T) {
	got, err := capture(t, func() error {
		return run([]string{"core", "-core", fakecore.Build(t)})
	})
	if err != nil {
		t.Fatalf("core: %v", err)
	}
	if !strings.Contains(got, "5.0.0") {
		t.Errorf("core output lacks the stub's version:\n%s", got)
	}
}

func TestInfoRejectsGarbage(t *testing.T) {
	p := filepath.Join(t.TempDir(), "junk.mofu")
	if err := os.WriteFile(p, []byte("this is not a mofu file"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := run([]string{"info", p}); err == nil {
		t.Error("info accepted garbage")
	}
}

func TestUnknownCommand(t *testing.T) {
	if err := run([]string{"frobnicate"}); err == nil {
		t.Error("an unknown command did not error")
	}
}

func TestParseFlagsAfterPositional(t *testing.T) {
	fs := flag.NewFlagSet("t", flag.ContinueOnError)
	v := fs.String("v", "", "")
	rest, err := parseFlags(fs, []string{"first", "-v", "x", "second"})
	if err != nil {
		t.Fatal(err)
	}
	if *v != "x" || len(rest) != 2 || rest[0] != "first" || rest[1] != "second" {
		t.Errorf("parseFlags: v=%q rest=%v", *v, rest)
	}
}

func TestDefaultOutput(t *testing.T) {
	for in, want := range map[string]string{
		"dir/hiyori.model3.json": filepath.FromSlash("dir/hiyori.mofu"),
		"plain.json":             "plain.mofu",
	} {
		if got := defaultOutput(in, ".mofu"); got != want {
			t.Errorf("defaultOutput(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestHumanBytes(t *testing.T) {
	for n, want := range map[int64]string{
		512:     "512 B",
		2048:    "2.0 KiB",
		1 << 20: "1.0 MiB",
	} {
		if got := humanBytes(n); got != want {
			t.Errorf("humanBytes(%d) = %q, want %q", n, got, want)
		}
	}
}
