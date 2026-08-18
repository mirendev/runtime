package coordinate

import (
	"context"
	"crypto/rand"
	"time"

	"github.com/oklog/ulid/v2"

	app_v1alpha "miren.dev/runtime/api/app/app_v1alpha"
	"miren.dev/runtime/pkg/uplink"
)

// The app.* message family carries the cluster's current app state to cloud
// for visibility reporting, as described in RFD-94.
//
// These types and their payloads live with the reporter rather than in
// pkg/uplink because the link is deliberately payload-agnostic: it moves
// opaque envelopes and understands no apps. A tenant owns its own wire shape.
const (
	// TypeAppSnapshot carries one batch of the cluster's full app set.
	TypeAppSnapshot = "app.snapshot"

	// TypeAppSnapshotComplete closes an epoch and is what authorizes cloud to
	// sweep. It is a separate message rather than a flag on the final batch so
	// that a cluster running no apps can still close an epoch, which is the
	// case where sweeping matters most: every app was deleted.
	TypeAppSnapshotComplete = "app.snapshot.complete"

	// TypeAppStatus carries one app whose sample changed between snapshots.
	TypeAppStatus = "app.status"
)

const (
	// snapshotBatchSize bounds one message so a large cluster's snapshot
	// arrives as a slide rather than a jump (RFD-94). Reporting must never take
	// meaningful horsepower from the cluster's real work.
	snapshotBatchSize = 100

	// batchPause spaces batches out for the same reason, and leaves the link
	// available to other tenants during the burst that follows a reconnect.
	batchPause = 100 * time.Millisecond

	// sampleInterval is how often the reporter re-derives app state and emits
	// deltas for whatever moved. It sets typical freshness.
	sampleInterval = 15 * time.Second

	// snapshotFloor is the safety net, not the main repair path, which is why
	// it is this loose.
	//
	// It once had three jobs and is down to one. Deltas are sent with the
	// blocking send, so they are not silently dropped; if a connection dies
	// mid-send, the reconnect opens a fresh snapshot anyway. Deletes no longer
	// wait for it either — a removal triggers a snapshot on the next sample.
	// What remains is a store that lost its samples without the connection
	// dropping, which on cloud's disk-backed Valkey survives an ordinary
	// restart and so is genuinely exceptional.
	//
	// The cost of a longer floor is how long that exceptional case shows apps
	// as awaiting a report. The cost of a shorter one is re-sending unchanged
	// state across the whole fleet forever. For a rare event, the trade favors
	// the fleet.
	snapshotFloor = 15 * time.Minute

	// connectSpread is how far the opening snapshot is scattered across the
	// fleet.
	//
	// Clusters reconnect for reasons that are the same for all of them at once
	// — a cloud deploy, a load balancer rotation — so without this every
	// cluster's opening snapshot lands on cloud in the same instant. Jittering
	// the reconnect alone would only move that spike, since the snapshot
	// follows the connect immediately.
	//
	// Chosen generously rather than tightly, because the cost is asymmetric.
	// This constant ships inside runtimes customers deploy, so widening it later
	// means getting a release onto every cluster in the fleet, and the fleets
	// that would most need it are the slowest to upgrade. Overshooting costs a
	// slightly later first full picture and nothing else, since this is the
	// ephemeral tier and deltas keep flowing during the wait.
	connectSpread = 60 * time.Second
)

// AppSnapshot is one batch of the cluster's view of every app it currently
// runs. Cloud treats a complete epoch as ground truth for that cluster.
//
// This is the ephemeral tier of RFD-94's volatility spectrum: the weakest
// durability contract in the design. Delivery is best-effort because loss
// self-heals — a dropped snapshot is repaired by the next one rather than
// retried, so nothing here is worth an ack.
type AppSnapshot struct {
	// Epoch identifies the snapshot this batch belongs to. Cloud accumulates
	// the apps it sees under an epoch and sweeps whatever is missing only once
	// the matching complete message arrives, so a snapshot interrupted midway
	// sweeps nothing at all rather than soft-deleting apps that simply had not
	// landed yet.
	//
	// A ULID rather than a counter because a counter restarts at zero when the
	// runtime does, and cloud would then see a fresh epoch collide with the
	// abandoned remains of the previous process's epoch of the same name.
	Epoch string `json:"epoch"`

	// ObservedAt is the runtime's own clock reading for when this state was
	// true. Cloud reconciles it against its own clock using the offset from
	// the link's time sync, and applies last-writer-wins with it, so a delayed
	// message cannot clobber fresher state.
	ObservedAt time.Time `json:"observed_at"`

	Apps []AppState `json:"apps"`
}

