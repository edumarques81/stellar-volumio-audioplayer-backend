//go:build darwin

package paths

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// fakeExecCommand returns a CommandContext substitute that runs `echo -n` with
// the canned output, ignoring the requested command. Used to feed canned
// `mount` / `sysctl` output to listMounts / systemHardware without touching
// the host's real mount table.
func fakeExecCommand(t *testing.T, stdout string) func(ctx context.Context, name string, args ...string) *exec.Cmd {
	t.Helper()
	return func(ctx context.Context, _ string, _ ...string) *exec.Cmd {
		return exec.CommandContext(ctx, "echo", "-n", stdout)
	}
}

func TestDarwinDataDirUnderHome(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("UserHomeDir() error = %v", err)
	}
	want := filepath.Join(home, "Library", "Application Support", "stellar")
	if got := dataDir(); got != want {
		t.Errorf("dataDir() = %q, want %q", got, want)
	}
}

func TestDarwinCacheDirUnderHome(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("UserHomeDir() error = %v", err)
	}
	want := filepath.Join(home, "Library", "Caches", "stellar")
	if got := cacheDir(); got != want {
		t.Errorf("cacheDir() = %q, want %q", got, want)
	}
}

func TestDarwinNasMountBase(t *testing.T) {
	if got := nasMountBase(); got != "/Volumes/stellar-nas" {
		t.Errorf("nasMountBase() = %q, want /Volumes/stellar-nas", got)
	}
}

func TestDarwinUsbMountBase(t *testing.T) {
	if got := usbMountBase(); got != "/Volumes" {
		t.Errorf("usbMountBase() = %q, want /Volumes", got)
	}
}

func TestDarwinParseMountOutput(t *testing.T) {
	input := `/dev/disk1s1 on / (apfs, local, journaled)
/dev/disk1s4 on /System/Volumes/VM (apfs, local, noexec)
//user@nas.local/Music on /Volumes/stellar-nas/Music (smbfs, nodev, nosuid)`

	mounts := parseDarwinMountOutput(input)
	if len(mounts) != 3 {
		t.Fatalf("len(mounts) = %d, want 3", len(mounts))
	}
	if mounts[0].Source != "/dev/disk1s1" || mounts[0].MountPoint != "/" || mounts[0].FSType != "apfs" {
		t.Errorf("mounts[0] = %+v, want {/dev/disk1s1 / apfs}", mounts[0])
	}
	if mounts[2].Source != "//user@nas.local/Music" {
		t.Errorf("mounts[2].Source = %q, want //user@nas.local/Music", mounts[2].Source)
	}
	if mounts[2].MountPoint != "/Volumes/stellar-nas/Music" {
		t.Errorf("mounts[2].MountPoint = %q, want /Volumes/stellar-nas/Music", mounts[2].MountPoint)
	}
	if mounts[2].FSType != "smbfs" {
		t.Errorf("mounts[2].FSType = %q, want smbfs", mounts[2].FSType)
	}
}

func TestDarwinParseMountOutputSkipsMalformed(t *testing.T) {
	input := `valid /dev/disk on / (apfs)
no separator here
on its own with no leading source
`
	mounts := parseDarwinMountOutput(input)
	if len(mounts) != 1 {
		t.Fatalf("len(mounts) = %d, want 1 (malformed rows skipped)", len(mounts))
	}
}

func TestDarwinListMountsViaFakeExec(t *testing.T) {
	orig := execCommand
	t.Cleanup(func() { execCommand = orig })
	execCommand = fakeExecCommand(t, `/dev/disk1s1 on / (apfs, local, journaled)
//user@nas/Music on /Volumes/stellar-nas/Music (smbfs, nodev, nosuid)`)

	mounts, err := listMounts()
	if err != nil {
		t.Fatalf("listMounts() error = %v", err)
	}
	if len(mounts) != 2 {
		t.Fatalf("len(mounts) = %d, want 2", len(mounts))
	}
	if !strings.Contains(mounts[1].FSType, "smbfs") {
		t.Errorf("mounts[1].FSType = %q, want contains smbfs", mounts[1].FSType)
	}
}

func TestDarwinSystemHardwareViaSysctl(t *testing.T) {
	orig := execCommand
	t.Cleanup(func() { execCommand = orig })
	execCommand = fakeExecCommand(t, "Macmini9,1\n")

	if got := systemHardware(); got != "Macmini9,1" {
		t.Errorf("systemHardware() = %q, want Macmini9,1", got)
	}
}
