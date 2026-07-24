package sandbox

import (
	"context"
	"testing"

	"github.com/containerd/containerd/v2/pkg/oci"
	"github.com/opencontainers/runtime-spec/specs-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The saga actions hold a context that nobody has namespaced, so every
// containerd call has to pick up the namespace on its way through sandboxOps.
// ContainerSpec used to be missing from that adapter and the saga called
// cont.Spec directly, which only worked because the server happens to build
// its containerd client with a default namespace. Anyone constructing a client
// without one (the test harness, for instance) got "namespace is required"
// instead.
func TestSandboxOpsContainerSpecCarriesNamespace(t *testing.T) {
	cont := &sagaMockContainer{
		id: "test-sandbox-1",
		spec: &oci.Spec{
			Linux: &specs.Linux{CgroupsPath: "/sys/fs/cgroup/sandbox/test-sandbox-1"},
		},
	}

	ops := &sandboxOps{ctrl: &SandboxController{Namespace: "miren-test"}}

	spec, err := ops.ContainerSpec(context.Background(), cont)
	require.NoError(t, err)
	assert.Equal(t, "/sys/fs/cgroup/sandbox/test-sandbox-1", spec.Linux.CgroupsPath)
	assert.Equal(t, "miren-test", cont.specNamespace,
		"ContainerSpec must namespace the context before handing it to containerd")
}
