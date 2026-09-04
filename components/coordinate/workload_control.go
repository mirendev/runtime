package coordinate

import (
	"context"
	"errors"
	"fmt"
	"time"

	"miren.dev/runtime/api/addon/addon_v1alpha"
	"miren.dev/runtime/api/compute/compute_v1alpha"
	"miren.dev/runtime/api/core/core_v1alpha"
	aes "miren.dev/runtime/api/entityserver"
	"miren.dev/runtime/api/exec/exec_v1alpha"
	"miren.dev/runtime/api/ingress/ingress_v1alpha"
	"miren.dev/runtime/api/run/run_v1alpha"
	"miren.dev/runtime/components/activator"
	"miren.dev/runtime/components/autotls"
	addonctrl "miren.dev/runtime/controllers/addon"
	certctrl "miren.dev/runtime/controllers/certificate"
	deploymentctrl "miren.dev/runtime/controllers/deployment"
	ingressctrl "miren.dev/runtime/controllers/ingress"
	nodehealthctrl "miren.dev/runtime/controllers/nodehealth"
	runctrl "miren.dev/runtime/controllers/run"
	"miren.dev/runtime/controllers/sandboxpool"
	schedulerctrl "miren.dev/runtime/controllers/scheduler"
	"miren.dev/runtime/pkg/controller"
	"miren.dev/runtime/pkg/entity"
	execproxy "miren.dev/runtime/servers/exec_proxy"
)

// NewWorkloadControl constructs the controllers that turn management intent
// into running and routable sandbox capacity.
func NewWorkloadControl(foundation *Foundation, applications *ApplicationManagement) *WorkloadControl {
	return &WorkloadControl{Foundation: foundation, applications: applications}
}

// WorkloadControl owns workload realization and operational routing: app and
// addon reconciliation, one-shot runs, placement, exec routing, route defaults,
// and certificates. The HTTP listener itself remains data plane.
type WorkloadControl struct {
	*Foundation

	applications *ApplicationManagement

	aa            activator.AppActivator
	spm           *sandboxpool.Manager
	cm            *controller.ControllerManager
	runScheduler  *runctrl.Scheduler
	certProvider  autotls.CertificateProvider
	autocertReady func() // nil when DNS-01 path is used
}

func (c *WorkloadControl) Activator() activator.AppActivator {
	return c.aa
}

func (c *WorkloadControl) SandboxPoolManager() *sandboxpool.Manager {
	return c.spm
}

func (c *WorkloadControl) Stop() {
	if c.cm != nil {
		c.cm.Stop()
	}
	if c.runScheduler != nil {
		c.runScheduler.Stop()
	}
}

