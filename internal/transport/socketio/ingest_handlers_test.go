package socketio

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/edumarques81/stellar-volumio-audioplayer-backend/internal/domain/ingest"
)

// --- fakes -----------------------------------------------------------------

type emitted struct {
	event   string
	payload any
}

// recorder stands in for a connected *socket.Socket.
type recorder struct {
	mu   sync.Mutex
	sent []emitted
}

func (r *recorder) Emit(ev string, args ...any) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	var payload any
	if len(args) > 0 {
		payload = args[0]
	}
	r.sent = append(r.sent, emitted{event: ev, payload: payload})
	return nil
}

func (r *recorder) events() []emitted {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]emitted(nil), r.sent...)
}

func (r *recorder) find(event string) (emitted, bool) {
	for _, e := range r.events() {
		if e.event == event {
			return e, true
		}
	}
	return emitted{}, false
}

type fakeIngest struct {
	status        ingest.Status
	previewReport ingest.Report
	previewErr    error
	commitReport  ingest.Report
	commitErr     error
	// pending is what PendingPreview hands back on a connect-time replay. Zero
	// value means "no plan on file", which is the common case.
	pending    ingest.Report
	hasPending bool
	// statusFn replaces the canned status when set, so a test can make a
	// snapshot block and force an interleaving.
	statusFn func() ingest.Status

	mu           sync.Mutex
	previewCalls int
	commitCalls  int
	commitTokens []string
	running      bool
	observer     func(running bool)
}

func (f *fakeIngest) Available() bool { return f.status.Available }

func (f *fakeIngest) Status() ingest.Status {
	if f.statusFn != nil {
		return f.statusFn()
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	st := f.status
	if f.running {
		st.Busy = true
	}
	return st
}

func (f *fakeIngest) SetRunStateObserver(fn func(running bool)) {
	f.mu.Lock()
	f.observer = fn
	f.mu.Unlock()
}

// refusedBeforeRun lists the errors the real Service returns *before* it
// enters execute, and which therefore announce no edges at all. Modelling this
// matters: a fake that fired edges for a stale plan would happily bless a
// handler that reported busy for a run that never started.
func refusedBeforeRun(err error) bool {
	return errors.Is(err, ingest.ErrBusy) ||
		errors.Is(err, ingest.ErrStalePlan) ||
		errors.Is(err, ingest.ErrNoPlan) ||
		errors.Is(err, ingest.ErrUnavailable)
}

// fireEdges mirrors what ingest.Service does around a real run: flip running,
// announce, run, unflip, announce. A test that skipped this would prove the
// handler emits nothing, which is exactly the bug.
func (f *fakeIngest) fireEdges() {
	f.mu.Lock()
	f.running = true
	obs := f.observer
	f.mu.Unlock()
	if obs != nil {
		obs(true)
	}

	f.mu.Lock()
	f.running = false
	obs = f.observer
	f.mu.Unlock()
	if obs != nil {
		obs(false)
	}
}

func (f *fakeIngest) Preview(context.Context) (ingest.Report, error) {
	f.mu.Lock()
	f.previewCalls++
	f.mu.Unlock()
	if refusedBeforeRun(f.previewErr) {
		return ingest.Report{}, f.previewErr
	}
	f.fireEdges()
	return f.previewReport, f.previewErr
}

func (f *fakeIngest) Commit(_ context.Context, token string) (ingest.Report, error) {
	f.mu.Lock()
	f.commitCalls++
	f.commitTokens = append(f.commitTokens, token)
	f.mu.Unlock()
	if refusedBeforeRun(f.commitErr) {
		return ingest.Report{}, f.commitErr
	}
	f.fireEdges()
	return f.commitReport, f.commitErr
}

func (f *fakeIngest) PendingPreview() (ingest.Report, bool) {
	return f.pending, f.hasPending
}

func (f *fakeIngest) calls() (preview, commit int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.previewCalls, f.commitCalls
}

// newHandlers wires a handler bundle whose broadcasts land in a slice instead
// of a live Socket.IO server.
func newHandlers(t *testing.T, svc IngestService, trusted ...string) (*IngestHandlers, *[]emitted, *sync.Mutex) {
	t.Helper()
	h, err := NewIngestHandlers(svc, nil, trusted)
	if err != nil {
		t.Fatalf("NewIngestHandlers: %v", err)
	}
	var mu sync.Mutex
	broadcasts := make([]emitted, 0, 4)
	h.broadcast = func(event string, payload any) {
		mu.Lock()
		defer mu.Unlock()
		broadcasts = append(broadcasts, emitted{event: event, payload: payload})
	}
	return h, &broadcasts, &mu
}

func hasEvent(mu *sync.Mutex, events *[]emitted, name string) (emitted, bool) {
	mu.Lock()
	defer mu.Unlock()
	for _, e := range *events {
		if e.event == name {
			return e, true
		}
	}
	return emitted{}, false
}

// --- auth ------------------------------------------------------------------

func TestIngest_AuthorizedCallers(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name    string
		ip      string
		trusted []string
		allow   bool
	}{
		{"ipv4 loopback", "127.0.0.1", nil, true},
		{"ipv6 loopback", "::1", nil, true},
		{"kiosk-style loopback range", "127.0.0.5", nil, true},
		{"lan client without allowlist", "192.168.86.30", nil, false},
		{"lan client in allowlist cidr", "192.168.86.30", []string{"192.168.86.0/24"}, true},
		{"lan client as bare ip", "192.168.86.30", []string{"192.168.86.30"}, true},
		{"lan client outside allowlist", "10.0.0.5", []string{"192.168.86.0/24"}, false},
		{"empty ip (no handshake)", "", nil, false},
		{"unparseable ip", "not-an-ip", []string{"192.168.86.0/24"}, false},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			h, _, _ := newHandlers(t, &fakeIngest{}, tc.trusted...)
			if got := h.isAuthorized(tc.ip); got != tc.allow {
				t.Fatalf("isAuthorized(%q) = %v, want %v", tc.ip, got, tc.allow)
			}
		})
	}
}

