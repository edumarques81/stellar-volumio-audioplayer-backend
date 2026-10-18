package filestream

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// escapePath escapes each segment of a library-relative path while leaving the
// separators intact, which is what a real client sends.
func escapePath(rel string) string {
	parts := strings.Split(rel, "/")
	for i, p := range parts {
		parts[i] = url.PathEscape(p)
	}
	return strings.Join(parts, "/")
}

func TestResolve(t *testing.T) {
	root := "/var/lib/mpd/music"
	s := New(root)

	tests := []struct {
		name    string
		rel     string
		want    string
		wantErr error
	}{
		{
			name: "simple relative path",
			rel:  "Dookie/01 Burnout.flac",
			want: filepath.Join(root, "Dookie/01 Burnout.flac"),
		},
		{
			name: "path through a symlinked source dir stays lexical",
			rel:  "USB/Dookie/01 Burnout.flac",
			want: filepath.Join(root, "USB/Dookie/01 Burnout.flac"),
		},
		{
			name: "interior dot-dot is rejected rather than collapsed",
			// Cleaning first would silently rewrite this to a path the caller
			// did not ask for, so it is refused outright.
			rel:     "Artist/Album/../Album/01 Track.flac",
			wantErr: ErrOutsideLibrary,
		},
		{
			name: "leading slash is not a relative path",
			rel:  "/var/lib/mpd/music/Dookie/01 Burnout.flac",
			// Rejected even though it names a real library file: accepting
			// absolute input would make the API ambiguous.
			wantErr: ErrOutsideLibrary,
		},
		{
			name:    "absolute path outside the library",
			rel:     "/etc/passwd",
			wantErr: ErrOutsideLibrary,
		},
		{
			name:    "dot-dot escape",
			rel:     "../../../etc/passwd",
			wantErr: ErrOutsideLibrary,
		},
		{
			name:    "dot-dot escape disguised by an interior segment",
			rel:     "Dookie/../../../etc/passwd",
			wantErr: ErrOutsideLibrary,
		},
		{
			name:    "backslash separators are normalised then caught",
			rel:     `..\..\..\etc\passwd`,
			wantErr: ErrOutsideLibrary,
		},
		{
			name:    "NUL byte",
			rel:     "Dookie/01 Burnout.flac\x00.jpg",
			wantErr: ErrOutsideLibrary,
		},
		{
			name:    "empty",
			rel:     "",
			wantErr: ErrEmptyPath,
		},
		{
			name:    "whitespace only",
			rel:     "   ",
			wantErr: ErrEmptyPath,
		},
		{
			name:    "bare dot",
			rel:     ".",
			wantErr: ErrEmptyPath,
		},
		{
			name:    "non-media extension is refused",
			rel:     "Dookie/notes.txt",
			wantErr: ErrUnsupportedType,
		},
		{
			name:    "no extension is refused",
			rel:     "Dookie/README",
			wantErr: ErrUnsupportedType,
		},
		{
			name:    "database file under the music root is refused",
			rel:     "library.db",
			wantErr: ErrUnsupportedType,
		},
		{
			name: "extension matching is case insensitive",
			rel:  "Dookie/01 Burnout.FLAC",
			want: filepath.Join(root, "Dookie/01 Burnout.FLAC"),
		},
		{
			name: "DSD is served even though no Cast receiver accepts it",
			rel:  "Miles/So What.dsf",
			want: filepath.Join(root, "Miles/So What.dsf"),
		},
		{
			name: "folder artwork is allowlisted",
			rel:  "Dookie/folder.jpg",
			want: filepath.Join(root, "Dookie/folder.jpg"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := s.Resolve(tt.rel)
			if tt.wantErr != nil {
				if err != tt.wantErr {
					t.Fatalf("Resolve(%q) error = %v, want %v", tt.rel, err, tt.wantErr)
				}
				if got != "" {
					t.Errorf("Resolve(%q) returned path %q alongside an error", tt.rel, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("Resolve(%q) unexpected error: %v", tt.rel, err)
			}
			if got != tt.want {
				t.Errorf("Resolve(%q) = %q, want %q", tt.rel, got, tt.want)
			}
		})
	}
}

// A sibling directory whose name merely starts with the root's name must not
// pass containment. This is the classic prefix bug.
func TestResolveRejectsSiblingWithRootAsPrefix(t *testing.T) {
	s := New("/var/lib/mpd/music")
	if _, err := s.Resolve("../music-backup/secret.flac"); err != ErrOutsideLibrary {
		t.Fatalf("expected ErrOutsideLibrary for a sibling sharing the root prefix, got %v", err)
	}
}

func TestWithinRoot(t *testing.T) {
	root := filepath.FromSlash("/var/lib/mpd/music")
	tests := []struct {
		name string
		p    string
		want bool
	}{
		{"root itself", root, true},
		{"child", filepath.Join(root, "a.flac"), true},
		{"nested child", filepath.Join(root, "a/b/c.flac"), true},
		{"parent", filepath.FromSlash("/var/lib/mpd"), false},
		{"sibling sharing the prefix", filepath.FromSlash("/var/lib/mpd/musicians/a.flac"), false},
		{"unrelated", filepath.FromSlash("/etc/passwd"), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := withinRoot(tt.p, root); got != tt.want {
				t.Errorf("withinRoot(%q, %q) = %v, want %v", tt.p, root, got, tt.want)
			}
		})
	}
}

func TestContentType(t *testing.T) {
	tests := []struct {
		path string
		want string
	}{
		{"a.flac", "audio/flac"},
		{"a.FLAC", "audio/flac"},
		{"a.wav", "audio/wav"},
		{"a.dsf", "audio/x-dsf"},
		{"a.dff", "audio/x-dff"},
		{"a.aiff", "audio/aiff"},
		{"a.mp3", "audio/mpeg"},
		{"a.m4a", "audio/mp4"},
		{"a.opus", "audio/opus"},
		{"folder.jpg", "image/jpeg"},
		{"a.txt", ""},
		{"noext", ""},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			if got := ContentType(tt.path); got != tt.want {
				t.Errorf("ContentType(%q) = %q, want %q", tt.path, got, tt.want)
			}
		})
	}
}

