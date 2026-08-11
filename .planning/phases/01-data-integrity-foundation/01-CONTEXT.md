# Phase 1: Data Integrity Foundation - Context

**Gathered:** 2026-08-11
**Status:** Ready for planning
**Source:** Synthesized from the design questions the user answered during `/gsd:new-project` on
2026-08-11 (recorded in `.planning/PROJECT.md` § Key Decisions). No separate `/gsd:discuss-phase`
run — the decisions below were made explicitly by the user, not inferred.

<domain>
## Phase Boundary

MPD's database and every backend browse path reflect only real audio files, and any future data gap
is visible rather than silently dropped. Covers `._` resource-fork removal (DATA-03), defensive
backend hardening against them (DATA-04), the untagged-file retag (DATA-01), and skipped-file
observability (DATA-02).

Artist-name normalisation, artwork re-keying, and UI badges are **out of this phase** — they are
Phases 2 and 3.

</domain>

<decisions>
## Implementation Decisions

### `._` resource-fork junk (DATA-03)
- **D-01:** Delete the `._` files from the SSD at source **and** harden the backend. The user chose
  this over backend-only filtering or `.mpdignore`, because filtering alone leaves MPD's own stats
  permanently wrong (1369 indexed vs 803 real).
- **D-02:** The `._` files are macOS sidecar metadata, **not** user content — deleting them loses
  nothing. The real audio files they shadow must never be touched.
- **D-03:** Deletion must be preceded by showing the user the exact file list, per the standing
  constraint that `/mnt/ssd/Music` holds irreplaceable masters. Confirm, then delete, then
  `mpc update` and verify.

### Untagged files (DATA-01)
- **D-04:** **The user retags, not the agent.** The user explicitly chose "You do it in your tagger"
  over "I do it on the Pi". The agent must NOT write tags to files under `/mnt/ssd/Music`.
- **D-05:** The phase deliverable is a precise, copy-pasteable list of the 16 affected real songs
  grouped by folder, each with a **recommended** `Album` and `AlbumArtist` value derived from the
  folder name and existing `Artist` tag. The user applies them; the agent then verifies.
- **D-06:** A folder-name fallback in `GetAlbumDetails` was explicitly **rejected** — the user chose
  clean source data over a heuristic that must parse strings like `..._FLAC_352k-24b`. Do not
  implement one.

### Skipped-file observability (DATA-02)
- **D-07:** The `if album == "" { continue }` skip at `internal/infra/mpd/client.go:749` stays, but
  it must stop being silent: count what it skips and surface that count.
- **D-08:** Surface via both a log line and a field on the existing cache-status payload
  (`pushLibraryCacheStatus` / `CacheStats`), so it is visible without SSH. Adding a field to an
  existing payload is additive and does not break the v2/v3/v4 Socket.IO client matrix — but it
  still requires a `docs/SOCKET-CONTRACT.md` update.
- **D-09:** Success is the count reading **0** after the DATA-01 retag, not merely that the
  mechanism exists.

### Backend hardening (DATA-04)
- **D-10:** `._` filtering must be applied at every place the backend reads MPD song lists, not just
  `GetAlbumTracks` (`internal/domain/library/service.go:485`, the only current site). At minimum
  `GetAlbumDetails`, `GetArtistAlbums`, and the cache builder paths.
- **D-11:** Prefer a single shared helper over repeated inline `strings.HasPrefix(base, "._")`
  checks, so a future call site cannot forget it.

### Claude's Discretion
- Exact naming/shape of the skipped-count field and helper function.
- Whether the skipped count is tracked per-basepath or globally.
- Log level and phrasing.
- Test-file organisation, within the TDD + table-driven constraint.

</decisions>

<specifics>
## Specific Ideas

- The reproducer the user reported, and the acceptance case for the whole phase:
  `/mnt/ssd/Music/RStrauss Also Sprach Zarathustra Till Eulenspiegel Karajn VPO__FLAC_352k-24b/`
  (MPD URI prefix `USB/…`). 10 real FLACs, `Artist: Herbert von Karajan  Wiener Philharmoniker`,
  `Format: 352800:24:2`, no `Album`, no `AlbumArtist`. The user's words: *"The streamer service
  detects the Album artist but it does not list the songs."*
- The other two affected folders: `USB/toe - The Future Is Now - WAV` (4 songs) and
  `USB/Sigxer SU-6 test` (2 songs).
- Verification is done by the agent on the real Pi, not handed back as QA.

</specifics>

<canonical_refs>
## Canonical References

**Downstream agents MUST read these before planning or implementing.**

### Project + architecture
- `/Users/eduardomarques/workspace/stellar-streamer/CLAUDE.md` — workspace layout, Pi topology,
  bit-perfect DO-NOT-TOUCH list, per-repo build/test/deploy commands
- `.planning/PROJECT.md` — verified live-system evidence, constraints, Key Decisions table
- `.planning/REQUIREMENTS.md` — DATA-01..04 wording and acceptance framing

### Wire contract (needed for DATA-02's status field)
- `/Users/eduardomarques/workspace/stellar-streamer/docs/SOCKET-CONTRACT.md` § Library cache —
  `library:cache:status` → `pushLibraryCacheStatus`, and the `CacheStats` payload shape that a
  skipped-count field would extend

### Code under change
- `internal/infra/mpd/client.go:723-800` — `GetAlbumDetails`, incl. the `if album == ""` skip at :749
- `internal/domain/library/service.go:449-557` — `GetAlbumTracks`, incl. the only existing `._`
  filter at :485
- `internal/domain/library/service.go:349-446` — `GetArtistAlbums`
- `internal/infra/cache/sqlite.go:340-399` — `GetStats` / `CacheStats` population
- `internal/infra/cache/builder.go` — cache build paths that consume MPD song lists

</canonical_refs>

<code_context>
## Existing Code Insights

### Reusable Assets
- `GetAlbumTracks` already contains the `._` skip logic (`strings.HasPrefix(base, "._")` after
  `path.Base`) — lift it into a shared helper rather than reinventing.
- `CacheStats` already flows to clients via `pushLibraryCacheStatus`, so DATA-02 needs no new event.
- `deploy/verify-cutover.sh` establishes the PASS/FAIL gate style used for on-Pi verification; a
  data-integrity check fits that idiom.

### Established Patterns
- Table-driven Go tests throughout; `make check` = fmt + vet + golangci-lint.
- MPD access is behind the `MPDClient` interface (`internal/domain/library/service.go:32`) — tests
  fake it rather than talking to a real MPD.
- Platform-specific code uses `_linux.go` / `_darwin.go` build tags; nothing in this phase should
  need that.

### Integration Points
- Backend → MPD over the protocol (localhost:6600 on the Pi).
- Backend → clients via `pushLibraryCacheStatus` for the new skipped-count field.
- Deploy: `make build` (ARM64 static) → `scp` → `sudo systemctl restart stellar-backend`.

</code_context>

<deferred>
## Deferred Ideas

- Folder-name fallback for untagged albums — explicitly rejected (D-06), recorded in
  REQUIREMENTS.md § Out of Scope.
- Removing the now-redundant defensive `._` filter once the SSD is clean — keep it; DATA-04 exists
  precisely because a future macOS copy can reintroduce the junk.
- MPD-as-source-of-truth refactor (ARCH-01..04) — v2 milestone.
- `sort=year` / `sort=recently_added` being broken — real, but BROWSE-05, not this phase.

</deferred>

---

*Phase: 01-data-integrity-foundation*
*Context gathered: 2026-08-11*
