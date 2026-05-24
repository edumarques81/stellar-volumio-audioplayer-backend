package socketio_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/edumarques81/stellar-volumio-audioplayer-backend/internal/transport/socketio"
)

func TestRemoteSourcesClient_ListShares_OK(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/sources/list" {
			t.Errorf("path = %q, want /api/sources/list", r.URL.Path)
		}
		if r.Header.Get("X-Auth-Token") != "secret-token" {
			t.Errorf("X-Auth-Token = %q, want %q", r.Header.Get("X-Auth-Token"), "secret-token")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"shares": [
				{"id":"abc","name":"Windows NAS","ip":"192.168.86.26","path":"MusicLibrary","fstype":"cifs","username":"u","options":"","mountPoint":"/mnt/NAS/Windows_NAS","mounted":true},
				{"id":"def","name":"USB","ip":"127.0.0.1","path":"/usb","fstype":"nfs","username":"","options":"ro","mountPoint":"/mnt/NAS/USB","mounted":false}
			]
		}`))
	}))
	defer srv.Close()

	c := socketio.NewRemoteSourcesClient(srv.URL, "secret-token")
	shares, err := c.ListShares()
	if err != nil {
		t.Fatalf("ListShares: %v", err)
	}
	if len(shares) != 2 {
		t.Fatalf("len(shares) = %d, want 2", len(shares))
	}
	if shares[0].Name != "Windows NAS" || !shares[0].Mounted || shares[0].MountPoint != "/mnt/NAS/Windows_NAS" {
		t.Errorf("share[0] = %+v, want Windows NAS mounted at /mnt/NAS/Windows_NAS", shares[0])
	}
	if shares[1].Name != "USB" || shares[1].Mounted {
		t.Errorf("share[1] = %+v, want USB unmounted", shares[1])
	}
}

func TestRemoteSourcesClient_ListShares_EmptyOK(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"shares":[]}`))
	}))
	defer srv.Close()

	c := socketio.NewRemoteSourcesClient(srv.URL, "tok")
	shares, err := c.ListShares()
	if err != nil {
		t.Fatalf("ListShares: %v", err)
	}
	if len(shares) != 0 {
		t.Errorf("len(shares) = %d, want 0", len(shares))
	}
}

func TestRemoteSourcesClient_ListShares_Non200ReturnsError(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()

	c := socketio.NewRemoteSourcesClient(srv.URL, "tok")
	_, err := c.ListShares()
	if err == nil {
		t.Fatal("expected error on 500, got nil")
	}
	if !strings.Contains(err.Error(), "HTTP 500") {
		t.Errorf("error message = %q, want substring %q", err.Error(), "HTTP 500")
	}
}

func TestRemoteSourcesClient_ListShares_TrailingSlashURL(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"shares":[]}`))
	}))
	defer srv.Close()

	// Trailing slash on the base URL should not double up.
	c := socketio.NewRemoteSourcesClient(srv.URL+"/", "tok")
	if _, err := c.ListShares(); err != nil {
		t.Fatalf("ListShares with trailing slash: %v", err)
	}
}