// newTestServer writes a library file of known content and returns a Server
// rooted at its parent, plus the file's relative path and payload.
func newTestServer(t *testing.T) (*Server, string, []byte) {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "Album"), 0o755); err != nil {
		t.Fatal(err)
	}
	// 4 KiB of non-repeating content, so a wrong offset is visible.
	payload := make([]byte, 4096)
	for i := range payload {
		payload[i] = byte(i % 251)
	}
	rel := "Album/01 Track.flac"
	if err := os.WriteFile(filepath.Join(root, rel), payload, 0o644); err != nil {
		t.Fatal(err)
	}
	// A file that must never be served, despite sitting under the root.
	if err := os.WriteFile(filepath.Join(root, "secrets.txt"), []byte("nope"), 0o644); err != nil {
		t.Fatal(err)
	}
	return New(root), rel, payload
}

func TestHandlerServesWholeFile(t *testing.T) {
	s, rel, payload := newTestServer(t)
	h := s.Handler("/stream")

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/stream/"+escapePath(rel), nil))

	res := rec.Result()
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", res.StatusCode)
	}
	if ct := res.Header.Get("Content-Type"); ct != "audio/flac" {
		t.Errorf("Content-Type = %q, want audio/flac", ct)
	}
	if ar := res.Header.Get("Accept-Ranges"); ar != "bytes" {
		t.Errorf("Accept-Ranges = %q, want bytes", ar)
	}
	if cl := res.Header.Get("Content-Length"); cl != fmt.Sprint(len(payload)) {
		t.Errorf("Content-Length = %q, want %d", cl, len(payload))
	}
	body, _ := io.ReadAll(res.Body)
	if len(body) != len(payload) {
		t.Fatalf("body length = %d, want %d", len(body), len(payload))
	}
	for i := range payload {
		if body[i] != payload[i] {
			t.Fatalf("body differs from payload at byte %d", i)
		}
	}
}

