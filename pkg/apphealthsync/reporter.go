// Package apphealthsync samples runtime-owned health for cloud's ephemeral cache.
package apphealthsync

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"miren.dev/runtime/pkg/apphealth"
	"miren.dev/runtime/pkg/uplink"
)

const (
	defaultBackground = 5 * time.Minute
	minInterval       = time.Second
	maxInterval       = time.Hour
	maxLease          = time.Minute
	maxBatch          = 100
)

// Source shares the classifier used by the app list.
type Source interface {
	ListAppHealth(context.Context) ([]apphealth.State, error)
}

type Link interface {
	OfferCapability(uplink.CapabilityOffer)
	OnSession(func(context.Context, uplink.Session))
	Handle(string, uplink.MessageHandler)
	SendMessageBlocking(context.Context, string, any) error
}

type samplingSession struct {
	id     string
	ctx    context.Context
	demand chan Demand
}

type Reporter struct {
	log     *slog.Logger
	source  Source
	mu      sync.Mutex
	session *samplingSession
}

func NewReporter(log *slog.Logger, source Source) *Reporter {
	return &Reporter{log: log, source: source}
}

func (r *Reporter) Register(_ context.Context, link Link) error {
	link.OfferCapability(uplink.CapabilityOffer{Name: uplink.CapabilityAppHealth, Versions: []uint{Version1}})
	link.Handle(TypeDemand, r.handleDemand)
	link.OnSession(func(ctx context.Context, session uplink.Session) {
		selection, ok := session.Capability(uplink.CapabilityAppHealth)
		if !ok {
			return
		}
		background := defaultBackground
		var config Config
		if len(selection.Config) > 0 {
			if err := json.Unmarshal(selection.Config, &config); err != nil {
				r.log.Warn("invalid app health configuration", "error", err)
				return
			}
			if config.BackgroundSeconds > 0 {
				background = time.Duration(min(config.BackgroundSeconds, 3600)) * time.Second
			}
		}
		active := &samplingSession{id: session.ID, ctx: ctx, demand: make(chan Demand, 1)}
		r.mu.Lock()
		r.session = active
		r.mu.Unlock()
		go r.run(active, link, background)
	})
	return nil
}

func (r *Reporter) handleDemand(_ context.Context, raw json.RawMessage) error {
	var demand Demand
	if err := json.Unmarshal(raw, &demand); err != nil {
		return err
	}
	if demand.IntervalSeconds < 1 || demand.IntervalSeconds > int(maxInterval/time.Second) || demand.LeaseSeconds < 1 || demand.LeaseSeconds > int(maxLease/time.Second) {
		return fmt.Errorf("invalid app health sampling demand")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	active := r.session
	if active == nil || active.ctx.Err() != nil || active.id != demand.SessionID {
		return nil
	}
	// Keep the latest request without blocking the shared uplink read loop.
	select {
	case <-active.demand:
	default:
	}
	active.demand <- demand
	return nil
}

func (r *Reporter) run(active *samplingSession, link Link, background time.Duration) {
	ctx := active.ctx
	r.log.Info("app health sampling started", "background_interval", background)
	// Spread unsolicited connection samples; viewing a panel bypasses this wait.
	next := time.Now().Add(uplink.SpreadOnConnect(time.Minute))
	var last, until time.Time
	interval := background
	timer := time.NewTimer(time.Until(next))
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case demand := <-active.demand:
			now := time.Now()
			wasActive := now.Before(until)
			interval = min(background, time.Duration(demand.IntervalSeconds)*time.Second)
			until = now.Add(time.Duration(demand.LeaseSeconds) * time.Second)
			if !wasActive {
				// A returning viewer gets a fresh sample, bounded even if short leases
				// and many viewers repeatedly enter fast mode.
				next = minTime(next, maxTime(now, last.Add(minInterval)))
			} else {
				next = minTime(next, last.Add(interval))
			}
		case <-timer.C:
			now := time.Now()
			if !until.IsZero() && !now.Before(until) {
				until = time.Time{}
				interval = background
				next = last.Add(background)
			}
			if !now.Before(next) {
				last = now
				r.sample(ctx, link)
				next = time.Now().Add(interval)
			}
		}
		wake := next
		if !until.IsZero() {
			wake = minTime(wake, until)
		}
		timer.Reset(time.Until(wake))
	}
}

func (r *Reporter) sample(ctx context.Context, link Link) {
	observed := time.Now().UTC()
	apps, err := r.source.ListAppHealth(ctx)
	if err != nil {
		r.log.Debug("failed to sample app health", "error", err)
		return
	}
	for start := 0; start < len(apps); start += maxBatch {
		batch := SampleBatch{ObservedAt: observed, Apps: make([]Sample, 0, min(maxBatch, len(apps)-start))}
		for _, app := range apps[start:min(start+maxBatch, len(apps))] {
			batch.Apps = append(batch.Apps, Sample{Name: app.Name, Health: app.Health, ReadyInstances: app.ReadyInstances, DesiredInstances: app.DesiredInstances})
		}
		if err := link.SendMessageBlocking(ctx, TypeSamples, batch); err != nil {
			r.log.Debug("failed to queue app health sample", "error", err)
			return
		}
	}
}

func minTime(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}
func maxTime(a, b time.Time) time.Time {
	if a.After(b) {
		return a
	}
	return b
}
