package sources

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/rs/zerolog/log"
)

// RemoteMounter implements Mounter by calling a Pi-resident
// stellar-mount-control HTTP service. Used by darwin + windows builds via
// NewPlatformMounter() when STELLAR_MOUNT_REMOTE_URL + _TOKEN are set.
//
// All operations are bounded by client timeouts. The mount operation gets a
// longer timeout (30s) because real network mount syscalls can be slow when
// the NAS is sluggish. Discovery/list operations are faster.
type RemoteMounter struct {
	baseURL string
	token   string
	client  *http.Client
}

// NewRemoteMounter builds a mounter with sensible default timeouts.
func NewRemoteMounter(baseURL, token string) *RemoteMounter {
	return NewRemoteMounterWithClient(baseURL, token, &http.Client{Timeout: 30 * time.Second})
}

// NewRemoteMounterWithClient lets tests inject a fake transport.
func NewRemoteMounterWithClient(baseURL, token string, client *http.Client) *RemoteMounter {
	return &RemoteMounter{
		baseURL: strings.TrimRight(baseURL, "/"),
		token:   token,
		client:  client,
	}
}

func (r *RemoteMounter) do(ctx context.Context, method, path string, body any) (*http.Response, error) {
	var rdr io.Reader
	if body != nil {
		buf, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("remote mount: encode body: %w", err)
		}
		rdr = bytes.NewReader(buf)
	}
	req, err := http.NewRequestWithContext(ctx, method, r.baseURL+path, rdr)
	if err != nil {
		return nil, fmt.Errorf("remote mount: build %s %s: %w", method, path, err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("X-Auth-Token", r.token)
	resp, err := r.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("remote mount: %s %s: %w", method, path, err)
	}
	return resp, nil
}

// Mount POSTs to /api/mount.
func (r *RemoteMounter) Mount(ctx context.Context, share *NasShare) error {
	body := map[string]any{
		"ip":         share.IP,
		"share":      share.Path,
		"fstype":     share.FSType,
		"mountpoint": share.MountPoint,
		"username":   share.Username,
		"password":   share.Password,
		"options":    share.Options,
	}
	resp, err := r.do(ctx, http.MethodPost, "/api/mount", body)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("remote mount: HTTP %d: %s", resp.StatusCode, readErrSnippet(resp.Body))
	}
	share.Mounted = true
	log.Info().Str("ip", share.IP).Str("share", share.Path).Str("mp", share.MountPoint).Msg("Remote mount OK")
	return nil
}

// Unmount POSTs to /api/mount/unmount.
func (r *RemoteMounter) Unmount(ctx context.Context, mountPoint string) error {
	body := map[string]any{"mountpoint": mountPoint}
	resp, err := r.do(ctx, http.MethodPost, "/api/mount/unmount", body)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("remote unmount: HTTP %d: %s", resp.StatusCode, readErrSnippet(resp.Body))
	}
	return nil
}

// IsMounted GETs /api/mount/is-mounted?path=... — returns false on any error
// or non-200 (matches the local impls' "best-effort" semantics).
func (r *RemoteMounter) IsMounted(mountPoint string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	resp, err := r.do(ctx, http.MethodGet, "/api/mount/is-mounted?path="+url.QueryEscape(mountPoint), nil)
	if err != nil {
		log.Debug().Err(err).Msg("remote IsMounted: request failed")
		return false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return false
	}
	var payload struct{ Mounted bool `json:"mounted"` }
	if err := json.NewDecoder(io.LimitReader(resp.Body, 512)).Decode(&payload); err != nil {
		return false
	}
	return payload.Mounted
}

// CreateMountPoint POSTs to /api/mount/mountpoint.
func (r *RemoteMounter) CreateMountPoint(path string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	resp, err := r.do(ctx, http.MethodPost, "/api/mount/mountpoint", map[string]any{"path": path})
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("remote create mountpoint: HTTP %d: %s", resp.StatusCode, readErrSnippet(resp.Body))
	}
	return nil
}

// RemoveMountPoint DELETEs /api/mount/mountpoint?path=...
func (r *RemoteMounter) RemoveMountPoint(path string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	resp, err := r.do(ctx, http.MethodDelete, "/api/mount/mountpoint?path="+url.QueryEscape(path), nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("remote remove mountpoint: HTTP %d: %s", resp.StatusCode, readErrSnippet(resp.Body))
	}
	return nil
}

// CreateSymlink POSTs to /api/mount/symlink.
func (r *RemoteMounter) CreateSymlink(source, target string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	resp, err := r.do(ctx, http.MethodPost, "/api/mount/symlink",
		map[string]any{"source": source, "target": target})
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("remote symlink: HTTP %d: %s", resp.StatusCode, readErrSnippet(resp.Body))
	}
	return nil
}

// RemoveSymlink DELETEs /api/mount/symlink?path=...
func (r *RemoteMounter) RemoveSymlink(path string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	resp, err := r.do(ctx, http.MethodDelete, "/api/mount/symlink?path="+url.QueryEscape(path), nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("remote rm symlink: HTTP %d: %s", resp.StatusCode, readErrSnippet(resp.Body))
	}
	return nil
}

// readErrSnippet returns a short error-message snippet for logging.
func readErrSnippet(rd io.Reader) string {
	buf, _ := io.ReadAll(io.LimitReader(rd, 512))
	return strings.TrimSpace(string(buf))
}
