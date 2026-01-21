// Package mpd provides a wrapper around the gompd MPD client.
package mpd

import (
	"fmt"
	"strconv"
	"sync"
	"time"

	"github.com/fhs/gompd/v2/mpd"
	"github.com/rs/zerolog/log"
)

// Client wraps the MPD client with reconnection logic.
type Client struct {
	mu       sync.RWMutex
	client   *mpd.Client
	watcher  *mpd.Watcher
	host     string
	port     int
	password string
}

// NewClient creates a new MPD client wrapper.
func NewClient(host string, port int, password string) *Client {
	return &Client{
		host:     host,
		port:     port,
		password: password,
	}
}

// Connect establishes connection to MPD.
func (c *Client) Connect() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	addr := fmt.Sprintf("%s:%d", c.host, c.port)
	log.Info().Str("addr", addr).Msg("Connecting to MPD")

	client, err := mpd.Dial("tcp", addr)
	if err != nil {
		return fmt.Errorf("failed to connect to MPD: %w", err)
	}

	if c.password != "" {
		if err := client.Command("password %s", c.password).OK(); err != nil {
			client.Close()
			return fmt.Errorf("MPD authentication failed: %w", err)
		}
	}

	c.client = client
	log.Info().Msg("Connected to MPD")
	return nil
}

// Close closes the MPD connection.
func (c *Client) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.watcher != nil {
		c.watcher.Close()
		c.watcher = nil
	}

	if c.client != nil {
		err := c.client.Close()
		c.client = nil
		return err
	}
	return nil
}

// Ping checks if the connection is alive.
func (c *Client) Ping() error {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.client == nil {
		return fmt.Errorf("not connected")
	}
	return c.client.Ping()
}

// Status returns the current MPD status.
func (c *Client) Status() (mpd.Attrs, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.client == nil {
		return nil, fmt.Errorf("not connected")
	}
	return c.client.Status()
}

// CurrentSong returns the currently playing song.
func (c *Client) CurrentSong() (mpd.Attrs, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.client == nil {
		return nil, fmt.Errorf("not connected")
	}
	return c.client.CurrentSong()
}

// Play starts playback. If pos is -1, resumes current track.
func (c *Client) Play(pos int) error {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.client == nil {
		return fmt.Errorf("not connected")
	}

	if pos < 0 {
		return c.client.Play(-1)
	}
	return c.client.Play(pos)
}

// Pause toggles pause state.
func (c *Client) Pause(pause bool) error {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.client == nil {
		return fmt.Errorf("not connected")
	}
	return c.client.Pause(pause)
}

// Stop stops playback.
func (c *Client) Stop() error {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.client == nil {
		return fmt.Errorf("not connected")
	}
	return c.client.Stop()
}

// Next plays the next song.
func (c *Client) Next() error {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.client == nil {
		return fmt.Errorf("not connected")
	}
	return c.client.Next()
}

// Previous plays the previous song.
func (c *Client) Previous() error {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.client == nil {
		return fmt.Errorf("not connected")
	}
	return c.client.Previous()
}

// Seek seeks to position in current song (seconds).
func (c *Client) Seek(pos int) error {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.client == nil {
		return fmt.Errorf("not connected")
	}

	status, err := c.client.Status()
	if err != nil {
		return err
	}

	songPos, err := strconv.Atoi(status["song"])
	if err != nil {
		return fmt.Errorf("no song playing")
	}

	return c.client.Seek(songPos, pos)
}

// SetVolume sets the volume (0-100).
func (c *Client) SetVolume(vol int) error {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.client == nil {
		return fmt.Errorf("not connected")
	}

	if vol < 0 {
		vol = 0
	} else if vol > 100 {
		vol = 100
	}

	return c.client.SetVolume(vol)
}

// SetRandom sets random/shuffle mode.
func (c *Client) SetRandom(on bool) error {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.client == nil {
		return fmt.Errorf("not connected")
	}
	return c.client.Random(on)
}

// SetRepeat sets repeat mode.
func (c *Client) SetRepeat(on bool) error {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.client == nil {
		return fmt.Errorf("not connected")
	}
	return c.client.Repeat(on)
}

// SetSingle sets single mode (repeat single song).
func (c *Client) SetSingle(on bool) error {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.client == nil {
		return fmt.Errorf("not connected")
	}
	return c.client.Single(on)
}

// PlaylistInfo returns the current queue.
func (c *Client) PlaylistInfo() ([]mpd.Attrs, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.client == nil {
		return nil, fmt.Errorf("not connected")
	}
	return c.client.PlaylistInfo(-1, -1)
}

// Clear clears the current queue.
func (c *Client) Clear() error {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.client == nil {
		return fmt.Errorf("not connected")
	}
	return c.client.Clear()
}

// Add adds a URI to the queue.
func (c *Client) Add(uri string) error {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.client == nil {
		return fmt.Errorf("not connected")
	}
	return c.client.Add(uri)
}

// Watch starts watching for MPD subsystem changes.
// Returns a channel that receives subsystem names when they change.
func (c *Client) Watch(subsystems ...string) (<-chan string, error) {
	addr := fmt.Sprintf("%s:%d", c.host, c.port)

	watcher, err := mpd.NewWatcher("tcp", addr, c.password, subsystems...)
	if err != nil {
		return nil, fmt.Errorf("failed to create watcher: %w", err)
	}

	c.mu.Lock()
	c.watcher = watcher
	c.mu.Unlock()

	ch := make(chan string, 10)

	go func() {
		defer close(ch)
		for {
			select {
			case subsystem, ok := <-watcher.Event:
				if !ok {
					return
				}
				ch <- subsystem
			case err := <-watcher.Error:
				log.Error().Err(err).Msg("MPD watcher error")
				// Try to reconnect after a delay
				time.Sleep(time.Second)
			}
		}
	}()

	return ch, nil
}

// ListAllInfo lists all songs in the database.
func (c *Client) ListAllInfo(uri string) ([]mpd.Attrs, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.client == nil {
		return nil, fmt.Errorf("not connected")
	}
	return c.client.ListAllInfo(uri)
}

// ListInfo lists contents of a directory.
func (c *Client) ListInfo(uri string) ([]mpd.Attrs, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.client == nil {
		return nil, fmt.Errorf("not connected")
	}
	return c.client.ListInfo(uri)
}
