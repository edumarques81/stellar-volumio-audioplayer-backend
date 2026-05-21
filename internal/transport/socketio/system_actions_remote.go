package socketio

import (
	"fmt"
	"net/http"
	"strings"
	"time"
)

// RemoteSystemActions calls a Pi-resident stellar-mount-control HTTP service
// for system:shutdown / system:reboot. Used by the Mac/Windows backend hosts
// in the M1.C+ topology where the backend lives off the audio appliance —
// without this proxy, /sbin/shutdown would power down the wrong host.
//
// The endpoints live alongside /api/mount/* on port 8082 (same service, same
// token) because the mount-control service already runs as root with the
// privileges shutdown/reboot need. A 5-second client timeout is enough for
// the trivial-payload calls; the Pi-side setTimeout(100ms) before exec
// ensures the HTTP response flushes before the kernel goes down.
type RemoteSystemActions struct {
	baseURL string
	token   string
	client  *http.Client
}

// NewRemoteSystemActions builds an actions client with a default 5s timeout.
func NewRemoteSystemActions(baseURL, token string) *RemoteSystemActions {
	return NewRemoteSystemActionsWithClient(baseURL, token, &http.Client{Timeout: 5 * time.Second})
}

// NewRemoteSystemActionsWithClient lets tests inject a fake transport.
func NewRemoteSystemActionsWithClient(baseURL, token string, client *http.Client) *RemoteSystemActions {
	return &RemoteSystemActions{
		baseURL: strings.TrimRight(baseURL, "/"),
		token:   token,
		client:  client,
	}
}

// Shutdown POSTs to /api/system/shutdown.
func (r *RemoteSystemActions) Shutdown() error { return r.post("/api/system/shutdown") }

// Reboot POSTs to /api/system/reboot.
func (r *RemoteSystemActions) Reboot() error { return r.post("/api/system/reboot") }

func (r *RemoteSystemActions) post(path string) error {
	req, err := http.NewRequest(http.MethodPost, r.baseURL+path, nil)
	if err != nil {
		return fmt.Errorf("system remote: build %s: %w", path, err)
	}
	req.Header.Set("X-Auth-Token", r.token)
	resp, err := r.client.Do(req)
	if err != nil {
		return fmt.Errorf("system remote: %s: %w", path, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("system remote: %s: HTTP %d", path, resp.StatusCode)
	}
	return nil
}
