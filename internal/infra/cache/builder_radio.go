// Package cache provides a SQLite-based caching layer for library metadata.
package cache

import (
	"crypto/md5"
	"fmt"
	"strings"

	"github.com/rs/zerolog/log"
)

// buildRadioStations builds the radio stations cache from MPD playlists.
func (b *Builder) buildRadioStations() error {
	playlists, err := b.provider.ListPlaylists()
	if err != nil {
		return fmt.Errorf("failed to list playlists: %w", err)
	}

	tx, err := b.db.BeginTx()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	radioCount := 0

	for _, playlist := range playlists {
		// Only process playlists that look like radio stations
		if !strings.HasPrefix(playlist, "Radio/") && !strings.HasPrefix(strings.ToLower(playlist), "radio") {
			continue
		}

		info, err := b.provider.ListPlaylistInfo(playlist)
		if err != nil {
			log.Warn().Err(err).Str("playlist", playlist).Msg("Failed to get playlist info")
			continue
		}

		if len(info) == 0 {
			continue
		}

		// Use first track as the stream URL
		uri := info[0].File
		name := strings.TrimPrefix(playlist, "Radio/")
		if name == "" {
			name = playlist
		}

		stationID := generateRadioID(name, uri)

		station := &CachedRadioStation{
			ID:   stationID,
			Name: name,
			URI:  uri,
		}

		if err := b.dao.InsertRadioStation(station); err != nil {
			log.Warn().Err(err).Str("station", name).Msg("Failed to insert radio station")
			continue
		}

		radioCount++
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit radio stations: %w", err)
	}

	log.Debug().Int("count", radioCount).Msg("Radio stations cached")
	return nil
}

func generateRadioID(name, uri string) string {
	data := name + "\x00" + uri
	return fmt.Sprintf("%x", md5.Sum([]byte(data)))
}
