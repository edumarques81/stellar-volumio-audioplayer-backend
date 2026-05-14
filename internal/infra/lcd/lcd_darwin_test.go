//go:build darwin

package lcd

import (
	"errors"
	"testing"
)

func TestDarwinStubReturnsOn(t *testing.T) {
	c := newPlatform()
	status, err := c.Status()
	if err != nil {
		t.Fatalf("Status() error = %v", err)
	}
	if !status.IsOn {
		t.Error("darwin stub Status should report IsOn=true (no LCD attached, default-on)")
	}
}

func TestDarwinStubSetReturnsUnsupported(t *testing.T) {
	c := newPlatform()
	if err := c.Set(false); !errors.Is(err, ErrUnsupported) {
		t.Errorf("Set(false) error = %v, want ErrUnsupported", err)
	}
}