func TestIngest_UnauthorizedNeverReachesTheService(t *testing.T) {
	t.Parallel()
	for _, phase := range []string{"status", "preview", "commit"} {
		phase := phase
		t.Run(phase, func(t *testing.T) {
			t.Parallel()
			svc := &fakeIngest{status: ingest.Status{Available: true, Items: []string{"secret album"}}}
			h, _, _ := newHandlers(t, svc)
			client := &recorder{}

			switch phase {
			case "status":
				h.handleStatus(client, "192.168.86.30")
			case "preview":
				h.handlePreview(client, "192.168.86.30")
			case "commit":
				h.handleCommit(client, "192.168.86.30", "tok")
			}

			previews, commits := svc.calls()
			if previews != 0 || commits != 0 {
				t.Fatalf("service was invoked by an unauthorized caller: preview=%d commit=%d", previews, commits)
			}
			ev, ok := client.find("pushIngestError")
			if !ok {
				t.Fatalf("no pushIngestError emitted; got %+v", client.events())
			}
			payload, ok := ev.payload.(IngestErrorEvent)
			if !ok {
				t.Fatalf("payload type %T, want IngestErrorEvent", ev.payload)
			}
			// The refusal must not leak why, nor what is in the inbox.
			if payload.Error != "unauthorized" {
				t.Fatalf("error = %q, want %q", payload.Error, "unauthorized")
			}
			if payload.Phase != phase {
				t.Fatalf("phase = %q, want %q", payload.Phase, phase)
			}
			if _, leaked := client.find("pushIngestStatus"); leaked {
				t.Fatal("inbox listing leaked to an unauthorized caller")
			}
		})
	}
}

// --- status ----------------------------------------------------------------

func TestIngest_StatusRepliesToRequester(t *testing.T) {
	t.Parallel()
	want := ingest.Status{Items: []string{"Album A"}, Count: 1, Available: true}
	h, _, _ := newHandlers(t, &fakeIngest{status: want})
	client := &recorder{}

	h.handleStatus(client, "127.0.0.1")

	ev, ok := client.find("pushIngestStatus")
	if !ok {
		t.Fatalf("no pushIngestStatus; got %+v", client.events())
	}
	got, ok := ev.payload.(ingest.Status)
	if !ok {
		t.Fatalf("payload type %T, want ingest.Status", ev.payload)
	}
	if got.Count != want.Count || len(got.Items) != 1 || got.Items[0] != "Album A" {
		t.Fatalf("status = %+v, want %+v", got, want)
	}
}

