package lbdmod

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildLockExcludesASecondHolder(t *testing.T) {
	dataPath := t.TempDir()

	first, err := acquireBuildLock(dataPath)
	require.NoError(t, err)
	require.NotNil(t, first)

	// The case this guards: an operator running `install` while the server is
	// already rebuilding after a kernel upgrade. Without the lock both would
	// clear the same build directory and delete each other's container.
	second, err := acquireBuildLock(dataPath)
	require.ErrorIs(t, err, ErrBuildInProgress)
	assert.Nil(t, second)

	first.release()

	// Once the first is done the lock is available again.
	third, err := acquireBuildLock(dataPath)
	require.NoError(t, err)
	third.release()
}

func TestBuildLockCreatesItsDirectory(t *testing.T) {
	// A host installing for the first time has no /var/lib/miren/lbd yet.
	dataPath := filepath.Join(t.TempDir(), "fresh")

	lock, err := acquireBuildLock(dataPath)
	require.NoError(t, err)
	defer lock.release()

	_, err = os.Stat(filepath.Join(dataPath, "lbd", "build.lock"))
	require.NoError(t, err)
}

func TestReleasingTwiceIsSafe(t *testing.T) {
	lock, err := acquireBuildLock(t.TempDir())
	require.NoError(t, err)

	lock.release()
	lock.release()

	var nilLock *buildLock
	nilLock.release()
}

func TestBuildLockIsNotHeldByAStaleFile(t *testing.T) {
	// An flock dies with the process that held it, so a build killed partway
	// through must not wedge the host behind a leftover file.
	dataPath := t.TempDir()

	lock, err := acquireBuildLock(dataPath)
	require.NoError(t, err)
	lock.release()

	// The file survives on purpose; only the lock is gone.
	_, err = os.Stat(filepath.Join(dataPath, "lbd", "build.lock"))
	require.NoError(t, err)

	again, err := acquireBuildLock(dataPath)
	require.NoError(t, err, "a leftover lock file must not block a later build")
	assert.False(t, errors.Is(err, ErrBuildInProgress))
	again.release()
}
