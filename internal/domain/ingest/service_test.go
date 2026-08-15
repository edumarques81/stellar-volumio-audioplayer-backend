package ingest_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/edumarques81/stellar-volumio-audioplayer-backend/internal/domain/ingest"
)

// --------------------------------------------------------------------------
// Helpers
// --------------------------------------------------------------------------

type fakeRunner struct {
	mu      sync.Mutex
	calls   [][]string
	stdout  []byte
	err     error
	onRun   func()
	stdouts [][]byte // consumed in order when non-empty
}

func (f *fakeRunner) Run(_ context.Context, args ...string) ([]byte, error) {
	f.mu.Lock()
	f.calls = append(f.calls, args)
	out := f.stdout
	if len(f.stdouts) > 0 {
		out = f.stdouts[0]
		f.stdouts = f.stdouts[1:]
	}
	hook := f.onRun
	f.mu.Unlock()

	if hook != nil {
		hook()
	}
	return out, f.err
}

func (f *fakeRunner) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

func (f *fakeRunner) lastArgs() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.calls) == 0 {
		return nil
	}
	return f.calls[len(f.calls)-1]
}

const previewJSON = `{
  "schema": 1, "dryRun": true, "error": "", "exitCode": 0,
  "items": [{"name":"Miles Davis - Kind Of Blue","status":"would-ingest",
             "reason":"dry run -- would land at /mnt/ssd/Music/Kind of Blue",
             "target":"/mnt/ssd/Music/Kind of Blue","audioFiles":5,
             "tagged":["01.flac"],"tagFailures":[],"md5Mismatches":[],
             "mbRelease":"Kind of Blue (e32a3f0b, score 100)","art":"folder.jpg","notes":[]}],
  "summary": {"total":1,"ingested":0,"wouldIngest":1,"refused":0,"skipped":0,
              "tagFailures":0,"audioAltered":0},
  "mpd": {}
}`

const commitJSON = `{
  "schema": 1, "dryRun": false, "error": "", "exitCode": 0,
  "items": [{"name":"Miles Davis - Kind Of Blue","status":"ingested",
             "target":"/mnt/ssd/Music/Kind of Blue","audioFiles":5}],
  "summary": {"total":1,"ingested":1,"wouldIngest":0,"refused":0,"skipped":0,
              "tagFailures":0,"audioAltered":0},
  "mpd": {"artists":124,"albums":62,"songs":826}
}`

// newService builds a service over a temp inbox containing the given entries,
// each a directory holding one file.
func newService(t *testing.T, runner ingest.Runner, entries ...string) (*ingest.Service, string) {
	t.Helper()
	dir := t.TempDir()
	inbox := filepath.Join(dir, "inbox")
	if err := os.MkdirAll(inbox, 0o755); err != nil {
		t.Fatalf("mkdir inbox: %v", err)
	}
	script := filepath.Join(dir, "stellar-ingest")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("write script: %v", err)
	}
	for _, name := range entries {
		addEntry(t, inbox, name, "audio bytes")
	}
	return ingest.NewService(ingest.Config{ScriptPath: script, InboxDir: inbox}, runner), inbox
}

func addEntry(t *testing.T, inbox, name, contents string) {
	t.Helper()
	dir := filepath.Join(inbox, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", name, err)
	}
	if err := os.WriteFile(filepath.Join(dir, "01.flac"), []byte(contents), 0o644); err != nil {
		t.Fatalf("write track: %v", err)
	}
}

// --------------------------------------------------------------------------
// Availability and status
// --------------------------------------------------------------------------

func TestAvailable(t *testing.T) {
	t.Parallel()

	svc, _ := newService(t, &fakeRunner{})
	if !svc.Available() {
		t.Fatal("expected available with script and inbox present")
	}

	tests := []struct {
		name string
		cfg  ingest.Config
	}{
		{"no script path", ingest.Config{InboxDir: t.TempDir()}},
		{"no inbox path", ingest.Config{ScriptPath: "/bin/sh"}},
		{"script missing", ingest.Config{ScriptPath: "/nope/stellar-ingest", InboxDir: t.TempDir()}},
		{"inbox missing", ingest.Config{ScriptPath: "/bin/sh", InboxDir: "/nope/inbox"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if ingest.NewService(tc.cfg, &fakeRunner{}).Available() {
				t.Fatalf("expected unavailable for %s", tc.name)
			}
		})
	}
}

func TestStatus_SkipsBookkeepingEntries(t *testing.T) {
	t.Parallel()

	svc, inbox := newService(t, &fakeRunner{}, "Album B", "Album A")
	addEntry(t, inbox, ".done", "retired")
	addEntry(t, inbox, ".rejected", "refused")
	addEntry(t, inbox, "staging", "work in progress")

	got := svc.Status()
	if !got.Available {
		t.Fatal("expected available")
	}
	if got.Busy {
		t.Fatal("expected not busy")
	}
	want := []string{"Album A", "Album B"}
	if len(got.Items) != len(want) || got.Count != len(want) {
		t.Fatalf("items = %v (count %d), want %v", got.Items, got.Count, want)
	}
	for i, name := range want {
		if got.Items[i] != name {
			t.Fatalf("items[%d] = %q, want %q (sorted)", i, got.Items[i], name)
		}
	}
}

