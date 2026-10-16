package socketio

import "testing"

// piAsoundConf is the Pi's real /etc/asound.conf as of 2026-10-16. It carries
// three PCMs that all end up at the same card, which is the whole difficulty:
// only one of them is the right answer.
const piAsoundConf = `# Stellar: tap the DAC stream for the LCD VU meter WITHOUT adding a second MPD
# output. See deploy/alsa-scope/stellar_vu_scope.c for why this exists.
#
# NOTE: unlike PCM plugins, alsa-lib's meter does NOT derive a scope's .so name
# from its type — ` + "`pcm_scope_type.<name>.lib`" + ` must name it explicitly.

pcm_scope_type.stellar_vu {
    lib "/usr/lib/aarch64-linux-gnu/alsa-lib/libasound_module_scope_stellar_vu.so"
}

pcm_scope.stellar_vu {
    type stellar_vu
    fifo "/tmp/mpd_spectrum.fifo"
}

pcm.stellar_dac {
    type meter
    slave.pcm "hw:2,0"
    scopes.0 stellar_vu
}

# Fallback for senders that cannot match the DAC's native format. Same conversion
# layer shairport-sync used to get from ` + "`plughw:`" + `, but with the VU tap inserted.
# Only use this if a sender fails to open pcm.stellar_dac directly.
pcm.stellar_dac_plug {
    type plug
    slave.pcm "stellar_dac"
}

# stellar_tap: extplug pass-through that copies the stream to the VU FIFO.
# Unlike ` + "`type meter`" + ` + s16 scope, this sees every format the card accepts,
# including native DSD.  -> deploy/alsa-tap/
pcm.stellar_dac_tap {
    type  stellar_tap
    slave.pcm "hw:2,0"
    fifo  "/tmp/mpd_spectrum.fifo"
}
`

