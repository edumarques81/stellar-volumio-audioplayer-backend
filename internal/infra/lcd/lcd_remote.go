package lcd

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// RemoteController calls a Pi-resident lcd-control HTTP service over the LAN.
// Used by darwin + windows builds via NewPlatform() when STELLAR_LCD_REMOTE_URL
// and STELLAR_LCD_REMOTE_TOKEN are set; on Linux the in-process wlr-randr/DPMS
// impl in lcd_linux.go is preferred.
//
// All HTTP calls are bounded by a 2-second client timeout — LCD power ops are
// infrequent and a wedged Pi-side service must not freeze the Mac backend.
type RemoteController struct {
	baseURL string
	token   string
	client  *http.Client
}

// NewRemoteController builds a controller with a default 2s-timeout client.
func NewRemoteController(baseURL, token string) *RemoteController {
	return NewRemoteControllerWithClient(baseURL, token, &http.Client{Timeout: 2 * time.Second})
}

// NewRemoteControllerWithClient lets tests inject a fake transport.
func NewRemoteControllerWithClient(baseURL, token string, client *http.Client) *RemoteController {
	return &RemoteController{
		baseURL: strings.TrimRight(baseURL, "/"),
		token:   token,
		client:  client,
	}
}

// Status reads the LCD power state from GET /api/screen/status.
// Response shape: {"status":"on"|"off"} or {"status":"unknown"}.
func (r *RemoteController) Status() (Status, error) {
	req, err := http.NewRequest(http.MethodGet, r.baseURL+"/api/screen/status", nil)
	if err != nil {
		return Status{}, fmt.Errorf("lcd remote: build status request: %w", err)
	}
	req.Header.Set("X-Auth-Token", r.token)

	resp, err := r.client.Do(req)
	if err != nil {
		return Status{}, fmt.Errorf("lcd remote: status: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return Status{}, fmt.Errorf("lcd remote: status: HTTP %d", resp.StatusCode)
	}

	var payload struct {
		Status string `json:"status"`
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1024))
	if err != nil {
		return Status{}, fmt.Errorf("lcd remote: read status body: %w", err)
	}
	if len(body) == 0 {
		return Status{IsOn: true}, nil // tolerant default
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return Status{}, fmt.Errorf("lcd remote: decode status body: %w", err)
	}
	return Status{IsOn: strings.EqualFold(payload.Status, "on")}, nil
}

// Set turns the LCD on or off via POST /api/screen/{on,off}.
func (r *RemoteController) Set(on bool) error {
	path := "/api/screen/off"
	if on {
		path = "/api/screen/on"
	}
	req, err := http.NewRequest(http.MethodPost, r.baseURL+path, nil)
	if err != nil {
		return fmt.Errorf("lcd remote: build set request: %w", err)
	}
	req.Header.Set("X-Auth-Token", r.token)

	resp, err := r.client.Do(req)
	if err != nil {
		return fmt.Errorf("lcd remote: set: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("lcd remote: set: HTTP %d", resp.StatusCode)
	}
	return nil
}
