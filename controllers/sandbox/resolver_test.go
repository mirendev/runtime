package sandbox

import (
	"net/netip"
	"strings"
	"testing"

	"github.com/containerd/containerd/v2/core/remotes/docker"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"miren.dev/runtime/components/netresolve"
	"miren.dev/runtime/components/ocireg"
	"miren.dev/runtime/pkg/workloadidentity"
)

func TestLocalRegistryHostUsesSandboxControllerIdentity(t *testing.T) {
	resolver, mapper := netresolve.NewLocalResolver()
	require.NoError(t, mapper.SetHost("cluster.local", netip.MustParseAddr("10.42.0.1")))

	issuer, err := workloadidentity.NewIssuer(workloadidentity.IssuerConfig{
		DataPath:  t.TempDir(),
		IssuerURL: "https://cluster.example.com",
	})
	require.NoError(t, err)

	controller := &SandboxController{
		Resolver:       resolver,
		WorkloadIssuer: issuer,
	}
	host, err := controller.localRegistryHost()
	require.NoError(t, err)

	assert.Equal(t, "10.42.0.1:5000", host.Host)
	assert.Equal(t, docker.HostCapabilityPull|docker.HostCapabilityResolve, host.Capabilities)
	assert.False(t, host.Capabilities&docker.HostCapabilityPush != 0)

	token := strings.TrimPrefix(host.Header.Get("Authorization"), "Bearer ")
	require.NotEmpty(t, token)
	_, err = issuer.VerifySystemWorkloadToken(
		token,
		ocireg.Audience,
		workloadidentity.SystemWorkloadSandboxController,
	)
	require.NoError(t, err)
}