func TestStatus_UnavailableHostReportsEmpty(t *testing.T) {
	t.Parallel()

	svc := ingest.NewService(ingest.Config{}, &fakeRunner{})
	got := svc.Status()
	if got.Available {
		t.Fatal("expected unavailable")
	}
	if got.Items == nil {
		t.Fatal("Items must be non-nil so the JSON payload is [] and not null")
	}
}

// --------------------------------------------------------------------------
// Preview
// --------------------------------------------------------------------------

func TestPreview_PassesDryRunAndReturnsToken(t *testing.T) {
	t.Parallel()

	runner := &fakeRunner{stdout: []byte(previewJSON)}
	svc, _ := newService(t, runner, "Miles Davis - Kind Of Blue")

	report, err := svc.Preview(context.Background())
	if err != nil {
		t.Fatalf("Preview: %v", err)
	}

	args := strings.Join(runner.lastArgs(), " ")
	if args != "--json --dry-run" {
		t.Fatalf("args = %q, want %q", args, "--json --dry-run")
	}
	if !report.DryRun {
		t.Fatal("expected DryRun true")
	}
	if report.Summary.WouldIngest != 1 || len(report.Items) != 1 {
		t.Fatalf("summary = %+v, items = %d", report.Summary, len(report.Items))
	}
	if report.Items[0].Status != "would-ingest" {
		t.Fatalf("status = %q, want would-ingest", report.Items[0].Status)
	}
	if report.Token == "" {
		t.Fatal("expected a plan token")
	}
}

func TestPreview_RejectsUnknownSchema(t *testing.T) {
	t.Parallel()

	runner := &fakeRunner{stdout: []byte(`{"schema": 99, "items": []}`)}
	svc, _ := newService(t, runner, "Album A")

	if _, err := svc.Preview(context.Background()); err == nil ||
		!strings.Contains(err.Error(), "redeploy stellar-ingest") {
		t.Fatalf("err = %v, want a schema-mismatch error naming the fix", err)
	}
}

func TestPreview_RejectsUnparseableOutput(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		stdout string
	}{
		{"empty", ""},
		{"human text", "Inbox: /home/eduardo/inbox (1 item)"},
		{"truncated json", `{"schema": 1, "items": [`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			runner := &fakeRunner{stdout: []byte(tc.stdout)}
			svc, _ := newService(t, runner, "Album A")
			if _, err := svc.Preview(context.Background()); err == nil {
				t.Fatal("expected an error")
			}
		})
	}
}

func TestPreview_UnavailableHost(t *testing.T) {
	t.Parallel()

	svc := ingest.NewService(ingest.Config{}, &fakeRunner{stdout: []byte(previewJSON)})
	if _, err := svc.Preview(context.Background()); !errors.Is(err, ingest.ErrUnavailable) {
		t.Fatalf("err = %v, want ErrUnavailable", err)
	}
}

// --------------------------------------------------------------------------
// Commit
// --------------------------------------------------------------------------

func TestCommit_HappyPath(t *testing.T) {
	t.Parallel()

	runner := &fakeRunner{stdouts: [][]byte{[]byte(previewJSON), []byte(commitJSON)}}
	svc, _ := newService(t, runner, "Miles Davis - Kind Of Blue")

	preview, err := svc.Preview(context.Background())
	if err != nil {
		t.Fatalf("Preview: %v", err)
	}

	report, err := svc.Commit(context.Background(), preview.Token)
	if err != nil {
		t.Fatalf("Commit: %v", err)
	}

	args := strings.Join(runner.lastArgs(), " ")
	if args != "--json" {
		t.Fatalf("commit args = %q, want %q (no --dry-run)", args, "--json")
	}
	if report.Summary.Ingested != 1 {
		t.Fatalf("ingested = %d, want 1", report.Summary.Ingested)
	}
	if report.MPD["songs"] != 826 {
		t.Fatalf("mpd songs = %d, want 826", report.MPD["songs"])
	}
}

