package localmusic

import (
	"fmt"
	"testing"
)

func TestSourceType_IsLocalSource(t *testing.T) {
	tests := []struct {
		source   SourceType
		expected bool
	}{
		{SourceLocal, true},
		{SourceUSB, true},
		{SourceNAS, false},
		{SourceMounted, false},
		{SourceStreaming, false},
		{SourceUnknown, false},
	}

	for _, tt := range tests {
		t.Run(string(tt.source), func(t *testing.T) {
			if got := tt.source.IsLocalSource(); got != tt.expected {
				t.Errorf("SourceType(%q).IsLocalSource() = %v, want %v", tt.source, got, tt.expected)
			}
		})
	}
}

func TestPathClassifier_GetSourceType(t *testing.T) {
	classifier := NewPathClassifier("/var/lib/mpd/music")

	tests := []struct {
		name     string
		uri      string
		expected SourceType
	}{
		// Streaming URIs
		{"Qobuz streaming", "qobuz://album/12345", SourceStreaming},
		{"Tidal streaming", "tidal://track/67890", SourceStreaming},
		{"Spotify streaming", "spotify://playlist/abc", SourceStreaming},

		// NAS paths
		{"NAS root", "NAS/MyNas/Album/track.flac", SourceNAS},
		{"NAS with music-library prefix", "music-library/NAS/Share/track.mp3", SourceNAS},
		{"NAS deep path", "NAS/Server/Music/Artist/Album/01-Track.dsf", SourceNAS},

		// USB paths
		{"USB root", "USB/MyDrive/Album/track.flac", SourceUSB},
		{"USB with music-library prefix", "music-library/USB/Drive/track.mp3", SourceUSB},
		{"USB deep path", "USB/Drive1/Music/Artist/Album/track.wav", SourceUSB},

		// Internal/Local paths
		{"INTERNAL root", "INTERNAL/Album/track.flac", SourceLocal},
		{"INTERNAL with prefix", "music-library/INTERNAL/Music/track.mp3", SourceLocal},
		{"INTERNAL deep path", "INTERNAL/Artist/Album/01-Track.dsf", SourceLocal},

		// Unknown/default paths (treated as local)
		{"Direct path", "Artist/Album/track.flac", SourceLocal},
		{"Root level file", "track.mp3", SourceLocal},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := classifier.GetSourceType(tt.uri)
			if got != tt.expected {
				t.Errorf("GetSourceType(%q) = %v, want %v", tt.uri, got, tt.expected)
			}
		})
	}
}

func TestPathClassifier_IsLocalPath(t *testing.T) {
	classifier := NewPathClassifier("/var/lib/mpd/music")

	tests := []struct {
		name     string
		uri      string
		expected bool
	}{
		// Should be local
		{"INTERNAL path", "INTERNAL/Album/track.flac", true},
		{"USB path", "USB/Drive/Album/track.mp3", true},

		// Should NOT be local
		{"NAS path", "NAS/Server/Album/track.flac", false},
		{"Qobuz streaming", "qobuz://album/123", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := classifier.IsLocalPath(tt.uri)
			if got != tt.expected {
				t.Errorf("IsLocalPath(%q) = %v, want %v", tt.uri, got, tt.expected)
			}
		})
	}
}

func TestPathClassifier_FilterLocalOnly(t *testing.T) {
	classifier := NewPathClassifier("/var/lib/mpd/music")

	uris := []string{
		"INTERNAL/Album1/track.flac",      // local
		"USB/Drive/Album2/track.mp3",      // local
		"NAS/Server/Album3/track.wav",     // NOT local
		"qobuz://album/12345",             // NOT local
		"INTERNAL/Album4/track.dsf",       // local
		"NAS/Other/Album5/track.aiff",     // NOT local
	}

	local, filtered := classifier.FilterLocalOnly(uris)

	if len(local) != 3 {
		t.Errorf("FilterLocalOnly() returned %d local URIs, want 3", len(local))
	}

	if filtered != 3 {
		t.Errorf("FilterLocalOnly() filtered %d URIs, want 3", filtered)
	}

	// Verify correct items were kept
	expectedLocal := map[string]bool{
		"INTERNAL/Album1/track.flac": true,
		"USB/Drive/Album2/track.mp3": true,
		"INTERNAL/Album4/track.dsf":  true,
	}

	for _, uri := range local {
		if !expectedLocal[uri] {
			t.Errorf("FilterLocalOnly() incorrectly included %q", uri)
		}
	}
}

