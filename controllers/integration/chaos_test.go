package integration

import (
	"context"
	"fmt"
	"math/rand"
	"os"
	"sort"
	"strings"
	"testing"
	"time"

	compute "miren.dev/runtime/api/compute/compute_v1alpha"
	storage "miren.dev/runtime/api/storage/storage_v1alpha"
	"miren.dev/runtime/pkg/controller"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/idgen"
)

// chaosReport collects statistics throughout the chaos test for a final summary.
type chaosReport struct {
	seed           int64
	duration       time.Duration
	iterations     int
	faultAttempts  map[string]int
	faultSuccesses map[string]int
	sandboxSpawns  int

	// Entity counts at convergence
	totalDisks  int
	totalLeases int
	totalMounts int

	// Lease status breakdown at convergence
	boundLeases    int
	releasedLeases int
	failedLeases   int
	pendingLeases  int

	// Disk status breakdown at convergence
	provisionedDisks int
	errorDisks       int
	otherDisks       int

	// Mount status breakdown at convergence
	mountedMounts  int
	detachedMounts int
	otherMounts    int

	// Invariant results
	invariantViolations int
}

func newChaosReport(seed int64, duration time.Duration) *chaosReport {
	return &chaosReport{
		seed:           seed,
		duration:       duration,
		faultAttempts:  make(map[string]int),
		faultSuccesses: make(map[string]int),
	}
}

func (r *chaosReport) recordAttempt(name string, succeeded bool) {
	r.faultAttempts[name]++
	if succeeded {
		r.faultSuccesses[name]++
	}
}

func (r *chaosReport) collectEntityStats(t *testing.T, ctx context.Context, h *TestHarness) {
	t.Helper()

	disks := listDisks(t, ctx, h)
	leases := listLeases(t, ctx, h)
	mounts := listDiskMounts(t, ctx, h)

	r.totalDisks = len(disks)
	r.totalLeases = len(leases)
	r.totalMounts = len(mounts)

	for _, d := range disks {
		switch d.Status {
		case storage.PROVISIONED:
			r.provisionedDisks++
		case storage.ERROR:
			r.errorDisks++
		case storage.PROVISIONING, storage.ATTACHED, storage.DETACHED, storage.DELETING, storage.RESTORING:
			// Counted together as "other".
			fallthrough
		default:
			r.otherDisks++
		}
	}

	for _, l := range leases {
		switch l.Status {
		case storage.BOUND:
			r.boundLeases++
		case storage.RELEASED:
			r.releasedLeases++
		case storage.FAILED:
			r.failedLeases++
		case storage.PENDING:
			r.pendingLeases++
		}
	}

	for _, m := range mounts {
		switch m.ActualState {
		case storage.DM_MOUNTED:
			r.mountedMounts++
		case storage.DM_DETACHED:
			r.detachedMounts++
		case storage.DM_PENDING, storage.DM_ATTACHING, storage.DM_ATTACHED, storage.DM_MOUNTING, storage.DM_UNMOUNTING, storage.DM_DETACHING, storage.DM_ERROR:
			// Counted together as "other".
			fallthrough
		default:
			r.otherMounts++
		}
	}
}

