//go:build linux

package paths

import (
	"bufio"
	"io"
	"os"
	"strings"
)

func dataDir() string      { return "/data/stellar" }
func cacheDir() string     { return "/data/stellar/cache" }
func nasMountBase() string { return "/mnt/NAS" }
func usbMountBase() string { return "/mnt/USB" }

// unescapeMountField decodes the octal escape sequences used by the kernel in
// /proc/mounts source and mountpoint fields: space (\040), tab (\011), newline
// (\012), and backslash (\134). Malformed escapes are passed through unchanged
// so downstream code never chokes on exotic input.
func unescapeMountField(s string) string {
	if !strings.Contains(s, `\`) {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c != '\\' || i+3 >= len(s) {
			b.WriteByte(c)
			continue
		}
		d1, d2, d3 := s[i+1], s[i+2], s[i+3]
		if !isOctalDigit(d1) || !isOctalDigit(d2) || !isOctalDigit(d3) {
			b.WriteByte(c)
			continue
		}
		v := (int(d1-'0') << 6) | (int(d2-'0') << 3) | int(d3-'0')
		b.WriteByte(byte(v))
		i += 3
	}
	return b.String()
}

func isOctalDigit(c byte) bool { return c >= '0' && c <= '7' }

// parseMounts parses /proc/mounts content from r and returns one Mount per
// non-empty line with >=3 fields. Source and MountPoint are octal-unescaped;
// FSType is taken verbatim (the kernel never escapes it).
func parseMounts(r io.Reader) ([]Mount, error) {
	var mounts []Mount
	scanner := bufio.NewScanner(r)
	// /proc/mounts lines are usually short but bind-mounts with long source
	// paths (containers, autofs) can exceed the default 64 KiB cap. Grow up
	// to 1 MiB before treating a line as pathological.
	scanner.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 3 {
			continue
		}
		mounts = append(mounts, Mount{
			Source:     unescapeMountField(fields[0]),
			MountPoint: unescapeMountField(fields[1]),
			FSType:     fields[2],
		})
	}
	if err := scanner.Err(); err != nil {
		return mounts, err
	}
	return mounts, nil
}

func listMounts() ([]Mount, error) {
	f, err := os.Open("/proc/mounts")
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return parseMounts(f)
}

// parseHardwareModel extracts the board "Model" line value from /proc/cpuinfo
// content. The Pi format is `Model\t\t: Raspberry Pi ...`. Matches are pinned
// to the board-model key so x86 `model name` lines (lowercase) do not match.
func parseHardwareModel(r io.Reader) string {
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := scanner.Text()
		if !(strings.HasPrefix(line, "Model\t") ||
			strings.HasPrefix(line, "Model :") ||
			strings.HasPrefix(line, "Model:")) {
			continue
		}
		if parts := strings.SplitN(line, ":", 2); len(parts) == 2 {
			return strings.TrimSpace(parts[1])
		}
	}
	return ""
}

func systemHardware() string {
	f, err := os.Open("/proc/cpuinfo")
	if err != nil {
		return ""
	}
	defer f.Close()
	return parseHardwareModel(f)
}
