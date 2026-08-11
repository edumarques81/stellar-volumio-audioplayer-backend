# Phase 1: Data Integrity Foundation - Pattern Map

**Mapped:** 2026-08-11
**Files analyzed:** 7 (6 modifications + 1 new shared helper + test files)
**Analogs found:** 6/6 (100%)

## File Classification

| New/Modified File | Role | Data Flow | Closest Analog | Match Quality |
|-------------------|------|-----------|----------------|---------------|
| `internal/infra/mpd/client.go` — `GetAlbumDetails` | service | CRUD (query) | `internal/infra/mpd/client.go:ListArtists` | exact |
| `internal/domain/library/service.go` — `GetAlbumTracks` | service | CRUD (query + filter) | `internal/domain/library/service.go:GetAlbumTracks` | existing code |
| `internal/domain/library/service.go` — `GetArtistAlbums` | service | CRUD (query + filter) | `internal/domain/library/service.go:GetAlbumTracks` | role-match |
| `internal/infra/cache/types.go` — `CacheStats` struct | model | data definition | `internal/infra/cache/types.go:CachedAlbum` | exact |
| `internal/infra/cache/sqlite.go` — `GetStats` | service | CRUD (query) | `internal/infra/cache/sqlite.go:GetStats` | existing code |
| `internal/transport/socketio/cache_handlers.go` — cache status emit | transport | request-response | `internal/transport/socketio/cache_handlers.go:handleGetCacheStatus` | existing code |
| `internal/domain/library/filter.go` — shared `._` helper (NEW) | utility | string check | N/A (new file) | — |
| `internal/domain/library/*_test.go` — new tests | test | table-driven | `internal/infra/cache/builder_test.go` | pattern-match |

## Pattern Assignments

### `internal/domain/library/filter.go` — NEW shared `._` filtering helper

**Purpose:** Extract the `._` resource-fork skip logic from `GetAlbumTracks` into a reusable function that can be called from:
- `GetAlbumTracks` (existing at :485-487)
- `GetAlbumDetails` (new skip logic in MPD client)
- `GetArtistAlbums` (new skip logic)
- Cache builder paths that consume MPD song lists

**Reusable pattern from:** `internal/domain/library/service.go:484-487`

**Code to extract** (lines 484-487 of service.go):
```go
// Skip macOS resource fork files (._prefix)
base := path.Base(file)
if strings.HasPrefix(base, "._") {
	continue
}
```

**Recommended function signature:**
```go
package library

import "path"

// IsResourceFork checks whether a filename is a macOS resource-fork sidecar.
// Resource forks start with "._" and must be filtered from every MPD query result
// to avoid corrupting album/artist counts (DATA-04).
func IsResourceFork(filePath string) bool {
	base := path.Base(filePath)
	return strings.HasPrefix(base, "._")
}
```

**Usage at call sites:**
```go
// In GetAlbumTracks (internal/domain/library/service.go:~485)
if IsResourceFork(track["file"]) {
	continue
}

// In GetAlbumDetails (internal/infra/mpd/client.go:~749)
if album == "" || IsResourceFork(song["file"]) {
	continue
}

// In GetArtistAlbums (internal/domain/library/service.go, new code path)
// If filtering tracks from an album
if IsResourceFork(trackFile) {
	continue
}
```

**Location rationale:** `internal/domain/library/filter.go` lives in the library domain package because:
1. Path filtering is conceptually part of library data integrity, not generic filesystem utilities
2. Keeps all library-specific helpers in one domain package
3. `internal/infra/paths` handles system directory resolution, not filename filtering

---

### `internal/infra/mpd/client.go:723-800` — `GetAlbumDetails` with skipped-count tracking

**Analog:** `internal/infra/mpd/client.go:796-819` (`ListArtists`)

**Imports pattern** (lines 1-12 of client.go):
```go
package mpd

import (
	"fmt"
	"strconv"
	"strings"
	"sync"

	"github.com/rs/zerolog/log"
)
```

