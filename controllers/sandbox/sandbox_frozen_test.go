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
		"sandbox.go":  "2d01b2c913efa7781d41433c3aba33ec00647c70ff5b3cd45913a923d968159b",
		"volume.go":   "580062fb8a34f3f7f965689467a4b0f2ed403bc63c1ecdeb44949a7ba7e08dff",
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