// --- preview ---------------------------------------------------------------

func TestIngest_PreviewBroadcastsPlan(t *testing.T) {
	t.Parallel()
	report := ingest.Report{
		Schema: ingest.SchemaVersion,
		DryRun: true,
		Token:  "plan-token",
		Items:  []ingest.Item{{Name: "Album A", Status: "would-ingest"}},
		Summary: ingest.Summary{
			Total: 1, WouldIngest: 1,
		},
	}
	svc := &fakeIngest{status: ingest.Status{Available: true, Items: []string{}}, previewReport: report}
	h, broadcasts, mu := newHandlers(t, svc)

	h.handlePreview(&recorder{}, "127.0.0.1")

	ev, ok := hasEvent(mu, broadcasts, "pushIngestPreview")
	if !ok {
		t.Fatal("preview was not broadcast; the LCD would never see a plan the phone requested")
	}
	got, ok := ev.payload.(ingest.Report)
	if !ok {
		t.Fatalf("payload type %T, want ingest.Report", ev.payload)
	}
	if got.Token != "plan-token" {
		t.Fatalf("token = %q, want %q -- without it the confirm tap cannot commit", got.Token, "plan-token")
	}
	if !got.DryRun {
		t.Fatal("dryRun = false on a preview report")
	}
	// Busy-state bookkeeping: clients need a status before and after.
	if _, ok := hasEvent(mu, broadcasts, "pushIngestStatus"); !ok {
		t.Fatal("no pushIngestStatus broadcast around the run")
	}
}

func TestIngest_PreviewErrorGoesOnlyToTheRequester(t *testing.T) {
	t.Parallel()
	svc := &fakeIngest{
		status:     ingest.Status{Available: true, Items: []string{}},
		previewErr: ingest.ErrBusy,
	}
	h, broadcasts, mu := newHandlers(t, svc)
	client := &recorder{}

	h.handlePreview(client, "127.0.0.1")

	ev, ok := client.find("pushIngestError")
	if !ok {
		t.Fatalf("no pushIngestError; got %+v", client.events())
	}
	payload := ev.payload.(IngestErrorEvent)
	if payload.Phase != "preview" {
		t.Fatalf("phase = %q, want preview", payload.Phase)
	}
	if !payload.Retryable {
		t.Fatal("ErrBusy should be marked retryable")
	}
	if _, leaked := hasEvent(mu, broadcasts, "pushIngestError"); leaked {
		t.Fatal("error was broadcast; other surfaces would show a failure nobody there triggered")
	}
	if _, ok := hasEvent(mu, broadcasts, "pushIngestPreview"); ok {
		t.Fatal("a failed preview must not broadcast a plan")
	}
}

// --- commit ----------------------------------------------------------------

func TestIngest_CommitPassesTokenAndBroadcastsResult(t *testing.T) {
	t.Parallel()
	report := ingest.Report{
		Schema:  ingest.SchemaVersion,
		Items:   []ingest.Item{{Name: "Album A", Status: "ingested"}},
		Summary: ingest.Summary{Total: 1, Ingested: 1},
		MPD:     map[string]int{"albums": 82},
	}
	svc := &fakeIngest{status: ingest.Status{Available: true, Items: []string{}}, commitReport: report}
	h, broadcasts, mu := newHandlers(t, svc)

	h.handleCommit(&recorder{}, "127.0.0.1", "plan-token")

	svc.mu.Lock()
	tokens := append([]string(nil), svc.commitTokens...)
	svc.mu.Unlock()
	if len(tokens) != 1 || tokens[0] != "plan-token" {
		t.Fatalf("commit tokens = %v, want [plan-token]", tokens)
	}
	ev, ok := hasEvent(mu, broadcasts, "pushIngestResult")
	if !ok {
		t.Fatal("commit result was not broadcast")
	}
	if got := ev.payload.(ingest.Report); got.Summary.Ingested != 1 {
		t.Fatalf("ingested = %d, want 1", got.Summary.Ingested)
	}
}

