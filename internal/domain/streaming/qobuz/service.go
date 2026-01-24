package qobuz

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"github.com/edumarques81/stellar-volumio-audioplayer-backend/internal/domain/streaming"
	"github.com/markhc/gobuz"
)

const (
	// QobuzServiceName is the identifier for the Qobuz service.
	QobuzServiceName = "qobuz"

	// QobuzIconPath is the path to the Qobuz icon.
	QobuzIconPath = "/albumart?sourceicon=music_service/qobuz/qobuz.svg"
)

// Service implements the Qobuz streaming service.
type Service struct {
	api        *gobuz.QobuzAPI
	config     *Config
	configPath string
	mu         sync.RWMutex
	status     *streaming.StreamingStatus
}

// Config holds Qobuz-specific configuration.
type Config struct {
	Email             string                   `json:"email,omitempty"`
	EncryptedPassword string                   `json:"encrypted_password,omitempty"`
	AuthToken         string                   `json:"auth_token,omitempty"`
	AppCredentials    *WebPlayerCredentials    `json:"app_credentials,omitempty"`
}

// NewService creates a new Qobuz service instance.
func NewService(configPath string) (*Service, error) {
	s := &Service{
		configPath: configPath,
		config:     &Config{},
		status: &streaming.StreamingStatus{
			LoggedIn: false,
		},
	}

	// Load existing config if available
	if err := s.loadConfig(); err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("failed to load config: %w", err)
	}

	// Initialize API with saved credentials if available
	if s.config.AppCredentials != nil {
		s.api = gobuz.NewQobuzAPI(
			gobuz.WithApplicationCredentials(
				s.config.AppCredentials.AppID,
				s.config.AppCredentials.AppSecret,
			),
		)

		// Restore auth token if available
		if s.config.AuthToken != "" {
			s.api = gobuz.NewQobuzAPI(
				gobuz.WithApplicationCredentials(
					s.config.AppCredentials.AppID,
					s.config.AppCredentials.AppSecret,
				),
				gobuz.WithAuthToken(s.config.AuthToken),
			)
			s.status.LoggedIn = true
			s.status.Email = s.config.Email
		}
	}

	return s, nil
}

// Name returns the service name.
func (s *Service) Name() string {
	return QobuzServiceName
}

// GetBrowseSource returns the browse source entry for Qobuz.
func (s *Service) GetBrowseSource() *streaming.StreamingSource {
	if !s.IsLoggedIn() {
		return nil
	}
	return &streaming.StreamingSource{
		Name:       "Qobuz",
		URI:        "qobuz://",
		PluginType: "music_service",
		PluginName: QobuzServiceName,
		AlbumArt:   QobuzIconPath,
		Icon:       "fa-qobuz",
	}
}

// IsLoggedIn returns true if the user is authenticated.
func (s *Service) IsLoggedIn() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.status.LoggedIn
}

// GetStatus returns the current status of the service.
func (s *Service) GetStatus() *streaming.StreamingStatus {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return &streaming.StreamingStatus{
		LoggedIn:     s.status.LoggedIn,
		Email:        s.status.Email,
		Subscription: s.status.Subscription,
		Country:      s.status.Country,
	}
}

// Login authenticates the user with Qobuz.
func (s *Service) Login(email, password string) (*streaming.LoginResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Ensure we have API credentials
	if s.api == nil {
		// Try to extract credentials from web player
		creds, err := ExtractWebPlayerCredentials()
		if err != nil {
			return &streaming.LoginResult{
				Success: false,
				Error:   fmt.Sprintf("failed to get API credentials: %v", err),
			}, nil
		}

		s.config.AppCredentials = creds
		s.api = gobuz.NewQobuzAPI(
			gobuz.WithApplicationCredentials(creds.AppID, creds.AppSecret),
		)
	}

	// Attempt login
	if err := s.api.Login(email, password); err != nil {
		return &streaming.LoginResult{
			Success: false,
			Error:   fmt.Sprintf("login failed: %v", err),
		}, nil
	}

	// Update status
	s.status.LoggedIn = true
	s.status.Email = email

	// Save credentials
	s.config.Email = email
	// TODO: Encrypt password before storing
	s.config.EncryptedPassword = password // Plain text for now - MUST BE ENCRYPTED
	// Note: gobuz doesn't expose the auth token directly, we'd need to modify it or track it ourselves

	if err := s.saveConfig(); err != nil {
		// Log but don't fail login
		fmt.Printf("Warning: failed to save config: %v\n", err)
	}

	return &streaming.LoginResult{
		Success: true,
		Message: "Successfully logged in to Qobuz",
		Status:  s.GetStatus(),
	}, nil
}

