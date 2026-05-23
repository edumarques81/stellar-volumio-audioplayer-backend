package socketio

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/edumarques81/stellar-volumio-audioplayer-backend/internal/domain/device"
	"github.com/edumarques81/stellar-volumio-audioplayer-backend/internal/infra/netinfo"
)

// RemoteInfoReader is the read-side proxy interface used by socket handlers
// to fetch host-specific data from the Pi appliance. RemoteInfoClient is
// the production HTTP implementation; tests inject stubs.
//
// All methods are best-effort: callers receive a zero-value response and a
// non-nil error if the Pi is unreachable, the token drifts, or the response
// can't be decoded. The server handler then emits the zero value to the
// frontend, which renders "—". This is per the M1.E design — honest empty
// payload beats fabricated Mac-local data.
type RemoteInfoReader interface {
	SystemInfo() (SystemInfo, error)
	DeviceInfo() (device.DeviceInfo, error)
	NetworkStatus() (netinfo.Status, error)
	BitPerfect() (BitPerfectStatus, error)
	DsdMode() (DsdModeResponse, error)
	MixerMode() (MixerModeResponse, error)
}

// RemoteInfoClient proxies six read handlers to the Pi-resident
// stellar-mount-control.service. Used by Mac/Windows backend hosts in
// the M1.C+ topology where the backend lives off the audio appliance.
// Reuses the same env vars (STELLAR_MOUNT_REMOTE_URL + _TOKEN) and the
// same X-Auth-Token gate as RemoteSystemActions (M1.D).
type RemoteInfoClient struct {
	baseURL string
	token   string
	client  *http.Client
}

// NewRemoteInfoClient builds a reader with a default 5s timeout. Match
// RemoteSystemActions's budget — these calls happen on the request path
// for Settings tab loads, so 5s is the upper bound a user will tolerate.
func NewRemoteInfoClient(baseURL, token string) *RemoteInfoClient {
	return NewRemoteInfoClientWithClient(baseURL, token, &http.Client{Timeout: 5 * time.Second})
}

// NewRemoteInfoClientWithClient lets tests inject a fake transport.
func NewRemoteInfoClientWithClient(baseURL, token string, client *http.Client) *RemoteInfoClient {
	return &RemoteInfoClient{
		baseURL: strings.TrimRight(baseURL, "/"),
		token:   token,
		client:  client,
	}
}

// get builds the GET, sets the auth header, decodes JSON into dst,
// wraps errors with the path so log lines are greppable.
func (r *RemoteInfoClient) get(path string, dst any) error {
	req, err := http.NewRequest(http.MethodGet, r.baseURL+path, nil)
	if err != nil {
		return fmt.Errorf("remote info: build %s: %w", path, err)
	}
	req.Header.Set("X-Auth-Token", r.token)
	resp, err := r.client.Do(req)
	if err != nil {
		return fmt.Errorf("remote info: %s: %w", path, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("remote info: %s: HTTP %d", path, resp.StatusCode)
	}
	if err := json.NewDecoder(resp.Body).Decode(dst); err != nil {
		return fmt.Errorf("remote info: %s: decode: %w", path, err)
	}
	return nil
}

// SystemInfo fetches the Pi's identity + hardware. Caller-side
// (server.systemInfo helper, Task 8) merges the binary's version/builddate
// after this returns.
func (r *RemoteInfoClient) SystemInfo() (SystemInfo, error) {
	var out SystemInfo
	err := r.get("/api/system/info", &out)
	return out, err
}

func (r *RemoteInfoClient) DeviceInfo() (device.DeviceInfo, error) {
	var out device.DeviceInfo
	err := r.get("/api/system/device", &out)
	return out, err
}

func (r *RemoteInfoClient) NetworkStatus() (netinfo.Status, error) {
	var out netinfo.Status
	err := r.get("/api/network/status", &out)
	return out, err
}

func (r *RemoteInfoClient) BitPerfect() (BitPerfectStatus, error) {
	var out BitPerfectStatus
	err := r.get("/api/audio/bitperfect", &out)
	return out, err
}

func (r *RemoteInfoClient) DsdMode() (DsdModeResponse, error) {
	var out DsdModeResponse
	err := r.get("/api/audio/dsd", &out)
	return out, err
}

func (r *RemoteInfoClient) MixerMode() (MixerModeResponse, error) {
	var out MixerModeResponse
	err := r.get("/api/audio/mixer", &out)
	return out, err
}
