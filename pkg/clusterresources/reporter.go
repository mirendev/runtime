// Package clusterresources samples whole-host CPU, memory, and storage for
// cloud's ephemeral cache, over the negotiated uplink session.
//
// This is measured state with no entity behind it, so it rides a purpose-built
// capability rather than entity sync, at the cadence cloud asks for. It is the
// resource half of the legacy status report; when cloud selects this
// capability the poll has nothing left to say about resources.
package clusterresources

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"miren.dev/runtime/pkg/uplink"
)

const (
	// defaultInterval applies when cloud selects the capability without
	// saying how often; it matches the poll this replaces.
	defaultInterval = 5 * time.Minute
	minInterval     = 10 * time.Second
	maxInterval     = time.Hour
)

// Source takes one reading. CloudControl implements it over sysstats.
type Source interface {
	ResourceSample() Sample
}

// Link is the slice of the uplink client the reporter uses.
type Link interface {
	OfferCapability(uplink.CapabilityOffer)
	OnSession(func(context.Context, uplink.Session))
	SendMessageBlocking(context.Context, string, any) error
}

type Reporter struct {
	log    *slog.Logger
	source Source
}

func NewReporter(log *slog.Logger, source Source) *Reporter {
	return &Reporter{log: log, source: source}
}

// Register offers the capability and, on each session that selects it,
// samples at the interval cloud configured until the session ends.
func (r *Reporter) Register(_ context.Context, link Link) error {
	link.OfferCapability(uplink.CapabilityOffer{Name: uplink.CapabilityClusterResources, Versions: []uint{Version1}})
	link.OnSession(func(ctx context.Context, session uplink.Session) {
		selection, ok := session.Capability(uplink.CapabilityClusterResources)
		if !ok {
			return
		}
		interval := defaultInterval
		if len(selection.Config) > 0 {
			var config Config
			if err := json.Unmarshal(selection.Config, &config); err != nil {
				r.log.Warn("invalid cluster resources configuration", "error", err)
				return
			}
			if config.IntervalSeconds > 0 {
				interval = min(maxInterval, max(minInterval, time.Duration(config.IntervalSeconds)*time.Second))
			}
		}
		go r.run(ctx, link, interval)
	})
	return nil
}

func (r *Reporter) run(ctx context.Context, link Link, interval time.Duration) {
	r.log.Info("cluster resource sampling started", "interval", interval)
	sample := func() {
		reading := r.source.ResourceSample()
		if reading.ObservedAt.IsZero() {
			reading.ObservedAt = time.Now().UTC()
		}
		if err := link.SendMessageBlocking(ctx, TypeSample, reading); err != nil {
			// Transport failures are expected on disconnect.
			r.log.Debug("failed to queue cluster resource sample", "error", err)
		}
	}
	sample()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			sample()
		}
	}
}
