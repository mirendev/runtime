package mysql

import (
	"context"
	"fmt"
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
	if IsSharedVariant(variant.Name) {
		return p.provisionShared(ctx, assoc, app, variant)
	}
	return p.provisionDedicated(ctx, assoc, app, variant)
}

func (p *Provider) Deprovision(ctx context.Context, assoc addon.AddonAssociation) error {
	variant := assoc.Variant
	if IsSharedVariant(variant) {
		return p.deprovisionShared(ctx, assoc)
	}
	return p.deprovisionDedicated(ctx, assoc)
}

func buildDatabaseURL(host string, port int, user, password, database string) string {
	u := &url.URL{
		Scheme: "mysql",
		User:   url.UserPassword(user, password),
		Host:   fmt.Sprintf("%s:%d", host, port),
		Path:   database,
	}
	return u.String()
}

func buildEnvVars(host string, port int, user, password, database string) []addon.Variable {
	return []addon.Variable{
		{Key: "DATABASE_URL", Value: buildDatabaseURL(host, port, user, password, database), Sensitive: true},
		{Key: "MYSQL_HOST", Value: host},
		{Key: "MYSQL_PORT", Value: fmt.Sprintf("%d", port)},
		{Key: "MYSQL_USER", Value: user},
		{Key: "MYSQL_PASSWORD", Value: password, Sensitive: true},
		{Key: "MYSQL_DATABASE", Value: database},
	}
}
