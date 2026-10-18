// Package filestream serves raw media bytes from the local music library over
// HTTP.
//
// It exists so that LAN devices which pull content by URL — Google Cast
// receivers, UPnP/DLNA renderers, AVPlayer on iOS — can play library files
// directly. The server is deliberately decoupled from MPD and ALSA: it only
// reads bytes off disk, so a remote device plays independently of whatever the
// DAC is doing and nothing here can disturb the bit-perfect output path.
//
// Range support is mandatory rather than optional. Cast receivers issue
// mid-file byte-range requests during normal playback, and a server that
// ignores Range appears to work until the first seek.
package filestream

import (
	"errors"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/rs/zerolog/log"
)

// Errors returned by Resolve. Callers map these onto status codes; the HTTP
// handler deliberately collapses them into generic responses so that a probing
// client cannot distinguish "outside the library" from "wrong extension".
var (
	// ErrEmptyPath is returned when no path was supplied.
	ErrEmptyPath = errors.New("filestream: empty path")
	// ErrOutsideLibrary is returned when the path escapes the music root.
	ErrOutsideLibrary = errors.New("filestream: path outside library root")
	// ErrUnsupportedType is returned when the extension is not on the allowlist.
	ErrUnsupportedType = errors.New("filestream: unsupported file type")
)

// mediaTypes maps the extensions this server will serve onto the Content-Type
// it reports. The allowlist is the point: a music library server has no
// business handing out arbitrary files that happen to sit under the music root,
// and several of these types need an explicit MIME because Go's own extension
// table does not know them.
//
// Cast receivers and some DLNA renderers sniff the format from the URL
// extension as well as the header, which is why the path-style URL form keeps
// the real extension intact.
var mediaTypes = map[string]string{
	// Lossless
	".flac": "audio/flac",
	".wav":  "audio/wav",
	".aif":  "audio/aiff",
	".aiff": "audio/aiff",
	".alac": "audio/mp4",
	".ape":  "audio/x-ape",
	".wv":   "audio/x-wavpack",
	// DSD — served as-is. No Cast receiver accepts these, but DLNA clients and
	// Audirvana decode them locally, so refusing to serve them would be wrong.
	".dsf": "audio/x-dsf",
	".dff": "audio/x-dff",
	// Lossy
	".mp3":  "audio/mpeg",
	".m4a":  "audio/mp4",
	".aac":  "audio/aac",
	".ogg":  "audio/ogg",
	".oga":  "audio/ogg",
	".opus": "audio/opus",
	".wma":  "audio/x-ms-wma",
	// Folder artwork, so a DIDL-Lite albumArtURI can point at the real file.
	".jpg":  "image/jpeg",
	".jpeg": "image/jpeg",
	".png":  "image/png",
	".webp": "image/webp",
}

// Server serves media files from a single music root.
type Server struct {
	root string
}

// New creates a Server rooted at musicDir. musicDir is the MPD music
// directory, e.g. /var/lib/mpd/music.
func New(musicDir string) *Server {
	return &Server{root: filepath.Clean(musicDir)}
}

// Root returns the music root this Server is confined to.
func (s *Server) Root() string { return s.root }

// Resolve turns a library-relative path into an absolute filesystem path,
// refusing anything that escapes the music root or is not an allowlisted media
// type.
//
// Containment is checked lexically, after cleaning, and must stay that way:
// the deployed library is reached through symlinks (/var/lib/mpd/music/USB ->
// /mnt/ssd/Music), so resolving symlinks before the comparison would classify
// every real file in the library as an escape.
func (s *Server) Resolve(rel string) (string, error) {
	if strings.TrimSpace(rel) == "" {
		return "", ErrEmptyPath
	}

	// Reject NUL bytes outright; they truncate paths in syscalls.
	if strings.ContainsRune(rel, 0) {
		return "", ErrOutsideLibrary
	}

	// An absolute path is never a library-relative path, even when it happens
	// to name a file inside the root — accepting it would make the API
	// ambiguous and invite traversal via a crafted prefix.
	if filepath.IsAbs(rel) || strings.HasPrefix(rel, "/") {
		return "", ErrOutsideLibrary
	}

	// Normalise Windows separators so a backslash cannot smuggle a segment
	// past the ".." check below.
	slashed := strings.ReplaceAll(rel, "\\", "/")

	// Reject any ".." segment outright, before cleaning.
	//
	// This check cannot be replaced by cleaning first: path.Clean on an
	// absolute path collapses leading ".." against the root, so
	// "../secret/x.flac" would quietly become "secret/x.flac" and resolve to a
	// *different* file inside the library than the caller asked for. Rejecting
	// is both safer and honest — the caller gets told no rather than served
	// something else.
	for _, seg := range strings.Split(slashed, "/") {
		if seg == ".." {
			return "", ErrOutsideLibrary
		}
	}

	cleaned := strings.TrimPrefix(path.Clean("/"+slashed), "/")
	if cleaned == "" || cleaned == "." {
		return "", ErrEmptyPath
	}

	full := filepath.Join(s.root, filepath.FromSlash(cleaned))

	// Belt and braces: Join already cleaned the result, so a surviving escape
	// means the input defeated the checks above.
	if !withinRoot(full, s.root) {
		return "", ErrOutsideLibrary
	}

	if _, ok := mediaTypes[strings.ToLower(filepath.Ext(full))]; !ok {
		return "", ErrUnsupportedType
	}

	return full, nil
}

