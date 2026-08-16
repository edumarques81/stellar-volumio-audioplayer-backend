//go:build unix

package fifo

import (
	"errors"
	"os"
	"syscall"
)

// Open opens path read-only without blocking.
//
// O_NONBLOCK returns from open immediately (POSIX guarantees this for
// O_RDONLY on a FIFO, whether or not a writer is attached) and hands the
// descriptor to the runtime poller, so reads honour SetReadDeadline and a
// Reader wrapped around it can be interrupted. See the package comment for
// what happens without it.
//
// One consequence to design around: because the open no longer waits for a
// writer, a read on a writerless FIFO returns EOF rather than blocking. A
// caller that tails a pipe across sessions must pace its reopen attempts
// instead of relying on open() to park it.
func Open(path string) (*os.File, error) {
	for {
		fd, err := syscall.Open(path, syscall.O_RDONLY|syscall.O_NONBLOCK, 0)
		if err == nil {
			return os.NewFile(uintptr(fd), path), nil
		}
		if errors.Is(err, syscall.EINTR) {
			continue
		}
		return nil, &os.PathError{Op: "open", Path: path, Err: err}
	}
}
