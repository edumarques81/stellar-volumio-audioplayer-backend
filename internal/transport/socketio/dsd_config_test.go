package socketio

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// confWithoutDop mirrors the shape of the Pi's real /etc/mpd.conf: native DSD is
// implicit so there is no dop line at all, `audio_output_format` sits at top
// level, and the input/decoder blocks come BEFORE audio_output. That ordering is
// the point — with the blocks the other way round, a mis-detected block opener
// still happens to land the insert in the right place, and the bug hides.
const confWithoutDop = `music_directory     "/var/lib/mpd/music"
playlist_directory  "/var/lib/mpd/playlists"

audio_output_format "44100:16:2"

input {
    plugin "curl"
}

decoder {
    plugin                  "hybrid_dsd"
    enabled                 "no"
}

audio_output {
    type        "alsa"
    name        "USB DAC"
    # a comment inside the block
    device      "hw:2,0"
    mixer_type  "none"
}
`

// confTwoOutputs adds the disabled second output the repo's reference config
// carries. Only the first block may ever be touched.
const confTwoOutputs = confWithoutDop + `
audio_output {
    type        "fifo"
    name        "Spectrum"
    path        "/tmp/mpd_spectrum.fifo"
    enabled     "no"
}
`

func confWithDop(line string) string {
	out := strings.Replace(confWithoutDop, `    mixer_type  "none"`,
		"    mixer_type  \"none\"\n"+line, 1)
	if out == confWithoutDop {
		panic("fixture anchor not found")
	}
	return out
}

func TestParseDsdMode(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    string
	}{
		{"absent entirely", confWithoutDop, "native"},
		{"empty config", "", "native"},
		{"single space yes", confWithDop(`    dop "yes"`), "dop"},
		{"wide padding yes", confWithDop(`    dop             "yes"`), "dop"},
		{"project padding yes", confWithDop(`    dop         "yes"`), "dop"},
		{"tab separated yes", confWithDop("    dop\t\"yes\""), "dop"},
		{"trailing comment yes", confWithDop(`    dop         "yes"   # test`), "dop"},
		{"no indent yes", confWithDop(`dop "yes"`), "dop"},
		{"unquoted yes", confWithDop(`    dop yes`), "dop"},
		{"explicit no", confWithDop(`    dop         "no"`), "native"},
		{"unquoted no", confWithDop(`    dop no`), "native"},
		{"commented out", confWithDop(`    # dop         "yes"`), "native"},
		{"commented with no space", confWithDop(`    #dop "yes"`), "native"},
		{"crlf", strings.ReplaceAll(confWithDop(`    dop "yes"`), "\n", "\r\n"), "dop"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := parseDsdMode(tt.content); got != tt.want {
				t.Errorf("parseDsdMode() = %q, want %q", got, tt.want)
			}
		})
	}
}

// A dop line outside the audio_output block is not an MPD setting at all, and
// must not be read as one.
func TestParseDsdMode_IgnoresDopOutsideAudioOutput(t *testing.T) {
	content := strings.Replace(confWithoutDop, `    plugin "curl"`,
		"    plugin \"curl\"\n    dop \"yes\"", 1)

	if got := parseDsdMode(content); got != "native" {
		t.Errorf("parseDsdMode() = %q, want native — a dop line in the input block is not the DSD mode", got)
	}
}

func TestApplyDsdMode_ReplacesExistingLine(t *testing.T) {
	tests := []struct {
		name     string
		existing string
		mode     string
		want     string
	}{
		{"single space yes to native", `    dop "yes"`, "native", "native"},
		{"wide padding yes to native", `    dop             "yes"`, "native", "native"},
		{"project padding yes to native", `    dop         "yes"`, "native", "native"},
		{"tab yes to native", "    dop\t\"yes\"", "native", "native"},
		{"unquoted yes to native", `    dop yes`, "native", "native"},
		{"no to dop", `    dop         "no"`, "dop", "dop"},
		{"yes to dop is idempotent", `    dop         "yes"`, "dop", "dop"},
		{"no to native is idempotent", `    dop         "no"`, "native", "native"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := applyDsdMode(confWithDop(tt.existing), tt.mode)
			if err != nil {
				t.Fatalf("applyDsdMode() error = %v", err)
			}
			if mode := parseDsdMode(got); mode != tt.want {
				t.Errorf("after apply, parseDsdMode() = %q, want %q\n%s", mode, tt.want, got)
			}
			if n := strings.Count(got, "dop"); n != 1 {
				t.Errorf("expected exactly one dop occurrence, got %d\n%s", n, got)
			}
		})
	}
}

