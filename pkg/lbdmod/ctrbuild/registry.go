package ctrbuild

import (
	"fmt"
	"net/http"

	"github.com/containerd/containerd/v2/core/remotes"
	"github.com/containerd/containerd/v2/core/remotes/docker"
	"miren.dev/runtime/components/netresolve"
	"miren.dev/runtime/components/ocireg"
	"miren.dev/runtime/pkg/containerdx"
	"miren.dev/runtime/pkg/workloadidentity"
)

// ClusterRegistry resolves images from the cluster-local registry, which is
// where the toolchain image lives. Without it a node can only reach public
// registries, and the builder image is deliberately not published to one.
//
// This is the same path app image pulls already take on a runner: the address
// comes from an in-process host map rather than DNS, the hop is plain HTTP on
// the cluster's own network, and the bearer token is minted by the coordinator
// on a distributed runner's behalf.
type ClusterRegistry struct {
	// Resolver maps cluster.local to an address. On a runner that is the
	// coordinator's IP; on the coordinator it is the local router.
	Resolver netresolve.Resolver

	// Issuer mints the registry token. A distributed runner holds a remote
	// issuer that proxies to the coordinator, since it has no signing key of
	// its own.
	Issuer workloadidentity.TokenIssuer
}

// resolver returns a containerd resolver that knows the cluster registry and
// falls back to the normal public behavior for every other host.
func (c *ClusterRegistry) resolver() remotes.Resolver {
	return docker.NewResolver(docker.ResolverOptions{
		Hosts: func(host string) ([]docker.RegistryHost, error) {
			switch host {
			case "cluster.local", ocireg.Host:
				h, err := c.host()
				if err != nil {
					return nil, err
				}
				return []docker.RegistryHost{h}, nil
			default:
				return []docker.RegistryHost{containerdx.DefaultRegistryHost(host)}, nil
			}
		},
	})
}

func (c *ClusterRegistry) host() (docker.RegistryHost, error) {
	addr, err := c.Resolver.LookupHost("cluster.local")
	if err != nil {
		return docker.RegistryHost{}, fmt.Errorf("resolving cluster.local: %w", err)
	}

	h := docker.RegistryHost{
		Client: http.DefaultClient,
		Host:   addr.String() + ":5000",
		Scheme: "http",
		Path:   "/v2",
		// Pull only. The registry refuses a push under this identity anyway,
		// and nothing here ever needs one.
		Capabilities: docker.HostCapabilityPull | docker.HostCapabilityResolve,
	}

	if c.Issuer == nil {
		return h, nil
	}

	// Reuses the sandbox controller's identity rather than minting a new one:
	// it is the identity that means "this node pulling an image from the
	// cluster registry", it is already the only non-BuildKit workload the
	// registry grants GET and HEAD to, and it is already in the set a runner
	// is allowed to ask the coordinator for.
	token, err := c.Issuer.IssueSystemWorkloadToken(
		workloadidentity.SystemWorkloadSandboxController,
		workloadidentity.TokenOptions{Audience: []string{ocireg.Audience}},
	)
	if err != nil {
		return docker.RegistryHost{}, fmt.Errorf("issuing a registry token: %w", err)
	}
	h.Header = http.Header{"Authorization": []string{"Bearer " + token}}

	return h, nil
}
