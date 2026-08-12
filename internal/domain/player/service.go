// Package player provides the player service for audio playback control.
package player

import (
	"path"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/edumarques81/stellar-volumio-audioplayer-backend/internal/infra/mpd"
	"github.com/rs/zerolog/log"

	"github.com/edumarques81/stellar-volumio-audioplayer-backend/internal/infra/arturl"
)

// audioExtensions defines supported audio file extensions.
// Package-level to avoid allocation on every isAudioFile call.
var audioExtensions = map[string]bool{
	".flac": true,
	".mp3":  true,
	".wav":  true,
	".aiff": true,
	".aif":  true,
	".ogg":  true,
	".m4a":  true,
	".aac":  true,
	".wma":  true,
	".dsf":  true,
	".dff":  true,
	".dsd":  true,
	".ape":  true,
	".wv":   true,
	".mpc":  true,
	".opus": true,
	".alac": true,
}

// Service handles player operations.
type Service struct {
	mpd *mpd.Client
}

// NewService creates a new player service.
func NewService(mpdClient *mpd.Client) *Service {
	return &Service{
		mpd: mpdClient,
	}
}

// GetState returns the current player state in Volumio-compatible format.
func (s *Service) GetState() (map[string]interface{}, error) {
	status, err := s.mpd.Status()
	if err != nil {
		return nil, err
	}

	song, err := s.mpd.CurrentSong()
	if err != nil {
		// Not fatal - might not have a song playing
		song = make(map[string]string)
	}

	state := s.buildState(status, song)
	return state, nil
}

// buildState converts MPD status and song to Volumio-compatible state.
func (s *Service) buildState(status, song map[string]string) map[string]interface{} {
	state := make(map[string]interface{})

	// Playback status
	mpdState := status["state"]
	switch mpdState {
	case "play":
		state["status"] = "play"
	case "pause":
		state["status"] = "pause"
	default:
		state["status"] = "stop"
	}

	// Position in queue
	if pos, err := strconv.Atoi(status["song"]); err == nil {
		state["position"] = pos
	} else {
		state["position"] = 0
	}

	// Seek position in milliseconds (MPD returns seconds with decimal)
	if elapsed, err := strconv.ParseFloat(status["elapsed"], 64); err == nil {
		state["seek"] = int(elapsed * 1000)
	} else {
		state["seek"] = 0
	}

	// Duration in seconds
	if duration, err := strconv.ParseFloat(status["duration"], 64); err == nil {
		state["duration"] = int(duration)
	} else if duration, err := strconv.ParseFloat(song["Time"], 64); err == nil {
		state["duration"] = int(duration)
	} else {
		state["duration"] = 0
	}

	// Volume
	if vol, err := strconv.Atoi(status["volume"]); err == nil {
		state["volume"] = vol
	} else {
		state["volume"] = 100
	}

	// Playback options
	state["random"] = status["random"] == "1"
	state["repeat"] = status["repeat"] == "1"
	state["repeatSingle"] = status["single"] == "1"
	state["consume"] = status["consume"] == "1"
	state["mute"] = false // MPD doesn't have mute, we'd track this separately

	// Track metadata
	state["title"] = song["Title"]
	if state["title"] == "" {
		// Use filename if no title tag
		if file := song["file"]; file != "" {
			parts := strings.Split(file, "/")
			state["title"] = parts[len(parts)-1]
		} else {
			state["title"] = ""
		}
	}

	state["artist"] = song["Artist"]
	state["album"] = song["Album"]
	state["uri"] = song["file"]

	// Album art - we'll need to implement albumart endpoint
	if file := song["file"]; file != "" {
		state["albumart"] = arturl.AlbumArt(file)
	} else {
		state["albumart"] = ""
	}

	// Audio format info
	if audio := status["audio"]; audio != "" {
		// Format: samplerate:bits:channels (e.g., "96000:24:2")
		parts := strings.Split(audio, ":")
		if len(parts) >= 2 {
			state["samplerate"] = parts[0]
			state["bitdepth"] = parts[1]
		}
		if len(parts) >= 3 {
			state["channels"] = parts[2]
		}
	}

	// Track type from file extension
	if file := song["file"]; file != "" {
		if idx := strings.LastIndex(file, "."); idx != -1 {
			state["trackType"] = strings.ToLower(file[idx+1:])
		}
	}

	// Service identifier
	state["service"] = "mpd"

	// Bit-perfect indicator (we're always bit-perfect with our config)
	state["bitperfect"] = true

	// Volatile state (for external services like Spotify)
	state["volatile"] = false

	// Stream info (for internet radio)
	state["stream"] = song["Name"] // Internet radio stream name

	// Disable volume control indicator (when mixer_type is none)
	state["disableVolumeControl"] = status["volume"] == "-1"

	return state
}

