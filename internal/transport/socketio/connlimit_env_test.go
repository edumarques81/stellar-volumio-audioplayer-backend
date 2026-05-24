package socketio

import (
	"testing"
)

func TestMaxExternalClientsFromEnv_FallbackOnEmpty(t *testing.T) {
	t.Setenv("STELLAR_MAX_EXTERNAL_CLIENTS", "")
	if got := maxExternalClientsFromEnv(42); got != 42 {
		t.Errorf("empty env: got %d, want 42", got)
	}
}

func TestMaxExternalClientsFromEnv_PositiveInteger(t *testing.T) {
	t.Setenv("STELLAR_MAX_EXTERNAL_CLIENTS", "7")
	if got := maxExternalClientsFromEnv(100); got != 7 {
		t.Errorf("explicit 7: got %d, want 7", got)
	}
}

func TestMaxExternalClientsFromEnv_FallbackOnNonInt(t *testing.T) {
	t.Setenv("STELLAR_MAX_EXTERNAL_CLIENTS", "many")
	if got := maxExternalClientsFromEnv(100); got != 100 {
		t.Errorf("non-int env: got %d, want fallback 100", got)
	}
}

func TestMaxExternalClientsFromEnv_FallbackOnZeroOrNegative(t *testing.T) {
	for _, v := range []string{"0", "-5"} {
		t.Setenv("STELLAR_MAX_EXTERNAL_CLIENTS", v)
		if got := maxExternalClientsFromEnv(100); got != 100 {
			t.Errorf("env=%q: got %d, want fallback 100", v, got)
		}
	}
}
