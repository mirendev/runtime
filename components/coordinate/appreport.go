package coordinate

import (
	"context"
	"time"

	"miren.dev/runtime/pkg/uplink"
)

// TypeAppSnapshot carries the cluster's full current app set to cloud for
// visibility reporting, one of the app.* family described in RFD-94.
//
// The type and its payload live with the reporter rather than in pkg/uplink
// because the link is deliberately payload-agnostic: it moves opaque envelopes
// and understands no apps. A tenant owns its own wire shape.
const TypeAppSnapshot = "app.snapshot"

// AppSnapshot is the cluster's view of every app it currently runs, sent on
// each (re)connect. Cloud treats it as ground truth for that cluster.
//
// This is the ephemeral tier of RFD-94's volatility spectrum: the weakest
// durability contract in the design. Delivery is best-effort because loss
// self-heals — a dropped snapshot is repaired by the next one rather than
// retried, so nothing here is worth an ack.
//
// Two things are deliberately absent and belong to the follow-up that adds
// sync correctness:
//
//   - No epoch. Cloud upserts what it receives and never sweeps, so an app
//     deleted in the cluster lingers in cloud until epochs arrive and make
//     mark-and-sweep safe. Sweeping without an epoch would soft-delete apps
//     that simply haven't landed yet, since a real snapshot arrives in
//     batches rather than atomically.
//   - No instance counts or resource usage. Health alone proves the seam.
type AppSnapshot struct {
	// ObservedAt is the runtime's own clock reading for when this state was
	// true. Cloud reconciles it against its own clock using the offset from
	// the link's time sync, and applies last-writer-wins with it, so a delayed
	// message cannot clobber fresher state.
	ObservedAt time.Time `json:"observed_at"`

	Apps []AppState `json:"apps"`
}

// AppState is one app's current sample within a snapshot.
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
}

// startAppReporter arranges for the cluster's app state to be reported to
// cloud on every (re)connection of the uplink.
//
// Reconnect is the trigger rather than a timer because the uplink discards
// anything queued while it was down. Whatever cloud missed during an outage
// never reached the wire at all, so it can only be recovered by re-deriving
// the current state from the entity store once the link is back. That makes
// the source, not the pipe, responsible for completeness — see RFD-94.
func (c *Coordinator) startAppReporter(link *uplink.Client) {
	link.OnConnect(func(ctx context.Context) {
		// Run off the connection goroutine: this reads the entity store, and
		// the callback returns before the read and write loops start.
		go c.reportAppSnapshot(ctx)
	})
}

// reportAppSnapshot sends the cluster's current app set to cloud.
//
// Failure here is deliberately non-fatal and unretried. Reporting is value
// flowing up to a value-add control plane, never a dependency flowing down:
// a cluster runs indefinitely with no cloud attached, and visibility simply
// stops updating until the link returns. Nothing the cluster does for its own
// users may degrade because this failed, so the loudest response available is
// a warning.
func (c *Coordinator) reportAppSnapshot(ctx context.Context) {
	if c.appInfo == nil || c.uplink == nil {
		return
	}

	apps, err := c.appInfo.ListApps(ctx)
	if err != nil {
		c.Log.Warn("failed to list apps for cloud snapshot", "error", err)
		return
	}

	snapshot := AppSnapshot{
		ObservedAt: time.Now().UTC(),
		Apps:       make([]AppState, 0, len(apps)),
	}

	for _, a := range apps {
		snapshot.Apps = append(snapshot.Apps, AppState{
			Name:   a.Name(),
			Health: a.Health(),
		})
	}

	if err := c.uplink.SendMessage(TypeAppSnapshot, snapshot); err != nil {
		c.Log.Warn("failed to queue app snapshot for cloud", "error", err)
		return
	}

	// Queued, not reported: SendMessage returns nil whether or not the envelope
	// made it into the outbox, which drops on overflow and is discarded on
	// reconnect. Claiming we reported would assert something we cannot observe
	// from here.
	c.Log.Info("queued app snapshot for cloud", "apps", len(snapshot.Apps))
}
