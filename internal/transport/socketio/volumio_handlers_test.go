package socketio

import (
	"testing"
)

// TestGetIntFromMap tests the helper function
func TestGetIntFromMap(t *testing.T) {
	tests := []struct {
		name       string
		m          map[string]interface{}
		key        string
		defaultVal int
		expected   int
	}{
		{
			name:       "nil map",
			m:          nil,
			key:        "test",
			defaultVal: -1,
			expected:   -1,
		},
		{
			name:       "missing key",
			m:          map[string]interface{}{"other": 5},
			key:        "test",
			defaultVal: -1,
			expected:   -1,
		},
		{
			name:       "int value",
			m:          map[string]interface{}{"test": 42},
			key:        "test",
			defaultVal: -1,
			expected:   42,
		},
		{
			name:       "float64 value",
			m:          map[string]interface{}{"test": float64(42)},
			key:        "test",
			defaultVal: -1,
			expected:   42,
		},
		{
			name:       "int64 value",
			m:          map[string]interface{}{"test": int64(42)},
			key:        "test",
			defaultVal: -1,
			expected:   42,
		},
		{
			name:       "string value returns default",
			m:          map[string]interface{}{"test": "42"},
			key:        "test",
			defaultVal: -1,
			expected:   -1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := getIntFromMap(tt.m, tt.key, tt.defaultVal)
			if result != tt.expected {
				t.Errorf("getIntFromMap() = %d, want %d", result, tt.expected)
			}
		})
	}
}

// TestNewVolumioHandlers tests handler creation
func TestNewVolumioHandlers(t *testing.T) {
	handlers := NewVolumioHandlers(nil, nil, nil)

	if handlers == nil {
		t.Fatal("NewVolumioHandlers should not return nil")
	}

	if handlers.deviceService != nil {
		t.Error("deviceService should be nil when passed nil")
	}
	if handlers.playerService != nil {
		t.Error("playerService should be nil when passed nil")
	}
	if handlers.server != nil {
		t.Error("server should be nil when passed nil")
	}
}

// Note: Full integration tests for Socket.IO handlers require a mock socket
// implementation. The handler logic is tested via unit tests for the services
// they depend on. Integration testing is done on the Raspberry Pi with real
// Volumio Connect apps.
