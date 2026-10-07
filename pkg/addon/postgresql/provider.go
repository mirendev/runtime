package postgresql

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"sync"

	"miren.dev/runtime/api/addon/addon_v1alpha"
	"miren.dev/runtime/pkg/addon"
	"miren.dev/runtime/pkg/addon/dbsaga"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/saga"
)

// Provider implements the AddonProvider interface for PostgreSQL.
type Provider struct {
	dbsaga.BaseProvider
	cloneLocks sync.Map
	// The last association's teardown must not delete a shared server while
	// another association is being provisioned on it.
	sharedMu sync.Mutex
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
		p.sharedMu.Lock()
		defer p.sharedMu.Unlock()
		return p.provisionShared(ctx, assoc, app, variant)
	}
	return p.provisionDedicated(ctx, assoc, app, variant)
}

func (p *Provider) Deprovision(ctx context.Context, assoc addon.AddonAssociation) error {
	variant := assoc.Variant
	if IsSharedVariant(variant) {
		p.sharedMu.Lock()
		defer p.sharedMu.Unlock()
	}
	if assoc.SourceAssociation != "" {
		var shared addon_v1alpha.PostgresqlSharedData
		var dedicated addon_v1alpha.PostgresqlDedicatedData
		shared.Decode(assoc.Entity)
		dedicated.Decode(assoc.Entity)
		if shared.PostgresServer != "" && IsSharedVariant(variant) {
			return p.deprovisionShared(ctx, assoc)
		}
		if dedicated.PostgresServer != "" && !IsSharedVariant(variant) {
			return p.deprovisionDedicated(ctx, assoc)
		}
		exec, err := p.Fw.Storage.Get(ctx, addon.CloneExecutionID(assoc.ID))
		if errors.Is(err, saga.ErrExecutionNotFound) {
			// Rejected clone requests allocate nothing before recording a saga.
			return nil
		}
		if err != nil {
			return err
		}
		registry := saga.NewRegistry()
		for _, register := range []func(*saga.Registry, *addon.ProviderFramework) error{
			registerCloneSharedSaga, registerCloneDedicatedToSharedSaga, registerCloneDedicatedSaga,
		} {
			if err := register(registry, p.Fw); err != nil {
				return err
			}
		}
		executor := saga.NewExecutor(p.Fw.Storage, saga.WithRegistry(registry), saga.WithLogger(p.Log))
		if exec.Status != saga.StatusCompleted {
			return executor.Compensate(ctx, exec.ID)
		}
		// A completed clone can crash before its association receives resource
		// attributes. Recover them from the durable outputs for normal teardown.
		outputs, err := executor.ExecutionOutputs(ctx, exec.ID)
		if err != nil {
			return err
		}
		var serverID entity.Id
		var database, username string
		dbKey, userKey := "databasename", "username"
		if IsSharedVariant(variant) {
			dbKey, userKey = "shareddatabasename", "sharedusername"
		}
		for key, target := range map[string]any{"serverid": &serverID, dbKey: &database, userKey: &username} {
			if err := outputs.Get(key, target); err != nil {
				return err
			}
		}
		var attrs []entity.Attr
		if IsSharedVariant(variant) {
			attrs = (&addon_v1alpha.PostgresqlSharedData{PostgresServer: serverID, DatabaseName: database, Username: username}).Encode()
		} else {
			attrs = (&addon_v1alpha.PostgresqlDedicatedData{PostgresServer: serverID, DatabaseName: database, Username: username}).Encode()
		}
		assoc.Entity = entity.New(entity.DBId, assoc.ID, attrs)
	}
	if IsSharedVariant(variant) {
		return p.deprovisionShared(ctx, assoc)
	}
	return p.deprovisionDedicated(ctx, assoc)
}

func (p *Provider) Clone(ctx context.Context, source, target addon.AddonAssociation, app addon.App, variant addon.Variant) (*addon.ProvisionResult, error) {
	value, _ := p.cloneLocks.LoadOrStore(source.ID, &sync.Mutex{})
	mu := value.(*sync.Mutex)
	mu.Lock()
	defer mu.Unlock()

	if IsSharedVariant(variant.Name) {
		p.sharedMu.Lock()
		defer p.sharedMu.Unlock()
		if !IsSharedVariant(source.Variant) {
			return p.cloneDedicatedToShared(ctx, source, target, app, variant)
		}
		return p.cloneShared(ctx, source, target, app)
	}
	if IsSharedVariant(source.Variant) {
		return nil, fmt.Errorf("cloning shared PostgreSQL to a dedicated variant is not supported")
	}
	return p.cloneDedicated(ctx, source, target, app, variant)
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