func TestIsAudioFile(t *testing.T) {
	tests := []struct {
		path     string
		expected bool
	}{
		// Audio files
		{"track.flac", true},
		{"track.mp3", true},
		{"track.dsf", true},
		{"track.DSD", true},
		{"TRACK.FLAC", true},

		// Non-audio files
		{"cover.jpg", false},
		{"playlist.m3u", false},
		{"readme.txt", false},
		{"album.cue", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			if got := isAudioFile(tt.path); got != tt.expected {
				t.Errorf("isAudioFile(%q) = %v, want %v", tt.path, got, tt.expected)
			}
		})
	}
}

func TestPlayOrigin_Values(t *testing.T) {
	// Ensure all play origin values are distinct
	origins := []PlayOrigin{
		PlayOriginManualTrack,
		PlayOriginAlbumContext,
		PlayOriginAutoplayNext,
		PlayOriginQueue,
	}

	seen := make(map[PlayOrigin]bool)
	for _, o := range origins {
		if seen[o] {
			t.Errorf("Duplicate PlayOrigin value: %q", o)
		}
		seen[o] = true
	}
}

// MockMPDClient for testing
type MockMPDClient struct {
	ListInfoResponse       map[string][]map[string]string
	ListAllInfoResponse    map[string][]map[string]string
	GetAlbumDetailsResp    map[string][]AlbumDetails
	ListInfoError          error
	GetAlbumDetailsError   error
}

func (m *MockMPDClient) ListInfo(uri string) ([]map[string]string, error) {
	if m.ListInfoError != nil {
		return nil, m.ListInfoError
	}
	if resp, ok := m.ListInfoResponse[uri]; ok {
		return resp, nil
	}
	return []map[string]string{}, nil
}

func (m *MockMPDClient) ListAllInfo(uri string) ([]map[string]string, error) {
	if resp, ok := m.ListAllInfoResponse[uri]; ok {
		return resp, nil
	}
	return []map[string]string{}, nil
}

func (m *MockMPDClient) GetAlbumDetails(basePath string) ([]AlbumDetails, error) {
	if m.GetAlbumDetailsError != nil {
		return nil, m.GetAlbumDetailsError
	}
	if resp, ok := m.GetAlbumDetailsResp[basePath]; ok {
		return resp, nil
	}
	return []AlbumDetails{}, nil
}

func TestService_GetLocalAlbums_Empty(t *testing.T) {
	mockMPD := &MockMPDClient{
		// Empty album details for both sources
		GetAlbumDetailsResp: map[string][]AlbumDetails{
			"INTERNAL": {},
			"USB":      {},
		},
	}

	service := &Service{
		mpd:        mockMPD,
		classifier: NewPathClassifier("/var/lib/mpd/music"),
	}

	resp := service.GetLocalAlbums(GetLocalAlbumsRequest{
		Sort: AlbumSortAlphabetical,
	})

	if len(resp.Albums) != 0 {
		t.Errorf("Expected 0 albums, got %d", len(resp.Albums))
	}
}

func TestService_GetLocalAlbums_WithAlbums(t *testing.T) {
	mockMPD := &MockMPDClient{
		GetAlbumDetailsResp: map[string][]AlbumDetails{
			"INTERNAL": {
				{
					Album:       "Album1",
					AlbumArtist: "Artist1",
					TrackCount:  2,
					FirstTrack:  "INTERNAL/Artist1/Album1/01-Track.flac",
					TotalTime:   300,
				},
			},
			"USB": {},
		},
	}

	service := &Service{
		mpd:        mockMPD,
		classifier: NewPathClassifier("/var/lib/mpd/music"),
	}

	resp := service.GetLocalAlbums(GetLocalAlbumsRequest{
		Sort: AlbumSortAlphabetical,
	})

	if len(resp.Albums) != 1 {
		t.Errorf("Expected 1 album, got %d", len(resp.Albums))
	}

	if len(resp.Albums) > 0 {
		album := resp.Albums[0]
		if album.Title != "Album1" {
			t.Errorf("Expected album title 'Album1', got %q", album.Title)
		}
		if album.Artist != "Artist1" {
			t.Errorf("Expected artist 'Artist1', got %q", album.Artist)
		}
		if album.Source != SourceLocal {
			t.Errorf("Expected source 'local', got %q", album.Source)
		}
		if album.TrackCount != 2 {
			t.Errorf("Expected 2 tracks, got %d", album.TrackCount)
		}
	}
}

