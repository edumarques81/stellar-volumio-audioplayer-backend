// Package wikipedia provides a thin HTTP client over Wikipedia's REST summary
// endpoint, used by the bio service to seed LLM summaries.
package wikipedia

import "errors"

// ErrNotFound is returned (wrapped) when no summary exists for the requested
// album or artist. Callers should use errors.Is to detect it.
var ErrNotFound = errors.New("wikipedia: not found")

// Result is the parsed payload from a single REST summary call.
type Result struct {
	Title       string
	Description string // short one-liner, e.g. "1959 studio album by Miles Davis"
	Extract     string // plain-text first paragraph
	SourceURL   string // canonical desktop page URL
	Kind        string // "album" or "artist" — set by LookupAlbum* methods
}
