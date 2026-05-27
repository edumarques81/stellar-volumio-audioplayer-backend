package airplay

import (
	"testing"
	"time"
)

func TestSessionUpdateMarksActive(t *testing.T) {
	s := NewSession(SessionConfig{HeartbeatTimeout: time.Second})
	if s.Snapshot().IsActive {
		t.Fatalf("new session should not be active")
	}

	s.Update(Frame{Title: "One", Artist: "Daft", Album: "Discovery", Sender: "iPhone"})
	snap := s.Snapshot()
	if !snap.IsActive {
		t.Errorf("after Update, IsActive should be true")
	}
	if !snap.IsPlaying {
		t.Errorf("first Update without explicit PlayState should default to IsPlaying=true")
	}
	if snap.Title != "One" || snap.Sender != "iPhone" {
		t.Errorf("snapshot fields wrong: %+v", snap)
	}
	if snap.SessionID == "" {
		t.Errorf("SessionID should be auto-assigned on first Update")
	}
}

func TestSessionPlayStateTransitions(t *testing.T) {
	s := NewSession(SessionConfig{HeartbeatTimeout: time.Second})
	s.Update(Frame{Title: "A", PlayState: PlayStatePlaying})
	if !s.Snapshot().IsPlaying {
		t.Errorf("pbeg should mark IsPlaying=true")
	}

	s.Update(Frame{PlayState: PlayStatePaused})
	if s.Snapshot().IsPlaying {
		t.Errorf("paus should mark IsPlaying=false")
	}

	s.Update(Frame{PlayState: PlayStatePlaying})
	if !s.Snapshot().IsPlaying {
		t.Errorf("prsm should restore IsPlaying=true")
	}

	// PlayStateUnknown is a no-op — title updates without a lifecycle
	// chunk must preserve the existing IsPlaying value.
	s.Update(Frame{PlayState: PlayStatePaused})
	s.Update(Frame{Title: "Other"})
	if s.Snapshot().IsPlaying {
		t.Errorf("unrelated update must preserve IsPlaying=false")
	}
}

func TestSessionUpdatePreservesUnchangedFields(t *testing.T) {
	s := NewSession(SessionConfig{HeartbeatTimeout: time.Second})
	s.Update(Frame{Title: "A", Artist: "B", Album: "C"})
	s.Update(Frame{Title: "A2"}) // no artist / album → should preserve previous

	snap := s.Snapshot()
	if snap.Title != "A2" {
		t.Errorf("Title should be updated: got %q", snap.Title)
	}
	if snap.Artist != "B" {
		t.Errorf("Artist should be preserved: got %q", snap.Artist)
	}
	if snap.Album != "C" {
		t.Errorf("Album should be preserved: got %q", snap.Album)
	}
}

func TestSessionUpdateRecordsRemoteToken(t *testing.T) {
	s := NewSession(SessionConfig{HeartbeatTimeout: time.Second})
	s.Update(Frame{ActiveRemote: "3823061215", DACPID: "63B5C32B3D40C25B"})
	if s.ActiveRemote() != "3823061215" {
		t.Errorf("ActiveRemote = %q", s.ActiveRemote())
	}
	if s.DACPID() != "63B5C32B3D40C25B" {
		t.Errorf("DACPID = %q", s.DACPID())
	}
	if !s.CanControl() {
		t.Errorf("CanControl should be true once ActiveRemote+DACPID set")
	}
}

func TestSessionEndClears(t *testing.T) {
	s := NewSession(SessionConfig{HeartbeatTimeout: time.Second})
	s.Update(Frame{Title: "A", Artist: "B", ActiveRemote: "tok", DACPID: "did"})
	sid := s.Snapshot().SessionID

	endedID, ok := s.End()
	if !ok {
		t.Fatalf("End() should return ok when session was active")
	}
	if endedID != sid {
		t.Errorf("ended SessionID = %q, want %q", endedID, sid)
	}
	snap := s.Snapshot()
	if snap.IsActive {
		t.Errorf("after End, IsActive should be false")
	}
	if snap.Title != "" || snap.ActiveRemote != "" {
		t.Errorf("End should clear all fields: %+v", snap)
	}
}

