package sources

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// RemoteDiscoverer implements Discoverer by calling stellar-mount-control's
// /api/mount/{devices,shares} endpoints over HTTP+bearer. Used by darwin +
// windows builds via NewPlatformDiscoverer() when STELLAR_MOUNT_REMOTE_URL +
// _TOKEN are set.
type RemoteDiscoverer struct {
	baseURL string
	token   string
	client  *http.Client
}

func NewRemoteDiscoverer(baseURL, token string) *RemoteDiscoverer {
	return NewRemoteDiscovererWithClient(baseURL, token, &http.Client{Timeout: 15 * time.Second})
}

func NewRemoteDiscovererWithClient(baseURL, token string, client *http.Client) *RemoteDiscoverer {
	return &RemoteDiscoverer{
		baseURL: strings.TrimRight(baseURL, "/"),
		token:   token,
		client:  client,
	}
}

// DiscoverDevices GETs /api/mount/devices.
func (d *RemoteDiscoverer) DiscoverDevices(ctx context.Context) ([]NasDevice, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, d.baseURL+"/api/mount/devices", nil)
	if err != nil {
		return nil, fmt.Errorf("remote discover: build: %w", err)
	}
	req.Header.Set("X-Auth-Token", d.token)
	resp, err := d.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("remote discover: do: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("remote discover: HTTP %d", resp.StatusCode)
	}
	var payload struct {
		Devices []NasDevice `json:"devices"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 64*1024)).Decode(&payload); err != nil {
		return nil, fmt.Errorf("remote discover: decode: %w", err)
	}
	if payload.Devices == nil {
		payload.Devices = []NasDevice{}
	}
	return payload.Devices, nil
}

// BrowseShares GETs /api/mount/shares?host=...&username=...&password=...
// Maps 502 + {code,message} JSON into a *ShareBrowseError so the existing
// transport layer's errors.As switch still works.
func (d *RemoteDiscoverer) BrowseShares(ctx context.Context, host, username, password string) ([]ShareInfo, error) {
	q := url.Values{}
	q.Set("host", host)
	if username != "" {
		q.Set("username", username)
	}
	if password != "" {
		q.Set("password", password)
	}
	u := d.baseURL + "/api/mount/shares?" + q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, fmt.Errorf("remote browse shares: build: %w", err)
	}
	req.Header.Set("X-Auth-Token", d.token)
	resp, err := d.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("remote browse shares: do: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 64*1024))

	if resp.StatusCode == http.StatusBadGateway {
		// Pi-side mapped a tool error into {code, message}. Reify as ShareBrowseError.
		var sbe ShareBrowseError
		if err := json.Unmarshal(body, &sbe); err == nil && sbe.Code != "" {
			return nil, &sbe
		}
		return nil, fmt.Errorf("remote browse shares: 502 %s", strings.TrimSpace(string(body)))
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("remote browse shares: HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var payload struct {
		Shares []ShareInfo `json:"shares"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("remote browse shares: decode: %w", err)
	}
	if payload.Shares == nil {
		payload.Shares = []ShareInfo{}
	}
	return payload.Shares, nil
}