// Logout clears the user session.
func (s *Service) Logout() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.status.LoggedIn = false
	s.status.Email = ""
	s.status.Subscription = ""

	// Clear saved credentials
	s.config.AuthToken = ""
	s.config.EncryptedPassword = ""
	s.config.Email = ""

	// Keep app credentials for future logins
	return s.saveConfig()
}

// HandleBrowseURI handles a browse request for Qobuz.
func (s *Service) HandleBrowseURI(uri string) (*streaming.BrowseResult, error) {
	if !s.IsLoggedIn() {
		return nil, fmt.Errorf("not logged in to Qobuz")
	}

	// Parse the URI
	// qobuz:// - root
	// qobuz://myalbums - user's albums
	// qobuz://myplaylists - user's playlists
	// qobuz://featured - featured content
	// qobuz://album/{id} - album tracks
	// qobuz://artist/{id} - artist albums
	// qobuz://playlist/{id} - playlist tracks

	path := strings.TrimPrefix(uri, "qobuz://")

	switch {
	case path == "" || path == "/":
		return s.browseRoot()
	case path == "myalbums":
		return s.browseMyAlbums()
	case path == "myplaylists":
		return s.browseMyPlaylists()
	case path == "featured":
		return s.browseFeatured()
	case strings.HasPrefix(path, "album/"):
		albumID := strings.TrimPrefix(path, "album/")
		return s.browseAlbum(albumID)
	case strings.HasPrefix(path, "artist/"):
		artistID := strings.TrimPrefix(path, "artist/")
		return s.browseArtist(artistID)
	case strings.HasPrefix(path, "playlist/"):
		playlistID := strings.TrimPrefix(path, "playlist/")
		return s.browsePlaylist(playlistID)
	default:
		return nil, fmt.Errorf("unknown Qobuz URI: %s", uri)
	}
}

// Search searches for content on Qobuz.
func (s *Service) Search(query string, limit int) (*streaming.BrowseResult, error) {
	if !s.IsLoggedIn() {
		return nil, fmt.Errorf("not logged in to Qobuz")
	}

	if limit <= 0 {
		limit = 50
	}

	var items []streaming.BrowseItem

	// Search albums
	albumResults, err := s.api.SearchAlbums(query).WithLimit(limit).Run()
	if err == nil && albumResults != nil {
		for _, album := range albumResults.Albums.Items {
			artistName := ""
			if album.Artist != nil {
				artistName = album.Artist.Name
			}
			items = append(items, streaming.BrowseItem{
				Service:  QobuzServiceName,
				Type:     "album",
				Title:    album.Title,
				Artist:   artistName,
				AlbumArt: album.Image.Large,
				URI:      fmt.Sprintf("qobuz://album/%s", album.ID),
				Year:     album.ReleasedAt.Year(),
			})
		}
	}

	// Search artists
	artistResults, err := s.api.SearchArtists(query).WithLimit(limit).Run()
	if err == nil && artistResults != nil {
		for _, artist := range artistResults.Artists.Items {
			items = append(items, streaming.BrowseItem{
				Service:  QobuzServiceName,
				Type:     "artist",
				Title:    artist.Name,
				AlbumArt: artist.Image.Large,
				URI:      fmt.Sprintf("qobuz://artist/%d", artist.ID),
			})
		}
	}

	// Search tracks
	trackResults, err := s.api.SearchTracks(query).WithLimit(limit).Run()
	if err == nil && trackResults != nil {
		for _, track := range trackResults.Tracks.Items {
			items = append(items, streaming.BrowseItem{
				Service:  QobuzServiceName,
				Type:     "song",
				Title:    track.Title,
				Artist:   track.Performer.Name,
				Album:    track.Album.Title,
				AlbumArt: track.Album.Image.Large,
				URI:      fmt.Sprintf("qobuz://track/%d", track.ID),
				Duration: track.Duration,
			})
		}
	}

	return &streaming.BrowseResult{
		Navigation: streaming.Navigation{
			Lists: []streaming.BrowseList{
				{
					Title: fmt.Sprintf("Search: %s", query),
					Items: items,
				},
			},
			IsSearch: true,
		},
	}, nil
}