func TestIngest_CommitWithoutTokenNeverRunsTheScript(t *testing.T) {
	t.Parallel()
	svc := &fakeIngest{status: ingest.Status{Available: true, Items: []string{}}}
	h, _, _ := newHandlers(t, svc)
	client := &recorder{}

	h.handleCommit(client, "127.0.0.1", "")

	if _, commits := svc.calls(); commits != 0 {
		t.Fatalf("commit ran %d time(s) with no token; the confirm gate is bypassed", commits)
	}
	ev, ok := client.find("pushIngestError")
	if !ok {
		t.Fatalf("no pushIngestError; got %+v", client.events())
	}
	if got := ev.payload.(IngestErrorEvent).Error; got != ingest.ErrNoPlan.Error() {
		t.Fatalf("error = %q, want %q", got, ingest.ErrNoPlan.Error())
	}
}

func TestIngest_CommitStalePlanIsRetryable(t *testing.T) {
	t.Parallel()
	svc := &fakeIngest{
		status:    ingest.Status{Available: true, Items: []string{}},
		commitErr: ingest.ErrStalePlan,
	}
	h, broadcasts, mu := newHandlers(t, svc)
	client := &recorder{}

	h.handleCommit(client, "127.0.0.1", "plan-token")

	ev, ok := client.find("pushIngestError")
	if !ok {
		t.Fatalf("no pushIngestError; got %+v", client.events())
	}
	payload := ev.payload.(IngestErrorEvent)
	if !payload.Retryable {
		t.Fatal("ErrStalePlan should be retryable -- the client fixes it by previewing again")
	}
	if _, ok := hasEvent(mu, broadcasts, "pushIngestResult"); ok {
		t.Fatal("a failed commit must not broadcast a result")
	}
}

func TestIngest_CommitOperationalErrorIsNotRetryable(t *testing.T) {
	t.Parallel()
	svc := &fakeIngest{
		status:    ingest.Status{Available: true, Items: []string{}},
		commitErr: errors.New("ingest: exit status 2"),
	}
	h, _, _ := newHandlers(t, svc)
	client := &recorder{}

	h.handleCommit(client, "127.0.0.1", "plan-token")

	ev, _ := client.find("pushIngestError")
	if payload := ev.payload.(IngestErrorEvent); payload.Retryable {
		t.Fatal("an operational failure must not be advertised as retryable")
	}
}

// --- connect-time replay ---------------------------------------------------

