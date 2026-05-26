package socketio

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/edumarques81/stellar-volumio-audioplayer-backend/internal/domain/airplay"
)

// testDACPServer is an in-process stand-in for the iPhone's DACP HTTP
// endpoint. It accepts any /ctrl-int/1/{cmd} request that carries an
// Active-Remote header, records the call count, and returns 204.
type testDACPServer struct {
	*httptest.Server
	calls atomicCounter
}

func newTestDACPServer(t *testing.T) *testDACPServer {
	t.Helper()
	s := &testDACPServer{}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/ctrl-int/1/") {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		if r.Header.Get("Active-Remote") == "" {
			http.Error(w, "no auth", http.StatusUnauthorized)
			return
		}
		s.calls.add(1)
		w.WriteHeader(http.StatusNoContent)
	}))
	return s
}

// fakeDACP scripts every command's outcome and records the receiving
// (baseURL, token, command) tuple. The shape mirrors the production
// airplay.DACPClient interface.
type fakeDACP struct {
	mu       sync.Mutex
	commands []fakeDACPCall
	err      error
}

type fakeDACPCall struct {
	cmd      string
	baseURL  string
	token    string
}

func (f *fakeDACP) Play(_ context.Context, baseURL, tok string) error { return f.record("play", baseURL, tok) }
func (f *fakeDACP) Pause(_ context.Context, baseURL, tok string) error {
	return f.record("pause", baseURL, tok)
}
func (f *fakeDACP) PlayPause(_ context.Context, baseURL, tok string) error {
	return f.record("playpause", baseURL, tok)
}
func (f *fakeDACP) Next(_ context.Context, baseURL, tok string) error { return f.record("next", baseURL, tok) }
func (f *fakeDACP) Prev(_ context.Context, baseURL, tok string) error { return f.record("prev", baseURL, tok) }

func (f *fakeDACP) record(cmd, baseURL, tok string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.commands = append(f.commands, fakeDACPCall{cmd: cmd, baseURL: baseURL, token: tok})
	return f.err
}

// fakeResolver returns the canned baseURL.
type fakeResolver struct {
	baseURL    string
	err        error
	calls      atomicCounter
	invalidated atomicCounter
}

func (f *fakeResolver) Resolve(_ context.Context, _ string) (string, error) {
	f.calls.add(1)
	if f.err != nil {
		return "", f.err
	}
	return f.baseURL, nil
}

func (f *fakeResolver) Invalidate(_ string) { f.invalidated.add(1) }

type atomicCounter struct {
	mu sync.Mutex
	n  int
}

func (a *atomicCounter) add(n int) { a.mu.Lock(); a.n += n; a.mu.Unlock() }
func (a *atomicCounter) load() int { a.mu.Lock(); defer a.mu.Unlock(); return a.n }

func makeAirplayCmdHandler(t *testing.T) (*AirplayCommandHandler, *airplay.Session, *fakeDACP, *fakeResolver) {
	t.Helper()
	session := airplay.NewSession(airplay.SessionConfig{HeartbeatTimeout: time.Second})
	session.Update(airplay.Frame{
		Title:        "x",
		ActiveRemote: "tok",
		DACPID:       "DEADBEEF",
	})
	dacp := &fakeDACP{}
	res := &fakeResolver{baseURL: "http://192.168.86.50:50556"}
	h := NewAirplayCommandHandler(session, dacp, res)
	return h, session, dacp, res
}

func TestAirplayCommandPlayDispatches(t *testing.T) {
	h, _, dacp, _ := makeAirplayCmdHandler(t)
	resp := h.Handle(context.Background(), map[string]interface{}{"cmd": "play"})
	if !resp.OK {
		t.Fatalf("expected ok=true, got %+v", resp)
	}
	if len(dacp.commands) != 1 || dacp.commands[0].cmd != "play" {
		t.Errorf("dacp commands = %+v", dacp.commands)
	}
	if dacp.commands[0].baseURL != "http://192.168.86.50:50556" {
		t.Errorf("baseURL = %q", dacp.commands[0].baseURL)
	}
	if dacp.commands[0].token != "tok" {
		t.Errorf("token = %q", dacp.commands[0].token)
	}
}

func TestAirplayCommandToggleAlias(t *testing.T) {
	h, _, dacp, _ := makeAirplayCmdHandler(t)
	resp := h.Handle(context.Background(), map[string]interface{}{"cmd": "toggle"})
	if !resp.OK {
		t.Fatalf("expected ok=true: %+v", resp)
	}
	if dacp.commands[0].cmd != "playpause" {
		t.Errorf("toggle should map to playpause, got %q", dacp.commands[0].cmd)
	}
}

