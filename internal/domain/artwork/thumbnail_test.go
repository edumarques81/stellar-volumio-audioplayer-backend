package artwork_test

import (
	"image"
	"image/jpeg"
	"os"
	"path/filepath"
	"testing"

	"github.com/edumarques81/stellar-volumio-audioplayer-backend/internal/domain/artwork"
)

func createTestImage(path string, width, height int) error {
	img := image.NewRGBA(image.Rect(0, 0, width, height))

	// Fill with some color
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			img.Set(x, y, image.White)
		}
	}

	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	return jpeg.Encode(f, img, &jpeg.Options{Quality: 90})
}

func TestThumbnailGenerator_GenerateThumbnail(t *testing.T) {
	// Create temp directories
	tmpDir, err := os.MkdirTemp("", "thumbnail_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create a test source image (800x600)
	sourcePath := filepath.Join(tmpDir, "source.jpg")
	if err := createTestImage(sourcePath, 800, 600); err != nil {
		t.Fatalf("Failed to create test image: %v", err)
	}

	gen := artwork.NewThumbnailGenerator(tmpDir)

	// Generate small thumbnail
	thumbPath, err := gen.GenerateThumbnail(sourcePath, "test-album", artwork.ThumbSmall)
	if err != nil {
		t.Fatalf("Failed to generate thumbnail: %v", err)
	}

	// Verify thumbnail exists
	info, err := os.Stat(thumbPath)
	if err != nil {
		t.Fatalf("Thumbnail file not found: %v", err)
	}

	if info.Size() == 0 {
		t.Error("Thumbnail file is empty")
	}

	// Verify thumbnail dimensions
	f, err := os.Open(thumbPath)
	if err != nil {
		t.Fatalf("Failed to open thumbnail: %v", err)
	}
	defer f.Close()

	img, _, err := image.Decode(f)
	if err != nil {
		t.Fatalf("Failed to decode thumbnail: %v", err)
	}

	bounds := img.Bounds()
	// Width should be 150 (landscape image scaled by width)
	if bounds.Dx() != 150 {
		t.Errorf("Expected thumbnail width 150, got %d", bounds.Dx())
	}
	// Height should be scaled proportionally (600/800 * 150 = 112.5 -> 112)
	srcH := 600.0
	srcW := 800.0
	targetW := 150.0
	expectedHeight := int(srcH * targetW / srcW)
	if bounds.Dy() != expectedHeight {
		t.Errorf("Expected thumbnail height %d, got %d", expectedHeight, bounds.Dy())
	}
}

func TestThumbnailGenerator_GenerateAllSizes(t *testing.T) {
	// Create temp directories
	tmpDir, err := os.MkdirTemp("", "thumbnail_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create a test source image
	sourcePath := filepath.Join(tmpDir, "source.jpg")
	if err := createTestImage(sourcePath, 1000, 1000); err != nil {
		t.Fatalf("Failed to create test image: %v", err)
	}

	gen := artwork.NewThumbnailGenerator(tmpDir)

	// Generate all sizes
	results, err := gen.GenerateAllSizes(sourcePath, "test-album")
	if err != nil {
		t.Fatalf("Failed to generate thumbnails: %v", err)
	}

	// Verify all sizes were generated
	expectedSizes := []artwork.ThumbnailSize{
		artwork.ThumbSmall,
		artwork.ThumbMedium,
		artwork.ThumbLarge,
	}

	for _, size := range expectedSizes {
		path, ok := results[size]
		if !ok {
			t.Errorf("Missing thumbnail for size %d", size)
			continue
		}

		if _, err := os.Stat(path); err != nil {
			t.Errorf("Thumbnail file missing for size %d: %v", size, err)
		}
	}
}

func TestThumbnailGenerator_CachesExisting(t *testing.T) {
	// Create temp directories
	tmpDir, err := os.MkdirTemp("", "thumbnail_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create a test source image
	sourcePath := filepath.Join(tmpDir, "source.jpg")
	if err := createTestImage(sourcePath, 500, 500); err != nil {
		t.Fatalf("Failed to create test image: %v", err)
	}

	gen := artwork.NewThumbnailGenerator(tmpDir)

	// Generate thumbnail first time
	path1, err := gen.GenerateThumbnail(sourcePath, "test-album", artwork.ThumbSmall)
	if err != nil {
		t.Fatalf("Failed to generate thumbnail: %v", err)
	}

	// Get modification time
	info1, _ := os.Stat(path1)
	mtime1 := info1.ModTime()

	// Generate again - should return cached
	path2, err := gen.GenerateThumbnail(sourcePath, "test-album", artwork.ThumbSmall)
	if err != nil {
		t.Fatalf("Failed to generate thumbnail second time: %v", err)
	}

	// Paths should be the same
	if path1 != path2 {
		t.Errorf("Expected same path, got different paths")
	}

	// Modification time should be the same (not regenerated)
	info2, _ := os.Stat(path2)
	mtime2 := info2.ModTime()

	if !mtime1.Equal(mtime2) {
		t.Error("Thumbnail was regenerated when it should have been cached")
	}
}

func TestThumbnailGenerator_CleanupThumbnails(t *testing.T) {
	// Create temp directories
	tmpDir, err := os.MkdirTemp("", "thumbnail_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create a test source image
	sourcePath := filepath.Join(tmpDir, "source.jpg")
	if err := createTestImage(sourcePath, 500, 500); err != nil {
		t.Fatalf("Failed to create test image: %v", err)
	}

	gen := artwork.NewThumbnailGenerator(tmpDir)

	// Generate all sizes
	results, err := gen.GenerateAllSizes(sourcePath, "test-album")
	if err != nil {
		t.Fatalf("Failed to generate thumbnails: %v", err)
	}

	// Verify they exist
	for _, path := range results {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("Thumbnail should exist before cleanup: %v", err)
		}
	}

	// Cleanup
	if err := gen.CleanupThumbnails("test-album"); err != nil {
		t.Fatalf("Cleanup failed: %v", err)
	}

	// Verify they're gone
	for _, path := range results {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Errorf("Thumbnail should not exist after cleanup: %s", path)
		}
	}
}
