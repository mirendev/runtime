package rpc

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type detachValueKey struct{}

func TestDetachIgnoresCallerCancellationAndPreservesValues(t *testing.T) {
	request, cancelRequest := context.WithCancel(context.WithValue(context.Background(), detachValueKey{}, "kept"))
	detached, cancelDetached := Detach(request)
	defer cancelDetached()

	cancelRequest()

	assert.NoError(t, detached.Err())
	assert.Equal(t, "kept", detached.Value(detachValueKey{}))
}

func TestDetachStopsWithServerLifetime(t *testing.T) {
	lifetime, stopServer := context.WithCancel(context.Background())
	request, cancelRequest := context.WithCancel(context.Background())
	request = contextWithServerLifetime(request, lifetime)
	detached, cancelDetached := Detach(request)
	defer cancelDetached()

	cancelRequest()
	assert.NoError(t, detached.Err())

	stopServer()
	require.Eventually(t, func() bool { return detached.Err() != nil }, time.Second, time.Millisecond)
	assert.ErrorIs(t, detached.Err(), context.Canceled)
}
