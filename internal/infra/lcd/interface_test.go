package lcd

import (
	"errors"
	"testing"
)

func TestStatusZeroValue(t *testing.T) {
	var s Status
	if s.IsOn {
		t.Error("Status zero-value IsOn should be false")
	}
}

func TestErrUnsupportedSentinel(t *testing.T) {
	if !errors.Is(ErrUnsupported, ErrUnsupported) {
		t.Error("ErrUnsupported should satisfy errors.Is against itself")
	}
}

func TestNewPlatformReturnsNonNil(t *testing.T) {
	c := NewPlatform()
	if c == nil {
		t.Fatal("NewPlatform() returned nil")
	}
}
