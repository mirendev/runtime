package ctrbuild

import (
	"net/netip"
	"testing"

	"github.com/containerd/containerd/v2/core/remotes/docker"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"miren.dev/runtime/components/ocireg"
	"miren.dev/runtime/pkg/workloadidentity"
)

type fakeResolver struct {
	addr netip.Addr
	err  error
}

func (f fakeResolver) LookupHost(string) (netip.Addr, error) { return f.addr, f.err }

// fakeIssuer records what identity and audience the pull asked for.
type fakeIssuer struct {
	workloadidentity.TokenIssuer

	workload workloadidentity.SystemWorkload
	audience []string
}

func (f *fakeIssuer) IssueSystemWorkloadToken(w workloadidentity.SystemWorkload, opts workloadidentity.TokenOptions) (string, error) {
	f.workload = w
	f.audience = opts.Audience
	return "test-token", nil
}

func TestClusterRegistryHostTargetsTheClusterRegistry(t *testing.T) {
	r := &ClusterRegistry{Resolver: fakeResolver{addr: netip.MustParseAddr("10.1.2.3")}}

	h, err := r.host()
	require.NoError(t, err)

	// The registry is reached over the cluster's own network, so plain HTTP on
	// the coordinator's address rather than a public registry over TLS.
	assert.Equal(t, "10.1.2.3:5000", h.Host)
	assert.Equal(t, "http", h.Scheme)
	assert.Equal(t, "/v2", h.Path)
	// Pull only: the registry refuses a push under this identity anyway.
	assert.Zero(t, h.Capabilities&docker.HostCapabilityPush)
	assert.NotZero(t, h.Capabilities&docker.HostCapabilityPull)
}

func TestClusterRegistryWithoutAnIssuerSendsNoToken(t *testing.T) {
	// A coordinator with no issuer configured still resolves; the registry
	// only enforces when it has an issuer of its own.
	r := &ClusterRegistry{Resolver: fakeResolver{addr: netip.MustParseAddr("10.1.2.3")}}

	h, err := r.host()
	require.NoError(t, err)
	assert.Empty(t, h.Header.Get("Authorization"))
}

func TestClusterRegistryReportsAResolveFailure(t *testing.T) {
	r := &ClusterRegistry{Resolver: fakeResolver{err: assert.AnError}}

	_, err := r.host()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "resolving cluster.local")
}

func TestClusterRegistryAsksForAPullIdentity(t *testing.T) {
	issuer := &fakeIssuer{}
	r := &ClusterRegistry{
		Resolver: fakeResolver{addr: netip.MustParseAddr("10.1.2.3")},
		Issuer:   issuer,
	}

	h, err := r.host()
	require.NoError(t, err)

	assert.Equal(t, "Bearer test-token", h.Header.Get("Authorization"))
	// The registry grants GET and HEAD to exactly this identity; anything else
	// is a 403 on every pull.
	assert.Equal(t, workloadidentity.SystemWorkloadSandboxController, issuer.workload)
	assert.Equal(t, []string{ocireg.Audience}, issuer.audience)
}

func TestResolverIsBuilt(t *testing.T) {
	r := &ClusterRegistry{Resolver: fakeResolver{addr: netip.MustParseAddr("10.1.2.3")}}
	assert.NotNil(t, r.resolver())
}
