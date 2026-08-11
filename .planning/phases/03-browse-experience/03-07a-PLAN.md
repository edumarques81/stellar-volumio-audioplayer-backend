---
phase: 03-browse-experience
plan: 07a
type: execute
wave: 5
depends_on: ["03-06"]
files_modified:
  - ../Volumio2-UI/src/lib/stores/library.ts
  - ../Volumio2-UI/src/lib/stores/__tests__/library.test.ts
  - ../Volumio2-UI/src/lib/components/redesign/AlbumPage.svelte
  - ../Volumio2-UI/src/lib/components/redesign/__tests__/AlbumPage.test.ts
  - ../Volumio2-UI/src/lib/components/redesign/AlbumTrackList.svelte
  - ../Volumio2-UI/src/lib/components/redesign/__tests__/AlbumTrackList.test.ts
  - ../Volumio2-UI/src/lib/components/redesign/LibraryView.svelte
  - ../Volumio2-UI/src/lib/components/redesign/__tests__/LibraryView.test.ts
autonomous: true
requirements: [ARTIST-04, BROWSE-01, BROWSE-03, BROWSE-04, BROWSE-07]
user_setup: []

must_haves:
  truths:
    - "On the LCD, a duplicate album (e.g. The Future Is Now) shows its badge text somewhere in the AlbumPage view"
    - "On the LCD, a unique album shows no badge"
    - "On the LCD, a multi-disc album's track list groups tracks under Disc N headers instead of one continuous unbroken list"
    - "On the LCD, drilling into an artist that resolves to zero albums shows a playable loose-song list with a working Play All action, not an empty screen"
---

<objective>
Implement the LCD-side (Volumio2-UI, branch `master`) rendering for all three phase features against
the now-deployed, contract-locked backend from 03-06: the duplicate-disambiguation badge (BROWSE-01/
BROWSE-03), the multi-disc grouped-album track view (BROWSE-07), and the loose-track empty-state
fallback (ARTIST-04/BROWSE-04).

Purpose: This is the LCD half of BROWSE-01/02/03/04/07 — the backend already computes everything;
this plan only renders it. The LCD's Library screen is a single-album carousel (LibraryView.svelte ->
AlbumPage.svelte), not a literal tile grid, so "the badge on the LCD's album interface" (ROADMAP
criterion 1) means rendering `album.badge` on the current AlbumPage, and "drilling into" a multi-disc
album (ROADMAP criterion 5) means AlbumTrackList grouping the already-inline track list by disc
rather than a separate navigation step.

Output: Badge visible on AlbumPage for duplicate albums, absent for unique ones; AlbumTrackList shows
"Disc N" section headers for `discCount > 1` albums; artist-drill-in shows a playable loose-track list
with a Play All action when the artist resolves to zero albums.
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
<!-- The backend contract this plan renders against, locked by 03-06. -->

```typescript
// Volumio2-UI/src/lib/stores/library.ts — existing Album/Track interfaces gain:
interface Album {
  // ...existing fields unchanged...
  badge?: string;      // e.g. "352.8kHz/24bit FLAC", "Disc 3", "USB" — render verbatim
  discCount?: number;  // >1 = multi-disc box set; uri already points at the combined-track root
}
interface Track {
  // ...existing fields unchanged...
  disc?: number;  // 1-based; tracks arrive pre-sorted by (disc, trackNumber, title)
}
interface ArtistAlbumsResponse {
  artist: string;
  albums: Album[];
  pagination: Pagination;
  looseTracks?: Track[];  // populated ONLY when albums is empty
}
```

<!-- Existing component shapes this plan extends — read the real files, these are orientation only. -->

