package entity

import (
	"context"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/fxamacker/cbor/v2"
	"github.com/stretchr/testify/require"
)

func TestMigrateEntityStore(t *testing.T) {
	r := require.New(t)

	client := setupTestEtcd(t)
	ctx := context.Background()

	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}))

	prefix := "/test-migrate-entities/"

	// Create some entities in old format
	oldEnt1 := OldEntity{
		ID:        "app/test-app",
		Revision:  5,
		CreatedAt: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC).UnixMilli(),
		UpdatedAt: time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC).UnixMilli(),
		Attrs: []Attr{
			String("app/name", "Test App"),
		},
	}

	oldEnt2 := OldEntity{
		ID:        "project/test-project",
		Revision:  3,
		CreatedAt: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC).UnixMilli(),
		UpdatedAt: time.Date(2024, 1, 3, 0, 0, 0, 0, time.UTC).UnixMilli(),
		Attrs: []Attr{
			String("project/owner", "test@example.com"),
		},
	}

	// Encode and store old entities
	data1, err := cbor.Marshal(oldEnt1)
	r.NoError(err)
	_, err = client.Put(ctx, prefix+"entity/app/test-app", string(data1))
	r.NoError(err)

	data2, err := cbor.Marshal(oldEnt2)
	r.NoError(err)
	_, err = client.Put(ctx, prefix+"entity/project/test-project", string(data2))
	r.NoError(err)

	// Create one entity that's already in new format
	newEnt := New(
		Ref(DBId, "sandbox/already-migrated"),
		Int64(Revision, 1),
		Time(CreatedAt, time.Date(2024, 1, 4, 0, 0, 0, 0, time.UTC)),
		Time(UpdatedAt, time.Date(2024, 1, 4, 0, 0, 0, 0, time.UTC)),
		String("sandbox/status", "running"),
	)
	data3, err := Encode(newEnt)
	r.NoError(err)
	_, err = client.Put(ctx, prefix+"entity/sandbox/already-migrated", string(data3))
	r.NoError(err)

	// Test dry run first
	t.Run("dry run", func(t *testing.T) {
		migrated, skipped, err := MigrateEntityStore(ctx, log, client, MigrateOptions{
			Prefix: prefix,
			DryRun: true,
		})
		r.NoError(err)
		r.Equal(2, migrated, "should have found 2 entities to migrate")
		r.Equal(1, skipped, "should have skipped 1 already-migrated entity")

		// Verify nothing was actually written (entities should still in old format)
		resp, err := client.Get(ctx, prefix+"entity/app/test-app")
		r.NoError(err)
		r.Len(resp.Kvs, 1)

		var checkOld OldEntity
		err = cbor.Unmarshal(resp.Kvs[0].Value, &checkOld)
		r.NoError(err)
		r.Equal("app/test-app", string(checkOld.ID))
	})

	// Test actual migration
	t.Run("actual migration", func(t *testing.T) {
		migrated, skipped, err := MigrateEntityStore(ctx, log, client, MigrateOptions{
			Prefix: prefix,
			DryRun: false,
		})
		r.NoError(err)
		r.Equal(2, migrated, "should have migrated 2 entities")
		r.Equal(1, skipped, "should have skipped 1 already-migrated entity")

		// Verify first entity was migrated
		resp, err := client.Get(ctx, prefix+"entity/app/test-app")
		r.NoError(err)
		r.Len(resp.Kvs, 1)

		var migratedEnt1 Entity
		err = Decode(resp.Kvs[0].Value, &migratedEnt1)
		r.NoError(err)

		r.Equal("app/test-app", string(migratedEnt1.Id()))
		r.Equal(int64(5), migratedEnt1.GetRevision())
		r.Equal(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC), migratedEnt1.GetCreatedAt())
		r.Equal(time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC), migratedEnt1.GetUpdatedAt())

		// Verify original attributes are preserved
		nameAttr, ok := migratedEnt1.Get("app/name")
		r.True(ok)
		r.Equal("Test App", nameAttr.Value.String())

		// Verify second entity was migrated
		resp, err = client.Get(ctx, prefix+"entity/project/test-project")
		r.NoError(err)
		r.Len(resp.Kvs, 1)

		var migratedEnt2 Entity
		err = Decode(resp.Kvs[0].Value, &migratedEnt2)
		r.NoError(err)

		r.Equal("project/test-project", string(migratedEnt2.Id()))
		r.Equal(int64(3), migratedEnt2.GetRevision())
		r.Equal(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC), migratedEnt2.GetCreatedAt())
		r.Equal(time.Date(2024, 1, 3, 0, 0, 0, 0, time.UTC), migratedEnt2.GetUpdatedAt())

		ownerAttr, ok := migratedEnt2.Get("project/owner")
		r.True(ok)
		r.Equal("test@example.com", ownerAttr.Value.String())

		// Verify already-migrated entity was not touched
		resp, err = client.Get(ctx, prefix+"entity/sandbox/already-migrated")
		r.NoError(err)
		r.Len(resp.Kvs, 1)

		var unchangedEnt Entity
		err = Decode(resp.Kvs[0].Value, &unchangedEnt)
		r.NoError(err)
		r.Equal("sandbox/already-migrated", string(unchangedEnt.Id()))
	})

	// Test that running migration again is idempotent
	t.Run("idempotent migration", func(t *testing.T) {
		migrated, skipped, err := MigrateEntityStore(ctx, log, client, MigrateOptions{
			Prefix: prefix,
			DryRun: false,
		})
		r.NoError(err)
		r.Equal(0, migrated, "should not migrate any entities on second run")
		r.Equal(3, skipped, "should skip all 3 entities")
	})
}

