---
phase: 03-browse-experience
plan: 08a
type: execute
wave: 5
depends_on: ["03-06"]
files_modified:
  - ../stellar-ios/StellarVolumiO/Models/LibraryModels.swift
  - ../stellar-ios/StellarVolumiOTests/LibraryEnvelopeParserTests.swift
  - ../stellar-ios/StellarVolumiOTests/Fixtures/Fixtures.swift
  - ../stellar-ios/StellarVolumiO/Stores/ArtistPickerStore.swift
  - ../stellar-ios/StellarVolumiO/Views/Library/AlbumPickerView.swift
  - ../stellar-ios/StellarVolumiO/Views/Library/ArtistDetailView.swift
  - ../stellar-ios/StellarVolumiO/Views/Library/AlbumTracksView.swift
autonomous: true
requirements: [ARTIST-04, BROWSE-02, BROWSE-03, BROWSE-04, BROWSE-07]
user_setup: []

must_haves:
  truths:
    - "On the iPhone app, a duplicate album's tile in the Albums grid and the Artist->Albums grid shows its badge text"
    - "On the iPhone app, a unique album's tile shows no badge"
    - "On the iPhone app, drilling into a multi-disc album's track list shows Disc N section headers"
    - "On the iPhone app, drilling into an artist that resolves to zero albums shows a real, tappable, playable song list instead of an empty grid"
---

<objective>
Implement the iPhone-side (stellar-ios, branch `main`) rendering for all three phase features against
the now-deployed, contract-locked backend from 03-06: the duplicate-disambiguation badge (BROWSE-02/
BROWSE-03) on both the Albums grid and the Artist->Albums grid, the multi-disc grouped-album track
view (BROWSE-07), and the loose-track empty-state fallback (ARTIST-04/BROWSE-04).

Purpose: This is the iPhone half of BROWSE-01/02/03/04/07 — mirrors 03-07a's LCD work but in SwiftUI.
Two platform-appropriate differences from the LCD, both deliberate: (1) iOS's `AlbumTile` is a real
`LazyVGrid` tile (ROADMAP criterion 2's literal "grid"), unlike the LCD's single-album carousel; (2)
iOS's track rows are already tap-to-play (unlike the LCD's deliberately read-only rows), so the
loose-track fallback needs NO new "Play All" affordance — the existing per-row tap IS the playable
interaction. Do not add a Play-All button on iOS; it would be scope creed against the
six-feature-only iOS app boundary in workspace CLAUDE.md.
</objective>

<execution_context>
@$HOME/.claude/get-shit-done/workflows/execute-plan.md
@$HOME/.claude/get-shit-done/templates/summary.md
</execution_context>

<context>
@.planning/PROJECT.md
@.planning/ROADMAP.md
@.planning/phases/03-browse-experience/03-CONTEXT.md
@docs/SOCKET-CONTRACT.md
</context>

<interfaces>
<!-- The backend contract this plan renders against, locked by 03-06 (same shapes as 03-07a). -->

```
Album gains: badge?: string, discCount?: number (uri already points at the combined-track root for a grouped album)
Track gains: disc?: number (1-based; pre-sorted by (disc, trackNumber, title))
ArtistAlbumsResponse gains: looseTracks?: Track[] (populated ONLY when albums is empty)
```

<!-- Existing iOS shapes this plan extends — read the real files, these are orientation only. -->

```swift
// LibraryModels.swift: LibraryAlbum and Track BOTH have TWO independent
// parsing paths that must stay in sync — a Codable init(from:)/encode(to:)
// pair AND a tolerant `init?(rawDict:)` extension (used by the ACTUAL live
// socket listeners via `socket.onRawDict(..., parser: X.init(rawDict:), ...)`
// in AlbumPickerStore/ArtistPickerStore/AlbumTracksStore — the Codable path
// exists for other purposes but must not silently drift out of sync).
// LibraryAlbum's designated init(id:title:artist:uri:albumart:year:trackCount:)
// is called from BOTH paths — it must grow badge/discCount parameters too.
// PushLibraryArtistAlbums has NO custom Codable — it relies on synthesis, so
// adding `looseTracks: [Track]?` to its stored properties is enough for the
// Codable side; only its `init?(rawDict:)` extension needs a manual edit.

// AlbumTracksView.swift: `private struct TrackList` and `private struct
// TrackRow` render `store.tracks` with per-row tap-to-play via
// `replaceAndPlay type: song`. ArtistDetailView's loose-track fallback
// should REUSE these (drop `private` so they're visible outside the file)
// rather than duplicating the row styling.

