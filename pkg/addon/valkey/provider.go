package valkey

import (
	"context"
	"fmt"
	"net"
	"net/url"

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

func buildValkeyURL(host string, port int, password string) string {
	u := &url.URL{
		Scheme: "redis",
		Host:   net.JoinHostPort(host, fmt.Sprintf("%d", port)),
	}
	if password != "" {
		u.User = url.UserPassword("", password)
	}
	return u.String()
}

func buildEnvVars(host string, port int, password string) []addon.Variable {
	valkeyURL := buildValkeyURL(host, port, password)
	portStr := fmt.Sprintf("%d", port)

	return []addon.Variable{
		{Key: "VALKEY_URL", Value: valkeyURL, Sensitive: true},
		{Key: "VALKEY_HOST", Value: host},
		{Key: "VALKEY_PORT", Value: portStr},
		{Key: "VALKEY_PASSWORD", Value: password, Sensitive: true},
		{Key: "REDIS_URL", Value: valkeyURL, Sensitive: true},
		{Key: "REDIS_HOST", Value: host},
		{Key: "REDIS_PORT", Value: portStr},
		{Key: "REDIS_PASSWORD", Value: password, Sensitive: true},
	}
}
