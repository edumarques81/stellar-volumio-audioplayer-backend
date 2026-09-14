package socketio

import (
	"context"
	"errors"
	"net"
	"sync"
	"time"

	"github.com/edumarques81/stellar-volumio-audioplayer-backend/internal/domain/ingest"
	"github.com/rs/zerolog/log"
	"github.com/zishang520/socket.io/servers/socket/v3"
)

// Timeouts for the two script invocations. Preview is bounded by MusicBrainz's
// 1 req/s rate limit plus a cover-art fetch per album; commit additionally
// copies the audio onto the SSD, which for a large multi-disc drop is minutes.
const (
	ingestPreviewTimeout = 10 * time.Minute
	ingestCommitTimeout  = 60 * time.Minute
)

// IngestService is the slice of the ingest domain service the transport needs.
type IngestService interface {
	Available() bool
	Status() ingest.Status
	Preview(ctx context.Context) (ingest.Report, error)
	Commit(ctx context.Context, token string) (ingest.Report, error)
	// PendingPreview returns a plan that is still confirmable, for replay to
	// a client that missed the broadcast.
	PendingPreview() (ingest.Report, bool)
	// SetRunStateObserver registers the callback fired on each edge of a run.
	// It is the only way the transport can learn that a run started, because
	// the flag lives inside the service for the duration of the call.
	//
	// Single slot, last writer wins: exactly one IngestHandlers per Service,
	// or the second silently detaches the first and the first's clients never
	// hear about another run.
	SetRunStateObserver(func(running bool))
}

// ingestEmitter is the one method these handlers need from a connected client.
// *socket.Socket satisfies it; tests supply a recorder.
type ingestEmitter interface {
	Emit(ev string, args ...any) error
}

// IngestHandlers exposes the drop-box ingest as three socket events:
//
//	ingest:status   -> pushIngestStatus    what is waiting, and are we busy
//	ingest:preview  -> pushIngestPreview   dry run; carries the plan token
//	ingest:commit   -> pushIngestResult    executes the previewed plan
//
// Auth mirrors the power actions (see SystemActionHandlers): loopback callers
// are always authorized, everyone else must match STELLAR_INGEST_TRUSTED_REMOTES.
// The kiosk loads http://localhost:3000, so the LCD button needs no allowlist
// entry at all — only remote controllers (the iPhone) do.
//
// The gate covers all three events, not just the mutating one: the status
// payload lists filenames from a private share, and a controller that may not
// ingest has no business enumerating them either.
//
// Results are broadcast rather than replied. A commit runs for minutes and its
// outcome has to reach the LCD even when the phone started it, otherwise the
// two surfaces disagree about what is still in the inbox.
type IngestHandlers struct {
	svc         IngestService
	trustedNets []*net.IPNet
	// broadcast fans a payload out to every connected client. Injectable so
	// handler tests can observe it without a live Socket.IO server.
	broadcast func(event string, payload any)

	// emitMu serializes every ingest push — and, the part that actually
	// matters, is held across the *read* of the state each push reports.
	//
	// Two goroutines produce status: the connect-time hydration for one
	// client, and the broadcast on a run's trailing edge. Unsynchronised they
	// inverted: hydration sampled `busy: true`, the run then finished and
	// broadcast `busy: false`, and hydration's older frame was written last.
	// The client was left soft-locked on a run that had finished, with nothing
	// left to correct it until its next reconnect.
	//
	// Serializing the sends alone would not fix that — the stale snapshot
	// would still be the last one written. The snapshot has to be taken inside
	// the same critical section, so a state read later can never be sent
	// earlier.
	//
	// Ordering the calls does order the wire, but not because the library
	// writes synchronously — the websocket transport hands the actual write to
	// a goroutine. It clears `writable` synchronously before spawning it and
	// only sets it again once the write has landed, and flush() dispatches
	// nothing while that flag is down; everything else queues in a FIFO write
	// buffer. So the frames leave in call order via that interlock. Worth
	// knowing, because the library carries a `// Needs further investigation`
	// on the offload and nothing here would catch it changing.
	//
	// Lock order is always emitMu -> the service's own lock, never the
	// reverse: the run-state observer is invoked with the service unlocked.
	emitMu sync.Mutex
}

// NewIngestHandlers builds the bundle. trustedSpecs takes the same IP/CIDR
// forms as the power-action allowlist; a malformed spec is a hard error so a
// typo fails loudly at boot instead of silently refusing the phone forever.
func NewIngestHandlers(svc IngestService, server *Server, trustedSpecs []string) (*IngestHandlers, error) {
	nets, err := parseTrustedSpecs(trustedSpecs)
	if err != nil {
		return nil, err
	}
	h := &IngestHandlers{svc: svc, trustedNets: nets}
	h.broadcast = func(event string, payload any) {
		if server != nil && server.io != nil {
			server.io.Emit(event, payload)
		}
	}
	if svc != nil {
		// Both edges, re-snapshotted rather than derived from the bool: the
		// payload also carries the inbox listing, which a commit has just
		// changed by the time the trailing edge fires.
		svc.SetRunStateObserver(func(bool) { h.broadcastStatus() })
	}
	return h, nil
}

