package socketio

import (
	"os"
	"regexp"
	"strings"
)

// alsaConfigPath is the ALSA configuration these helpers read. A var rather
// than a const so tests can point at a fixture.
var alsaConfigPath = "/etc/asound.conf"

// alsaTapPCMType is the type name of our own extplug filter plugin
// (deploy/alsa-tap/). A PCM of this type is a transparent pass-through to the
// card that *also* copies the stream to the VU-meter FIFO, so it is the only
// named PCM this package will ever select on the user's behalf.
//
// Deliberately not `meter` and not `plug`, both of which also sit in front of
// the same card in /etc/asound.conf and are both wrong to choose:
//
//   - `meter` + the s16 scope aborts mpd mid-track on DSD
//     (alsa-lib discards snd_pcm_scope_enable's error, then trips
//     assert(s16->buf_areas) on the next accessor call).
//   - `plug` is a conversion layer, which is the opposite of bit-perfect.
//
// → deploy/README-alsa-tap.md
const alsaTapPCMType = "stellar_tap"

// alsaPCM is one `pcm.<name>` definition from an ALSA configuration file.
type alsaPCM struct {
	Name  string
	Type  string
	Slave string // raw slave.pcm value: either "hw:N,M" or another PCM's name
}

var (
	// alsaPCMOpenRe matches a `pcm.<name> {` opener. The literal dot is what
	// keeps it from also matching `pcm_scope.` and `pcm_scope_type.`, neither
	// of which is a PCM — treating them as one invents devices that cannot be
	// opened.
	alsaPCMOpenRe = regexp.MustCompile(`^pcm\.([A-Za-z0-9_]+)[ \t]*(\{)?[ \t]*(?:#.*)?$`)

	// alsaKeyValueRe matches an ALSA `key value` or `key.sub value` line,
	// quoted or not, with an optional trailing comment. MPD-style `key "value"`
	// and ALSA's bare `key value` both appear in practice.
	alsaKeyValueRe = regexp.MustCompile(`^([A-Za-z0-9_.]+)[ \t]+(?:"([^"]*)"|([^\s#]+))[ \t]*(?:#.*)?$`)

	// alsaHWDeviceRe matches a raw ALSA hardware device string, capturing the
	// card number. Accepts the `hw:N` and `hw:N,M` spellings; a card *name*
	// (`hw:U20SU6,0`) deliberately does not match, because this package only
	// ever needs to compare against aplay's numeric card indices.
	alsaHWDeviceRe = regexp.MustCompile(`^hw:([0-9]+)(?:,[0-9]+)?$`)
)

// parseAlsaPCMs extracts every `pcm.<name>` definition from an ALSA config.
// Blocks are shallow by design: only `type` and the slave PCM are read, which
// is all the output-device mapping needs.
func parseAlsaPCMs(content string) []alsaPCM {
	var out []alsaPCM

	lines := strings.Split(content, "\n")
	for i := 0; i < len(lines); i++ {
		trimmed := strings.TrimSpace(lines[i])
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}

		m := alsaPCMOpenRe.FindStringSubmatch(trimmed)
		if m == nil {
			continue
		}

		// The brace may sit on the following line, which ALSA also accepts.
		body := i + 1
		if m[2] == "" {
			j := nextSignificant(lines, i+1)
			if j == -1 || !strings.HasPrefix(strings.TrimSpace(lines[j]), "{") {
				continue
			}
			body = j + 1
		}

		pcm, next := parseAlsaPCMBody(lines, body)
		pcm.Name = m[1]
		out = append(out, pcm)
		i = next
	}

	return out
}

// parseAlsaPCMBody reads one block's `type` and slave PCM, returning the index
// of its closing brace. Nested blocks are skipped wholesale except `slave {`,
// whose `pcm` key is the alternative spelling of `slave.pcm`.
func parseAlsaPCMBody(lines []string, from int) (pcm alsaPCM, end int) {
	depth := 0
	inSlave := false

	for i := from; i < len(lines); i++ {
		trimmed := strings.TrimSpace(lines[i])
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}

		if strings.HasPrefix(trimmed, "}") {
			if depth == 0 {
				return pcm, i
			}
			depth--
			inSlave = false
			continue
		}

		if strings.HasSuffix(trimmed, "{") {
			depth++
			inSlave = depth == 1 && strings.HasPrefix(trimmed, "slave")
			continue
		}

		kv := alsaKeyValueRe.FindStringSubmatch(trimmed)
		if kv == nil {
			continue
		}
		key, value := kv[1], kv[2]
		if value == "" {
			value = kv[3]
		}

		switch {
		case depth == 0 && key == "type":
			pcm.Type = value
		case depth == 0 && key == "slave.pcm":
			pcm.Slave = value
		case inSlave && key == "pcm":
			pcm.Slave = value
		}
	}

	return pcm, len(lines) - 1
}