**Core query pattern** (lines 733-785 of GetAlbumDetails):
```go
// Get all songs in the base path
// AttrsList("file") tells the parser each song starts with "file:" key
songs, err := c.client.Command("search base %s", basePath).AttrsList("file")
if err != nil {
	return nil, fmt.Errorf("failed to search base %s: %w", basePath, err)
}

// Group songs by album + artist + directory (so different quality versions
// of the same album in different folders become separate entries)
albumMap := make(map[string]*AlbumDetails)

for _, song := range songs {
	album := song["Album"]
	artist := song["AlbumArtist"]
	if artist == "" {
		artist = song["Artist"]
	}

	// Skip songs without album tag
	if album == "" {
		continue
	}

	// Extract directory from file path for grouping
	filePath := song["file"]
	// ... directory extraction logic
	key := album + "\x00" + artist + "\x00" + directory
	// ... continue grouping
}

// Convert map to slice
var albums []AlbumDetails
for _, details := range albumMap {
	albums = append(albums, *details)
}

return albums, nil
```

**Location for skipped-count tracking:**
- Add a local counter `skippedCount := 0` before the loop
- Increment it when both `album == ""` condition is true
- Log at the end: `log.Debug().Str("path", basePath).Int("skipped", skippedCount).Msg("…")`
- NOTE: The count should be returned as a new return value OR logged, per D-07. See CacheStats pattern below for integration.

---

### `internal/domain/library/service.go:449-557` — `GetAlbumTracks` (MODIFY existing)

**Analog:** Self (this file already contains the pattern)

**Existing `._` filter pattern** (lines 484-487):
```go
// Skip macOS resource fork files (._prefix)
base := path.Base(file)
if strings.HasPrefix(base, "._") {
	continue
}
```

**Change:** Replace with call to new helper:
```go
if IsResourceFork(file) {
	continue
}
```

**Import addition:**
```go
import (
	"path"          // Already present
	"strings"       // Already present
	// Add if not already present
)
```

---

### `internal/domain/library/service.go:349-446` — `GetArtistAlbums` (MODIFY existing)

**Analog:** `internal/domain/library/service.go:74-223` (`GetAlbums`), which calls `GetAlbumDetails` at line 158

**Data-flow pattern** — GetArtistAlbums iterates GetAlbumDetails results:
```go
// Lines 349-362 in GetArtistAlbums
basePaths := s.getBasePathsForScope(ScopeAll)

for _, basePath := range basePaths {
	sourceType := s.sourceTypeForBasePath(basePath)
	albumDetails, err := s.mpd.GetAlbumDetails(basePath)
	if err != nil {
		log.Debug().Err(err).Str("path", basePath).Msg("Failed to get albums from database")
		continue
	}

	for _, details := range albumDetails {
		// Case-insensitive AlbumArtist match
		if !strings.EqualFold(details.AlbumArtist, req.Artist) {
			continue
		}
		// ... populate Album record
	}
}
```

**Change:** If GetArtistAlbums also needs to filter individual tracks (e.g., when searching within an album), use the `IsResourceFork` helper. Current code does not iterate tracks, only AlbumDetails, so no change needed unless new track filtering is added.

---

### `internal/infra/cache/types.go:90-102` — `CacheStats` struct (MODIFY existing)

**Analog:** `internal/infra/cache/types.go:6-26` (`CachedAlbum`)

**Current CacheStats definition** (lines 90-102):
```go
// CacheStats provides statistics about the cache.
type CacheStats struct {
	AlbumCount    int       `json:"albumCount"`
	ArtistCount   int       `json:"artistCount"`
	TrackCount    int       `json:"trackCount"`
	ArtworkCount  int       `json:"artworkCount"`
	ArtworkMissing int      `json:"artworkMissing"`
	RadioCount    int       `json:"radioCount"`
	SchemaVersion string    `json:"schemaVersion"`
	LastFullBuild time.Time `json:"lastFullBuild"`
	LastUpdated   time.Time `json:"lastUpdated"`
	IsBuilding    bool      `json:"isBuilding"`
	BuildProgress int       `json:"buildProgress"` // 0-100
}
```