// AlbumPickerView.swift AND ArtistDetailView.swift each define their OWN
// private `AlbumTile` struct (near-identical, historically duplicated) —
// the badge overlay must be added to BOTH.
```
</interfaces>

<tasks>

<task type="auto" tdd="true">
  <name>Task 1: Extend LibraryModels.swift (badge, discCount, disc, looseTracks) + wire ArtistPickerStore</name>
  <files>../stellar-ios/StellarVolumiO/Models/LibraryModels.swift, ../stellar-ios/StellarVolumiOTests/LibraryEnvelopeParserTests.swift, ../stellar-ios/StellarVolumiOTests/Fixtures/Fixtures.swift, ../stellar-ios/StellarVolumiO/Stores/ArtistPickerStore.swift</files>
  <read_first>
    - stellar-ios/StellarVolumiO/Models/LibraryModels.swift (full LibraryAlbum, Track, PushLibraryArtistAlbums struct + rawDict extension definitions — every touch point listed in &lt;interfaces&gt; above)
    - stellar-ios/StellarVolumiOTests/LibraryEnvelopeParserTests.swift (existing test style for rawDict parsers)
    - stellar-ios/StellarVolumiOTests/Fixtures/Fixtures.swift (existing raw-dict fixture literals to extend/add alongside)
    - stellar-ios/StellarVolumiO/Stores/ArtistPickerStore.swift (the pushLibraryArtistAlbums binding + select()/clearSelection())
  </read_first>
  <behavior>
    - `LibraryAlbum(rawDict:)` given `"badge": "352.8kHz/24bit FLAC", "discCount": 11` produces an
      album with those values; given neither key present, both are nil.
    - `Track(rawDict:)` given `"disc": 2` produces `track.disc == 2`; given no key, `disc == 0` (or
      nil — pick one, matching the existing `trackNumber`/`duration` zero-default convention in this
      same initializer, and document the choice).
    - `PushLibraryArtistAlbums(rawDict:)` given `"looseTracks": [{...}, {...}]` produces
      `env.looseTracks` with 2 parsed `Track` entries; given no key, `looseTracks` is nil/empty.
    - `ArtistPickerStore.select(artist)` on a payload with empty `albums` and non-empty
      `looseTracks` populates a new `artistLooseTracks: [Track]` property; a payload with non-empty
      `albums` leaves `artistLooseTracks` empty; `clearSelection()` resets it.
  </behavior>
  <action>
    In LibraryModels.swift: add `let badge: String?` and `let discCount: Int?` to `LibraryAlbum`'s
    stored properties, its `CodingKeys` (if the Codable path reads/writes these — check whether
    `badge`/`discCount` need coding keys given the struct's existing custom
    `init(from:)`/`encode(to:)`; add them consistently with how `year`/`trackCount` are already
    handled), its `init(from decoder:)` (decodeIfPresent), its designated `init(id:title:artist:uri:
    albumart:year:trackCount:)` (extend with `badge: String? = nil, discCount: Int? = nil`), its
    `encode(to:)`, and its `init?(rawDict:)` extension (`d["badge"] as? String`,
    `d["discCount"] as? Int`). Add `let disc: Int` (or `Int?` — match the existing
    `trackNumber`/`duration` zero-default style) to `Track` the same way across all its touch points
    (stored property, `Track(rawDict:)` reading `d["disc"] as? Int ?? 0`, and the designated
    initializer signature used by both the rawDict extension and any other call sites — grep for
    `Track(id:` to find them all). Add `let looseTracks: [Track]?` to `PushLibraryArtistAlbums`'s
    stored properties (no other Codable edits needed — no custom init(from:)/encode exist for this
    type) and populate it in its `init?(rawDict:)` extension:
    `d["looseTracks"] as? [[String: Any]] ?? []` mapped through `Track(rawDict:)`. In
    ArtistPickerStore.swift, add `var artistLooseTracks: [Track] = []`, set it from
    `payload.looseTracks ?? []` inside the existing `pushLibraryArtistAlbums` binding, and reset it
    to `[]` in both `select(_:)` (before the fetch, mirroring the existing `artistAlbums = []` reset)
    and `clearSelection()`. Add fixture dicts + test cases to Fixtures.swift /
    LibraryEnvelopeParserTests.swift covering the &lt;behavior&gt; scenarios above.
  </action>
  <acceptance_criteria>
    - New parser tests pass for badge/discCount/disc/looseTracks, both present and absent.
    - `scripts/build.sh` compiles cleanly (Codable and rawDict paths for LibraryAlbum/Track stay in sync — a mismatch here is a compile error, not a runtime bug).
  </acceptance_criteria>
  <verify>
    <automated>cd ../stellar-ios && export IOS_SIM_ID=$(xcrun simctl list devices | grep "iPhone 16 Pro" | head -1 | grep -oE '[0-9A-F-]{36}') && scripts/test.sh 2>&1 | tail -60</automated>
  </verify>
  <done>LibraryAlbum/Track/PushLibraryArtistAlbums carry badge/discCount/disc/looseTracks through both their Codable and rawDict parsing paths; ArtistPickerStore exposes artistLooseTracks; all new and existing tests pass.</done>
</task>

<task type="auto" tdd="true">
  <name>Task 2: Badge overlay on both AlbumTile definitions + disc-grouped headers in AlbumTracksView</name>
  <files>../stellar-ios/StellarVolumiO/Views/Library/AlbumPickerView.swift, ../stellar-ios/StellarVolumiO/Views/Library/ArtistDetailView.swift, ../stellar-ios/StellarVolumiO/Views/Library/AlbumTracksView.swift</files>
  <read_first>
    - stellar-ios/StellarVolumiO/Views/Library/AlbumPickerView.swift (full file — the `private struct AlbumTile` to extend)
    - stellar-ios/StellarVolumiO/Views/Library/ArtistDetailView.swift (full file — its OWN near-duplicate `private struct AlbumTile` to extend identically)
    - stellar-ios/StellarVolumiO/Views/Library/AlbumTracksView.swift (full file — the `private struct TrackList`/`TrackRow` to make non-private and extend with disc headers)
  </read_first>
  <behavior>
    - `AlbumTile(album:)` given `album.badge = "352.8kHz/24bit FLAC"` renders that text as a small
      overlay/caption on the tile; given `album.badge == nil`, no badge element renders.
    - `AlbumTracksView` given `album.discCount ?? 0 > 1` and tracks whose `disc` values span 1 and 2
      renders a "Disc 1" section label before disc 1's tracks and "Disc 2" before disc 2's, each
      track still tap-to-play exactly as before.
    - `AlbumTracksView` given `discCount &lt;= 1` (or nil) renders exactly as before — no section
      headers, zero regression.
  </behavior>
  <action>
    In BOTH AlbumPickerView.swift's and ArtistDetailView.swift's private `AlbumTile` struct, add a
    small badge caption (e.g. a `Text(album.badge ?? "")` wrapped in `if let badge = album.badge,
    !badge.isEmpty { ... }`, styled as a small overlay chip — match the existing dark/gold-accent
    palette already used in this file's gradient placeholder, do not invent a new color scheme)
    positioned over or below the artwork square. Keep both copies structurally identical (they
    already are duplicated; this plan does not need to deduplicate them, just extend both
    consistently — note in the SUMMARY if you choose to also deduplicate). In AlbumTracksView.swift,
    remove `private` from `TrackList`/`TrackRow` (needed by Task 3's reuse) and add disc-grouping: in
    `TrackList`'s body, when the tracks span more than one distinct `disc` value, group consecutive
    tracks by `disc` (they arrive pre-sorted from the backend) and render a section label (`Text("Disc
    \(n)")`) before each group instead of one flat `ForEach`; when all tracks share one disc value (or
    `disc == 0`), render exactly the pre-existing flat `ForEach` with no headers.
  </action>
  <acceptance_criteria>
    - Badge renders/hides correctly on both AlbumTile copies (verified via a snapshot or state-inspection test if the project has UI test infra for these views; otherwise via a build-and-manual-reasoning check documented in the SUMMARY — stellar-ios's test suite is store-level, not SwiftUI-view-level, per `reference_stellar_ios_swift_test_blocked_by_observable`).
    - Disc-grouped headers appear only when discCount &gt; 1; single-disc rendering is unchanged.
    - `scripts/build.sh` succeeds against the simulator.
  </acceptance_criteria>
  <verify>
    <automated>cd ../stellar-ios && export IOS_SIM_ID=$(xcrun simctl list devices | grep "iPhone 16 Pro" | head -1 | grep -oE '[0-9A-F-]{36}') && xcodegen generate --spec project.yml && scripts/build.sh 2>&1 | tail -60</automated>
  </verify>
  <done>Both album-grid tiles show/hide the duplicate badge correctly; a multi-disc album's track list groups by disc; single-disc albums are unaffected.</done>
</task>

<task type="auto" tdd="true">
  <name>Task 3: ArtistDetailView loose-track fallback when an artist resolves to zero albums</name>
  <files>../stellar-ios/StellarVolumiO/Views/Library/ArtistDetailView.swift</files>
  <read_first>
    - stellar-ios/StellarVolumiO/Views/Library/ArtistDetailView.swift (full file — the LazyVGrid over `store.artistAlbums` that currently renders NOTHING when empty)
    - Task 2's edit to AlbumTracksView.swift's TrackList/TrackRow (now non-private, reusable)
  </read_first>
  <behavior>
    - `store.artistAlbums.isEmpty && !store.artistLooseTracks.isEmpty` (post-Task-1's store field) ->
      ArtistDetailView renders a tappable, playable track list (reusing `TrackList`/`TrackRow`)
      instead of an empty grid; tapping a row emits the same `replaceAndPlay type: song` shape
      per-track tap already uses elsewhere in this app (mirror AlbumTracksView's `playTrack`, or
      call through a shared helper if one exists after Task 2 — your call).
    - `store.artistAlbums.isEmpty && store.artistLooseTracks.isEmpty` (the true zero-content case,
      not expected live per D-08 but must not crash) renders a plain "No albums" message, not a
      blank screen.
    - `store.artistAlbums` non-empty renders exactly as before (existing grid unaffected).
  </behavior>
  <action>
    In ArtistDetailView.swift, wrap the existing `ScrollView { LazyVGrid { ForEach(store.
    artistAlbums)... } }` in a conditional: when `store.artistAlbums.isEmpty &&
    !store.artistLooseTracks.isEmpty`, render the reused `TrackList` (from AlbumTracksView.swift,
    now non-private) fed `store.artistLooseTracks`, with each row's tap emitting `replaceAndPlay`
    with `type: "song"` for that track's `uri` (same shape as AlbumTracksView's `playTrack` — extract
    a tiny shared free function or duplicate the ~8-line emit call, your call, document the choice).
    When both are empty, render a simple centered "No albums" `Text`. Otherwise keep the existing
    grid unchanged.
  </action>
  <acceptance_criteria>
    - Manual/store-level test (or documented build-and-reasoning check, per stellar-ios's SwiftUI test limitation) confirms the three branches (loose-tracks-shown, both-empty, normal-grid) render distinctly.
    - `scripts/build.sh` succeeds.
  </acceptance_criteria>
  <verify>
    <automated>cd ../stellar-ios && export IOS_SIM_ID=$(xcrun simctl list devices | grep "iPhone 16 Pro" | head -1 | grep -oE '[0-9A-F-]{36}') && scripts/build.sh 2>&1 | tail -60</automated>
  </verify>
  <done>An artist drilled into with zero albums shows a real, tappable, playable loose-track list on iOS; every other state is unchanged.</done>
</task>

</tasks>

<threat_model>
## Trust Boundaries

| Boundary | Description |
|----------|--------------|
| Backend-pushed `badge`/`discCount`/`disc`/`looseTracks` -> rendered SwiftUI views | Plain strings/numbers from a trusted backend (localhost/LAN Bonjour-discovered), rendered via SwiftUI `Text` (no raw HTML/webview rendering anywhere in this app). |

## STRIDE Threat Register

| Threat ID | Category | Component | Disposition | Mitigation Plan |
|-----------|----------|-----------|-------------|-----------------|
| T-03-12 | Tampering | rawDict parser type-mismatch defence | mitigate | Follows the file's existing convention (`as? String ?? ""`, `as? Int ?? 0`) — a malformed/adversarial payload from a compromised backend produces empty/zero fields, never a crash. This plan's new fields use the identical defensive-default pattern. |

No new package-manager installs in this plan.
</threat_model>

<verification>
`scripts/test.sh` and `scripts/build.sh` both pass with zero regressions to any pre-existing test or
build target.
</verification>

<success_criteria>
The iPhone app renders the duplicate badge on both album grids, groups multi-disc track lists by
disc, and shows a real playable loose-track list for a zero-album artist drill-in — proven by
store-level unit tests and a clean simulator build. Live simulator visual confirmation happens in
03-08b.
</success_criteria>

<output>
Create `.planning/phases/03-browse-experience/03-08a-SUMMARY.md` when done
</output>
