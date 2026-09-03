package coordinate

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"time"

	appclient "miren.dev/runtime/api/app"
	"miren.dev/runtime/api/app/app_v1alpha"
	"miren.dev/runtime/api/build/build_v1alpha"
	"miren.dev/runtime/api/compute/compute_v1alpha"
	"miren.dev/runtime/api/debug/debug_v1alpha"
	deployment_v1alpha "miren.dev/runtime/api/deployment/deployment_v1alpha"
	aes "miren.dev/runtime/api/entityserver"
	"miren.dev/runtime/api/ingress"
	"miren.dev/runtime/api/ingress/ingress_v1alpha"
	"miren.dev/runtime/api/oidcbinding/oidcbinding_v1alpha"
	addonctrl "miren.dev/runtime/controllers/addon"
	"miren.dev/runtime/pkg/addon"
	"miren.dev/runtime/pkg/addon/memcache"
	"miren.dev/runtime/pkg/addon/mysql"
	"miren.dev/runtime/pkg/addon/postgresql"
	"miren.dev/runtime/pkg/addon/rabbitmq"
	addonsqlite "miren.dev/runtime/pkg/addon/sqlite"
	"miren.dev/runtime/pkg/addon/valkey"
	"miren.dev/runtime/pkg/entitysync"
	"miren.dev/runtime/pkg/labs"
	"miren.dev/runtime/pkg/rpc"
	"miren.dev/runtime/pkg/saga"
	"miren.dev/runtime/servers/app"
	"miren.dev/runtime/servers/build"
	debugsrv "miren.dev/runtime/servers/debug"
	"miren.dev/runtime/servers/deployment"
	"miren.dev/runtime/servers/logs"
	oidcbindingsrv "miren.dev/runtime/servers/oidcbinding"
	routessrv "miren.dev/runtime/servers/routes"
	sandboxserver "miren.dev/runtime/servers/sandbox"
)

// NewApplicationManagement constructs the application management plane on top
// of cluster state and the secret store.
func NewApplicationManagement(foundation *Foundation, secrets *SecretStore, diagnostics ...*entitysync.Diagnostics) *ApplicationManagement {
	var entitySyncDiagnostics *entitysync.Diagnostics
	if len(diagnostics) > 0 {
		entitySyncDiagnostics = diagnostics[0]
	}
	return &ApplicationManagement{Foundation: foundation, secrets: secrets, entitySyncDiagnostics: entitySyncDiagnostics}
}

// ApplicationManagement owns the durable app, addon, and run management surface.
// WorkloadControl realizes that intent as running workloads.
type ApplicationManagement struct {
	*Foundation
	secrets               *SecretStore
	addonRegistry         *addon.Registry
	addonFramework        *addon.ProviderFramework
	appInfo               *app.AppInfo
	debugServer           *debugsrv.Server
	entitySyncDiagnostics *entitysync.Diagnostics
	sagaBuilder           *build.SagaBuilder
	buildHandler          *rpc.Interface
	deploymentHandler     *rpc.Interface
}

// Start initializes the durable application-management objects and exposes their
// RPC handlers. Controllers that realize this intent belong to WorkloadControl.
func (c *ApplicationManagement) Start(ctx context.Context) error {
	if c.state == nil || c.store == nil || c.etcdStore == nil || c.eac == nil {
		return errors.New("cluster foundation is not ready")
	}
	if c.secrets == nil || c.secrets.Registry() == nil {
		return errors.New("application management prerequisites are not ready")
	}

	eac := c.eac
	ec := aes.NewClient(c.Log, eac)
	addonRegistry := addon.NewRegistry()
	addonFramework := addon.NewProviderFramework(c.Log, ec, eac, saga.NewEntityStorage(c.etcdStore, c.Log))
	addonRegistry.Register(postgresql.AddonName, postgresql.NewProvider(addonFramework), postgresql.Definition())
	addonRegistry.Register(mysql.AddonName, mysql.NewProvider(addonFramework), mysql.Definition())
	addonRegistry.Register(valkey.AddonName, valkey.NewProvider(addonFramework), valkey.Definition())
	addonRegistry.Register(rabbitmq.AddonName, rabbitmq.NewProvider(addonFramework), rabbitmq.Definition())
	addonRegistry.Register(memcache.AddonName, memcache.NewProvider(addonFramework), memcache.Definition())
	addonRegistry.Register(addonsqlite.AddonName, addonsqlite.NewProvider(addonFramework), addonsqlite.Definition())
	if err := addonRegistry.EnsureEntities(ctx, ec); err != nil {
		return fmt.Errorf("ensuring addon entities: %w", err)
	}

	c.addonRegistry = addonRegistry
	c.addonFramework = addonFramework
	c.appInfo = app.NewAppInfo(c.Log, ec, c.Cpu, c.Mem, c.HTTP, c.secrets.Registry())
	if err := c.exposeManagementAPIs(ctx); err != nil {
		return err
	}
	c.auditAddonAssociations(ctx, ec)
	return nil
}

// auditAddonAssociations reports bindings left stale by old credential
// rotations. It is a bounded, read-only upgrade check, so it belongs with the
// durable addon model but does not delay that model becoming available.
func (c *ApplicationManagement) auditAddonAssociations(ctx context.Context, ec *aes.Client) {
	go func() {
		auditCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
		defer cancel()
		stale, checked, err := addonctrl.ReportStaleAssociationVariables(auditCtx, c.Log, ec, c.eac)
		if err != nil {
			c.Log.Warn("addon association variable check did not complete",
				"stale", stale, "checked", checked, "error", err)
		} else if stale > 0 {
			c.Log.Warn("addon associations disagree with their apps' stored config",
				"stale", stale, "checked", checked)
		}
	}()
}