func (r *chaosReport) emit(t *testing.T) {
	t.Helper()

	var b strings.Builder
	b.WriteString("\n")
	b.WriteString("╔══════════════════════════════════════════════╗\n")
	b.WriteString("║          CHAOS CONVERGENCE REPORT            ║\n")
	b.WriteString("╚══════════════════════════════════════════════╝\n")

	fmt.Fprintf(&b, "\nSeed: %d\n", r.seed)
	fmt.Fprintf(&b, "Duration: %s\n", r.duration)
	fmt.Fprintf(&b, "Iterations: %d\n", r.iterations)
	fmt.Fprintf(&b, "Sandboxes spawned mid-test: %d\n", r.sandboxSpawns)

	b.WriteString("\n── Fault Injection ─────────────────────────────\n")
	fmt.Fprintf(&b, "  %-25s %8s %8s %7s\n", "Fault", "Attempts", "Injected", "Rate")
	fmt.Fprintf(&b, "  %-25s %8s %8s %7s\n", "─────", "────────", "────────", "────")

	// Sort fault names for deterministic output
	var names []string
	for name := range r.faultAttempts {
		names = append(names, name)
	}
	sort.Strings(names)

	totalAttempts := 0
	totalSuccesses := 0
	for _, name := range names {
		attempts := r.faultAttempts[name]
		successes := r.faultSuccesses[name]
		totalAttempts += attempts
		totalSuccesses += successes
		rate := 0
		if attempts > 0 {
			rate = successes * 100 / attempts
		}
		fmt.Fprintf(&b, "  %-25s %8d %8d %6d%%\n", name, attempts, successes, rate)
	}
	fmt.Fprintf(&b, "  %-25s %8d %8d\n", "TOTAL", totalAttempts, totalSuccesses)

	b.WriteString("\n── Entities at Convergence ─────────────────────\n")
	fmt.Fprintf(&b, "  Disks:  %d total  (provisioned=%d, error=%d, other=%d)\n",
		r.totalDisks, r.provisionedDisks, r.errorDisks, r.otherDisks)
	fmt.Fprintf(&b, "  Leases: %d total  (bound=%d, released=%d, failed=%d, pending=%d)\n",
		r.totalLeases, r.boundLeases, r.releasedLeases, r.failedLeases, r.pendingLeases)
	fmt.Fprintf(&b, "  Mounts: %d total  (mounted=%d, detached=%d, other=%d)\n",
		r.totalMounts, r.mountedMounts, r.detachedMounts, r.otherMounts)

	b.WriteString("\n── Invariants ─────────────────────────────────\n")
	if r.invariantViolations == 0 {
		b.WriteString("  All 6 invariants passed\n")
	} else {
		fmt.Fprintf(&b, "  VIOLATIONS: %d (see errors above)\n", r.invariantViolations)
	}

	t.Log(b.String())
}

// chaosSlot tracks a sandbox and its associated disk resources.
type chaosSlot struct {
	sandboxID entity.Id
	diskID    entity.Id
	diskName  string
	leaseID   entity.Id
	alive     bool
}

// chaosState tracks all sandboxes created during the chaos test.
type chaosState struct {
	slots   []chaosSlot
	nextIdx int
}

func (cs *chaosState) newSlot(sandboxID, diskID entity.Id, diskName string, leaseID entity.Id) {
	cs.slots = append(cs.slots, chaosSlot{
		sandboxID: sandboxID,
		diskID:    diskID,
		diskName:  diskName,
		leaseID:   leaseID,
		alive:     true,
	})
}

func (cs *chaosState) aliveSlots() []int {
	var indices []int
	for i, s := range cs.slots {
		if s.alive {
			indices = append(indices, i)
		}
	}
	return indices
}

// chaosFault defines a named fault injection function.
type chaosFault struct {
	name   string
	weight int
	fn     func(t *testing.T, ctx context.Context, h *TestHarness, rng *rand.Rand, cs *chaosState) bool
}

func chaosFaultCatalog() []chaosFault {
	return []chaosFault{
		{"faultDeleteMount", 3, faultDeleteMount},
		{"faultMountError", 3, faultMountError},
		{"faultDiskError", 1, faultDiskError},
		{"faultDeleteLease", 2, faultDeleteLease},
		{"faultStopSandbox", 3, faultStopSandbox},
		{"faultRestartSandbox", 3, faultRestartSandbox},
		{"faultDuplicateLease", 1, faultDuplicateLease},
	}
}

// pickFault selects a random fault using weighted selection.
func pickFault(rng *rand.Rand, catalog []chaosFault) *chaosFault {
	totalWeight := 0
	for _, f := range catalog {
		totalWeight += f.weight
	}
	pick := rng.Intn(totalWeight)
	for i := range catalog {
		pick -= catalog[i].weight
		if pick < 0 {
			return &catalog[i]
		}
	}
	return &catalog[len(catalog)-1]
}

