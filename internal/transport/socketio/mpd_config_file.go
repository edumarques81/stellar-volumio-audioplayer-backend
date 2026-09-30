package socketio

import (
	"bytes"
	"errors"
	"os/exec"
	"strings"
	"sync"
)

// mpdConfigPath is the MPD configuration file these helpers read and rewrite.
// A var rather than a const so tests can point the whole read-modify-write path
// at a temporary file.
var mpdConfigPath = "/etc/mpd.conf"

// mpdConfigMu serialises read-modify-write cycles over mpdConfigPath. Every
// caller that rewrites the file reads it first, and three clients (LCD kiosk,
// iOS, Volumio Connect) can issue settings changes concurrently — without this
// a DSD toggle and a mixer change race, and the loser's edit is silently lost.
//
// Read-only helpers deliberately do not take it: writeMPDConfig installs the new
// contents with an atomic rename, so a reader always sees one whole version of
// the file, never a torn one.
var mpdConfigMu sync.Mutex

// writeMPDConfig replaces the MPD config, keeping the previous contents in a
// rolling .bak and, in a write-once .orig, the config as it stood before
// Stellar's first write on this machine. It is a var so tests can substitute a
// non-privileged writer.
//
// The old implementation piped straight into `sudo tee`, which truncates before
// writing: any failure mid-write left the file that the whole bit-perfect chain
// depends on damaged. Staging a sibling file and renaming it keeps the original
// intact until the new one is complete and on disk.
var writeMPDConfig = writeMPDConfigWithSudo

// restartMPD applies a rewritten config. A var for the same reason.
var restartMPD = func() error {
	return exec.Command("sudo", "systemctl", "restart", "mpd").Run()
}

// mpdConfigWriteScript stages the new config beside the original, gives it the
// original's ownership and mode, forces it to disk, and only then renames over
// the target. Notes on the details, each of which is load-bearing:
//
//   - `set -e` plus a `trap` on EXIT and on the usual signals, so a failure or a
//     kill anywhere leaves no stray file in /etc.
//   - `umask 077` because `cat >` creates the staged file before anything fixes
//     its mode, and /etc/mpd.conf is deliberately not world-readable.
//   - `mktemp` rather than a fixed `.new`, so two concurrent writers (in this or
//     any other process) cannot interleave into the same inode.
//   - `cp -a` to clone mode and ownership; `cat >` then replaces the contents
//     without disturbing either.
//   - `sync` before the rename: the rename is atomic, but without this a power
//     cut can leave a renamed-but-empty /etc/mpd.conf on an SD-card appliance.
//   - a non-empty check before the rename. `cat` exits 0 on a short or empty
//     stream, so without this a truncated relay would promote a zero-byte file
//     over a good config and report success. No caller can reach that today,
//     but this is the one file the whole appliance depends on.
//
// This uses GNU coreutils behaviour (`sync FILE`, `mktemp` in /etc) and runs
// only on the appliance. On darwin/windows the audio config is edited through
// the remote proxy in remote_audio.go, so this path is never reached there.
const mpdConfigWriteScript = `set -e
umask 077
target='__TARGET__'
tmp="$(mktemp "$(dirname "$target")/.mpd.conf.XXXXXX")"
trap 'rm -f "$tmp"' EXIT INT TERM HUP
cp -a "$target" "$tmp"
cat > "$tmp"
sync "$tmp"
[ -s "$tmp" ] || { echo "refusing to install an empty config" >&2; exit 1; }
[ -e "$target.orig" ] || cp -a "$target" "$target.orig"
cp -a "$target" "$target.bak"
mv -f "$tmp" "$target"
`

// writeMPDConfigWithSudo is the privileged implementation of writeMPDConfig.
func writeMPDConfigWithSudo(content string) error {
	// The path is interpolated into a single-quoted shell word; refuse anything
	// that could break out of it rather than building a broken script.
	if strings.ContainsAny(mpdConfigPath, "'\n") {
		return errors.New("refusing to write MPD config: unsafe path " + mpdConfigPath)
	}

	script := strings.Replace(mpdConfigWriteScript, "__TARGET__", mpdConfigPath, 1)

	cmd := exec.Command("sudo", "sh", "-c", script)
	cmd.Stdin = strings.NewReader(content)

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		// Without this the caller — and the frontend toast — only ever sees
		// "exit status 1", with no hint which link of the chain broke.
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return errors.New(err.Error() + ": " + msg)
		}
		return err
	}
	return nil
}