// AppSnapshotComplete closes an epoch and tells cloud how many apps that epoch
// should have contained.
//
// There is deliberately no timestamp here. Every batch already carries the
// epoch's observed_at, which is the moment the state was derived; a second
// timestamp taken when transmission happened to finish would describe the
// sender's pacing rather than the data, and sooner or later someone would
// reasonably mistake it for the latter.
type AppSnapshotComplete struct {
	Epoch string `json:"epoch"`

	// AppCount is how many apps the runtime sent under this epoch, and exists
	// so a snapshot missing a batch fails safe.
	//
	// The link drops on outbox overflow and is drained on reconnect, so batches
	// can go missing with no error anywhere. Cloud cannot tell a batch that was
	// dropped from apps that were deleted, and guessing wrong soft-deletes live
	// apps. Comparing the count it accumulated against the count claimed here
	// lets it decline to sweep instead, leaving the repair to the next epoch.
	AppCount int `json:"app_count"`
}

// AppStatus carries the apps whose samples changed since the last report. A
// lost delta is not a correctness problem: the next snapshot repairs it.
//
// A list rather than one app per message, because changes cluster in time. A
// rolling deploy, a node draining, an autoscale event: each moves many apps
// within a single sampling tick, and one message per app would turn that into a
// burst of envelopes that cloud pays for individually. The ordinary case of a
// single app degenerates to a one-element list at no cost.
//
// All apps in one message share ObservedAt because they come from one
// derivation of the cluster's state, which is the same reason a snapshot batch
// carries one timestamp for its whole list.
type AppStatus struct {
	ObservedAt time.Time  `json:"observed_at"`
	Apps       []AppState `json:"apps"`
}

// AppState is one app's current sample.
//
// Note there is no cluster or organization field. Cloud derives both from the
// authenticated cluster identity on the socket and its own clusters record — a
// cluster is never trusted to say which tenant its data belongs to, only what
// its own apps are doing.
type AppState struct {
	Name string `json:"name"`

	// Health is the runtime's classification, one of the apphealth values:
	// healthy, degraded, starting, crashed, idle, or unknown. It is computed
	// the same way `miren app list` computes it, deliberately, so the CLI and
	// the dashboard cannot disagree about whether an app is fine.
	Health string `json:"health"`

	ReadyInstances   int32 `json:"ready_instances"`
	DesiredInstances int32 `json:"desired_instances"`
}

// reportLink is the slice of the uplink the reporter uses. Depending on the
// narrow interface keeps the reporter testable without standing up a socket,
// and keeps unrelated link changes from rippling into it.
//
// Only the blocking send is here on purpose: everything this reporter sends is
// part of a sequence, and the dropping form is unsafe for sequences.
type reportLink interface {
	SendMessageBlocking(ctx context.Context, msgType string, data any) error
}

// startAppReporter arranges for the cluster's app state to be reported to
// cloud for as long as each connection lasts.
//
// Reporting is scoped to a connection rather than run on a standalone timer
// because the uplink discards anything queued while it was down. Whatever
// cloud missed during an outage never reached the wire at all, so it can only
// be recovered by re-deriving current state from the entity store once the
// link is back. That makes the source, not the pipe, responsible for
// completeness — see RFD-94.
func (c *Coordinator) startAppReporter(link *uplink.Client) {
	link.OnConnect(func(ctx context.Context) {
		// Run off the connection goroutine: this reads the entity store, and
		// the callback returns before the read and write loops start.
		go c.runAppReporter(ctx, link)
	})
}

// runAppReporter reports app state until the connection drops.
//
// It opens with a full snapshot, then samples on a timer, sending deltas for
// what changed and a fresh snapshot whenever the floor expires. The two share
// one derivation of app state so a delta and a snapshot can never disagree
// about an app's health.
//
// Failure here is deliberately non-fatal and unretried. Reporting is value
// flowing up to a value-add control plane, never a dependency flowing down: a
// cluster runs indefinitely with no cloud attached, and visibility simply
// stops updating until the link returns. Nothing the cluster does for its own
// users may degrade because this failed, so the loudest response available is
// a warning.
func (c *Coordinator) runAppReporter(ctx context.Context, link reportLink) {
	if c.appInfo == nil {
		return
	}

	c.reportLoop(ctx, link, c.deriveAppState, reportTiming{
		spread: connectSpread,
		sample: sampleInterval,
		floor:  snapshotFloor,
	})
}

