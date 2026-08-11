# Deferred Items — Phase 01 data-integrity-foundation

Items discovered during execution that are out of scope for the plan that found them
(per the executor scope-boundary rule: log, don't fix, don't re-run builds hoping they
resolve).

## Repo-wide `gofmt` drift (found during 01-01, task 2 verification)

**Found during:** 01-01 Task 2, running `make check` (which runs `go fmt ./...` as its
first step).

**Issue:** `go fmt ./...` reformats ~30 pre-existing files across the repo (mostly struct
tag column re-alignment — e.g. `internal/infra/cache/types.go`'s `CachedAlbum` struct tag
comments) that this plan never touched. This indicates the repo's checked-in formatting
was produced by a different `gofmt`/Go toolchain version than the one installed in this
execution environment (`go 1.25.5` per `go.mod`).

**Files affected (30, non-exhaustive — whatever `go fmt ./...` currently touches):**
`internal/audio/controller.go`, `internal/audio/controller_test.go`,
`internal/domain/device/service_test.go`, `internal/domain/library/types.go`,
`internal/domain/localmusic/service_test.go`, `internal/domain/localmusic/types.go`,
`internal/domain/player/service_test.go`, `internal/domain/sources/discoverer_linux.go`,
`internal/domain/sources/mounter_remote.go`, `internal/domain/sources/mounter_remote_test.go`,
`internal/domain/streaming/qobuz/credentials_test.go`, `internal/domain/streaming/qobuz/service.go`,
`internal/domain/streaming/types.go`, `internal/domain/upnp/didl.go`,
`internal/domain/upnp/ssdp_test.go`, `internal/infra/cache/sqlite.go`,
`internal/infra/cache/sqlite_test.go`, `internal/infra/cache/types.go`,
`internal/infra/enrichment/musicbrainz.go`, `internal/infra/enrichment/types.go`,
`internal/infra/enrichment/worker_test.go`, `internal/infra/paths/paths_darwin.go`,
`internal/infra/spectrum/spectrum.go`, `internal/transport/mdns/advertiser_test.go`,
`internal/transport/socketio/airplay_command.go`,
`internal/transport/socketio/airplay_command_test.go`,
`internal/transport/socketio/audioengine_handlers.go`,
`internal/transport/socketio/cache_handlers.go`,
`internal/transport/socketio/remote_audio_test.go`, `internal/transport/socketio/server.go`,
`internal/transport/socketio/server_test.go`, `internal/transport/socketio/system_actions_test.go`,
`cmd/stellar-airplay/bundler.go`.

**Action taken:** Reverted the unrelated reformatting (`git checkout -- <files>`) so the
01-01 commits only contain the musicfile/library changes this plan authorized. Verified
gofmt/vet/golangci-lint clean scoped to the two files this plan actually touched
(`internal/infra/musicfile/musicfile.go`, `internal/domain/library/service.go`) instead of
running the repo-wide `make check` target, to avoid re-triggering the drift.

**Not fixed because:** Out of scope — pre-existing, not caused by this plan's changes, and
touches ~30 files across unrelated packages/phases. Reformatting them is a legitimate but
separate housekeeping task.

**Recommended follow-up:** Either (a) pin/document the exact `go` toolchain version used to
format this repo so `gofmt` output is reproducible across dev machines, or (b) run
`go fmt ./...` once as a deliberate, isolated `style(...)` commit reviewed on its own, not
bundled into a feature plan. Flag to the user before the next phase that starts touching
these files, since a scoped `make check` in later plans will hit the same drift.

## golangci-lint pre-existing findings (repo-wide, found during 01-01 verification)

**Found during:** 01-01 Task 2, running `make check` (which chains `fmt vet lint`).

**Issue:** `golangci-lint run ./...` reports 62 pre-existing issues (50 errcheck, 1
ineffassign, 7 staticcheck, 4 unused) spread across `cmd/stellar`, `internal/infra/lcd`,
`internal/infra/mpd`, `internal/infra/enrichment`, `internal/domain/artwork`,
`internal/domain/localmusic`, `internal/domain/streaming/qobuz`, `internal/domain/sources`,
`internal/transport/socketio`. None are in files this plan touched — confirmed via
`golangci-lint run ./internal/infra/musicfile/... ./internal/domain/library/...` → `0 issues`.

**Not fixed because:** Out of scope per the scope-boundary rule — pre-existing, not caused
by this plan, and touches files well outside 01-01's `files_modified` list.

**Recommended follow-up:** A dedicated lint-cleanup plan/phase, or fix incrementally as
each file is legitimately touched by future phase work.
