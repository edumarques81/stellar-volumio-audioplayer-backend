// Package player provides the player service for audio playback control.
package player

import (
	"path"
	"sort"
	"strconv"
	"strings"

	"github.com/edumarques81/stellar-volumio-audioplayer-backend/internal/infra/mpd"
	"github.com/rs/zerolog/log"
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
		state["albumart"] = "/albumart?path=" + file
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
			// Use filename if no title
			parts := strings.Split(song["file"], "/")
			item["title"] = parts[len(parts)-1]
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
			item["albumart"] = "/albumart?path=" + file
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
		item["albumart"] = "/albumart?path=" + file

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
func (s *Service) ReplaceAndPlay(uri string) error {
	log.Info().Str("uri", uri).Msg("ReplaceAndPlay")

	// Clear current queue
	if err := s.mpd.Clear(); err != nil {
		return err
	}

	// Check if this is a file (has a known audio extension) vs a directory
	if isAudioFile(uri) {
		// Get parent directory using path.Dir for URI-style forward slashes
		parentDir := path.Dir(uri)
		siblings, err := s.mpd.ListInfo(parentDir)
		if err != nil {
			log.Warn().Err(err).Str("dir", parentDir).Msg("Failed to list parent directory, falling back to single track")
			// Fall back to single track
			if err := s.mpd.Add(uri); err != nil {
				return err
			}
			return s.mpd.Play(0)
		}

		// Collect all audio files from the directory
		var audioFiles []string
		for _, item := range siblings {
			if file, ok := item["file"]; ok {
				if isAudioFile(file) {
					audioFiles = append(audioFiles, file)
				}
			}
		}

		// Handle edge case: no audio files found in directory
		if len(audioFiles) == 0 {
			log.Warn().Str("dir", parentDir).Msg("No audio files found in directory, adding single track")
			if err := s.mpd.Add(uri); err != nil {
				return err
			}
			return s.mpd.Play(0)
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

		// Handle edge case: selected track not found (could happen if file was deleted)
		if selectedPos < 0 {
			log.Warn().Str("uri", uri).Msg("Selected track not found in directory, playing from start")
			selectedPos = 0
		}

		// Add all files to the queue
		for _, file := range audioFiles {
			if err := s.mpd.Add(file); err != nil {
				log.Warn().Err(err).Str("file", file).Msg("Failed to add file to queue")
			}
		}

		log.Info().
			Int("totalTracks", len(audioFiles)).
			Int("startPosition", selectedPos).
			Str("startTrack", uri).
			Msg("Queued album tracks")

		// Start playing from the selected track
		return s.mpd.Play(selectedPos)
	}

	// For directories, just add the directory (MPD will add all contents)
	if err := s.mpd.Add(uri); err != nil {
		return err
	}

	return s.mpd.Play(0)
}

// isAudioFile checks if a URI appears to be an audio file based on extension.
func isAudioFile(uri string) bool {
	ext := strings.ToLower(path.Ext(uri))
	return audioExtensions[ext]
}
