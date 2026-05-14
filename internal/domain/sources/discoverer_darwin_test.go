//go:build darwin

package sources

import (
	"context"
	"testing"
)

// capturingExec is defined in mounter_darwin_test.go (same package). It is
// reused here for the discoverer tests so we don't duplicate the helper.

func TestDarwinDiscovererParsesDNSSDOutput(t *testing.T) {
	cap := &capturingExec{
		stdout: `Browsing for _smb._tcp
DATE: ---Mon 12 May 2026---
 1:39:42.123  ...STARTING...
Timestamp     A/R    Flags  if Domain   Service Type         Instance Name
 1:39:42.456  Add        2   4 local.   _smb._tcp.           nas-music
 1:39:42.789  Add        2   4 local.   _smb._tcp.           NAS_Backup
`,
	}
	orig := execCommand
	t.Cleanup(func() { execCommand = orig })
	execCommand = cap.cmd

	d := NewDarwinDiscoverer()
	devices, err := d.DiscoverDevices(context.Background())
	if err != nil {
		t.Fatalf("DiscoverDevices() error = %v", err)
	}
	if len(devices) < 2 {
		t.Fatalf("expected at least 2 devices, got %d", len(devices))
	}
	names := map[string]bool{}
	for _, dev := range devices {
		names[dev.Name] = true
	}
	if !names["nas-music"] {
		t.Error("expected device named 'nas-music'")
	}
	if !names["NAS_Backup"] {
		t.Error("expected device named 'NAS_Backup'")
	}
}

func TestDarwinDiscovererBrowseSharesParsesSmbutilView(t *testing.T) {
	cap := &capturingExec{
		stdout: `Share                                 Type    Comments
-------------------------------
Music                                 Disk
Videos                                Disk    Video collection
IPC$                                  IPC     Remote IPC
`,
	}
	orig := execCommand
	t.Cleanup(func() { execCommand = orig })
	execCommand = cap.cmd

	d := NewDarwinDiscoverer()
	shares, err := d.BrowseShares(context.Background(), "nas-music", "", "")
	if err != nil {
		t.Fatalf("BrowseShares() error = %v", err)
	}
	if len(shares) != 2 {
		t.Fatalf("expected 2 shares (IPC$ filtered), got %d", len(shares))
	}
	if shares[0].Name != "Music" || shares[1].Name != "Videos" {
		t.Errorf("got shares %v, want Music + Videos", shares)
	}
}
