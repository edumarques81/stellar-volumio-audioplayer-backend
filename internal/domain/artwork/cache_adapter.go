// Package artwork provides artwork resolution and caching for albums and artists.
package artwork

import (
	"github.com/edumarques81/stellar-volumio-audioplayer-backend/internal/infra/cache"
)

// CacheDAOAdapter adapts the cache.DAO to implement the ArtworkDAO interface.
type CacheDAOAdapter struct {
	dao *cache.DAO
}

// NewCacheDAOAdapter creates a new adapter for cache.DAO.
func NewCacheDAOAdapter(dao *cache.DAO) *CacheDAOAdapter {
	return &CacheDAOAdapter{dao: dao}
}

// GetArtwork retrieves cached artwork metadata by album ID.
func (a *CacheDAOAdapter) GetArtwork(albumID string) (*CachedArtwork, error) {
	cached, err := a.dao.GetArtworkByAlbum(albumID)
	if err != nil {
		return nil, err
	}
	if cached == nil {
		return nil, nil
	}

	// Convert from cache.CachedArtwork to artwork.CachedArtwork
	return &CachedArtwork{
		ID:        cached.ID,
		AlbumID:   cached.AlbumID,
		ArtistID:  cached.ArtistID,
		Type:      cached.Type,
		FilePath:  cached.FilePath,
		Source:    cached.Source,
		MimeType:  cached.MimeType,
		Width:     cached.Width,
		Height:    cached.Height,
		FileSize:  cached.FileSize,
		Checksum:  cached.Checksum,
		FetchedAt: cached.FetchedAt,
		ExpiresAt: cached.ExpiresAt,
		CreatedAt: cached.CreatedAt,
	}, nil
}

// SaveArtwork saves artwork metadata to the cache.
func (a *CacheDAOAdapter) SaveArtwork(art *CachedArtwork) error {
	// Convert from artwork.CachedArtwork to cache.CachedArtwork
	cached := &cache.CachedArtwork{
		ID:        art.ID,
		AlbumID:   art.AlbumID,
		ArtistID:  art.ArtistID,
		Type:      art.Type,
		FilePath:  art.FilePath,
		Source:    art.Source,
		MimeType:  art.MimeType,
		Width:     art.Width,
		Height:    art.Height,
		FileSize:  art.FileSize,
		Checksum:  art.Checksum,
		FetchedAt: art.FetchedAt,
		ExpiresAt: art.ExpiresAt,
		CreatedAt: art.CreatedAt,
	}

	if err := a.dao.InsertArtwork(cached); err != nil {
		return err
	}

	// Also update the album to link to this artwork
	return a.dao.UpdateAlbumArtwork(art.AlbumID, art.ID)
}
