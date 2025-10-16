package entity

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMetaEncodeDecode(t *testing.T) {
	t.Run("encode and decode meta with entity", func(t *testing.T) {
		r := require.New(t)

		// Create a meta with an entity
		originalEntity := NewEntity(Attrs(
			Ident, "test/entity",
			Doc, "Test documentation",
			Type, TypeStr,
		))

		originalMeta := &Meta{
			Entity:   originalEntity,
			Revision: 42,
			Previous: 41,
		}

		// Encode the meta
		data, err := Encode(originalMeta)
		r.NoError(err)
		r.NotEmpty(data)

		// Decode into a new meta
		var decodedMeta Meta
		err = Decode(data, &decodedMeta)
		r.NoError(err)

		// Verify the decoded values
		assert.Equal(t, originalMeta.Revision, decodedMeta.Revision)
		assert.Equal(t, originalMeta.Previous, decodedMeta.Previous)
		assert.NotNil(t, decodedMeta.Entity)

		// Verify entity attributes
		assert.Equal(t, originalEntity.Id(), decodedMeta.Id())

		docAttr, ok := decodedMeta.Get(Doc)
		r.True(ok)
		assert.Equal(t, "Test documentation", docAttr.Value.String())

		typeAttr, ok := decodedMeta.Get(Type)
		r.True(ok)
		assert.Equal(t, TypeStr, typeAttr.Value.Id())
	})

	t.Run("encode and decode meta with nil entity", func(t *testing.T) {
		r := require.New(t)

		originalMeta := &Meta{
			Entity:   nil,
			Revision: 10,
			Previous: 9,
		}

		// Encode the meta
		data, err := Encode(originalMeta)
		r.NoError(err)

		// Decode into a new meta
		var decodedMeta Meta
		err = Decode(data, &decodedMeta)
		r.NoError(err)

		assert.Equal(t, originalMeta.Revision, decodedMeta.Revision)
		assert.Equal(t, originalMeta.Previous, decodedMeta.Previous)
		assert.Nil(t, decodedMeta.Entity)
	})

	t.Run("GetRevision uses meta revision when set", func(t *testing.T) {
		meta := &Meta{
			Entity:   NewEntity(Attrs(Ident, "test/entity")),
			Revision: 100,
		}

		assert.Equal(t, int64(100), meta.GetRevision())
	})

	t.Run("GetRevision uses entity revision when meta revision is zero", func(t *testing.T) {
		entity := NewEntity(Attrs(Ident, "test/entity"))
		entity.SetRevision(50)

		meta := &Meta{
			Entity:   entity,
			Revision: 0,
		}

		assert.Equal(t, int64(50), meta.GetRevision())
	})
}