// Range support is the whole point: Cast receivers issue mid-file range
// requests during normal playback, and a server that ignores Range looks
// healthy until the first seek.
func TestHandlerRangeRequests(t *testing.T) {
	s, rel, payload := newTestServer(t)
	h := s.Handler("/stream")
	total := len(payload)

	tests := []struct {
		name       string
		rangeHdr   string
		wantStatus int
		wantStart  int
		wantEnd    int // inclusive
		wantCR     string
	}{
		{
			name:       "leading bytes",
			rangeHdr:   "bytes=0-1023",
			wantStatus: http.StatusPartialContent,
			wantStart:  0,
			wantEnd:    1023,
			wantCR:     fmt.Sprintf("bytes 0-1023/%d", total),
		},
		{
			name:       "mid-file window, the Cast seek shape",
			rangeHdr:   "bytes=1024-2047",
			wantStatus: http.StatusPartialContent,
			wantStart:  1024,
			wantEnd:    2047,
			wantCR:     fmt.Sprintf("bytes 1024-2047/%d", total),
		},
		{
			name:       "open-ended at exactly EOF is unsatisfiable",
			rangeHdr:   "bytes=4096-",
			wantStatus: http.StatusRequestedRangeNotSatisfiable,
		},
		{
			name:       "open-ended from inside the file, the shape the Hub sent",
			rangeHdr:   "bytes=3072-",
			wantStatus: http.StatusPartialContent,
			wantStart:  3072,
			wantEnd:    total - 1,
			wantCR:     fmt.Sprintf("bytes 3072-%d/%d", total-1, total),
		},
		{
			name:       "suffix range",
			rangeHdr:   "bytes=-512",
			wantStatus: http.StatusPartialContent,
			wantStart:  total - 512,
			wantEnd:    total - 1,
			wantCR:     fmt.Sprintf("bytes %d-%d/%d", total-512, total-1, total),
		},
		{
			name:       "range end past EOF is clamped",
			rangeHdr:   "bytes=4000-99999",
			wantStatus: http.StatusPartialContent,
			wantStart:  4000,
			wantEnd:    total - 1,
			wantCR:     fmt.Sprintf("bytes 4000-%d/%d", total-1, total),
		},
		{
			name:       "entirely beyond EOF",
			rangeHdr:   "bytes=99999-",
			wantStatus: http.StatusRequestedRangeNotSatisfiable,
		},
		{
			// Note: RFC 7233 suggests ignoring a malformed Range and serving
			// 200. Go's ServeContent returns 416 instead. Recording the actual
			// behaviour rather than the aspiration — no observed client sends
			// a malformed Range, so it is not worth overriding the stdlib.
			name:       "malformed Range yields 416 from ServeContent",
			rangeHdr:   "bytes=abc",
			wantStatus: http.StatusRequestedRangeNotSatisfiable,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/stream/"+escapePath(rel), nil)
			req.Header.Set("Range", tt.rangeHdr)
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)

			res := rec.Result()
			defer func() { _ = res.Body.Close() }()
			if res.StatusCode != tt.wantStatus {
				t.Fatalf("status = %d, want %d", res.StatusCode, tt.wantStatus)
			}
			if tt.wantStatus == http.StatusRequestedRangeNotSatisfiable {
				return
			}
			if tt.wantCR != "" {
				if cr := res.Header.Get("Content-Range"); cr != tt.wantCR {
					t.Errorf("Content-Range = %q, want %q", cr, tt.wantCR)
				}
			}
			body, _ := io.ReadAll(res.Body)
			want := payload[tt.wantStart : tt.wantEnd+1]
			if len(body) != len(want) {
				t.Fatalf("body length = %d, want %d", len(body), len(want))
			}
			for i := range want {
				if body[i] != want[i] {
					t.Fatalf("byte %d of range = %d, want %d (wrong offset served)", i, body[i], want[i])
				}
			}
		})
	}
}

