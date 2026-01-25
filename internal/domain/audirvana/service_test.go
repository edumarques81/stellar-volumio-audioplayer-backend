package audirvana

import (
	"testing"
)

func TestParseAvahiBrowseOutput(t *testing.T) {
	tests := []struct {
		name     string
		output   string
		expected []Instance
	}{
		{
			name:     "empty output",
			output:   "",
			expected: []Instance{},
		},
		{
			name: "valid output with one instance",
			output: `+   eth0 IPv6 stellar                                       _audirvana-ap._tcp   local
+   eth0 IPv4 stellar                                       _audirvana-ap._tcp   local
=   eth0 IPv4 stellar                                       _audirvana-ap._tcp   local
   hostname = [stellar.local]
   address = [192.168.86.34]
   port = [39887]
   txt = ["protovers=4.1.0" "osversion=Linux" "txtvers=1"]`,
			expected: []Instance{
				{
					Name:            "stellar",
					Hostname:        "stellar.local",
					Address:         "192.168.86.34",
					Port:            39887,
					ProtocolVersion: "4.1.0",
					OS:              "Linux",
				},
			},
		},
		{
			name: "valid output with two instances",
			output: `=   eth0 IPv4 stellar                                       _audirvana-ap._tcp   local
   hostname = [stellar.local]
   address = [192.168.86.34]
   port = [39887]
   txt = ["protovers=4.1.0" "osversion=Linux" "txtvers=1"]
=   eth0 IPv4 DESKTOP-7B25TBE                               _audirvana-ap._tcp   local
   hostname = [DESKTOP-7B25TBE.local]
   address = [192.168.86.28]
   port = [57768]
   txt = ["txtvers=1" "osversion=Win" "protovers=4.1.0"]`,
			expected: []Instance{
				{
					Name:            "stellar",
					Hostname:        "stellar.local",
					Address:         "192.168.86.34",
					Port:            39887,
					ProtocolVersion: "4.1.0",
					OS:              "Linux",
				},
				{
					Name:            "DESKTOP-7B25TBE",
					Hostname:        "DESKTOP-7B25TBE.local",
					Address:         "192.168.86.28",
					Port:            57768,
					ProtocolVersion: "4.1.0",
					OS:              "Win",
				},
			},
		},
		{
			name: "output with no resolved services (only discoveries)",
			output: `+   eth0 IPv6 stellar                                       _audirvana-ap._tcp   local
+   eth0 IPv4 stellar                                       _audirvana-ap._tcp   local`,
			expected: []Instance{},
		},
		{
			name: "IPv6 address",
			output: `=   eth0 IPv6 stellar                                       _audirvana-ap._tcp   local
   hostname = [stellar.local]
   address = [fd8f:a9c6:654:5933:ede4:428c:88a2:186]
   port = [39887]
   txt = ["protovers=4.1.0" "osversion=Linux" "txtvers=1"]`,
			expected: []Instance{
				{
					Name:            "stellar",
					Hostname:        "stellar.local",
					Address:         "fd8f:a9c6:654:5933:ede4:428c:88a2:186",
					Port:            39887,
					ProtocolVersion: "4.1.0",
					OS:              "Linux",
				},
			},
		},
		{
			name: "deduplicate instances prefer IPv4",
			output: `=   eth0 IPv6 stellar                                       _audirvana-ap._tcp   local
   hostname = [stellar.local]
   address = [fd8f:a9c6:654:5933::186]
   port = [39887]
   txt = ["protovers=4.1.0" "osversion=Linux" "txtvers=1"]
=   eth0 IPv4 stellar                                       _audirvana-ap._tcp   local
   hostname = [stellar.local]
   address = [192.168.86.34]
   port = [39887]
   txt = ["protovers=4.1.0" "osversion=Linux" "txtvers=1"]`,
			expected: []Instance{
				{
					Name:            "stellar",
					Hostname:        "stellar.local",
					Address:         "192.168.86.34",
					Port:            39887,
					ProtocolVersion: "4.1.0",
					OS:              "Linux",
				},
			},
		},
		{
			name: "empty TXT record",
			output: `=   eth0 IPv4 stellar                                       _audirvana-ap._tcp   local
   hostname = [stellar.local]
   address = [192.168.86.34]
   port = [39887]
   txt = []`,
			expected: []Instance{
				{
					Name:            "stellar",
					Hostname:        "stellar.local",
					Address:         "192.168.86.34",
					Port:            39887,
					ProtocolVersion: "unknown",
					OS:              "unknown",
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			instances := ParseAvahiBrowseOutput(tt.output)
			if len(instances) != len(tt.expected) {
				t.Errorf("expected %d instances, got %d", len(tt.expected), len(instances))
				return
			}

			for i, exp := range tt.expected {
				got := instances[i]
				if got.Name != exp.Name {
					t.Errorf("instance %d: expected name %q, got %q", i, exp.Name, got.Name)
				}
				if got.Hostname != exp.Hostname {
					t.Errorf("instance %d: expected hostname %q, got %q", i, exp.Hostname, got.Hostname)
				}
				if got.Address != exp.Address {
					t.Errorf("instance %d: expected address %q, got %q", i, exp.Address, got.Address)
				}
				if got.Port != exp.Port {
					t.Errorf("instance %d: expected port %d, got %d", i, exp.Port, got.Port)
				}
				if got.ProtocolVersion != exp.ProtocolVersion {
					t.Errorf("instance %d: expected protocol_version %q, got %q", i, exp.ProtocolVersion, got.ProtocolVersion)
				}
				if got.OS != exp.OS {
					t.Errorf("instance %d: expected os %q, got %q", i, exp.OS, got.OS)
				}
			}
		})
	}
}

func TestParseSystemctlStatus(t *testing.T) {
	tests := []struct {
		name     string
		output   string
		expected ServiceStatus
	}{
		{
			name:   "service not found",
			output: `Unit audirvanaStudio.service could not be found.`,
			expected: ServiceStatus{
				Loaded:  false,
				Enabled: false,
				Active:  false,
				Running: false,
			},
		},
		{
			name: "active and running",
			output: `● audirvanaStudio.service - Run audirvanaStudio
     Loaded: loaded (/etc/systemd/system/audirvanaStudio.service; enabled; preset: enabled)
     Active: active (running) since Sun 2026-01-25 13:52:11 AEST; 3s ago
   Main PID: 6448 (audirvanaStudio)
      Tasks: 68 (limit: 9572)
        CPU: 40ms
     CGroup: /system.slice/audirvanaStudio.service
             └─6448 /opt/audirvana/studio/audirvanaStudio`,
			expected: ServiceStatus{
				Loaded:  true,
				Enabled: true,
				Active:  true,
				Running: true,
				PID:     6448,
			},
		},
		{
			name: "inactive (dead)",
			output: `● audirvanaStudio.service - Run audirvanaStudio
     Loaded: loaded (/etc/systemd/system/audirvanaStudio.service; enabled; preset: enabled)
     Active: inactive (dead)`,
			expected: ServiceStatus{
				Loaded:  true,
				Enabled: true,
				Active:  false,
				Running: false,
			},
		},
		{
			name: "failed service",
			output: `● audirvanaStudio.service - Run audirvanaStudio
     Loaded: loaded (/etc/systemd/system/audirvanaStudio.service; enabled; preset: enabled)
     Active: failed (Result: exit-code) since Sun 2026-01-25 12:00:00 AEST; 1h ago
    Process: 1234 ExecStart=/opt/audirvana/studio/audirvanaStudio (code=exited, status=1/FAILURE)`,
			expected: ServiceStatus{
				Loaded:  true,
				Enabled: true,
				Active:  false,
				Running: false,
			},
		},
		{
			name: "disabled service",
			output: `● audirvanaStudio.service - Run audirvanaStudio
     Loaded: loaded (/etc/systemd/system/audirvanaStudio.service; disabled; preset: enabled)
     Active: inactive (dead)`,
			expected: ServiceStatus{
				Loaded:  true,
				Enabled: false,
				Active:  false,
				Running: false,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			status := ParseSystemctlStatus(tt.output)
			if status.Loaded != tt.expected.Loaded {
				t.Errorf("expected loaded=%v, got %v", tt.expected.Loaded, status.Loaded)
			}
			if status.Enabled != tt.expected.Enabled {
				t.Errorf("expected enabled=%v, got %v", tt.expected.Enabled, status.Enabled)
			}
			if status.Active != tt.expected.Active {
				t.Errorf("expected active=%v, got %v", tt.expected.Active, status.Active)
			}
			if status.Running != tt.expected.Running {
				t.Errorf("expected running=%v, got %v", tt.expected.Running, status.Running)
			}
			if status.PID != tt.expected.PID {
				t.Errorf("expected pid=%d, got %d", tt.expected.PID, status.PID)
			}
		})
	}
}

func TestExtractTxtValue(t *testing.T) {
	tests := []struct {
		name       string
		txtContent string
		key        string
		expected   string
	}{
		{
			name:       "extract protovers",
			txtContent: `"protovers=4.1.0" "osversion=Linux" "txtvers=1"`,
			key:        "protovers",
			expected:   "4.1.0",
		},
		{
			name:       "extract osversion",
			txtContent: `"protovers=4.1.0" "osversion=Linux" "txtvers=1"`,
			key:        "osversion",
			expected:   "Linux",
		},
		{
			name:       "key not found",
			txtContent: `"protovers=4.1.0" "osversion=Linux"`,
			key:        "missing",
			expected:   "",
		},
		{
			name:       "empty content",
			txtContent: "",
			key:        "protovers",
			expected:   "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractTxtValue(tt.txtContent, tt.key)
			if result != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, result)
			}
		})
	}
}
