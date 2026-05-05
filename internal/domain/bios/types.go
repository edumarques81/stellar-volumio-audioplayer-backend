// Package bios orchestrates Wikipedia → LLM-summarize → SQLite-cache for
// album and artist bios shown on the Library screen.
package bios

import "time"

// Config tunes the service.
type Config struct {
	TTL time.Duration // default 90 days when zero
}

// Bio is the externally returned shape (no internal cache types leak out).
type Bio struct {
	Summary   string
	SourceURL string
	Kind      string // "album" or "artist" (empty when no hit)
}
