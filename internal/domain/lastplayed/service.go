// Package lastplayed manages the persisted "last played album" record used to
// hydrate the frontend Player view's idle state on boot.
//
// The MPD watcher (in the socketio package) calls Record on every album-boundary
// transition while the player is in "play" state. The Socket.IO connect-time
// batch and the library:lastPlayed:get event call Get to fetch the row.
package lastplayed

import (
	"errors"
	"strings"
	"time"

	"github.com/edumarques81/stellar-volumio-audioplayer-backend/internal/infra/cache"
)

// Album is the domain shape; matches the frontend's Album type closely enough
// that the socket payload can be a direct field-rename map.
type Album struct {
	Artist     string
	Album      string
	AlbumArt   string
	TrackURI   string
	TrackType  string
	SampleRate string
	BitDepth   string
}

// Store is the persistence dependency the service needs (interface for tests).
type Store interface {
	Put(row cache.LastPlayedAlbum) error
	GetMostRecent() (cache.LastPlayedAlbum, bool, error)
}

// Service wraps a Store with album-boundary recording semantics.
type Service struct {
	store Store
}

// NewService constructs a Service. A nil store yields a service whose calls all
// return ErrNoStore — useful when the cache DB couldn't open at boot.
func NewService(store Store) *Service {
	return &Service{store: store}
}

// ErrNoStore is returned when the service was constructed with a nil store.
var ErrNoStore = errors.New("lastplayed: store unavailable")

// Record persists the album as the most-recently-played, stamped at time.Now().
// Empty artist+album are no-ops (radio streams, malformed metadata).
func (s *Service) Record(a Album) error {
	if s.store == nil {
		return ErrNoStore
	}
	if strings.TrimSpace(a.Artist) == "" || strings.TrimSpace(a.Album) == "" {
		return nil // skip; not an error
	}
	return s.store.Put(cache.LastPlayedAlbum{
		Artist:       a.Artist,
		Album:        a.Album,
		AlbumArt:     a.AlbumArt,
		TrackURI:     a.TrackURI,
		TrackType:    a.TrackType,
		SampleRate:   a.SampleRate,
		BitDepth:     a.BitDepth,
		LastPlayedAt: time.Now().Unix(),
	})
}

// Get returns the most-recently-played album, or (zero, false, nil) on miss.
func (s *Service) Get() (Album, bool, error) {
	if s.store == nil {
		return Album{}, false, ErrNoStore
	}
	row, ok, err := s.store.GetMostRecent()
	if err != nil || !ok {
		return Album{}, ok, err
	}
	return Album{
		Artist:     row.Artist,
		Album:      row.Album,
		AlbumArt:   row.AlbumArt,
		TrackURI:   row.TrackURI,
		TrackType:  row.TrackType,
		SampleRate: row.SampleRate,
		BitDepth:   row.BitDepth,
	}, true, nil
}