func TestHandlerQueryParamForm(t *testing.T) {
	s, rel, payload := newTestServer(t)
	h := s.Handler("/stream")

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/stream?path="+url.QueryEscape(rel), nil))

	res := rec.Result()
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", res.StatusCode)
	}
	body, _ := io.ReadAll(res.Body)
	if len(body) != len(payload) {
		t.Errorf("body length = %d, want %d", len(body), len(payload))
	}
}

func TestHandlerHEAD(t *testing.T) {
	s, rel, payload := newTestServer(t)
	h := s.Handler("/stream")

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodHead, "/stream/"+escapePath(rel), nil))

	res := rec.Result()
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", res.StatusCode)
	}
	if cl := res.Header.Get("Content-Length"); cl != fmt.Sprint(len(payload)) {
		t.Errorf("Content-Length = %q, want %d", cl, len(payload))
	}
	body, _ := io.ReadAll(res.Body)
	if len(body) != 0 {
		t.Errorf("HEAD returned %d body bytes, want 0", len(body))
	}
}

func TestHandlerRejections(t *testing.T) {
	s, _, _ := newTestServer(t)
	h := s.Handler("/stream")

	tests := []struct {
		name       string
		method     string
		target     string
		wantStatus int
	}{
		{"no path", http.MethodGet, "/stream", http.StatusBadRequest},
		{"empty query path", http.MethodGet, "/stream?path=", http.StatusBadRequest},
		{"traversal", http.MethodGet, "/stream/../../../etc/passwd", http.StatusNotFound},
		{"traversal via query", http.MethodGet, "/stream?path=../../../etc/passwd", http.StatusNotFound},
		{"non-media file under the root", http.MethodGet, "/stream/secrets.txt", http.StatusNotFound},
		{"missing file with a good extension", http.MethodGet, "/stream/Album/nope.flac", http.StatusNotFound},
		{"directory", http.MethodGet, "/stream/Album", http.StatusNotFound},
		{"percent-encoded traversal", http.MethodGet, "/stream/%2e%2e/%2e%2e/etc/passwd", http.StatusNotFound},
		{"POST", http.MethodPost, "/stream/Album/01%20Track.flac", http.StatusMethodNotAllowed},
		{"DELETE", http.MethodDelete, "/stream/Album/01%20Track.flac", http.StatusMethodNotAllowed},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, httptest.NewRequest(tt.method, tt.target, nil))
			if rec.Code != tt.wantStatus {
				t.Errorf("%s %s status = %d, want %d", tt.method, tt.target, rec.Code, tt.wantStatus)
			}
		})
	}
}

// Rejections must not leak which rule fired: containment and file-type refusals
// have to look identical to a client probing for library layout.
func TestHandlerRejectionsAreIndistinguishable(t *testing.T) {
	s, _, _ := newTestServer(t)
	h := s.Handler("/stream")

	bodyFor := func(target string) (int, string) {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, target, nil))
		return rec.Code, strings.TrimSpace(rec.Body.String())
	}

	outsideCode, outsideBody := bodyFor("/stream/../../../etc/passwd")
	typeCode, typeBody := bodyFor("/stream/secrets.txt")
	missingCode, missingBody := bodyFor("/stream/Album/nope.flac")

	if outsideCode != typeCode || typeCode != missingCode {
		t.Errorf("rejection statuses differ: outside=%d type=%d missing=%d", outsideCode, typeCode, missingCode)
	}
	if outsideBody != typeBody || typeBody != missingBody {
		t.Errorf("rejection bodies differ: outside=%q type=%q missing=%q", outsideBody, typeBody, missingBody)
	}
}