func (c *ApplicationManagement) exposeManagementAPIs(ctx context.Context) error {
	if c.state == nil || c.eac == nil || c.appInfo == nil || c.secrets == nil {
		return errors.New("application API prerequisites are not ready")
	}
	rs := c.state
	server := rs.Server()
	eac := c.eac
	ec := aes.NewClient(c.Log, eac)
	loopback, err := rs.Connect(rs.LoopbackAddr(), "entities")
	if err != nil {
		return err
	}
	ai := c.appInfo
	addonRegistry := c.addonRegistry
	secretRegistry := c.secrets.Registry()

	server.ExposeValue("dev.miren.runtime/app", app_v1alpha.AdaptCrud(ai))
	server.ExposeValue("dev.miren.runtime/app-status", app_v1alpha.AdaptAppStatus(ai))
	// Keep durable runs available with the rest of the management surface. A
	// run created before workload controllers are ready remains PENDING; hiding
	// this capability instead makes current CLIs mistake a booting server for a
	// pre-durable-runs release and silently fall back to legacy exec.
	server.ExposeValue("dev.miren.runtime/app-runs", app_v1alpha.AdaptRuns(ai))
	server.ExposeValue("dev.miren.runtime/sandboxes", compute_v1alpha.AdaptSandboxes(sandboxserver.NewServer(c.Log, ec)))
	server.ExposeValue("dev.miren.runtime/addons", app_v1alpha.AdaptAddons(
		app.NewAddonsServer(c.Log, ec, addonRegistry, addon.NewRegistryImageChecker()),
	))

	addonsLoopback, err := rs.Connect(rs.LoopbackAddr(), "dev.miren.runtime/addons")
	if err != nil {
		return err
	}
	addonsClient := app_v1alpha.NewAddonsClient(addonsLoopback)
	appClient := appclient.NewClient(c.Log, loopback)
	bs := build.NewBuilder(c.Log, eac, appClient, addonsClient, c.Resolver, c.TempDir, c.LogWriter, c.CloudAuth.DNSHostname, c.BuildKit, c.DataPath)
	bs.WorkloadIssuer = c.WorkloadIssuer
	bs.Secrets = secretRegistry

	var buildHandler build_v1alpha.Builder = bs
	if labs.Sagas() {
		sagaBuilder := build.NewSagaBuilder(bs, saga.NewEntityStorage(c.etcdStore, c.Log), c.Log)
		if err := sagaBuilder.Init(); err != nil {
			return err
		}
		c.sagaBuilder = sagaBuilder
		buildHandler = sagaBuilder
	}
	c.buildHandler = build_v1alpha.AdaptBuilder(buildHandler)
	server.ExposeValue("dev.miren.runtime/logs", app_v1alpha.AdaptLogs(logs.NewServer(c.Log, ec, c.Logs)))

	deploymentServer, err := deployment.NewDeploymentServer(c.Log, eac, ec, appClient, c.CloudAuth.DNSHostname, secretRegistry)
	if err != nil {
		return err
	}
	c.deploymentHandler = deployment_v1alpha.AdaptDeployment(deploymentServer)
	server.ExposeValue("dev.miren.runtime/oidc-bindings", oidcbinding_v1alpha.AdaptOidcBindings(oidcbindingsrv.NewServer(c.Log, ec, eac)))
	server.ExposeValue("dev.miren.runtime/routes", ingress_v1alpha.AdaptRoutes(
		routessrv.NewServer(c.Log, ingress.NewClient(c.Log, loopback)),
	))
	c.debugServer, err = debugsrv.NewServer(c.Log, filepath.Join(c.DataPath, "net.db"), eac)
	if err != nil {
		return err
	}
	c.debugServer.CloudSync = c.entitySyncDiagnostics
	server.ExposeValue("dev.miren.runtime/debug-netdb", debug_v1alpha.AdaptNetDB(c.debugServer))
	server.ExposeValue("dev.miren.runtime/debug-cloud-sync", debug_v1alpha.AdaptCloudSync(c.debugServer))
	return nil
}

func (c *ApplicationManagement) stopManagementAPIs() {
	if c.debugServer != nil {
		if err := c.debugServer.Close(); err != nil {
			c.Log.Error("failed to close debug server", "error", err)
		}
	}
}

func (c *ApplicationManagement) ExposeBuildAndDeployment() error {
	if c.state == nil || c.buildHandler == nil || c.deploymentHandler == nil {
		return errors.New("build and deployment APIs are not initialized")
	}
	c.state.Server().ExposeValue("dev.miren.runtime/build", c.buildHandler)
	c.state.Server().ExposeValue("dev.miren.runtime/deployment", c.deploymentHandler)
	return nil
}

func (c *ApplicationManagement) RecoverBuildSagas(ctx context.Context) {
	if c.sagaBuilder != nil {
		if err := c.sagaBuilder.Recover(ctx); err != nil {
			c.Log.Error("build saga recovery completed with errors", "error", err)
		}
	}
}

// Stop tears down the management RPC resources.
func (c *ApplicationManagement) Stop() {
	c.stopManagementAPIs()
}