// Play starts playback at the given position, or resumes if pos < 0.
func (s *Service) Play(pos int) error {
	log.Info().Int("position", pos).Msg("Play")
	return s.mpd.Play(pos)
}

// Pause pauses playback.
func (s *Service) Pause() error {
	log.Info().Msg("Pause")
	return s.mpd.Pause(true)
}

// Stop stops playback.
func (s *Service) Stop() error {
	log.Info().Msg("Stop")
	return s.mpd.Stop()
}

// Next plays the next track.
func (s *Service) Next() error {
	log.Info().Msg("Next")
	return s.mpd.Next()
}

// Previous plays the previous track.
func (s *Service) Previous() error {
	log.Info().Msg("Previous")
	return s.mpd.Previous()
}

// Seek seeks to position in seconds.
func (s *Service) Seek(pos int) error {
	log.Info().Int("position", pos).Msg("Seek")
	return s.mpd.Seek(pos)
}

// SetVolume sets the volume (0-100).
func (s *Service) SetVolume(vol int) error {
	log.Info().Int("volume", vol).Msg("SetVolume")
	return s.mpd.SetVolume(vol)
}

// SetRandom sets shuffle/random mode.
func (s *Service) SetRandom(on bool) error {
	log.Info().Bool("random", on).Msg("SetRandom")
	return s.mpd.SetRandom(on)
}

// SetRepeat sets repeat mode.
func (s *Service) SetRepeat(on, single bool) error {
	log.Info().Bool("repeat", on).Bool("single", single).Msg("SetRepeat")
	if err := s.mpd.SetRepeat(on); err != nil {
		return err
	}
	return s.mpd.SetSingle(single)
}

// GetQueue returns the current queue in Volumio-compatible format.
func (s *Service) GetQueue() ([]map[string]interface{}, error) {
	playlist, err := s.mpd.PlaylistInfo()
	if err != nil {
		return nil, err
	}

	queue := make([]map[string]interface{}, len(playlist))
	for i, song := range playlist {
		item := make(map[string]interface{})
		item["uri"] = song["file"]
		item["title"] = song["Title"]
		if item["title"] == "" {
			// Use filename if no title — strip path and extension
			parts := strings.Split(song["file"], "/")
			name := parts[len(parts)-1]
			if idx := strings.LastIndex(name, "."); idx > 0 {
				name = name[:idx]
			}
			item["title"] = name
		}
		item["artist"] = song["Artist"]
		item["album"] = song["Album"]
		item["service"] = "mpd"

		if duration, err := strconv.Atoi(song["Time"]); err == nil {
			item["duration"] = duration
		}

		// Track type from extension
		if file := song["file"]; file != "" {
			if idx := strings.LastIndex(file, "."); idx != -1 {
				item["trackType"] = strings.ToLower(file[idx+1:])
			}
		}

		// Album art
		if file := song["file"]; file != "" {
			item["albumart"] = arturl.AlbumArt(file)
		}

		queue[i] = item
	}

	return queue, nil
}

