// Package airplay holds the Mac-side domain model for an active AirPlay
// session. The Session struct is the single source of truth that the
// Socket.IO broadcaster consults when emitting pushAirplayState /
// pushAirplayEnded, and the DACPClient is what the airplay:command
// socket handler uses to proxy play/pause/next/prev back to the iPhone
// over Bonjour-discovered DACP HTTP.
//
// No I/O happens in this package — DACPClient takes an injectable
// *http.Client, and the bonjour resolver is fed pre-captured CLI output
// for unit testing. cmd/stellar wires the production *exec.Cmd path.
package airplay

import (
	"crypto/rand"
	"encoding/hex"
	"sync"
	"time"
)

// Frame is the input shape the parser package's BuildFrame returns. We
// re-declare a structurally compatible copy here to avoid a leaky
// import — the session package never needs the lower-level Chunk type.
type Frame struct {
	Title           string
	Artist          string
	Album           string
	Sender          string
	CoverDataURL    string
	ActiveRemote    string
	DACPID          string
	SeekSeconds     int
	DurationSeconds int
	SampleRate      int
	BitDepth        int

	SessionBegan bool
	SessionEnded bool
	Paused       bool
	Resumed      bool
}

// Snapshot is the broadcast-ready view of the current session. The
// transport layer marshals this struct directly into the
// pushAirplayState event payload.
type Snapshot struct {
	IsActive        bool   `json:"isActive"`
	Title           string `json:"title"`
	Artist          string `json:"artist"`
	Album           string `json:"album"`
	Sender          string `json:"sender"`
	CoverDataURL    string `json:"coverDataURL"`
	SeekSeconds     int    `json:"seekSeconds"`
	DurationSeconds int    `json:"durationSeconds"`
	CanControl      bool   `json:"canControl"`
	SessionID       string `json:"sessionID"`
	SampleRate      int    `json:"sampleRate"`
	BitDepth        int    `json:"bitDepth"`
	// Internal-only fields (omitted by the Socket.IO emitter via a
	// payload shaper).
	ActiveRemote string `json:"-"`
	DACPID       string `json:"-"`
}

// SessionConfig is the construction-time configuration for Session.
type SessionConfig struct {
	// HeartbeatTimeout is the duration after the last Update/Heartbeat
	// after which TickExpire will end the session. 5s is the contract
	// default; tests inject shorter values.
	HeartbeatTimeout time.Duration

	// now is a clock override used by tests. nil → time.Now.
	now func() time.Time
}

// Session is the concurrent-safe in-memory state of the current (or
// most-recently-active) AirPlay session.
type Session struct {
	mu      sync.RWMutex
	cfg     SessionConfig
	snap    Snapshot
	lastHB  time.Time
	nowFn   func() time.Time
}

// NewSession returns a Session with HeartbeatTimeout enforced by
// callers via TickExpire. A zero HeartbeatTimeout disables auto-expire
// (caller drives end-of-session via Update(Frame{SessionEnded:true})
// or the explicit End method).
func NewSession(cfg SessionConfig) *Session {
	now := cfg.now
	if now == nil {
		now = time.Now
	}
	if cfg.HeartbeatTimeout < 0 {
		cfg.HeartbeatTimeout = 0
	}
	return &Session{
		cfg:    cfg,
		nowFn:  now,
		lastHB: now(),
	}
}

// Update applies a Frame to the current session state. Empty fields in
// the Frame leave the prior value intact (delta-merge semantics).
// SessionBegan mints a fresh SessionID; SessionEnded triggers an
// internal End().
func (s *Session) Update(f Frame) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.lastHB = s.nowFn()

	if f.SessionEnded {
		s.endLocked()
		return
	}

	if f.SessionBegan || !s.snap.IsActive {
		s.snap.SessionID = newSessionID()
		s.snap.IsActive = true
	}

	// Delta-merge: only non-zero fields overwrite.
	if f.Title != "" {
		s.snap.Title = f.Title
	}
	if f.Artist != "" {
		s.snap.Artist = f.Artist
	}
	if f.Album != "" {
		s.snap.Album = f.Album
	}
	if f.Sender != "" {
		s.snap.Sender = f.Sender
	}
	if f.CoverDataURL != "" {
		s.snap.CoverDataURL = f.CoverDataURL
	}
	if f.ActiveRemote != "" {
		s.snap.ActiveRemote = f.ActiveRemote
	}
	if f.DACPID != "" {
		s.snap.DACPID = f.DACPID
	}
	if f.SeekSeconds != 0 {
		s.snap.SeekSeconds = f.SeekSeconds
	}
	if f.DurationSeconds != 0 {
		s.snap.DurationSeconds = f.DurationSeconds
	}
	if f.SampleRate != 0 {
		s.snap.SampleRate = f.SampleRate
	}
	if f.BitDepth != 0 {
		s.snap.BitDepth = f.BitDepth
	}

	s.snap.CanControl = s.snap.ActiveRemote != "" && s.snap.DACPID != ""
}

// Heartbeat refreshes the deadline without otherwise touching state.
// Called by the HTTP /internal/airplay/heartbeat handler.
func (s *Session) Heartbeat() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lastHB = s.nowFn()
}

// Snapshot returns a copy of the current state safe to marshal.
func (s *Session) Snapshot() Snapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.snap
}

// CanControl reports whether the session has enough information
// (Active-Remote + DACP-ID) to proxy a DACP command.
func (s *Session) CanControl() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.snap.CanControl
}

// ActiveRemote returns the Active-Remote token (DACP auth header
// value) for the current session, or "" if unknown.
func (s *Session) ActiveRemote() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.snap.ActiveRemote
}

// DACPID returns the DACP-ID (Bonjour iTunes_Ctrl_<id> instance name)
// for the current session, or "" if unknown.
func (s *Session) DACPID() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.snap.DACPID
}

// End force-clears the session. Returns the just-ended SessionID and
// ok=true if the session was active prior to the call, ok=false if it
// was already inactive (no-op).
func (s *Session) End() (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.snap.IsActive {
		return "", false
	}
	id := s.snap.SessionID
	s.endLocked()
	return id, true
}

// TickExpire returns (ended=true, sessionID) if the heartbeat deadline
// has passed since the last Update or Heartbeat. Idempotent — repeated
// calls after expiry return (false, ""). When HeartbeatTimeout is zero,
// always returns (false, "").
func (s *Session) TickExpire() (bool, string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.snap.IsActive || s.cfg.HeartbeatTimeout == 0 {
		return false, ""
	}
	if s.nowFn().Sub(s.lastHB) <= s.cfg.HeartbeatTimeout {
		return false, ""
	}
	id := s.snap.SessionID
	s.endLocked()
	return true, id
}

func (s *Session) endLocked() {
	s.snap = Snapshot{}
}

// newSessionID returns a short, opaque 16-hex-char identifier. We use
// crypto/rand for collision resistance across daemon restarts; failure
// to read random bytes returns a fixed sentinel rather than panicking
// (extremely unlikely on the Mac).
func newSessionID() string {
	var buf [8]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "0000000000000000"
	}
	return hex.EncodeToString(buf[:])
}
