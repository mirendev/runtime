//go:build blackbox

package blackbox

import (
	"strings"
	"testing"
	"time"

	"miren.dev/runtime/blackbox/harness"
)

func TestDiskUndelete(t *testing.T) {
	t.Parallel()
	c := harness.NewCluster(t)
	m := harness.NewMiren(t, c)

	diskName := harness.UniqueAppName(t, "undel-disk")

	// Step 1: Create a test disk
	t.Log("Creating test disk...")
	r := m.MustRun("debug", "disk", "create", "-n", diskName, "-s", "1")
	r.RequireContains(t, diskName)

	// Step 2: Wait for disk to be provisioned
	t.Log("Waiting for disk to be provisioned...")
	harness.Poll(t, "disk provisioned", 60*time.Second, 2*time.Second,
		func() (bool, string) {
			r := m.Run("debug", "disk", "list")
			if !r.Success() {
				return false, "debug disk list failed"
			}
			if r.OutputContains(diskName) && r.OutputContains("provisioned") {
				return true, ""
			}
			return false, "disk not yet provisioned"
		},
	)

	// Step 3: Get the disk ID so we can delete it
	r = m.MustRun("debug", "disk", "list")
	diskID := extractDiskID(t, r, diskName)
	t.Logf("Disk ID: %s", diskID)

	// Step 4: Delete the disk
	t.Log("Deleting disk...")
	m.MustRun("debug", "disk", "delete", "-i", diskID)

	// Step 5: Wait for the disk entity to be fully removed by the controller
	t.Log("Waiting for disk deletion to complete...")
	harness.Poll(t, "disk deleted", 60*time.Second, 2*time.Second,
		func() (bool, string) {
			r := m.Run("debug", "disk", "list")
			if !r.Success() {
				return false, "debug disk list failed"
			}
			if !r.OutputContains(diskID) {
				return true, ""
			}
			return false, "disk entity still exists"
		},
	)

	// Step 6: Verify the disk appears in list-deleted
	// These commands need sudo because /var/lib/miren/disk-data is owned by root
	t.Log("Checking list-deleted...")
	r = m.RunCmd("sudo", "m", "disk", "list-deleted")
	r.RequireSuccess(t)
	r.RequireContains(t, diskName)

	// Step 7: Undelete the disk
	t.Log("Undeleting disk...")
	r = m.RunCmd("sudo", "m", "disk", "undelete", "-n", diskName)
	r.RequireSuccess(t)
	r.RequireContains(t, "Disk restored successfully")

	// Step 8: Verify the disk is back and provisioned
	t.Log("Verifying restored disk...")
	harness.Poll(t, "disk restored", 30*time.Second, 2*time.Second,
		func() (bool, string) {
			r := m.Run("debug", "disk", "list")
			if !r.Success() {
				return false, "debug disk list failed"
			}
			if r.OutputContains(diskName) && r.OutputContains("provisioned") {
				return true, ""
			}
			return false, "restored disk not yet provisioned"
		},
	)

	// Step 9: Verify it's no longer in list-deleted
	r = m.RunCmd("sudo", "m", "disk", "list-deleted")
	r.RequireSuccess(t)
	if r.OutputContains(diskName) {
		t.Error("disk should no longer appear in list-deleted after undelete")
	}

	// Step 10: Verify the restored disk can be leased
	t.Log("Creating lease on restored disk...")
	r = m.MustRun("debug", "disk", "list")
	restoredID := extractDiskID(t, r, diskName)

	r = m.MustRun("debug", "disk", "lease", "-d", restoredID)
	r.RequireContains(t, "Disk lease created successfully")
	leaseID := extractLeaseID(t, r)
	t.Logf("Lease ID: %s", leaseID)

	// Wait for lease to become bound
	t.Log("Waiting for lease to bind...")
	harness.Poll(t, "lease bound", 60*time.Second, 2*time.Second,
		func() (bool, string) {
			r := m.Run("debug", "disk", "lease-status", "-i", leaseID)
			if !r.Success() {
				return false, "lease-status failed"
			}
			if r.OutputContains("bound") {
				return true, ""
			}
			if r.OutputContains("failed") {
				return false, "lease failed"
			}
			return false, "lease not yet bound"
		},
	)

	// Step 11: Verify the disk is actually mounted
	t.Log("Verifying disk is mounted...")
	r = m.RunCmd("sudo", "m", "debug", "disk", "mounts")
	r.RequireSuccess(t)
	// The mount output shows loop devices under /var/lib/miren/disks/<volume-id>
	// Extract the volume ID from the restored disk
	r2 := m.MustRun("debug", "disk", "list")
	volID := extractVolumeID(t, r2, diskName)
	if !r.OutputContains(volID) {
		t.Errorf("expected disk mount for volume %s, got:\n%s", volID, r.Stdout+r.Stderr)
	}

	// Cleanup: release the lease, then delete the disk
	t.Log("Cleaning up...")
	m.Run("debug", "disk", "lease-release", "-i", leaseID)
	// Wait briefly for release to process before deleting
	harness.Poll(t, "lease released", 30*time.Second, 2*time.Second,
		func() (bool, string) {
			r := m.Run("debug", "disk", "lease-list", "-d", restoredID)
			if !r.Success() {
				return true, "" // list failed, probably no leases
			}
			if !r.OutputContains("bound") {
				return true, ""
			}
			return false, "lease still bound"
		},
	)
	m.Run("debug", "disk", "delete", "-i", restoredID)
}

