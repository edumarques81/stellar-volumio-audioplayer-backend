package localmusic

import (
	"bufio"
	"os"
	"path"
	"strings"
	"sync"

	"github.com/rs/zerolog/log"
)

// PathClassifier classifies music file paths by source type.
type PathClassifier struct {
	mpdMusicDir string
	mountCache  map[string]string // path -> mount type cache
	cacheMu     sync.RWMutex
}

// NewPathClassifier creates a new path classifier.
func NewPathClassifier(mpdMusicDir string) *PathClassifier {
	return &PathClassifier{
		mpdMusicDir: mpdMusicDir,
		mountCache:  make(map[string]string),
	}
}

// GetSourceType determines the source type for a given URI/path.
// This is the single source of truth for source classification.
func (c *PathClassifier) GetSourceType(uri string) SourceType {
	// Handle streaming URIs
	if strings.HasPrefix(uri, "qobuz://") ||
		strings.HasPrefix(uri, "tidal://") ||
		strings.HasPrefix(uri, "spotify://") {
		return SourceStreaming
	}

	// Normalize the path
	normalizedPath := c.normalizePath(uri)

	// Check path prefixes for known source types
	// These are the canonical paths within MPD's music directory

	// NAS paths: music-library/NAS/... or NAS/...
	if strings.HasPrefix(normalizedPath, "NAS/") ||
		strings.HasPrefix(normalizedPath, "music-library/NAS/") {
		return SourceNAS
	}

	// USB paths: music-library/USB/... or USB/...
	if strings.HasPrefix(normalizedPath, "USB/") ||
		strings.HasPrefix(normalizedPath, "music-library/USB/") {
		return SourceUSB
	}

	// INTERNAL paths: music-library/INTERNAL/... or INTERNAL/...
	if strings.HasPrefix(normalizedPath, "INTERNAL/") ||
		strings.HasPrefix(normalizedPath, "music-library/INTERNAL/") {
		return SourceLocal
	}

	// If no known prefix, check if it's a mounted filesystem
	fullPath := c.getFullPath(normalizedPath)
	if c.isMountedFilesystem(fullPath) {
		mountType := c.getMountType(fullPath)
		if mountType == "cifs" || mountType == "nfs" || mountType == "smbfs" ||
			mountType == "fuse.sshfs" || mountType == "fuse.rclone" {
			return SourceMounted
		}
	}

	// Default to local for paths that don't match any network pattern
	// This covers direct paths within the MPD music directory
	return SourceLocal
}

// IsLocalPath returns true if the path is a local source (local or USB).
func (c *PathClassifier) IsLocalPath(uri string) bool {
	sourceType := c.GetSourceType(uri)
	return sourceType.IsLocalSource()
}

// IsNASPath returns true if the path is a NAS source.
func (c *PathClassifier) IsNASPath(uri string) bool {
	return c.GetSourceType(uri) == SourceNAS
}

// IsStreamingPath returns true if the path is a streaming source.
func (c *PathClassifier) IsStreamingPath(uri string) bool {
	return c.GetSourceType(uri) == SourceStreaming
}

// normalizePath normalizes a URI/path for classification.
func (c *PathClassifier) normalizePath(uri string) string {
	// Strip music-library prefix if present
	if strings.HasPrefix(uri, "music-library/") {
		uri = strings.TrimPrefix(uri, "music-library/")
	}
	return uri
}

// getFullPath converts a relative MPD path to a full filesystem path.
func (c *PathClassifier) getFullPath(relativePath string) string {
	if path.IsAbs(relativePath) {
		return relativePath
	}
	return path.Join(c.mpdMusicDir, relativePath)
}

// isMountedFilesystem checks if a path is on a mounted filesystem.
func (c *PathClassifier) isMountedFilesystem(filePath string) bool {
	mounts := c.loadMounts()
	for mountPoint := range mounts {
		if strings.HasPrefix(filePath, mountPoint) && mountPoint != "/" {
			return true
		}
	}
	return false
}

// getMountType returns the filesystem type for a given path.
func (c *PathClassifier) getMountType(filePath string) string {
	mounts := c.loadMounts()
	longestMatch := ""
	matchedType := ""

	for mountPoint, fsType := range mounts {
		if strings.HasPrefix(filePath, mountPoint) && len(mountPoint) > len(longestMatch) {
			longestMatch = mountPoint
			matchedType = fsType
		}
	}

	return matchedType
}

// loadMounts reads /proc/mounts to get current mount information.
func (c *PathClassifier) loadMounts() map[string]string {
	c.cacheMu.RLock()
	if len(c.mountCache) > 0 {
		cached := c.mountCache
		c.cacheMu.RUnlock()
		return cached
	}
	c.cacheMu.RUnlock()

	c.cacheMu.Lock()
	defer c.cacheMu.Unlock()

	// Double-check after acquiring write lock
	if len(c.mountCache) > 0 {
		return c.mountCache
	}

	mounts := make(map[string]string)

	file, err := os.Open("/proc/mounts")
	if err != nil {
		log.Warn().Err(err).Msg("Failed to read /proc/mounts, assuming all paths are local")
		return mounts
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) >= 3 {
			mountPoint := fields[1]
			fsType := fields[2]
			mounts[mountPoint] = fsType
		}
	}

	c.mountCache = mounts
	return mounts
}

// RefreshMountCache clears the mount cache to force a refresh.
func (c *PathClassifier) RefreshMountCache() {
	c.cacheMu.Lock()
	defer c.cacheMu.Unlock()
	c.mountCache = make(map[string]string)
}

// FilterLocalOnly filters a list of URIs to only include local sources.
func (c *PathClassifier) FilterLocalOnly(uris []string) (local []string, filtered int) {
	for _, uri := range uris {
		if c.IsLocalPath(uri) {
			local = append(local, uri)
		} else {
			filtered++
		}
	}
	return local, filtered
}

// ClassifyURIs classifies a list of URIs by source type.
func (c *PathClassifier) ClassifyURIs(uris []string) map[SourceType][]string {
	result := make(map[SourceType][]string)
	for _, uri := range uris {
		sourceType := c.GetSourceType(uri)
		result[sourceType] = append(result[sourceType], uri)
	}
	return result
}