// ClearQueue clears the queue.
func (s *Service) ClearQueue() error {
	log.Info().Msg("ClearQueue")
	return s.mpd.Clear()
}

// AddToQueue adds a URI to the queue.
func (s *Service) AddToQueue(uri string) error {
	log.Info().Str("uri", uri).Msg("AddToQueue")
	return s.mpd.Add(uri)
}

// BrowseLibrary returns directory contents in Volumio-compatible format.
func (s *Service) BrowseLibrary(uri string) (map[string]interface{}, error) {
	// Handle special URIs
	if uri == "" || uri == "music-library" {
		// Root of music library - list MPD database root
		uri = ""
	} else if strings.HasPrefix(uri, "music-library/") {
		// Strip the music-library prefix to get the actual path
		uri = strings.TrimPrefix(uri, "music-library/")
	}

	log.Info().Str("uri", uri).Msg("BrowseLibrary")

	entries, err := s.mpd.ListInfo(uri)
	if err != nil {
		log.Error().Err(err).Str("uri", uri).Msg("Failed to list directory")
		return nil, err
	}

	items := make([]map[string]interface{}, 0, len(entries))
	for _, entry := range entries {
		item := s.entryToBrowseItem(entry, uri)
		if item != nil {
			items = append(items, item)
		}
	}

	// Build Volumio-compatible response
	response := map[string]interface{}{
		"navigation": map[string]interface{}{
			"lists": []map[string]interface{}{
				{
					"title":              "Music Library",
					"icon":               "fa fa-folder-open-o",
					"availableListViews": []string{"list", "grid"},
					"items":              items,
				},
			},
		},
	}

	// Add prev navigation if not at root
	if uri != "" {
		prevUri := "music-library"
		if idx := strings.LastIndex(uri, "/"); idx != -1 {
			prevUri = "music-library/" + uri[:idx]
		}
		response["navigation"].(map[string]interface{})["prev"] = map[string]interface{}{
			"uri": prevUri,
		}
	}

	return response, nil
}

// entryToBrowseItem converts an MPD entry to a Volumio browse item.
func (s *Service) entryToBrowseItem(entry map[string]string, parentUri string) map[string]interface{} {
	item := make(map[string]interface{})

	// Directory entry
	if dir, ok := entry["directory"]; ok {
		item["type"] = "folder"
		item["title"] = getBaseName(dir)
		item["uri"] = "music-library/" + dir
		item["icon"] = "fa fa-folder-open-o"
		item["service"] = "mpd"
		return item
	}

	// File entry (song)
	if file, ok := entry["file"]; ok {
		item["type"] = "song"
		item["service"] = "mpd"
		item["uri"] = file

		// Title - use tag or filename
		if title := entry["Title"]; title != "" {
			item["title"] = title
		} else {
			item["title"] = getBaseName(file)
		}

		item["artist"] = entry["Artist"]
		item["album"] = entry["Album"]

		// Duration
		if duration, err := strconv.Atoi(entry["Time"]); err == nil {
			item["duration"] = duration
		}

		// Track number
		if trackNum, err := strconv.Atoi(entry["Track"]); err == nil {
			item["tracknumber"] = trackNum
		}

		// Track type from extension
		if idx := strings.LastIndex(file, "."); idx != -1 {
			item["trackType"] = strings.ToLower(file[idx+1:])
		}

		// Album art URL - the /albumart endpoint will fetch from MPD
		item["albumart"] = arturl.AlbumArt(file)

		return item
	}

	// Playlist entry
	if playlist, ok := entry["playlist"]; ok {
		item["type"] = "playlist"
		item["title"] = getBaseName(playlist)
		item["uri"] = playlist
		item["icon"] = "fa fa-list-ol"
		item["service"] = "mpd"
		return item
	}

	return nil
}

// getBaseName returns the last component of a path.
func getBaseName(path string) string {
	if idx := strings.LastIndex(path, "/"); idx != -1 {
		return path[idx+1:]
	}
	return path
}

