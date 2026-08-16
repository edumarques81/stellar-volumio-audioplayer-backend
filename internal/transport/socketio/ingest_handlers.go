package socketio

import (
	"context"
	"errors"
	"net"
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
	return h, nil
}

// RegisterHandlers attaches the ingest events to a client.
//
// Status answers inline — it is a directory listing. Preview and commit are
// dispatched to a goroutine because both shell out to the ingest script, and
// blocking here would stall the client's entire event loop.
func (h *IngestHandlers) RegisterHandlers(client *socket.Socket) {
	client.On("ingest:status", func(_ ...any) {
		h.handleStatus(client, extractRemoteIP(client))
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
// Nothing is sent when there is no plan to confirm, and the same auth gate as
// the interactive events applies: an unauthorized controller must not learn
// the inbox's filenames just by connecting. Refusals are silent — this is a
// push nobody asked for, so an error banner would be noise.
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
	report, ok := h.svc.PendingPreview()
	if !ok {
		return
	}
	// Status first: the clients derive "a run is in flight" from it, and a
	// plan arriving before that would flash a confirmable button on a surface
	// that is actually mid-commit.
	h.emit(em, "pushIngestStatus", h.svc.Status())
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
	h.emit(em, "pushIngestStatus", h.svc.Status())
}

func (h *IngestHandlers) handlePreview(em ingestEmitter, ip string) {
	if !h.authorize(em, ip, "preview") {
		return
	}

	log.Info().Str("remote_ip", ip).Msg("ingest: preview requested")
	h.broadcastStatus()
	defer h.broadcastStatus()

	ctx, cancel := context.WithTimeout(context.Background(), ingestPreviewTimeout)
	defer cancel()

	report, err := h.svc.Preview(ctx)
	if err != nil {
		h.emitError(em, "preview", err)
		return
	}

	log.Info().
		Int("would_ingest", report.Summary.WouldIngest).
		Int("refused", report.Summary.Refused).
		Msg("ingest: preview complete")
	h.broadcast("pushIngestPreview", report)
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
	h.broadcastStatus()
	defer h.broadcastStatus()

	ctx, cancel := context.WithTimeout(context.Background(), ingestCommitTimeout)
	defer cancel()

	report, err := h.svc.Commit(ctx, token)
	if err != nil {
		h.emitError(em, "commit", err)
		return
	}

	log.Info().
		Int("ingested", report.Summary.Ingested).
		Int("refused", report.Summary.Refused).
		Int("audio_altered", report.Summary.AudioAltered).
		Msg("ingest: commit complete")
	h.broadcast("pushIngestResult", report)
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

func (h *IngestHandlers) emit(em ingestEmitter, event string, payload any) {
	if em == nil {
		return
	}
	// A dropped emit means the client vanished mid-run; the ingest itself is
	// unaffected and there is nothing useful to do about it here.
	_ = em.Emit(event, payload)
}

func (h *IngestHandlers) broadcastStatus() {
	h.broadcast("pushIngestStatus", h.svc.Status())
}
