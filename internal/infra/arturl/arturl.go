// Package arturl builds the backend's artwork URLs.
//
// It exists because these URLs were assembled by string concatenation in ten
// places (`"/albumart?path=" + trackPath`), which silently corrupts any track
// path containing a character that is significant in a query string. The
// reported case was the album "Miles Ahead - Miles Davis + 19": a literal "+"
// in a query value decodes to a SPACE, so the handler looked for a directory
// named "Miles Davis   19-DSF-11289k-1b", found nothing, and returned 404 —
// while the same request with the "+" percent-encoded returned the cover
// (HTTP 200, 440,399 bytes). "&", "#", "%" and "=" are the same class of bug
// waiting on a differently-named album.
//
// The package is a leaf: it imports only net/url, so every layer (domain,
// transport, cmd) can depend on it without inverting the project's
// infra-never-imports-domain rule.
package arturl

import "net/url"

// AlbumArt returns the album-art URL for an MPD track path, encoded so that
// the handler's r.URL.Query().Get("path") reads back exactly trackPath.
//
// The "/albumart?path=" prefix is deliberately preserved verbatim:
// isArtworkRedirectURL (internal/transport/socketio) and the frontend's asset
// rewriter both match on it.
func AlbumArt(trackPath string) string {
	return "/albumart?path=" + url.QueryEscape(trackPath)
}
