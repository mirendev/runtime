package entity

import (
	"context"
	"fmt"
	"log/slog"
	"path"
	"strings"
	"time"

	"github.com/fxamacker/cbor/v2"
	clientv3 "go.etcd.io/etcd/client/v3"
	"miren.dev/runtime/pkg/entity/types"
)

// OldEntity represents the entity format before the migration to attribute-based metadata.
// In the old format, ID, Revision, CreatedAt, and UpdatedAt were struct fields in the CBOR encoding.
type OldEntity struct {
	ID        types.Id `cbor:"id"`
	Revision  int64    `cbor:"revision,omitempty"`
	CreatedAt int64    `cbor:"created_at"` // milliseconds since epoch
	UpdatedAt int64    `cbor:"updated_at"` // milliseconds since epoch
	Attrs     []Attr   `cbor:"attrs"`
}

func (e *OldEntity) Get(attr Id) (Attr, bool) {
	for _, a := range e.Attrs {
		if a.ID == attr {
			return a, true
		}
	}
	return Attr{}, false
}

// MigrateOptions configures the migration behavior
type MigrateOptions struct {
	// DryRun prevents writing changes back to etcd
	DryRun bool
	// Prefix is the etcd key prefix to scan (default: "/entity/")
	Prefix string
}

// MigrateEntityStore migrates entities from the old CBOR format (with struct fields)
// to the new format (with attribute-based metadata).
//
// This migration:
//   - Reads all entities from etcd under the given prefix
//   - Detects entities in the old format (ID/Revision/CreatedAt/UpdatedAt as struct fields)
//   - Converts them to the new format (ID/Revision/CreatedAt/UpdatedAt as attributes)
//   - Writes the migrated entities back to etcd
//
// Returns the number of entities migrated, skipped, and any error encountered.
func MigrateEntityStore(ctx context.Context, log *slog.Logger, client *clientv3.Client, opts MigrateOptions) (migrated int, skipped int, err error) {
	opts.Prefix = path.Join(opts.Prefix, "entity")

	log.Info("starting entity migration", "prefix", opts.Prefix, "dry_run", opts.DryRun)

	// List all entities
	resp, err := client.Get(ctx, opts.Prefix, clientv3.WithPrefix())
	if err != nil {
		return 0, 0, fmt.Errorf("failed to list entities: %w", err)
	}

	log.Info("found entities to scan", "count", len(resp.Kvs))

	var errors int

	for _, kv := range resp.Kvs {
		key := string(kv.Key)

		if strings.Contains(key, "/session/") {
			continue
		}

		// Try to decode as old format first
		var oldEnt OldEntity
		err := cbor.Unmarshal(kv.Value, &oldEnt)
		if err != nil {
			// If we can't decode as old format, check if it's already in new format
			var newEnt Entity
			err2 := Decode(kv.Value, &newEnt)
			if err2 != nil {
				log.Error("failed to decode entity in both formats",
					"key", key,
					"old_error", err,
					"new_error", err2)
				errors++
				continue
			}

			skipped++
			continue
		}

		// Check if this entity actually needs migration
		// An entity needs migration if it has old-style struct fields
		needsMigration := oldEnt.ID != "" || oldEnt.Revision != 0 || oldEnt.CreatedAt != 0 || oldEnt.UpdatedAt != 0

		if !needsMigration {
			skipped++
			continue
		}

		log.Info("migrating entity",
			"key", key,
			"id", oldEnt.ID,
			"revision", oldEnt.Revision,
			"created_at", time.Unix(0, oldEnt.CreatedAt*int64(time.Millisecond)),
			"updated_at", time.Unix(0, oldEnt.UpdatedAt*int64(time.Millisecond)),
			"attrs_count", len(oldEnt.Attrs))

		// Create new entity starting with existing attributes
		newEnt := New(oldEnt.Attrs)

		// Add ID as attribute if present and not already in attributes
		if oldEnt.ID != "" {
			if _, ok := oldEnt.Get(DBId); !ok {
				if newEnt.SetID(oldEnt.ID) {
					log.Debug("db/id updated", "new_id", oldEnt.ID)
				} else {
					log.Debug("db/id not updated, already set", "key", key, "existing", newEnt.Id(), "candidate", oldEnt.ID)
				}
			}
		}

		if oldEnt.Revision != 0 {
			if newEnt.Set(Int64(Revision, oldEnt.Revision)) {
				log.Debug("db/revision updated", "revision", oldEnt.Revision)
			} else {
				log.Debug("added db/revision attribute", "key", key, "revision", oldEnt.Revision)
			}
		}

		if oldEnt.CreatedAt != 0 {
			createdAt := time.Unix(0, oldEnt.CreatedAt*int64(time.Millisecond))
			if newEnt.SetCreatedAt(createdAt) {
				log.Debug("db/created-at updated")
			} else {
				log.Debug("added db/created-at attribute", "key", key, "created_at", createdAt)
			}
		} else if _, ok := oldEnt.Get(CreatedAt); !ok {
			newEnt.Remove(CreatedAt)
		}

		if oldEnt.UpdatedAt != 0 {
			updatedAt := time.Unix(0, oldEnt.UpdatedAt*int64(time.Millisecond))

			if newEnt.SetUpdatedAt(updatedAt) {
				log.Debug("db/updated-at updated")
			} else {
				log.Debug("db/updated-at not updated, already newer", "key", key, "existing", newEnt.GetUpdatedAt(), "candidate", updatedAt)
			}
		} else if _, ok := oldEnt.Get(UpdatedAt); !ok {
			newEnt.Remove(UpdatedAt)
		}

		// Migrate old timestamp attribute names
		// db/entity.created-at (Int) -> db/entity.created (Time)
		// db/entity.updated-at (Int) -> db/entity.updated (Time)
		oldCreatedAtId := Id("db/entity.created-at")
		if oldAttr, ok := newEnt.Get(oldCreatedAtId); ok && oldAttr.Value.Kind() == KindInt64 {
			createdAtMs := oldAttr.Value.Int64()
			createdAt := time.Unix(0, createdAtMs*int64(time.Millisecond))
			newEnt.SetCreatedAt(createdAt)
			newEnt.Remove(oldCreatedAtId)

			log.Info("migrated db/entity.created-at (Int) to db/entity.created (Time)",
				"key", key,
				"old_value_ms", createdAtMs,
				"new_value", createdAt)
		}

		oldUpdatedAtId := Id("db/entity.updated-at")
		if oldAttr, ok := newEnt.Get(oldUpdatedAtId); ok && oldAttr.Value.Kind() == KindInt64 {
			updatedAtMs := oldAttr.Value.Int64()
			updatedAt := time.Unix(0, updatedAtMs*int64(time.Millisecond))
			newEnt.SetUpdatedAt(updatedAt)
			newEnt.Remove(oldUpdatedAtId)

			log.Info("migrated db/entity.updated-at (Int) to db/entity.updated (Time)",
				"key", key,
				"old_value_ms", updatedAtMs,
				"new_value", updatedAt)
		}

		// Fix a bug where we'd sometimes set a session attribute on entities
		newEnt.Remove(AttrSession)

		// Sort attributes for consistency
		newEnt.attrs = SortedAttrs(newEnt.attrs)

		// Encode new entity
		newData, err := Encode(newEnt)
		if err != nil {
			log.Error("failed to encode new entity", "key", key, "error", err)
			errors++
			continue
		}

		// Verify we can decode it back
		var verifyEnt Entity
		err = Decode(newData, &verifyEnt)
		if err != nil {
			log.Error("failed to verify encoded entity", "key", key, "error", err)
			errors++
			continue
		}

		if oldEnt.Revision != 0 && verifyEnt.GetRevision() != oldEnt.Revision {
			log.Error("revision mismatch after migration",
				"key", key,
				"old", oldEnt.Revision,
				"new", verifyEnt.GetRevision())
			errors++
			continue
		}

		if opts.DryRun {
			log.Info("dry-run: would update entity",
				"key", key,
				"id", newEnt.Id(),
				"old_size", len(kv.Value),
				"new_size", len(newData))
			migrated++
			continue
		}

		// Write back to etcd
		_, err = client.Put(ctx, key, string(newData))
		if err != nil {
			log.Error("failed to write migrated entity", "key", key, "error", err)
			errors++
			continue
		}

		log.Info("successfully migrated entity",
			"key", key,
			"id", newEnt.Id(),
			"old_size", len(kv.Value),
			"new_size", len(newData))
		migrated++
	}

	if errors > 0 {
		log.Warn("migration completed with errors",
			"total", len(resp.Kvs),
			"migrated", migrated,
			"skipped", skipped,
			"errors", errors)
		return migrated, skipped, fmt.Errorf("migration completed with %d errors", errors)
	}

	log.Info("migration completed successfully",
		"total", len(resp.Kvs),
		"migrated", migrated,
		"skipped", skipped)

	return migrated, skipped, nil
}
