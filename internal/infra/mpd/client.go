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

	return c.connectLocked()
}

// connectLocked establishes connection (must hold lock).
func (c *Client) connectLocked() error {
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

// ensureConnected checks connection and reconnects if needed.
func (c *Client) ensureConnected() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.client == nil {
		return c.connectLocked()
	}

	// Try a ping to check if connection is alive
	if err := c.client.Ping(); err != nil {
		log.Warn().Err(err).Msg("MPD connection lost, reconnecting...")
		// Close old connection
		c.client.Close()
		c.client = nil
		// Reconnect
		return c.connectLocked()
	}

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
	if err := c.ensureConnected(); err != nil {
		return nil, err
	}

	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.client.Status()
}

// CurrentSong returns the currently playing song.
func (c *Client) CurrentSong() (mpd.Attrs, error) {
	if err := c.ensureConnected(); err != nil {
		return nil, err
	}

	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.client.CurrentSong()
}

// Play starts playback. If pos is -1, resumes current track.
func (c *Client) Play(pos int) error {
	if err := c.ensureConnected(); err != nil {
		return err
	}

	c.mu.RLock()
	defer c.mu.RUnlock()

	if pos < 0 {
		return c.client.Play(-1)
	}
	return c.client.Play(pos)
}

// Pause toggles pause state.
func (c *Client) Pause(pause bool) error {
	if err := c.ensureConnected(); err != nil {
		return err
	}

	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.client.Pause(pause)
}

// Stop stops playback.
func (c *Client) Stop() error {
	if err := c.ensureConnected(); err != nil {
		return err
	}

	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.client.Stop()
}

// Next plays the next song.
func (c *Client) Next() error {
	if err := c.ensureConnected(); err != nil {
		return err
	}

	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.client.Next()
}

// Previous plays the previous song.
func (c *Client) Previous() error {
	if err := c.ensureConnected(); err != nil {
		return err
	}

	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.client.Previous()
}

// Seek seeks to position in current song (seconds).
func (c *Client) Seek(pos int) error {
	if err := c.ensureConnected(); err != nil {
		return err
	}

	c.mu.RLock()
	defer c.mu.RUnlock()

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
	if err := c.ensureConnected(); err != nil {
		return err
	}

	c.mu.RLock()
	defer c.mu.RUnlock()

	if vol < 0 {
		vol = 0
	} else if vol > 100 {
		vol = 100
	}

	return c.client.SetVolume(vol)
}

// SetRandom sets random/shuffle mode.
func (c *Client) SetRandom(on bool) error {
	if err := c.ensureConnected(); err != nil {
		return err
	}

	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.client.Random(on)
}

// SetRepeat sets repeat mode.
func (c *Client) SetRepeat(on bool) error {
	if err := c.ensureConnected(); err != nil {
		return err
	}

	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.client.Repeat(on)
}

// SetSingle sets single mode (repeat single song).
func (c *Client) SetSingle(on bool) error {
	if err := c.ensureConnected(); err != nil {
		return err
	}

	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.client.Single(on)
}

// PlaylistInfo returns the current queue.
func (c *Client) PlaylistInfo() ([]mpd.Attrs, error) {
	if err := c.ensureConnected(); err != nil {
		return nil, err
	}

	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.client.PlaylistInfo(-1, -1)
}

// Clear clears the current queue.
func (c *Client) Clear() error {
	if err := c.ensureConnected(); err != nil {
		return err
	}

	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.client.Clear()
}

// Add adds a URI to the queue.
func (c *Client) Add(uri string) error {
	if err := c.ensureConnected(); err != nil {
		return err
	}

	c.mu.RLock()
	defer c.mu.RUnlock()

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
	if err := c.ensureConnected(); err != nil {
		return nil, err
	}

	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.client.ListAllInfo(uri)
}

// ListInfo lists contents of a directory.
func (c *Client) ListInfo(uri string) ([]mpd.Attrs, error) {
	if err := c.ensureConnected(); err != nil {
		return nil, err
	}

	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.client.ListInfo(uri)
}

// ReadPicture retrieves embedded album art for a song.
func (c *Client) ReadPicture(uri string) ([]byte, error) {
	if err := c.ensureConnected(); err != nil {
		return nil, err
	}

	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.client.ReadPicture(uri)
}

// AlbumArt retrieves album art from the music directory (cover.jpg, etc).
func (c *Client) AlbumArt(uri string) ([]byte, error) {
	if err := c.ensureConnected(); err != nil {
		return nil, err
	}

	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.client.AlbumArt(uri)
}

// AlbumInfo represents an album with its metadata from MPD database.
type AlbumInfo struct {
	Album       string
	AlbumArtist string
}