// GetStreamURL returns the streaming URL for a track.
func (s *Service) GetStreamURL(trackID string) (*streaming.TrackStreamInfo, error) {
	if !s.IsLoggedIn() {
		return nil, fmt.Errorf("not logged in to Qobuz")
	}

	// Convert trackID from string to int
	trackIDInt, err := strconv.Atoi(trackID)
	if err != nil {
		return nil, fmt.Errorf("invalid track ID: %w", err)
	}

	// Always request highest quality (24-bit/192kHz FLAC)
	// Format IDs: 5=MP3, 6=FLAC16, 7=FLAC24/96, 27=FLAC24/192
	fileURL, err := s.api.GetTrackFileUrl(trackIDInt, gobuz.TrackFormatHiRes24Bit192Khz)
	if err != nil {
		// Fall back to 24/96 if 24/192 not available
		fileURL, err = s.api.GetTrackFileUrl(trackIDInt, gobuz.TrackFormatHiRes24Bit96Khz)
		if err != nil {
			// Fall back to 16-bit FLAC
			fileURL, err = s.api.GetTrackFileUrl(trackIDInt, gobuz.TrackFormatFLAC)
			if err != nil {
				return nil, fmt.Errorf("failed to get stream URL: %w", err)
			}
		}
	}

	return &streaming.TrackStreamInfo{
		URL:        fileURL.URL,
		Format:     "flac",
		BitDepth:   fileURL.BitDepth,
		SampleRate: int(fileURL.SamplingRate),
		Duration:   fileURL.Duration,
		MimeType:   fileURL.MimeType,
	}, nil
}

// browseRoot returns the root menu for Qobuz.
func (s *Service) browseRoot() (*streaming.BrowseResult, error) {
	items := []streaming.BrowseItem{
		{
			Service: QobuzServiceName,
			Type:    "folder",
			Title:   "My Albums",
			URI:     "qobuz://myalbums",
			Icon:    "fa-album",
		},
		{
			Service: QobuzServiceName,
			Type:    "folder",
			Title:   "My Playlists",
			URI:     "qobuz://myplaylists",
			Icon:    "fa-list",
		},
		{
			Service: QobuzServiceName,
			Type:    "folder",
			Title:   "Featured",
			URI:     "qobuz://featured",
			Icon:    "fa-star",
		},
	}

	return &streaming.BrowseResult{
		Navigation: streaming.Navigation{
			Lists: []streaming.BrowseList{
				{
					Title: "Qobuz",
					Items: items,
				},
			},
		},
	}, nil
}

// browseMyAlbums returns the user's albums.
func (s *Service) browseMyAlbums() (*streaming.BrowseResult, error) {
	// TODO: Implement when gobuz supports user favorites
	// For now, return empty with a message
	return &streaming.BrowseResult{
		Navigation: streaming.Navigation{
			PrevURI: "qobuz://",
			Lists: []streaming.BrowseList{
				{
					Title: "My Albums",
					Items: []streaming.BrowseItem{},
				},
			},
		},
	}, nil
}

// browseMyPlaylists returns the user's playlists.
func (s *Service) browseMyPlaylists() (*streaming.BrowseResult, error) {
	// TODO: Implement when gobuz supports user playlists
	return &streaming.BrowseResult{
		Navigation: streaming.Navigation{
			PrevURI: "qobuz://",
			Lists: []streaming.BrowseList{
				{
					Title: "My Playlists",
					Items: []streaming.BrowseItem{},
				},
			},
		},
	}, nil
}

// browseFeatured returns featured content.
func (s *Service) browseFeatured() (*streaming.BrowseResult, error) {
	// TODO: Implement featured content browsing
	return &streaming.BrowseResult{
		Navigation: streaming.Navigation{
			PrevURI: "qobuz://",
			Lists: []streaming.BrowseList{
				{
					Title: "Featured",
					Items: []streaming.BrowseItem{},
				},
			},
		},
	}, nil
}