// alsaPCMCard resolves a device string to the ALSA card number it ends up at,
// following `slave.pcm` references by name. `name` may be a raw `hw:N,M` or the
// name of a PCM defined in pcms.
//
// The hop limit is not defensive dressing: a cyclic or self-referential config
// is invalid but entirely possible to write, and this runs inside a Socket.IO
// handler where a spin would wedge the event loop for every client.
func alsaPCMCard(pcms []alsaPCM, name string) (string, bool) {
	const maxHops = 8

	for hop := 0; hop < maxHops; hop++ {
		if name == "" {
			return "", false
		}
		if m := alsaHWDeviceRe.FindStringSubmatch(name); m != nil {
			return m[1], true
		}

		next := ""
		for _, p := range pcms {
			if p.Name == name {
				next = p.Slave
				break
			}
		}
		if next == "" {
			return "", false
		}
		name = next
	}

	return "", false
}

// preferredAlsaPCMForCard returns the name of the VU-meter tap PCM that fronts
// the given card, or "" when the card has none.
//
// "" is the important case, not an error case: it means the caller must write
// the raw `hw:N,0`. Naming a PCM whose plugin is not installed does not degrade
// to "no VU meter" — mpd fails to open the device, so the symptom is no sound
// at all.
func preferredAlsaPCMForCard(pcms []alsaPCM, cardNum string) string {
	if cardNum == "" {
		return ""
	}

	for _, p := range pcms {
		if p.Type != alsaTapPCMType {
			continue
		}
		if card, ok := alsaPCMCard(pcms, p.Name); ok && card == cardNum {
			return p.Name
		}
	}

	return ""
}

// resolveOutputDeviceValue returns the value an `audio_output` block's `device`
// setting should carry for the given card: the VU-meter tap when one fronts
// that card, and the raw hardware device otherwise.
//
// This exists because the audio-output selector's wire contract is a *card
// name*, which has no way to express "the tap in front of that card". Writing
// `hw:N,0` unconditionally — which is what this replaced — silently removes the
// tap from MPD's path and kills the VU meter, with no error anywhere.
func resolveOutputDeviceValue(cardNum, asoundConfig string) string {
	if pcm := preferredAlsaPCMForCard(parseAlsaPCMs(asoundConfig), cardNum); pcm != "" {
		return pcm
	}
	return "hw:" + cardNum + ",0"
}

// cardNumberForDeviceValue maps an `audio_output` device setting back to a card
// number, resolving named PCMs through the ALSA config.
//
// The read side needs this so a correctly-configured appliance reports a device
// that is actually present in the selector's own options list. Reporting the
// raw PCM name instead renders the control blank, with no placeholder and no
// warning, which is an open invitation to pick a card and overwrite the tap.
func cardNumberForDeviceValue(device, asoundConfig string) (string, bool) {
	if device == "" {
		return "", false
	}
	if m := alsaHWDeviceRe.FindStringSubmatch(device); m != nil {
		return m[1], true
	}
	return alsaPCMCard(parseAlsaPCMs(asoundConfig), device)
}

// readAlsaConfig returns the contents of alsaConfigPath, or "" when it cannot
// be read. An unreadable ALSA config is not fatal: every caller falls back to
// the raw card, which is the pre-tap behaviour.
func readAlsaConfig() string {
	data, err := os.ReadFile(alsaConfigPath)
	if err != nil {
		return ""
	}
	return string(data)
}

// mpdOutputDevice returns the `device` value from the first audio_output block
// of an mpd.conf, or "" when there is none.
//
// Scoped with the same block finder the DSD helpers use, rather than searching
// the whole file: the Pi's config carries thirteen commented-out `#audio_output
// {` decoys and a top-level `audio_output_format` directive, so a whole-file
// scan for `device` can return a value MPD never reads.
func mpdOutputDevice(mpdConfig string) string {
	lines := strings.Split(mpdConfig, "\n")

	open, closeIdx, ok := findAudioOutputBlock(lines)
	if !ok {
		return ""
	}

	for i := open + 1; i < closeIdx; i++ {
		trimmed := strings.TrimSpace(lines[i])
		if strings.HasPrefix(trimmed, "#") || !deviceKeyRe.MatchString(trimmed) {
			continue
		}
		if m := alsaKeyValueRe.FindStringSubmatch(trimmed); m != nil {
			if m[2] != "" {
				return m[2]
			}
			return m[3]
		}
	}

	return ""
}

