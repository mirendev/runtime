//go:build blackbox

package blackbox

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"miren.dev/runtime/blackbox/harness"
)

// TestDiskBackupRestoreOverRPC exercises the RFD-108 claim: backup and restore
// work from a client, without a shell on the server.
//
// The proof is in what the commands are NOT given. Every invocation here is a
// plain `m disk ...` with no sudo and no --data-path, where the old commands
// needed both — they opened /var/lib/miren directly and stopped early when it
// was not there.
func TestDiskBackupRestoreOverRPC(t *testing.T) {
	c := harness.NewCluster(t)
	m := harness.NewMiren(t, c)

	diskName := harness.UniqueAppName(t, "bk-disk")
	restoredName := diskName + "-restored"
	snapshotPath := fmt.Sprintf("/tmp/%s.miren.zst", diskName)

	t.Log("Creating source disk...")
	m.MustRun("debug", "disk", "create", "-n", diskName, "-s", "1")
	waitDiskProvisioned(t, m, diskName)

	// No sudo, no --data-path.
	t.Log("Backing up over RPC...")
	r := m.MustRun("disk", "backup", "-n", diskName, "-o", snapshotPath)
	r.RequireContains(t, "Backup complete")
	r.RequireContains(t, "Checksum:")

	// The snapshot has to have actually landed on the client side, since that
	// is the half the server streamed rather than wrote.
	r = m.RunCmd("test", "-s", snapshotPath)
	r.RequireSuccess(t)

	// Restoring into a name the cluster has never seen must create the disk.
	// This is the disaster-recovery path: on a rebuilt host there is nothing to
	// restore into.
	t.Log("Restoring into a new disk over RPC...")
	r = m.MustRun("disk", "restore", "-s", snapshotPath, "-n", restoredName)
	r.RequireContains(t, "Restore complete")
	r.RequireContains(t, "Created disk")

	waitDiskProvisioned(t, m, restoredName)

	// A universal-mode disk is mounted by the volume controller whether or not
	// anything leased it, and a loop device holds the image's inode rather than
	// its path. Restoring over it would leave the mounted filesystem on the old
	// image while reporting success, so it must be refused instead.
	t.Log("Checking that restore refuses a disk that is in use...")
	r = m.Run("disk", "restore", "-s", snapshotPath, "-n", diskName, "--force")
	if r.Success() {
		t.Errorf("restore into a mounted disk should have been refused, got:\n%s", r.Stdout+r.Stderr)
	}
	r.RequireContains(t, "in use")
}

// TestDiskBackupRejectsPinWithoutCloud covers the flag combination an operator
// is most likely to try first, since --pin only means anything to miren.cloud.
func TestDiskBackupRejectsPinWithoutCloud(t *testing.T) {
	c := harness.NewCluster(t)
	m := harness.NewMiren(t, c)

	r := m.Run("disk", "backup", "-n", "does-not-matter", "--pin", "some-name")
	if r.Success() {
		t.Fatalf("--pin without --cloud should have been rejected, got:\n%s", r.Stdout+r.Stderr)
	}
	r.RequireContains(t, "--cloud")
}

func waitDiskProvisioned(t *testing.T, m *harness.Miren, name string) {
	t.Helper()
	harness.Poll(t, "disk provisioned: "+name, 60*time.Second, 2*time.Second,
		func() (bool, string) {
			r := m.Run("debug", "disk", "list")
			if !r.Success() {
				return false, "debug disk list failed"
			}
			if diskIsProvisioned(r.Stdout+r.Stderr, name) {
				return true, ""
			}
			return false, "disk not yet provisioned"
		},
	)
}

// diskIsProvisioned reports whether the named disk's own entry says provisioned.
//
// Asking whether the whole listing mentions the name and mentions "provisioned"
// is not the same question: any other provisioned disk in the cluster answers
// the second half, so the poll would return while this disk was still coming
// up. The name and the status sit on different lines of one entry, so the match
// has to run over the entry rather than a line.
func diskIsProvisioned(output, name string) bool {
	lines := strings.Split(output, "\n")

	for i, line := range lines {
		if !strings.Contains(line, "Name:") || !strings.Contains(line, name) {
			continue
		}
		// Scan this entry's fields, stopping where the next entry begins.
		for _, field := range lines[i+1:] {
			if strings.HasPrefix(strings.TrimSpace(field), "ID:") {
				break
			}
			if strings.Contains(field, "Status:") {
				return strings.Contains(field, "provisioned")
			}
		}
	}
	return false
}