// ReplaceAndPlay clears the queue, adds the item and its siblings, and starts playing.
// When a single track is selected, all tracks from the same folder are added to the queue,
// with the selected track playing first. This enables proper next/prev navigation.
//
// All MPD calls in this function emit per-step timing telemetry so the slow hop
// of an album-switch round-trip can be attributed (see album-switch latency
// investigation: MPD's Play(0) can stall for seconds when the first queue entry
// is an AppleDouble ghost `._*` file that MPD attempts to decode before
// auto-skipping). The function also filters AppleDouble ghosts out of both
// the per-file `Add` loop (audio-file branch) and the post-Add queue scan
// (directory branch) so playback starts on a real track immediately.
//
// We Stop() before Clear() because a rapid second ReplaceAndPlay (e.g. tap 2
// arriving while tap 1 is still negotiating with the USB DAC) caused Clear()
// to block ~34s — MPD serializes commands and won't tear the queue down while
// an active decoder is feeding from it. Stop() closes the output cleanly so
// Clear() runs against an idle decoder and returns immediately. Stop errors
// are logged-and-continued because MPD's Stop can return errors for benign
// states (e.g. already stopped); the subsequent Clear/Add/Play will surface
// any real failure.
func (s *Service) ReplaceAndPlay(uri string) error {
	overallStart := time.Now()
	log.Info().Str("uri", uri).Msg("ReplaceAndPlay")

	defer func() {
		log.Info().
			Int64("totalElapsedMs", time.Since(overallStart).Milliseconds()).
			Str("uri", uri).
			Msg("ReplaceAndPlay done")
	}()

	// Stop any in-flight playback before clearing — see function docs above.
	stopStart := time.Now()
	if err := s.mpd.Stop(); err != nil {
		// Don't bail — Stop can fail benignly if MPD is already stopped.
		log.Warn().Err(err).Int64("elapsedMs", time.Since(stopStart).Milliseconds()).Msg("mpd: stop failed (continuing)")
	} else {
		log.Info().Int64("elapsedMs", time.Since(stopStart).Milliseconds()).Msg("mpd: stopped output")
	}

	// Clear current queue
	clearStart := time.Now()
	if err := s.mpd.Clear(); err != nil {
		return err
	}
	log.Info().Int64("elapsedMs", time.Since(clearStart).Milliseconds()).Msg("mpd: cleared queue")

	// Check if this is a file (has a known audio extension) vs a directory
	if isAudioFile(uri) {
		// Get parent directory using path.Dir for URI-style forward slashes
		parentDir := path.Dir(uri)
		listStart := time.Now()
		siblings, err := s.mpd.ListInfo(parentDir)
		if err != nil {
			log.Warn().Err(err).Str("dir", parentDir).Msg("Failed to list parent directory, falling back to single track")
			// Fall back to single track
			addStart := time.Now()
			if err := s.mpd.Add(uri); err != nil {
				return err
			}
			log.Info().Int64("elapsedMs", time.Since(addStart).Milliseconds()).Msg("mpd: added directory")
			playStart := time.Now()
			err := s.mpd.Play(0)
			log.Info().Int64("elapsedMs", time.Since(playStart).Milliseconds()).Int("position", 0).Msg("mpd: started playback")
			return err
		}
		log.Info().
			Int64("elapsedMs", time.Since(listStart).Milliseconds()).
			Int("count", len(siblings)).
			Str("dir", parentDir).
			Msg("mpd: listed parent dir")

		// Collect all audio files from the directory, skipping AppleDouble
		// ghost files (`._*`) — macOS NAS shares pepper these alongside real
		// `.flac` files and MPD will index them as decode-failing tracks.
		var audioFiles []string
		ghostsFiltered := 0
		for _, item := range siblings {
			if file, ok := item["file"]; ok {
				if !isAudioFile(file) {
					continue
				}
				if isAppleDouble(file) {
					ghostsFiltered++
					continue
				}
				audioFiles = append(audioFiles, file)
			}
		}
		if ghostsFiltered > 0 {
			log.Info().Int("ghostsFiltered", ghostsFiltered).Str("dir", parentDir).Msg("AppleDouble ghosts filtered from queue")
		}

		// Handle edge case: no audio files found in directory
		if len(audioFiles) == 0 {
			log.Warn().Str("dir", parentDir).Msg("No audio files found in directory, adding single track")
			addStart := time.Now()
			if err := s.mpd.Add(uri); err != nil {
				return err
			}
			log.Info().Int64("elapsedMs", time.Since(addStart).Milliseconds()).Msg("mpd: added directory")
			playStart := time.Now()
			err := s.mpd.Play(0)
			log.Info().Int64("elapsedMs", time.Since(playStart).Milliseconds()).Int("position", 0).Msg("mpd: started playback")
			return err
		}

		// Sort files alphabetically for consistent track order
		// This works well for files named with track numbers (e.g., "01-Track.flac")
		sort.Strings(audioFiles)

		// Find the position of the selected track
		selectedPos := -1
		for i, file := range audioFiles {
			if file == uri {
				selectedPos = i
				break
			}
		}

		// Handle edge case: selected track not found (could happen if file was deleted
		// or the user explicitly tapped an AppleDouble ghost that we just filtered)
		if selectedPos < 0 {
			log.Warn().Str("uri", uri).Msg("Selected track not found in directory, playing from start")
			selectedPos = 0
		}

		// Add all files to the queue
		addLoopStart := time.Now()
		for _, file := range audioFiles {
			if err := s.mpd.Add(file); err != nil {
				log.Warn().Err(err).Str("file", file).Msg("Failed to add file to queue")
			}
		}
		log.Info().
			Int64("elapsedMs", time.Since(addLoopStart).Milliseconds()).
			Int("tracks", len(audioFiles)).
			Msg("mpd: added N tracks")

		log.Info().
			Int("totalTracks", len(audioFiles)).
			Int("startPosition", selectedPos).
			Str("startTrack", uri).
			Msg("Queued album tracks")

		// Start playing from the selected track
		playStart := time.Now()
		err = s.mpd.Play(selectedPos)
		log.Info().
			Int64("elapsedMs", time.Since(playStart).Milliseconds()).
			Int("position", selectedPos).
			Msg("mpd: started playback")
		return err
	}

	// For directories, add the directory (MPD expands it internally — much
	// faster than listing siblings + N×Add) then peek at the resulting queue
	// to skip past any leading AppleDouble ghost entries before Play.
	addStart := time.Now()
	if err := s.mpd.Add(uri); err != nil {
		return err
	}
	log.Info().Int64("elapsedMs", time.Since(addStart).Milliseconds()).Str("dir", uri).Msg("mpd: added directory")

	// Scan the new queue to find the first non-ghost entry. If the wrapper
	// fails or the queue is empty, fall back to Play(0) — better to attempt
	// playback than to silently stall.
	playPos := 0
	queueStart := time.Now()
	queue, err := s.mpd.PlaylistInfo()
	if err != nil {
		log.Warn().Err(err).Msg("Failed to read queue after directory add, falling back to Play(0)")
	} else {
		log.Info().
			Int64("elapsedMs", time.Since(queueStart).Milliseconds()).
			Int("queueLen", len(queue)).
			Msg("mpd: read queue for ghost-skip")
		// Convert []mpd.Attrs (= []map[string]string) to a form the helper accepts.
		entries := make([]map[string]string, len(queue))
		for i, e := range queue {
			entries[i] = e
		}
		idx, skipped := firstPlayableIndex(entries)
		if idx < 0 {
			log.Warn().Msg("No playable track in queue after directory add, falling back to Play(0)")
		} else {
			if skipped > 0 {
				log.Info().Int("ghostsSkipped", skipped).Str("dir", uri).Msg("AppleDouble ghosts skipped before first playable")
			}
			playPos = idx
		}
	}

	playStart := time.Now()
	err = s.mpd.Play(playPos)
	log.Info().
		Int64("elapsedMs", time.Since(playStart).Milliseconds()).
		Int("position", playPos).
		Msg("mpd: started playback")
	return err
}

