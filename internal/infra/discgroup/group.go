// Package discgroup holds the pure BROWSE-07 multi-disc box-set detection
// rule: collapsing a cluster of folder-level album entries that share a
// title+artist into one grouped tile when they are genuinely the discs of
// one box set, and leaving them separate when they are not (e.g. distinct
// duplicate releases of the same title).
//
// This package lives under internal/infra, not internal/domain/library, for
// the same two verified facts about this codebase's layering that justify
// internal/infra/musicfile's and internal/infra/artistidentity's placement
// (see artistidentity's package doc):
//
//  1. internal/infra packages import zero internal/domain packages outside
//     test files. internal/domain packages import internal/infra freely.
//     Both internal/domain/library and internal/infra/cache need Group when
//     assembling the album list; if Group lived in internal/domain/library,
//     internal/infra/cache would have to import a domain package to reach
//     it, inverting the only direction infra->domain imports currently flow
//     in this codebase.
//  2. internal/infra/discgroup is a new leaf package with zero internal
//     dependencies (only the standard library "path", "regexp", "strconv",
//     and "strings" packages), matching the existing
//     internal/infra/{cache,mpd,musicfile,artistidentity,paths,...} sibling
//     pattern -- importable by both internal/infra/cache and
//     internal/domain/library without creating a cycle either way.
package discgroup

import (
	"path"
	"regexp"
	"strconv"
	"strings"
)

// Folder is one physical MPD-song-directory's worth of an album -- the unit
// internal/infra/mpd's groupAlbumDetails already produces one entry per
// (Album, AlbumArtist, Directory) tuple. Each disc of a box set, and each
// distinct quality/source version of a normal duplicate, is one Folder.
type Folder struct {
	Album       string
	AlbumArtist string
	Directory   string // parent directory of the tracks, e.g. "USB/Mahler The Symphonies/CD 01"
	Disc        string // MPD Disc tag from a representative track in this folder; "" if absent
	FirstTrack  string
	TrackCount  int
	TotalTime   int
	Format      string
	Genre       string
}

// Group is the output unit: either a single ungrouped Folder (DiscCount 0 or
// 1) or a merged multi-disc box set (DiscCount = number of member folders).
type Group struct {
	Album       string
	AlbumArtist string
	RootDir     string // playback/browse URI for this group -- see note below
	Disc        string // representative folder's Disc value (only meaningful when DiscCount<=1)
	FirstTrack  string // representative folder's FirstTrack (disc-1's, for album art)
	TrackCount  int    // summed across all member folders when grouped
	TotalTime   int    // summed across all member folders when grouped
	Format      string // representative folder's Format
	Genre       string // representative folder's Genre
	DiscCount   int    // 0 or 1 = not a box set; >1 = folders merged
}

// cdMarker matches the "/CD ?\d+/" path segment marker (case-insensitive),
// e.g. "CD 01", "CD1", "CD 11", anchored to a whole path segment so it does
// not match substrings inside unrelated words.
var cdMarker = regexp.MustCompile(`(?i)(^|/)CD ?\d+(/|$)`)

// GroupFolders detects multi-disc box sets among folder-level album entries and
// collapses each qualifying cluster into one Group. Folders are clustered by
// (lower(Album), lower(AlbumArtist)). A cluster of size > 1 is collapsed ONLY
// when ALL of these hold for every member folder:
//  1. every folder carries a non-empty Disc tag, and the set of Disc values
//     across the cluster has no duplicates (as many distinct values as
//     folders)
//  2. every folder's Directory matches the /CD ?\d+/ path marker
//     (case-insensitive, whole path segment: "CD 01", "CD1", "CD 11", ...)
//  3. every folder shares the same parent directory (one level up from
//     Directory) -- the box set's common root
//
// Any cluster that fails one of these checks is returned as one Group per
// input Folder, UNMERGED -- this is deliberate: it is what keeps the Miles
// Davis "Kind Of Blue" cluster (Disc:1 on all 3 folders -- check 1 fails) as
// 3 separate entries rather than merging on title+artist alone.
//
// RootDir on a grouped output IS THE PARENT of the member folders (e.g.
// "USB/Mahler The Symphonies", not "USB/Mahler The Symphonies/CD 01"). This
// matters downstream: MPD's `search base <path>` is RECURSIVE, so pointing a
// grouped album's playback URI at RootDir makes the existing
// `library:album:tracks` uri-scoped query return every disc's tracks with NO
// new query logic. On an ungrouped Group, RootDir equals the folder's own
// Directory (unchanged existing behavior).
//
// A cluster of size 1 is never treated as a box set regardless of its lone
// folder's Disc tag or CD-marker path: DiscCount is 1 (this package's chosen
// convention for "not a box set" -- see the DiscCount field doc, "0 or 1").
//
// Output order matches Go map iteration only insofar as callers must NOT
// depend on it; sort/group at a higher layer if the caller needs determinism
// (Plan 03's callers already sort the resulting Album list).
//
// DEVIATION FROM PLAN (see 03-01-SUMMARY.md): the plan's <interfaces> block
// specifies both `type Group struct` and `func Group(...) []Group` in the
// same package, which Go's compiler rejects (a type and a function cannot
// share one identifier in one package -- "Group redeclared in this block").
// The Group struct is kept as specified since it is the pervasive return
// type Plan 03's callers will hold onto; this entrypoint function is named
// GroupFolders instead. Plan 03 must call discgroup.GroupFolders(folders),
// not discgroup.Group(folders).
func GroupFolders(folders []Folder) []Group {
	type clusterKey struct {
		album  string
		artist string
	}

	order := make([]clusterKey, 0, len(folders))
	clusters := make(map[clusterKey][]Folder, len(folders))

	for _, f := range folders {
		key := clusterKey{album: strings.ToLower(f.Album), artist: strings.ToLower(f.AlbumArtist)}
		if _, seen := clusters[key]; !seen {
			order = append(order, key)
		}
		clusters[key] = append(clusters[key], f)
	}

	result := make([]Group, 0, len(folders))
	for _, key := range order {
		members := clusters[key]
		if len(members) > 1 && qualifiesAsBoxSet(members) {
			result = append(result, mergeBoxSet(members))
			continue
		}
		for _, f := range members {
			result = append(result, passthrough(f))
		}
	}

	return result
}

