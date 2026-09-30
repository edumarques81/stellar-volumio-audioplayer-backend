package socketio

import (
	"errors"
	"os"
	"regexp"
	"strings"

	"github.com/rs/zerolog/log"
)

// Errors returned by the DSD config helpers.
var (
	errInvalidDsdMode = errors.New("invalid mode: must be 'native' or 'dop'")
	errNoAudioOutput  = errors.New("could not find audio_output block in MPD config")
	errReadMPDConfig  = errors.New("failed to read MPD config")
)

// mpdConfigIndentPad is the column existing mpd.conf settings align their values
// to, e.g. `device      "hw:2,0"`. Used only when the block has no sibling
// setting to copy the alignment from.
const mpdConfigIndentPad = 12

// dopLineRe matches an active (non-commented) dop setting, capturing the leading
// indentation and the key/value separator so a rewrite preserves the file's
// formatting. Deliberately permissive about both whitespace and quoting: the
// previous implementation compared against four hardcoded spellings, so any
// other spacing — including the complete absence of the key, which is how the
// Pi ships — silently failed. MPD's own tokeniser accepts an unquoted value, so
// `dop yes` has to be recognised too or it would be duplicated rather than
// rewritten. A trailing comment is dropped on rewrite: it describes the value
// being replaced, so keeping it would be actively misleading.
var dopLineRe = regexp.MustCompile(`^([ \t]*)dop([ \t]+)(?:"([^"]*)"|([^\s#]+))[ \t]*(?:#.*)?$`)

// audioOutputOpenRe matches the opening line of an audio_output block. The brace
// is what distinguishes it from `audio_output_format`, a real top-level MPD
// directive that a plain prefix test would mistake for a block — and mistaking
// it flips block tracking on at top level, so the dop line lands inside whatever
// block closes next. On the Pi that is `input {` or `decoder {`, both of which
// make mpd.conf fatally invalid.
var audioOutputOpenRe = regexp.MustCompile(`^audio_output[ \t]*\{[ \t]*(?:#.*)?$`)

// audioOutputBareRe matches an audio_output opener whose brace is on the next
// line, which MPD also accepts.
var audioOutputBareRe = regexp.MustCompile(`^audio_output[ \t]*(?:#.*)?$`)

// parseDsdMode reports the DSD playback mode a given mpd.conf expresses.
// An absent or "no" dop setting means native DSD, which is MPD's default.
func parseDsdMode(content string) string {
	lines := strings.Split(content, "\n")

	open, closeIdx, ok := findAudioOutputBlock(lines)
	if !ok {
		return "native"
	}

	for i := open + 1; i < closeIdx; i++ {
		if m := dopLineRe.FindStringSubmatch(strings.TrimRight(lines[i], "\r")); m != nil {
			if dopValue(m) == "yes" {
				return "dop"
			}
			return "native"
		}
	}

	return "native"
}

// dopValue picks the quoted or unquoted capture out of a dopLineRe match.
func dopValue(m []string) string {
	if m[3] != "" {
		return m[3]
	}
	return m[4]
}

// applyDsdMode returns content with the DSD playback mode set to mode,
// rewriting an existing dop setting inside the first audio_output block or
// inserting one there when none is present. It is idempotent, and never touches
// anything outside that block.
func applyDsdMode(content, mode string) (string, error) {
	value, err := dopValueForMode(mode)
	if err != nil {
		return "", err
	}

	lines := strings.Split(content, "\n")

	open, closeIdx, ok := findAudioOutputBlock(lines)
	if !ok {
		return "", errNoAudioOutput
	}

	rewritten := false
	kept := make([]string, 0, len(lines)+1)
	kept = append(kept, lines[:open+1]...)

	for i := open + 1; i < closeIdx; i++ {
		bare, cr := splitCR(lines[i])
		m := dopLineRe.FindStringSubmatch(bare)
		if m == nil {
			kept = append(kept, lines[i])
			continue
		}
		if rewritten {
			// A second dop line in one block is a hard MPD config error, so
			// drop it rather than faithfully preserving a broken file.
			continue
		}
		kept = append(kept, m[1]+"dop"+m[2]+`"`+value+`"`+cr)
		rewritten = true
	}

	if rewritten {
		kept = append(kept, lines[closeIdx:]...)
		return strings.Join(kept, "\n"), nil
	}

	indent, pad := blockAlignment(lines, open, closeIdx)
	_, cr := splitCR(lines[closeIdx])

	out := make([]string, 0, len(lines)+1)
	out = append(out, lines[:closeIdx]...)
	out = append(out, indent+pad+`"`+value+`"`+cr)
	out = append(out, lines[closeIdx:]...)

	return strings.Join(out, "\n"), nil
}

// dopValueForMode maps a mode name onto the dop setting's value.
func dopValueForMode(mode string) (string, error) {
	switch mode {
	case "dop":
		return "yes", nil
	case "native":
		return "no", nil
	default:
		return "", errInvalidDsdMode
	}
}

