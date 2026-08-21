//go:build linux

package lcd

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLinuxControllerStatus(t *testing.T) {
	c := newPlatform()
	status, err := c.Status()
	if err != nil {
		t.Fatalf("Status() error = %v", err)
	}
	// On any Linux host, Status returns IsOn=true by default when no LCD
	// is detected (the function defaults to true and only flips to false
	// when a known backlight/DPMS path indicates so).
	_ = status
}

func TestSysfsIsWritableByAnyone(t *testing.T) {
	tests := []struct {
		name string
		mode os.FileMode
		want bool
	}{
		// The Pi 5's DRM DPMS attribute. Read-only for *everyone*, root
		// included — which is why the `sudo sh -c 'echo On > …'` fallback
		// could never have worked, and why probing the mode (rather than our
		// own access) is the right test.
		{"r--r--r-- is unwritable even by root", 0o444, false},
		{"rw-r--r-- is writable by root", 0o644, true},
		{"rw-rw-rw- is writable by all", 0o666, true},
		{"-w--w--w- is write-only but writable", 0o222, true},
		{"---------- is unwritable", 0o000, false},
		{"r--r--rw- group-less other write still counts", 0o446, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "attr")
			if err := os.WriteFile(path, []byte("On"), 0o644); err != nil {
				t.Fatalf("seed file: %v", err)
			}
			if err := os.Chmod(path, tt.mode); err != nil {
				t.Fatalf("chmod: %v", err)
			}
			if got := sysfsIsWritableByAnyone(path); got != tt.want {
				t.Errorf("sysfsIsWritableByAnyone(mode %o) = %v, want %v", tt.mode, got, tt.want)
			}
		})
	}
}

func TestSysfsIsWritableByAnyoneMissingPath(t *testing.T) {
	if sysfsIsWritableByAnyone(filepath.Join(t.TempDir(), "nope")) {
		t.Error("a path that does not exist must not be reported writable")
	}
	if sysfsIsWritableByAnyone("") {
		t.Error("an empty path must not be reported writable")
	}
}
