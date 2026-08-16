// Package ingest drives the stellar-ingest script that lands staged music from
// the Samba drop-box onto the read-only SSD.
//
// The backend never touches /mnt/ssd itself. It shells out to
// deploy/stellar-ingest.py in --json mode and re-exports the script's report,
// which keeps the guarded remount,rw -> copy -> remount,ro protocol in exactly
// one place.
//
// Two safety properties this package is responsible for:
//
//   - Single-flight. Two concurrent ingests would open the write window twice
//     and race each other's remount-back-to-ro. Only one run at a time, ever.
//   - Preview binds to commit. Preview returns a token derived from the inbox
//     contents; Commit refuses a token that no longer matches. A user who
//     confirms a plan gets that plan executed, not whatever happens to be in
//     the inbox by the time the tap lands.
package ingest

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

// Errors callers are expected to distinguish. Everything else is an
// operational failure and should surface as-is.
var (
	// ErrBusy means an ingest is already running.
	ErrBusy = errors.New("ingest: a run is already in progress")
	// ErrStalePlan means the inbox changed between preview and commit.
	ErrStalePlan = errors.New("ingest: the inbox changed since the preview; preview again")
	// ErrNoPlan means Commit was called without a preceding Preview.
	ErrNoPlan = errors.New("ingest: no preview to confirm")
	// ErrUnavailable means the script or the inbox is missing on this host.
	ErrUnavailable = errors.New("ingest: not available on this host")
)

// Runner executes the ingest script and returns its stdout. Implementations
// must return the stdout bytes even when the process exits non-zero: the script
// reports refusals through exit code 1 with a perfectly valid JSON document on
// stdout, and discarding it would throw away the reason.
type Runner interface {
	Run(ctx context.Context, args ...string) (stdout []byte, err error)
}

// Config wires the service to the host.
type Config struct {
	// ScriptPath is the stellar-ingest executable. Empty disables the service.
	ScriptPath string
	// InboxDir is the Samba drop-box root.
	InboxDir string
}

// Service is safe for concurrent use.
type Service struct {
	cfg    Config
	runner Runner

	mu      sync.Mutex
	running bool
	token   string
	// pending is the report that issued token, kept for as long as the token
	// is spendable. Preview and commit results reach clients as broadcasts, so
	// a controller that was backgrounded or off-network while the dry run
	// finished never saw the plan and has no way to ask for it again without
	// paying for a second full run. Retaining it here lets a reconnecting
	// client be handed the plan its token would confirm.
	pending Report
}

// NewService constructs a Service. Passing a nil runner installs the real
// exec-based one.
func NewService(cfg Config, runner Runner) *Service {
	if runner == nil {
		runner = &execRunner{script: cfg.ScriptPath}
	}
	return &Service{cfg: cfg, runner: runner}
}

// Available reports whether this host can ingest at all. It is false on the
// Mac, where neither the script nor the inbox exists.
func (s *Service) Available() bool {
	if s == nil || s.cfg.ScriptPath == "" || s.cfg.InboxDir == "" {
		return false
	}
	if _, err := os.Stat(s.cfg.ScriptPath); err != nil {
		return false
	}
	info, err := os.Stat(s.cfg.InboxDir)
	return err == nil && info.IsDir()
}

// Status lists what is waiting in the inbox without running the script.
func (s *Service) Status() Status {
	if !s.Available() {
		return Status{Available: false, Items: []string{}}
	}
	s.mu.Lock()
	busy := s.running
	s.mu.Unlock()

	names, err := s.inboxEntries()
	if err != nil {
		return Status{Available: true, Busy: busy, Items: []string{}, Error: err.Error()}
	}
	return Status{Items: names, Count: len(names), Busy: busy, Available: true}
}

// Preview runs the script with --dry-run and returns the plan plus a token
// bound to the current inbox contents.
func (s *Service) Preview(ctx context.Context) (Report, error) {
	report, err := s.execute(ctx, true)
	if err != nil {
		return report, err
	}

	token, err := s.planToken()
	if err != nil {
		return report, fmt.Errorf("ingest: fingerprinting the inbox: %w", err)
	}
	report.Token = token

	s.mu.Lock()
	s.token = token
	s.pending = report
	s.mu.Unlock()

	return report, nil
}

// PendingPreview returns the plan a client could still confirm, if any.
//
// It re-fingerprints the inbox rather than trusting the stored token: a plan
// whose files have since changed would be refused by Commit anyway, and
// handing it to a reconnecting client would arm a confirm button that can only
// fail. A plan found stale here is dropped, exactly as Commit drops it.
func (s *Service) PendingPreview() (Report, bool) {
	s.mu.Lock()
	expected := s.token
	report := s.pending
	s.mu.Unlock()

	if expected == "" {
		return Report{}, false
	}

	current, err := s.planToken()
	if err != nil || current != expected {
		s.clearPlan()
		return Report{}, false
	}
	return report, true
}

