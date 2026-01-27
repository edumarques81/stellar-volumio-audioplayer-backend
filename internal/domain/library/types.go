// Package library provides MPD-driven library browsing for albums, artists, and radio.
package library

import "time"

// Scope defines the source filter for library queries.
type Scope string

const (
	ScopeAll   Scope = "all"
	ScopeNAS   Scope = "nas"
	ScopeLocal Scope = "local"
	ScopeUSB   Scope = "usb"
)

// SortOrder defines how results should be sorted.
type SortOrder string

const (
	SortAlphabetical   SortOrder = "alphabetical"
	SortByArtist       SortOrder = "by_artist"
	SortRecentlyAdded  SortOrder = "recently_added"
	SortYear           SortOrder = "year"
)

// SourceType represents the origin of a music file.
// Matches localmusic.SourceType for consistency.
type SourceType string

const (
	SourceLocal     SourceType = "local"
	SourceUSB       SourceType = "usb"
	SourceNAS       SourceType = "nas"
	SourceStreaming SourceType = "streaming"
)

// Album represents an album in the library.
type Album struct {
	ID         string     `json:"id"`
	Title      string     `json:"title"`
	Artist     string     `json:"artist"`
	URI        string     `json:"uri"`
	AlbumArt   string     `json:"albumArt,omitempty"`
	TrackCount int        `json:"trackCount,omitempty"`
	Source     SourceType `json:"source"`
	Year       int        `json:"year,omitempty"`
	AddedAt    time.Time  `json:"addedAt,omitempty"`
}

// Artist represents an artist in the library.
type Artist struct {
	Name       string `json:"name"`
	AlbumCount int    `json:"albumCount"`
	AlbumArt   string `json:"albumArt,omitempty"` // From first album
}

// Track represents a track in an album.
type Track struct {
	ID          string     `json:"id"`
	Title       string     `json:"title"`
	Artist      string     `json:"artist"`
	Album       string     `json:"album"`
	URI         string     `json:"uri"`
	TrackNumber int        `json:"trackNumber,omitempty"`
	Duration    int        `json:"duration,omitempty"`
	AlbumArt    string     `json:"albumArt,omitempty"`
	Source      SourceType `json:"source"`
}

// RadioStation represents a web radio station.
type RadioStation struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	URI   string `json:"uri"`
	Icon  string `json:"icon,omitempty"`
	Genre string `json:"genre,omitempty"`
}

// Pagination contains pagination info for list responses.
type Pagination struct {
	Page    int  `json:"page"`
	Limit   int  `json:"limit"`
	Total   int  `json:"total"`
	HasMore bool `json:"hasMore"`
}

// GetAlbumsRequest is the request for listing albums.
type GetAlbumsRequest struct {
	Scope Scope     `json:"scope"`
	Sort  SortOrder `json:"sort"`
	Query string    `json:"query,omitempty"`
	Page  int       `json:"page,omitempty"`
	Limit int       `json:"limit,omitempty"`
}

// AlbumsResponse is the response for listing albums.
type AlbumsResponse struct {
	Albums     []Album    `json:"albums"`
	Pagination Pagination `json:"pagination"`
}

// GetArtistsRequest is the request for listing artists.
type GetArtistsRequest struct {
	Query string `json:"query,omitempty"`
	Page  int    `json:"page,omitempty"`
	Limit int    `json:"limit,omitempty"`
}

// ArtistsResponse is the response for listing artists.
type ArtistsResponse struct {
	Artists    []Artist   `json:"artists"`
	Pagination Pagination `json:"pagination"`
}

// GetArtistAlbumsRequest is the request for listing albums by an artist.
type GetArtistAlbumsRequest struct {
	Artist string    `json:"artist"`
	Sort   SortOrder `json:"sort"`
	Page   int       `json:"page,omitempty"`
	Limit  int       `json:"limit,omitempty"`
}

// ArtistAlbumsResponse is the response for listing albums by an artist.
type ArtistAlbumsResponse struct {
	Artist     string     `json:"artist"`
	Albums     []Album    `json:"albums"`
	Pagination Pagination `json:"pagination"`
}

// GetAlbumTracksRequest is the request for listing tracks in an album.
type GetAlbumTracksRequest struct {
	Album       string `json:"album"`
	AlbumArtist string `json:"albumArtist,omitempty"`
}

// AlbumTracksResponse is the response for listing tracks in an album.
type AlbumTracksResponse struct {
	Album         string  `json:"album"`
	AlbumArtist   string  `json:"albumArtist"`
	Tracks        []Track `json:"tracks"`
	TotalDuration int     `json:"totalDuration"`
	Error         string  `json:"error,omitempty"`
}

// GetRadioRequest is the request for listing radio stations.
type GetRadioRequest struct {
	Query string `json:"query,omitempty"`
	Page  int    `json:"page,omitempty"`
	Limit int    `json:"limit,omitempty"`
}

// RadioResponse is the response for listing radio stations.
type RadioResponse struct {
	Stations   []RadioStation `json:"stations"`
	Pagination Pagination     `json:"pagination"`
}

// DefaultLimit is the default page size for listings.
const DefaultLimit = 50

// MaxLimit is the maximum page size for listings.
const MaxLimit = 200
