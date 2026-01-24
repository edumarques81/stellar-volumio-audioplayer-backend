// Package localmusic provides local music management excluding NAS and streaming sources.
package localmusic

import "time"

// SourceType represents the origin of a music file.
type SourceType string

const (
	// SourceLocal represents music on the device's internal storage.
	SourceLocal SourceType = "local"
	// SourceUSB represents music on a connected USB drive.
	SourceUSB SourceType = "usb"
	// SourceNAS represents music from a network-attached storage.
	SourceNAS SourceType = "nas"
	// SourceMounted represents other mounted network filesystems.
	SourceMounted SourceType = "mounted"
	// SourceStreaming represents streaming service content (Qobuz, Tidal, etc.).
	SourceStreaming SourceType = "streaming"
	// SourceUnknown represents an unclassified source.
	SourceUnknown SourceType = "unknown"
)

// IsLocalSource returns true if the source type is allowed in Local Music.
// Only local device storage and USB drives are considered "local".
func (s SourceType) IsLocalSource() bool {
	return s == SourceLocal || s == SourceUSB
}

// PlayOrigin represents how a track was played.
type PlayOrigin string

const (
	// PlayOriginManualTrack indicates the user explicitly selected this track.
	PlayOriginManualTrack PlayOrigin = "manual_track"
	// PlayOriginAlbumContext indicates the track played as part of album playback.
	PlayOriginAlbumContext PlayOrigin = "album_context"
	// PlayOriginAutoplayNext indicates the track played automatically after the previous track.
	PlayOriginAutoplayNext PlayOrigin = "autoplay_next"
	// PlayOriginQueue indicates the track was played from the queue.
	PlayOriginQueue PlayOrigin = "queue"
)

// Album represents a local music album.
type Album struct {
	ID         string     `json:"id"`
	Title      string     `json:"title"`
	Artist     string     `json:"artist"`
	URI        string     `json:"uri"`
	AlbumArt   string     `json:"albumArt,omitempty"`
	TrackCount int        `json:"trackCount,omitempty"`
	Source     SourceType `json:"source"`
	AddedAt    time.Time  `json:"addedAt,omitempty"`
}

// Track represents a local music track.
type Track struct {
	ID       string     `json:"id"`
	Title    string     `json:"title"`
	Artist   string     `json:"artist"`
	Album    string     `json:"album"`
	URI      string     `json:"uri"`
	Duration int        `json:"duration,omitempty"`
	AlbumArt string     `json:"albumArt,omitempty"`
	Source   SourceType `json:"source"`
}

// PlayHistoryEntry represents a record of a track being played.
type PlayHistoryEntry struct {
	ID        string     `json:"id"`
	TrackURI  string     `json:"trackURI"`
	Title     string     `json:"title"`
	Artist    string     `json:"artist"`
	Album     string     `json:"album"`
	AlbumArt  string     `json:"albumArt,omitempty"`
	Source    SourceType `json:"source"`
	Origin    PlayOrigin `json:"origin"`
	PlayedAt  time.Time  `json:"playedAt"`
	PlayCount int        `json:"playCount,omitempty"`
}

// AlbumSortOrder defines how albums should be sorted.
type AlbumSortOrder string

const (
	AlbumSortRecentlyAdded AlbumSortOrder = "recent"
	AlbumSortAlphabetical  AlbumSortOrder = "az"
	AlbumSortByArtist      AlbumSortOrder = "artist"
)

// TrackSortOrder defines how tracks should be sorted.
type TrackSortOrder string

const (
	TrackSortLastPlayed TrackSortOrder = "recent"
	TrackSortAlphabetical TrackSortOrder = "az"
	TrackSortMostPlayed   TrackSortOrder = "mostPlayed"
)

// GetLocalAlbumsRequest represents a request to get local albums.
type GetLocalAlbumsRequest struct {
	Sort  AlbumSortOrder `json:"sort"`
	Query string         `json:"query,omitempty"`
	Limit int            `json:"limit,omitempty"`
}

// GetLastPlayedRequest represents a request to get last played tracks.
type GetLastPlayedRequest struct {
	Sort  TrackSortOrder `json:"sort"`
	Limit int            `json:"limit,omitempty"`
}

// LocalAlbumsResponse represents the response for local albums.
type LocalAlbumsResponse struct {
	Albums       []Album `json:"albums"`
	TotalCount   int     `json:"totalCount"`
	FilteredOut  int     `json:"filteredOut"` // Count of non-local albums filtered out (for debugging)
}

// LastPlayedResponse represents the response for last played tracks.
type LastPlayedResponse struct {
	Tracks      []PlayHistoryEntry `json:"tracks"`
	TotalCount  int                `json:"totalCount"`
}