func TestAirplayCommandAllCommands(t *testing.T) {
	cases := []struct{ in, want string }{
		{"play", "play"},
		{"pause", "pause"},
		{"toggle", "playpause"},
		{"playpause", "playpause"},
		{"next", "next"},
		{"prev", "prev"},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			h, _, dacp, _ := makeAirplayCmdHandler(t)
			resp := h.Handle(context.Background(), map[string]interface{}{"cmd": tc.in})
			if !resp.OK {
				t.Fatalf("expected ok=true for %q: %+v", tc.in, resp)
			}
			if len(dacp.commands) != 1 || dacp.commands[0].cmd != tc.want {
				t.Errorf("cmd %q → dacp %+v, want %q", tc.in, dacp.commands, tc.want)
			}
		})
	}
}

func TestAirplayCommandRejectsUnknownCommand(t *testing.T) {
	h, _, _, _ := makeAirplayCmdHandler(t)
	resp := h.Handle(context.Background(), map[string]interface{}{"cmd": "nuke"})
	if resp.OK {
		t.Errorf("unknown cmd should not be ok")
	}
	if resp.Error == "" {
		t.Errorf("expected an error message")
	}
}

func TestAirplayCommandRejectsWhenSessionInactive(t *testing.T) {
	session := airplay.NewSession(airplay.SessionConfig{HeartbeatTimeout: time.Second})
	dacp := &fakeDACP{}
	res := &fakeResolver{baseURL: "http://h"}
	h := NewAirplayCommandHandler(session, dacp, res)
	resp := h.Handle(context.Background(), map[string]interface{}{"cmd": "play"})
	if resp.OK {
		t.Errorf("inactive session should reject")
	}
}

func TestAirplayCommandRejectsWhenCannotControl(t *testing.T) {
	session := airplay.NewSession(airplay.SessionConfig{HeartbeatTimeout: time.Second})
	session.Update(airplay.Frame{Title: "x"}) // no ActiveRemote/DACPID
	dacp := &fakeDACP{}
	res := &fakeResolver{baseURL: "http://h"}
	h := NewAirplayCommandHandler(session, dacp, res)
	resp := h.Handle(context.Background(), map[string]interface{}{"cmd": "play"})
	if resp.OK {
		t.Errorf("session without canControl should reject")
	}
}

func TestAirplayCommandInvalidatesResolverOnDACPError(t *testing.T) {
	h, _, dacp, res := makeAirplayCmdHandler(t)
	dacp.err = errors.New("connection refused")
	resp := h.Handle(context.Background(), map[string]interface{}{"cmd": "play"})
	if resp.OK {
		t.Fatalf("expected !ok on transport error")
	}
	if res.invalidated.load() != 1 {
		t.Errorf("resolver should be invalidated once; got %d", res.invalidated.load())
	}
}

func TestAirplayCommandPropagatesResolverError(t *testing.T) {
	session := airplay.NewSession(airplay.SessionConfig{HeartbeatTimeout: time.Second})
	session.Update(airplay.Frame{Title: "x", ActiveRemote: "tok", DACPID: "X"})
	dacp := &fakeDACP{}
	res := &fakeResolver{err: errors.New("not found")}
	h := NewAirplayCommandHandler(session, dacp, res)
	resp := h.Handle(context.Background(), map[string]interface{}{"cmd": "play"})
	if resp.OK {
		t.Errorf("resolver failure should propagate")
	}
}

func TestAirplayCommandIsCallableFromHTTPRoundTripTest(t *testing.T) {
	// Smoke-test that the handler integrates with a stub HTTP server. We
	// construct a fake DACP target with httptest.Server and use the real
	// airplay.NewDACPClient + a stubbed resolver pointing at the test
	// server. This crosses the network boundary end-to-end inside the
	// process.
	t.Parallel()
	server := newTestDACPServer(t)
	defer server.Close()

	session := airplay.NewSession(airplay.SessionConfig{HeartbeatTimeout: time.Second})
	session.Update(airplay.Frame{Title: "x", ActiveRemote: "REMOTE-XYZ", DACPID: "ABC"})

	dacp := airplay.NewDACPClient(&http.Client{Timeout: time.Second})
	res := &fakeResolver{baseURL: server.URL}
	h := NewAirplayCommandHandler(session, dacp, res)

	resp := h.Handle(context.Background(), map[string]interface{}{"cmd": "pause"})
	if !resp.OK {
		t.Fatalf("expected ok=true; got %+v", resp)
	}
	if server.calls.load() != 1 {
		t.Errorf("expected 1 request to fake DACP, got %d", server.calls.load())
	}
}