func TestService_GetLocalAlbums_WithQuery(t *testing.T) {
	mockMPD := &MockMPDClient{
		GetAlbumDetailsResp: map[string][]AlbumDetails{
			"INTERNAL": {
				{
					Album:       "Jazz Album",
					AlbumArtist: "Jazz Artist",
					TrackCount:  1,
					FirstTrack:  "INTERNAL/Album1/track.flac",
				},
				{
					Album:       "Rock Album",
					AlbumArtist: "Rock Artist",
					TrackCount:  1,
					FirstTrack:  "INTERNAL/Album2/track.flac",
				},
			},
			"USB": {},
		},
	}

	service := &Service{
		mpd:        mockMPD,
		classifier: NewPathClassifier("/var/lib/mpd/music"),
	}

	resp := service.GetLocalAlbums(GetLocalAlbumsRequest{
		Sort:  AlbumSortAlphabetical,
		Query: "jazz",
	})

	if len(resp.Albums) != 1 {
		t.Errorf("Expected 1 album matching 'jazz', got %d", len(resp.Albums))
	}

	if len(resp.Albums) > 0 && resp.Albums[0].Title != "Jazz Album" {
		t.Errorf("Expected 'Jazz Album', got %q", resp.Albums[0].Title)
	}
}

func TestSortAlbums(t *testing.T) {
	service := &Service{
		classifier: NewPathClassifier("/var/lib/mpd/music"),
	}

	albums := []Album{
		{Title: "Zebra", Artist: "Artist C"},
		{Title: "Apple", Artist: "Artist A"},
		{Title: "Banana", Artist: "Artist B"},
	}

	// Test alphabetical sort
	albumsCopy := make([]Album, len(albums))
	copy(albumsCopy, albums)
	service.sortAlbums(albumsCopy, AlbumSortAlphabetical)

	if albumsCopy[0].Title != "Apple" || albumsCopy[1].Title != "Banana" || albumsCopy[2].Title != "Zebra" {
		t.Errorf("Alphabetical sort failed: got %v", albumsCopy)
	}

	// Test artist sort
	copy(albumsCopy, albums)
	service.sortAlbums(albumsCopy, AlbumSortByArtist)

	if albumsCopy[0].Artist != "Artist A" || albumsCopy[1].Artist != "Artist B" || albumsCopy[2].Artist != "Artist C" {
		t.Errorf("Artist sort failed: got %v", albumsCopy)
	}
}

func TestService_GetAlbumTracks_EmptyURI(t *testing.T) {
	mockMPD := &MockMPDClient{}
	service := &Service{
		mpd:        mockMPD,
		classifier: NewPathClassifier("/var/lib/mpd/music"),
	}

	resp := service.GetAlbumTracks(GetAlbumTracksRequest{AlbumURI: ""})

	if resp.Error != "album URI is required" {
		t.Errorf("Expected error 'album URI is required', got %q", resp.Error)
	}
	if len(resp.Tracks) != 0 {
		t.Errorf("Expected 0 tracks, got %d", len(resp.Tracks))
	}
}

func TestService_GetAlbumTracks_DirectoryNotFound(t *testing.T) {
	mockMPD := &MockMPDClient{
		ListInfoError: fmt.Errorf("directory not found"),
	}
	service := &Service{
		mpd:        mockMPD,
		classifier: NewPathClassifier("/var/lib/mpd/music"),
	}

	resp := service.GetAlbumTracks(GetAlbumTracksRequest{AlbumURI: "INTERNAL/NonExistent"})

	if resp.Error == "" {
		t.Error("Expected error for non-existent directory")
	}
	if resp.AlbumURI != "INTERNAL/NonExistent" {
		t.Errorf("Expected albumUri 'INTERNAL/NonExistent', got %q", resp.AlbumURI)
	}
}

