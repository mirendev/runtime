package run

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	coreutil "miren.dev/runtime/api/core"
	"miren.dev/runtime/api/core/core_v1alpha"
	runapi "miren.dev/runtime/api/run"
	run_v1alpha "miren.dev/runtime/api/run/run_v1alpha"
	"miren.dev/runtime/pkg/cond"
	"miren.dev/runtime/pkg/entity"
)

// admit reports whether a run may move from pending to running.
//
// The guarantee differs by limit, and deliberately so. At max_concurrent = 1
// the property users depend on is exact -- "two deploys cannot run my migration
// at the same time" -- and that is expressible: at-most-one keyed on a derived
// name is what the store's put-if-absent Create already provides.
//
// Above 1 admission is best-effort. Counting live runs and then creating one is
// check-then-act, so two invokes arriving at nine live runs can both pass and
// produce eleven. Making it exact would need a transaction spanning the counter
// and the run, and pkg/entity's store is single-entity with no cross-entity
// compare-and-swap; the create guard is a key-absence predicate that cannot
// express "count < N". A ceiling exists to stop runaway automation rather than
// to enforce an invariant, so that trade is taken openly rather than papered
// over with a counter entity that would drift upward on any crash between
// teardown and decrement -- and a drifted counter denies service permanently,
// which is the worst way for an admission control to fail.
func (c *Controller) admit(ctx context.Context, r *run_v1alpha.Run) (bool, error) {
	maxConcurrent, err := c.maxConcurrent(ctx, r)
	if err != nil {
		return false, err
	}

	if maxConcurrent <= 1 {
		return c.acquireSlot(ctx, r)
	}
	return c.admitByCount(ctx, r, maxConcurrent)
}

// slotName derives the one gate entity for a task. The app id carries a "kind/"
// prefix, so flatten it to keep the name a single clean segment.
func slotName(app entity.Id, task string) string {
	return fmt.Sprintf("slot-%s-%s", strings.ReplaceAll(app.String(), "/", "-"), task)
}

// acquireSlot is the exact-at-one gate.
//
// The slot is a separate pointer entity rather than the Run itself. Deriving a
// name from app and task and letting Create gate the Run directly would work
// once, but every subsequent run would have to reclaim that same entity --
// destroying the execution history the Run exists to keep, and with it
// `miren app runs`, retention, and the tick dedup that depends on scheduled
// runs persisting.
//
// Validity is decided by reading the run the slot points at, not by a lease. A
// controller that crashes mid-run leaves the pointer aimed at a run that is
// terminal or gone, which the next admission reclaims. A lease would need a
// timeout tuned against the longest plausible run, and would deny service for
// that whole window if it were ever set too long.
func (c *Controller) acquireSlot(ctx context.Context, r *run_v1alpha.Run) (bool, error) {
	name := slotName(r.App, r.Task)

	var existing run_v1alpha.RunSlot
	existingEnt, err := c.EC.GetWithEntity(ctx, name, &existing)
	if err != nil {
		if !errors.Is(err, cond.ErrNotFound{}) {
			return false, fmt.Errorf("checking run slot: %w", err)
		}

		// No slot yet. Create is put-if-absent on the derived name, so a racing
		// admission loses here and is told the slot is taken by the re-read below.
		if _, cerr := c.EC.Create(ctx, name, &run_v1alpha.RunSlot{
			App:        r.App,
			Task:       r.Task,
			Run:        r.ID,
			AcquiredAt: time.Now(),
		}); cerr == nil {
			return true, nil
		} else {
			// Usually a lost race, which the re-read below reports as
			// in-flight. It can also be a transient store failure, and those
			// look identical from here -- so record it, or an operator sees
			// only a run that stays pending for no visible reason.
			c.Log.Debug("run slot create did not take, re-reading",
				"run", r.ID, "task", r.Task, "error", cerr)
		}

		existingEnt, err = c.EC.GetWithEntity(ctx, name, &existing)
		if err != nil {
			return false, nil
		}
	}

	// Already ours: a reconcile repeated after a dropped error, which the
	// framework does routinely since it never requeues.
	if existing.Run == r.ID {
		return true, nil
	}

	if held, err := c.slotHolderIsLive(ctx, existing.Run); err != nil {
		return false, err
	} else if held {
		return false, nil
	}

	// The holder is terminal or gone. Reclaim in place with a revision-guarded
	// patch; losing that CAS means another run took it first.
	if err := c.EC.Patch(ctx, existing.ID, existingEnt.Revision(),
		entity.Ref(run_v1alpha.RunSlotRunId, r.ID),
		entity.Ref(run_v1alpha.RunSlotAppId, r.App),
		entity.String(run_v1alpha.RunSlotTaskId, r.Task),
		entity.Time(run_v1alpha.RunSlotAcquiredAtId, time.Now()),
	); err != nil {
		if errors.Is(err, cond.ErrConflict{}) {
			return false, nil
		}
		return false, fmt.Errorf("reclaiming run slot: %w", err)
	}

	return true, nil
}

