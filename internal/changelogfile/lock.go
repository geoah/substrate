//go:build unix

package changelogfile

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

// LockFileName is the file the writer lock is taken on, inside the changelog
// directory. It holds nothing; the lock is the kernel's, so a process that dies
// releases it. Every listing skips it.
const LockFileName = ".lock"

// ErrLocked is returned when another process holds a repository's changelog
// writer lock: a running server, or an operator command that opened the
// repository for writing. One writer per directory is what keeps the seqs in
// the file gapless (decision 0017), so the second one is refused, never
// queued.
var ErrLocked = errors.New("changelogfile: another process holds the changelog writer lock")

// dirLock is an exclusive advisory lock on a changelog directory, held through
// the open lock file's descriptor.
type dirLock struct {
	f *os.File
}

// lockDir takes the directory's writer lock without waiting. flock is per open
// file description, so two opens in one process conflict exactly as two
// processes do, and a crashed holder's lock dies with its descriptor.
func lockDir(dir string) (*dirLock, error) {
	f, err := os.OpenFile(filepath.Join(dir, LockFileName), os.O_RDWR|os.O_CREATE, fileMode)
	if err != nil {
		return nil, fmt.Errorf("changelogfile: open the lock file: %w", err)
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, fmt.Errorf("%w: %s", ErrLocked, dir)
		}
		return nil, fmt.Errorf("changelogfile: lock %s: %w", dir, err)
	}
	return &dirLock{f: f}, nil
}

// release drops the lock. Closing the descriptor releases a flock; the file
// stays, because a lock file that is unlinked while another process is about
// to open it is two processes locking two inodes.
func (l *dirLock) release() error {
	if l == nil || l.f == nil {
		return nil
	}
	err := l.f.Close()
	l.f = nil
	return err
}
