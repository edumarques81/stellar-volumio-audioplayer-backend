package paths

import (
	"errors"
	"testing"
)

func TestDataDirNonEmpty(t *testing.T) {
	if got := DataDir(); got == "" {
		t.Errorf("DataDir() = %q, want non-empty", got)
	}
}

func TestCacheDirNonEmpty(t *testing.T) {
	if got := CacheDir(); got == "" {
		t.Errorf("CacheDir() = %q, want non-empty", got)
	}
}

func TestNasMountBaseNonEmpty(t *testing.T) {
	if got := NasMountBase(); got == "" {
		t.Errorf("NasMountBase() = %q, want non-empty", got)
	}
}

func TestUsbMountBaseNonEmpty(t *testing.T) {
	if got := UsbMountBase(); got == "" {
		t.Errorf("UsbMountBase() = %q, want non-empty", got)
	}
}

func TestErrUnsupportedIdentity(t *testing.T) {
	// Sentinel must be comparable via errors.Is across the package.
	wrapped := errors.New("wrap: " + ErrUnsupported.Error())
	if errors.Is(wrapped, ErrUnsupported) {
		t.Error("plain errors.New wrap should not satisfy errors.Is for sentinel")
	}
	if !errors.Is(ErrUnsupported, ErrUnsupported) {
		t.Error("ErrUnsupported should satisfy errors.Is against itself")
	}
}