func TestCommit_TokenRules(t *testing.T) {
	t.Parallel()

	t.Run("without a preview", func(t *testing.T) {
		t.Parallel()
		svc, _ := newService(t, &fakeRunner{stdout: []byte(commitJSON)}, "Album A")
		if _, err := svc.Commit(context.Background(), "anything"); !errors.Is(err, ingest.ErrNoPlan) {
			t.Fatalf("err = %v, want ErrNoPlan", err)
		}
	})

	t.Run("wrong token", func(t *testing.T) {
		t.Parallel()
		runner := &fakeRunner{stdouts: [][]byte{[]byte(previewJSON), []byte(commitJSON)}}
		svc, _ := newService(t, runner, "Album A")
		if _, err := svc.Preview(context.Background()); err != nil {
			t.Fatalf("Preview: %v", err)
		}
		if _, err := svc.Commit(context.Background(), "not-the-token"); !errors.Is(err, ingest.ErrStalePlan) {
			t.Fatalf("err = %v, want ErrStalePlan", err)
		}
		if runner.callCount() != 1 {
			t.Fatalf("script ran %d times; a rejected token must not run it", runner.callCount())
		}
	})

	t.Run("token spent after a successful commit", func(t *testing.T) {
		t.Parallel()
		runner := &fakeRunner{stdouts: [][]byte{[]byte(previewJSON), []byte(commitJSON)}}
		svc, _ := newService(t, runner, "Album A")
		preview, err := svc.Preview(context.Background())
		if err != nil {
			t.Fatalf("Preview: %v", err)
		}
		if _, err := svc.Commit(context.Background(), preview.Token); err != nil {
			t.Fatalf("Commit: %v", err)
		}
		if _, err := svc.Commit(context.Background(), preview.Token); !errors.Is(err, ingest.ErrNoPlan) {
			t.Fatalf("replayed token err = %v, want ErrNoPlan", err)
		}
	})
}

func TestCommit_RefusesWhenInboxChanged(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		mutate func(t *testing.T, inbox string)
	}{
		{"item added", func(t *testing.T, inbox string) {
			addEntry(t, inbox, "Late Arrival", "more audio")
		}},
		{"item removed", func(t *testing.T, inbox string) {
			if err := os.RemoveAll(filepath.Join(inbox, "Album A")); err != nil {
				t.Fatalf("remove: %v", err)
			}
		}},
		{"nested file changed", func(t *testing.T, inbox string) {
			p := filepath.Join(inbox, "Album A", "01.flac")
			if err := os.WriteFile(p, []byte("different bytes entirely"), 0o644); err != nil {
				t.Fatalf("rewrite: %v", err)
			}
		}},
		{"nested file added deep in the tree", func(t *testing.T, inbox string) {
			deep := filepath.Join(inbox, "Album A", "disc2")
			if err := os.MkdirAll(deep, 0o755); err != nil {
				t.Fatalf("mkdir: %v", err)
			}
			if err := os.WriteFile(filepath.Join(deep, "02.flac"), []byte("x"), 0o644); err != nil {
				t.Fatalf("write: %v", err)
			}
		}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			runner := &fakeRunner{stdouts: [][]byte{[]byte(previewJSON), []byte(commitJSON)}}
			svc, inbox := newService(t, runner, "Album A")

			preview, err := svc.Preview(context.Background())
			if err != nil {
				t.Fatalf("Preview: %v", err)
			}

			tc.mutate(t, inbox)

			if _, err := svc.Commit(context.Background(), preview.Token); !errors.Is(err, ingest.ErrStalePlan) {
				t.Fatalf("err = %v, want ErrStalePlan", err)
			}
			if runner.callCount() != 1 {
				t.Fatalf("script ran %d times; a stale plan must not run it", runner.callCount())
			}
		})
	}
}

// --------------------------------------------------------------------------
// Single-flight
// --------------------------------------------------------------------------

func TestSingleFlight_SecondRunIsRefused(t *testing.T) {
	t.Parallel()

	release := make(chan struct{})
	entered := make(chan struct{})
	runner := &fakeRunner{
		stdout: []byte(previewJSON),
		onRun: func() {
			close(entered)
			<-release
		},
	}
	svc, _ := newService(t, runner, "Album A")

	done := make(chan error, 1)
	go func() {
		_, err := svc.Preview(context.Background())
		done <- err
	}()

	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("first run never started")
	}

	if _, err := svc.Preview(context.Background()); !errors.Is(err, ingest.ErrBusy) {
		t.Fatalf("concurrent err = %v, want ErrBusy", err)
	}
	if got := svc.Status(); !got.Busy {
		t.Fatal("Status should report busy while a run is in flight")
	}

	close(release)
	if err := <-done; err != nil {
		t.Fatalf("first run: %v", err)
	}
	if got := svc.Status(); got.Busy {
		t.Fatal("Status should report idle once the run finished")
	}
}

func TestSingleFlight_LockReleasedAfterFailure(t *testing.T) {
	t.Parallel()

	runner := &fakeRunner{stdouts: [][]byte{[]byte("not json"), []byte(previewJSON)}}
	svc, _ := newService(t, runner, "Album A")

	if _, err := svc.Preview(context.Background()); err == nil {
		t.Fatal("expected the first run to fail")
	}
	if _, err := svc.Preview(context.Background()); err != nil {
		t.Fatalf("second run after a failure: %v", err)
	}
}