// reportTiming is the loop's cadence, injectable so a test can drive the
// snapshot-or-delta decision without waiting out a real fifteen-minute floor.
type reportTiming struct {
	spread time.Duration
	sample time.Duration
	floor  time.Duration
}

// reportLoop is runAppReporter with its two dependencies handed in: where state
// comes from, and how fast the clock runs. The decision it makes each tick is
// the part worth testing and the part nothing else covers, since everything it
// calls is exercised directly elsewhere.
func (c *Coordinator) reportLoop(
	ctx context.Context,
	link reportLink,
	deriveState func(context.Context) (map[string]AppState, error),
	timing reportTiming,
) {
	// Wait out this cluster's share of the spread before the opening snapshot,
	// so a fleet that reconnected together does not report together.
	select {
	case <-ctx.Done():
		return
	case <-time.After(uplink.SpreadOnConnect(timing.spread)):
	}

	// Held per connection, not on the Coordinator. After a reconnect cloud's
	// state is unknown and the opening snapshot re-establishes all of it, so
	// carrying deltas across the gap would be reasoning from a baseline that
	// no longer describes anything.
	var lastSent map[string]AppState

	// A store that stays unhappy would otherwise warn on every sample tick for
	// as long as it lasts, which is the retry-loop-that-cannot-converge case
	// CLAUDE.md calls out: it drowns the tier it is in. Say it once, stay quiet
	// while it persists, and say how many times it happened once it clears, so
	// an operator gets the beginning and the end rather than the middle.
	failures := 0
	derive := func() (map[string]AppState, bool) {
		state, err := deriveState(ctx)
		if err != nil {
			failures++
			if failures == 1 {
				c.Log.Warn("failed to list apps for cloud report", "error", err)
			} else {
				c.Log.Debug("still failing to list apps for cloud report",
					"error", err, "failures", failures)
			}
			return nil, false
		}

		if failures > 0 {
			c.Log.Info("listing apps for cloud report recovered", "failures", failures)
			failures = 0
		}
		return state, true
	}

	if state, ok := derive(); ok {
		if c.sendAppSnapshot(ctx, link, state) == nil {
			lastSent = state
		}
	}

	ticker := time.NewTicker(timing.sample)
	defer ticker.Stop()

	lastSnapshot := time.Now()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}

		state, ok := derive()
		if !ok {
			continue
		}

		if lastSent == nil || removedAny(lastSent, state) || time.Since(lastSnapshot) >= timing.floor {
			if c.sendAppSnapshot(ctx, link, state) != nil {
				continue
			}
			lastSnapshot = time.Now()
			lastSent = state
			continue
		}

		if c.sendAppDeltas(ctx, link, lastSent, state) != nil {
			continue
		}
		lastSent = state
	}
}

// removedAny reports whether any app in the last report is gone now.
//
// A removal is the one change deltas cannot carry, so it is what promotes an
// ordinary sample tick into a full snapshot. Cloud learns about deletes only
// by comparing a completed epoch against what it holds, which means without
// this the floor would set how long a deleted app keeps appearing in the
// console — and the floor is tuned for a rare failure, not for something a
// user does on purpose and then goes looking for.
//
// Firing a whole snapshot to communicate a delete looks heavy-handed next to
// sending a "deleted" message, and that is the point: a message can be lost,
// and its loss leaves a phantom app in the console with nothing to correct it.
// Deriving deletes from a complete epoch has no such failure mode. This only
// changes when that epoch happens, never how deletes are decided.
//
// No debounce is needed. One tick sees every removal since the last report at
// once, so a mass deletion produces a single snapshot, and the sample interval
// already bounds how often this can fire.
func removedAny(previous, current map[string]AppState) bool {
	for name := range previous {
		if _, ok := current[name]; !ok {
			return true
		}
	}
	return false
}

// deriveAppState lists the cluster's apps and reduces them to the sample
// fields cloud stores, keyed by name.
// The caller owns the logging, because how loudly a failure should be reported
// depends on how many came before it, and only the loop knows that.
func (c *Coordinator) deriveAppState(ctx context.Context) (map[string]AppState, error) {
	apps, err := c.appInfo.ListApps(ctx)
	if err != nil {
		return nil, err
	}

	state := make(map[string]AppState, len(apps))
	for _, a := range apps {
		state[a.Name()] = appStateFrom(a)
	}
	return state, nil
}

func appStateFrom(a *app_v1alpha.AppInfo) AppState {
	return AppState{
		Name:             a.Name(),
		Health:           a.Health(),
		ReadyInstances:   a.ReadyInstances(),
		DesiredInstances: a.DesiredInstances(),
	}
}

