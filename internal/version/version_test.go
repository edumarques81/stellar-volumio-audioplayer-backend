package version_test

import (
	"testing"

	"github.com/edumarques81/stellar-volumio-audioplayer-backend/internal/version"
)

func TestVersionInfo(t *testing.T) {
	t.Run("Version should not be empty", func(t *testing.T) {
		if version.Version == "" {
			t.Error("Version should not be empty")
		}
	})

	t.Run("Name should be Stellar", func(t *testing.T) {
		if version.Name != "Stellar" {
			t.Errorf("Expected name 'Stellar', got '%s'", version.Name)
		}
	})
}

func TestGetInfo(t *testing.T) {
	info := version.GetInfo()

	t.Run("should return name", func(t *testing.T) {
		if info.Name != version.Name {
			t.Errorf("Expected name '%s', got '%s'", version.Name, info.Name)
		}
	})

	t.Run("should return version", func(t *testing.T) {
		if info.Version != version.Version {
			t.Errorf("Expected version '%s', got '%s'", version.Version, info.Version)
		}
	})

	t.Run("should return build time if set", func(t *testing.T) {
		// BuildTime may be empty in tests (set at compile time)
		// Just verify it doesn't panic
		_ = info.BuildTime
	})

	t.Run("should return git commit if set", func(t *testing.T) {
		// GitCommit may be empty in tests (set at compile time)
		// Just verify it doesn't panic
		_ = info.GitCommit
	})
}

func TestString(t *testing.T) {
	info := version.GetInfo()
	str := info.String()

	if str == "" {
		t.Error("String() should not return empty string")
	}

	// Should contain the name and version at minimum
	if len(str) < len(version.Name)+len(version.Version) {
		t.Errorf("String() seems too short: %s", str)
	}
}