// Start recovers the workload and routing view. The server boot graph waits for
// durable management state and the restored local sandbox host before calling it.
func (c *WorkloadControl) Start(ctx context.Context) error {
	if c.state == nil || c.eac == nil {
		return errors.New("cluster foundation is not ready")
	}
	if c.applications == nil || c.applications.appInfo == nil || c.applications.addonRegistry == nil || c.applications.addonFramework == nil {
		return errors.New("application management is not ready")
	}

	eac := c.eac
	ec := aes.NewClient(c.Log, eac)
	rs := c.state
	server := rs.Server()
	ai := c.applications.appInfo
	addonRegistry := c.applications.addonRegistry
	addonFw := c.applications.addonFramework
	aa := activator.NewLocalActivator(ctx, c.Log, eac)
	spm := sandboxpool.NewManager(c.Log, eac)
	if err := spm.Init(ctx); err != nil {
		return fmt.Errorf("initializing sandbox pool manager: %w", err)
	}

	launcher := deploymentctrl.NewLauncher(c.Log, eac)
	launcher.DataPath = c.DataPath
	if err := launcher.Init(ctx); err != nil {
		return fmt.Errorf("initializing deployment launcher: %w", err)
	}
	aa.SetPoolCreator(launcher)

	cm := controller.NewControllerManager(
		controller.WithMetrics(c.MetricsWriter, map[string]string{"role": "coordinator"}),
	)
	// Add addon controller (reconciles addon associations for provisioning/deprovisioning)
	addonController := addonctrl.NewController(c.Log, ec, eac, addonRegistry, addonFw.Storage)
	if err := addonController.Init(ctx); err != nil {
		c.Log.Error("failed to initialize addon controller", "error", err)
		return err
	}

	addonReconciler := controller.NewReconcileController(
		"addon",
		c.Log,
		entity.Ref(entity.EntityKind, addon_v1alpha.KindAddonAssociation),
		eac,
		controller.AdaptReconcileController[addon_v1alpha.AddonAssociation](addonController),
		time.Minute,
		// Multiple workers so a long-running provisioning saga for one
		// association does not block reconciliation of others. Same-entity
		// concurrency is already prevented by ReconcileController.inFlight.
		4,
	)
	cm.AddController(addonReconciler)

	// Add addon rotation controller (reconciles rotation requests: apply a new
	// secret to the live engine, update the stored value, redeploy consumers).
	rotationController := addonctrl.NewRotationController(c.Log, ec, eac, addonRegistry)
	if err := rotationController.Init(ctx); err != nil {
		c.Log.Error("failed to initialize addon rotation controller", "error", err)
		return err
	}

	rotationReconciler := controller.NewReconcileController(
		"addon-rotation",
		c.Log,
		entity.Ref(entity.EntityKind, addon_v1alpha.KindRotationRequest),
		eac,
		controller.AdaptReconcileController[addon_v1alpha.RotationRequest](rotationController),
		time.Minute,
		4,
	)
	cm.AddController(rotationReconciler)

	// Run controller: one sandbox, one command, one exit code, then teardown.
	runController := runctrl.NewController(c.Log, ec, eac)
	if err := runController.Init(ctx); err != nil {
		c.Log.Error("failed to initialize run controller", "error", err)
		return err
	}

	runReconciler := controller.NewReconcileController(
		"run",
		c.Log,
		entity.Ref(entity.EntityKind, run_v1alpha.KindRun),
		eac,
		controller.AdaptReconcileController[run_v1alpha.Run](runController),
		time.Minute,
		4,
	)
	// The controller needs a handle on its own queue: the deadline sweep and the
	// sandbox bridge enqueue work rather than transitioning runs themselves, so
	// every status change stays inside the reconcile the framework serializes
	// per entity.
	runController.RC = runReconciler
	runReconciler.SetPeriodic(runctrl.SweepInterval, runController.SweepDeadlines)
	cm.AddController(runReconciler)

	// A sandbox reaching STOPPED produces no event on the run index, so without
	// this bridge a finished run would wait for the sweep to notice it.
	runSandboxWatch := runctrl.NewSandboxWatchController(runReconciler)
	runSandboxReconciler := controller.NewReconcileController(
		"run-sandbox-watch",
		c.Log,
		entity.Ref(entity.EntityKind, compute_v1alpha.KindSandbox),
		eac,
		controller.AdaptController(runSandboxWatch),
		0,
		1,
	)
	cm.AddController(runSandboxReconciler)

	cm.AddController(controller.NewReconcileController(
		"deploymentlauncher", c.Log,
		entity.Ref(entity.EntityKind, core_v1alpha.KindApp), eac,
		controller.AdaptReconcileController[core_v1alpha.App](launcher), time.Minute,
		2, // Parallelize apps; same-app work is serialized by the controller and launcher.
	))
	cm.AddController(controller.NewReconcileController(
		"deploymentlauncher-addons", c.Log,
		entity.Ref(entity.EntityKind, addon_v1alpha.KindAddonAssociation), eac,
		launcher.AddonAssociationHandler(), 0,
		2, // Parallelize apps; the launcher's per-app lock serializes matching associations.
	))
	cm.AddController(controller.NewReconcileController(
		"sandboxpool", c.Log,
		entity.Ref(entity.EntityKind, compute_v1alpha.KindSandboxPool), eac,
		controller.AdaptReconcileController[compute_v1alpha.SandboxPool](spm), 10*time.Second, 1,
	))

	scheduler := schedulerctrl.NewController(c.Log, eac)
	if err := scheduler.Init(ctx); err != nil {
		return fmt.Errorf("initializing scheduler: %w", err)
	}
	cm.AddController(controller.NewReconcileController(
		"scheduler", c.Log,
		entity.Ref(entity.EntityKind, compute_v1alpha.KindSandbox), eac,
		controller.AdaptReconcileController[compute_v1alpha.Sandbox](scheduler), time.Minute, 1,
	))

	nodeHealth := nodehealthctrl.NewController(c.Log, eac)
	if err := nodeHealth.Init(ctx); err != nil {
		return fmt.Errorf("initializing node health controller: %w", err)
	}
	cm.AddController(controller.NewReconcileController(
		"nodehealth", c.Log,
		entity.Ref(entity.EntityKind, compute_v1alpha.KindNode), eac,
		controller.AdaptReconcileController[compute_v1alpha.Node](nodeHealth), 30*time.Second, 1,
	))

	// Default routes are cluster-global derived state, not runner-local work.
	defaultRouteApp := ingressctrl.NewDefaultRouteAppController(c.Log, eac)
	defaultRoute := ingressctrl.NewDefaultRouteController(c.Log, eac)
	cm.AddController(controller.NewReconcileController(
		"default-route-app", c.Log,
		entity.Ref(entity.EntityKind, core_v1alpha.KindApp), eac,
		controller.AdaptController(defaultRouteApp), 0, 1,
	))
	cm.AddController(controller.NewReconcileController(
		"default-route", c.Log,
		entity.Ref(entity.EntityKind, ingress_v1alpha.KindHttpRoute), eac,
		controller.AdaptController(defaultRoute), 0, 1,
	))

	var clusterHostnames []string
	if c.CloudAuth.DNSHostname != "" {
		clusterHostnames = append(clusterHostnames, c.CloudAuth.DNSHostname)
	}
	if c.AcmeDNSProvider != "" {
		dnsController := certctrl.NewController(c.Log, c.DataPath, c.AcmeEmail, c.AcmeDNSProvider, clusterHostnames)
		if err := dnsController.Init(ctx); err != nil {
			return fmt.Errorf("initializing DNS certificate controller: %w", err)
		}
		c.certProvider = dnsController
		cm.AddController(controller.NewReconcileController(
			"certificate", c.Log,
			entity.Ref(entity.EntityKind, ingress_v1alpha.KindHttpRoute), eac,
			controller.AdaptReconcileController[ingress_v1alpha.HttpRoute](dnsController), time.Hour, 1,
		))
	} else {
		autocertController := certctrl.NewAutocertController(certctrl.AutocertControllerOpts{
			Log:              c.Log,
			EAC:              eac,
			DataPath:         c.DataPath,
			Email:            c.AcmeEmail,
			PublicIPs:        c.PublicIPs,
			ClusterHostnames: clusterHostnames,
		})
		if err := autocertController.Init(ctx); err != nil {
			return fmt.Errorf("initializing autocert controller: %w", err)
		}
		c.certProvider = autocertController
		c.autocertReady = autocertController.SetReady
		cm.AddController(controller.NewReconcileController(
			"certificate", c.Log,
			entity.Ref(entity.EntityKind, ingress_v1alpha.KindHttpRoute), eac,
			controller.AdaptReconcileController[ingress_v1alpha.HttpRoute](autocertController), time.Hour, 1,
		))
	}

	// Exec is operational routing: the coordinator forwards the request to the
	// node that owns the sandbox. The legacy app-name path uses AppInfo only to
	// translate that request into a durable Run before attaching.
	execServer := execproxy.NewServer(c.Log, eac, rs, ai)
	server.ExposeValue("dev.miren.runtime/exec", exec_v1alpha.AdaptSandboxExec(execServer))

	// Addon credential rotation executes inside the backing sandbox. Wire it to
	// the routing proxy before any controller can reconcile pending work.
	execLoopback, err := rs.Connect(rs.LoopbackAddr(), "dev.miren.runtime/exec")
	if err != nil {
		return fmt.Errorf("connecting to exec RPC service: %w", err)
	}
	addonFw.Exec = exec_v1alpha.NewSandboxExecClient(execLoopback)

	// Record ownership before starting so graph cleanup can stop controllers
	// that started before a later controller failed.
	c.cm = cm
	if err := cm.Start(ctx); err != nil {
		return fmt.Errorf("starting workload controllers: %w", err)
	}
	c.aa = aa
	c.spm = spm
	// Scheduled tasks. Deliberately not a reconcile controller: ticks come from
	// config on a timer, not from an entity changing, and dedup is the
	// create-if-absent on the tick-derived name rather than any lock.
	//
	// Started here with the other background controllers rather than at
	// construction, so it does not begin firing runs before the reconcile
	// manager -- including the run controller that would execute them -- is up.
	runScheduler := runctrl.NewScheduler(c.Log, ec, eac)
	runScheduler.Start(ctx)
	c.runScheduler = runScheduler
	return nil
}

// CertificateProvider returns the certificate provider for use by autotls.
func (c *WorkloadControl) CertificateProvider() autotls.CertificateProvider {
	return c.certProvider
}

// AutocertReadySignal returns a function that signals the autocert controller
// that the port-80 ACME challenge server is ready. Returns nil when the DNS-01
// path is used (which doesn't need port 80).
func (c *WorkloadControl) AutocertReadySignal() func() {
	return c.autocertReady
}
