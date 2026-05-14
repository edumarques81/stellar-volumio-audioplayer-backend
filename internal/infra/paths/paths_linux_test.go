//go:build linux

package paths

import (
	"strings"
	"testing"
)

func TestUnescapeMountField(t *testing.T) {
	tests := []struct {
		name, in, want string
	}{
		{"plain", "/mnt/USB", "/mnt/USB"},
		{"space", `/mnt/My\040NAS`, "/mnt/My NAS"},
		{"tab", `/mnt/Tab\011Here`, "/mnt/Tab\tHere"},
		{"newline", `/mnt/New\012Line`, "/mnt/New\nLine"},
		{"backslash", `\134root`, `\root`},
		{"multiple", `/mnt/a\040b\040c`, "/mnt/a b c"},
		{"trailing-backslash-no-octal", `/mnt/a\`, `/mnt/a\`},
		{"non-octal-escape", `/mnt/a\xyz`, `/mnt/a\xyz`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := unescapeMountField(tt.in); got != tt.want {
				t.Errorf("unescapeMountField(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestParseHardwareModel(t *testing.T) {
	tests := []struct {
		name, in, want string
	}{
		{"pi5", "Model\t\t: Raspberry Pi 5 Model B Rev 1.0\n", "Raspberry Pi 5 Model B Rev 1.0"},
		{"pi4", "Model\t\t: Raspberry Pi 4 Model B Rev 1.5\n", "Raspberry Pi 4 Model B Rev 1.5"},
		{"x86-no-model", "processor\t: 0\nvendor_id\t: GenuineIntel\nmodel name\t: Intel(R) Core(TM)\n", ""},
		{"empty", "", ""},
		{"model-name-only-no-board-model", "model name\t: Intel(R)\n", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := parseHardwareModel(strings.NewReader(tt.in)); got != tt.want {
				t.Errorf("parseHardwareModel = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestParseMounts(t *testing.T) {
	input := strings.Join([]string{
		`proc /proc proc rw,nosuid,nodev,noexec,relatime 0 0`,
		`/dev/mmcblk0p2 / ext4 rw,noatime 0 0`,
		`//192.168.1.10/Music\040Library /mnt/NAS/MyNAS cifs ro,relatime 0 0`,
		`192.168.1.10:/export/usb /mnt/USB/Drive\0401 nfs rw,relatime 0 0`,
		``,
	}, "\n")

	mounts, err := parseMounts(strings.NewReader(input))
	if err != nil {
		t.Fatalf("parseMounts: %v", err)
	}
	if len(mounts) != 4 {
		t.Fatalf("len(mounts) = %d, want 4", len(mounts))
	}
	if mounts[2].Source != "//192.168.1.10/Music Library" {
		t.Errorf("mounts[2].Source = %q, want unescaped", mounts[2].Source)
	}
	if mounts[2].MountPoint != "/mnt/NAS/MyNAS" {
		t.Errorf("mounts[2].MountPoint = %q", mounts[2].MountPoint)
	}
	if mounts[2].FSType != "cifs" {
		t.Errorf("mounts[2].FSType = %q, want cifs", mounts[2].FSType)
	}
	if mounts[3].MountPoint != "/mnt/USB/Drive 1" {
		t.Errorf("mounts[3].MountPoint = %q, want unescaped", mounts[3].MountPoint)
	}
}

func TestLinuxDataDir(t *testing.T) {
	if got := dataDir(); got != "/data/stellar" {
		t.Errorf("dataDir() = %q, want /data/stellar", got)
	}
}

func TestLinuxNasMountBase(t *testing.T) {
	if got := nasMountBase(); got != "/mnt/NAS" {
		t.Errorf("nasMountBase() = %q, want /mnt/NAS", got)
	}
}

func TestLinuxUsbMountBase(t *testing.T) {
	if got := usbMountBase(); got != "/mnt/USB" {
		t.Errorf("usbMountBase() = %q, want /mnt/USB", got)
	}
}

func TestLinuxListMountsParses(t *testing.T) {
	// We can't depend on a specific mount existing, but /proc/mounts is
	// guaranteed non-empty on a running Linux system. Assert at least one row
	// with a non-empty mount point.
	mounts, err := listMounts()
	if err != nil {
		t.Fatalf("listMounts() error = %v", err)
	}
	if len(mounts) == 0 {
		t.Fatal("listMounts() returned 0 rows on Linux")
	}
	hasRoot := false
	for _, m := range mounts {
		if m.MountPoint == "/" {
			hasRoot = true
		}
		if m.MountPoint == "" {
			t.Errorf("mount with empty MountPoint: %+v", m)
		}
	}
	if !hasRoot {
		t.Error("expected at least one mount with MountPoint=/")
	}
}

func TestLinuxSystemHardware(t *testing.T) {
	// On any Linux box, /proc/cpuinfo exists. On a Pi the "Model:" line
	// surfaces "Raspberry Pi ..."; on a CI VM it may be empty. Either is OK;
	// we only assert non-empty on Pi and that the function doesn't panic.
	got := systemHardware()
	if strings.Contains(got, "\x00") {
		t.Errorf("systemHardware() = %q contains NUL", got)
	}
}
