package memcache

import (
	"context"
	"fmt"
	"net"

	"miren.dev/runtime/pkg/addon"
	"miren.dev/runtime/pkg/addon/dbsaga"
)

type Provider struct {
	dbsaga.BaseProvider
}

func NewProvider(fw *addon.ProviderFramework) *Provider {
	return &Provider{
		BaseProvider: dbsaga.BaseProvider{
			Fw:  fw,
			Log: fw.Log.With("addon", AddonName),
		},
	}
}

func (p *Provider) Provision(ctx context.Context, assoc addon.AddonAssociation, app addon.App, variant addon.Variant) (*addon.ProvisionResult, error) {
	return p.provisionDedicated(ctx, assoc, app, variant)
}

func (p *Provider) Deprovision(ctx context.Context, assoc addon.AddonAssociation) error {
	return p.deprovisionDedicated(ctx, assoc)
}

func buildMemcacheURL(host string, port int) string {
	return fmt.Sprintf("memcache://%s", net.JoinHostPort(host, fmt.Sprintf("%d", port)))
}

func buildEnvVars(host string, port int) []addon.Variable {
	portStr := fmt.Sprintf("%d", port)

	return []addon.Variable{
		{Key: "MEMCACHE_URL", Value: buildMemcacheURL(host, port)},
		{Key: "MEMCACHE_HOST", Value: host},
		{Key: "MEMCACHE_PORT", Value: portStr},
	}
}
