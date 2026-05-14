//go:build windows

package paths

import (
	"os"
	"path/filepath"
)

func dataDir() string {
	if appdata := os.Getenv("LOCALAPPDATA"); appdata != "" {
		return filepath.Join(appdata, "stellar")
	}
	return filepath.Join(os.TempDir(), "stellar")
}

func cacheDir() string { return filepath.Join(dataDir(), "cache") }

func nasMountBase() string { return `C:\stellar\nas` }
func usbMountBase() string { return `C:\stellar\usb` }

func listMounts() ([]Mount, error) { return nil, ErrUnsupported }
func systemHardware() string       { return "Windows host" }