func TestMigrateEntityWithMissingFields(t *testing.T) {
	r := require.New(t)

	client := setupTestEtcd(t)
	ctx := context.Background()

	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}))

	prefix := "/test-migrate-partial/"

	// Create entity with only some old-style fields
	oldEnt := OldEntity{
		ID:       "app/partial",
		Revision: 2,
		// No timestamps
		Attrs: []Attr{
			Keyword(Ident, "partial"),
			String("app/name", "Partial App"),
		},
	}

	data, err := cbor.Marshal(oldEnt)
	r.NoError(err)
	_, err = client.Put(ctx, prefix+"entity/app/partial", string(data))
	r.NoError(err)

	// Migrate
	migrated, skipped, err := MigrateEntityStore(ctx, log, client, MigrateOptions{
		Prefix: prefix,
		DryRun: false,
	})
	r.NoError(err)
	r.Equal(1, migrated)
	r.Equal(0, skipped)

	// Verify migration
	resp, err := client.Get(ctx, prefix+"entity/app/partial")
	r.NoError(err)
	r.Len(resp.Kvs, 1)

	var migratedEnt Entity
	err = Decode(resp.Kvs[0].Value, &migratedEnt)
	r.NoError(err)

	r.Equal("app/partial", string(migratedEnt.Id()))
	r.Equal(int64(2), migratedEnt.GetRevision())
	// Timestamps should be zero time since they weren't set
	r.True(migratedEnt.GetCreatedAt().IsZero())
	r.True(migratedEnt.GetUpdatedAt().IsZero())
}

func TestMigrateEntityPreservesExistingAttributes(t *testing.T) {
	r := require.New(t)

	client := setupTestEtcd(t)
	ctx := context.Background()

	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}))

	prefix := "/test-migrate-preserve/"

	// Create entity that already has db/id in attributes (shouldn't happen but test it)
	oldEnt := OldEntity{
		ID:        "app/conflict",
		Revision:  1,
		CreatedAt: time.Now().UnixMilli(),
		UpdatedAt: time.Now().UnixMilli(),
		Attrs: []Attr{
			Ref(DBId, "app/from-attrs"), // This should take precedence
			Keyword(Ident, "conflict"),
		},
	}

	data, err := cbor.Marshal(oldEnt)
	r.NoError(err)
	_, err = client.Put(ctx, prefix+"entity/app/conflict", string(data))
	r.NoError(err)

	// Migrate
	migrated, _, err := MigrateEntityStore(ctx, log, client, MigrateOptions{
		Prefix: prefix,
		DryRun: false,
	})
	r.NoError(err)
	r.Equal(1, migrated)

	// Verify the attribute version was preserved
	resp, err := client.Get(ctx, prefix+"entity/app/conflict")
	r.NoError(err)
	r.Len(resp.Kvs, 1)

	var migratedEnt Entity
	err = Decode(resp.Kvs[0].Value, &migratedEnt)
	r.NoError(err)

	// Should use the ID from attributes, not from struct field
	r.Equal("app/from-attrs", string(migratedEnt.Id()))
}

