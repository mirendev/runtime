package dbsaga

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"miren.dev/runtime/pkg/cond"
	"miren.dev/runtime/pkg/entity"
)

type conflictingCounter struct {
	count   int64
	patches int
	err     error
}

func (c *conflictingCounter) GetAssociationCount(context.Context, entity.Id) (int64, int64, error) {
	return c.count, int64(c.patches), nil
}

func (c *conflictingCounter) PatchAssociationCount(_ context.Context, _ entity.Id, revision, count int64) error {
	c.patches++
	if c.err != nil {
		return c.err
	}
	if c.patches == 1 {
		c.count = 5
		return cond.ErrConflict{}
	}
	if revision != int64(c.patches-1) {
		return errors.New("stale revision")
	}
	c.count = count
	return nil
}

func TestChangeAssociationCountRetriesWithFreshCount(t *testing.T) {
	for _, tc := range []struct {
		name  string
		delta int64
		want  int64
	}{
		{"increment", 1, 6},
		{"decrement", -1, 4},
	} {
		t.Run(tc.name, func(t *testing.T) {
			counter := &conflictingCounter{count: 2}
			count, err := changeAssociationCount(t.Context(), counter, "server/shared", tc.delta)
			require.NoError(t, err)
			require.Equal(t, tc.want, count)
			require.Equal(t, tc.want, counter.count)
			require.Equal(t, 2, counter.patches)
		})
	}
}

func TestChangeAssociationCountStopsOnErrorOrCancellation(t *testing.T) {
	patchErr := errors.New("server unavailable")
	counter := &conflictingCounter{count: 2, err: patchErr}
	_, err := changeAssociationCount(t.Context(), counter, "server/shared", 1)
	require.ErrorIs(t, err, patchErr)
	require.Equal(t, 1, counter.patches)

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err = changeAssociationCount(ctx, counter, "server/shared", 1)
	require.ErrorIs(t, err, context.Canceled)
	require.Equal(t, 1, counter.patches)

	counter = &conflictingCounter{}
	count, err := changeAssociationCount(t.Context(), counter, "server/shared", -1)
	require.NoError(t, err)
	require.Zero(t, count)
	require.Zero(t, counter.patches)
}
