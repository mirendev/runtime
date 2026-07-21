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
		// Updated for MIR-1428 (mounted disks writable by the run user) and
		// MIR-1435 (inject MIREN_INSTANCE_NUM alongside MIREN_RUNTIME_INSTANCE_NUM
		// as a deprecated alias). Both changes live in buildSubContainerSpec, which
		// the saga path also reaches via deps.runtime.BootContainers, so both
		// controllers already share the new behavior and there was nothing to
		// mirror in create_saga.go.
		//
		// Also updated for MIR-1072 (typed node-id boundary): the NodeId fields
		// became compute_v1alpha.NodeId and the inline entity.Id("node/"+c.NodeId)
		// constructions became c.NodeId.Id(). This is a mechanical retype that
		// produces byte-identical entity ids at runtime, and the saga path does no
		// node-id construction, so there was nothing to mirror there either.
		"sandbox.go":  "08abc0b3e6c57e4e2c4018d9b252d1de98768e8559e5eda8606fdb0c77347408",
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