func TestSessionPbegStartsFreshID(t *testing.T) {
	s := NewSession(SessionConfig{HeartbeatTimeout: time.Second})
	s.Update(Frame{Title: "A"})
	first := s.Snapshot().SessionID

	// Simulate the sender ending and a new session starting.
	s.Update(Frame{SessionEnded: true})
	if s.Snapshot().IsActive {
		t.Errorf("pend should end the session")
	}

	s.Update(Frame{SessionBegan: true, Title: "B"})
	second := s.Snapshot().SessionID
	if second == first || second == "" {
		t.Errorf("new pbeg should mint a new SessionID; got %q (prev %q)", second, first)
	}
}

// TestSessionPbegMidSessionKeepsSameID — shairport emits `pbeg` at every
// track boundary inside a single AirPlay session. Earlier behaviour
// minted a new SessionID on every pbeg, which made the iOS app's
// sessionID-matched `pushAirplayEnded` filter race the next track's
// pushAirplayState and the UI would flap blank between tracks. After
// the fix, only the FIRST update of a session mints a SessionID; later
// pbegs (during the same session) keep the same ID and just flip
// IsPlaying back to true.
func TestSessionPbegMidSessionKeepsSameID(t *testing.T) {
	s := NewSession(SessionConfig{HeartbeatTimeout: time.Second})

	// Initial pbeg → mints session ID.
	s.Update(Frame{SessionBegan: true, Title: "Track 1"})
	first := s.Snapshot().SessionID
	if first == "" {
		t.Fatal("first pbeg should mint a SessionID")
	}

	// Mid-session pause then resume — IsPlaying flips.
	s.Update(Frame{PlayState: PlayStatePaused})
	if s.Snapshot().IsPlaying {
		t.Errorf("PlayStatePaused should clear IsPlaying")
	}

	// Next track's pbeg arrives. Session is still active → keep same ID.
	s.Update(Frame{SessionBegan: true, Title: "Track 2"})
	second := s.Snapshot().SessionID
	if second != first {
		t.Errorf("pbeg during active session should NOT mint a new SessionID; got %q (was %q)", second, first)
	}
	if !s.Snapshot().IsPlaying {
		t.Errorf("pbeg should flip IsPlaying back to true")
	}
	if s.Snapshot().Title != "Track 2" {
		t.Errorf("title should have updated to Track 2; got %q", s.Snapshot().Title)
	}
}

// TestSessionPbegMidSessionResetsSeek — track-change pbeg inside an active
// session must reset SeekSeconds to 0. Without this, the Mac's snap retains
// the previous track's last-known seek position because the delta-merge
// inside Update() treats SeekSeconds==0 as "field absent" and ignores it.
// The Pi daemon's prgr handler emits seekSeconds:0 at every track start
// (where shairport's RTP `cur == start`), so without an explicit reset the
// snap carries stale data until the next mid-track prgr arrives 1-3s later.
// An iOS app opening during that window renders the previous track's
// elapsed time as the current; the 1Hz local ticker then advances from
// the wrong base. See 2026-05-28 Phase H investigation for the live capture.
func TestSessionPbegMidSessionResetsSeek(t *testing.T) {
	s := NewSession(SessionConfig{HeartbeatTimeout: time.Second})

	// Track 1 starts and accumulates a mid-song seek position.
	s.Update(Frame{SessionBegan: true, Title: "Track 1"})
	s.Update(Frame{SeekSeconds: 90, DurationSeconds: 240})
	if got := s.Snapshot().SeekSeconds; got != 90 {
		t.Fatalf("setup: SeekSeconds=%d, want 90", got)
	}

	// Track 2's pbeg arrives during the active session. SeekSeconds must
	// reset to 0 even though the pbeg frame carries no seek field.
	s.Update(Frame{SessionBegan: true, Title: "Track 2"})
	if got := s.Snapshot().SeekSeconds; got != 0 {
		t.Errorf("after mid-session pbeg, SeekSeconds = %d, want 0 (stale-seek regression)", got)
	}
	// Belt-and-braces: title also updated, so the test isn't passing because
	// the whole Update path no-op'd.
	if got := s.Snapshot().Title; got != "Track 2" {
		t.Errorf("title = %q, want Track 2", got)
	}
}