func TestParseAlsaPCMs(t *testing.T) {
	got := parseAlsaPCMs(piAsoundConf)

	want := []alsaPCM{
		{Name: "stellar_dac", Type: "meter", Slave: "hw:2,0"},
		{Name: "stellar_dac_plug", Type: "plug", Slave: "stellar_dac"},
		{Name: "stellar_dac_tap", Type: "stellar_tap", Slave: "hw:2,0"},
	}

	if len(got) != len(want) {
		t.Fatalf("parsed %d PCMs, want %d: %+v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("PCM %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestParseAlsaPCMsIgnoresNonPCMBlocks(t *testing.T) {
	// `pcm_scope` and `pcm_scope_type` are not PCMs. A prefix test on "pcm"
	// would swallow both and invent devices that cannot be opened.
	for _, p := range parseAlsaPCMs(piAsoundConf) {
		if p.Name == "stellar_vu" {
			t.Fatalf("pcm_scope block parsed as a PCM: %+v", p)
		}
	}
}

func TestParseAlsaPCMsVariants(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    []alsaPCM
	}{
		{
			name:    "empty",
			content: "",
			want:    nil,
		},
		{
			name: "nested slave block",
			content: `pcm.nested {
    type plug
    slave {
        pcm "hw:1,0"
    }
}`,
			want: []alsaPCM{{Name: "nested", Type: "plug", Slave: "hw:1,0"}},
		},
		{
			name: "unquoted slave value",
			content: `pcm.bare {
    type stellar_tap
    slave.pcm hw:3,0
}`,
			want: []alsaPCM{{Name: "bare", Type: "stellar_tap", Slave: "hw:3,0"}},
		},
		{
			name: "brace on the next line",
			content: `pcm.nextline
{
    type stellar_tap
    slave.pcm "hw:2,0"
}`,
			want: []alsaPCM{{Name: "nextline", Type: "stellar_tap", Slave: "hw:2,0"}},
		},
		{
			name: "commented-out block is not a PCM",
			content: `#pcm.decoy {
#    type stellar_tap
#    slave.pcm "hw:9,0"
#}
pcm.real {
    type stellar_tap
    slave.pcm "hw:2,0"
}`,
			want: []alsaPCM{{Name: "real", Type: "stellar_tap", Slave: "hw:2,0"}},
		},
		{
			name: "trailing comment on a value",
			content: `pcm.trailing {
    type stellar_tap   # our extplug
    slave.pcm "hw:2,0" # the DAC
}`,
			want: []alsaPCM{{Name: "trailing", Type: "stellar_tap", Slave: "hw:2,0"}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseAlsaPCMs(tt.content)
			if len(got) != len(tt.want) {
				t.Fatalf("parsed %d PCMs, want %d: %+v", len(got), len(tt.want), got)
			}
			for i := range tt.want {
				if got[i] != tt.want[i] {
					t.Errorf("PCM %d = %+v, want %+v", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestAlsaPCMCard(t *testing.T) {
	pcms := parseAlsaPCMs(piAsoundConf)

	tests := []struct {
		name     string
		pcmName  string
		wantCard string
		wantOK   bool
	}{
		{name: "direct hw slave", pcmName: "stellar_dac_tap", wantCard: "2", wantOK: true},
		{name: "meter also lands on card 2", pcmName: "stellar_dac", wantCard: "2", wantOK: true},
		// stellar_dac_plug -> stellar_dac -> hw:2,0. One indirection.
		{name: "follows a name chain", pcmName: "stellar_dac_plug", wantCard: "2", wantOK: true},
		{name: "bare hw value", pcmName: "hw:1,0", wantCard: "1", wantOK: true},
		{name: "unknown name", pcmName: "nope", wantOK: false},
		{name: "empty", pcmName: "", wantOK: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			card, ok := alsaPCMCard(pcms, tt.pcmName)
			if ok != tt.wantOK {
				t.Fatalf("ok = %v, want %v (card %q)", ok, tt.wantOK, card)
			}
			if ok && card != tt.wantCard {
				t.Errorf("card = %q, want %q", card, tt.wantCard)
			}
		})
	}
}

func TestAlsaPCMCardDoesNotLoopForever(t *testing.T) {
	// A self-referential or mutually-referential config is invalid, but it must
	// not hang the backend: this runs inside a Socket.IO handler.
	cyclic := `pcm.a {
    type plug
    slave.pcm "b"
}
pcm.b {
    type plug
    slave.pcm "a"
}`
	done := make(chan struct{})
	go func() {
		defer close(done)
		if card, ok := alsaPCMCard(parseAlsaPCMs(cyclic), "a"); ok {
			t.Errorf("cyclic chain resolved to %q, want no result", card)
		}
	}()
	<-done
}

func TestPreferredAlsaPCMForCard(t *testing.T) {
	pcms := parseAlsaPCMs(piAsoundConf)

	tests := []struct {
		name    string
		cardNum string
		want    string
	}{
		{
			// Three PCMs resolve to card 2. Only the extplug tap is a correct
			// answer: `meter` aborts mpd mid-track on DSD and `plug` is a
			// conversion layer, so neither may ever be auto-selected.
			name:    "picks the extplug tap over meter and plug",
			cardNum: "2",
			want:    "stellar_dac_tap",
		},
		{
			name:    "no tap for a card without one",
			cardNum: "0",
			want:    "",
		},
		{
			name:    "empty card number",
			cardNum: "",
			want:    "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := preferredAlsaPCMForCard(pcms, tt.cardNum); got != tt.want {
				t.Errorf("preferredAlsaPCMForCard(%q) = %q, want %q", tt.cardNum, got, tt.want)
			}
		})
	}
}

func TestPreferredAlsaPCMForCardNoTapDefined(t *testing.T) {
	// A Pi with the plugin uninstalled must fall through to the raw card rather
	// than naming a PCM that cannot be opened — a missing module means mpd
	// fails to open the device, i.e. no sound at all.
	plain := `pcm.something {
    type plug
    slave.pcm "hw:2,0"
}`
	if got := preferredAlsaPCMForCard(parseAlsaPCMs(plain), "2"); got != "" {
		t.Errorf("got %q, want \"\" so the caller writes hw:2,0", got)
	}
}

// TestResolveOutputDeviceValue pins the write-side decision that the VU meter
// depends on: selecting the DAC must install the tap, not the bare card.
func TestResolveOutputDeviceValue(t *testing.T) {
	tests := []struct {
		name    string
		cardNum string
		asound  string
		want    string
	}{
		{
			name:    "DAC resolves to the tap",
			cardNum: "2",
			asound:  piAsoundConf,
			want:    "stellar_dac_tap",
		},
		{
			name:    "HDMI has no tap and stays raw",
			cardNum: "0",
			asound:  piAsoundConf,
			want:    "hw:0,0",
		},
		{
			name:    "unreadable asound.conf falls back to the raw card",
			cardNum: "2",
			asound:  "",
			want:    "hw:2,0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := resolveOutputDeviceValue(tt.cardNum, tt.asound); got != tt.want {
				t.Errorf("resolveOutputDeviceValue(%q) = %q, want %q", tt.cardNum, got, tt.want)
			}
		})
	}
}

// TestCardNumberForDeviceValue pins the read side. Without it the Settings
// selector reports a value absent from its own options list, renders blank, and
// invites the user to pick a card — which is what overwrites the tap.
func TestCardNumberForDeviceValue(t *testing.T) {
	tests := []struct {
		name     string
		device   string
		asound   string
		wantCard string
		wantOK   bool
	}{
		{name: "hw value", device: "hw:2,0", asound: piAsoundConf, wantCard: "2", wantOK: true},
		{name: "named tap maps back to its card", device: "stellar_dac_tap", asound: piAsoundConf, wantCard: "2", wantOK: true},
		{name: "named meter maps back too", device: "stellar_dac", asound: piAsoundConf, wantCard: "2", wantOK: true},
		{name: "unknown name", device: "mystery", asound: piAsoundConf, wantOK: false},
		{name: "empty device", device: "", asound: piAsoundConf, wantOK: false},
		{name: "no asound config", device: "stellar_dac_tap", asound: "", wantOK: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			card, ok := cardNumberForDeviceValue(tt.device, tt.asound)
			if ok != tt.wantOK {
				t.Fatalf("ok = %v, want %v (card %q)", ok, tt.wantOK, card)
			}
			if ok && card != tt.wantCard {
				t.Errorf("card = %q, want %q", card, tt.wantCard)
			}
		})
	}
}