**Change — add field for DATA-02** (skipped-file count per D-07):
```go
// CacheStats provides statistics about the cache.
type CacheStats struct {
	AlbumCount      int       `json:"albumCount"`
	ArtistCount     int       `json:"artistCount"`
	TrackCount      int       `json:"trackCount"`
	ArtworkCount    int       `json:"artworkCount"`
	ArtworkMissing  int       `json:"artworkMissing"`
	RadioCount      int       `json:"radioCount"`
	SkippedCount    int       `json:"skippedCount"`    // DATA-02: untagged + resource-fork files
	SchemaVersion   string    `json:"schemaVersion"`
	LastFullBuild   time.Time `json:"lastFullBuild"`
	LastUpdated     time.Time `json:"lastUpdated"`
	IsBuilding      bool      `json:"isBuilding"`
	BuildProgress   int       `json:"buildProgress"` // 0-100
}
```

**Rationale:**
- JSON struct tag `"skippedCount"` matches client contract (see SOCKET-CONTRACT.md update required)
- Type is `int`, not pointer, because it is always set (zero or positive)
- Placed after RadioCount for logical grouping with cache health metrics

---

### `internal/infra/cache/sqlite.go:340-399` — `GetStats()` method (MODIFY existing)

**Analog:** Self (existing code); pattern from lines 355-383

**Current GetStats signature and structure** (lines 340-399):
```go
// GetStats returns cache statistics.
func (d *DB) GetStats() (*CacheStats, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	if d.db == nil {
		return nil, fmt.Errorf("database not open")
	}

	stats := &CacheStats{
		IsBuilding:    d.isBuilding,
		BuildProgress: d.buildProgress,
	}

	// Get counts
	var err error
	err = d.db.QueryRow("SELECT COUNT(*) FROM albums").Scan(&stats.AlbumCount)
	if err != nil {
		return nil, err
	}
	
	// ... similar queries for artist, track, artwork, radio
	
	// Get metadata
	stats.SchemaVersion, _ = d.getMeta("schema_version")
	// ... lastBuild, lastUpdated parsing
	
	return stats, nil
}
```

**Change — add SkippedCount query**:

After line 380 (RadioCount query), add:
```go
// Get skipped files count (untagged albums + resource-fork junk)
err = d.db.QueryRow(`
	SELECT COALESCE(SUM(
		(SELECT COUNT(*) FROM albums WHERE artist IS NULL OR artist = '')
		+
		(SELECT COUNT(*) FROM songs WHERE file LIKE '._%%')
	), 0)
`).Scan(&stats.SkippedCount)
if err != nil {
	// If query fails (e.g., schema mismatch), default to 0
	stats.SkippedCount = 0
}
```

**Alternative (simpler) approach** if the above SQL is complex:
- Initialize `SkippedCount` to 0 in the struct literal (line 348-350)
- Populate it from a dedicated method or counter that the cache builder maintains during rebuild
- This trades one SQL query for passing a counter through the build pipeline

**Current pattern for similar counters**: Lines 355-380 show the row-by-row query pattern already in use; follow that style.

---

### `internal/transport/socketio/cache_handlers.go:36-48` — `CacheStatusResponse` struct (MODIFY existing)

**Analog:** `internal/transport/socketio/cache_handlers.go:86-93` (`CacheUpdatedEvent`)

**Current CacheStatusResponse** (lines 36-48):
```go
// CacheStatusResponse represents the cache status response.
type CacheStatusResponse struct {
	LastUpdated    string `json:"lastUpdated"`
	AlbumCount     int    `json:"albumCount"`
	ArtistCount    int    `json:"artistCount"`
	TrackCount     int    `json:"trackCount"`
	ArtworkCached  int    `json:"artworkCached"`
	ArtworkMissing int    `json:"artworkMissing"`
	RadioCount     int    `json:"radioCount"`
	IsBuilding     bool   `json:"isBuilding"`
	BuildProgress  int    `json:"buildProgress"`
	SchemaVersion  string `json:"schemaVersion"`
}
```

