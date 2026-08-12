package sqlite

import (
	"context"
	"log/slog"

	"miren.dev/runtime/pkg/addon"
)

// Provider implements the SQLite addon.
//
// It does not embed dbsaga.BaseProvider the way the server-backed addons do:
// that base hardcodes OnCluster locality and exists to share sandbox-pool saga
// steps, none of which apply here.
type Provider struct {
	log *slog.Logger
}

var _ addon.AddonProvider = (*Provider)(nil)

// NewProvider returns a Provider. It takes the framework for signature parity
// with the other providers, but has no use for it: there are no entities to
// create, since the database follows the app's own sandbox.
func NewProvider(fw *addon.ProviderFramework) *Provider {
	return &Provider{log: fw.Log.With("addon", AddonName)}
}

func (p *Provider) LocalityMode() addon.LocalityMode { return addon.InApp }

// Provision contributes the database to the app. There is nothing to create
// here: the disk is materialized when the app's sandbox next starts, and the
// database itself is created and replicated by the sqlite disk provider.
func (p *Provider) Provision(ctx context.Context, assoc addon.AddonAssociation, app addon.App, variant addon.Variant) (*addon.ProvisionResult, error) {
	p.log.Info("attaching sqlite database to app", "app", app.Name, "path", DatabasePath)

	return &addon.ProvisionResult{
		EnvVars: buildEnvVars(),
		Disks: []addon.Disk{
			{
				Name:      DiskName,
				Provider:  "sqlite",
				MountPath: MountPath,
				DbFile:    DbFile,
				// SQLite serializes writers within a process but cannot
				// coordinate across them, so more than one instance would
				// corrupt the database.
				RequiresSingleWriter: true,
			},
		},
	}, nil
}

// AdjustEnvVars leaves the variables alone on collision, matching every other
// provider. A user who has already set DATABASE_URL keeps their value, because
// the controller never overwrites a manually set variable.
func (p *Provider) AdjustEnvVars(ctx context.Context, result *addon.ProvisionResult, assoc addon.AddonAssociation, collisions []string) ([]addon.Variable, error) {
	return result.EnvVars, nil
}

// Deprovision has nothing of its own to tear down. The controller removes the
// disk from the app's config, which stops replication and detaches the database
// when the app next reconciles.
//
// The database file itself is deliberately left on the node, matching how a
// miren disk survives app deletion: the data is the user's, and an addon
// detaching is not a request to destroy it.
func (p *Provider) Deprovision(ctx context.Context, assoc addon.AddonAssociation) error {
	p.log.Info("detaching sqlite database", "association", assoc.ID)
	return nil
}

// buildEnvVars hands the app both a URL and the bare path, following the
// convention the other addons use. The file: form is what most SQLite drivers
// accept in a DATABASE_URL slot.
func buildEnvVars() []addon.Variable {
	return []addon.Variable{
		{Key: "DATABASE_URL", Value: "file:" + DatabasePath},
		{Key: "SQLITE_PATH", Value: DatabasePath},
	}
}