// The rewritten value must not keep a comment that described the old value.
func TestApplyDsdMode_DropsStaleTrailingComment(t *testing.T) {
	content := confWithDop(`    dop         "yes"        # TEMPORARY - DoP test`)

	got, err := applyDsdMode(content, "native")
	if err != nil {
		t.Fatalf("applyDsdMode() error = %v", err)
	}
	if strings.Contains(got, "TEMPORARY") {
		t.Errorf("stale comment survived the value rewrite\n%s", got)
	}
	if parseDsdMode(got) != "native" {
		t.Errorf("expected native\n%s", got)
	}
}

// The regression this whole fix exists for: with no dop line present, the old
// code returned "Could not find dop setting in MPD config" and wrote nothing.
func TestApplyDsdMode_InsertsWhenAbsent(t *testing.T) {
	for _, mode := range []string{"dop", "native"} {
		t.Run(mode, func(t *testing.T) {
			got, err := applyDsdMode(confWithoutDop, mode)
			if err != nil {
				t.Fatalf("applyDsdMode() error = %v", err)
			}
			if parsed := parseDsdMode(got); parsed != mode {
				t.Errorf("parseDsdMode() = %q, want %q\n%s", parsed, mode, got)
			}
		})
	}
}

func TestApplyDsdMode_InsertsInsideAudioOutputBlock(t *testing.T) {
	got, err := applyDsdMode(confWithoutDop, "dop")
	if err != nil {
		t.Fatalf("applyDsdMode() error = %v", err)
	}

	lines := strings.Split(got, "\n")
	open, closeIdx, ok := findAudioOutputBlock(lines)
	if !ok {
		t.Fatal("audio_output block missing from result")
	}

	found := false
	for i := open + 1; i < closeIdx; i++ {
		if strings.Contains(lines[i], "dop") {
			found = true
		}
	}
	if !found {
		t.Errorf("dop line landed outside the audio_output block:\n%s", got)
	}
}

// `audio_output_format` is a real top-level MPD directive. A prefix test treats
// it as a block opener, and the insert then lands in whichever block closes
// next — on the Pi, `input {`, which makes mpd.conf fatally invalid.
func TestApplyDsdMode_NotFooledByAudioOutputFormat(t *testing.T) {
	got, err := applyDsdMode(confWithoutDop, "dop")
	if err != nil {
		t.Fatalf("applyDsdMode() error = %v", err)
	}

	for _, block := range []string{"input {", "decoder {"} {
		start := strings.Index(got, block)
		if start == -1 {
			t.Fatalf("%q missing from result", block)
		}
		end := strings.Index(got[start:], "\n}")
		if end == -1 {
			t.Fatalf("%q has no closing brace", block)
		}
		if strings.Contains(got[start:start+end], "dop") {
			t.Errorf("dop line leaked into %q:\n%s", block, got)
		}
	}
}

// Only the first audio_output block is ours to edit.
func TestApplyDsdMode_LeavesSecondAudioOutputAlone(t *testing.T) {
	got, err := applyDsdMode(confTwoOutputs, "dop")
	if err != nil {
		t.Fatalf("applyDsdMode() error = %v", err)
	}

	if n := strings.Count(got, "dop"); n != 1 {
		t.Errorf("expected exactly one dop line, got %d\n%s", n, got)
	}
	fifo := strings.Index(got, `"fifo"`)
	if fifo == -1 {
		t.Fatal("second audio_output block missing from result")
	}
	if strings.Contains(got[fifo:], "dop") {
		t.Errorf("dop line leaked into the second audio_output block:\n%s", got)
	}
}

func TestApplyDsdMode_PreservesBitPerfectSettings(t *testing.T) {
	got, err := applyDsdMode(confWithoutDop, "dop")
	if err != nil {
		t.Fatalf("applyDsdMode() error = %v", err)
	}

	// These are load-bearing for bit-perfect playback; a config rewrite must
	// never disturb them.
	for _, must := range []string{
		`mixer_type  "none"`,
		`device      "hw:2,0"`,
		`music_directory     "/var/lib/mpd/music"`,
		`plugin                  "hybrid_dsd"`,
		`audio_output_format "44100:16:2"`,
		"# a comment inside the block",
	} {
		if !strings.Contains(got, must) {
			t.Errorf("rewrite dropped %q\n%s", must, got)
		}
	}

	if n := strings.Count(got, "audio_output {"); n != 1 {
		t.Errorf("expected exactly one audio_output block, got %d\n%s", n, got)
	}
}

// The inserted line should be indistinguishable from one that was always there.
func TestApplyDsdMode_InsertMatchesBlockFormatting(t *testing.T) {
	got, err := applyDsdMode(confWithoutDop, "dop")
	if err != nil {
		t.Fatalf("applyDsdMode() error = %v", err)
	}

	if !strings.Contains(got, `    dop         "yes"`) {
		t.Errorf("inserted line does not match the block's indentation/alignment\n%s", got)
	}
}