func TestIngestPushTo(t *testing.T) {
	t.Parallel()

	plan := ingest.Report{
		Token:   "plan-token",
		Summary: ingest.Summary{WouldIngest: 2},
		Items:   []ingest.Item{},
	}

	for _, tc := range []struct {
		name    string
		svc     *fakeIngest
		ip      string
		trusted []string
		// want is the exact event sequence the client should receive. Order
		// matters: status establishes "is a run in flight" before the plan
		// arrives, so an empty slice means "say nothing at all".
		want []string
	}{
		{
			name: "pending plan is replayed status-first",
			svc: &fakeIngest{
				status:     ingest.Status{Available: true, Count: 2},
				pending:    plan,
				hasPending: true,
			},
			ip:   "127.0.0.1",
			want: []string{"pushIngestStatus", "pushIngestPreview"},
		},
		{
			// Status is unconditional. It is the only event that ever sets
			// the clients' `isAvailable`, and the whole ingest card renders
			// behind it -- a reconnecting client that gets nothing here has
			// no ingest UI at all for the rest of its session. It is also
			// how a client whose commit finished while it was away learns
			// the run is over (busy=false), which is the only thing that
			// unsticks a stranded "Importing..." spinner.
			name: "no plan on file still sends status",
			svc: &fakeIngest{
				status: ingest.Status{Available: true, Count: 2},
			},
			ip:   "127.0.0.1",
			want: []string{"pushIngestStatus"},
		},
		{
			// Mid-commit: the plan has been spent, so there is nothing to
			// replay, but busy=true is exactly what the reconnecting client
			// needs in order to keep its spinner honest.
			name: "busy with no plan still sends status",
			svc: &fakeIngest{
				status: ingest.Status{Available: true, Count: 1, Busy: true},
			},
			ip:   "127.0.0.1",
			want: []string{"pushIngestStatus"},
		},
		{
			// The inbox filenames come off a private share; a controller that
			// may not ingest must not learn them just by connecting.
			name: "unauthorized client learns nothing",
			svc: &fakeIngest{
				status:     ingest.Status{Available: true, Count: 2},
				pending:    plan,
				hasPending: true,
			},
			ip:   "192.168.86.30",
			want: nil,
		},
		{
			name: "allowlisted remote gets the replay",
			svc: &fakeIngest{
				status:     ingest.Status{Available: true, Count: 2},
				pending:    plan,
				hasPending: true,
			},
			ip:      "192.168.86.30",
			trusted: []string{"192.168.86.0/24"},
			want:    []string{"pushIngestStatus", "pushIngestPreview"},
		},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			h, broadcasts, mu := newHandlers(t, tc.svc, tc.trusted...)
			client := &recorder{}

			h.pushTo(client, tc.ip)

			got := make([]string, 0, 2)
			for _, e := range client.events() {
				got = append(got, e.event)
			}
			if len(got) != len(tc.want) {
				t.Fatalf("emitted %v, want %v", got, tc.want)
			}
			for i, ev := range tc.want {
				if got[i] != ev {
					t.Fatalf("emitted %v, want %v", got, tc.want)
				}
			}

			// A replay is addressed to one client. Broadcasting it would make
			// every other surface redraw a plan it did not ask for.
			if _, ok := hasEvent(mu, broadcasts, "pushIngestPreview"); ok {
				t.Fatal("replay must not broadcast")
			}
			// Status matters more, now that it is unconditional: broadcast, it
			// would fan every single connect out to every client, each one
			// paying a directory read for a snapshot nobody asked for.
			if _, ok := hasEvent(mu, broadcasts, "pushIngestStatus"); ok {
				t.Fatal("hydration must not broadcast status")
			}
			// Refusals are silent: this is an unsolicited push, so an error
			// banner on connect would be pure noise.
			if _, ok := client.find("pushIngestError"); ok {
				t.Fatal("replay must never emit pushIngestError")
			}
		})
	}
}

func TestIngestPushTo_ReplaysTheRetainedPlan(t *testing.T) {
	t.Parallel()

	plan := ingest.Report{
		Token:   "plan-token",
		Summary: ingest.Summary{WouldIngest: 3},
		Items:   []ingest.Item{},
	}
	svc := &fakeIngest{
		status:     ingest.Status{Available: true, Count: 3},
		pending:    plan,
		hasPending: true,
	}
	h, _, _ := newHandlers(t, svc)
	client := &recorder{}

	h.pushTo(client, "127.0.0.1")

	ev, ok := client.find("pushIngestPreview")
	if !ok {
		t.Fatal("expected the retained plan to be replayed")
	}
	// The token is what arms the client's Import button; a replay that dropped
	// it would leave the user looking at a plan they cannot confirm.
	got := ev.payload.(ingest.Report)
	if got.Token != plan.Token {
		t.Fatalf("replayed token = %q, want %q", got.Token, plan.Token)
	}
	if got.Summary.WouldIngest != plan.Summary.WouldIngest {
		t.Fatalf("replayed WouldIngest = %d, want %d",
			got.Summary.WouldIngest, plan.Summary.WouldIngest)
	}
}

func TestIngestPushTo_NilSafe(t *testing.T) {
	t.Parallel()
	// The connect-time batch calls this unconditionally; a host without an
	// ingest service must not panic every client that connects.
	var h *IngestHandlers
	h.PushTo(nil)
}

// --- busy broadcasting and emit ordering -----------------------------------