// isAudioFile checks if a URI appears to be an audio file based on extension.
func isAudioFile(uri string) bool {
	ext := strings.ToLower(path.Ext(uri))
	return audioExtensions[ext]
}

// isAppleDouble reports whether uri is a macOS AppleDouble ghost file —
// the `._*` companion files that macOS writes alongside real files on NAS
// shares to carry resource forks and extended attributes. MPD indexes them
// because they pass the extension check, but they fail to decode and cause
// MPD's Play(0) to stall while it errors out and auto-skips to the next
// entry. Pattern: basename starts with `._`. Matches at any path depth,
// including a bare basename with no leading slash.
func isAppleDouble(uri string) bool {
	base := path.Base(uri)
	return strings.HasPrefix(base, "._")
}

// firstPlayableIndex returns the index of the first non-AppleDouble entry
// in a queue listing (each entry being an mpd.Attrs / map[string]string with
// a "file" key), and the count of ghost entries skipped before it. Returns
// (-1, n) if no playable entry exists. Entries without a "file" key are
// treated as playable (defensive: they shouldn't appear, but skipping them
// would be wrong).
func firstPlayableIndex(queue []map[string]string) (int, int) {
	skipped := 0
	for i, entry := range queue {
		file, ok := entry["file"]
		if !ok {
			return i, skipped
		}
		if isAppleDouble(file) {
			skipped++
			continue
		}
		return i, skipped
	}
	return -1, skipped
}

