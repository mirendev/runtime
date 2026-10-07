package commands

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

type backupCloseError struct{ err error }

func (c backupCloseError) Close() error { return c.err }

func TestCloseDebugBackupOutput(t *testing.T) {
	writeErr := errors.New("snapshot write failed")
	closeErr := errors.New("delayed write-back failure")

	for _, tc := range []struct {
		name       string
		closeErr   error
		backupErr  error
		wantRemove bool
	}{
		{"finished snapshot", nil, nil, false},
		{"unfinished snapshot", nil, writeErr, true},
		{"failed close", closeErr, nil, true},
		{"both failures", closeErr, writeErr, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "backup.miren.zst")
			require.NoError(t, os.WriteFile(path, []byte("snapshot"), 0600))

			err := closeDebugBackupOutput(backupCloseError{tc.closeErr}, path, tc.backupErr)
			if tc.wantRemove {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			if tc.backupErr != nil {
				require.ErrorIs(t, err, writeErr)
			}
			if tc.closeErr != nil {
				require.ErrorIs(t, err, closeErr)
			}
			_, statErr := os.Stat(path)
			if tc.wantRemove {
				require.ErrorIs(t, statErr, os.ErrNotExist)
			} else {
				require.NoError(t, statErr)
			}
		})
	}
}
