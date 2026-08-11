// Package mpd provides a wrapper around the gompd MPD client.
package mpd

import (
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/edumarques81/stellar-volumio-audioplayer-backend/internal/infra/musicfile"
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

	// stopWatch is closed by Close() to signal the watch goroutine to
	// exit cleanly. It is created lazily by Watch() and reset to nil
	// after the goroutine drains. The goroutine owns calling
	// watcher.Close() on the live watcher; Close() must not double-close.
	stopWatch chan struct{}
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
//
// If a watch goroutine is running, this signals it to exit; the goroutine
// owns closing its own *mpd.Watcher to avoid double-close races against
// the reconnect path.
func (c *Client) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.stopWatch != nil {
		// Idempotent: close() on an already-closed channel panics, so
		// nil it out after the first close.
		close(c.stopWatch)
		c.stopWatch = nil
		// Drop our reference; the goroutine will close the watcher.
		c.watcher = nil
	} else if c.watcher != nil {
		// No active watch goroutine (shouldn't happen given Watch()
		// always starts one, but kept for safety): close directly.
		_ = c.watcher.Close()
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
//
// If MPD is unreachable at call time, Watch no longer returns an error.
// Instead it starts the goroutine immediately with a nil initial watcher so
// the retry/backoff loop runs and picks up MPD once it becomes available.
// This enables the backend to boot before MPD is ready (e.g. on the Pi when
// stellar-backend starts before mpd.service has fully initialised).
//
// If the underlying MPD connection dies after startup, the goroutine
// closes the dead *mpd.Watcher and creates a fresh one with exponential
// backoff (500ms → 1s → 2s → 4s → 8s → 10s, capped). gompd's *Watcher does
// NOT auto-reconnect on its own, so this loop is the only thing keeping
// track-end auto-advance alive after a transient socket failure.
func (c *Client) Watch(subsystems ...string) (<-chan string, error) {
	addr := fmt.Sprintf("%s:%d", c.host, c.port)

	// Attempt to create the initial watcher. On failure (e.g. MPD not yet
	// running at boot) log a warning and proceed with nil — the goroutine's
	// retry loop will establish the connection once MPD is ready.
	watcher, err := mpd.NewWatcher("tcp", addr, c.password, subsystems...)
	if err != nil {
		log.Warn().Err(err).Str("addr", addr).
			Msg("MPD watcher initial dial failed; will retry in background")
		watcher = nil
	}

	stop := make(chan struct{})

	c.mu.Lock()
	c.watcher = watcher // may be nil; the goroutine owns closing it
	c.stopWatch = stop
	c.mu.Unlock()

	ch := make(chan string, 10)

	go c.watchLoop(addr, subsystems, watcher, stop, ch)

	return ch, nil
}

// watchLoop drains events from the active watcher and reconnects with
// exponential backoff whenever the underlying connection dies. The initial
// watcher is passed in so the first iteration consumes it without a
// reconnect log.
//
// The goroutine owns the live *mpd.Watcher: it is the only caller of
// watcher.Close() on the active instance. Client.Close() signals teardown
// via the stop channel and lets this goroutine drain and close cleanly.
func (c *Client) watchLoop(addr string, subsystems []string, initial *mpd.Watcher, stop <-chan struct{}, ch chan<- string) {
	defer close(ch)

	const (
		initialBackoff = 500 * time.Millisecond
		maxBackoff     = 10 * time.Second
	)

	watcher := initial
	backoff := initialBackoff
	attempt := 0

	defer func() {
		// Final teardown: close whichever watcher we currently hold.
		if watcher != nil {
			_ = watcher.Close()
		}
	}()

	for {
		// Only drain events if we have a live watcher. If the previous
		// reconnect attempt failed, watcher is nil here and we skip
		// straight to the sleep+retry path below, avoiding a nil-deref
		// panic on watcher.Event / watcher.Error.
		if watcher != nil {
			// Drain events from the current watcher until it errors or
			// until stop is signalled. Any successful event resets the
			// backoff so a long-stable connection followed by a
			// transient failure starts at 500ms again.
			eventDelivered := false
			errored := false
			for !errored {
				select {
				case <-stop:
					return
				case subsystem, ok := <-watcher.Event:
					if !ok {
						// Channel closed unexpectedly (not via our stop
						// signal). Treat as terminal — gompd has torn down
						// this watcher and we have no live instance to
						// reconnect with. The deferred Close() above is a
						// no-op because the channels are already closed,
						// but we nil out the watcher so it doesn't try.
						watcher = nil
						return
					}
					ch <- subsystem
					eventDelivered = true
				case err, ok := <-watcher.Error:
					if !ok {
						watcher = nil
						return
					}
					log.Error().Err(err).Msg("MPD watcher error")
					errored = true
				}
			}

			if eventDelivered {
				backoff = initialBackoff
			}

			// Tear down the dead watcher. Close() is safe to call on a
			// watcher whose connection is already broken — gompd's noidle
			// write may fail but the goroutine will still exit.
			_ = watcher.Close()
			watcher = nil
		}

		// Sleep with backoff, but bail early if we're being torn down.
		select {
		case <-stop:
			return
		case <-time.After(backoff):
		}

		newWatcher, err := mpd.NewWatcher("tcp", addr, c.password, subsystems...)
		if err != nil {
			log.Error().Err(err).Dur("delay", backoff).Msg("MPD watcher reconnect failed")
			// Bump backoff up to the cap and retry. We DO NOT swap
			// c.watcher here because the old (dead) one was already
			// closed and there is no replacement yet.
			backoff = nextBackoff(backoff, maxBackoff)
			continue
		}

		attempt++
		log.Info().Int("attempt", attempt).Dur("delay", backoff).Msg("MPD watcher reconnected")

		c.mu.Lock()
		// If Close() was called between our select-stop check and now,
		// it set c.watcher to nil and closed stop. Detect that and
		// tear down the watcher we just created instead of leaking it.
		if c.stopWatch == nil {
			c.mu.Unlock()
			_ = newWatcher.Close()
			return
		}
		c.watcher = newWatcher
		c.mu.Unlock()

		watcher = newWatcher
		backoff = nextBackoff(backoff, maxBackoff)
	}
}

// nextBackoff doubles the current delay, capped at max.
func nextBackoff(current, max time.Duration) time.Duration {
	next := current * 2
	if next > max {
		return max
	}
	return next
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

// CapabilityFlags represents MPD server capabilities.
type CapabilityFlags struct {
	HasReadPicture  bool   // MPD 0.22+ - embedded album art extraction
	HasAlbumArt     bool   // MPD 0.21+ - folder-based album art
	HasGrouping     bool   // list command supports "group" parameter
	HasAddedTag     bool   // MPD 0.24+ - "added" timestamp in database
	ProtocolVersion string // MPD protocol version (e.g., "0.24.0")
}

// DatabaseStats represents MPD database statistics.
type DatabaseStats struct {
	Artists    int // Number of unique artists
	Albums     int // Number of unique albums
	Songs      int // Number of songs
	Uptime     int // MPD uptime in seconds
	DbPlaytime int // Total playtime of all songs
	DbUpdate   int // Last database update timestamp
	PlayTime   int // Total play time
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

// FindTracksByArtist finds all tracks crediting the given artist via MPD's
// Artist tag, independent of the AlbumArtist grouping GetAlbumDetails/
// GetArtistAlbums otherwise use. This is the ARTIST-04/BROWSE-04 fallback
// query: it can surface tracks that never got an AlbumArtist grouping at
// all (e.g. loose, untagged imports with no album).
//
// NOTE: MPD's "search" command (as opposed to "find") is a case-insensitive
// SUBSTRING match, not an exact match -- a search for "Bach" also matches
// "Bach Collegium". Callers MUST filter the returned songs client-side for
// an exact Artist-tag match, mirroring the strings.EqualFold filter already
// applied to AlbumArtist elsewhere in this package.
func (c *Client) FindTracksByArtist(artist string) ([]mpd.Attrs, error) {
	if err := c.ensureConnected(); err != nil {
		return nil, err
	}

	c.mu.RLock()
	defer c.mu.RUnlock()

	// AttrsList("file") tells the parser each song starts with "file:" key
	return c.client.Command("search artist %s", artist).AttrsList("file")
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
	Format      string // Audio format from MPD, e.g. "44100:16:2"
	Genre       string // Album-level genre (first track's Genre tag, normalized)
	// Disc is the MPD Disc tag from a representative track, first-track-wins
	// (same convention as Format/Genre). "" when absent.
	Disc string
}

// NormalizeGenre converts MPD's Genre tag value into the form the Library UI
// expects. MPD may return a `;`-separated string for multi-genre tracks
// (e.g. "Ambient; Post-Rock"); the frontend renders the genre as a single
// segment in the album meta strip and wants `/`-separated values without
// stray whitespace.
func NormalizeGenre(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}
	// Replace "; " before bare ";" so the longer match wins; then split and
	// re-join to clean up whitespace around values.
	parts := strings.Split(trimmed, ";")
	cleaned := make([]string, 0, len(parts))
	for _, p := range parts {
		if v := strings.TrimSpace(p); v != "" {
			cleaned = append(cleaned, v)
		}
	}
	return strings.Join(cleaned, " / ")
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

	albums, skipped := groupAlbumDetails(songs)
	log.Debug().Str("basePath", basePath).Int("skipped", skipped).
		Msg("GetAlbumDetails: skipped untagged real songs")

	return albums, nil
}

// groupAlbumDetails groups raw MPD song attrs by album + artist + directory
// (so different quality versions of the same album in different folders
// become separate entries), returning the grouped albums alongside a count
// of real (non-resource-fork) songs that were skipped for lacking an Album
// tag -- DATA-02's skipped/untagged signal.
//
// macOS AppleDouble resource-fork sidecar files (musicfile.IsResourceFork)
// are always excluded from grouping and are never counted as "skipped" --
// they are junk DATA-04 filtering removes, not untagged real music DATA-01
// retagging should fix (T-01-03).
func groupAlbumDetails(songs []mpd.Attrs) (albums []AlbumDetails, skipped int) {
	// Group songs by album + artist + directory (so different quality versions
	// of the same album in different folders become separate entries)
	albumMap := make(map[string]*AlbumDetails)

	for _, song := range songs {
		if musicfile.IsResourceFork(song["file"]) {
			continue
		}

		album := song["Album"]
		artist := song["AlbumArtist"]
		if artist == "" {
			artist = song["Artist"]
		}

		// Skip songs without album tag
		if album == "" {
			skipped++
			continue
		}

		// Extract directory from file path for grouping
		filePath := song["file"]
		directory := filePath
		if idx := strings.LastIndex(directory, "/"); idx > 0 {
			directory = directory[:idx]
		}

		key := album + "\x00" + artist + "\x00" + directory

		if _, exists := albumMap[key]; !exists {
			albumMap[key] = &AlbumDetails{
				Album:       album,
				AlbumArtist: artist,
				FirstTrack:  filePath,
				Format:      song["Format"],
				// First-track-wins: matches the existing precedent for
				// Format/quality. Multi-value genres get normalized to
				// "Ambient / Post-Rock" by NormalizeGenre.
				Genre: NormalizeGenre(song["Genre"]),
				Disc:  song["Disc"],
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
	for _, details := range albumMap {
		albums = append(albums, *details)
	}

	return albums, skipped
}

// ListArtists returns all unique album artists from the MPD database.
func (c *Client) ListArtists() ([]string, error) {
	if err := c.ensureConnected(); err != nil {
		return nil, err
	}

	c.mu.RLock()
	defer c.mu.RUnlock()

	// Use "list albumartist" to get all unique album artists
	// AttrsList("AlbumArtist") tells the parser each entry starts with "AlbumArtist:" key
	attrs, err := c.client.Command("list albumartist").AttrsList("AlbumArtist")
	if err != nil {
		return nil, fmt.Errorf("failed to list artists: %w", err)
	}

	var artists []string
	for _, attr := range attrs {
		artist := attr["AlbumArtist"]
		if artist != "" {
			artists = append(artists, artist)
		}
	}

	return artists, nil
}

// FindAlbumsByArtist finds all albums by a specific album artist.
func (c *Client) FindAlbumsByArtist(artist string) ([]AlbumInfo, error) {
	if err := c.ensureConnected(); err != nil {
		return nil, err
	}

	c.mu.RLock()
	defer c.mu.RUnlock()

	// Use "list album albumartist X" to get albums by artist
	attrs, err := c.client.Command("list album albumartist %s", artist).AttrsList("Album")
	if err != nil {
		return nil, fmt.Errorf("failed to find albums by artist: %w", err)
	}

	var albums []AlbumInfo
	for _, attr := range attrs {
		album := attr["Album"]
		if album != "" {
			albums = append(albums, AlbumInfo{
				Album:       album,
				AlbumArtist: artist,
			})
		}
	}

	return albums, nil
}

// ListPlaylists returns all saved playlists.
func (c *Client) ListPlaylists() ([]string, error) {
	if err := c.ensureConnected(); err != nil {
		return nil, err
	}

	c.mu.RLock()
	defer c.mu.RUnlock()

	// Use "listplaylists" to get all saved playlists
	attrs, err := c.client.Command("listplaylists").AttrsList("playlist")
	if err != nil {
		return nil, fmt.Errorf("failed to list playlists: %w", err)
	}

	var playlists []string
	for _, attr := range attrs {
		playlist := attr["playlist"]
		if playlist != "" {
			playlists = append(playlists, playlist)
		}
	}

	return playlists, nil
}

// ListPlaylistInfo returns the contents of a specific playlist.
func (c *Client) ListPlaylistInfo(name string) ([]mpd.Attrs, error) {
	if err := c.ensureConnected(); err != nil {
		return nil, err
	}

	c.mu.RLock()
	defer c.mu.RUnlock()

	// Use "listplaylistinfo" to get playlist contents
	return c.client.Command("listplaylistinfo %s", name).AttrsList("file")
}

// SavePlaylist saves the current queue as a new playlist.
func (c *Client) SavePlaylist(name string) error {
	if err := c.ensureConnected(); err != nil {
		return err
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	// Use "save" to save current queue as playlist
	return c.client.Command("save %s", name).OK()
}

// DeletePlaylist removes a saved playlist.
func (c *Client) DeletePlaylist(name string) error {
	if err := c.ensureConnected(); err != nil {
		return err
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	// Use "rm" to delete playlist
	return c.client.Command("rm %s", name).OK()
}

// LoadPlaylist loads a playlist into the queue and optionally plays it.
func (c *Client) LoadPlaylist(name string, play bool) error {
	if err := c.ensureConnected(); err != nil {
		return err
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	// Clear the queue first
	if err := c.client.Clear(); err != nil {
		return fmt.Errorf("failed to clear queue: %w", err)
	}

	// Load the playlist
	if err := c.client.Command("load %s", name).OK(); err != nil {
		return fmt.Errorf("failed to load playlist: %w", err)
	}

	// Start playback if requested
	if play {
		if err := c.client.Play(0); err != nil {
			return fmt.Errorf("failed to start playback: %w", err)
		}
	}

	return nil
}

// PlaylistAdd adds a URI to a saved playlist.
func (c *Client) PlaylistAdd(playlistName, uri string) error {
	if err := c.ensureConnected(); err != nil {
		return err
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	// Use "playlistadd" to add song to playlist
	return c.client.Command("playlistadd %s %s", playlistName, uri).OK()
}

// PlaylistDelete removes a song at position from a saved playlist.
func (c *Client) PlaylistDelete(playlistName string, pos int) error {
	if err := c.ensureConnected(); err != nil {
		return err
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	// Use "playlistdelete" to remove song from playlist
	return c.client.Command("playlistdelete %s %d", playlistName, pos).OK()
}

// FindSongInPlaylist finds the position of a URI in a playlist, returns -1 if not found.
func (c *Client) FindSongInPlaylist(playlistName, uri string) (int, error) {
	items, err := c.ListPlaylistInfo(playlistName)
	if err != nil {
		return -1, err
	}

	for i, item := range items {
		if item["file"] == uri {
			return i, nil
		}
	}
	return -1, nil
}

// DetectCapabilities detects what features the MPD server supports.
// This queries the server for available commands and protocol version.
func (c *Client) DetectCapabilities() (*CapabilityFlags, error) {
	if err := c.ensureConnected(); err != nil {
		return nil, err
	}

	c.mu.RLock()
	defer c.mu.RUnlock()

	flags := &CapabilityFlags{}

	// Get protocol version from status
	status, err := c.client.Status()
	if err == nil {
		// Protocol version isn't in status, we need to parse from initial connection
		// For now, we'll detect capabilities by trying commands
	}
	_ = status // Avoid unused variable warning

	// Get list of available commands
	// The "commands" command returns all available commands
	attrs, err := c.client.Command("commands").AttrsList("command")
	if err != nil {
		log.Warn().Err(err).Msg("Failed to get MPD commands list, assuming basic capabilities")
		return flags, nil
	}

	// Check for specific commands
	for _, attr := range attrs {
		cmd := attr["command"]
		switch cmd {
		case "readpicture":
			flags.HasReadPicture = true
		case "albumart":
			flags.HasAlbumArt = true
		}
	}

	// Test if "list" command supports grouping by trying a harmless command
	// If grouping works, we have MPD 0.21+
	_, err = c.client.Command("list album group albumartist window 0:1").AttrsList("Album")
	if err == nil {
		flags.HasGrouping = true
	}

	// Test for "added" tag support (MPD 0.24+) by checking if sort by added works
	_, err = c.client.Command("search any '' sort added window 0:1").AttrsList("file")
	if err == nil {
		flags.HasAddedTag = true
	}

	log.Info().
		Bool("readpicture", flags.HasReadPicture).
		Bool("albumart", flags.HasAlbumArt).
		Bool("grouping", flags.HasGrouping).
		Bool("added_tag", flags.HasAddedTag).
		Msg("Detected MPD capabilities")

	return flags, nil
}

// WatchDatabase starts watching for MPD database changes.
// Returns a channel that receives notifications when the database is updated.
// This is specifically for cache invalidation purposes.
func (c *Client) WatchDatabase() (<-chan string, error) {
	// Watch for database and update subsystems
	return c.Watch("database", "update")
}

// GetDatabaseStats returns statistics about the MPD database.
func (c *Client) GetDatabaseStats() (*DatabaseStats, error) {
	if err := c.ensureConnected(); err != nil {
		return nil, err
	}

	c.mu.RLock()
	defer c.mu.RUnlock()

	// Use "stats" command to get database statistics
	attrs, err := c.client.Command("stats").Attrs()
	if err != nil {
		return nil, fmt.Errorf("failed to get stats: %w", err)
	}

	stats := &DatabaseStats{}

	if v, err := strconv.Atoi(attrs["artists"]); err == nil {
		stats.Artists = v
	}
	if v, err := strconv.Atoi(attrs["albums"]); err == nil {
		stats.Albums = v
	}
	if v, err := strconv.Atoi(attrs["songs"]); err == nil {
		stats.Songs = v
	}
	if v, err := strconv.Atoi(attrs["uptime"]); err == nil {
		stats.Uptime = v
	}
	if v, err := strconv.Atoi(attrs["db_playtime"]); err == nil {
		stats.DbPlaytime = v
	}
	if v, err := strconv.Atoi(attrs["db_update"]); err == nil {
		stats.DbUpdate = v
	}
	if v, err := strconv.Atoi(attrs["playtime"]); err == nil {
		stats.PlayTime = v
	}

	return stats, nil
}

// CountAlbums returns the total count of unique albums in the database.
// This is more efficient than fetching all albums when only count is needed.
func (c *Client) CountAlbums() (int, error) {
	if err := c.ensureConnected(); err != nil {
		return 0, err
	}

	c.mu.RLock()
	defer c.mu.RUnlock()

	// Use "list album" and count results (more accurate than stats which might be cached)
	attrs, err := c.client.Command("list album").AttrsList("Album")
	if err != nil {
		return 0, fmt.Errorf("failed to count albums: %w", err)
	}

	return len(attrs), nil
}

// CountArtists returns the total count of unique album artists in the database.
// This is more efficient than fetching all artists when only count is needed.
func (c *Client) CountArtists() (int, error) {
	if err := c.ensureConnected(); err != nil {
		return 0, err
	}

	c.mu.RLock()
	defer c.mu.RUnlock()

	// Use "list albumartist" and count results
	attrs, err := c.client.Command("list albumartist").AttrsList("AlbumArtist")
	if err != nil {
		return 0, fmt.Errorf("failed to count artists: %w", err)
	}

	return len(attrs), nil
}

// CountAlbumsForArtist returns the count of albums by a specific artist.
// This is more efficient than the N+1 query pattern.
func (c *Client) CountAlbumsForArtist(artist string) (int, error) {
	if err := c.ensureConnected(); err != nil {
		return 0, err
	}

	c.mu.RLock()
	defer c.mu.RUnlock()

	attrs, err := c.client.Command("list album albumartist %s", artist).AttrsList("Album")
	if err != nil {
		return 0, fmt.Errorf("failed to count albums for artist: %w", err)
	}

	return len(attrs), nil
}

// GetArtistsWithAlbumCounts returns all artists with their album counts efficiently.
// This avoids the N+1 query problem by using MPD's grouping feature.
func (c *Client) GetArtistsWithAlbumCounts() (map[string]int, error) {
	if err := c.ensureConnected(); err != nil {
		return nil, err
	}

	c.mu.RLock()
	defer c.mu.RUnlock()

	// Get all albums grouped by artist
	attrs, err := c.client.Command("list album group albumartist").AttrsList("Album")
	if err != nil {
		return nil, fmt.Errorf("failed to list albums with artists: %w", err)
	}

	// Count albums per artist
	counts := make(map[string]int)
	for _, attr := range attrs {
		artist := attr["AlbumArtist"]
		if artist != "" {
			counts[artist]++
		}
	}

	return counts, nil
}

// Update initiates a database update (rescan) for the music directory.
// If uri is empty, it updates the entire database.
// Returns the job ID for the update.
func (c *Client) Update(uri string) (int, error) {
	if err := c.ensureConnected(); err != nil {
		return 0, err
	}

	c.mu.RLock()
	defer c.mu.RUnlock()

	jobID, err := c.client.Update(uri)
	if err != nil {
		return 0, fmt.Errorf("failed to update database: %w", err)
	}

	return jobID, nil
}

// ============================================================
// Queue Manipulation Methods (for Volumio integration)
// ============================================================

// AddId adds a URI to the queue and returns the song ID.
// If position is -1, adds to the end of the queue.
// If position >= 0, inserts at that position.
func (c *Client) AddId(uri string, position int) (int, error) {
	if err := c.ensureConnected(); err != nil {
		return 0, err
	}

	c.mu.RLock()
	defer c.mu.RUnlock()

	// Use addid command which returns the song ID
	var attrs mpd.Attrs
	var err error

	if position >= 0 {
		attrs, err = c.client.Command("addid %s %d", uri, position).Attrs()
	} else {
		attrs, err = c.client.Command("addid %s", uri).Attrs()
	}

	if err != nil {
		return 0, fmt.Errorf("failed to add song: %w", err)
	}

	// Parse the returned ID
	idStr := attrs["Id"]
	if idStr == "" {
		return 0, fmt.Errorf("no song ID returned")
	}

	id, err := strconv.Atoi(idStr)
	if err != nil {
		return 0, fmt.Errorf("invalid song ID: %w", err)
	}

	return id, nil
}

// Move moves a song in the queue from one position to another.
// Note: gompd's Move function takes (start, end, to) for range moves.
// We use (from, from+1, to) to move a single song from position 'from' to 'to'.
func (c *Client) Move(from, to int) error {
	if err := c.ensureConnected(); err != nil {
		return err
	}

	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.client.Move(from, from+1, to)
}

// Delete removes a song from the queue by position.
func (c *Client) Delete(pos int) error {
	if err := c.ensureConnected(); err != nil {
		return err
	}

	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.client.Delete(pos, pos+1)
}

// GetCurrentPosition returns the position of the currently playing song.
// Returns -1 if nothing is playing.
func (c *Client) GetCurrentPosition() (int, error) {
	if err := c.ensureConnected(); err != nil {
		return 0, err
	}

	c.mu.RLock()
	defer c.mu.RUnlock()

	status, err := c.client.Status()
	if err != nil {
		return 0, err
	}

	songPos := status["song"]
	if songPos == "" {
		return -1, nil
	}

	pos, err := strconv.Atoi(songPos)
	if err != nil {
		return -1, nil
	}

	return pos, nil
}

// GetQueueLength returns the number of songs in the queue.
func (c *Client) GetQueueLength() (int, error) {
	if err := c.ensureConnected(); err != nil {
		return 0, err
	}

	c.mu.RLock()
	defer c.mu.RUnlock()

	status, err := c.client.Status()
	if err != nil {
		return 0, err
	}

	lengthStr := status["playlistlength"]
	if lengthStr == "" {
		return 0, nil
	}

	length, err := strconv.Atoi(lengthStr)
	if err != nil {
		return 0, nil
	}

	return length, nil
}