// appendVuTapStatus reports whether MPD's output actually passes through the
// VU-meter tap, given the live mpd.conf and ALSA config.
//
// This check exists because its absence let a regression sit unnoticed: with
// `device "hw:2,0"` the bit-perfect panel reported "Direct hardware output:
// hw:2,0 (good)" — which is true, and bit-perfect, and silently means the LCD
// VU meter cannot move, because nothing is left writing the spectrum FIFO.
// Both needles park on the left end stop, which is indistinguishable from
// silence. Nothing else in the system notices.
func appendVuTapStatus(status *BitPerfectStatus, mpdConfig, alsaConfig string) {
	if alsaConfig == "" || mpdConfig == "" {
		return
	}

	device := mpdOutputDevice(mpdConfig)
	if device == "" {
		return
	}

	pcms := parseAlsaPCMs(alsaConfig)

	// Already on a tap: nothing to warn about.
	for _, p := range pcms {
		if p.Name == device && p.Type == alsaTapPCMType {
			status.Config = append(status.Config,
				"LCD VU meter: tapped via "+device+" (transparent pass-through)")
			return
		}
	}

	card, ok := cardNumberForDeviceValue(device, alsaConfig)
	if !ok {
		return
	}

	tap := preferredAlsaPCMForCard(pcms, card)
	if tap == "" {
		// No tap defined for this card, so bypassing one is not possible.
		return
	}

	status.Warnings = append(status.Warnings,
		"LCD VU meter: MPD output is \""+device+"\", which bypasses the ALSA tap - "+
			"the VU meter cannot move. Expected \""+tap+"\" (see deploy/README-alsa-tap.md)")
}

// cardNameFromAplay extracts the card name for a card number from `aplay -l`
// output. The name is what the audio-output selector uses as an option value,
// so it is also what the read side must report back.
func cardNameFromAplay(aplayOutput, cardNum string) string {
	if cardNum == "" {
		return ""
	}

	for _, line := range strings.Split(aplayOutput, "\n") {
		if !strings.HasPrefix(line, "card "+cardNum+":") {
			continue
		}
		parts := strings.SplitN(line, ":", 2)
		if len(parts) < 2 {
			continue
		}
		cardPart := strings.TrimSpace(parts[1])
		end := strings.Index(cardPart, " [")
		if end == -1 {
			end = strings.Index(cardPart, ",")
		}
		if end != -1 {
			return strings.TrimSpace(cardPart[:end])
		}
	}

	return ""
}

// resolveCurrentAudioOutput reports which card MPD is currently outputting to,
// named the way the selector's own options are named.
//
// The indirection through the ALSA config is the point. MPD's device may be a
// named PCM (`stellar_dac_tap`), which is not a card name and therefore matches
// no option: the Settings control renders blank, with no placeholder, which
// reads as "nothing configured" and invites a pick that overwrites the tap.
// Resolving the PCM back to its card makes the round trip lossless.
func resolveCurrentAudioOutput(mpdConfig, asoundConfig, aplayOutput string) string {
	device := mpdOutputDevice(mpdConfig)
	if device == "" {
		return ""
	}

	if card, ok := cardNumberForDeviceValue(device, asoundConfig); ok {
		if name := cardNameFromAplay(aplayOutput, card); name != "" {
			return name
		}
	}

	// Unresolvable: the raw value is at least truthful about what MPD reads.
	return device
}

// appendOutputDeviceStatus classifies MPD's configured output device for the
// bit-perfect panel.
//
// The device is read block-scoped rather than by substring, which is what the
// previous version did: `strings.Contains(mpdConfig, "\"hw:")` is satisfied by
// the thirteen commented-out decoy blocks in the Pi's mpd.conf, so once the
// real device became a named PCM the check matched on a decoy, failed its
// `hw:` prefix test, and reported nothing at all -- not even its own
// "could not determine" warning.
func appendOutputDeviceStatus(status *BitPerfectStatus, mpdConfig, alsaConfig string) {
	if mpdConfig == "" {
		return
	}

	device := mpdOutputDevice(mpdConfig)
	if device == "" {
		status.Warnings = append(status.Warnings, "MPD: Could not determine audio device")
		return
	}

	if alsaHWDeviceRe.MatchString(device) || strings.HasPrefix(device, "hw:") {
		status.Config = append(status.Config, "MPD: Direct hardware output: "+device+" (good)")
		return
	}

	if device == "volumio" {
		status.Issues = append(status.Issues, "MPD: Using 'volumio' device (goes through plug layer)")
		return
	}

	for _, p := range parseAlsaPCMs(alsaConfig) {
		if p.Name != device {
			continue
		}
		switch p.Type {
		case alsaTapPCMType:
			status.Config = append(status.Config,
				"MPD: Output via "+device+" -> "+p.Slave+" (transparent ALSA tap, good)")
		case "plug":
			status.Issues = append(status.Issues,
				"MPD: Output via "+device+" is a plug layer - audio may be converted")
		default:
			status.Warnings = append(status.Warnings,
				"MPD: Output device \""+device+"\" is an ALSA "+p.Type+
					" PCM - transparency is not verifiable from config")
		}
		return
	}

	status.Warnings = append(status.Warnings,
		"MPD: Output device \""+device+"\" is a named ALSA PCM not found in the ALSA config")
}
