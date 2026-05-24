package socketio

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/edumarques81/stellar-volumio-audioplayer-backend/internal/domain/sources"
)

// RemoteSourcesClient is the M1.E.2 read-only proxy for NAS share listings.
// On off-appliance backends (Mac/Windows/Linux), the local sources.json may
// be empty even when shares exist on the Pi — addNasShare etc. don't
// auto-replicate. This client asks the Pi mount-control service directly
// so the Settings page renders the authoritative list.
//
// Mount/unmount operations themselves still proxy through RemoteMounter
// (M1.C); this client only reads. Credentials never leave the Pi.
type RemoteSourcesClient interface {
	ListShares() ([]sources.NasShare, error)
}

// RemoteSourcesClientImpl is the production HTTP implementation. Reuses the
// same STELLAR_MOUNT_REMOTE_URL + _TOKEN env vars as the other M1.D/E/E.1
// proxies; the endpoint is `GET /api/sources/list` added to the Pi's
// stellar-mount-control.service in the same release.
type RemoteSourcesClientImpl struct {
	baseURL    string
	token      string
	readClient *http.Client
}

// NewRemoteSourcesClient builds a client with a 5s read timeout to match
// the rest of the M1.E read budget.
func NewRemoteSourcesClient(baseURL, token string) *RemoteSourcesClientImpl {
	return NewRemoteSourcesClientWithClient(
		baseURL, token,
		&http.Client{Timeout: 5 * time.Second},
	)
}

// NewRemoteSourcesClientWithClient lets tests inject a stub transport.
func NewRemoteSourcesClientWithClient(baseURL, token string, client *http.Client) *RemoteSourcesClientImpl {
	return &RemoteSourcesClientImpl{
		baseURL:    strings.TrimRight(baseURL, "/"),
		token:      token,
		readClient: client,
	}
}

// listSharesResponse mirrors the wire shape produced by the Pi
// mount-control's `GET /api/sources/list` handler. Field names match
// sources.NasShare's json tags so we can unmarshal directly into a slice.
//
// Pi-side intentionally OMITS the password and INCLUDES Mounted +
// MountPoint computed against the live filesystem.
type listSharesResponse struct {
	Shares []sources.NasShare `json:"shares"`
}

// ListShares fetches the NAS share list from the Pi. Returns ([], nil) when
// the Pi has no shares configured; (nil, err) on any transport / decode
// failure. Callers should treat err as "fall back to local state" rather
// than fatal — the Settings page rendering a stale list is better than an
// unhandled error in the connect-time batch.
func (r *RemoteSourcesClientImpl) ListShares() ([]sources.NasShare, error) {
	req, err := http.NewRequest(http.MethodGet, r.baseURL+"/api/sources/list", nil)
	if err != nil {
		return nil, fmt.Errorf("remote sources: build request: %w", err)
	}
	req.Header.Set("X-Auth-Token", r.token)

	resp, err := r.readClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("remote sources: GET /api/sources/list: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("remote sources: HTTP %d", resp.StatusCode)
	}

	var body listSharesResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, fmt.Errorf("remote sources: decode: %w", err)
	}
	return body.Shares, nil
}
