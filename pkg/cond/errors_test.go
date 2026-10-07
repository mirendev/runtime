package cond

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestWorkload(t *testing.T) {
	exited := errors.New("sandbox process exited")
	err := Workload(exited)

	assert.Equal(t, exited.Error(), err.Error(), "marking an error does not change its message")
	assert.ErrorIs(t, err, exited)
	assert.True(t, IsWorkload(err))
	assert.True(t, IsWorkload(fmt.Errorf("saga failed: %w", err)), "wrapping keeps the mark")
	assert.True(t, IsWorkload(errors.Join(nil, fmt.Errorf("handler: %w", err))), "so does errors.Join")
	assert.Equal(t, err, Wrap(err), "Wrap passes it through rather than flattening it to a string")

	assert.False(t, IsWorkload(exited))
	assert.False(t, IsWorkload(nil))
	assert.Nil(t, Workload(nil))
}
