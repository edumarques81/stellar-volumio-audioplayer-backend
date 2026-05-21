package socketio

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestRemoteSystemActions_PostsAuthAndPath verifies both methods hit the
// expected URL with the right verb + auth header and surface non-200s as
// errors. Uses httptest because the body+method surface area is tiny.
func TestRemoteSystemActions_PostsAuthAndPath(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name     string
		action   func(*RemoteSystemActions) error
		wantPath string
	}{
		{"Shutdown", (*RemoteSystemActions).Shutdown, "/api/system/shutdown"},
		{"Reboot", (*RemoteSystemActions).Reboot, "/api/system/reboot"},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var gotPath, gotMethod, gotAuth string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotPath = r.URL.Path
				gotMethod = r.Method
				gotAuth = r.Header.Get("X-Auth-Token")
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`{"success":true}`))
			}))
			t.Cleanup(srv.Close)

			r := NewRemoteSystemActions(srv.URL, "secret-token-xyz")
			if err := tc.action(r); err != nil {
				t.Fatalf("%s: unexpected error: %v", tc.name, err)
			}
			if gotPath != tc.wantPath {
				t.Fatalf("path = %q, want %q", gotPath, tc.wantPath)
			}
			if gotMethod != http.MethodPost {
				t.Fatalf("method = %q, want POST", gotMethod)
			}
			if gotAuth != "secret-token-xyz" {
				t.Fatalf("X-Auth-Token = %q, want %q", gotAuth, "secret-token-xyz")
			}
		})
	}
}

// TestRemoteSystemActions_Non200SurfacesError ensures we don't silently treat
// a 401/500 as success — the auth gate on the Pi side must propagate.
func TestRemoteSystemActions_Non200SurfacesError(t *testing.T) {
	t.Parallel()

	for _, code := range []int{http.StatusUnauthorized, http.StatusForbidden, http.StatusInternalServerError, http.StatusBadGateway} {
		code := code
		t.Run(http.StatusText(code), func(t *testing.T) {
			t.Parallel()
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(code)
			}))
			t.Cleanup(srv.Close)

			r := NewRemoteSystemActions(srv.URL, "tok")
			if err := r.Shutdown(); err == nil {
				t.Fatalf("Shutdown: expected error for HTTP %d, got nil", code)
			}
			if err := r.Reboot(); err == nil {
				t.Fatalf("Reboot: expected error for HTTP %d, got nil", code)
			}
		})
	}
}

// TestRemoteSystemActions_TrimsTrailingSlashInBaseURL guards the small bit of
// normalization in NewRemoteSystemActions — easy to lose under refactor.
func TestRemoteSystemActions_TrimsTrailingSlashInBaseURL(t *testing.T) {
	t.Parallel()
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	// Note trailing slash on baseURL — must not produce //api/system/shutdown
	r := NewRemoteSystemActions(srv.URL+"/", "tok")
	if err := r.Shutdown(); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
	if gotPath != "/api/system/shutdown" {
		t.Fatalf("path = %q, want /api/system/shutdown (trailing slash should be trimmed)", gotPath)
	}
}
