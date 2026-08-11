// Package dupebadge holds the pure BROWSE-01/BROWSE-02/BROWSE-03
// duplicate-disambiguation badge rule: within a Title+Artist duplicate
// cluster, computing whichever of quality, disc, or source actually varies
// so the badge disambiguates something real, rather than printing an
// identical, meaningless label on every tile.
//
// This package lives under internal/infra, not internal/domain/library,
// for the same two verified facts about this codebase's layering that
// justify internal/infra/artistidentity's placement (see
// artistidentity/collapse.go's package doc):
//
//  1. internal/infra packages import zero internal/domain packages outside
//     test files. internal/domain packages import internal/infra freely.
//     internal/infra/cache (the cache builder) and internal/domain/library
//     (the MPD-direct service) both need Compute when assembling album
//     lists; if Compute lived in internal/domain/library,
//     internal/infra/cache would have to import a domain package to reach
//     it, inverting the only direction infra->domain imports currently
//     flow in this codebase.
//  2. internal/infra/dupebadge is a new leaf package with zero internal
//     dependencies (only the standard library "strings" package), matching
//     the existing internal/infra/{artistidentity,discgroup,musicfile,...}
//     sibling pattern -- importable by both internal/infra/cache and
//     internal/domain/library without creating a cycle either way.
//
// Compute deliberately does not compute the Quality string itself: callers
// pass the already-formatted output of internal/domain/library's
// formatQualityLabel (sample_rate/bit_depth/track_type -> "352.8kHz/24bit
// FLAC", "DSD256", ...) in Candidate.Quality, per this package's
// <interfaces> contract. Layering forbids this leaf package from importing
// that function directly.
//
// Why disc, not quality-only (D-02, superseding the original D-01 "always
// show quality" design): live measurement on 2026-08-12 showed 6 of the 8
// live duplicate groups on the Pi have only ONE distinct quality among
// their versions. A quality-only badge would print identical text on every
// tile in those 6 groups and disambiguate nothing. The revised rule shows
// whatever actually varies within the group -- quality first (it is the
// most informative signal when it varies), then disc, then source -- and
// shows no badge at all when nothing tracked varies, rather than a
// meaningless one.
package dupebadge

import "strings"

// Candidate is one album version considered for a duplicate-disambiguation
// badge. It is a minimal, domain-decoupled projection of
// internal/domain/library.Album (per the infra->domain layering rule, this
// package cannot import the library.Album type itself).
type Candidate struct {
	Title   string
	Artist  string
	Quality string // formatQualityLabel() output, e.g. "352.8kHz/24bit FLAC", "DSD256" — verbatim, already formatted
	Disc    string // MPD Disc tag representative value, "" if absent/not applicable
	Source  string // internal/domain/library.SourceType as a raw string: "local", "usb", "nas"
}

// Compute returns one badge string per input candidate, same length and
// order as candidates ("" meaning no badge). Implements the REVISED
// precedence (CONTEXT.md D-02): candidates are clustered by
// (lower(Title), lower(Artist)); a cluster of size 1 always gets "". For a
// cluster of size > 1:
//
//  1. if the cluster's set of distinct Quality values has cardinality > 1,
//     every member's badge is its OWN Quality string verbatim
//  2. else if the cluster's set of distinct non-empty Disc values has
//     cardinality > 1, every member's badge is "Disc " + its own Disc value
//  3. else if the cluster's set of distinct Source values has cardinality
//     > 1, every member's badge is strings.ToUpper(its own Source)
//  4. else (nothing tracked varies) every member's badge is ""
//
// Only the first tier that actually varies fires — ties are broken by
// precedence order, not combined (a cluster that varies in BOTH quality and
// source gets ONLY the quality badge, per D-02's stated precedence).
func Compute(candidates []Candidate) []string {
	badges := make([]string, len(candidates))

	type clusterKey struct {
		title  string
		artist string
	}

	clusters := make(map[clusterKey][]int, len(candidates))
	for i, c := range candidates {
		key := clusterKey{strings.ToLower(c.Title), strings.ToLower(c.Artist)}
		clusters[key] = append(clusters[key], i)
	}

	for _, indices := range clusters {
		if len(indices) < 2 {
			// Cluster of size 1: badges[i] is already "" (zero value).
			continue
		}

		qualitySet := make(map[string]struct{}, len(indices))
		discSet := make(map[string]struct{}, len(indices))
		sourceSet := make(map[string]struct{}, len(indices))
		for _, i := range indices {
			qualitySet[candidates[i].Quality] = struct{}{}
			if candidates[i].Disc != "" {
				discSet[candidates[i].Disc] = struct{}{}
			}
			sourceSet[candidates[i].Source] = struct{}{}
		}

		switch {
		case len(qualitySet) > 1:
			for _, i := range indices {
				badges[i] = candidates[i].Quality
			}
		case len(discSet) > 1:
			for _, i := range indices {
				badges[i] = "Disc " + candidates[i].Disc
			}
		case len(sourceSet) > 1:
			for _, i := range indices {
				badges[i] = strings.ToUpper(candidates[i].Source)
			}
		}
		// Otherwise: nothing tracked varies, badges[i] stays "" (D-02 rule 4).
	}

	return badges
}