// faultDeleteMount deletes a random DM_MOUNTED mount entity.
func faultDeleteMount(t *testing.T, ctx context.Context, h *TestHarness, rng *rand.Rand, _ *chaosState) bool {
	t.Helper()
	mounts := listDiskMounts(t, ctx, h)
	var mounted []*storage.DiskMount
	for _, m := range mounts {
		if m.ActualState == storage.DM_MOUNTED {
			mounted = append(mounted, m)
		}
	}
	if len(mounted) == 0 {
		return false
	}
	target := mounted[rng.Intn(len(mounted))]
	deleteMountEntity(t, ctx, h, target.ID)
	return true
}

// faultMountError patches a random mount to MNT_ERROR.
func faultMountError(t *testing.T, ctx context.Context, h *TestHarness, rng *rand.Rand, _ *chaosState) bool {
	t.Helper()
	mounts := listDiskMounts(t, ctx, h)
	var mounted []*storage.DiskMount
	for _, m := range mounts {
		if m.ActualState == storage.DM_MOUNTED {
			mounted = append(mounted, m)
		}
	}
	if len(mounted) == 0 {
		return false
	}
	target := mounted[rng.Intn(len(mounted))]
	patchMountError(t, ctx, h, target.ID, "chaos: injected error")
	return true
}

// faultDiskError patches a random PROVISIONED disk to ERROR.
func faultDiskError(t *testing.T, ctx context.Context, h *TestHarness, rng *rand.Rand, _ *chaosState) bool {
	t.Helper()
	disks := listDisks(t, ctx, h)
	var provisioned []*storage.Disk
	for _, d := range disks {
		if d.Status == storage.PROVISIONED {
			provisioned = append(provisioned, d)
		}
	}
	if len(provisioned) == 0 {
		return false
	}
	target := provisioned[rng.Intn(len(provisioned))]
	patchDiskStatus(t, ctx, h, target.ID, storage.ERROR)
	return true
}

// faultDeleteLease deletes a random BOUND lease entity and fires the delete event
// so the controller can clean up the associated mount.
func faultDeleteLease(t *testing.T, ctx context.Context, h *TestHarness, rng *rand.Rand, _ *chaosState) bool {
	t.Helper()
	leases := listLeases(t, ctx, h)
	var bound []*storage.DiskLease
	for _, l := range leases {
		if l.Status == storage.BOUND {
			bound = append(bound, l)
		}
	}
	if len(bound) == 0 {
		return false
	}
	target := bound[rng.Intn(len(bound))]
	deleteLeaseEntity(t, ctx, h, target.ID)

	// Fire delete event so the controller processes mount cleanup
	deleteEvent := controller.Event{
		Type: controller.EventDeleted,
		Id:   target.ID,
	}
	_ = h.DiskLeaseRC.ProcessEventForTest(ctx, deleteEvent)
	return true
}

// faultStopSandbox stops a random alive sandbox.
func faultStopSandbox(t *testing.T, ctx context.Context, h *TestHarness, rng *rand.Rand, cs *chaosState) bool {
	t.Helper()
	alive := cs.aliveSlots()
	if len(alive) == 0 {
		return false
	}
	idx := alive[rng.Intn(len(alive))]
	slot := &cs.slots[idx]
	_ = h.FakeSandbox.ReleaseDiskLeases(ctx, slot.sandboxID)
	markSandboxDead(t, ctx, h, slot.sandboxID)
	slot.alive = false
	return true
}