// RegisterHandlers attaches the ingest events to a client.
//
// All three are dispatched to a goroutine. Preview and commit obviously, since
// both shell out to the ingest script for minutes. Status used to answer
// inline on the grounds that it is only a directory listing, and that stopped
// being true when it started taking emitMu: the longest holder of that lock is
// a connect-time hydration walking the whole drop-box to hash a pending plan,
// and the kiosk reconnects on an idle timer. Blocking here stalls the client's
// entire event loop.
func (h *IngestHandlers) RegisterHandlers(client *socket.Socket) {
	client.On("ingest:status", func(_ ...any) {
		go h.handleStatus(client, extractRemoteIP(client))
	})
	client.On("ingest:preview", func(_ ...any) {
		go h.handlePreview(client, extractRemoteIP(client))
	})
	client.On("ingest:commit", func(args ...any) {
		go h.handleCommit(client, extractRemoteIP(client), ingestToken(args...))
	})
}

// PushTo hydrates a freshly-connected client with the ingest state it would
// otherwise only learn from a broadcast it was not around for.
//
// A preview runs for minutes. A phone that locks its screen or loses Wi-Fi
// during one misses `pushIngestPreview` entirely and comes back with no plan
// on screen, no Import button, and no way to recover except paying for a
// second full run — which is exactly what a user does not want to do after
// waiting for the first. Replaying here mirrors what the connect-time batch
// already does for AirPlay state.
//
// Status goes out unconditionally, the plan only when there is one. That
// asymmetry is the point: status is the only event that ever sets the clients'
// `isAvailable`, and the entire ingest card renders behind it, so a client that
// reconnects between runs must still get one or the feature is invisible to it
// for the rest of the session — and the client that needs it most is exactly
// the one whose own `ingest:status` was lost with the connection that carried
// it. It also re-establishes `busy` for a client that was away when its own
// commit finished: `pushIngestResult` is a one-shot broadcast to whoever
// happened to be connected, and a commit runs for minutes, so a locked screen
// or a Wi-Fi blip loses it for good. Clearing the resulting stuck spinner is
// the client's job (each surface drops its own phase latch on connect), but it
// cannot do it against a stale `busy` it was never sent a correction for —
// observed 2026-09-11, a four-minute commit left the iPad spinning for two and
// a half hours.
//
// The same auth gate as the interactive events applies: an unauthorized
// controller must not learn the inbox's filenames just by connecting. Refusals
// are silent — this is a push nobody asked for, so an error banner would be
// noise.
func (h *IngestHandlers) PushTo(client *socket.Socket) {
	if h == nil || client == nil {
		return
	}
	h.pushTo(client, extractRemoteIP(client))
}

func (h *IngestHandlers) pushTo(em ingestEmitter, ip string) {
	if !h.isAuthorized(ip) {
		return
	}
	// Both reads happen inside emitMu, so neither this status nor this plan
	// can be overtaken by a broadcast describing newer state (see emitMu).
	h.emitMu.Lock()
	defer h.emitMu.Unlock()

	// Status first: the clients derive "a run is in flight" from it, and a
	// plan arriving before that would flash a confirmable button on a surface
	// that is actually mid-commit.
	h.emit(em, "pushIngestStatus", h.svc.Status())
	report, ok := h.svc.PendingPreview()
	if !ok {
		return
	}
	h.emit(em, "pushIngestPreview", report)
}

// IngestErrorEvent is the payload of pushIngestError.
type IngestErrorEvent struct {
	Phase string `json:"phase"` // status | preview | commit
	Error string `json:"error"`
	// Retryable marks errors the client can clear on its own by previewing
	// again, as opposed to the ones that need a human.
	Retryable bool `json:"retryable"`
}

func (h *IngestHandlers) handleStatus(em ingestEmitter, ip string) {
	if !h.authorize(em, ip, "status") {
		return
	}
	h.emitMu.Lock()
	defer h.emitMu.Unlock()
	h.emit(em, "pushIngestStatus", h.svc.Status())
}

func (h *IngestHandlers) handlePreview(em ingestEmitter, ip string) {
	if !h.authorize(em, ip, "preview") {
		return
	}

	log.Info().Str("remote_ip", ip).Msg("ingest: preview requested")
	// No status broadcast here: the run announces its own edges through the
	// observer wired in NewIngestHandlers, which is the only place that can
	// report `busy: true` — the flag is set inside Preview and cleared before
	// it returns, so anything emitted around this call reports idle twice.

	ctx, cancel := context.WithTimeout(context.Background(), ingestPreviewTimeout)
	defer cancel()

	report, err := h.svc.Preview(ctx)
	if err != nil {
		h.emitErrorWithStatus(em, "preview", err)
		return
	}

	log.Info().
		Int("would_ingest", report.Summary.WouldIngest).
		Int("refused", report.Summary.Refused).
		Msg("ingest: preview complete")
	h.broadcastOrdered("pushIngestPreview", report)
}