// ListAlbums returns all unique albums from the MPD database grouped by album artist.
// This uses MPD's "list" command which is much faster than scanning directories.
func (c *Client) ListAlbums() ([]AlbumInfo, error) {
	if err := c.ensureConnected(); err != nil {
		return nil, err
	}

	c.mu.RLock()
	defer c.mu.RUnlock()

	// Use "list album group albumartist" to get albums with their artists
	// AttrsList("Album") tells the parser that each new entry starts with "Album:" key
	attrs, err := c.client.Command("list album group albumartist").AttrsList("Album")
	if err != nil {
		return nil, fmt.Errorf("failed to list albums: %w", err)
	}

	var albums []AlbumInfo
	for _, attr := range attrs {
		album := attr["Album"]
		artist := attr["AlbumArtist"]
		if album != "" {
			albums = append(albums, AlbumInfo{
				Album:       album,
				AlbumArtist: artist,
			})
		}
	}

	return albums, nil
}

// FindAlbumTracks finds all tracks for a specific album and optionally album artist.
// Returns track information including file paths, which can be used to determine source.
func (c *Client) FindAlbumTracks(album string, albumArtist string) ([]mpd.Attrs, error) {
	if err := c.ensureConnected(); err != nil {
		return nil, err
	}

	c.mu.RLock()
	defer c.mu.RUnlock()

	// Build the find command
	// Format: find album "album name" albumartist "artist name"
	var cmd *mpd.Command
	if albumArtist != "" {
		cmd = c.client.Command("find album %s albumartist %s", album, albumArtist)
	} else {
		cmd = c.client.Command("find album %s", album)
	}

	// AttrsList("file") tells the parser each song starts with "file:" key
	return cmd.AttrsList("file")
}

// SearchByBase searches for all songs within a specific base path.
// This is useful for filtering songs by source (e.g., INTERNAL, USB).
func (c *Client) SearchByBase(basePath string) ([]mpd.Attrs, error) {
	if err := c.ensureConnected(); err != nil {
		return nil, err
	}

	c.mu.RLock()
	defer c.mu.RUnlock()

	// Use "search base" to find songs under a path
	// MPD supports: search base "INTERNAL"
	// AttrsList("file") tells the parser each song starts with "file:" key
	return c.client.Command("search base %s", basePath).AttrsList("file")
}

// ListAlbumsInBase returns unique albums that have tracks in the specified base path.
// This combines "list album" filtering with base path checking.
func (c *Client) ListAlbumsInBase(basePath string) ([]AlbumInfo, error) {
	if err := c.ensureConnected(); err != nil {
		return nil, err
	}

	c.mu.RLock()
	defer c.mu.RUnlock()

	// Use search base to get all songs in the path, then extract unique albums
	// AttrsList("file") tells the parser each song starts with "file:" key
	songs, err := c.client.Command("search base %s", basePath).AttrsList("file")
	if err != nil {
		return nil, fmt.Errorf("failed to search base %s: %w", basePath, err)
	}

	// Extract unique album/artist combinations
	seen := make(map[string]bool)
	var albums []AlbumInfo

	for _, song := range songs {
		album := song["Album"]
		artist := song["AlbumArtist"]
		if artist == "" {
			artist = song["Artist"]
		}

		// Skip songs without album tag
		if album == "" {
			continue
		}

		key := album + "\x00" + artist
		if !seen[key] {
			seen[key] = true
			albums = append(albums, AlbumInfo{
				Album:       album,
				AlbumArtist: artist,
			})
		}
	}

	return albums, nil
}

// GetAlbumDetails returns detailed information about an album including track count
// and a representative track path (for album art and source detection).
type AlbumDetails struct {
	Album       string
	AlbumArtist string
	TrackCount  int
	FirstTrack  string // Path to first track (for album art)
	TotalTime   int    // Total duration in seconds
}

// GetAlbumDetails retrieves detailed information for albums within a base path.
func (c *Client) GetAlbumDetails(basePath string) ([]AlbumDetails, error) {
	if err := c.ensureConnected(); err != nil {
		return nil, err
	}

	c.mu.RLock()
	defer c.mu.RUnlock()

	// Get all songs in the base path
	// AttrsList("file") tells the parser each song starts with "file:" key
	songs, err := c.client.Command("search base %s", basePath).AttrsList("file")
	if err != nil {
		return nil, fmt.Errorf("failed to search base %s: %w", basePath, err)
	}

	// Group songs by album
	albumMap := make(map[string]*AlbumDetails)

	for _, song := range songs {
		album := song["Album"]
		artist := song["AlbumArtist"]
		if artist == "" {
			artist = song["Artist"]
		}

		// Skip songs without album tag
		if album == "" {
			continue
		}

		key := album + "\x00" + artist

		if _, exists := albumMap[key]; !exists {
			albumMap[key] = &AlbumDetails{
				Album:       album,
				AlbumArtist: artist,
				FirstTrack:  song["file"],
			}
		}

		details := albumMap[key]
		details.TrackCount++

		// Parse duration
		if dur, err := strconv.Atoi(song["Time"]); err == nil {
			details.TotalTime += dur
		} else if dur, err := strconv.ParseFloat(song["duration"], 64); err == nil {
			details.TotalTime += int(dur)
		}
	}

	// Convert map to slice
	var albums []AlbumDetails
	for _, details := range albumMap {
		albums = append(albums, *details)
	}

	return albums, nil
}
