// Package musicfile holds shared predicates over raw MPD song attribute maps
// (the []map[string]string shape returned by the mpd protocol client) that
// every backend read path needs when deciding what counts as real, playable
// audio content.
//
// This package lives under internal/infra, not internal/domain/library,
// for two verified facts about this codebase's current layering:
//
//  1. internal/infra packages import zero internal/domain packages outside
//     test files. internal/domain packages import internal/infra freely
//     (e.g. internal/domain/library already imports internal/infra/cache).
//     internal/infra/mpd (Plan 01-02) needs this predicate too; if it lived
//     in internal/domain/library, internal/infra/mpd would have to import a
//     domain package to reach it, inverting the only direction infra→domain
//     imports currently flow in this codebase.
//  2. internal/infra/musicfile is a new leaf package with zero internal
//     dependencies, matching the existing internal/infra/{cache,mpd,paths,...}
//     sibling pattern — importable by both internal/infra/mpd and
//     internal/domain/library without creating a cycle either way.
package musicfile

import (
	"path"
	"strings"
)

// IsResourceFork reports whether filePath's basename is a macOS AppleDouble
// resource-fork sidecar file — the "._"-prefixed companion files macOS
// writes alongside real files on NAS shares and exFAT/FAT volumes to carry
// resource forks and extended attributes. MPD indexes them because they
// pass extension checks, but they are not playable audio and must be
// filtered from every backend read path that lists MPD song results
// (DATA-04).
//
// The match is basename-only via path.Base, so it applies at any path
// depth including a bare basename with no leading slash, and it does not
// false-positive on "__"-prefixed (Python dunder-style) names or ordinary
// single-dot hidden files.
func IsResourceFork(filePath string) bool {
	base := path.Base(filePath)
	return strings.HasPrefix(base, "._")
}

// CountUntagged returns the number of entries in songs that are real (not a
// resource-fork sidecar per IsResourceFork on the "file" key) and have an
// empty "Album" tag. Resource-fork junk is deliberately excluded even when
// its "Album" value is empty — it is not "untagged real music", it is junk
// that DATA-04 filtering should remove, not junk that DATA-01 retagging
// should fix. Entries with no "file" key at all are treated as real
// (IsResourceFork("") is false) and counted if their "Album" is empty.
func CountUntagged(songs []map[string]string) int {
	count := 0
	for _, song := range songs {
		if IsResourceFork(song["file"]) {
			continue
		}
		if song["Album"] == "" {
			count++
		}
	}
	return count
}