func TestMigrateMetaFromBytes(t *testing.T) {
	r := require.New(t)

	t.Run("migrates old Meta format", func(t *testing.T) {
		// Create an entity in old format with struct fields
		oldMeta := OldMeta{
			Entity: OldEntity{
				ID:        "sandbox/test-sandbox",
				Revision:  42,
				CreatedAt: time.Date(2024, 6, 15, 10, 30, 0, 0, time.UTC).UnixMilli(),
				UpdatedAt: time.Date(2024, 6, 15, 12, 45, 0, 0, time.UTC).UnixMilli(),
				Attrs: []Attr{
					String("sandbox/status", "running"),
					String("sandbox/image", "test-image:latest"),
				},
			},
			Revision: 5,
			Previous: 4,
		}

		// Encode in old format
		data, err := cbor.Marshal(oldMeta)
		r.NoError(err)

		// Migrate using MigrateMetaFromBytes
		migratedMeta, err := MigrateMetaFromBytes(data)
		r.NoError(err)
		r.NotNil(migratedMeta)

		// Verify Meta-level fields
		r.Equal(int64(5), migratedMeta.Revision)
		r.Equal(int64(4), migratedMeta.Previous)

		// Verify Entity was migrated correctly
		r.Equal("sandbox/test-sandbox", string(migratedMeta.Entity.Id()))
		r.Equal(int64(42), migratedMeta.Entity.GetRevision())
		r.True(migratedMeta.Entity.GetCreatedAt().Equal(time.Date(2024, 6, 15, 10, 30, 0, 0, time.UTC)))
		r.True(migratedMeta.Entity.GetUpdatedAt().Equal(time.Date(2024, 6, 15, 12, 45, 0, 0, time.UTC)))

		// Verify original attributes are preserved
		statusAttr, ok := migratedMeta.Entity.Get("sandbox/status")
		r.True(ok)
		r.Equal("running", statusAttr.Value.String())

		imageAttr, ok := migratedMeta.Entity.Get("sandbox/image")
		r.True(ok)
		r.Equal("test-image:latest", imageAttr.Value.String())
	})

	t.Run("handles new Meta format without migration", func(t *testing.T) {
		// Create an entity in new format
		newEnt := New(
			Ref(DBId, "sandbox/new-sandbox"),
			Int64(Revision, 10),
			Time(CreatedAt, time.Date(2024, 7, 1, 0, 0, 0, 0, time.UTC)),
			Time(UpdatedAt, time.Date(2024, 7, 1, 1, 0, 0, 0, time.UTC)),
			String("sandbox/status", "stopped"),
		)

		newMeta := &Meta{
			Entity:   newEnt,
			Revision: 15,
			Previous: 14,
		}

		// Encode in new format
		data, err := Encode(newMeta)
		r.NoError(err)

		// Decode using MigrateMetaFromBytes (should work without migration)
		decodedMeta, err := MigrateMetaFromBytes(data)
		r.NoError(err)
		r.NotNil(decodedMeta)

		// Verify it's identical to original
		r.Equal(int64(15), decodedMeta.Revision)
		r.Equal(int64(14), decodedMeta.Previous)
		r.Equal("sandbox/new-sandbox", string(decodedMeta.Entity.Id()))
		r.Equal(int64(10), decodedMeta.Entity.GetRevision())

		statusAttr, ok := decodedMeta.Entity.Get("sandbox/status")
		r.True(ok)
		r.Equal("stopped", statusAttr.Value.String())
	})

	t.Run("migrates entity without timestamps", func(t *testing.T) {
		// Create old format entity with no timestamps
		oldMeta := OldMeta{
			Entity: OldEntity{
				ID:       "sandbox/minimal",
				Revision: 1,
				// No CreatedAt/UpdatedAt
				Attrs: []Attr{
					String("sandbox/name", "minimal-sandbox"),
				},
			},
			Revision: 1,
			Previous: 0,
		}

		data, err := cbor.Marshal(oldMeta)
		r.NoError(err)

		migratedMeta, err := MigrateMetaFromBytes(data)
		r.NoError(err)

		r.Equal("sandbox/minimal", string(migratedMeta.Entity.Id()))
		r.Equal(int64(1), migratedMeta.Entity.GetRevision())
		// Timestamps should be zero since they weren't set
		r.True(migratedMeta.Entity.GetCreatedAt().IsZero())
		r.True(migratedMeta.Entity.GetUpdatedAt().IsZero())

		nameAttr, ok := migratedMeta.Entity.Get("sandbox/name")
		r.True(ok)
		r.Equal("minimal-sandbox", nameAttr.Value.String())
	})
}