// withinRoot reports whether p is root itself or sits beneath it. Comparing
// with a trailing separator avoids the classic prefix bug where "/musicians"
// passes a check against "/music".
func withinRoot(p, root string) bool {
	if p == root {
		return true
	}
	return strings.HasPrefix(p, root+string(filepath.Separator))
}

// ContentType returns the Content-Type for a resolved path, or "" when the
// extension is not allowlisted.
func ContentType(p string) string {
	return mediaTypes[strings.ToLower(filepath.Ext(p))]
}

// Handler returns an http.Handler serving library files.
//
// Two URL forms are accepted:
//
//	GET /stream/Artist/Album/01 Track.flac    (preferred)
//	GET /stream?path=Artist/Album/01 Track.flac
//
// The path form is preferred because Cast receivers and several DLNA renderers
// infer the media type from the URL extension in addition to the response
// header. prefix is the mount point to strip, e.g. "/stream".
//
// The endpoint is LAN-reachable by design: Cast receivers and DLNA renderers
// fetch content themselves, so a loopback-only gate would defeat its purpose.
// Read-only access, the root containment above, and the extension allowlist are
// the controls — the same posture as the library's existing read-only SMB
// share.
func (s *Server) Handler(prefix string) http.Handler {
	prefix = strings.TrimSuffix(prefix, "/")
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		rel := strings.TrimPrefix(r.URL.Path, prefix)
		rel = strings.TrimPrefix(rel, "/")
		if rel == "" {
			rel = r.URL.Query().Get("path")
		}

		full, err := s.Resolve(rel)
		if err != nil {
			// Collapse the reasons: a client probing for library layout should
			// not learn whether a rejection was containment or file type.
			switch {
			case errors.Is(err, ErrEmptyPath):
				http.Error(w, "path required", http.StatusBadRequest)
			default:
				http.Error(w, "not found", http.StatusNotFound)
			}
			return
		}

		f, err := os.Open(full)
		if err != nil {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		defer func() { _ = f.Close() }()

		info, err := f.Stat()
		if err != nil || info.IsDir() {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}

		w.Header().Set("Content-Type", ContentType(full))
		// Advertise ranges explicitly. ServeContent sets this too, but being
		// explicit documents the contract that Cast and DLNA depend on.
		w.Header().Set("Accept-Ranges", "bytes")
		// Library files are immutable in practice; ingest writes new paths
		// rather than rewriting existing ones.
		w.Header().Set("Cache-Control", "public, max-age=86400")

		// Clear the server's write deadline for this response.
		//
		// The HTTP server sets a global WriteTimeout to keep ordinary API
		// handlers honest, but that budget is a truncation bug here: a Cast
		// receiver pulling a 157 MB FLAC, or a DLNA client pulling a 687 MB
		// DSD file, legitimately takes longer than any sane API timeout, and
		// the stream would be cut off mid-track. Media transfers are bounded
		// by file size and link speed, not by a clock.
		//
		// If the deadline cannot be cleared the transfer still proceeds; it
		// just remains subject to the server default, so log it rather than
		// failing the request.
		if err := http.NewResponseController(w).SetWriteDeadline(time.Time{}); err != nil {
			log.Debug().Err(err).Msg("filestream: could not clear write deadline; large transfers may truncate")
		}

		// ServeContent handles Range, If-Range, If-Modified-Since, HEAD and
		// 206/416 responses.
		http.ServeContent(w, r, info.Name(), info.ModTime(), f)
	})
}