// TestSessionPauseResumePreservesSeek — pause/resume during the same
// track must NOT reset SeekSeconds. Only pbeg-on-active-session triggers
// the reset; lifecycle-only frames (paus/prsm) leave the position alone.
func TestSessionPauseResumePreservesSeek(t *testing.T) {
	s := NewSession(SessionConfig{HeartbeatTimeout: time.Second})
	s.Update(Frame{SessionBegan: true, Title: "T1"})
	s.Update(Frame{SeekSeconds: 75})

	s.Update(Frame{PlayState: PlayStatePaused})
	if got := s.Snapshot().SeekSeconds; got != 75 {
		t.Errorf("paus must preserve seek: got %d", got)
	}

	s.Update(Frame{PlayState: PlayStatePlaying})
	if got := s.Snapshot().SeekSeconds; got != 75 {
		t.Errorf("prsm must preserve seek: got %d", got)
	}
}

// TestSessionSnapshotAdvancesSeekWhilePlaying — shairport-sync's `prgr`
// (progress) chunks are sparse: sometimes only one fires near track start,
// then nothing for the rest of the track. Without server-side wall-clock
// advance, the snap's SeekSeconds stays frozen at the last received value,
// so a client that re-rehydrates mid-track (iOS foreground refresh,
// LCD page reload) sees the stale value and jumps BACKWARDS. The
// 2026-05-28 capture caught it cleanly: iOS interpolator at 1:31 →
// foreground refresh → server snap had SeekSeconds=3 (last prgr) →
// iOS rendered 0:03. Fix: Snapshot() returns SeekSeconds + (now -
// lastSeekUpdateAt) while IsPlaying.
func TestSessionSnapshotAdvancesSeekWhilePlaying(t *testing.T) {
	base := time.Now()
	clk := base
	s := NewSession(SessionConfig{HeartbeatTimeout: time.Minute, now: func() time.Time { return clk }})

	s.Update(Frame{SessionBegan: true, Title: "T1"})
	s.Update(Frame{SeekSeconds: 10, DurationSeconds: 240})
	if got := s.Snapshot().SeekSeconds; got != 10 {
		t.Fatalf("immediate snap = %d, want 10", got)
	}

	// 5 seconds pass without any further Update. Snapshot must reflect
	// the wall-clock advance.
	clk = base.Add(5 * time.Second)
	if got := s.Snapshot().SeekSeconds; got != 15 {
		t.Errorf("after 5s, snap.SeekSeconds = %d, want 15 (wall-clock advance)", got)
	}

	// Another 7 seconds (total 12s since the SeekSeconds=10 update).
	clk = base.Add(12 * time.Second)
	if got := s.Snapshot().SeekSeconds; got != 22 {
		t.Errorf("after 12s, snap.SeekSeconds = %d, want 22", got)
	}
}

// TestSessionSnapshotDoesNotAdvanceWhenPaused — wall-clock advance must
// be gated on IsPlaying. A paused AirPlay session (user paused via Apple
// Music) must freeze the elapsed counter even though no Update arrives.
func TestSessionSnapshotDoesNotAdvanceWhenPaused(t *testing.T) {
	base := time.Now()
	clk := base
	s := NewSession(SessionConfig{HeartbeatTimeout: time.Minute, now: func() time.Time { return clk }})

	s.Update(Frame{SessionBegan: true, Title: "T1"})
	s.Update(Frame{SeekSeconds: 30})
	s.Update(Frame{PlayState: PlayStatePaused})

	clk = base.Add(10 * time.Second)
	if got := s.Snapshot().SeekSeconds; got != 30 {
		t.Errorf("paused snap must NOT advance: got %d, want 30", got)
	}

	// Resume — anchor advances from the resume moment, not from the
	// original prgr. Otherwise the elapsed would jump forward by the
	// duration of the pause.
	s.Update(Frame{PlayState: PlayStatePlaying})
	clk = base.Add(15 * time.Second)
	if got := s.Snapshot().SeekSeconds; got != 35 {
		t.Errorf("resumed snap must advance from resume moment: got %d, want 35 (30 + 5s post-resume, not 30 + 15s wall-clock)", got)
	}
}

