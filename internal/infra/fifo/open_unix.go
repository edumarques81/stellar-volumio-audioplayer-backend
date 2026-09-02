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

// OpenPersistent opens path for reading with a descriptor that never reports
// EOF, so a caller can hold one read end open across many writer sessions.
//
// Use it when the FIFO's writer is a third party that decides whether to
// attach based on whether a reader is already there. shairport-sync is exactly
// that: it opens its metadata pipe O_WRONLY|O_NONBLOCK, which fails with ENXIO
// unless a reader is attached at that instant, and it does not hold the
// descriptor across the gap. Tailing that pipe with Open — read EOF, close,
// sleep, reopen — leaves the read end absent for most of every cycle and hands
// shairport a different descriptor each time it is present, so the writer
// mostly cannot attach at all. That is what silently killed AirPlay track
// metadata: not a config or parsing fault, but a reader that kept hanging up.
//
// The mechanism is the standard FIFO idiom: O_RDWR makes the kernel count this
// process as a writer as well as a reader, so the writer count never falls to
// zero and reads block (here: return EAGAIN to the poller) instead of
// returning EOF when the real writer detaches. The descriptor stays valid
// across an unlimited number of writer sessions.
//
// POSIX leaves O_RDWR on a FIFO undefined; Linux defines it, and this daemon
// only ever runs on the Pi. Where it is refused — a read-only pipe, a
// restricted platform — this falls back to Open, which restores the previous
// polling behaviour rather than failing outright.
//
// Note the consequence for callers: a read on this descriptor never ends on
// its own. Cancellation is the only exit, so it must be wrapped in a Reader
// bound to a context, exactly as Open's descriptors are.
func OpenPersistent(path string) (*os.File, error) {
	for {
		fd, err := syscall.Open(path, syscall.O_RDWR|syscall.O_NONBLOCK, 0)
		if err == nil {
			return os.NewFile(uintptr(fd), path), nil
		}
		if errors.Is(err, syscall.EINTR) {
			continue
		}
		if errors.Is(err, syscall.EACCES) || errors.Is(err, syscall.EPERM) ||
			errors.Is(err, syscall.EINVAL) {
			return Open(path)
		}
		return nil, &os.PathError{Op: "open", Path: path, Err: err}
	}
}