func TestApplyDsdMode_PreservesCRLF(t *testing.T) {
	content := strings.ReplaceAll(confWithoutDop, "\n", "\r\n")

	got, err := applyDsdMode(content, "dop")
	if err != nil {
		t.Fatalf("applyDsdMode() error = %v", err)
	}
	if parseDsdMode(got) != "dop" {
		t.Errorf("expected dop\n%q", got)
	}
	if strings.Contains(strings.ReplaceAll(got, "\r\n", ""), "\n") {
		t.Errorf("rewrite introduced a bare LF into a CRLF file\n%q", got)
	}
}

func TestApplyDsdMode_RoundTrip(t *testing.T) {
	toDop, err := applyDsdMode(confWithoutDop, "dop")
	if err != nil {
		t.Fatalf("to dop: %v", err)
	}
	if parseDsdMode(toDop) != "dop" {
		t.Fatalf("expected dop after first apply\n%s", toDop)
	}

	backToNative, err := applyDsdMode(toDop, "native")
	if err != nil {
		t.Fatalf("back to native: %v", err)
	}
	if parseDsdMode(backToNative) != "native" {
		t.Errorf("expected native after round trip\n%s", backToNative)
	}
	if n := strings.Count(backToNative, "dop"); n != 1 {
		t.Errorf("round trip should leave exactly one dop line, got %d\n%s", n, backToNative)
	}
}

func TestApplyDsdMode_InvalidMode(t *testing.T) {
	for _, mode := range []string{"", "DOP", "pcm", "yes"} {
		t.Run(mode, func(t *testing.T) {
			if _, err := applyDsdMode(confWithoutDop, mode); !errors.Is(err, errInvalidDsdMode) {
				t.Errorf("applyDsdMode(%q) error = %v, want errInvalidDsdMode", mode, err)
			}
		})
	}
}

func TestApplyDsdMode_NoAudioOutputBlock(t *testing.T) {
	const noBlock = `music_directory "/var/lib/mpd/music"
audio_output_format "44100:16:2"
decoder {
    plugin "hybrid_dsd"
}
`
	if _, err := applyDsdMode(noBlock, "dop"); !errors.Is(err, errNoAudioOutput) {
		t.Errorf("error = %v, want errNoAudioOutput", err)
	}
}

// The old code only matched a dop line when it used one of four exact spellings.
// Pin that the current implementation is whitespace-agnostic so the bug cannot
// silently return.
func TestApplyDsdMode_WhitespaceAgnostic(t *testing.T) {
	spellings := []string{
		`    dop "no"`,
		`    dop  "no"`,
		`    dop   "no"`,
		`    dop         "no"`,
		`    dop             "no"`,
		"    dop\t\"no\"",
		"\tdop\t\t\"no\"",
	}

	for _, s := range spellings {
		t.Run(strings.TrimSpace(s), func(t *testing.T) {
			got, err := applyDsdMode(confWithDop(s), "dop")
			if err != nil {
				t.Fatalf("applyDsdMode() error = %v", err)
			}
			if parseDsdMode(got) != "dop" {
				t.Errorf("spelling %q was not recognised\n%s", s, got)
			}
			if n := strings.Count(got, "dop"); n != 1 {
				t.Errorf("spelling %q produced %d dop lines\n%s", s, n, got)
			}
		})
	}
}

func TestFindAudioOutputBlock_BraceOnNextLine(t *testing.T) {
	const content = `audio_output_format "44100:16:2"
audio_output
{
    device "hw:2,0"
}
`
	got, err := applyDsdMode(content, "dop")
	if err != nil {
		t.Fatalf("applyDsdMode() error = %v", err)
	}
	if parseDsdMode(got) != "dop" {
		t.Errorf("expected dop\n%s", got)
	}
}

// MPD treats a duplicated block parameter as a hard config error, so a rewrite
// must collapse them rather than faithfully preserving a broken file.
func TestApplyDsdMode_DropsDuplicateDopLines(t *testing.T) {
	content := confWithDop("    dop         \"yes\"\n    dop         \"no\"")

	got, err := applyDsdMode(content, "dop")
	if err != nil {
		t.Fatalf("applyDsdMode() error = %v", err)
	}
	if n := strings.Count(got, "dop"); n != 1 {
		t.Errorf("expected the duplicate to be dropped, got %d dop lines\n%s", n, got)
	}
	if parseDsdMode(got) != "dop" {
		t.Errorf("expected dop\n%s", got)
	}
}

