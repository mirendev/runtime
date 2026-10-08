package entityserver_v1alpha

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"miren.dev/runtime/pkg/entity"
)

func TestEntityCopiesOnlyPresentMetadata(t *testing.T) {
	t.Run("parsed entity carries only its attributes", func(t *testing.T) {
		var rpcEnt Entity
		rpcEnt.SetAttrs([]entity.Attr{entity.String(entity.Doc, "parsed")})

		ent := rpcEnt.Entity()

		for _, id := range []entity.Id{entity.DBId, entity.CreatedAt, entity.UpdatedAt, entity.Revision} {
			_, ok := ent.Get(id)
			assert.False(t, ok, "absent wire field must not become attribute %s", id)
		}
		doc, ok := ent.Get(entity.Doc)
		require.True(t, ok)
		assert.Equal(t, "parsed", doc.Value.String())
	})

	t.Run("stored entity carries its metadata", func(t *testing.T) {
		created := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
		updated := created.Add(time.Hour)

		var rpcEnt Entity
		rpcEnt.SetId("app/web")
		rpcEnt.SetRevision(42)
		rpcEnt.SetCreatedAt(created.UnixMilli())
		rpcEnt.SetUpdatedAt(updated.UnixMilli())

		ent := rpcEnt.Entity()

		assert.Equal(t, entity.Id("app/web"), ent.Id())
		assert.Equal(t, int64(42), ent.GetRevision())
		assert.True(t, created.Equal(ent.GetCreatedAt()))
		assert.True(t, updated.Equal(ent.GetUpdatedAt()))
	})
}
