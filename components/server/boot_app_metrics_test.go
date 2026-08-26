//go:build linux

package server

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestManagedMetricsClusterLabel(t *testing.T) {
	assert.Equal(t, "cluster-123", managedMetricsClusterLabel("cluster-123", "friendly-name"))
	assert.Equal(t, "friendly-name", managedMetricsClusterLabel("", "friendly-name"))
	assert.Equal(t, "local", managedMetricsClusterLabel("", ""))
}