// The device rewrite in SetPlaybackSettings keys off this; a bare prefix test
// would also match a longer setting that merely starts with "device".
func TestDeviceKeyRe(t *testing.T) {
	tests := []struct {
		line string
		want bool
	}{
		{`device      "hw:2,0"`, true},
		{"device\t\"hw:2,0\"", true},
		{`device_other "x"`, false},
		{`devices "x"`, false},
		{`device`, false},
	}

	for _, tt := range tests {
		t.Run(tt.line, func(t *testing.T) {
			if got := deviceKeyRe.MatchString(tt.line); got != tt.want {
				t.Errorf("deviceKeyRe.MatchString(%q) = %v, want %v", tt.line, got, tt.want)
			}
		})
	}
}

// --- full read/write path, exercised against a temp file ---

// withTempMPDConfig points the config helpers at a temp file and a plain
// (unprivileged) writer for the duration of the test.
//
// These are package vars, so no test in this package may call t.Parallel() —
// two tests swapping them concurrently would see each other's config.
func withTempMPDConfig(t *testing.T, content string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "mpd.conf")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("seed config: %v", err)
	}

	origPath, origWriter, origRestart := mpdConfigPath, writeMPDConfig, restartMPD
	t.Cleanup(func() {
		mpdConfigPath, writeMPDConfig, restartMPD = origPath, origWriter, origRestart
	})

	mpdConfigPath = path
	writeMPDConfig = func(c string) error { return os.WriteFile(path, []byte(c), 0o600) }
	restartMPD = func() error { return nil }

	return path
}

func TestSetDsdMode_WritesAndReportsSuccess(t *testing.T) {
	path := withTempMPDConfig(t, confWithoutDop)

	if got := GetDsdMode(); got.Mode != "native" || !got.Success {
		t.Fatalf("GetDsdMode() = %+v, want native/success", got)
	}

	res := SetDsdMode("dop")
	if !res.Success || res.Mode != "dop" {
		t.Fatalf("SetDsdMode(dop) = %+v, want dop/success", res)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if parseDsdMode(string(data)) != "dop" {
		t.Errorf("file was not updated:\n%s", data)
	}
	if got := GetDsdMode(); got.Mode != "dop" {
		t.Errorf("GetDsdMode() = %+v, want dop", got)
	}
}

// The contract the frontend depends on: a failed switch never reports the
// requested mode, so the radio cannot show a change that did not happen.
func TestSetDsdMode_ReportsEffectiveModeOnFailure(t *testing.T) {
	tests := []struct {
		name  string
		setup func(t *testing.T)
	}{
		{
			name: "write fails",
			setup: func(t *testing.T) {
				orig := writeMPDConfig
				t.Cleanup(func() { writeMPDConfig = orig })
				writeMPDConfig = func(string) error { return errors.New("permission denied") }
			},
		},
		{
			name: "restart fails",
			setup: func(t *testing.T) {
				orig := restartMPD
				t.Cleanup(func() { restartMPD = orig })
				restartMPD = func() error { return errors.New("unit failed") }
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			withTempMPDConfig(t, confWithoutDop)
			tt.setup(t)

			res := SetDsdMode("dop")
			if res.Success {
				t.Fatalf("SetDsdMode() = %+v, want failure", res)
			}
			if res.Mode != "native" {
				t.Errorf("Mode = %q, want native — the mode still in effect", res.Mode)
			}
			if res.Error == "" {
				t.Error("expected an error message for the client to surface")
			}
		})
	}
}

func TestSetDsdMode_InvalidModeLeavesFileUntouched(t *testing.T) {
	path := withTempMPDConfig(t, confWithoutDop)

	res := SetDsdMode("pcm")
	if res.Success {
		t.Fatalf("SetDsdMode(pcm) = %+v, want failure", res)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if string(data) != confWithoutDop {
		t.Errorf("invalid mode modified the config:\n%s", data)
	}
}

func TestGetDsdMode_MissingFile(t *testing.T) {
	withTempMPDConfig(t, confWithoutDop)
	mpdConfigPath = filepath.Join(t.TempDir(), "does-not-exist.conf")

	res := GetDsdMode()
	if res.Success {
		t.Errorf("GetDsdMode() = %+v, want failure", res)
	}
	if res.Mode != "native" {
		t.Errorf("Mode = %q, want the safe default native", res.Mode)
	}
}

func TestWriteMPDConfigWithSudo_RefusesUnsafePath(t *testing.T) {
	orig := mpdConfigPath
	t.Cleanup(func() { mpdConfigPath = orig })

	mpdConfigPath = "/etc/mpd.conf'; rm -rf /"
	if err := writeMPDConfigWithSudo("anything"); err == nil {
		t.Error("expected a refusal for a path that breaks out of the shell quoting")
	}
}
