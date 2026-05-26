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

func nowFn(t time.Time) func() time.Time {
	return func() time.Time { return t }
}
