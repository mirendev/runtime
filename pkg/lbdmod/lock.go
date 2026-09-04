package lbdmod

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

// ErrBuildInProgress is returned when another process is already building the
// module on this host.
var ErrBuildInProgress = errors.New("an lbd build is already running on this host")

// buildLock serializes installs across processes.
//
// Two can genuinely overlap now that the server rebuilds unattended: an
// operator running `miren disk accelerator install` while a kernel upgrade has
// the server rebuilding in the background. Both would use the same build
// directory, which build() clears with RemoveAll, and the same fixed container
// name, which the builder deletes before creating its own -- so each would
// destroy the other's work and report a baffling failure.
//
// The lock is an flock rather than a lockfile whose existence is the signal,
// so a process killed mid-build releases it instead of wedging the host until
// someone deletes a stale file.
type buildLock struct {
	f *os.File
}

// acquireBuildLock takes the host-wide build lock without waiting. It returns
// ErrBuildInProgress if another process holds it.
func acquireBuildLock(dataPath string) (*buildLock, error) {
	dir := filepath.Join(dataPath, "lbd")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("creating %s: %w", dir, err)
	}

	path := filepath.Join(dir, "build.lock")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0644)
	if err != nil {
		return nil, fmt.Errorf("opening %s: %w", path, err)
	}

	if err := unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		f.Close()
		if errors.Is(err, unix.EWOULDBLOCK) {
			return nil, ErrBuildInProgress
		}
		return nil, fmt.Errorf("locking %s: %w", path, err)
	}

	return &buildLock{f: f}, nil
}

// release drops the lock. The file is left behind on purpose: removing it
// would let a second process create and lock a new file at the same path while
// a third still holds the old one.
func (l *buildLock) release() {
	if l == nil || l.f == nil {
		return
	}
	unix.Flock(int(l.f.Fd()), unix.LOCK_UN)
	l.f.Close()
	l.f = nil
}
