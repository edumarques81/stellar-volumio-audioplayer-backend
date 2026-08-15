package ingest

import (
	"context"
	"errors"
	"os/exec"
)

// execRunner is the production Runner: it invokes the stellar-ingest script and
// captures stdout only. The script sends its running commentary to stderr in
// --json mode, so stdout is the report document and nothing else.
type execRunner struct {
	script string
}

func (r *execRunner) Run(ctx context.Context, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, r.script, args...)
	stdout, err := cmd.Output()

	// Exit 1 means "ran fine, something was refused" and exit 2 means "could
	// not start work" -- both come with a valid JSON document on stdout, so the
	// exit status alone is not an error worth discarding the report for.
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && len(stdout) > 0 {
		return stdout, nil
	}
	return stdout, err
}
