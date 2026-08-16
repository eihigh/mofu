//go:build windows

package core

import (
	"fmt"
	"syscall"
)

func defaultLibNames() []string {
	return []string{"Live2DCubismCore.dll"}
}

func openLibrary(path string) (uintptr, error) {
	h, err := syscall.LoadLibrary(path)
	if err != nil {
		return 0, fmt.Errorf("core: LoadLibrary %s: %w", path, err)
	}
	return uintptr(h), nil
}

func closeLibrary(h uintptr) error {
	return syscall.FreeLibrary(syscall.Handle(h))
}

func lookup(h uintptr, name string) (uintptr, error) {
	p, err := syscall.GetProcAddress(syscall.Handle(h), name)
	if err != nil {
		return 0, fmt.Errorf("core: GetProcAddress %s: %w", name, err)
	}
	return p, nil
}
