package disk

import (
	"log/slog"
	"testing"

	compute "miren.dev/runtime/api/compute/compute_v1alpha"

	"github.com/stretchr/testify/assert"
)

func TestDiskController_New(t *testing.T) {
	log := slog.Default()
	controller := NewDiskController(log, nil, compute.NewNodeId("test-node"), "", true)

	assert.NotNil(t, controller)
	assert.NotNil(t, controller.Log)
	assert.Equal(t, "/var/lib/miren/disks", controller.mountBasePath)
	assert.Equal(t, compute.NewNodeId("test-node"), controller.NodeId)
}