// sendAppSnapshot streams a full epoch: every app in bounded batches, then the
// complete message that authorizes cloud to sweep.
//
// Batches use the blocking send. A snapshot is a sequence, and a silently
// dropped batch in a sequence does not look like a lost sample that the next
// report repairs — it looks exactly like those apps having been deleted.
func (c *Coordinator) sendAppSnapshot(ctx context.Context, link reportLink, state map[string]AppState) error {
	// Not MustNew. This runs on a goroutine of its own, so a panic here would
	// take the process down over a failure to report, and reporting is the one
	// thing in this file that must never cost the cluster anything. An entropy
	// failure is close to impossible on a real host, which is exactly why it
	// would be an absurd way to lose a runtime.
	id, err := ulid.New(ulid.Now(), rand.Reader)
	if err != nil {
		c.Log.Warn("failed to mint app snapshot epoch", "error", err)
		return err
	}

	epoch := id.String()
	observedAt := time.Now().UTC()

	batch := make([]AppState, 0, snapshotBatchSize)
	flush := func() error {
		if len(batch) == 0 {
			return nil
		}
		// Hand off the slice and start a fresh one rather than truncating and
		// refilling. Reusing the backing array would leave every batch already
		// queued pointing at storage the next batch overwrites, which is only
		// invisible today because the sender marshals before it returns.
		apps := batch
		batch = make([]AppState, 0, snapshotBatchSize)

		return link.SendMessageBlocking(ctx, TypeAppSnapshot, AppSnapshot{
			Epoch:      epoch,
			ObservedAt: observedAt,
			Apps:       apps,
		})
	}

	for _, app := range state {
		batch = append(batch, app)
		if len(batch) < snapshotBatchSize {
			continue
		}
		if err := flush(); err != nil {
			c.Log.Warn("failed to queue app snapshot batch", "epoch", epoch, "error", err)
			return err
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(batchPause):
		}
	}

	if err := flush(); err != nil {
		c.Log.Warn("failed to queue app snapshot batch", "epoch", epoch, "error", err)
		return err
	}

	err = link.SendMessageBlocking(ctx, TypeAppSnapshotComplete, AppSnapshotComplete{
		Epoch:    epoch,
		AppCount: len(state),
	})
	if err != nil {
		// Without the complete message cloud sweeps nothing, which is the safe
		// outcome: the apps it received still upsert, and the next epoch closes
		// properly. Worth a warning because deletes stop propagating until then.
		c.Log.Warn("failed to close app snapshot epoch", "epoch", epoch, "error", err)
		return err
	}

	// Queued, not sent. The blocking send tightened the claim but did not close
	// it: the envelope sits in the outbox until the write loop drains it, and a
	// reconnect discards whatever is still there. Saying "sent" would mislead
	// whoever reads this line against an empty console.
	c.Log.Info("queued app snapshot for cloud", "epoch", epoch, "apps", len(state))
	return nil
}

// sendAppDeltas reports the apps whose samples moved since the last report.
//
// Only changes go on the wire, which is what keeps a 15-second sampling
// interval from becoming a 15-second write cycle on an idle cluster. Apps that
// appeared are deltas too; apps that disappeared are not, because a delta
// cannot express a delete. Removal rides on a snapshot's sweep instead — see
// removedAny, which is what makes sure that snapshot comes promptly rather
// than whenever the floor next expires.
func (c *Coordinator) sendAppDeltas(ctx context.Context, link reportLink, previous, current map[string]AppState) error {
	observedAt := time.Now().UTC()

	changed := make([]AppState, 0, len(current))
	for name, app := range current {
		if before, ok := previous[name]; ok && before == app {
			continue
		}
		changed = append(changed, app)
	}

	if len(changed) == 0 {
		return nil
	}

	// Deltas go out in one message however many apps moved. They are bounded by
	// what changed in a single sampling interval rather than by the size of the
	// cluster, so unlike a snapshot there is nothing here to batch.
	err := link.SendMessageBlocking(ctx, TypeAppStatus, AppStatus{
		ObservedAt: observedAt,
		Apps:       changed,
	})
	if err != nil {
		c.Log.Warn("failed to queue app status deltas", "apps", len(changed), "error", err)
		return err
	}

	// Debug rather than Info: this fires on the sample tick whenever anything
	// moved, and an operator cannot act on the fact that three apps changed.
	// The snapshot log above marks the state transitions worth reading.
	c.Log.Debug("queued app status deltas for cloud", "apps", len(changed))
	return nil
}
