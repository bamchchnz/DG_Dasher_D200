//go:build linux

package main

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

// Older Raspberry Pi graphics (the VC4, as used on the Pi 3 and earlier)
// only offer OpenGL ES 2.0, but the graphics library needs ES 3.0. Mesa's
// software renderer works fine for a terminal screen, so on those machines,
// or whenever the graphics fail to start, D200 restarts itself with
// LIBGL_ALWAYS_SOFTWARE=1. Setting the variable from Go is not enough: the
// GL libraries read the C environment, so the program has to be re-executed.

const softGLMark = "DASHER200_SOFTGL"

// useSoftwareGLIfNeeded restarts the program with software rendering when
// the only GPU driver present is VC4. It returns normally otherwise.
func useSoftwareGLIfNeeded() {
	if os.Getenv(softGLMark) != "" || os.Getenv("LIBGL_ALWAYS_SOFTWARE") != "" {
		return
	}
	if oldPiGraphics() {
		restartWithSoftwareGL()
	}
}

// oldPiGraphics reports whether this is a Raspberry Pi whose GPU only offers
// OpenGL ES 2.0: the VC4 driver (named "vc4-drm" on the Pi) with no V3D, or
// a board model known to have that GPU (Pi 1, 2, 3, Zero, Compute Module 3).
func oldPiGraphics() bool {
	var vc4, v3d bool
	for d := range gpuDrivers() {
		vc4 = vc4 || strings.HasPrefix(d, "vc4")
		v3d = v3d || strings.HasPrefix(d, "v3d")
	}
	if vc4 && !v3d {
		return true
	}
	model, _ := os.ReadFile("/proc/device-tree/model")
	m := string(model)
	for _, old := range []string{"Raspberry Pi 3", "Raspberry Pi 2", "Raspberry Pi Model", "Raspberry Pi Zero", "Compute Module 3"} {
		if strings.Contains(m, old) {
			return true
		}
	}
	return false
}

// retryWithSoftwareGL restarts with software rendering after a graphics
// start-up failure, unless that has already been tried. It returns only if
// no restart happened.
func retryWithSoftwareGL(err error) {
	if err == nil || os.Getenv(softGLMark) != "" || os.Getenv("LIBGL_ALWAYS_SOFTWARE") != "" {
		return
	}
	msg := strings.ToLower(err.Error())
	for _, w := range []string{"glfw", "egl", "glx", "opengl", "gl:"} {
		if strings.Contains(msg, w) {
			os.Stderr.WriteString("graphics failed to start (" + err.Error() + "); retrying with software rendering\n")
			restartWithSoftwareGL()
			return
		}
	}
}

func restartWithSoftwareGL() {
	exe, err := os.Executable()
	if err != nil {
		return
	}
	env := append(os.Environ(), "LIBGL_ALWAYS_SOFTWARE=1", softGLMark+"=1")
	_ = syscall.Exec(exe, os.Args, env) // only returns on failure
}

// gpuDrivers lists the kernel drivers behind /sys/class/drm, e.g. "v3d", "vc4".
func gpuDrivers() map[string]bool {
	found := map[string]bool{}
	cards, _ := filepath.Glob("/sys/class/drm/card*")
	for _, c := range cards {
		if strings.Contains(filepath.Base(c), "-") { // connectors like card0-HDMI-A-1
			continue
		}
		if target, err := os.Readlink(filepath.Join(c, "device", "driver")); err == nil {
			found[filepath.Base(target)] = true
		}
	}
	return found
}