// statusesFrom pulls the Busy flag out of every pushIngestStatus in order.
// A failure that never reached the run still has to leave the requester's
// listing correct.
//
// ErrStalePlan is the case with teeth: it means the inbox moved under the
// plan, so the client's cached count is wrong by construction. The old code
// refreshed it with a `defer broadcastStatus()` on every return path; removing
// that (it was reporting idle twice and was the reason busy never reached the
// wire) took this with it. ErrBusy is deliberately excluded — the run that
// refused this one is already broadcasting both of its own edges.
func TestIngest_PreRunFailuresStillRefreshTheRequestersStatus(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name        string
		err         error
		wantRefresh bool
	}{
		{"stale plan", ingest.ErrStalePlan, true},
		{"no plan", ingest.ErrNoPlan, true},
		{"unavailable", ingest.ErrUnavailable, true},
		{"busy", ingest.ErrBusy, false},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			svc := &fakeIngest{
				status:    ingest.Status{Available: true, Items: []string{"Album A"}, Count: 1},
				commitErr: tc.err,
			}
			h, broadcasts, bmu := newHandlers(t, svc)
			client := &recorder{}

			h.handleCommit(client, "127.0.0.1", "spent-token")

			if _, ok := client.find("pushIngestError"); !ok {
				t.Fatal("the requester must still be told what failed")
			}
			st, ok := client.find("pushIngestStatus")
			if ok != tc.wantRefresh {
				t.Fatalf("status refresh to requester = %v, want %v", ok, tc.wantRefresh)
			}
			if tc.wantRefresh {
				if got := st.payload.(ingest.Status); got.Count != 1 || got.Busy {
					t.Fatalf("refreshed status = %+v, want the live listing and not busy", got)
				}
				// It is the requester's problem, not everyone's: a commit that
				// really changed the inbox broadcasts from the run's own
				// trailing edge.
				if busy := statusesFrom(bmu, broadcasts); len(busy) != 0 {
					t.Fatalf("broadcast %v, want nothing — no run started", busy)
				}
			}
		})
	}
}

func statusesFrom(mu *sync.Mutex, events *[]emitted) []bool {
	mu.Lock()
	defer mu.Unlock()
	var busy []bool
	for _, e := range *events {
		if e.event != "pushIngestStatus" {
			continue
		}
		st, ok := e.payload.(ingest.Status)
		if !ok {
			continue
		}
		busy = append(busy, st.Busy)
	}
	return busy
}

func TestIngest_RunEdgesBroadcastBusyBothWays(t *testing.T) {
	t.Parallel()

	// Without this, `busy` is dead on the wire: the handler's own broadcasts
	// were taken before the run flipped the flag and after it had cleared it,
	// so every surface that did not start the run thought the system was idle
	// and offered an Import button that could only fail with ErrBusy.
	for _, tc := range []struct {
		name string
		run  func(h *IngestHandlers, client *recorder)
	}{
		{
			name: "preview",
			run: func(h *IngestHandlers, client *recorder) {
				h.handlePreview(client, "127.0.0.1")
			},
		},
		{
			name: "commit",
			run: func(h *IngestHandlers, client *recorder) {
				h.handleCommit(client, "127.0.0.1", "plan-token")
			},
		},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			svc := &fakeIngest{status: ingest.Status{Available: true, Items: []string{}}}
			h, broadcasts, mu := newHandlers(t, svc)

			tc.run(h, &recorder{})

			if got := statusesFrom(mu, broadcasts); len(got) != 2 || got[0] != true || got[1] != false {
				t.Fatalf("broadcast busy sequence = %v, want [true false]", got)
			}
		})
	}
}

func TestIngest_RefusedRunBroadcastsNothing(t *testing.T) {
	t.Parallel()

	// A second run is refused before single-flight touches `running`. Emitting
	// a trailing edge for it would clear a busy flag the first run still owns.
	svc := &fakeIngest{
		status:     ingest.Status{Available: true, Items: []string{}},
		previewErr: ingest.ErrBusy,
	}
	h, broadcasts, mu := newHandlers(t, svc)

	h.handlePreview(&recorder{}, "127.0.0.1")

	if got := statusesFrom(mu, broadcasts); len(got) != 0 {
		t.Fatalf("refused run broadcast %v, want nothing", got)
	}
}

