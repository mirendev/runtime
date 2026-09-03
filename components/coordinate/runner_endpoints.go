package coordinate

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"

	"miren.dev/runtime/api/runner/runner_v1alpha"
	"miren.dev/runtime/api/sqlitebackup/sqlitebackup_v1alpha"
	"miren.dev/runtime/api/telemetry/telemetry_v1alpha"
	"miren.dev/runtime/pkg/rpc"
	runnerserver "miren.dev/runtime/servers/runner"
	sqlitebackupsrv "miren.dev/runtime/servers/sqlitebackup"
	telemetrysrv "miren.dev/runtime/servers/telemetry"
)

// NewRunnerEndpoints constructs the coordinator endpoints a runner needs in order
// to join and preserve local state before the rest of the control plane boots.
func NewRunnerEndpoints(foundation *Foundation) *RunnerEndpoints {
	return &RunnerEndpoints{Foundation: foundation}
}

// RunnerEndpoints owns coordinator endpoints needed by runners before workload
// reconciliation begins.
type RunnerEndpoints struct {
	*Foundation
}

// Stop intentionally does nothing. RunnerEndpoints only registers handlers on
// the foundation-owned RPC server; declaring the method prevents
// Foundation.Stop from being promoted as this component's lifecycle.
func (*RunnerEndpoints) Stop() {}

// Start exposes the coordinator capabilities a runner needs to initialize.
// Keeping these ahead of workload control lets a restarting host recover disks
// and rejoin without depending on unrelated controllers.
func (c *RunnerEndpoints) Start(context.Context) error {
	if c.state == nil || c.eac == nil || c.authority == nil {
		return errors.New("cluster foundation is not ready")
	}

	server := c.state.Server()
	backup, err := sqlitebackupsrv.NewServer(c.Log, filepath.Join(c.DataPath, "sqlite-backups"))
	if err != nil {
		return fmt.Errorf("creating sqlite backup server: %w", err)
	}
	server.ExposeValue(rpc.ServiceSqliteBackup, sqlitebackup_v1alpha.AdaptSqliteBackup(backup))

	runnerReg := runnerserver.NewRegistrationServer(runnerserver.RegistrationServerConfig{
		Log:                    c.Log,
		Authority:              c.authority,
		EAC:                    c.eac,
		CoordinatorAddr:        c.Address,
		EtcdEndpoints:          c.EtcdEndpoints,
		EtcdPrefix:             c.Prefix,
		VictoriametricsAddress: c.VictoriametricsAddress,
		VictorialogsAddress:    c.VictorialogsAddress,
		WorkloadIssuer:         c.WorkloadIssuer,
	})
	server.ExposeValue(rpc.ServiceRunner, runner_v1alpha.AdaptRunnerRegistration(runnerReg))
	server.ExposeValue("dev.miren.runtime/telemetry", telemetry_v1alpha.AdaptTelemetry(telemetrysrv.NewServer(c.Log)))
	return nil
}
