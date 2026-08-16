//go:build darwin || linux || freebsd || netbsd

package core

import (
	"fmt"
	"runtime"

	"github.com/ebitengine/purego"
)

// defaultLibNames lists the file names the official Cubism Core distribution
// uses on this platform, most likely first: the first entry is also what Find
// falls back to when nothing is found on disk.
func defaultLibNames() []string {
	if runtime.GOOS == "darwin" {
		return []string{"libLive2DCubismCore.dylib", "libLive2DCubismCore.so"}
	}
	return []string{"libLive2DCubismCore.so", "libLive2DCubismCore.dylib"}
}

func openLibrary(path string) (uintptr, error) {
	h, err := purego.Dlopen(path, purego.RTLD_NOW|purego.RTLD_GLOBAL)
	if err != nil {
		return 0, fmt.Errorf("core: dlopen %s: %w", path, err)
	}
	return h, nil
}

func closeLibrary(h uintptr) error {
	return purego.Dlclose(h)
}

func lookup(h uintptr, name string) (uintptr, error) {
	p, err := purego.Dlsym(h, name)
	if err != nil {
		return 0, fmt.Errorf("core: dlsym %s: %w", name, err)
	}
	if p == 0 {
		return 0, fmt.Errorf("core: symbol %s not found", name)
	}
	return p, nil
}
