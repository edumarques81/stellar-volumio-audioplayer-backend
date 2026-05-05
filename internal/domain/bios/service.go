package bios

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/edumarques81/stellar-volumio-audioplayer-backend/internal/infra/cache"
	"github.com/edumarques81/stellar-volumio-audioplayer-backend/internal/infra/llm"
	"github.com/edumarques81/stellar-volumio-audioplayer-backend/internal/infra/wikipedia"
)

const defaultTTL = 90 * 24 * time.Hour

// WikiClient is the subset of *wikipedia.Client the service needs.
// Defined as an interface here so tests can substitute a fake.
type WikiClient interface {
	LookupAlbumOrArtist(ctx context.Context, artist, album string) (wikipedia.Result, error)
}

// Service orchestrates the bio pipeline.
type Service struct {
	dao  *cache.BiosDAO
	wiki WikiClient
	llm  llm.Client
	ttl  time.Duration
}

// NewService wires the orchestrator. cfg.TTL of zero applies the 90-day default.
func NewService(dao *cache.BiosDAO, wiki WikiClient, ll llm.Client, cfg Config) *Service {
	ttl := cfg.TTL
	if ttl == 0 {
		ttl = defaultTTL
	}
	return &Service{dao: dao, wiki: wiki, llm: ll, ttl: ttl}
}

// GetAlbumBio returns the cached bio if fresh; otherwise fetches+summarizes,
// stores, and returns. Always returns a Bio (possibly empty); errors are
// reserved for unexpected I/O failure.
//
// Cache strategy: check album_bios first; if a fresh row exists, serve from
// there. If not, fetch from Wikipedia (which may fall back to the artist
// page); if Wikipedia returned an artist result, also check artist_bios cache
// before calling the LLM, so a previous artist-fallback summary is reused.
func (s *Service) GetAlbumBio(ctx context.Context, artist, album string) (Bio, error) {
	if cached, ok, err := s.dao.GetAlbumBio(artist, album); err == nil && ok {
		if cached.ExpiresAt > time.Now().Unix() {
			return Bio{Summary: cached.Summary, SourceURL: cached.SourceURL, Kind: "album"}, nil
		}
	}
	// No fresh album row — also check the artist cache (may have been populated
	// by a previous album lookup that fell back to the artist page).
	if cached, ok, err := s.dao.GetArtistBio(artist); err == nil && ok {
		if cached.ExpiresAt > time.Now().Unix() {
			return Bio{Summary: cached.Summary, SourceURL: cached.SourceURL, Kind: "artist"}, nil
		}
	}
	return s.fetchAndCacheAlbum(ctx, artist, album)
}

// RefreshAlbumBio invalidates the cache row(s) and re-fetches.
func (s *Service) RefreshAlbumBio(ctx context.Context, artist, album string) (Bio, error) {
	if err := s.dao.DeleteAlbumBio(artist, album); err != nil {
		log.Warn().Err(err).Str("artist", artist).Str("album", album).Msg("RefreshAlbumBio delete failed; continuing")
	}
	if err := s.dao.DeleteArtistBio(artist); err != nil {
		log.Warn().Err(err).Str("artist", artist).Msg("RefreshAlbumBio artist delete failed; continuing")
	}
	return s.fetchAndCacheAlbum(ctx, artist, album)
}

func (s *Service) fetchAndCacheAlbum(ctx context.Context, artist, album string) (Bio, error) {
	res, err := s.wiki.LookupAlbumOrArtist(ctx, artist, album)
	if err != nil {
		if errors.Is(err, wikipedia.ErrNotFound) {
			log.Debug().Str("artist", artist).Str("album", album).Msg("wiki miss; returning empty bio")
			return Bio{}, nil
		}
		log.Warn().Err(err).Str("artist", artist).Str("album", album).Msg("wiki lookup error; returning empty bio")
		return Bio{}, nil
	}

	summary, err := s.llm.Summarize(ctx, res.Extract, llm.Options{MaxTokens: 256, Temperature: 0.3})
	if err != nil {
		log.Warn().Err(err).Str("artist", artist).Str("album", album).Msg("LLM summarize failed; not caching")
		return Bio{}, nil
	}
	summary = strings.TrimSpace(summary)
	if summary == "" {
		// Noop client or empty completion — no value in caching.
		return Bio{}, nil
	}

	now := time.Now().Unix()
	expires := now + int64(s.ttl.Seconds())

	switch res.Kind {
	case "album":
		if err := s.dao.PutAlbumBio(cache.AlbumBio{
			Artist: artist, Album: album,
			Summary: summary, SourceURL: res.SourceURL,
			FetchedAt: now, ExpiresAt: expires,
		}); err != nil {
			log.Warn().Err(err).Msg("PutAlbumBio failed; returning bio without caching")
		}
	case "artist":
		if err := s.dao.PutArtistBio(cache.ArtistBio{
			Artist:  artist,
			Summary: summary, SourceURL: res.SourceURL,
			FetchedAt: now, ExpiresAt: expires,
		}); err != nil {
			log.Warn().Err(err).Msg("PutArtistBio failed; returning bio without caching")
		}
	}
	return Bio{Summary: summary, SourceURL: res.SourceURL, Kind: res.Kind}, nil
}
