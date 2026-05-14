//go:build linux

package lcd

import "testing"

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