// extractVolumeID parses the debug disk list output to find the Volume ID for a given disk name.
func extractVolumeID(t *testing.T, r *harness.Result, diskName string) string {
	t.Helper()

	output := r.Stdout + r.Stderr
	lines := strings.Split(output, "\n")
	// Find the disk by name, then look for the Volume ID line after it
	inDisk := false
	for _, line := range lines {
		if strings.Contains(line, "Name:") && strings.Contains(line, diskName) {
			inDisk = true
			continue
		}
		if inDisk && strings.Contains(line, "Volume ID:") {
			_, after, ok := strings.Cut(line, "Volume ID:")
			if ok {
				return strings.TrimSpace(after)
			}
		}
		// Next disk entry starts with "ID:"
		if inDisk && strings.Contains(line, "ID:") {
			break
		}
	}
	t.Fatalf("could not find Volume ID for disk %q in output:\n%s", diskName, output)
	return ""
}

// extractLeaseID parses the debug disk lease output to find the lease ID.
// The output contains: "Lease ID: disk_lease/disk-lease-xxx"
func extractLeaseID(t *testing.T, r *harness.Result) string {
	t.Helper()

	output := r.Stdout + r.Stderr
	for _, line := range strings.Split(output, "\n") {
		if strings.Contains(line, "Lease ID:") {
			_, after, ok := strings.Cut(line, "Lease ID:")
			if ok {
				return strings.TrimSpace(after)
			}
		}
	}
	t.Fatalf("could not find Lease ID in output:\n%s", output)
	return ""
}

// extractDiskID parses the debug disk list output to find the ID for a given disk name.
// The output format is:
//
//	ID: disk/disk-xxx
//	  Name: my-disk
func extractDiskID(t *testing.T, r *harness.Result, diskName string) string {
	t.Helper()

	output := r.Stdout + r.Stderr
	lines := strings.Split(output, "\n")
	for i, line := range lines {
		if strings.Contains(line, "Name:") && strings.Contains(line, diskName) {
			if i > 0 {
				idLine := lines[i-1]
				if strings.Contains(idLine, "ID:") {
					_, after, ok := strings.Cut(idLine, "ID:")
					if ok {
						return strings.TrimSpace(after)
					}
				}
			}
		}
	}
	t.Fatalf("could not find disk ID for %q in output:\n%s", diskName, output)
	return ""
}
