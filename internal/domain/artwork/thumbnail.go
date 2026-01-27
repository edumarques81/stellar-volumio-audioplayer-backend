// Package artwork provides artwork resolution and caching for albums and artists.
package artwork

import (
	"fmt"
	"image"
	_ "image/gif"  // GIF decoder
	_ "image/jpeg" // JPEG decoder
	_ "image/png"  // PNG decoder
	"os"
	"path/filepath"

	"github.com/rs/zerolog/log"
	"golang.org/x/image/draw"
	_ "golang.org/x/image/webp" // WebP decoder
)

import (
	"image/jpeg"
)

// ThumbnailSize represents common thumbnail dimensions.
type ThumbnailSize int

const (
	// ThumbSmall is 150x150 pixels - for list views
	ThumbSmall ThumbnailSize = 150
	// ThumbMedium is 300x300 pixels - for grid views
	ThumbMedium ThumbnailSize = 300
	// ThumbLarge is 500x500 pixels - for detail views
	ThumbLarge ThumbnailSize = 500
)

// ThumbnailGenerator creates thumbnails from source images.
type ThumbnailGenerator struct {
	cacheDir string
}

// NewThumbnailGenerator creates a new thumbnail generator.
func NewThumbnailGenerator(cacheDir string) *ThumbnailGenerator {
	return &ThumbnailGenerator{
		cacheDir: cacheDir,
	}
}

// GenerateThumbnail creates a thumbnail of the specified size.
// Returns the path to the generated thumbnail.
func (g *ThumbnailGenerator) GenerateThumbnail(sourcePath string, albumID string, size ThumbnailSize) (string, error) {
	// Create thumbnails directory
	thumbDir := filepath.Join(g.cacheDir, "thumbs")
	if err := os.MkdirAll(thumbDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create thumbnail directory: %w", err)
	}

	// Generate output path
	thumbPath := filepath.Join(thumbDir, fmt.Sprintf("%s_%d.jpg", albumID, size))

	// Check if thumbnail already exists
	if _, err := os.Stat(thumbPath); err == nil {
		return thumbPath, nil
	}

	// Open source image
	src, err := os.Open(sourcePath)
	if err != nil {
		return "", fmt.Errorf("failed to open source image: %w", err)
	}
	defer src.Close()

	// Decode image
	img, format, err := image.Decode(src)
	if err != nil {
		return "", fmt.Errorf("failed to decode image: %w", err)
	}

	log.Debug().
		Str("source", sourcePath).
		Str("format", format).
		Int("size", int(size)).
		Msg("Generating thumbnail")

	// Create thumbnail
	thumb := g.resize(img, int(size))

	// Save thumbnail as JPEG
	out, err := os.Create(thumbPath)
	if err != nil {
		return "", fmt.Errorf("failed to create thumbnail file: %w", err)
	}
	defer out.Close()

	if err := jpeg.Encode(out, thumb, &jpeg.Options{Quality: 85}); err != nil {
		return "", fmt.Errorf("failed to encode thumbnail: %w", err)
	}

	return thumbPath, nil
}

// resize scales an image to fit within the given size while maintaining aspect ratio.
func (g *ThumbnailGenerator) resize(src image.Image, maxSize int) image.Image {
	bounds := src.Bounds()
	srcW := bounds.Dx()
	srcH := bounds.Dy()

	// Calculate new dimensions
	var newW, newH int
	if srcW > srcH {
		newW = maxSize
		newH = int(float64(srcH) * float64(maxSize) / float64(srcW))
	} else {
		newH = maxSize
		newW = int(float64(srcW) * float64(maxSize) / float64(srcH))
	}

	// Create destination image
	dst := image.NewRGBA(image.Rect(0, 0, newW, newH))

	// Scale using CatmullRom (high quality)
	draw.CatmullRom.Scale(dst, dst.Bounds(), src, src.Bounds(), draw.Over, nil)

	return dst
}

// GenerateAllSizes generates thumbnails in all standard sizes.
func (g *ThumbnailGenerator) GenerateAllSizes(sourcePath string, albumID string) (map[ThumbnailSize]string, error) {
	sizes := []ThumbnailSize{ThumbSmall, ThumbMedium, ThumbLarge}
	result := make(map[ThumbnailSize]string)

	for _, size := range sizes {
		path, err := g.GenerateThumbnail(sourcePath, albumID, size)
		if err != nil {
			log.Warn().
				Err(err).
				Str("albumID", albumID).
				Int("size", int(size)).
				Msg("Failed to generate thumbnail")
			continue
		}
		result[size] = path
	}

	return result, nil
}

// CleanupThumbnails removes all thumbnails for an album.
func (g *ThumbnailGenerator) CleanupThumbnails(albumID string) error {
	thumbDir := filepath.Join(g.cacheDir, "thumbs")
	sizes := []ThumbnailSize{ThumbSmall, ThumbMedium, ThumbLarge}

	for _, size := range sizes {
		thumbPath := filepath.Join(thumbDir, fmt.Sprintf("%s_%d.jpg", albumID, size))
		if err := os.Remove(thumbPath); err != nil && !os.IsNotExist(err) {
			log.Warn().Err(err).Str("path", thumbPath).Msg("Failed to remove thumbnail")
		}
	}

	return nil
}
