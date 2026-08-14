package postgresql

import (
	"context"
	"fmt"
	"net/url"

	"miren.dev/runtime/pkg/addon"
	"miren.dev/runtime/pkg/addon/dbsaga"
)

// Provider implements the AddonProvider interface for PostgreSQL.
type Provider struct {
	dbsaga.BaseProvider
}

// NewProvider creates a new PostgreSQL addon provider.
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

// buildDatabaseURL constructs a postgres:// connection URL.
func buildDatabaseURL(host string, port int, user, password, database string) string {
	u := &url.URL{
		Scheme: "postgres",
		User:   url.UserPassword(user, password),
		Host:   fmt.Sprintf("%s:%d", host, port),
		Path:   database,
	}
	return u.String()
}

// buildEnvVars creates the standard set of PostgreSQL environment variables.
func buildEnvVars(host string, port int, user, password, database string) []addon.Variable {
	return []addon.Variable{
		{Key: "DATABASE_URL", Value: buildDatabaseURL(host, port, user, password, database), Sensitive: true},
		{Key: "PGHOST", Value: host},
		{Key: "PGPORT", Value: fmt.Sprintf("%d", port)},
		{Key: "PGUSER", Value: user},
		{Key: "PGPASSWORD", Value: password, Sensitive: true},
		{Key: "PGDATABASE", Value: database},
	}
}