// ============================================================
// Volumio Integration Methods
// ============================================================

// Toggle toggles between play and pause based on current state.
// This is commonly used by Volumio Connect apps instead of separate play/pause commands.
func (s *Service) Toggle() error {
	status, err := s.mpd.Status()
	if err != nil {
		return err
	}

	state := status["state"]
	log.Info().Str("state", state).Msg("Toggle")

	switch state {
	case "play":
		// Currently playing -> pause
		return s.mpd.Pause(true)
	case "pause", "stop":
		// Currently paused or stopped -> resume/play
		return s.mpd.Play(-1)
	default:
		// Unknown state -> try to play
		return s.mpd.Play(-1)
	}
}

// AddAndPlay clears the queue, adds the URI, and starts playing.
// This is equivalent to replaceAndPlay but using the Volumio event name.
func (s *Service) AddAndPlay(uri string) error {
	log.Info().Str("uri", uri).Msg("AddAndPlay")
	return s.ReplaceAndPlay(uri)
}

// InsertNext adds a URI to the queue right after the currently playing track.
// If nothing is playing (currentPos == -1), it adds to position 0 (beginning of queue).
func (s *Service) InsertNext(uri string) error {
	log.Info().Str("uri", uri).Msg("InsertNext")

	// Get current position (-1 if nothing playing)
	currentPos, err := s.mpd.GetCurrentPosition()
	if err != nil {
		currentPos = -1
	}

	// Insert after current track: if currentPos is -1, insertPos becomes 0 (beginning)
	// if currentPos is 3, insertPos becomes 4 (after the current track)
	insertPos := currentPos + 1

	_, err = s.mpd.AddId(uri, insertPos)
	return err
}

// MoveQueueItem moves a track from one position to another in the queue.
func (s *Service) MoveQueueItem(from, to int) error {
	log.Info().Int("from", from).Int("to", to).Msg("MoveQueueItem")
	return s.mpd.Move(from, to)
}

// RemoveQueueItem removes a track at the specified position from the queue.
func (s *Service) RemoveQueueItem(pos int) error {
	log.Info().Int("position", pos).Msg("RemoveQueueItem")
	return s.mpd.Delete(pos)
}
