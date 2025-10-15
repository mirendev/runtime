package entity

import (
	"context"
	"fmt"
	"log/slog"
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
	if opts.Prefix == "" {
		opts.Prefix = "/entity/"
	}

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

			// Successfully decoded as new format - skip
			log.Debug("entity already in new format", "key", key, "id", newEnt.Id())
			skipped++
			continue
		}

		// Check if this entity actually needs migration
		// An entity needs migration if it has old-style struct fields
		needsMigration := oldEnt.ID != "" || oldEnt.Revision != 0 || oldEnt.CreatedAt != 0 || oldEnt.UpdatedAt != 0

		if !needsMigration {
			log.Debug("entity has no old-style fields, skipping", "key", key)
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
		newEnt := &Entity{
			Attrs: oldEnt.Attrs,
		}

		// Track whether we added db/id or it already existed
		var addedDBId bool

		// Add ID as attribute if present and not already in attributes
		if oldEnt.ID != "" {
			if _, ok := newEnt.Get(DBId); !ok {
				newEnt.Attrs = append(newEnt.Attrs, Ref(DBId, Id(oldEnt.ID)))
				log.Debug("added db/id attribute", "key", key, "id", oldEnt.ID)
				addedDBId = true
			} else {
				log.Debug("db/id already exists in attributes", "key", key)
			}
		}

		// Add Revision as attribute if present and not already in attributes
		if oldEnt.Revision != 0 {
			if _, ok := newEnt.Get(Revision); !ok {
				newEnt.Attrs = append(newEnt.Attrs, Int64(Revision, oldEnt.Revision))
				log.Debug("added db/revision attribute", "key", key, "revision", oldEnt.Revision)
			} else {
				log.Debug("db/revision already exists in attributes", "key", key)
			}
		}

		// Add CreatedAt as attribute if present and not already in attributes
		if oldEnt.CreatedAt != 0 {
			createdAt := time.Unix(0, oldEnt.CreatedAt*int64(time.Millisecond))
			if _, ok := newEnt.Get(CreatedAt); !ok {
				newEnt.Attrs = append(newEnt.Attrs, Time(CreatedAt, createdAt))
				log.Debug("added db/created-at attribute", "key", key, "created_at", createdAt)
			} else {
				log.Debug("db/created-at already exists in attributes", "key", key)
			}
		}

		// Add UpdatedAt as attribute if present and not already in attributes
		if oldEnt.UpdatedAt != 0 {
			updatedAt := time.Unix(0, oldEnt.UpdatedAt*int64(time.Millisecond))
			if _, ok := newEnt.Get(UpdatedAt); !ok {
				newEnt.Attrs = append(newEnt.Attrs, Time(UpdatedAt, updatedAt))
				log.Debug("added db/updated-at attribute", "key", key, "updated_at", updatedAt)
			} else {
				log.Debug("db/updated-at already exists in attributes", "key", key)
			}
		}

		// Sort attributes for consistency
		newEnt.Attrs = SortedAttrs(newEnt.Attrs)

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

		// Verify critical fields match
		// Only check ID if we added it from the struct field
		// (if db/id already existed in attributes, it takes precedence)
		if addedDBId && verifyEnt.Id() != oldEnt.ID {
			log.Error("ID mismatch after migration",
				"key", key,
				"old", oldEnt.ID,
				"new", verifyEnt.Id())
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
