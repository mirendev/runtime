package coordinate

import (
	"context"
	"time"

	"miren.dev/runtime/api/core/core_v1alpha"
	"miren.dev/runtime/pkg/entitysync"
)

type ControlPlaneParts struct {
	Secrets         *SecretStore
	RunnerEndpoints *RunnerEndpoints
	Workloads       *WorkloadControl
	Applications    *ApplicationManagement
	Maintenance     *EntityMaintenance
	Cloud           *CloudControl
}

// NewControlPlane reconstitutes the public cluster-control handle from
// independently owned parts. Direct callers may omit parts and let Start
// compose them; the server boot graph passes the exact instances it owns.
func NewControlPlane(foundation *Foundation, supplied ...ControlPlaneParts) *ControlPlane {
	parts := ControlPlaneParts{}
	if len(supplied) > 0 {
		parts = supplied[0]
	}
	diagnostics := entitysync.NewDiagnostics(core_v1alpha.CloudExportContract.Digest())
	if parts.Cloud != nil {
		diagnostics = parts.Cloud.EntitySyncDiagnostics()
	} else if parts.Applications != nil && parts.Applications.entitySyncDiagnostics != nil {
		diagnostics = parts.Applications.entitySyncDiagnostics
	}
	if parts.Applications != nil && parts.Applications.entitySyncDiagnostics == nil {
		parts.Applications.entitySyncDiagnostics = diagnostics
	}
	if parts.Secrets == nil {
		parts.Secrets = NewSecretStore(foundation)
	}
	if parts.RunnerEndpoints == nil {
		parts.RunnerEndpoints = NewRunnerEndpoints(foundation)
	}
	if parts.Applications == nil {
		parts.Applications = NewApplicationManagement(foundation, parts.Secrets, diagnostics)
	}
	if parts.Workloads == nil {
		parts.Workloads = NewWorkloadControl(foundation, parts.Applications)
	}
	if parts.Maintenance == nil {
		parts.Maintenance = NewEntityMaintenance(foundation)
	}
	if parts.Cloud == nil {
		parts.Cloud = NewCloudControl(foundation, diagnostics)
	}
	return &ControlPlane{
		Foundation:      foundation,
		secrets:         parts.Secrets,
		runnerEndpoints: parts.RunnerEndpoints,
		workloads:       parts.Workloads,
		applications:    parts.Applications,
		maintenance:     parts.Maintenance,
		cloud:           parts.Cloud,
	}
}

// ControlPlane is the reconstituted cluster-control handle. It is not a server
// boot phase; the graph owns and schedules each part above.
type ControlPlane struct {
	*Foundation
	secrets         *SecretStore
	runnerEndpoints *RunnerEndpoints
	workloads       *WorkloadControl
	applications    *ApplicationManagement
	maintenance     *EntityMaintenance
	cloud           *CloudControl
}

func (c *ControlPlane) WorkloadControl() *WorkloadControl {
	return c.workloads
}

// Stop tears down the control plane and then its foundation for callers that
// construct both directly instead of using the server boot graph.
func (c *ControlPlane) Stop(ctx context.Context) error {
	if c.cloud != nil {
		c.cloud.Stop()
	}
	if c.maintenance != nil {
		c.maintenance.Stop()
	}
	if c.workloads != nil {
		c.workloads.Stop()
	}
	if c.applications != nil {
		c.applications.Stop()
	}
	if c.runnerEndpoints != nil {
		c.runnerEndpoints.Stop()
	}
	if c.secrets != nil {
		c.secrets.Stop()
	}
	return c.Foundation.Stop(ctx)
}

// Start is the compatibility path for tests and embedded callers that need the
// cluster control plane in one call. Server-only migrations, build and
// deployment admission and recovery, the local runner, and ingress start
// separately in the server boot graph.
func (c *ControlPlane) Start(ctx context.Context) (retErr error) {
	if err := c.Foundation.Start(ctx); err != nil {
		return err
	}
	defer func() {
		if retErr == nil {
			return
		}
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := c.Stop(cleanupCtx); err != nil {
			c.Log.Error("failed to roll back partial control plane startup", "error", err)
		}
	}()
	if err := c.PrepareAppData(ctx); err != nil {
		return err
	}
	if err := c.secrets.Start(ctx); err != nil {
		return err
	}
	if err := c.runnerEndpoints.Start(ctx); err != nil {
		return err
	}
	if err := c.applications.Start(ctx); err != nil {
		return err
	}
	if err := c.workloads.Start(ctx); err != nil {
		return err
	}
	if err := c.maintenance.Start(ctx); err != nil {
		return err
	}
	return c.cloud.Start(ctx)
}
