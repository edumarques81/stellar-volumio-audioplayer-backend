//go:build windows

package netinfo

import "context"

type windowsReporter struct{}

func newPlatform() Reporter                  { return &windowsReporter{} }
func (r *windowsReporter) GetStatus() Status { return Status{Type: "none"} }

// startWatcher is a no-op on windows — M1.A windows is stub-only.
func startWatcher(ctx context.Context, r Reporter, brd Broadcaster) {}
