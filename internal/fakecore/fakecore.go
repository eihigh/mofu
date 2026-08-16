// Package fakecore builds a stand-in for the proprietary Live2D Cubism Core.
//
// The real library cannot be redistributed, so the binding and the bake
// pipeline are exercised against a small C stub that implements the same
// entry points over a hard-coded model. The stub is compiled on demand; tests
// that need it skip when no C compiler is available.
//
// The stub source is carried as fakecore.c.txt rather than fakecore.c so that
// the Go tool does not mistake this for a cgo package.
package fakecore

import (
	_ "embed"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
)

//go:embed fakecore.c.txt
var source []byte

var (
	once sync.Once
	path string
	err  error
)

// Build compiles the stub once per process and returns the path to the shared
// library. The test is skipped if the platform or toolchain cannot build one.
func Build(t *testing.T) string {
	t.Helper()
	switch runtime.GOOS {
	case "linux", "darwin", "freebsd":
	default:
		t.Skipf("fakecore: not supported on %s", runtime.GOOS)
	}
	cc, lookErr := exec.LookPath("cc")
	if lookErr != nil {
		t.Skip("fakecore: no C compiler available")
	}
	once.Do(func() { path, err = build(cc) })
	if err != nil {
		t.Fatalf("fakecore: %v", err)
	}
	return path
}

func build(cc string) (string, error) {
	dir, err := os.MkdirTemp("", "mofu-fakecore")
	if err != nil {
		return "", err
	}
	src := filepath.Join(dir, "fakecore.c")
	if err := os.WriteFile(src, source, 0o644); err != nil {
		return "", err
	}
	name := "libLive2DCubismCore.so"
	if runtime.GOOS == "darwin" {
		name = "libLive2DCubismCore.dylib"
	}
	out := filepath.Join(dir, name)
	cmd := exec.Command(cc, "-shared", "-fPIC", "-O1", "-o", out, src)
	if b, err := cmd.CombinedOutput(); err != nil {
		return "", &buildError{output: string(b), err: err}
	}
	return out, nil
}

type buildError struct {
	output string
	err    error
}

func (e *buildError) Error() string { return "compiling the stub: " + e.err.Error() + "\n" + e.output }
func (e *buildError) Unwrap() error { return e.err }
