package sandbox

import (
	"testing"

	"github.com/stretchr/testify/assert"
	compute "miren.dev/runtime/api/compute/compute_v1alpha"
	"miren.dev/runtime/pkg/entity"
)

// The entity's recorded addresses are only a safe cleanup source while the
// sandbox still owns them. Once it reaches DEAD a teardown has already released
// them, and nothing clears Network, so a later delete callback would be handing
// back an address that may since have been reserved by someone else.
func TestEntityFallbackIPs(t *testing.T) {
	network := []compute.Network{
		{Address: "10.8.0.5/32"},
		{Address: "10.8.0.6"},
	}

	tests := []struct {
		name   string
		status compute.SandboxStatus
		want   map[string]bool
	}{
		{
			name:   "running sandbox still owns its addresses",
			status: compute.RUNNING,
			want:   map[string]bool{"10.8.0.5": true, "10.8.0.6": true},
		},
		{
			name:   "stopped sandbox has not released them yet",
			status: compute.STOPPED,
			want:   map[string]bool{"10.8.0.5": true, "10.8.0.6": true},
		},
		{
			name:   "dead sandbox already released them",
			status: compute.DEAD,
			want:   map[string]bool{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sb := &compute.Sandbox{
				ID:      entity.Id("sandbox/test"),
				Status:  tt.status,
				Network: network,
			}

			assert.Equal(t, tt.want, entityFallbackIPs(sb))
		})
	}
}

func TestEntityFallbackIPs_SkipsEmptyAddresses(t *testing.T) {
	sb := &compute.Sandbox{
		ID:     entity.Id("sandbox/test"),
		Status: compute.RUNNING,
		Network: []compute.Network{
			{Address: ""},
			{Address: "10.8.0.7/32"},
		},
	}

	assert.Equal(t, map[string]bool{"10.8.0.7": true}, entityFallbackIPs(sb))
}