// TestSessionSnapshotPbegResetsAnchor — when SeekSeconds resets to 0 on
// mid-session pbeg, the wall-clock anchor must also reset. Otherwise the
// reset is paired with an immediate "advance" from the stale anchor and
// the elapsed counter jumps forward by the duration of the previous track.
func TestSessionSnapshotPbegResetsAnchor(t *testing.T) {
	base := time.Now()
	clk := base
	s := NewSession(SessionConfig{HeartbeatTimeout: time.Minute, now: func() time.Time { return clk }})

	s.Update(Frame{SessionBegan: true, Title: "T1"})
	s.Update(Frame{SeekSeconds: 100})

	// 60 seconds pass — snap shows 160.
	clk = base.Add(60 * time.Second)
	if got := s.Snapshot().SeekSeconds; got != 160 {
		t.Fatalf("setup: snap = %d, want 160", got)
	}

	// Track change. SeekSeconds resets to 0; the anchor must also
	// reset, otherwise the very next snap returns 60+0=60.
	s.Update(Frame{SessionBegan: true, Title: "T2"})
	if got := s.Snapshot().SeekSeconds; got != 0 {
		t.Errorf("immediately after track-change pbeg, snap = %d, want 0", got)
	}

	// 3 more seconds — fresh anchor.
	clk = base.Add(63 * time.Second)
	if got := s.Snapshot().SeekSeconds; got != 3 {
		t.Errorf("3s after track change, snap = %d, want 3", got)
	}
}

func TestSessionHeartbeatExpiresAfterTimeout(t *testing.T) {
	s := NewSession(SessionConfig{HeartbeatTimeout: 50 * time.Millisecond, now: nowFn(time.Time{})})

	base := time.Now()
	s.nowFn = nowFn(base)
	s.Update(Frame{Title: "A"})

	// Just under timeout — still active.
	s.nowFn = nowFn(base.Add(40 * time.Millisecond))
	if !s.Snapshot().IsActive {
		t.Errorf("session should remain active before timeout")
	}
	// Beyond timeout — Tick should auto-end.
	s.nowFn = nowFn(base.Add(100 * time.Millisecond))
	ended, id := s.TickExpire()
	if !ended || id == "" {
		t.Fatalf("TickExpire should report ended=true with the ended ID")
	}
	if s.Snapshot().IsActive {
		t.Errorf("session should be inactive after TickExpire")
	}
}

func TestSessionHeartbeatBumpedByUpdate(t *testing.T) {
	s := NewSession(SessionConfig{HeartbeatTimeout: 50 * time.Millisecond})

	base := time.Now()
	s.nowFn = nowFn(base)
	s.Update(Frame{Title: "A"})

	// At 40ms, before timeout, a heartbeat resets the deadline.
	s.nowFn = nowFn(base.Add(40 * time.Millisecond))
	s.Heartbeat()

	// At 80ms (would-be-expired without heartbeat) we are within the
	// post-heartbeat window (40 + 50 = 90).
	s.nowFn = nowFn(base.Add(80 * time.Millisecond))
	ended, _ := s.TickExpire()
	if ended {
		t.Errorf("heartbeat should keep session alive within new window")
	}
}

// TestSessionEndCooldownDropsLateFrames pins the orphan-session bug fix:
// after End() (typically called by the shairport post-hook curl), any
// straggler ingest POSTs from the daemon — which are racing the post-hook
// at the Mac's HTTP server — must NOT resurrect the just-ended session.
// Without this, a metadata frame arriving 50ms after the {ended:true}
// POST would mint a fresh SessionID with isActive=true, and the daemon's
// unconditional 2s heartbeats would then refresh lastHB forever, leaving
// an undying orphan that pins the iOS / LCD UI on AirPlay.
func TestSessionEndCooldownDropsLateFrames(t *testing.T) {
	base := time.Now()
	clock := nowFn(base)
	s := NewSession(SessionConfig{
		HeartbeatTimeout: time.Second,
		PostEndCooldown:  500 * time.Millisecond,
		now:              clock,
	})
	s.nowFn = clock

	// Active session with a pbeg.
	s.Update(Frame{SessionBegan: true, Title: "A"})
	if !s.Snapshot().IsActive {
		t.Fatalf("session should be active after pbeg")
	}

	// Post-hook curl arrives: explicit End().
	endedID, ok := s.End()
	if !ok || endedID == "" {
		t.Fatalf("End() should report the just-ended session")
	}

	// 50ms later, a straggler metadata frame (no SessionBegan) arrives —
	// e.g. the cover-art PICT that was in flight when post-hook fired.
	s.nowFn = nowFn(base.Add(50 * time.Millisecond))
	s.Update(Frame{CoverDataURL: "data:image/jpeg;base64,..."})
	if s.Snapshot().IsActive {
		t.Errorf("straggler frame within cooldown must NOT resurrect session; snap=%+v", s.Snapshot())
	}
}