// slotHolderIsLive reports whether the run holding a slot is still running it.
// A missing run counts as not live: the entity was garbage collected or never
// finished being created, and either way it will never release the slot.
func (c *Controller) slotHolderIsLive(ctx context.Context, holder entity.Id) (bool, error) {
	if holder == "" {
		return false, nil
	}

	resp, err := c.EAC.Get(ctx, holder.String())
	if err != nil {
		if errors.Is(err, cond.ErrNotFound{}) {
			return false, nil
		}
		return false, fmt.Errorf("reading slot holder: %w", err)
	}

	var held run_v1alpha.Run
	held.Decode(resp.Entity().Entity())
	return !isTerminal(held.Status), nil
}

// release hands the slot back once a run is terminal.
//
// It is deliberately best-effort: a slot left pointing at a terminal run is
// reclaimed by the next admission, so a failure here delays the next run rather
// than blocking it forever.
func (c *Controller) release(ctx context.Context, r *run_v1alpha.Run) {
	name := slotName(r.App, r.Task)

	var slot run_v1alpha.RunSlot
	slotEnt, err := c.EC.GetWithEntity(ctx, name, &slot)
	if err != nil {
		return
	}

	// Only clear a slot this run actually holds. Another run may already have
	// reclaimed it, and stamping over that would let a third be admitted
	// alongside it.
	if slot.Run != r.ID {
		return
	}

	if err := c.EC.Patch(ctx, slot.ID, slotEnt.Revision(),
		entity.Ref(run_v1alpha.RunSlotRunId, entity.Id("")),
	); err != nil {
		c.Log.Debug("failed to release run slot", "run", r.ID, "task", r.Task, "error", err)
	}
}

// admitByCount is the best-effort gate above one. See admit's comment for why
// it cannot be exact on this store.
func (c *Controller) admitByCount(ctx context.Context, r *run_v1alpha.Run, maxConcurrent int64) (bool, error) {
	// List by status rather than by app. Runs are durable history, so the app
	// index grows without bound -- a task on a one-minute schedule would have
	// every admission decode a year of runs to count a handful. The running
	// index is bounded by what is executing cluster-wide.
	ids, err := c.EAC.List(ctx, entity.Ref(run_v1alpha.RunStatusId, run_v1alpha.RunStatusRunningId))
	if err != nil {
		return false, fmt.Errorf("listing runs for admission: %w", err)
	}

	var live int64
	for _, e := range ids.Values() {
		if e.Id() == r.ID.String() {
			continue
		}
		var other run_v1alpha.Run
		other.Decode(e.Entity())

		// Count only what is actually executing. Pending runs are queued
		// behind this same gate, so counting them would have every run in a
		// batch see the others and deny them all.
		if other.App == r.App && other.Task == r.Task && other.Status == run_v1alpha.RUNNING {
			live++
		}
	}

	if live >= maxConcurrent {
		c.Log.Info("run held back at the concurrency limit",
			"run", r.ID, "task", r.Task, "live", live, "max_concurrent", maxConcurrent)
		return false, nil
	}
	return true, nil
}

// maxConcurrent reads the task's cap from the version's config.
//
// A task that cannot be resolved -- no version, a version since deleted, an app
// redeployed without the task while a run was pending -- falls back to the
// default for its name rather than erroring, so the run resolves instead of
// wedging. runapi.MaxConcurrent supplies the defaults, including the console
// convention's higher one, so this agrees with the refusal the app server
// quotes to a caller.
func (c *Controller) maxConcurrent(ctx context.Context, r *run_v1alpha.Run) (int64, error) {
	if r.Version == "" {
		return runapi.MaxConcurrent(nil, r.Task), nil
	}

	resp, err := c.EAC.Get(ctx, r.Version.String())
	if err != nil {
		if errors.Is(err, cond.ErrNotFound{}) {
			return runapi.MaxConcurrent(nil, r.Task), nil
		}
		return 0, fmt.Errorf("reading app version for admission: %w", err)
	}

	var ver core_v1alpha.AppVersion
	ver.Decode(resp.Entity().Entity())

	cfgSpec, err := coreutil.ResolveRuntimeConfig(ctx, c.EAC, &ver)
	if err != nil {
		return runapi.MaxConcurrent(nil, r.Task), nil
	}

	for i := range cfgSpec.Tasks {
		if cfgSpec.Tasks[i].Name == r.Task {
			return runapi.MaxConcurrent(&cfgSpec.Tasks[i], r.Task), nil
		}
	}

	return runapi.MaxConcurrent(nil, r.Task), nil
}
