package artwork_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/edumarques81/stellar-volumio-audioplayer-backend/internal/domain/artwork"
)

// MockMPDClient implements the MPDArtworkProvider interface for testing.
type MockMPDClient struct {
	albumArtData    []byte
	albumArtErr     error
	readPictureData []byte
	readPictureErr  error
}

func (m *MockMPDClient) AlbumArt(uri string) ([]byte, error) {
	return m.albumArtData, m.albumArtErr
}

func (m *MockMPDClient) ReadPicture(uri string) ([]byte, error) {
	return m.readPictureData, m.readPictureErr
}

// MockDAO implements the ArtworkDAO interface for testing.
type MockDAO struct {
	artwork *artwork.CachedArtwork
	getErr  error
	saveErr error
}

func (m *MockDAO) GetArtwork(albumID string) (*artwork.CachedArtwork, error) {
	return m.artwork, m.getErr
}

func (m *MockDAO) SaveArtwork(art *artwork.CachedArtwork) error {
	m.artwork = art
	return m.saveErr
}

func TestResolver_CacheHit(t *testing.T) {
	// Create temp directory for cache
	tmpDir, err := os.MkdirTemp("", "artwork_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create a test image file
	testImagePath := filepath.Join(tmpDir, "test_album.jpg")
	testImageData := []byte{0xFF, 0xD8, 0xFF, 0xE0} // JPEG magic bytes
	if err := os.WriteFile(testImagePath, testImageData, 0644); err != nil {
		t.Fatalf("Failed to write test image: %v", err)
	}

	// Setup mock DAO with cached artwork
	mockDAO := &MockDAO{
		artwork: &artwork.CachedArtwork{
			ID:       "test-artwork-id",
			AlbumID:  "test-album-id",
			FilePath: testImagePath,
			Source:   "mpd",
			MimeType: "image/jpeg",
		},
	}

	// MPD client should not be called for cache hit
	mockMPD := &MockMPDClient{}

	resolver := artwork.NewResolver(mockMPD, mockDAO, tmpDir)

	// Resolve should return cached artwork
	result, err := resolver.Resolve("test-album-id", "NAS/Artist/Album/track.flac")
	if err != nil {
		t.Fatalf("Resolve failed: %v", err)
	}

	if result.Source != "cache" {
		t.Errorf("Expected source 'cache', got '%s'", result.Source)
	}
	if result.FilePath != testImagePath {
		t.Errorf("Expected path '%s', got '%s'", testImagePath, result.FilePath)
	}
}

func TestResolver_MPDAlbumArt(t *testing.T) {
	// Create temp directory for cache
	tmpDir, err := os.MkdirTemp("", "artwork_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// No cached artwork
	mockDAO := &MockDAO{artwork: nil}

	// MPD returns album art
	testImageData := []byte{0xFF, 0xD8, 0xFF, 0xE0, 0x00, 0x10, 'J', 'F', 'I', 'F'}
	mockMPD := &MockMPDClient{
		albumArtData: testImageData,
	}

	resolver := artwork.NewResolver(mockMPD, mockDAO, tmpDir)

	result, err := resolver.Resolve("test-album-id", "NAS/Artist/Album/track.flac")
	if err != nil {
		t.Fatalf("Resolve failed: %v", err)
	}

	if result.Source != "mpd" {
		t.Errorf("Expected source 'mpd', got '%s'", result.Source)
	}

	// Verify file was cached
	if _, err := os.Stat(result.FilePath); os.IsNotExist(err) {
		t.Error("Artwork file should be cached on disk")
	}
}

func TestResolver_MPDReadPicture(t *testing.T) {
	// Create temp directory for cache
	tmpDir, err := os.MkdirTemp("", "artwork_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// No cached artwork
	mockDAO := &MockDAO{artwork: nil}

	// AlbumArt fails, but ReadPicture succeeds
	testImageData := []byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A} // PNG magic
	mockMPD := &MockMPDClient{
		albumArtData:    nil,
		albumArtErr:     artwork.ErrNoArtwork,
		readPictureData: testImageData,
	}

	resolver := artwork.NewResolver(mockMPD, mockDAO, tmpDir)

	result, err := resolver.Resolve("test-album-id", "NAS/Artist/Album/track.flac")
	if err != nil {
		t.Fatalf("Resolve failed: %v", err)
	}

	if result.Source != "embedded" {
		t.Errorf("Expected source 'embedded', got '%s'", result.Source)
	}

	// Verify PNG file was cached
	if _, err := os.Stat(result.FilePath); os.IsNotExist(err) {
		t.Error("Artwork file should be cached on disk")
	}
}

func TestResolver_NoArtwork(t *testing.T) {
	// Create temp directory for cache
	tmpDir, err := os.MkdirTemp("", "artwork_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// No cached artwork
	mockDAO := &MockDAO{artwork: nil}

	// Neither AlbumArt nor ReadPicture returns data
	mockMPD := &MockMPDClient{
		albumArtErr:    artwork.ErrNoArtwork,
		readPictureErr: artwork.ErrNoArtwork,
	}

	resolver := artwork.NewResolver(mockMPD, mockDAO, tmpDir)

	result, err := resolver.Resolve("test-album-id", "NAS/Artist/Album/track.flac")
	if err != nil {
		t.Fatalf("Resolve should not fail even without artwork: %v", err)
	}

	if result.Source != "placeholder" {
		t.Errorf("Expected source 'placeholder', got '%s'", result.Source)
	}
}

func TestResolver_SaveToCache(t *testing.T) {
	// Create temp directory for cache
	tmpDir, err := os.MkdirTemp("", "artwork_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// No cached artwork
	mockDAO := &MockDAO{artwork: nil}

	// MPD returns album art
	testImageData := []byte{0xFF, 0xD8, 0xFF, 0xE0, 0x00, 0x10, 'J', 'F', 'I', 'F'}
	mockMPD := &MockMPDClient{
		albumArtData: testImageData,
	}

	resolver := artwork.NewResolver(mockMPD, mockDAO, tmpDir)

	result, err := resolver.Resolve("test-album-id", "NAS/Artist/Album/track.flac")
	if err != nil {
		t.Fatalf("Resolve failed: %v", err)
	}

	// Verify file was written to disk
	data, err := os.ReadFile(result.FilePath)
	if err != nil {
		t.Fatalf("Failed to read cached file: %v", err)
	}

	if len(data) != len(testImageData) {
		t.Errorf("Cached file size mismatch: expected %d, got %d", len(testImageData), len(data))
	}

	// Verify DAO was called to save artwork metadata
	if mockDAO.artwork == nil {
		t.Error("DAO.SaveArtwork should have been called")
	} else {
		if mockDAO.artwork.AlbumID != "test-album-id" {
			t.Errorf("Expected album ID 'test-album-id', got '%s'", mockDAO.artwork.AlbumID)
		}
		if mockDAO.artwork.Source != "mpd" {
			t.Errorf("Expected source 'mpd', got '%s'", mockDAO.artwork.Source)
		}
	}
}

func TestDetectMimeType(t *testing.T) {
	tests := []struct {
		name     string
		data     []byte
		expected string
	}{
		{
			name:     "JPEG",
			data:     []byte{0xFF, 0xD8, 0xFF, 0xE0},
			expected: "image/jpeg",
		},
		{
			name:     "PNG",
			data:     []byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A},
			expected: "image/png",
		},
		{
			name:     "GIF",
			data:     []byte{'G', 'I', 'F', '8', '9', 'a'},
			expected: "image/gif",
		},
		{
			name:     "WebP",
			data:     []byte{'R', 'I', 'F', 'F', 0, 0, 0, 0, 'W', 'E', 'B', 'P'},
			expected: "image/webp",
		},
		{
			name:     "Unknown",
			data:     []byte{0x00, 0x01, 0x02, 0x03},
			expected: "application/octet-stream",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := artwork.DetectMimeType(tt.data)
			if result != tt.expected {
				t.Errorf("Expected mime type '%s', got '%s'", tt.expected, result)
			}
		})
	}
}

func TestGetExtensionForMime(t *testing.T) {
	tests := []struct {
		mime     string
		expected string
	}{
		{"image/jpeg", ".jpg"},
		{"image/png", ".png"},
		{"image/gif", ".gif"},
		{"image/webp", ".webp"},
		{"unknown/type", ".bin"},
	}

	for _, tt := range tests {
		t.Run(tt.mime, func(t *testing.T) {
			result := artwork.GetExtensionForMime(tt.mime)
			if result != tt.expected {
				t.Errorf("Expected extension '%s', got '%s'", tt.expected, result)
			}
		})
	}
}