// A symlink inside the library pointing outside it is how the real deployment
// works (USB -> /mnt/ssd/Music), so it must resolve rather than be refused.
func TestHandlerFollowsSymlinkedSourceDir(t *testing.T) {
	root := t.TempDir()
	external := t.TempDir()
	if err := os.WriteFile(filepath.Join(external, "track.flac"), []byte("audio"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(external, filepath.Join(root, "USB")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	h := New(root).Handler("/stream")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/stream/USB/track.flac", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 — a symlinked source dir must be served", rec.Code)
	}
	if got := rec.Body.String(); got != "audio" {
		t.Errorf("body = %q, want %q", got, "audio")
	}
}

func TestHandlerPrefixVariants(t *testing.T) {
	s, rel, _ := newTestServer(t)
	for _, prefix := range []string{"/stream", "/stream/"} {
		t.Run(prefix, func(t *testing.T) {
			h := s.Handler(prefix)
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/stream/"+escapePath(rel), nil))
			if rec.Code != http.StatusOK {
				t.Errorf("prefix %q: status = %d, want 200", prefix, rec.Code)
			}
		})
	}
}

func TestRoot(t *testing.T) {
	if got := New("/var/lib/mpd/music/").Root(); got != "/var/lib/mpd/music" {
		t.Errorf("Root() = %q, want cleaned path", got)
	}
}

// A large media transfer must not be bounded by the server's global
// WriteTimeout. httptest.ResponseRecorder does not implement the deadline
// interface, so this exercises the fallback path: the handler must log and
// carry on serving rather than erroring out.
func TestHandlerServesWhenWriteDeadlineCannotBeCleared(t *testing.T) {
	s, rel, payload := newTestServer(t)
	h := s.Handler("/stream")

	rec := httptest.NewRecorder() // no SetWriteDeadline support
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/stream/"+escapePath(rel), nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 — an unsupported deadline must not fail the transfer", rec.Code)
	}
	if rec.Body.Len() != len(payload) {
		t.Errorf("body length = %d, want %d", rec.Body.Len(), len(payload))
	}
}

// Over a real listener the deadline is clearable, and a response larger than a
// short WriteTimeout must still arrive whole. This is the regression guard for
// the truncation bug the global timeout would otherwise cause.
func TestServerWriteTimeoutDoesNotTruncateLargeTransfer(t *testing.T) {
	root := t.TempDir()
	// 8 MiB, enough that the write cannot plausibly complete inside the
	// deadline set below if the deadline were still in force.
	payload := make([]byte, 8<<20)
	for i := range payload {
		payload[i] = byte(i % 251)
	}
	if err := os.WriteFile(filepath.Join(root, "big.flac"), payload, 0o644); err != nil {
		t.Fatal(err)
	}

	mux := http.NewServeMux()
	mux.Handle("/stream/", New(root).Handler("/stream"))

	srv := httptest.NewUnstartedServer(mux)
	srv.Config.WriteTimeout = 150 * time.Millisecond
	srv.Start()
	defer srv.Close()

	res, err := http.Get(srv.URL + "/stream/big.flac")
	if err != nil {
		t.Fatalf("GET failed: %v", err)
	}
	defer func() { _ = res.Body.Close() }()

	// Throttle the read so the transfer is guaranteed to outlive the
	// WriteTimeout above.
	var got []byte
	buf := make([]byte, 64<<10)
	for {
		n, err := res.Body.Read(buf)
		got = append(got, buf[:n]...)
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("read failed after %d bytes (transfer was truncated): %v", len(got), err)
		}
		time.Sleep(2 * time.Millisecond)
	}

	if len(got) != len(payload) {
		t.Fatalf("received %d bytes, want %d — transfer was truncated", len(got), len(payload))
	}
}
