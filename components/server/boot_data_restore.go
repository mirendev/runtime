//go:build linux

package server

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"time"

	"miren.dev/runtime/components/etcd"
	"miren.dev/runtime/pkg/boot"
	"miren.dev/runtime/pkg/serverconfig"
	"miren.dev/runtime/pkg/serverlifecycle"
	"miren.dev/runtime/version"
)

// dataRestoreBoot answers a rolled-back upgrade's request to put the
// pre-upgrade data back, and refuses to let the server start if it cannot. The lifecycle executor cannot do it: embedded etcd
// is not a child of the server process, so whether it is still running when
// the executor restarts the service depends on how the last process ended,
// and the one moment the data can be replaced regardless is here, after
// containerd is up and before etcd is started on it. The request and the
// answer both live in the lifecycle store; see serverlifecycle.DataRestore.
//
// On most boots there is nothing pending and this is a directory listing.
type dataRestoreBoot struct {
	component *boot.Component
	inputs    dataRestoreInputs
}

type dataRestoreInputs struct {
	lifecycleDir string
	dataPath     string
	etcd         serverconfig.EtcdConfig
	// buildVersion and buildCommit identify this binary; a restore request
	// names the build it is for, and any other build leaves it alone.
	buildVersion string
	buildCommit  string
}

func dataRestoreInputsFrom(options StartOptions) dataRestoreInputs {
	build := version.GetInfo()
	return dataRestoreInputs{
		lifecycleDir: serverlifecycle.DefaultDir,
		dataPath:     options.Config.Server.GetDataPath(),
		etcd:         options.Config.Etcd,
		buildVersion: build.Version,
		buildCommit:  build.Commit,
	}
}

func newDataRestoreBoot(inputs dataRestoreInputs, containerd boot.Output[containerdBootOutput], observability boot.Output[observabilityBootOutput]) *dataRestoreBoot {
	b := &dataRestoreBoot{inputs: inputs}
	b.component = boot.Run2("data-restore", containerd, observability, b.start)
	return b
}

func (b *dataRestoreBoot) start(ctx context.Context, containerd containerdBootOutput, observability observabilityBootOutput) error {
	log := observability.log
	if _, err := os.Stat(b.inputs.lifecycleDir); errors.Is(err, os.ErrNotExist) {
		return nil
	}
	store, err := serverlifecycle.NewStore(b.inputs.lifecycleDir)
	if err != nil {
		return err
	}
	// A store that cannot be read might be hiding a request, and etcd must
	// not open the data directory until that is known, so this fails
	// closed. Only a lifecycle directory that does not exist at all (above)
	// is taken as nothing pending.
	op, err := store.PendingRestore()
	if err != nil {
		return fmt.Errorf("check for a pending data restore: %w", err)
	}
	if op == nil {
		return nil
	}
	if !op.DataRestore.MeantFor(b.inputs.buildVersion, b.inputs.buildCommit) {
		// The build being rolled back from, still running under systemd
		// while the executor swaps the binary. Restoring here would only
		// hand the rolled-back build data this one had migrated again.
		log.Info("leaving a data restore request for the build it is for", "operation", op.ID,
			"for_version", op.DataRestore.ForVersion, "this_version", b.inputs.buildVersion)
		return nil
	}

	ref := op.DataRestore.BackupRef
	log.Info("restoring data before start, as asked by a rolled back upgrade", "operation", op.ID, "backup_ref", ref)
	result := &serverlifecycle.RestoreResult{OperationID: op.ID, BackupRef: ref}
	restoreErr := b.restore(ctx, log, containerd, ref, op.ID)
	if restoreErr == nil {
		result.RestoredAt = time.Now().UTC()
		log.Info("data restored", "operation", op.ID, "backup_ref", ref)
	} else {
		result.Error = restoreErr.Error()
	}
	recorded, err := store.RecordRestoreAttempt(result)
	if err != nil {
		return fmt.Errorf("record data restore result for %s: %w", op.ID, err)
	}
	if recorded.Abandoned {
		// An operator abandoned the request while this attempt ran (and
		// likely restarted the service, which is what failed it). Honor it.
		log.Warn("data restore abandoned by operator during the attempt; starting on the data as it is", "operation", op.ID, "backup_ref", ref)
		return nil
	}
	if restoreErr != nil {
		// Starting anyway would mean the old build serving the data the
		// rollback was meant to replace, which is the outcome this component
		// exists to prevent. The live data is untouched (Restore stages
		// before it swaps), the request stays pending, and the failure is
		// recorded for the executor. So refuse to start; systemd will try
		// again, and an operator can end it with abandon.
		log.Error("data restore failed; refusing to start on the data as it is",
			"operation", op.ID, "backup_ref", ref, "error", restoreErr,
			"hint", "fix the cause and restart, or 'miren server operations abandon "+op.ID+"' to start without restoring")
		return fmt.Errorf("data restore for operation %s failed: %w", op.ID, restoreErr)
	}
	return nil
}

// restore knows what a BackupRef is: today, the path of an etcd snapshot
// taken by the executor's etcd DataBackup.
func (b *dataRestoreBoot) restore(ctx context.Context, log *slog.Logger, containerd containerdBootOutput, ref, label string) error {
	if !b.inputs.etcd.GetStartEmbedded() {
		return fmt.Errorf("cannot restore %s: etcd is not embedded", ref)
	}
	component := etcd.NewEtcdComponent(log, containerd.Client, containerd.Namespace, b.inputs.dataPath)
	return component.Restore(ctx, etcd.RestoreOptions{
		Snapshot: ref,
		Config:   embeddedEtcdConfig(b.inputs.etcd),
		Label:    label,
	})
}
