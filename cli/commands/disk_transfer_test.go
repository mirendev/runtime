package commands

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"miren.dev/runtime/pkg/cond"
)

// Retrying a refusal six times only makes the operator wait longer to read a
// message that will say the same thing every time.
func TestRefusalsAreNotRetried(t *testing.T) {
	refusals := []error{
		cond.ValidationFailure("disk-backup", `disk "data" is in use`),
		cond.NotFound("disk", "data"),
		fmt.Errorf("wrapped: %w", cond.ValidationFailure("disk-backup", "no cloud")),
	}
	for _, err := range refusals {
		assert.False(t, resumable(err), "should not retry: %v", err)
	}
}

// A dropped connection is exactly what resume exists for.
func TestTransportFailuresAreRetried(t *testing.T) {
	assert.True(t, resumable(errors.New("connection reset by peer")))
	assert.True(t, resumable(errors.New("the cluster's link to the cloud dropped")))
}

func TestNilIsNotRetried(t *testing.T) {
	assert.False(t, resumable(nil))
}

// Two transfers must not collide in the server's staging directory.
func TestTransferIDsAreDistinct(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 100; i++ {
		id, err := newTransferID()
		require.NoError(t, err)
		require.NotEmpty(t, id)
		assert.False(t, seen[id], "transfer ids must not repeat")
		seen[id] = true
	}
}

// The id names a file on the server, which validates it — so the ids we
// generate have to be ones it accepts.
func TestTransferIDsUseOnlySafeCharacters(t *testing.T) {
	id, err := newTransferID()
	require.NoError(t, err)

	for _, r := range id {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
		default:
			t.Fatalf("transfer id %q contains %q, which the server rejects", id, r)
		}
	}
}
