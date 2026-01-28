// Package artwork provides artwork resolution and caching for albums and artists.
package artwork

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/rs/zerolog/log"
)

// ArtworkFilenames defines common artwork filenames in priority order.
var ArtworkFilenames = []string{
	"cover",
	"folder",
	"front",
	"album",
	"artwork",
}

// ArtworkExtensions defines supported image extensions.
var ArtworkExtensions = []string{
	".jpg",
	".jpeg",
	".png",
	".webp",
}

// FilesystemFinder searches for artwork files on the filesystem.
type FilesystemFinder struct {
	musicDir  string // MPD music directory (e.g., /var/lib/mpd/music)
	maxLevels int    // Maximum parent directories to search (default: 3)
}

// NewFilesystemFinder creates a new filesystem artwork finder.
func NewFilesystemFinder(musicDir string) *FilesystemFinder {
	return &FilesystemFinder{
		musicDir:  musicDir,
		maxLevels: 3,
	}
}

// FindArtwork searches for artwork file starting from the track's directory.
// Returns the full path to the artwork file if found, empty string otherwise.
func (f *FilesystemFinder) FindArtwork(trackURI string) (string, error) {
	if trackURI == "" {
		return "", nil
	}

	// Calculate full path from relative URI
	fullPath := filepath.Join(f.musicDir, trackURI)
	trackDir := filepath.Dir(fullPath)

	// Normalize the music directory path for comparison
	musicDirAbs, err := filepath.Abs(f.musicDir)
	if err != nil {
		return "", err
	}

	log.Debug().
		Str("trackURI", trackURI).
		Str("trackDir", trackDir).
		Str("musicDir", musicDirAbs).
		Msg("Searching for artwork")

	// Search starting from track directory, going up to maxLevels parents
	currentDir := trackDir
	for level := 0; level <= f.maxLevels; level++ {
		// Check if we've gone above the music root
		currentDirAbs, err := filepath.Abs(currentDir)
		if err != nil {
			break
		}
		if !strings.HasPrefix(currentDirAbs, musicDirAbs) {
			log.Debug().
				Str("currentDir", currentDirAbs).
				Str("musicDir", musicDirAbs).
				Msg("Reached music root boundary, stopping search")
			break // Don't search outside music directory
		}

		// Search for artwork in current directory
		if artPath := f.searchDirectory(currentDir); artPath != "" {
			log.Debug().
				Str("artPath", artPath).
				Int("level", level).
				Msg("Found artwork file")
			return artPath, nil
		}

		// Move to parent directory
		parentDir := filepath.Dir(currentDir)
		if parentDir == currentDir {
			break // Reached filesystem root
		}
		currentDir = parentDir
	}

	log.Debug().
		Str("trackURI", trackURI).
		Msg("No artwork found in directory tree")
	return "", nil
}

// searchDirectory searches a single directory for artwork files.
func (f *FilesystemFinder) searchDirectory(dir string) string {
	// First, try known artwork filenames with priority order
	for _, name := range ArtworkFilenames {
		for _, ext := range ArtworkExtensions {
			// Try lowercase
			filename := name + ext
			path := filepath.Join(dir, filename)
			if fileExists(path) {
				return path
			}

			// Try capitalized (Cover.jpg)
			filenameCap := strings.Title(name) + ext
			pathCap := filepath.Join(dir, filenameCap)
			if fileExists(pathCap) {
				return pathCap
			}

			// Try uppercase (COVER.JPG)
			filenameUpper := strings.ToUpper(name) + strings.ToUpper(ext)
			pathUpper := filepath.Join(dir, filenameUpper)
			if fileExists(pathUpper) {
				return pathUpper
			}
		}
	}

	// If no standard names found, look for any image file
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		// Skip macOS AppleDouble resource fork files (._filename)
		if strings.HasPrefix(entry.Name(), "._") {
			continue
		}
		ext := strings.ToLower(filepath.Ext(entry.Name()))
		for _, validExt := range ArtworkExtensions {
			if ext == validExt {
				return filepath.Join(dir, entry.Name())
			}
		}
	}

	return ""
}

// fileExists checks if a file exists and is not a directory.
func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

// ReadArtwork reads the artwork file and returns its data.
func (f *FilesystemFinder) ReadArtwork(path string) ([]byte, error) {
	return os.ReadFile(path)
}