// findAudioOutputBlock locates the first audio_output block, returning the index
// of its opening line and of its closing brace. Only the first block is ever
// touched: the repo's reference config carries a second, disabled one, and an
// edit that swept every block would be exactly the "never add a second
// audio_output" rule broken from the other end.
func findAudioOutputBlock(lines []string) (open, closeIdx int, ok bool) {
	for i := 0; i < len(lines); i++ {
		trimmed := strings.TrimSpace(lines[i])
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}

		start := -1
		switch {
		case audioOutputOpenRe.MatchString(trimmed):
			start = i
		case audioOutputBareRe.MatchString(trimmed):
			// Brace on a following line; anything else means this was not a
			// block opener after all.
			if j := nextSignificant(lines, i+1); j != -1 && strings.HasPrefix(strings.TrimSpace(lines[j]), "{") {
				start = j
			}
		}
		if start == -1 {
			continue
		}

		for j := start + 1; j < len(lines); j++ {
			if strings.HasPrefix(strings.TrimSpace(lines[j]), "}") {
				return start, j, true
			}
		}
		return 0, 0, false
	}

	return 0, 0, false
}

// nextSignificant returns the index of the next non-blank, non-comment line.
func nextSignificant(lines []string, from int) int {
	for i := from; i < len(lines); i++ {
		trimmed := strings.TrimSpace(lines[i])
		if trimmed != "" && !strings.HasPrefix(trimmed, "#") {
			return i
		}
	}
	return -1
}

// blockAlignment derives the indentation and key padding an inserted setting
// should use from the block's existing settings, so the result looks like it was
// always there. Falls back to the project's own four-space / column-12 style.
func blockAlignment(lines []string, open, closeIdx int) (indent, paddedKey string) {
	indent = "    "
	column := mpdConfigIndentPad

	for i := open + 1; i < closeIdx; i++ {
		bare, _ := splitCR(lines[i])
		trimmed := strings.TrimSpace(bare)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}

		if n := len(bare) - len(strings.TrimLeft(bare, " \t")); n > 0 {
			indent = bare[:n]
		}
		if key := strings.IndexAny(trimmed, " \t"); key > 0 {
			rest := trimmed[key:]
			if gap := len(rest) - len(strings.TrimLeft(rest, " \t")); gap > 0 {
				column = key + gap
			}
		}
	}

	if pad := column - len("dop"); pad > 0 {
		return indent, "dop" + strings.Repeat(" ", pad)
	}
	return indent, "dop "
}

// splitCR separates a trailing carriage return so a CRLF config survives a
// rewrite with its line endings intact.
func splitCR(line string) (bare, cr string) {
	if strings.HasSuffix(line, "\r") {
		return line[:len(line)-1], "\r"
	}
	return line, ""
}

// GetDsdMode returns the current DSD playback mode from MPD config.
// No lock: writeMPDConfig renames the new file into place, so a read sees one
// whole version of the config or the other, never a partial one.
func GetDsdMode() DsdModeResponse {
	data, err := os.ReadFile(mpdConfigPath)
	if err != nil {
		log.Error().Err(err).Msg("Failed to read MPD config")
		return DsdModeResponse{Mode: "native", Success: false, Error: errReadMPDConfig.Error()}
	}

	return DsdModeResponse{Mode: parseDsdMode(string(data)), Success: true}
}

// SetDsdMode sets the DSD playback mode in MPD config and restarts MPD.
// On failure the returned Mode reports the mode still in effect, never the
// requested one, so a client cannot show a switch that did not happen.
func SetDsdMode(mode string) DsdModeResponse {
	mpdConfigMu.Lock()
	defer mpdConfigMu.Unlock()

	data, err := os.ReadFile(mpdConfigPath)
	if err != nil {
		log.Error().Err(err).Msg("Failed to read MPD config")
		return DsdModeResponse{Mode: "native", Success: false, Error: errReadMPDConfig.Error()}
	}

	current := parseDsdMode(string(data))
	fail := func(msg string) DsdModeResponse {
		return DsdModeResponse{Mode: current, Success: false, Error: msg}
	}

	newContent, err := applyDsdMode(string(data), mode)
	if err != nil {
		return fail(err.Error())
	}

	if err := writeMPDConfig(newContent); err != nil {
		log.Error().Err(err).Msg("Failed to write MPD config")
		return fail("Failed to write MPD config: " + err.Error())
	}

	if err := restartMPD(); err != nil {
		// The file now says the new mode but MPD is still running the old one,
		// so the mode in effect is still the old one.
		log.Error().Err(err).Msg("Failed to restart MPD")
		return fail("Config updated but MPD failed to restart, still playing " +
			current + ": " + err.Error())
	}

	log.Info().Str("mode", mode).Msg("DSD mode changed successfully")
	return DsdModeResponse{Mode: mode, Success: true}
}