func TestService_GetAlbumTracks_WithTracks(t *testing.T) {
	mockMPD := &MockMPDClient{
		ListInfoResponse: map[string][]map[string]string{
			"INTERNAL/Artist/Album": {
				{
					"file":   "INTERNAL/Artist/Album/01-Track1.flac",
					"Title":  "Track One",
					"Artist": "Test Artist",
					"Album":  "Test Album",
					"Track":  "1",
					"Time":   "240",
				},
				{
					"file":   "INTERNAL/Artist/Album/02-Track2.flac",
					"Title":  "Track Two",
					"Artist": "Test Artist",
					"Album":  "Test Album",
					"Track":  "2",
					"Time":   "180",
				},
				{
					"file":      "INTERNAL/Artist/Album/cover.jpg",
					"directory": "", // Non-audio file
				},
			},
		},
	}

	service := &Service{
		mpd:        mockMPD,
		classifier: NewPathClassifier("/var/lib/mpd/music"),
	}

	resp := service.GetAlbumTracks(GetAlbumTracksRequest{AlbumURI: "INTERNAL/Artist/Album"})

	if resp.Error != "" {
		t.Errorf("Unexpected error: %q", resp.Error)
	}
	if len(resp.Tracks) != 2 {
		t.Errorf("Expected 2 tracks, got %d", len(resp.Tracks))
	}
	if resp.TotalCount != 2 {
		t.Errorf("Expected TotalCount 2, got %d", resp.TotalCount)
	}
	if resp.AlbumURI != "INTERNAL/Artist/Album" {
		t.Errorf("Expected albumUri 'INTERNAL/Artist/Album', got %q", resp.AlbumURI)
	}

	// Verify tracks are sorted by track number
	if len(resp.Tracks) >= 2 {
		if resp.Tracks[0].TrackNumber != 1 {
			t.Errorf("First track should have TrackNumber 1, got %d", resp.Tracks[0].TrackNumber)
		}
		if resp.Tracks[1].TrackNumber != 2 {
			t.Errorf("Second track should have TrackNumber 2, got %d", resp.Tracks[1].TrackNumber)
		}
	}

	// Verify track metadata
	if len(resp.Tracks) > 0 {
		track := resp.Tracks[0]
		if track.Title != "Track One" {
			t.Errorf("Expected title 'Track One', got %q", track.Title)
		}
		if track.Artist != "Test Artist" {
			t.Errorf("Expected artist 'Test Artist', got %q", track.Artist)
		}
		if track.Duration != 240 {
			t.Errorf("Expected duration 240, got %d", track.Duration)
		}
		if track.Source != SourceLocal {
			t.Errorf("Expected source 'local', got %q", track.Source)
		}
	}
}

func TestService_GetAlbumTracks_TrackNumberFormats(t *testing.T) {
	mockMPD := &MockMPDClient{
		ListInfoResponse: map[string][]map[string]string{
			"USB/Album": {
				{
					"file":  "USB/Album/track1.flac",
					"Title": "Track A",
					"Track": "3/12", // Track X of Y format
					"Time":  "200",
				},
				{
					"file":  "USB/Album/track2.flac",
					"Title": "Track B",
					"Track": "1", // Simple format
					"Time":  "150",
				},
			},
		},
	}

	service := &Service{
		mpd:        mockMPD,
		classifier: NewPathClassifier("/var/lib/mpd/music"),
	}

	resp := service.GetAlbumTracks(GetAlbumTracksRequest{AlbumURI: "USB/Album"})

	if len(resp.Tracks) != 2 {
		t.Errorf("Expected 2 tracks, got %d", len(resp.Tracks))
	}

	// Tracks should be sorted by track number (1 before 3)
	if len(resp.Tracks) >= 2 {
		if resp.Tracks[0].TrackNumber != 1 {
			t.Errorf("First track should be track 1, got %d", resp.Tracks[0].TrackNumber)
		}
		if resp.Tracks[1].TrackNumber != 3 {
			t.Errorf("Second track should be track 3, got %d", resp.Tracks[1].TrackNumber)
		}
	}
}

