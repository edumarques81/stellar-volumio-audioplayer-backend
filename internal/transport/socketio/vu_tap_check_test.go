package socketio

import (
	"strings"
	"testing"
)

// mpdConfWithDevice builds a minimal mpd.conf whose audio_output block carries
// the given device value, with the two traps the real Pi config contains: a
// top-level `audio_output_format` directive that a prefix test mistakes for a
// block opener, and a commented-out decoy block.
func mpdConfWithDevice(device string) string {
	return `audio_output_format "44100:16:2"

#audio_output {
#    type        "alsa"
#    device      "hw:9,0"
#}

audio_output {
    type        "alsa"
    name        "USB DAC"
    # NOT "hw:2,0" -- see deploy/README-alsa-tap.md
    device      "` + device + `"
    mixer_type  "none"
    dop         "no"
}
`
}

func TestMPDOutputDevice(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    string
	}{
		{
			name:    "tap PCM",
			content: mpdConfWithDevice("stellar_dac_tap"),
			want:    "stellar_dac_tap",
		},
		{
			name:    "raw hardware device",
			content: mpdConfWithDevice("hw:2,0"),
			want:    "hw:2,0",
		},
		{
			name: "a device outside any block is not ours",
			content: `device "hw:7,0"

audio_output {
    type        "alsa"
    device      "stellar_dac_tap"
}
`,
			want: "stellar_dac_tap",
		},
		{
			name: "commented device inside the block is skipped",
			content: `audio_output {
    type        "alsa"
#   device      "hw:9,0"
    device      "stellar_dac_tap"
}
`,
			want: "stellar_dac_tap",
		},
		{
			name: "block with no device",
			content: `audio_output {
    type        "alsa"
    mixer_type  "none"
}
`,
			want: "",
		},
		{
			name:    "no audio_output block at all",
			content: `audio_output_format "44100:16:2"`,
			want:    "",
		},
		{
			name:    "empty config",
			content: "",
			want:    "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := mpdOutputDevice(tt.content); got != tt.want {
				t.Errorf("mpdOutputDevice() = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestCheckBitPerfectVuTap is the guard that would have surfaced the 2026-10-16
// regression. The bit-perfect panel previously reported `hw:2,0` as "Direct
// hardware output (good)" while the LCD VU meter was dead, because nothing in
// the system knew the tap was supposed to be in the path.
func TestCheckBitPerfectVuTap(t *testing.T) {
	tests := []struct {
		name        string
		device      string
		alsa        string
		wantWarning bool
		wantSubstr  string
	}{
		{
			name:        "tap in use is reported as healthy",
			device:      "stellar_dac_tap",
			alsa:        piAsoundConf,
			wantWarning: false,
			wantSubstr:  "stellar_dac_tap",
		},
		{
			name:        "tap available but bypassed is a warning",
			device:      "hw:2,0",
			alsa:        piAsoundConf,
			wantWarning: true,
			wantSubstr:  "stellar_dac_tap",
		},
		{
			name:        "card with no tap defined is not flagged",
			device:      "hw:0,0",
			alsa:        piAsoundConf,
			wantWarning: false,
		},
		{
			name:        "no ALSA config means nothing to say",
			device:      "hw:2,0",
			alsa:        "",
			wantWarning: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CheckBitPerfectFromConfig(mpdConfWithDevice(tt.device), tt.alsa, "")

			warned := false
			for _, w := range got.Warnings {
				if strings.Contains(w, "VU meter") {
					warned = true
					if tt.wantSubstr != "" && !strings.Contains(w, tt.wantSubstr) {
						t.Errorf("warning %q does not name %q", w, tt.wantSubstr)
					}
				}
			}
			if warned != tt.wantWarning {
				t.Fatalf("VU-meter warning present = %v, want %v\nwarnings: %v",
					warned, tt.wantWarning, got.Warnings)
			}

			if !tt.wantWarning && tt.wantSubstr != "" {
				found := false
				for _, c := range got.Config {
					if strings.Contains(c, "VU meter") && strings.Contains(c, tt.wantSubstr) {
						found = true
					}
				}
				if !found {
					t.Errorf("expected a healthy VU-meter config line naming %q, got %v",
						tt.wantSubstr, got.Config)
				}
			}
		})
	}
}

// piAplayOutput is `aplay -l` on the Pi as of 2026-10-16.
const piAplayOutput = `**** List of PLAYBACK Hardware Devices ****
card 0: vc4hdmi0 [vc4-hdmi-0], device 0: MAI PCM i2s-hifi-0 [MAI PCM i2s-hifi-0]
  Subdevices: 1/1
  Subdevice #0: subdevice #0
card 1: vc4hdmi1 [vc4-hdmi-1], device 0: MAI PCM i2s-hifi-0 [MAI PCM i2s-hifi-0]
  Subdevices: 1/1
  Subdevice #0: subdevice #0
card 2: U20SU6 [USB Audio 2.0(SU-6)], device 0: USB Audio [USB Audio]
  Subdevices: 1/1
  Subdevice #0: subdevice #0
`

// TestResolveCurrentAudioOutput pins the read side end to end. The value this
// returns must be one of the option values GetPlaybackOptions builds from the
// same `aplay -l` output, or the Settings selector renders blank.
func TestResolveCurrentAudioOutput(t *testing.T) {
	tests := []struct {
		name   string
		device string
		alsa   string
		want   string
	}{
		{
			// The regression this fixes: the tap PCM used to be returned raw,
			// which matches no option, so the control went blank and the first
			// pick overwrote the tap.
			name:   "tap PCM reports the card it fronts",
			device: "stellar_dac_tap",
			alsa:   piAsoundConf,
			want:   "U20SU6",
		},
		{
			name:   "raw hardware device still reports its card",
			device: "hw:2,0",
			alsa:   piAsoundConf,
			want:   "U20SU6",
		},
		{
			name:   "HDMI card",
			device: "hw:0,0",
			alsa:   piAsoundConf,
			want:   "vc4hdmi0",
		},
		{
			// Without the ALSA config there is no way to map the name, so the
			// raw value is the honest answer -- and better than "".
			name:   "unmappable name falls back to the raw value",
			device: "stellar_dac_tap",
			alsa:   "",
			want:   "stellar_dac_tap",
		},
		{
			name:   "unknown PCM name falls back to the raw value",
			device: "mystery",
			alsa:   piAsoundConf,
			want:   "mystery",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := resolveCurrentAudioOutput(mpdConfWithDevice(tt.device), tt.alsa, piAplayOutput)
			if got != tt.want {
				t.Errorf("resolveCurrentAudioOutput() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestResolveCurrentAudioOutputNoDevice(t *testing.T) {
	if got := resolveCurrentAudioOutput("", piAsoundConf, piAplayOutput); got != "" {
		t.Errorf("got %q, want \"\" so the caller applies its own default", got)
	}
}

// TestCardNameFromAplay covers the parsing in isolation, including the header
// and Subdevice lines that surround the card rows.
func TestCardNameFromAplay(t *testing.T) {
	tests := []struct {
		cardNum string
		want    string
	}{
		{cardNum: "0", want: "vc4hdmi0"},
		{cardNum: "2", want: "U20SU6"},
		{cardNum: "9", want: ""},
		{cardNum: "", want: ""},
	}

	for _, tt := range tests {
		t.Run("card "+tt.cardNum, func(t *testing.T) {
			if got := cardNameFromAplay(piAplayOutput, tt.cardNum); got != tt.want {
				t.Errorf("cardNameFromAplay(%q) = %q, want %q", tt.cardNum, got, tt.want)
			}
		})
	}
}

// TestCheckBitPerfectOutputDevice covers Check 3's reporting of MPD's output
// device. Its old gates were substring tests over the whole file: `"hw:` is
// present in the Pi config's thirteen commented-out decoy blocks, so once the
// real device became `stellar_dac_tap` the check matched, failed the
// `HasPrefix("hw:")` test, and emitted nothing at all -- not even the
// "could not determine" warning it has for that case.
func TestCheckBitPerfectOutputDevice(t *testing.T) {
	tests := []struct {
		name       string
		content    string
		alsa       string
		wantSubstr string
		wantKind   string // "config", "issue", "warning"
	}{
		{
			name:       "tap PCM is reported, not swallowed",
			content:    mpdConfWithDevice("stellar_dac_tap"),
			alsa:       piAsoundConf,
			wantSubstr: "transparent ALSA tap",
			wantKind:   "config",
		},
		{
			// A named PCM the ALSA config does not define cannot be opened, so
			// MPD would fail to start. Saying nothing is the one wrong answer.
			name:       "named PCM absent from the ALSA config is a warning",
			content:    mpdConfWithDevice("stellar_dac_tap"),
			alsa:       "",
			wantSubstr: "not found in the ALSA config",
			wantKind:   "warning",
		},
		{
			name:       "plug PCM is a conversion layer",
			content:    mpdConfWithDevice("stellar_dac_plug"),
			alsa:       piAsoundConf,
			wantSubstr: "plug layer",
			wantKind:   "issue",
		},
		{
			// The retired meter PCM: bit-perfect transparency is not something
			// this check can assert for an arbitrary plugin type.
			name:       "unknown PCM type is reported as unverifiable",
			content:    mpdConfWithDevice("stellar_dac"),
			alsa:       piAsoundConf,
			wantSubstr: "not verifiable",
			wantKind:   "warning",
		},
		{
			name:       "raw hardware device",
			content:    mpdConfWithDevice("hw:2,0"),
			wantSubstr: "hw:2,0",
			wantKind:   "config",
		},
		{
			name:       "volumio device goes through a plug layer",
			content:    mpdConfWithDevice("volumio"),
			wantSubstr: "volumio",
			wantKind:   "issue",
		},
		{
			name: "no device at all is a warning",
			content: `audio_output {
    type        "alsa"
    mixer_type  "none"
}
`,
			wantSubstr: "Could not determine audio device",
			wantKind:   "warning",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CheckBitPerfectFromConfig(tt.content, tt.alsa, "")

			var pool []string
			switch tt.wantKind {
			case "config":
				pool = got.Config
			case "issue":
				pool = got.Issues
			case "warning":
				pool = got.Warnings
			}

			for _, s := range pool {
				if strings.Contains(s, tt.wantSubstr) {
					return
				}
			}
			t.Errorf("no %s line contains %q\nconfig: %v\nissues: %v\nwarnings: %v",
				tt.wantKind, tt.wantSubstr, got.Config, got.Issues, got.Warnings)
		})
	}
}
