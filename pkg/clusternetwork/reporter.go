// Package clusternetwork tells cloud how this cluster can be reached, over the
// negotiated uplink session.
//
// The facts are measured on the box (advertised addresses, the CA the API
// presents, netcheck's verdict, whether the server runs in a container) and
// describe the link rather than any stored entity, which is why they ride a
// purpose-built capability instead of entity sync. They are the same facts
// the legacy status poll carries; when cloud selects this capability the
// poll has nothing left to say about the network.
package clusternetwork

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"miren.dev/runtime/pkg/uplink"
)

const (
	// checkInterval is how often the facts are recomputed and compared with
	// what was last sent. Computing them is cheap; the hourly netcheck behind
	// the verdict is the source's own concern and cached there.
	checkInterval = time.Minute
	// resendInterval bounds how long cloud goes without hearing the facts
	// even when nothing changed, so a report cloud lost is repaired.
	resendInterval = time.Hour
)

// Source produces the current facts. CloudControl implements it.
type Source interface {
	NetworkFacts(context.Context) Report
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

	// now and checkEvery are injectable so tests can drive the loop.
	now         func() time.Time
	checkEvery  time.Duration
	resendEvery time.Duration
}

func NewReporter(log *slog.Logger, source Source) *Reporter {
	return &Reporter{log: log, source: source, now: time.Now, checkEvery: checkInterval, resendEvery: resendInterval}
}

// Register offers the capability and, on each session that selects it,
// reports the facts immediately, again whenever they change, and at least
// once an hour regardless.
func (r *Reporter) Register(_ context.Context, link Link) error {
	link.OfferCapability(uplink.CapabilityOffer{Name: uplink.CapabilityClusterNetwork, Versions: []uint{Version1}})
	link.OnSession(func(ctx context.Context, session uplink.Session) {
		if _, ok := session.Capability(uplink.CapabilityClusterNetwork); !ok {
			return
		}
		go r.run(ctx, link)
	})
	return nil
}

func (r *Reporter) run(ctx context.Context, link Link) {
	r.log.Info("cluster network reporting started")
	var lastSent []byte
	var sentAt time.Time
	report := func() {
		facts := r.source.NetworkFacts(ctx)
		if ctx.Err() != nil {
			return
		}
		encoded, err := json.Marshal(facts)
		if err != nil {
			r.log.Warn("failed to encode cluster network report", "error", err)
			return
		}
		now := r.now()
		if bytes.Equal(encoded, lastSent) && now.Sub(sentAt) < r.resendEvery {
			return
		}
		if err := link.SendMessageBlocking(ctx, TypeReport, facts); err != nil {
			// Transport failures are expected on disconnect; the next session
			// starts with a fresh report anyway.
			r.log.Debug("failed to queue cluster network report", "error", err)
			return
		}
		lastSent, sentAt = encoded, now
	}

	report()
	ticker := time.NewTicker(r.checkEvery)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			report()
		}
	}
}
