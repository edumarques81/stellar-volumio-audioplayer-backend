package localmusic

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
)

// HistoryStore manages playback history persistence.
type HistoryStore struct {
	filePath   string
	classifier *PathClassifier
	entries    []PlayHistoryEntry
	mu         sync.RWMutex
	maxEntries int
}

// NewHistoryStore creates a new history store.
func NewHistoryStore(dataDir string, classifier *PathClassifier) *HistoryStore {
	h := &HistoryStore{
		filePath:   filepath.Join(dataDir, "playback_history.json"),
		classifier: classifier,
		entries:    []PlayHistoryEntry{},
		maxEntries: 1000, // Keep last 1000 entries
	}
	h.load()
	return h
}

// RecordPlay records a track play event.
func (h *HistoryStore) RecordPlay(trackURI, title, artist, album, albumArt string, origin PlayOrigin) {
	h.mu.Lock()
	defer h.mu.Unlock()

	sourceType := h.classifier.GetSourceType(trackURI)

	// Check if this track was recently played (within 5 seconds) to avoid duplicates
	now := time.Now()
	for i := len(h.entries) - 1; i >= 0 && i >= len(h.entries)-5; i-- {
		if h.entries[i].TrackURI == trackURI && now.Sub(h.entries[i].PlayedAt) < 5*time.Second {
			// Update existing entry instead of creating duplicate
			h.entries[i].PlayedAt = now
			h.entries[i].PlayCount++
			log.Debug().
				Str("uri", trackURI).
				Str("origin", string(origin)).
				Msg("Updated existing play history entry")
			h.saveAsync()
			return
		}
	}

	// Create new entry
	entry := PlayHistoryEntry{
		ID:        uuid.New().String(),
		TrackURI:  trackURI,
		Title:     title,
		Artist:    artist,
		Album:     album,
		AlbumArt:  albumArt,
		Source:    sourceType,
		Origin:    origin,
		PlayedAt:  now,
		PlayCount: 1,
	}

	h.entries = append(h.entries, entry)

	// Trim to max entries
	if len(h.entries) > h.maxEntries {
		h.entries = h.entries[len(h.entries)-h.maxEntries:]
	}

	log.Info().
		Str("uri", trackURI).
		Str("title", title).
		Str("origin", string(origin)).
		Str("source", string(sourceType)).
		Msg("Recorded play history")

	h.saveAsync()
}

// GetLastPlayed returns the last played tracks, optionally filtered to local-only.
func (h *HistoryStore) GetLastPlayed(req GetLastPlayedRequest, localOnly bool, manualOnly bool) LastPlayedResponse {
	h.mu.RLock()
	defer h.mu.RUnlock()

	var filtered []PlayHistoryEntry

	// Filter entries
	for _, entry := range h.entries {
		// Filter by source if localOnly is requested
		if localOnly && !entry.Source.IsLocalSource() {
			continue
		}

		// Filter by origin if manualOnly is requested
		if manualOnly && entry.Origin != PlayOriginManualTrack {
			continue
		}

		filtered = append(filtered, entry)
	}

	// Sort entries
	switch req.Sort {
	case TrackSortLastPlayed:
		sort.Slice(filtered, func(i, j int) bool {
			return filtered[i].PlayedAt.After(filtered[j].PlayedAt)
		})
	case TrackSortAlphabetical:
		sort.Slice(filtered, func(i, j int) bool {
			return filtered[i].Title < filtered[j].Title
		})
	case TrackSortMostPlayed:
		sort.Slice(filtered, func(i, j int) bool {
			return filtered[i].PlayCount > filtered[j].PlayCount
		})
	default:
		// Default to most recent
		sort.Slice(filtered, func(i, j int) bool {
			return filtered[i].PlayedAt.After(filtered[j].PlayedAt)
		})
	}

	// Apply limit
	limit := req.Limit
	if limit <= 0 {
		limit = 50
	}
	if len(filtered) > limit {
		filtered = filtered[:limit]
	}

	return LastPlayedResponse{
		Tracks:     filtered,
		TotalCount: len(filtered),
	}
}

// GetPlayCount returns the total play count for a track.
func (h *HistoryStore) GetPlayCount(trackURI string) int {
	h.mu.RLock()
	defer h.mu.RUnlock()

	count := 0
	for _, entry := range h.entries {
		if entry.TrackURI == trackURI {
			count += entry.PlayCount
		}
	}
	return count
}

// ClearHistory clears all playback history.
func (h *HistoryStore) ClearHistory() {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.entries = []PlayHistoryEntry{}
	h.saveAsync()
	log.Info().Msg("Playback history cleared")
}

// load reads history from disk.
func (h *HistoryStore) load() {
	data, err := os.ReadFile(h.filePath)
	if err != nil {
		if !os.IsNotExist(err) {
			log.Warn().Err(err).Str("file", h.filePath).Msg("Failed to read playback history")
		}
		return
	}

	var entries []PlayHistoryEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		log.Warn().Err(err).Msg("Failed to parse playback history")
		return
	}

	h.entries = entries
	log.Info().Int("count", len(entries)).Msg("Loaded playback history")
}

// saveAsync saves history to disk asynchronously.
func (h *HistoryStore) saveAsync() {
	go func() {
		h.mu.RLock()
		entriesCopy := make([]PlayHistoryEntry, len(h.entries))
		copy(entriesCopy, h.entries)
		h.mu.RUnlock()

		data, err := json.MarshalIndent(entriesCopy, "", "  ")
		if err != nil {
			log.Error().Err(err).Msg("Failed to marshal playback history")
			return
		}

		// Ensure directory exists
		if err := os.MkdirAll(filepath.Dir(h.filePath), 0755); err != nil {
			log.Error().Err(err).Msg("Failed to create history directory")
			return
		}

		if err := os.WriteFile(h.filePath, data, 0644); err != nil {
			log.Error().Err(err).Msg("Failed to save playback history")
		}
	}()
}

// Stats returns statistics about the playback history.
func (h *HistoryStore) Stats() map[string]interface{} {
	h.mu.RLock()
	defer h.mu.RUnlock()

	stats := map[string]interface{}{
		"totalEntries": len(h.entries),
	}

	// Count by source type
	sourceCounts := make(map[SourceType]int)
	originCounts := make(map[PlayOrigin]int)

	for _, entry := range h.entries {
		sourceCounts[entry.Source]++
		originCounts[entry.Origin]++
	}

	stats["bySource"] = sourceCounts
	stats["byOrigin"] = originCounts

	return stats
}