// qualifiesAsBoxSet reports whether every member folder satisfies all three
// checks required to collapse the cluster into one multi-disc Group.
func qualifiesAsBoxSet(members []Folder) bool {
	seenDisc := make(map[string]bool, len(members))
	var commonParent string

	for i, f := range members {
		if f.Disc == "" {
			return false // check 1: non-empty Disc required
		}
		if seenDisc[f.Disc] {
			return false // check 1: Disc values must be distinct (no duplicates)
		}
		seenDisc[f.Disc] = true

		if !cdMarker.MatchString(f.Directory) {
			return false // check 2: Directory must carry the /CD ?\d+/ marker
		}

		parent := path.Dir(f.Directory)
		if i == 0 {
			commonParent = parent
		} else if parent != commonParent {
			return false // check 3: every folder must share the same parent directory
		}
	}

	return true
}

// mergeBoxSet collapses a qualifying cluster into one Group. The
// representative folder is the one with the lowest parsed Disc number; on a
// parse failure or a tie, the lexicographically first Directory wins.
func mergeBoxSet(members []Folder) Group {
	rep := members[0]
	repNum, repOK := parseDiscNumber(rep.Disc)

	for _, f := range members[1:] {
		num, ok := parseDiscNumber(f.Disc)
		switch {
		case ok && repOK && num < repNum:
			rep, repNum, repOK = f, num, true
		case ok && repOK && num == repNum && f.Directory < rep.Directory:
			rep = f
		case (!ok || !repOK) && f.Directory < rep.Directory:
			// Fall back to lexicographically first Directory when either
			// value fails to parse as a disc number.
			rep = f
			repNum, repOK = num, ok
		}
	}

	var trackCount, totalTime int
	for _, f := range members {
		trackCount += f.TrackCount
		totalTime += f.TotalTime
	}

	return Group{
		Album:       rep.Album,
		AlbumArtist: rep.AlbumArtist,
		RootDir:     path.Dir(rep.Directory),
		Disc:        rep.Disc,
		FirstTrack:  rep.FirstTrack,
		TrackCount:  trackCount,
		TotalTime:   totalTime,
		Format:      rep.Format,
		Genre:       rep.Genre,
		DiscCount:   len(members),
	}
}

// passthrough converts a single, ungrouped Folder into its unchanged Group
// representation. DiscCount is 1 (this package's chosen "not a box set"
// value; see the Group.DiscCount field doc).
func passthrough(f Folder) Group {
	return Group{
		Album:       f.Album,
		AlbumArtist: f.AlbumArtist,
		RootDir:     f.Directory,
		Disc:        f.Disc,
		FirstTrack:  f.FirstTrack,
		TrackCount:  f.TrackCount,
		TotalTime:   f.TotalTime,
		Format:      f.Format,
		Genre:       f.Genre,
		DiscCount:   1,
	}
}

// parseDiscNumber parses disc as a base-10 integer, returning ok=false on
// empty input or any parse failure.
func parseDiscNumber(disc string) (n int, ok bool) {
	if disc == "" {
		return 0, false
	}
	v, err := strconv.Atoi(strings.TrimSpace(disc))
	if err != nil {
		return 0, false
	}
	return v, true
}
