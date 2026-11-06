// Package certrevocation keeps an rpc.CertRevocations in step with the
// certificate_revocation entities in the store.
package certrevocation

import (
	"context"
	"fmt"
	"log/slog"

	"miren.dev/runtime/api/core/core_v1alpha"
	"miren.dev/runtime/api/entityserver/entityserver_v1alpha"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/entity/indexwatch"
	"miren.dev/runtime/pkg/rpc"
)

const loadPageSize = 500

var kindIndex = entity.Ref(entity.EntityKind, core_v1alpha.KindCertificateRevocation)

// Load installs every stored revocation into set, leaving it loaded. Any error
// leaves it unloaded and should stop the server: a record that cannot be read
// might be the only thing keeping a revoked certificate out, so nothing short of
// every record counts as loaded. Clearing a bad record then means repairing it in
// etcd, since the API is not up to do it.
//
// Load runs before the entity server exists, which is why it takes the store
// rather than an RPC client.
func Load(ctx context.Context, log *slog.Logger, store entity.Store, set *rpc.CertRevocations) error {
	var serials []string
	cursor := ""
	for {
		page, err := store.ListIndexEntitiesPage(ctx, kindIndex, cursor, loadPageSize)
		if err != nil {
			return fmt.Errorf("listing certificate revocations: %w", err)
		}
		for i, e := range page.Entities {
			if e == nil {
				if page.Undecodable[page.Ids[i]] {
					return fmt.Errorf("certificate revocation %s is unreadable", page.Ids[i])
				}
				continue
			}
			serial, err := serialOf(e)
			if err != nil {
				return fmt.Errorf("certificate revocation %s: %w", e.Id(), err)
			}
			serials = append(serials, serial)
		}
		cursor = page.Cursor
		if cursor == "" {
			break
		}
	}

	set.Load(serials)
	log.Info("certificate revocations loaded", "count", len(serials))
	return nil
}

// Watch keeps set current until ctx ends. On the coordinator it picks up writes
// that bypassed the Revoke handler. On a runner it is the only source, so the
// runner refuses certificates until the first snapshot arrives, and a revocation
// reaches it a moment after Revoke returns.
func Watch(ctx context.Context, log *slog.Logger, eac *entityserver_v1alpha.EntityAccessClient, set *rpc.CertRevocations) {
	w := indexwatch.New(eac, kindIndex, indexwatch.Options{Logger: log})
	if err := w.Start(ctx); err != nil {
		log.Error("failed to watch certificate revocations", "error", err)
		return
	}
	defer w.Stop()

	follow(ctx, log, w.Updates(), set)
}

// follow applies store changes to set, and only ever adds. A snapshot can predate
// a revocation already enforced and listings omit records that will not decode,
// while a delete event can be older than a write-through Revoke of the same
// serial, so neither can safely lift anything. Deleting a revocation by hand
// takes effect when each process restarts.
func follow(ctx context.Context, log *slog.Logger, updates <-chan indexwatch.Event, set *rpc.CertRevocations) {
	for {
		select {
		case <-ctx.Done():
			return
		case ev, ok := <-updates:
			if !ok {
				return
			}
			switch ev.Type {
			case indexwatch.EventSync:
				serials := make([]string, 0, len(ev.Entities))
				complete := true
				for _, e := range ev.Entities {
					serial, err := serialOf(e)
					if err != nil {
						log.Error("unusable certificate revocation", "id", e.Id(), "error", err)
						complete = false
						continue
					}
					serials = append(serials, serial)
				}
				// A first snapshot with a bad record leaves a runner unloaded, the
				// same outcome as the coordinator failing to boot on one. A set
				// already loaded keeps enforcing rather than lock out a live node.
				if !complete && !set.Loaded() {
					for _, serial := range serials {
						set.Add(serial)
					}
					continue
				}
				set.Load(serials)
				log.Info("certificate revocations synced", "count", len(serials))
			case indexwatch.EventAdded, indexwatch.EventUpdated:
				if ev.Entity == nil {
					continue
				}
				serial, err := serialOf(ev.Entity)
				if err != nil {
					log.Warn("ignoring certificate revocation", "id", ev.Id, "error", err)
					continue
				}
				set.Add(serial)
				log.Info("certificate revocation applied", "serial", serial)
			case indexwatch.EventDeleted:
			}
		}
	}
}

func serialOf(e *entity.Entity) (string, error) {
	var revocation core_v1alpha.CertificateRevocation
	revocation.Decode(e)
	return rpc.ParseCertSerial(revocation.Serial)
}