**Change — add SkippedCount field**:
```go
// CacheStatusResponse represents the cache status response.
type CacheStatusResponse struct {
	LastUpdated    string `json:"lastUpdated"`
	AlbumCount     int    `json:"albumCount"`
	ArtistCount    int    `json:"artistCount"`
	TrackCount     int    `json:"trackCount"`
	ArtworkCached  int    `json:"artworkCached"`
	ArtworkMissing int    `json:"artworkMissing"`
	RadioCount     int    `json:"radioCount"`
	SkippedCount   int    `json:"skippedCount"`    // DATA-02: untagged + resource-fork files
	IsBuilding     bool   `json:"isBuilding"`
	BuildProgress  int    `json:"buildProgress"`
	SchemaVersion  string `json:"schemaVersion"`
}
```

**Mapping logic in handleGetCacheStatus** (lines 51-84):

Current mapping (lines 61-71):
```go
resp := CacheStatusResponse{
	AlbumCount:     stats.AlbumCount,
	ArtistCount:    stats.ArtistCount,
	TrackCount:     stats.TrackCount,
	ArtworkCached:  stats.ArtworkCount,
	ArtworkMissing: stats.ArtworkMissing,
	RadioCount:     stats.RadioCount,
	IsBuilding:     stats.IsBuilding,
	BuildProgress:  stats.BuildProgress,
	SchemaVersion:  stats.SchemaVersion,
}
```

**Add after line 70**:
```go
	SkippedCount:   stats.SkippedCount,    // DATA-02: new field
```

**Broadcasting chain:**
```
CacheStats (internal/infra/cache/types.go:90-102)
  ↓ populated by
GetStats (internal/infra/cache/sqlite.go:340-399)
  ↓ mapped into
CacheStatusResponse (internal/transport/socketio/cache_handlers.go:36-48)
  ↓ emitted via
client.Emit("pushLibraryCacheStatus", resp)   (line 83 of cache_handlers.go)
```

---

## Shared Patterns

### Table-Driven Test Pattern

**Source:** `internal/infra/cache/builder_test.go:54-123`

**Apply to:** All new `_test.go` files for `._` filter helper, modified GetAlbumDetails, GetArtistAlbums, GetStats tests

**Pattern:**
```go
func TestFunction_CaseName(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		// Input fields
		inputFile      string
		inputBasePath  string
		// Expected output
		wantSkipped    bool
		wantCount      int
		wantError      error
		wantErrorMsg   string
	}{
		{
			name:        "description of test case 1",
			inputFile:   "path/to/file.flac",
			wantSkipped: false,
			wantCount:   0,
		},
		{
			name:        "description of test case 2",
			inputFile:   "path/to/._sidecar",
			wantSkipped: true,
			wantCount:   1,
		},
		// ... more cases
	}

	for _, tc := range cases {
		tc := tc  // Capture loop variable for parallel tests
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			// Test body using tc.inputFile, tc.wantSkipped, etc.
		})
	}
}
```

**Key conventions:**
- `tc := tc` captures the loop variable (required for `t.Parallel()`)
- Prefix struct fields: `input*` for params, `want*` for expectations, `*Error` for error cases
- Use `t.Run(tc.name, ...)` for sub-test naming and isolation
- `t.Parallel()` enables parallel execution across test cases

---

### MPDClient Mock Pattern

**Source:** `internal/domain/library/service_test.go:8-123`

**Apply to:** Tests for `GetAlbumDetails`, cache builder, any code that consumes MPDClient

