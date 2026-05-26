package airplay

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// DACPClient issues iTunes DACP "ctrl-int" commands to a discovered DACP
// HTTP endpoint published by the AirPlay sender (the iPhone). Each
// command is a fire-and-forget GET with no body; the only required header
// is `Active-Remote: <token>`.
//
// Reference: https://github.com/jkiddo/jolivia/wiki/DACP-Protocol
type DACPClient struct {
	httpClient *http.Client
}

// NewDACPClient builds a client using the supplied *http.Client. Pass a
// timeout-bounded client (the recommendation is 2s — DACP requests are
// LAN-only and short).
func NewDACPClient(c *http.Client) *DACPClient {
	if c == nil {
		c = &http.Client{Timeout: 2 * time.Second}
	}
	return &DACPClient{httpClient: c}
}

// Play sends ctrl-int/1/play.
func (c *DACPClient) Play(ctx context.Context, baseURL, activeRemote string) error {
	return c.do(ctx, baseURL, activeRemote, "play")
}

// Pause sends ctrl-int/1/pause.
func (c *DACPClient) Pause(ctx context.Context, baseURL, activeRemote string) error {
	return c.do(ctx, baseURL, activeRemote, "pause")
}

// PlayPause sends ctrl-int/1/playpause (toggle).
func (c *DACPClient) PlayPause(ctx context.Context, baseURL, activeRemote string) error {
	return c.do(ctx, baseURL, activeRemote, "playpause")
}

// Next sends ctrl-int/1/nextitem.
func (c *DACPClient) Next(ctx context.Context, baseURL, activeRemote string) error {
	return c.do(ctx, baseURL, activeRemote, "nextitem")
}

// Prev sends ctrl-int/1/previtem.
func (c *DACPClient) Prev(ctx context.Context, baseURL, activeRemote string) error {
	return c.do(ctx, baseURL, activeRemote, "previtem")
}

func (c *DACPClient) do(ctx context.Context, baseURL, activeRemote, command string) error {
	if baseURL == "" {
		return errors.New("dacp: empty base URL")
	}
	if activeRemote == "" {
		return errors.New("dacp: empty Active-Remote token")
	}
	url := strings.TrimRight(baseURL, "/") + "/ctrl-int/1/" + command
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("dacp: build request: %w", err)
	}
	req.Header.Set("Active-Remote", activeRemote)
	req.Header.Set("User-Agent", "stellar-airplay/1")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("dacp: request: %w", err)
	}
	defer func() {
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
	}()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("dacp: unexpected status %d", resp.StatusCode)
	}
	return nil
}