func (h *IngestHandlers) handleCommit(em ingestEmitter, ip, token string) {
	if !h.authorize(em, ip, "commit") {
		return
	}
	if token == "" {
		h.emitError(em, "commit", ingest.ErrNoPlan)
		return
	}

	log.Info().Str("remote_ip", ip).Msg("ingest: commit requested")
	// Edges come from the run itself; see handlePreview.

	ctx, cancel := context.WithTimeout(context.Background(), ingestCommitTimeout)
	defer cancel()

	report, err := h.svc.Commit(ctx, token)
	if err != nil {
		h.emitErrorWithStatus(em, "commit", err)
		return
	}

	log.Info().
		Int("ingested", report.Summary.Ingested).
		Int("refused", report.Summary.Refused).
		Int("audio_altered", report.Summary.AudioAltered).
		Msg("ingest: commit complete")
	h.broadcastOrdered("pushIngestResult", report)
}

// ingestToken pulls the plan token out of the event args, accepting both a
// bare string and the `{token: "..."}` object the JS and Swift clients find
// natural to send.
func ingestToken(args ...any) string {
	if len(args) == 0 {
		return ""
	}
	switch v := args[0].(type) {
	case string:
		return v
	case map[string]interface{}:
		return getString(v, "token")
	}
	return ""
}

// authorize gates an event, emitting a sanitized refusal to the caller and
// logging the detail for ops.
func (h *IngestHandlers) authorize(em ingestEmitter, ip, phase string) bool {
	if h.isAuthorized(ip) {
		return true
	}
	log.Warn().Str("remote_ip", ip).Str("phase", phase).Msg("ingest rejected: caller not authorized")
	h.emit(em, "pushIngestError", IngestErrorEvent{Phase: phase, Error: "unauthorized"})
	return false
}

func (h *IngestHandlers) isAuthorized(remoteIP string) bool {
	if isLoopback(remoteIP) {
		return true
	}
	parsed := net.ParseIP(remoteIP)
	if parsed == nil {
		return false
	}
	for _, n := range h.trustedNets {
		if n.Contains(parsed) {
			return true
		}
	}
	return false
}

// emitError replies to the requesting client only. A failure belongs to the
// client that caused it; broadcasting would make every surface show an error
// nobody there triggered.
func (h *IngestHandlers) emitError(em ingestEmitter, phase string, err error) {
	log.Warn().Err(err).Str("phase", phase).Msg("ingest failed")
	h.emit(em, "pushIngestError", IngestErrorEvent{
		Phase:     phase,
		Error:     err.Error(),
		Retryable: errors.Is(err, ingest.ErrStalePlan) || errors.Is(err, ingest.ErrBusy),
	})
}

// emitErrorWithStatus is emitError plus a fresh status, for the failures that
// never reached the run and therefore produced no observer edges.
//
// ErrStalePlan is the one that matters: it means the inbox moved under the
// plan, so the requester's cached listing is wrong by definition, and the
// old handler-level `defer broadcastStatus()` used to refresh it. ErrBusy is
// excluded because the run that refused this one is itself broadcasting both
// its edges, and a second frame here would only race them.
//
// Status goes first, for the same reason it does in pushTo: the clients react
// to the error by clearing their plan, and they should do that against the
// listing that caused it.
func (h *IngestHandlers) emitErrorWithStatus(em ingestEmitter, phase string, err error) {
	if !errors.Is(err, ingest.ErrBusy) {
		h.emitMu.Lock()
		h.emit(em, "pushIngestStatus", h.svc.Status())
		h.emitMu.Unlock()
	}
	h.emitError(em, phase, err)
}

func (h *IngestHandlers) emit(em ingestEmitter, event string, payload any) {
	if em == nil {
		return
	}
	// A dropped emit means the client vanished mid-run; the ingest itself is
	// unaffected and there is nothing useful to do about it here.
	_ = em.Emit(event, payload)
}

// broadcastStatus snapshots and fans out in one critical section. Called on
// both edges of every run, and nowhere else — the handlers deliberately do not
// bracket their own calls with it (see handlePreview).
func (h *IngestHandlers) broadcastStatus() {
	h.emitMu.Lock()
	defer h.emitMu.Unlock()
	h.broadcast("pushIngestStatus", h.svc.Status())
}

// broadcastOrdered fans out an already-built payload, in line behind whatever
// else is being pushed. A plan or a result that jumped ahead of the status
// describing the same run would arrive at a client that still believes the
// previous state.
func (h *IngestHandlers) broadcastOrdered(event string, payload any) {
	h.emitMu.Lock()
	defer h.emitMu.Unlock()
	h.broadcast(event, payload)
}
