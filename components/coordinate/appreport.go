package coordinate

import (
	"context"
	"time"

	"miren.dev/runtime/pkg/anywhere"
)

// startAppReporter arranges for the cluster's app state to be reported to
// cloud on every (re)connection of the uplink.
//
// Reconnect is the trigger rather than a timer because the uplink discards
// anything queued while it was down. Whatever cloud missed during an outage
// never reached the wire at all, so it can only be recovered by re-deriving
// the current state from the entity store once the link is back. That makes
// the source, not the pipe, responsible for completeness — see RFD-94.
func (c *Coordinator) startAppReporter(conn *anywhere.Connector) {
	conn.OnConnect(func(ctx context.Context) {
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
	if c.appInfo == nil || c.anywhere == nil {
		return
	}

	apps, err := c.appInfo.ListApps(ctx)
	if err != nil {
		c.Log.Warn("failed to list apps for cloud snapshot", "error", err)
		return
	}

	snapshot := anywhere.AppSnapshot{
		ObservedAt: time.Now().UTC(),
		Apps:       make([]anywhere.AppState, 0, len(apps)),
	}

	for _, a := range apps {
		snapshot.Apps = append(snapshot.Apps, anywhere.AppState{
			Name:   a.Name(),
			Health: a.Health(),
		})
	}

	if err := c.anywhere.SendMessage(anywhere.TypeAppSnapshot, snapshot); err != nil {
		c.Log.Warn("failed to send app snapshot to cloud", "error", err)
		return
	}

	c.Log.Info("reported app snapshot to cloud", "apps", len(snapshot.Apps))
}
