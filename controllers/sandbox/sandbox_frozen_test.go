package sandbox

import (
	"crypto/sha256"
	"fmt"
	"os"
	"testing"
)

// TestSandboxControllerFrozen guards against accidental modifications to the
// original sandbox controller files while the saga-based replacement is being
// developed.
//
// If this test fails, it means one of the frozen files was modified. Before
// updating the hash, please:
//
//  1. Audit the saga controller (saga_controller.go, create_saga.go) to ensure
//     it reflects the same behavioral change.
//  2. Consider whether the change can wait until we fully cut over to sagas,
//     which would avoid maintaining two code paths.
//
// To update hashes after an intentional change:
//
//	sha256sum controllers/sandbox/sandbox.go controllers/sandbox/volume.go controllers/sandbox/firewall.go
func TestSandboxControllerFrozen(t *testing.T) {
	frozen := map[string]string{
		// SQLite disks (revised after review): replication is now released
		// after DestroySubContainers rather than before, so writes made during
		// graceful shutdown still reach the coordinator; surviving sandboxes
		// are re-registered in reconcileSandboxesOnBoot, since replication
		// lives in the runner process and does not survive its restart; and
		// the volume's directory mode is set explicitly because MkdirAll
		// respects the umask.
		//
		// SQLite disks: ConfigureVolumes gained a "sqlite" provider arm
		// (configureSqliteVolume), BuildSpec includes sqlite volumes in the
		// chown-to-run-user set so the container can write its own database,
		// and StopSandbox releases replication alongside disk leases. The saga
		// path needs no matching edit: it reaches all three through
		// sandboxOps.{ConfigureVolumes,BuildSpec}, which delegate to the
		// SandboxController methods, and ReleaseSqliteDisks was added to the
		// runtime interface and wired into the create saga's undo (see
		// create_saga.go) the same way ReleaseTokenState is.
		//
		// Workload identity: BuildSpec mounts the cluster CA and injects
		// MIREN_IN_CLUSTER/MIREN_API_ADDRESS/MIREN_CA_CERT_PATH, Init opens the
		// API port on the bridge, and mint now resolves the app's workload role
		// (resolveAppAndRole) so the token carries it. The saga path needs no
		// matching edit: it reaches the same code through sandboxOps.BuildSpec,
		// which delegates to SandboxController.BuildSpec.
		//
		// RFD-97 app tasks (MIR-853): monitorTaskExit records the exit code
		// alongside the STOPPED transition, and Create honors
		// SandboxSpec.RestartPolicy == never rather than rebooting a sandbox
		// whose command must execute at most once.
		//
		// Also adds the stdio fan-out Hub: attachable containers get a stdin
		// FIFO at boot and their output teed to attached clients.
		//
		// Saga path audited per the instructions below. BootContainers and
		// monitorTaskExit are not duplicated there -- create_saga.go reaches
		// them through sandbox_ops.go, so it inherits the exit recording. The
		// Create dispatch *is* duplicated, so both restart-policy guards were
		// mirrored into SagaSandboxController.Create.
		//
		// monitorTaskExit also reports the exit to the task run that owns the
		// sandbox, which is what makes an exit code reliable: the sandbox entity
		// has several writers and a stale read-modify-write can lose one, while
		// the run has a single writer plus that input.
		//
		// createSandbox's boot-failure defer now also tears down the sandbox's
		// Hubs. A Hub is created before the container's task is, so a container
		// that never started leaves one behind, and an attaching client finds it
		// and blocks on output that can never arrive.
		//
		// Saga path audited: this one is NOT inherited. saga_controller.go
		// replaces createSandbox wholesale with createSandboxViaSaga, so the
		// cleanup was mirrored into undoBootContainers in create_saga.go,
		// alongside the disk-lease and token-state releases that are duplicated
		// there for exactly the same reason.
		// Anchor moves: boot reconciliation notices a mounted token minted
		// under a different issuer and rewrites every token file once, rather
		// than leaving sandboxes on a superseded issuer until the 45-minute
		// refresh tick. No matching saga edit either: reconcileSandboxesOnBoot
		// is only reached from Init, which both paths share, and the saga
		// controller has no boot reconciliation of its own.
		"sandbox.go":  "3fbe652158f1e202f5a5dfe913d0b4eb718e6b69b27cf3ab8b5867a2702e5a1e",
		"volume.go":   "0555947db9407572a87b7e42f0792b80cebb061e0fe6a07be138689332e958f0",
		"firewall.go": "648cb5d91091d5eb7400152b19695a8045585feae59c5dd36c12d663a27bb91f",
	}

	for file, expectedHash := range frozen {
		t.Run(file, func(t *testing.T) {
			data, err := os.ReadFile(file)
			if err != nil {
				t.Fatalf("reading %s: %v", file, err)
			}

			actual := fmt.Sprintf("%x", sha256.Sum256(data))
			if actual != expectedHash {
				t.Fatalf(`%s has been modified (hash mismatch).

  expected: %s
  actual:   %s

This file is frozen while the saga-based sandbox controller is being developed.
Before updating the hash, please:

  1. Audit saga_controller.go and create_saga.go to ensure they reflect
     the same behavioral change you're making here.
  2. Consider holding off on this change until we fully switch over to sagas,
     so we don't have to maintain two code paths.

To update hashes:
  sha256sum controllers/sandbox/sandbox.go controllers/sandbox/volume.go controllers/sandbox/firewall.go`,
					file, expectedHash, actual)
			}
		})
	}
}