// TestSessionEndCooldownAllowsSessionBegan ensures the cooldown only
// drops non-SessionBegan frames. A real pbeg arriving inside the
// cooldown window (rare but possible if the sender reconnects quickly)
// must still mint a fresh session.
func TestSessionEndCooldownAllowsSessionBegan(t *testing.T) {
	base := time.Now()
	s := NewSession(SessionConfig{
		HeartbeatTimeout: time.Second,
		PostEndCooldown:  500 * time.Millisecond,
		now:              nowFn(base),
	})
	s.nowFn = nowFn(base)

	s.Update(Frame{SessionBegan: true, Title: "First"})
	first := s.Snapshot().SessionID
	if _, ok := s.End(); !ok {
		t.Fatalf("End() should succeed")
	}

	// 100ms into cooldown, a new sender connects and shairport emits pbeg.
	s.nowFn = nowFn(base.Add(100 * time.Millisecond))
	s.Update(Frame{SessionBegan: true, Title: "Second"})

	snap := s.Snapshot()
	if !snap.IsActive {
		t.Errorf("pbeg inside cooldown should reactivate; snap=%+v", snap)
	}
	if snap.SessionID == "" || snap.SessionID == first {
		t.Errorf("new pbeg should mint a fresh SessionID; got %q (was %q)", snap.SessionID, first)
	}
	if snap.Title != "Second" {
		t.Errorf("title should update; got %q", snap.Title)
	}
}

// TestSessionEndCooldownExpires confirms that after the cooldown window
// elapses, normal "metadata starts a session" behaviour is restored.
// We keep this path because a fresh daemon process may legitimately
// emit acre/daid/snam before the first pbeg (shairport's ordering
// during RTSP setup), and we shouldn't permanently lose those frames.
func TestSessionEndCooldownExpires(t *testing.T) {
	base := time.Now()
	s := NewSession(SessionConfig{
		HeartbeatTimeout: time.Second,
		PostEndCooldown:  500 * time.Millisecond,
		now:              nowFn(base),
	})
	s.nowFn = nowFn(base)

	s.Update(Frame{SessionBegan: true, Title: "X"})
	if _, ok := s.End(); !ok {
		t.Fatalf("End() should succeed")
	}

	// 1 second later — well past cooldown.
	s.nowFn = nowFn(base.Add(time.Second))
	s.Update(Frame{Title: "Y", ActiveRemote: "abc"})
	if !s.Snapshot().IsActive {
		t.Errorf("metadata after cooldown should mint a fresh session; snap=%+v", s.Snapshot())
	}
}

// TestSessionEndCooldownZeroDisablesCooldown ensures the feature is opt-in
// — leaving PostEndCooldown at zero preserves the old behaviour (any
// frame resurrects). Tests in other packages that construct sessions
// without setting PostEndCooldown stay green.
func TestSessionEndCooldownZeroDisablesCooldown(t *testing.T) {
	s := NewSession(SessionConfig{HeartbeatTimeout: time.Second})
	s.Update(Frame{SessionBegan: true, Title: "A"})
	if _, ok := s.End(); !ok {
		t.Fatalf("End() should succeed")
	}
	// No cooldown configured → straggler frame is allowed through.
	s.Update(Frame{Title: "B"})
	if !s.Snapshot().IsActive {
		t.Errorf("with cooldown=0, metadata after End should still mint (legacy behaviour)")
	}
}

func nowFn(t time.Time) func() time.Time {
	return func() time.Time { return t }
}