// browseAlbum returns tracks for an album.
func (s *Service) browseAlbum(albumID string) (*streaming.BrowseResult, error) {
	album, err := s.api.GetAlbum(albumID).WithAuth().Run()
	if err != nil {
		return nil, fmt.Errorf("failed to get album: %w", err)
	}

	var items []streaming.BrowseItem
	for _, track := range album.Tracks.Items {
		items = append(items, streaming.BrowseItem{
			Service:     QobuzServiceName,
			Type:        "song",
			Title:       track.Title,
			Artist:      track.Performer.Name,
			Album:       album.Title,
			AlbumArt:    album.Image.Large,
			URI:         fmt.Sprintf("qobuz://track/%d", track.ID),
			Duration:    track.Duration,
			TrackNumber: track.TrackNumber,
		})
	}

	artistName := ""
	if album.Artist != nil {
		artistName = album.Artist.Name
	}

	return &streaming.BrowseResult{
		Navigation: streaming.Navigation{
			PrevURI: "qobuz://myalbums",
			Info: &streaming.BrowseInfo{
				URI:      fmt.Sprintf("qobuz://album/%s", albumID),
				Title:    album.Title,
				Service:  QobuzServiceName,
				Type:     "album",
				AlbumArt: album.Image.Large,
				Artist:   artistName,
				Year:     album.ReleasedAt.Year(),
			},
			Lists: []streaming.BrowseList{
				{
					Title: album.Title,
					Items: items,
				},
			},
		},
	}, nil
}

// browseArtist returns albums for an artist.
func (s *Service) browseArtist(artistID string) (*streaming.BrowseResult, error) {
	// Convert artistID from string to int
	artistIDInt, err := strconv.Atoi(artistID)
	if err != nil {
		return nil, fmt.Errorf("invalid artist ID: %w", err)
	}

	artist, err := s.api.GetArtist(artistIDInt).WithAuth().WithExtra("albums").Run()
	if err != nil {
		return nil, fmt.Errorf("failed to get artist: %w", err)
	}

	var items []streaming.BrowseItem
	for _, album := range artist.Albums.Items {
		items = append(items, streaming.BrowseItem{
			Service:  QobuzServiceName,
			Type:     "album",
			Title:    album.Title,
			Artist:   artist.Name,
			AlbumArt: album.Image.Large,
			URI:      fmt.Sprintf("qobuz://album/%s", album.ID),
			Year:     album.ReleasedAt.Year(),
		})
	}

	return &streaming.BrowseResult{
		Navigation: streaming.Navigation{
			PrevURI: "qobuz://",
			Info: &streaming.BrowseInfo{
				URI:      fmt.Sprintf("qobuz://artist/%s", artistID),
				Title:    artist.Name,
				Service:  QobuzServiceName,
				Type:     "artist",
				AlbumArt: artist.Image.Large,
			},
			Lists: []streaming.BrowseList{
				{
					Title: fmt.Sprintf("Albums by %s", artist.Name),
					Items: items,
				},
			},
		},
	}, nil
}

// browsePlaylist returns tracks for a playlist.
func (s *Service) browsePlaylist(playlistID string) (*streaming.BrowseResult, error) {
	// Convert playlistID from string to int
	playlistIDInt, err := strconv.Atoi(playlistID)
	if err != nil {
		return nil, fmt.Errorf("invalid playlist ID: %w", err)
	}

	playlist, err := s.api.GetPlaylist(playlistIDInt).WithAuth().Run()
	if err != nil {
		return nil, fmt.Errorf("failed to get playlist: %w", err)
	}

	var items []streaming.BrowseItem
	for _, track := range playlist.Tracks {
		items = append(items, streaming.BrowseItem{
			Service:  QobuzServiceName,
			Type:     "song",
			Title:    track.Title,
			Artist:   track.Performer.Name,
			Album:    track.Album.Title,
			AlbumArt: track.Album.Image.Large,
			URI:      fmt.Sprintf("qobuz://track/%d", track.ID),
			Duration: track.Duration,
		})
	}

	return &streaming.BrowseResult{
		Navigation: streaming.Navigation{
			PrevURI: "qobuz://myplaylists",
			Info: &streaming.BrowseInfo{
				URI:     fmt.Sprintf("qobuz://playlist/%s", playlistID),
				Title:   playlist.Name,
				Service: QobuzServiceName,
				Type:    "playlist",
			},
			Lists: []streaming.BrowseList{
				{
					Title: playlist.Name,
					Items: items,
				},
			},
		},
	}, nil
}

// loadConfig loads the configuration from disk.
func (s *Service) loadConfig() error {
	data, err := os.ReadFile(s.configPath)
	if err != nil {
		return err
	}

	var config Config
	if err := json.Unmarshal(data, &config); err != nil {
		return fmt.Errorf("failed to parse config: %w", err)
	}

	s.config = &config
	return nil
}

// saveConfig saves the configuration to disk.
func (s *Service) saveConfig() error {
	// Ensure directory exists
	dir := filepath.Dir(s.configPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	data, err := json.MarshalIndent(s.config, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	if err := os.WriteFile(s.configPath, data, 0600); err != nil {
		return fmt.Errorf("failed to write config: %w", err)
	}

	return nil
}
