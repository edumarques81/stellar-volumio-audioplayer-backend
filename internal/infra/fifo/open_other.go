//go:build !unix

package fifo

import "os"

// Open falls back to a plain open on platforms without POSIX FIFO semantics.
// Only the Pi (linux) and the dev Mac (darwin) run this code for real; the
// windows target exists as a portability compile check, so the interruptible
// behaviour documented in open_unix.go is not reproduced here.
func Open(path string) (*os.File, error) {
	return os.Open(path)
}