// faultRestartSandbox stops a sandbox then boots a replacement with the same disk.
func faultRestartSandbox(t *testing.T, ctx context.Context, h *TestHarness, rng *rand.Rand, cs *chaosState) bool {
	t.Helper()
	alive := cs.aliveSlots()
	if len(alive) == 0 {
		return false
	}
	idx := alive[rng.Intn(len(alive))]
	slot := &cs.slots[idx]

	_ = h.FakeSandbox.ReleaseDiskLeases(ctx, slot.sandboxID)
	markSandboxDead(t, ctx, h, slot.sandboxID)
	slot.alive = false

	h.ReconcileAll(ctx, 5)

	newSbID := entity.Id(fmt.Sprintf("sandbox/chaos-r-%d", cs.nextIdx))
	cs.nextIdx++
	createSandboxEntity(t, ctx, h, newSbID, compute.PENDING)

	leaseID, err := h.FakeSandbox.AcquireDiskLease(ctx, slot.diskID, newSbID, "", "/data", false)
	if err != nil {
		return false
	}

	cs.newSlot(newSbID, slot.diskID, slot.diskName, leaseID)
	return true
}

// faultDuplicateLease creates a conflicting BOUND lease for a disk that already has one.
func faultDuplicateLease(t *testing.T, ctx context.Context, h *TestHarness, rng *rand.Rand, cs *chaosState) bool {
	t.Helper()
	leases := listLeases(t, ctx, h)
	var bound []*storage.DiskLease
	for _, l := range leases {
		if l.Status == storage.BOUND {
			bound = append(bound, l)
		}
	}
	if len(bound) == 0 {
		return false
	}
	target := bound[rng.Intn(len(bound))]

	conflictSbID := entity.Id(fmt.Sprintf("sandbox/chaos-dup-%d", cs.nextIdx))
	cs.nextIdx++
	createSandboxEntity(t, ctx, h, conflictSbID, compute.RUNNING)

	conflictLease := &storage.DiskLease{
		DiskId:     target.DiskId,
		SandboxId:  conflictSbID,
		Status:     storage.BOUND,
		AcquiredAt: time.Now(),
		Mount: storage.Mount{
			Path:    "/data",
			Options: "rw",
		},
		NodeId: compute.NewNodeId(testNodeId).Id(),
	}

	conflictLeaseID := entity.Id("disk-lease/" + idgen.GenNS("disk-lease"))
	_, err := h.EAC.Create(ctx, entity.New(
		entity.DBId, conflictLeaseID,
		conflictLease.Encode,
	).Attrs())
	if err != nil {
		return false
	}

	_ = conflictLeaseID
	return true
}

// chaosReconcileRound runs each controller 1-3 times in random order.
func chaosReconcileRound(ctx context.Context, h *TestHarness, rng *rand.Rand) {
	nodeId := compute.NewNodeId(testNodeId).Id()

	type ctrlDef struct {
		index entity.Attr
		rc    *controller.ReconcileController
	}

	controllers := []ctrlDef{
		{entity.Ref(entity.EntityKind, storage.KindDisk), h.DiskRC},
		{entity.Ref(storage.DiskVolumeNodeIdId, nodeId), h.DiskVolRC},
		{entity.Ref(storage.DiskMountNodeIdId, nodeId), h.DiskMntRC},
		{entity.Ref(entity.EntityKind, storage.KindDiskLease), h.DiskLeaseRC},
	}

	rng.Shuffle(len(controllers), func(i, j int) {
		controllers[i], controllers[j] = controllers[j], controllers[i]
	})

	for _, c := range controllers {
		passes := rng.Intn(3) + 1
		for i := 0; i < passes; i++ {
			h.reconcileByIndex(ctx, c.index, c.rc)
		}
	}
}

