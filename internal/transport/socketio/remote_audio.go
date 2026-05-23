package socketio

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/edumarques81/stellar-volumio-audioplayer-backend/internal/domain/device"
	"github.com/edumarques81/stellar-volumio-audioplayer-backend/internal/infra/netinfo"
)

// RemoteAudioClient is the read-side proxy interface used by socket handlers
// to fetch host-specific data from the Pi appliance. RemoteAudioClientImpl is
// the production HTTP implementation; tests inject stubs.
//
// All methods are best-effort: callers receive a zero-value response and a
// non-nil error if the Pi is unreachable, the token drifts, or the response
// can't be decoded. The server handler then emits the zero value to the
// frontend, which renders "—". This is per the M1.E design — honest empty
// payload beats fabricated Mac-local data.
type RemoteAudioClient interface {
	SystemInfo() (SystemInfo, error)
	DeviceInfo() (device.DeviceInfo, error)
	NetworkStatus() (netinfo.Status, error)
	BitPerfect() (BitPerfectStatus, error)
	DsdMode() (DsdModeResponse, error)
	MixerMode() (MixerModeResponse, error)
}

// RemoteAudioClientImpl proxies six read handlers to the Pi-resident
// stellar-mount-control.service. Used by Mac/Windows backend hosts in
// the M1.C+ topology where the backend lives off the audio appliance.
// Reuses the same env vars (STELLAR_MOUNT_REMOTE_URL + _TOKEN) and the
// same X-Auth-Token gate as RemoteSystemActions (M1.D).
type RemoteAudioClientImpl struct {
	baseURL     string
	token       string
	readClient  *http.Client
	writeClient *http.Client
}

// NewRemoteAudioClient builds a client with a 5s read timeout and a 30s write
// timeout. Read timeout matches RemoteSystemActions's budget — Settings tab
// loads must stay snappy. Write timeout accommodates Pi-side operations such
// as `systemctl restart mpd` (D3 spec: up to ~10s).
func NewRemoteAudioClient(baseURL, token string) *RemoteAudioClientImpl {
	return NewRemoteAudioClientWithClients(
		baseURL, token,
		&http.Client{Timeout: 5 * time.Second},
		&http.Client{Timeout: 30 * time.Second},
	)
}

// NewRemoteAudioClientWithClients lets tests inject separate read and write
// transports (e.g. a fast stub for reads, a slow stub for write paths).
func NewRemoteAudioClientWithClients(baseURL, token string, readClient, writeClient *http.Client) *RemoteAudioClientImpl {
	return &RemoteAudioClientImpl{
		baseURL:     strings.TrimRight(baseURL, "/"),
		token:       token,
		readClient:  readClient,
		writeClient: writeClient,
	}
}

// NewRemoteAudioClientWithClient is a convenience wrapper for callers that
// only have a single *http.Client (e.g. existing tests). Both the read and
// write paths use the provided client unchanged.
func NewRemoteAudioClientWithClient(baseURL, token string, client *http.Client) *RemoteAudioClientImpl {
	return NewRemoteAudioClientWithClients(baseURL, token, client, client)
}

// get builds the GET, sets the auth header, decodes JSON into dst,
// wraps errors with the path so log lines are greppable.
func (r *RemoteAudioClientImpl) get(path string, dst any) error {
	req, err := http.NewRequest(http.MethodGet, r.baseURL+path, nil)
	if err != nil {
		return fmt.Errorf("remote audio: build %s: %w", path, err)
	}
	req.Header.Set("X-Auth-Token", r.token)
	resp, err := r.readClient.Do(req)
	if err != nil {
		return fmt.Errorf("remote audio: %s: %w", path, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("remote audio: %s: HTTP %d", path, resp.StatusCode)
	}
	if err := json.NewDecoder(resp.Body).Decode(dst); err != nil {
		return fmt.Errorf("remote audio: %s: decode: %w", path, err)
	}
	return nil
}

// post serializes body to JSON, posts to path with X-Auth-Token, and decodes
// the JSON response into dst. Uses the 30s writeClient so Pi-side operations
// (e.g. systemctl restart mpd) have room to complete. Errors wrap the path
// so log lines are greppable. body and dst may be nil.
func (r *RemoteAudioClientImpl) post(path string, body any, dst any) error {
	var reqBody io.Reader
	if body != nil {
		buf, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("remote audio: marshal %s: %w", path, err)
		}
		reqBody = bytes.NewReader(buf)
	}
	req, err := http.NewRequest(http.MethodPost, r.baseURL+path, reqBody)
	if err != nil {
		return fmt.Errorf("remote audio: build %s: %w", path, err)
	}
	req.Header.Set("X-Auth-Token", r.token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := r.writeClient.Do(req)
	if err != nil {
		return fmt.Errorf("remote audio: %s: %w", path, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("remote audio: %s: HTTP %d", path, resp.StatusCode)
	}
	if dst != nil {
		if err := json.NewDecoder(resp.Body).Decode(dst); err != nil {
			return fmt.Errorf("remote audio: %s: decode: %w", path, err)
		}
	}
	return nil
}

// SystemInfo fetches the Pi's identity + hardware. Caller-side
// (server.systemInfo helper, Task 8) merges the binary's version/builddate
// after this returns.
func (r *RemoteAudioClientImpl) SystemInfo() (SystemInfo, error) {
	var out SystemInfo
	err := r.get("/api/system/info", &out)
	return out, err
}

func (r *RemoteAudioClientImpl) DeviceInfo() (device.DeviceInfo, error) {
	var out device.DeviceInfo
	err := r.get("/api/system/device", &out)
	return out, err
}

func (r *RemoteAudioClientImpl) NetworkStatus() (netinfo.Status, error) {
	var out netinfo.Status
	err := r.get("/api/network/status", &out)
	return out, err
}

func (r *RemoteAudioClientImpl) BitPerfect() (BitPerfectStatus, error) {
	var out BitPerfectStatus
	err := r.get("/api/audio/bitperfect", &out)
	return out, err
}

func (r *RemoteAudioClientImpl) DsdMode() (DsdModeResponse, error) {
	var out DsdModeResponse
	err := r.get("/api/audio/dsd", &out)
	return out, err
}

func (r *RemoteAudioClientImpl) MixerMode() (MixerModeResponse, error) {
	var out MixerModeResponse
	err := r.get("/api/audio/mixer", &out)
	return out, err
}