func TestService_GetAlbumTracks_MissingMetadata(t *testing.T) {
	mockMPD := &MockMPDClient{
		ListInfoResponse: map[string][]map[string]string{
			"INTERNAL/Album": {
				{
					"file": "INTERNAL/Album/01 - Some Song.flac",
					// No Title, Artist, Track, or Time metadata
				},
			},
		},
	}

	service := &Service{
		mpd:        mockMPD,
		classifier: NewPathClassifier("/var/lib/mpd/music"),
	}

	resp := service.GetAlbumTracks(GetAlbumTracksRequest{AlbumURI: "INTERNAL/Album"})

	if len(resp.Tracks) != 1 {
		t.Errorf("Expected 1 track, got %d", len(resp.Tracks))
	}

	// Should fallback to filename for title
	if len(resp.Tracks) > 0 {
		track := resp.Tracks[0]
		if track.Title != "01 - Some Song" {
			t.Errorf("Expected title '01 - Some Song' (from filename), got %q", track.Title)
		}
		if track.TrackNumber != 0 {
			t.Errorf("Expected TrackNumber 0 (missing), got %d", track.TrackNumber)
		}
		if track.Duration != 0 {
			t.Errorf("Expected Duration 0 (missing), got %d", track.Duration)
		}
	}
}

func TestService_GetAlbumTracks_DurationFormats(t *testing.T) {
	mockMPD := &MockMPDClient{
		ListInfoResponse: map[string][]map[string]string{
			"INTERNAL/Album": {
				{
					"file":     "INTERNAL/Album/track1.flac",
					"Title":    "Track with Time",
					"Time":     "300", // Integer seconds
					"duration": "",
				},
				{
					"file":     "INTERNAL/Album/track2.flac",
					"Title":    "Track with duration",
					"duration": "245.5", // Float seconds
				},
			},
		},
	}

	service := &Service{
		mpd:        mockMPD,
		classifier: NewPathClassifier("/var/lib/mpd/music"),
	}

	resp := service.GetAlbumTracks(GetAlbumTracksRequest{AlbumURI: "INTERNAL/Album"})

	if len(resp.Tracks) != 2 {
		t.Errorf("Expected 2 tracks, got %d", len(resp.Tracks))
	}

	// Check Time format parsing
	if len(resp.Tracks) > 0 && resp.Tracks[0].Duration != 300 {
		t.Errorf("Expected duration 300 from Time field, got %d", resp.Tracks[0].Duration)
	}

	// Check duration format parsing (float truncated to int)
	if len(resp.Tracks) > 1 && resp.Tracks[1].Duration != 245 {
		t.Errorf("Expected duration 245 from duration field, got %d", resp.Tracks[1].Duration)
	}
}

func TestService_GetAlbumTracks_USBSource(t *testing.T) {
	mockMPD := &MockMPDClient{
		ListInfoResponse: map[string][]map[string]string{
			"USB/Drive/Album": {
				{
					"file":  "USB/Drive/Album/track.flac",
					"Title": "USB Track",
				},
			},
		},
	}

	service := &Service{
		mpd:        mockMPD,
		classifier: NewPathClassifier("/var/lib/mpd/music"),
	}

	resp := service.GetAlbumTracks(GetAlbumTracksRequest{AlbumURI: "USB/Drive/Album"})

	if len(resp.Tracks) != 1 {
		t.Errorf("Expected 1 track, got %d", len(resp.Tracks))
	}

	if len(resp.Tracks) > 0 && resp.Tracks[0].Source != SourceUSB {
		t.Errorf("Expected source 'usb', got %q", resp.Tracks[0].Source)
	}
}

func TestService_GetAlbumTracks_AlbumArt(t *testing.T) {
	mockMPD := &MockMPDClient{
		ListInfoResponse: map[string][]map[string]string{
			"INTERNAL/Album": {
				{
					"file":  "INTERNAL/Album/track.flac",
					"Title": "Track",
				},
			},
		},
	}

	service := &Service{
		mpd:        mockMPD,
		classifier: NewPathClassifier("/var/lib/mpd/music"),
	}

	resp := service.GetAlbumTracks(GetAlbumTracksRequest{AlbumURI: "INTERNAL/Album"})

	if len(resp.Tracks) > 0 {
		expectedArt := "/albumart?path=INTERNAL/Album/track.flac"
		if resp.Tracks[0].AlbumArt != expectedArt {
			t.Errorf("Expected albumArt %q, got %q", expectedArt, resp.Tracks[0].AlbumArt)
		}
	}
}