func TestChaosConvergence(t *testing.T) {
	ctx := context.Background()
	h := NewTestHarness(t)

	// Determine test duration
	duration := 30 * time.Second
	if testing.Short() {
		duration = 5 * time.Second
	}
	if envDur := os.Getenv("CHAOS_DURATION"); envDur != "" {
		d, err := time.ParseDuration(envDur)
		if err != nil {
			t.Fatalf("invalid CHAOS_DURATION %q: %v", envDur, err)
		}
		duration = d
	}

	seed := time.Now().UnixNano()
	rng := rand.New(rand.NewSource(seed))
	report := newChaosReport(seed, duration)

	cs := &chaosState{}

	// Phase 1: Boot N sandboxes with individual disks
	const numSandboxes = 3
	for i := 0; i < numSandboxes; i++ {
		sbID := entity.Id(fmt.Sprintf("sandbox/chaos-%d", i))
		diskName := fmt.Sprintf("chaos-disk-%d", i)
		diskID, leaseID := bootSandboxWithDisk(t, ctx, h, sbID, diskName, 10)
		cs.newSlot(sbID, diskID, diskName, leaseID)
		cs.nextIdx = i + 1
	}
	cs.nextIdx = numSandboxes

	for _, slot := range cs.slots {
		lease := getLease(t, ctx, h, slot.leaseID)
		if lease.Status != storage.BOUND {
			t.Fatalf("initial sandbox %s lease not BOUND: %s", slot.sandboxID, lease.Status)
		}
	}

	// Phase 2: Fault injection loop
	catalog := chaosFaultCatalog()

	deadline := time.Now().Add(duration)
	for time.Now().Before(deadline) {
		report.iterations++

		fault := pickFault(rng, catalog)
		ok := fault.fn(t, ctx, h, rng, cs)
		report.recordAttempt(fault.name, ok)

		rounds := rng.Intn(5) + 1
		for r := 0; r < rounds; r++ {
			chaosReconcileRound(ctx, h, rng)
		}

		// Occasionally boot a new sandbox with a new disk
		if rng.Intn(10) == 0 && len(cs.aliveSlots()) < 6 {
			newIdx := cs.nextIdx
			cs.nextIdx++
			sbID := entity.Id(fmt.Sprintf("sandbox/chaos-new-%d", newIdx))
			diskName := fmt.Sprintf("chaos-disk-new-%d", newIdx)

			createSandboxEntity(t, ctx, h, sbID, compute.PENDING)
			diskID, err := h.FakeSandbox.EnsureDisk(ctx, diskName, 10, "ext4")
			if err != nil {
				continue
			}
			leaseID, err := h.FakeSandbox.AcquireDiskLease(ctx, diskID, sbID, "", "/data", false)
			if err != nil {
				continue
			}
			cs.newSlot(sbID, diskID, diskName, leaseID)
			report.sandboxSpawns++
		}
	}

	// Phase 3: Converge to stable state
	h.ReconcileAll(ctx, 100)

	// Phase 4: Validate invariants and collect final stats
	report.collectEntityStats(t, ctx, h)
	report.invariantViolations = validateChaosInvariants(t, ctx, h, cs)
	report.emit(t)
}

