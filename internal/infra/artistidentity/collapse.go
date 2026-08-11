// Package artistidentity holds the pure ARTIST-01/ARTIST-02/ARTIST-03
// collapse rule: reducing a raw, possibly multi-credit MPD `Artist` tag
// value down to the single first-credited performer that should be shown
// as one row in the artist list and used as the artist identity key.
//
// This package lives under internal/infra, not internal/domain/library,
// for the same two verified facts about this codebase's layering that
// justify internal/infra/musicfile's placement (see musicfile.go's package
// doc):
//
//  1. internal/infra packages import zero internal/domain packages outside
//     test files. internal/domain packages import internal/infra freely.
//     internal/infra/cache (the cache builder, a later plan in this phase)
//     needs Collapse when writing artist rows; if Collapse lived in
//     internal/domain/library, internal/infra/cache would have to import a
//     domain package to reach it, inverting the only direction infra->domain
//     imports currently flow in this codebase.
//  2. internal/infra/artistidentity is a new leaf package with zero
//     internal dependencies (only the standard library "strings" package),
//     matching the existing internal/infra/{cache,mpd,musicfile,paths,...}
//     sibling pattern -- importable by both internal/infra/cache and
//     internal/domain/library without creating a cycle either way.
package artistidentity

import "strings"

// Collapse reduces raw -- a raw MPD `Artist` tag value that may credit
// several performers joined by one of four observed conventions (comma,
// spaced hyphen, " with ", or a double-space role-suffix run) -- to the
// single first-credited performer (ARTIST-01, ARTIST-02).
//
// The rule is a single earliest-delimiter-wins scan: Collapse finds the
// lowest byte index at which any of these four delimiter substrings occurs
// in the trimmed input --
//
//   - "," (comma-joined credits, e.g. "Duke Ellington, John Coltrane")
//   - " - " (space-hyphen-space; a bare hyphen with no surrounding spaces,
//     as in "Jean-Pierre Lang", does NOT match, so hyphenated names are
//     left alone)
//   - " with " (space-with-space, case-sensitive; bare "and"/"And"/"&"/";"
//     do NOT match, so values joined by those must NOT collapse)
//   - "  " (a run of two consecutive spaces, the role-suffix/orchestra
//     convention observed in values like
//     "Herbert von Karajan  Wiener Philharmoniker" and
//     "Ella Fitzgerald - vocals  Paul Smith - piano")
//
// -- and returns everything before that earliest delimiter, trimmed. If
// none of the four delimiters are present, raw is returned unchanged
// (trimmed). If raw is empty or all whitespace, Collapse returns ""
// immediately without scanning (ARTIST-03) and never panics on any input.
//
// This single code path is deliberate: all four join conventions collapse
// correctly through the same earliest-delimiter rule, with no per-convention
// special-casing and no hand-maintained exception list (D-06). One known,
// accepted consequence is that
// "Adderley - Coltrane - Chambers - Cobb - Kelly" collapses to "Adderley"
// rather than a fuller name like "Cannonball Adderley" -- see
// TestCollapse_AdderleyKnownAcceptedImperfection.
func Collapse(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}

	idx := -1
	for _, delim := range [...]string{",", " - ", " with ", "  "} {
		if i := strings.Index(trimmed, delim); i >= 0 && (idx == -1 || i < idx) {
			idx = i
		}
	}

	if idx == -1 {
		return trimmed
	}

	return strings.TrimSpace(trimmed[:idx])
}
