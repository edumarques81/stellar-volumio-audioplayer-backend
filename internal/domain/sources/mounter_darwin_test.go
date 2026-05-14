//go:build darwin

package sources

import (
	"context"
	"os/exec"
	"strings"
	"testing"
)

// capturingExec records the (name, args) tuple of every command invocation so
// tests can assert the exact CLI shape the implementation emits. Returns an
// `echo -n <stdout>` *exec.Cmd so the caller's CombinedOutput() / Output() /
// .Output() reads the configured stdout without ever spawning the real tool.
//
// Shared between mounter_darwin_test.go and discoverer_darwin_test.go —
// declared once here, referenced from both since they're in the same package.
type capturingExec struct {
	calls  [][]string
	stdout string
	err    error
}

func (c *capturingExec) cmd(ctx context.Context, name string, args ...string) *exec.Cmd {
	c.calls = append(c.calls, append([]string{name}, args...))
	return exec.CommandContext(ctx, "echo", "-n", c.stdout)
}

// equalArgs is a tiny helper for slice equality on the captured arg vectors.
func equalArgs(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestDarwinMounterMountCifsBuildsExpectedCommand(t *testing.T) {
	cap := &capturingExec{}
	orig := mountCommand
	t.Cleanup(func() { mountCommand = orig })
	mountCommand = cap.cmd

	m := NewDarwinMounter()
	share := &NasShare{
		IP:         "192.168.1.10",
		Path:       "Music",
		MountPoint: "/Volumes/stellar-nas/Music",
		FSType:     "cifs",
		Username:   "alice",
		Password:   "s3cret",
	}

	if err := m.Mount(context.Background(), share); err != nil {
		t.Fatalf("Mount() error = %v", err)
	}
	if len(cap.calls) != 1 {
		t.Fatalf("expected 1 mount call, got %d", len(cap.calls))
	}
	want := []string{"/sbin/mount_smbfs", "//alice:s3cret@192.168.1.10/Music", "/Volumes/stellar-nas/Music"}
	got := cap.calls[0]
	if !equalArgs(got, want) {
		t.Errorf("mount call args:\n  got:  %v\n  want: %v", got, want)
	}
	if !share.Mounted {
		t.Error("share.Mounted should be true after successful Mount")
	}
}

func TestDarwinMounterMountNfsBuildsExpectedCommand(t *testing.T) {
	cap := &capturingExec{}
	orig := mountCommand
	t.Cleanup(func() { mountCommand = orig })
	mountCommand = cap.cmd

	m := NewDarwinMounter()
	share := &NasShare{
		IP:         "192.168.1.10",
		Path:       "/export/music",
		MountPoint: "/Volumes/stellar-nas/Music",
		FSType:     "nfs",
	}
	if err := m.Mount(context.Background(), share); err != nil {
		t.Fatalf("Mount() error = %v", err)
	}
	want := []string{"/sbin/mount_nfs", "192.168.1.10:/export/music", "/Volumes/stellar-nas/Music"}
	if !equalArgs(cap.calls[0], want) {
		t.Errorf("nfs mount call args:\n  got:  %v\n  want: %v", cap.calls[0], want)
	}
}

func TestDarwinMounterIsMountedReadsMountTable(t *testing.T) {
	cap := &capturingExec{
		stdout: strings.Join([]string{
			"/dev/disk1s1 on / (apfs, local, journaled)",
			"//user@host/share on /Volumes/stellar-nas/Music (smbfs)",
		}, "\n"),
	}
	orig := mountCommand
	t.Cleanup(func() { mountCommand = orig })
	mountCommand = cap.cmd

	m := NewDarwinMounter()
	if !m.IsMounted("/Volumes/stellar-nas/Music") {
		t.Error("IsMounted should return true for mounted SMB share")
	}
	if m.IsMounted("/Volumes/not-mounted") {
		t.Error("IsMounted should return false for non-mounted path")
	}
}