**Pattern:**
```go
// MockMPDClient implements the MPDClient interface for testing.
type MockMPDClient struct {
	// One field per interface method's return value
	ListAlbumsResponse    []AlbumInfo
	ListAlbumsError       error
	ListAlbumsInBaseResp  map[string][]AlbumInfo
	ListAlbumsInBaseError error
	GetAlbumDetailsResp   map[string][]AlbumDetails
	GetAlbumDetailsError  error
	// ... one pair per method
}

func (m *MockMPDClient) GetAlbumDetails(basePath string) ([]AlbumDetails, error) {
	if m.GetAlbumDetailsError != nil {
		return nil, m.GetAlbumDetailsError
	}
	if resp, ok := m.GetAlbumDetailsResp[basePath]; ok {
		return resp, nil
	}
	return []AlbumDetails{}, nil
}

// ... implement each interface method
```

**Usage in tests:**
```go
mockMPD := &MockMPDClient{
	GetAlbumDetailsResp: map[string][]AlbumDetails{
		"USB": {
			{
				Album:       "Test Album",
				AlbumArtist: "Test Artist",
				TrackCount:  10,
				FirstTrack:  "USB/Album/track.flac",
			},
		},
	},
}

// Pass mockMPD to the service under test
service := NewService(mockMPD, classifier)
```

---

## CacheStats End-to-End Flow

**Requirement DATA-02:** Surface skipped-file count in cache status so clients see untagged files without SSH.

**The chain:**

1. **Data collection** (internal/infra/mpd/client.go)
   - `GetAlbumDetails` counts skipped songs (album == "") and logs them
   - Count passed to cache builder or tracked during rebuild

2. **Persistence** (internal/infra/cache/sqlite.go)
   - `GetStats()` queries database for skipped-file count
   - Stores result in `CacheStats.SkippedCount`

3. **Schema** (internal/infra/cache/types.go)
   - `CacheStats` struct includes new `SkippedCount int` field
   - JSON tag: `"skippedCount"`

4. **Socket.IO Transport** (internal/transport/socketio/cache_handlers.go)
   - `CacheStatusResponse` struct mirrors `CacheStats` fields
   - `handleGetCacheStatus()` maps `stats.SkippedCount` to `resp.SkippedCount`
   - Emits `client.Emit("pushLibraryCacheStatus", resp)`

5. **Wire Contract** (docs/SOCKET-CONTRACT.md)
   - Add field to TypeScript `interface CacheStatus` (lines 81-93)
   - Field name: `skippedCount: number;` (after `radioCount`)

**Verification:**
- After Phase 1 DATA-01 (user retags all 16 untagged songs), SkippedCount must read **0**
- If a macOS copy reintroduces `._` files, SkippedCount must reflect the count (DATA-04)

---

## No Analog Found

None. All files modified in Phase 1 have precedents in the codebase (either existing code or proven patterns from similar services).

---

## Metadata

**Analog search scope:** 
- `internal/domain/library/` (Service layer)
- `internal/infra/mpd/` (MPD client)
- `internal/infra/cache/` (Cache layer + types)
- `internal/transport/socketio/` (Wire layer)

**Files scanned:** 12
- service.go (1,648 lines)
- client.go (~850 lines)
- sqlite.go (~410 lines)
- types.go (145 lines)
- cache_handlers.go (134 lines)
- service_test.go (818 lines)
- builder_test.go (250+ lines)
- 5 other supporting files

**Pattern extraction date:** 2026-08-11

**Key insights:**
1. **Existing `._` filter is inline** — must be extracted to `internal/domain/library/filter.go` to avoid duplication
2. **Skipped-count tracking requires three-layer change** — MPD client (count) → cache DB (persist) → Socket.IO transport (emit)
3. **Table-driven tests** are the house style; all new tests must follow the `struct + t.Run` pattern from builder_test.go
4. **MockMPDClient** is the standard test double; use it for any code path that needs MPDClient without a live daemon
5. **No utility package exists yet** — recommend creating `internal/domain/library/filter.go` rather than a generic util package, to keep concern-specific helpers in their domain

---

*Pattern mapping complete: 2026-08-11*