```svelte
<!-- AlbumPage.svelte: export let album: Album; renders title/artist/meta-strip/HiResAudioStrip.
     Badge should render near the meta-strip, sourced from album.badge. -->

<!-- AlbumTrackList.svelte: export let tracks: { uri, title, duration }[] = [];
     Renders a flat <ol> with array-index-based numbering (pad2(i+1)) — this numbering convention
     MUST be preserved for discCount<=1 albums (do not regress single-disc numbering). For
     discCount>1, group consecutive same-disc tracks under a "Disc N" header, using each track's
     OWN `disc`/`trackNumber` fields (already correctly per-disc from the backend) instead of the
     flat array index. -->

<!-- LibraryView.svelte: `currentAlbum` derives from `albumsList` (either $libraryAlbums or, when
     $selectedArtist is set, $artistAlbums). When albumsList.length === 0, it currently renders
     `<div class="empty">No albums in library</div>` unconditionally. This must become conditional
     on whether a loose-track fallback exists for the current $selectedArtist. -->
```
</interfaces>

<tasks>

<task type="auto" tdd="true">
  <name>Task 1: Extend library.ts types + store for badge/discCount/disc/looseTracks</name>
  <files>../Volumio2-UI/src/lib/stores/library.ts, ../Volumio2-UI/src/lib/stores/__tests__/library.test.ts</files>
  <read_first>
    - Volumio2-UI/src/lib/stores/library.ts (Album/Track/ArtistAlbumsResponse interfaces, the pushLibraryArtistAlbums handler in initLibraryStore, clearArtistFilter)
    - Volumio2-UI/src/lib/stores/__tests__/library.test.ts (existing test patterns for the store)
  </read_first>
  <behavior>
    - `Album` and `Track` interfaces gain the new optional fields exactly as specified in
      &lt;interfaces&gt; above.
    - A new writable store `artistLooseTracks: Writable&lt;Track[]&gt;` starts empty, is set from
      `data.looseTracks ?? []` inside the existing `pushLibraryArtistAlbums` handler, and is cleared
      (`[]`) inside `clearArtistFilter()` alongside the existing `artistAlbums`/`currentLibraryIndex`
      resets.
    - A new action `libraryActions.playLooseTracks(tracks: Track[])` clears the queue, adds every
      track's `uri` in order, plays from position 0, and navigates to the Player view — mirroring
      the existing `playAlbum`/`replaceQueueAndPlay` socket-emit shape (`clearQueue`, `addToQueue`
      with `{uri: [...]}`, `play({value: 0})`, `viewActions.goToPlayer()`), but synchronous (no
      fetch-then-subscribe dance needed — `looseTracks` already arrived with the artist-albums
      payload, unlike `replaceQueueAndPlay`'s async per-album track fetch).
  </behavior>
  <action>
    Add `badge?: string; discCount?: number;` to the `Album` interface, `disc?: number;` to `Track`,
    and `looseTracks?: Track[];` to `ArtistAlbumsResponse`. Add
    `export const artistLooseTracks = writable&lt;Track[]&gt;([]);` near the existing
    `artistAlbums` store declaration. In `initLibraryStore`'s `pushLibraryArtistAlbums` handler, add
    `artistLooseTracks.set(data?.looseTracks?.map(fixAlbumArt) ?? []);` (reuse the existing
    `fixAlbumArt` helper for consistency, even though tracks carry `albumArt` not `albumart` — check
    the actual key name on `Track` before assuming). In `libraryActions.clearArtistFilter`, add
    `artistLooseTracks.set([]);`. Add `playLooseTracks(tracks: Track[])` to `libraryActions` per the
    &lt;behavior&gt; spec. Add test cases to library.test.ts: `pushLibraryArtistAlbums` with
    `looseTracks` populates `artistLooseTracks`; `clearArtistFilter` resets it; `playLooseTracks`
    emits the expected `clearQueue`/`addToQueue`/`play` socket calls in order.
  </action>
  <acceptance_criteria>
    - New/modified tests pass; existing library.test.ts suite has zero regressions.
    - `npx tsc --noEmit` passes (Vitest passing != tsc passing per project convention).
  </acceptance_criteria>
  <verify>
    <automated>cd ../Volumio2-UI && npm run test:run src/lib/stores/__tests__/library.test.ts && npx tsc --noEmit</automated>
  </verify>
  <done>library.ts exposes badge/discCount/disc/looseTracks with a working artistLooseTracks store and playLooseTracks action, fully tested.</done>
</task>

<task type="auto" tdd="true">
  <name>Task 2: Render the badge on AlbumPage + disc-grouped headers in AlbumTrackList</name>
  <files>../Volumio2-UI/src/lib/components/redesign/AlbumPage.svelte, ../Volumio2-UI/src/lib/components/redesign/__tests__/AlbumPage.test.ts, ../Volumio2-UI/src/lib/components/redesign/AlbumTrackList.svelte, ../Volumio2-UI/src/lib/components/redesign/__tests__/AlbumTrackList.test.ts</files>
  <read_first>
    - Volumio2-UI/src/lib/components/redesign/AlbumPage.svelte (meta-strip layout, existing `data-testid="album-meta-strip"` convention)
    - Volumio2-UI/src/lib/components/redesign/AlbumTrackList.svelte (the full existing file — 56 lines, flat `<ol>` with `pad2(i+1)` numbering)
    - Volumio2-UI/src/lib/components/redesign/__tests__/AlbumTrackList.test.ts (existing render-assertion style)
  </read_first>
  <behavior>
    - `AlbumPage` given `album.badge = "352.8kHz/24bit FLAC"` renders that text somewhere visible
      near the meta-strip with a `data-testid="album-duplicate-badge"`; given `album.badge`
      undefined/empty, that element is absent entirely (BROWSE-03 — no visual noise for unique
      albums).
    - `AlbumTrackList` given tracks with uniform/absent `disc` (or `discCount` not passed / &lt;=1)
      renders EXACTLY as before — flat list, `pad2(i+1)` numbering, zero regression.
    - `AlbumTrackList` given tracks spanning `disc` 1 and 2 (each internally sorted by
      `trackNumber`) and a truthy multi-disc signal renders a "Disc 1" header, disc 1's tracks
      numbered from their own `trackNumber` (not the flat array index), then a "Disc 2" header,
      disc 2's tracks similarly numbered.
  </behavior>
  <action>
    In AlbumPage.svelte, add a badge element (simple styled `<span>`/`<div>`, matching the existing
    dark/gold LCD design tokens already used elsewhere in this file — do not invent a new color
    palette) inside the `info-zone`, conditionally rendered `{#if album.badge}`, with
    `data-testid="album-duplicate-badge"`. In AlbumTrackList.svelte, extend the `tracks` prop type to
    `{ uri: string; title: string; duration: number; disc?: number; trackNumber?: number }[]` and add
    an exported `discCount: number = 0` prop (passed down from AlbumPage as `album.discCount ?? 0`,
    wired at the `<AlbumTrackList {tracks} discCount={album.discCount ?? 0} />` call site in
    AlbumPage.svelte's `tracklist-zone`). When `discCount > 1`, derive disc-grouped sections (group
    consecutive tracks by `disc`, preserving arrival order — tracks already arrive disc-sorted from
    the backend) and render a header row (`data-testid="disc-header"`, text `Disc {n}`) before each
    group; within a group, use `pad2(t.trackNumber || i+1)` for numbering instead of the flat global
    index. When `discCount <= 1`, render exactly the pre-existing flat markup unchanged. Extend
    AlbumTrackList.test.ts and AlbumPage.test.ts with cases for both behaviors above.
  </action>
  <acceptance_criteria>
    - New tests for badge presence/absence and disc-grouped headers pass.
    - Every pre-existing AlbumPage.test.ts / AlbumTrackList.test.ts case still passes unmodified (single-disc rendering is byte-identical to before).
    - `npx tsc --noEmit` passes.
  </acceptance_criteria>
  <verify>
    <automated>cd ../Volumio2-UI && npm run test:run src/lib/components/redesign/__tests__/AlbumPage.test.ts src/lib/components/redesign/__tests__/AlbumTrackList.test.ts && npx tsc --noEmit</automated>
  </verify>
  <done>Duplicate badge renders/hides correctly; multi-disc albums show Disc N headers; single-disc rendering is unchanged.</done>
</task>

<task type="auto" tdd="true">
  <name>Task 3: LibraryView loose-track fallback when a drilled-in artist resolves to zero albums</name>
  <files>../Volumio2-UI/src/lib/components/redesign/LibraryView.svelte, ../Volumio2-UI/src/lib/components/redesign/__tests__/LibraryView.test.ts</files>
  <read_first>
    - Volumio2-UI/src/lib/components/redesign/LibraryView.svelte (the `{#if currentAlbum} ... {:else} <div class="empty">No albums in library</div> {/if}` branch — the ARTIST-04/BROWSE-04 dead end to fix)
    - Volumio2-UI/src/lib/components/redesign/__tests__/LibraryView.test.ts (existing test harness/mocking pattern for this component)
  </read_first>
  <behavior>
    - `$selectedArtist` set, `$artistAlbums` empty, `$artistLooseTracks` non-empty -&gt; LibraryView
      renders a playable loose-track view (reusing `AlbumTrackList` for the listing plus a "Play
      All" button wired to `libraryActions.playLooseTracks($artistLooseTracks)`) instead of the
      generic "No albums in library" message.
    - `$selectedArtist` null (the full-library view, not an artist drill-in) with an empty
      `$libraryAlbums` still shows the existing generic empty message — this fallback is scoped to
      the per-artist drill-in only, never the whole-library case (which per D-08 should not occur in
      practice and has no loose-track data to show anyway).
    - `$artistAlbums` non-empty renders exactly as before (no regression to the normal carousel).
  </behavior>
  <action>
    Import `artistLooseTracks` from `$lib/stores/library`. In LibraryView.svelte's `albums` page-kind
    branch, change the `{:else}` fallback: when `$selectedArtist !== null &&
    $artistLooseTracks.length > 0`, render a new inline block (or extract a small
    `LooseTracksView.svelte` if that reads cleaner — your call, document the choice in the SUMMARY)
    showing the artist name, an `AlbumTrackList` fed `$artistLooseTracks` (mapped to the `{uri,
    title, duration}` shape it expects), and a "Play All" affordance calling
    `libraryActions.playLooseTracks($artistLooseTracks)`. Otherwise, keep the existing generic empty
    message unchanged. Add `data-testid="library-loose-tracks"` on the new block's root. Extend
    LibraryView.test.ts with cases for both the fallback-shown and fallback-not-shown (whole-library
    empty, normal artist-albums-present) scenarios.
  </action>
  <acceptance_criteria>
    - New loose-track fallback tests pass.
    - The pre-existing `library-empty` test case (whole-library, no artist selected) still passes unmodified.
    - `npx tsc --noEmit` passes.
  </acceptance_criteria>
  <verify>
    <automated>cd ../Volumio2-UI && npm run test:run src/lib/components/redesign/__tests__/LibraryView.test.ts && npx tsc --noEmit</automated>
  </verify>
  <done>An artist drilled into with zero albums shows a playable loose-track list with a working Play All action; every other empty/populated case is unchanged.</done>
</task>

</tasks>

<threat_model>
## Trust Boundaries

| Boundary | Description |
|----------|--------------|
| Backend-pushed `badge`/`discCount`/`disc`/`looseTracks` values -> rendered DOM | All values are plain strings/numbers from a trusted backend (localhost/LAN Socket.IO, no third-party content); rendered via Svelte's default text-binding (auto-escaped), never `{@html}`. |

## STRIDE Threat Register

| Threat ID | Category | Component | Disposition | Mitigation Plan |
|-----------|----------|-----------|-------------|-----------------|
| T-03-10 | Tampering (XSS) | badge text rendering | accept | Svelte text interpolation (`{album.badge}`) is auto-escaped by default; no `{@html}` is introduced by this plan. |

No new package-manager installs in this plan.
</threat_model>

<verification>
`npm run test:run` (full suite) and `npx tsc --noEmit` both pass with zero regressions to any
pre-existing test.
</verification>

<success_criteria>
The LCD renders the duplicate badge, groups multi-disc track lists by disc, and shows a playable
loose-track list for a zero-album artist drill-in — all proven by unit/component tests against the
locked backend contract. Live LCD visual confirmation happens in 03-07b.
</success_criteria>

<output>
Create `.planning/phases/03-browse-experience/03-07a-SUMMARY.md` when done
</output>