// orderedEmitter records into a shared log so a test can assert the global
// order of snapshots and sends across goroutines.
type orderedEmitter struct {
	record func(string)
}

func (o *orderedEmitter) Emit(ev string, args ...any) error {
	label := "emit:" + ev
	if len(args) > 0 {
		if st, ok := args[0].(ingest.Status); ok {
			label = fmt.Sprintf("emit:%s(busy=%v)", ev, st.Busy)
		}
	}
	o.record(label)
	return nil
}

// A status push reports state the backend read moments earlier, and two
// goroutines produce them: the connect-time hydration, and the broadcast on a
// run's trailing edge. Left unsynchronised they invert — hydration samples
// busy=true, the run ends and broadcasts busy=false, and hydration's older
// frame lands last. The client is then soft-locked on a run that finished,
// with nothing left to correct it until the next reconnect: precisely the
// class of bug this whole area exists to fix.
//
// Serializing the sends is not enough on its own; the state each one reports
// has to be *read* inside the same critical section, which is what this pins.
func TestIngest_StatusSendsAreOrderedWithTheirSnapshots(t *testing.T) {
	var logMu sync.Mutex
	var events []string
	record := func(s string) {
		logMu.Lock()
		events = append(events, s)
		logMu.Unlock()
	}

	var calls atomic.Int32
	held := make(chan struct{})
	release := make(chan struct{})

	svc := &fakeIngest{status: ingest.Status{Available: true, Items: []string{}}}
	svc.statusFn = func() ingest.Status {
		n := calls.Add(1)
		record(fmt.Sprintf("snapshot%d", n))
		if n == 1 {
			// The hydration snapshot: taken while a run is still in flight,
			// and held open long enough for that run to finish underneath it.
			close(held)
			<-release
			return ingest.Status{Available: true, Busy: true}
		}
		return ingest.Status{Available: true, Busy: false}
	}

	h, _, _ := newHandlers(t, svc)
	h.broadcast = func(ev string, payload any) {
		if st, ok := payload.(ingest.Status); ok {
			record(fmt.Sprintf("broadcast:%s(busy=%v)", ev, st.Busy))
			return
		}
		record("broadcast:" + ev)
	}

	hydrated := make(chan struct{})
	go func() {
		defer close(hydrated)
		h.pushTo(&orderedEmitter{record: record}, "127.0.0.1")
	}()
	<-held

	broadcast := make(chan struct{})
	go func() {
		defer close(broadcast)
		h.broadcastStatus()
	}()
	// Give the competing broadcast every opportunity to overtake: without the
	// shared critical section it snapshots and sends immediately, inverting
	// the pair.
	time.Sleep(50 * time.Millisecond)
	close(release)

	<-hydrated
	<-broadcast

	logMu.Lock()
	got := append([]string(nil), events...)
	logMu.Unlock()

	want := []string{
		"snapshot1",
		"emit:pushIngestStatus(busy=true)",
		"snapshot2",
		"broadcast:pushIngestStatus(busy=false)",
	}
	if len(got) != len(want) {
		t.Fatalf("event log = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("event log = %v, want %v", got, want)
		}
	}
}

// --- token parsing ---------------------------------------------------------

func TestIngestToken(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		args []any
		want string
	}{
		{"no args", nil, ""},
		{"bare string", []any{"abc"}, "abc"},
		{"object form", []any{map[string]interface{}{"token": "abc"}}, "abc"},
		{"object without token key", []any{map[string]interface{}{"plan": "abc"}}, ""},
		{"wrong type", []any{42}, ""},
		{"nil arg", []any{nil}, ""},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := ingestToken(tc.args...); got != tc.want {
				t.Fatalf("ingestToken(%v) = %q, want %q", tc.args, got, tc.want)
			}
		})
	}
}

func TestNewIngestHandlers_InvalidTrustedSpec(t *testing.T) {
	t.Parallel()
	if _, err := NewIngestHandlers(&fakeIngest{}, nil, []string{"not-a-cidr"}); err == nil {
		t.Fatal("expected a hard error on a malformed allowlist spec, got nil")
	}
}