func (s *Service) clearPlan() {
	s.mu.Lock()
	s.token = ""
	s.pending = Report{}
	s.mu.Unlock()
}

// Commit runs the real ingest, but only if token still describes the inbox.
func (s *Service) Commit(ctx context.Context, token string) (Report, error) {
	s.mu.Lock()
	expected := s.token
	s.mu.Unlock()

	if expected == "" {
		return Report{}, ErrNoPlan
	}
	if token == "" || token != expected {
		return Report{}, ErrStalePlan
	}

	// Re-fingerprint at commit time: the stored token was taken at preview,
	// and the inbox is a network share anyone can drop into meanwhile.
	current, err := s.planToken()
	if err != nil {
		return Report{}, fmt.Errorf("ingest: fingerprinting the inbox: %w", err)
	}
	if current != expected {
		s.clearPlan()
		return Report{}, ErrStalePlan
	}

	report, err := s.execute(ctx, false)
	if err != nil {
		return report, err
	}

	// The plan has been spent either way — the inbox has moved on.
	s.clearPlan()

	return report, nil
}

// execute holds the single-flight lock for the whole run.
func (s *Service) execute(ctx context.Context, dryRun bool) (Report, error) {
	if !s.Available() {
		return Report{}, ErrUnavailable
	}

	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return Report{}, ErrBusy
	}
	s.running = true
	s.mu.Unlock()

	defer func() {
		s.mu.Lock()
		s.running = false
		s.mu.Unlock()
	}()

	args := []string{"--json"}
	if dryRun {
		args = append(args, "--dry-run")
	}

	stdout, runErr := s.runner.Run(ctx, args...)
	report, parseErr := parseReport(stdout)
	if parseErr != nil {
		if runErr != nil {
			// No parseable document AND a failed process: the process failure
			// is the more useful of the two.
			return Report{}, fmt.Errorf("ingest: %w", runErr)
		}
		return Report{}, parseErr
	}
	return report, nil
}

func parseReport(stdout []byte) (Report, error) {
	trimmed := strings.TrimSpace(string(stdout))
	if trimmed == "" {
		return Report{}, errors.New("ingest: script produced no report")
	}

	var report Report
	if err := json.Unmarshal([]byte(trimmed), &report); err != nil {
		return Report{}, fmt.Errorf("ingest: unreadable report: %w", err)
	}
	if report.Schema != SchemaVersion {
		return Report{}, fmt.Errorf("ingest: report schema %d, expected %d -- redeploy stellar-ingest",
			report.Schema, SchemaVersion)
	}
	if report.Items == nil {
		report.Items = []Item{}
	}
	return report, nil
}

// inboxEntries lists the top-level drop-box entries, skipping the script's own
// bookkeeping directories (.done, .rejected, staging).
func (s *Service) inboxEntries() ([]string, error) {
	entries, err := os.ReadDir(s.cfg.InboxDir)
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".") || e.Name() == "staging" {
			continue
		}
		names = append(names, e.Name())
	}
	sort.Strings(names)
	return names, nil
}

// planToken fingerprints the inbox tree — every file's path, size and mtime.
// Walking rather than stat-ing the top level is deliberate: a directory's own
// mtime does not change when a file three levels down does, and a preview that
// silently commits different bytes than it showed is the failure mode this
// token exists to prevent.
func (s *Service) planToken() (string, error) {
	names, err := s.inboxEntries()
	if err != nil {
		return "", err
	}

	h := sha256.New()
	for _, name := range names {
		root := filepath.Join(s.cfg.InboxDir, name)
		walkErr := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			rel, relErr := filepath.Rel(s.cfg.InboxDir, path)
			if relErr != nil {
				return relErr
			}
			// hash.Hash writes never fail, so the Fprintf errors are discarded
			// deliberately rather than plumbed through the walk.
			if d.IsDir() {
				_, _ = fmt.Fprintf(h, "d\x00%s\x00\n", rel)
				return nil
			}
			info, infoErr := d.Info()
			if infoErr != nil {
				return infoErr
			}
			_, _ = fmt.Fprintf(h, "f\x00%s\x00%d\x00%d\n", rel, info.Size(), info.ModTime().UnixNano())
			return nil
		})
		if walkErr != nil {
			return "", walkErr
		}
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
