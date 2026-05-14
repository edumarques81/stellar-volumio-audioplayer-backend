//go:build darwin

package netinfo

import (
	"context"
	"os/exec"
	"testing"
)

// fakeExecCommand returns a CommandContext substitute that emits canned
// stdout per command name. Unmatched commands return empty stdout.
func fakeExecCommand(t *testing.T, byCommand map[string]string) func(ctx context.Context, name string, args ...string) *exec.Cmd {
	t.Helper()
	return func(ctx context.Context, name string, args ...string) *exec.Cmd {
		out := byCommand[name]
		return exec.CommandContext(ctx, "echo", "-n", out)
	}
}

func TestDarwinEthernet(t *testing.T) {
	orig := execCommand
	t.Cleanup(func() { execCommand = orig })
	execCommand = fakeExecCommand(t, map[string]string{
		"/usr/sbin/networksetup": `Hardware Port: Ethernet
Device: en0
Ethernet Address: aa:bb

Hardware Port: Wi-Fi
Device: en1
`,
		"/sbin/ifconfig": `en0: flags=8863<UP,BROADCAST,RUNNING> mtu 1500
	inet 192.168.86.221 netmask 0xffffff00 broadcast 192.168.86.255
	status: active`,
	})

	r := newPlatform()
	got := r.GetStatus()
	if got.Type != "ethernet" {
		t.Errorf("Type = %q, want ethernet", got.Type)
	}
	if got.IP != "192.168.86.221" {
		t.Errorf("IP = %q, want 192.168.86.221", got.IP)
	}
	if got.Strength != 3 {
		t.Errorf("Strength = %d, want 3", got.Strength)
	}
}

func TestDarwinWifiWithStrongSignal(t *testing.T) {
	orig := execCommand
	t.Cleanup(func() { execCommand = orig })
	execCommand = fakeExecCommand(t, map[string]string{
		"/usr/sbin/networksetup": `Hardware Port: Wi-Fi
Device: en0
`,
		"/sbin/ifconfig": `en0: flags=8863<UP,BROADCAST,RUNNING> mtu 1500
	inet 10.0.0.5 netmask 0xffffff00
	status: active`,
		"/System/Library/PrivateFrameworks/Apple80211.framework/Versions/Current/Resources/airport": ` agrCtlRSSI: -55
 SSID: HomeNet
 lastTxRate: 433`,
	})

	r := newPlatform()
	got := r.GetStatus()
	if got.Type != "wifi" {
		t.Errorf("Type = %q, want wifi", got.Type)
	}
	if got.SSID != "HomeNet" {
		t.Errorf("SSID = %q, want HomeNet", got.SSID)
	}
	if got.IP != "10.0.0.5" {
		t.Errorf("IP = %q, want 10.0.0.5", got.IP)
	}
	// -55 dBm → 2*(-55+100) = 90 → Strength 3
	if got.Strength != 3 {
		t.Errorf("Strength = %d, want 3 for -55 dBm RSSI", got.Strength)
	}
	if got.Signal != 90 {
		t.Errorf("Signal = %d, want 90 for -55 dBm RSSI", got.Signal)
	}
}

func TestDarwinNone(t *testing.T) {
	orig := execCommand
	t.Cleanup(func() { execCommand = orig })
	execCommand = fakeExecCommand(t, map[string]string{})

	r := newPlatform()
	got := r.GetStatus()
	if got.Type != "none" {
		t.Errorf("Type = %q, want none", got.Type)
	}
}

func TestParseIntForgiving(t *testing.T) {
	tests := []struct {
		in   string
		want int
	}{
		{"42", 42},
		{"-55", -55},
		{"  ", 0},
		{"-55 dBm", -55},
		{"50.5", 50},
		{"abc", 0},
		{"", 0},
		{"0", 0},
	}
	for _, tt := range tests {
		if got := parseIntForgiving(tt.in); got != tt.want {
			t.Errorf("parseIntForgiving(%q) = %d, want %d", tt.in, got, tt.want)
		}
	}
}

func TestRssiToSignal(t *testing.T) {
	tests := []struct {
		rssi int
		want int
	}{
		{0, 0},     // sentinel
		{-50, 100}, // max
		{-55, 90},
		{-75, 50},
		{-100, 0}, // floor
		{-150, 0}, // pinned to floor
		{50, 100}, // positive RSSI shouldn't happen but pinned to ceiling
	}
	for _, tt := range tests {
		if got := rssiToSignal(tt.rssi); got != tt.want {
			t.Errorf("rssiToSignal(%d) = %d, want %d", tt.rssi, got, tt.want)
		}
	}
}
