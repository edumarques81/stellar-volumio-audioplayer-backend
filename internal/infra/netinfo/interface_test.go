package netinfo

import (
	"errors"
	"testing"
)

func TestStatusZeroValue(t *testing.T) {
	var s Status
	if s.Type != "" || s.Signal != 0 || s.Strength != 0 {
		t.Errorf("Status zero value not all-zero: %+v", s)
	}
}

func TestErrUnsupportedSentinel(t *testing.T) {
	if !errors.Is(ErrUnsupported, ErrUnsupported) {
		t.Error("ErrUnsupported should satisfy errors.Is against itself")
	}
}

func TestNewPlatformReturnsNonNil(t *testing.T) {
	r := NewPlatform()
	if r == nil {
		t.Fatal("NewPlatform() returned nil")
	}
}