func validateChaosInvariants(t *testing.T, ctx context.Context, h *TestHarness, cs *chaosState) int {
	t.Helper()
	violations := 0

	allLeases := listLeases(t, ctx, h)
	allMounts := listDiskMounts(t, ctx, h)
	allDisks := listDisks(t, ctx, h)

	// Build lookup maps
	diskByID := make(map[entity.Id]*storage.Disk)
	for _, d := range allDisks {
		diskByID[d.ID] = d
	}

	leaseByID := make(map[entity.Id]*storage.DiskLease)
	for _, l := range allLeases {
		leaseByID[l.ID] = l
	}

	mountsByLeaseID := make(map[entity.Id][]*storage.DiskMount)
	for _, m := range allMounts {
		if m.DiskLeaseId != "" {
			mountsByLeaseID[m.DiskLeaseId] = append(mountsByLeaseID[m.DiskLeaseId], m)
		}
	}

	// Build set of alive sandbox IDs
	aliveSandboxes := make(map[entity.Id]bool)
	for _, slot := range cs.slots {
		if slot.alive {
			aliveSandboxes[slot.sandboxID] = true
		}
	}

	// Track which disks have BOUND leases (for exclusive access check)
	boundLeasesByDisk := make(map[entity.Id][]entity.Id)

	for _, lease := range allLeases {
		if lease.Status == storage.BOUND {
			boundLeasesByDisk[lease.DiskId] = append(boundLeasesByDisk[lease.DiskId], lease.ID)
		}
	}

	// Invariant 1: Every non-FAILED, non-RELEASED lease for a live sandbox is BOUND
	for _, lease := range allLeases {
		if !aliveSandboxes[lease.SandboxId] {
			continue
		}
		if lease.Status == storage.FAILED || lease.Status == storage.RELEASED {
			continue
		}
		if lease.Status != storage.BOUND {
			t.Errorf("INVARIANT 1 violated: lease %s for live sandbox %s is %s (expected BOUND)", lease.ID, lease.SandboxId, lease.Status)
			violations++
		}
	}

	// Invariant 2: Every BOUND lease has exactly 1 DM_MOUNTED mount
	for _, lease := range allLeases {
		if lease.Status != storage.BOUND {
			continue
		}
		mounts := mountsByLeaseID[lease.ID]
		if len(mounts) == 0 {
			t.Errorf("INVARIANT 2 violated: BOUND lease %s has no mount", lease.ID)
			violations++
		} else if len(mounts) > 1 {
			t.Errorf("INVARIANT 2 violated: BOUND lease %s has %d mounts (expected 1)", lease.ID, len(mounts))
			violations++
		} else if mounts[0].ActualState != storage.DM_MOUNTED {
			t.Errorf("INVARIANT 2 violated: BOUND lease %s mount %s is %s (expected DM_MOUNTED)", lease.ID, mounts[0].ID, mounts[0].ActualState)
			violations++
		}
	}

	// Invariant 3: No two BOUND leases share the same disk
	for diskID, leaseIDs := range boundLeasesByDisk {
		if len(leaseIDs) > 1 {
			t.Errorf("INVARIANT 3 violated: disk %s has %d BOUND leases: %v", diskID, len(leaseIDs), leaseIDs)
			violations++
		}
	}

	// Invariant 4: Every BOUND lease's disk is PROVISIONED
	for _, lease := range allLeases {
		if lease.Status != storage.BOUND {
			continue
		}
		disk := diskByID[lease.DiskId]
		if disk == nil {
			t.Errorf("INVARIANT 4 violated: BOUND lease %s references missing disk %s", lease.ID, lease.DiskId)
			violations++
		} else if disk.Status != storage.PROVISIONED {
			t.Errorf("INVARIANT 4 violated: BOUND lease %s disk %s is %s (expected PROVISIONED)", lease.ID, lease.DiskId, disk.Status)
			violations++
		}
	}

	// Invariant 5: No orphaned mounts — every DM_MOUNTED mount has a corresponding BOUND lease.
	// NOTE: If this invariant is violated by a FAILED lease with a mounted mount, it indicates
	// a real controller bug — FAILED leases are terminal and never trigger mount cleanup,
	// leaking the mount resource.
	for _, mount := range allMounts {
		if mount.ActualState != storage.DM_MOUNTED {
			continue
		}
		lease := leaseByID[mount.DiskLeaseId]
		if lease == nil {
			t.Errorf("INVARIANT 5 violated: DM_MOUNTED mount %s references missing lease %s", mount.ID, mount.DiskLeaseId)
			violations++
		} else if lease.Status != storage.BOUND {
			t.Errorf("INVARIANT 5 violated: DM_MOUNTED mount %s lease %s is %s (expected BOUND)", mount.ID, mount.DiskLeaseId, lease.Status)
			violations++
		}
	}

	// Invariant 6: Disks in ERROR have no BOUND leases
	for _, disk := range allDisks {
		if disk.Status != storage.ERROR {
			continue
		}
		if leaseIDs, ok := boundLeasesByDisk[disk.ID]; ok && len(leaseIDs) > 0 {
			t.Errorf("INVARIANT 6 violated: ERROR disk %s has BOUND leases: %v", disk.ID, leaseIDs)
			violations++
		}
	}

	return violations
}
