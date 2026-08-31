package export

import (
	"testing"

	"github.com/stretchr/testify/require"
	"miren.dev/runtime/pkg/entity"
)

func TestFilterDropsUnlistedAndNestedAttributes(t *testing.T) {
	contract := MustParse([]byte(`{"version":1,"target":"cloud","marker":"test/cloud","kinds":[{"id":"test/kind.app","lifecycle":"mirror","attributes":[{"id":"test/app.name","type":"string"},{"id":"test/app.actor","type":"component"},{"id":"test/actor.subject","type":"string","parent":"test/app.actor"}]}]}`))

	source := entity.New(
		entity.Ref(entity.DBId, "app/web"),
		entity.Ref(entity.EntityKind, "test/kind.app"),
		entity.String("test/app.name", "web"),
		entity.String("test/app.secret", "do not send"),
		entity.Bool("test/cloud", true),
		entity.Component("test/app.actor", []entity.Attr{
			entity.String("test/actor.subject", "user-1"),
			entity.String("test/actor.email", "secret@example.com"),
		}),
	)

	filtered, policy, err := contract.Filter(source)
	require.NoError(t, err)
	require.Equal(t, LifecycleMirror, policy.Lifecycle)
	require.Equal(t, "web", entity.MustGet(filtered, "test/app.name").Value.String())
	_, ok := filtered.Get("test/app.secret")
	require.False(t, ok)
	_, ok = filtered.Get("test/cloud")
	require.False(t, ok)
	actor := entity.MustGet(filtered, "test/app.actor").Value.Component()
	require.Equal(t, "user-1", entity.MustGet(actor, "test/actor.subject").Value.String())
	_, ok = actor.Get("test/actor.email")
	require.False(t, ok)
}

func TestFilterRejectsWrongAllowedValueShape(t *testing.T) {
	contract := MustParse([]byte(`{"version":1,"target":"cloud","marker":"test/cloud","kinds":[{"id":"test/kind.app","lifecycle":"mirror","attributes":[{"id":"test/app.name","type":"string"}]}]}`))
	source := entity.New(
		entity.Ref(entity.DBId, "app/web"),
		entity.Ref(entity.EntityKind, "test/kind.app"),
		entity.Int64("test/app.name", 42),
	)

	_, _, err := contract.Filter(source)
	require.ErrorContains(t, err, "want String")
}

func TestDigestUsesCanonicalOrdering(t *testing.T) {
	a := MustParse([]byte(`{"version":1,"target":"cloud","marker":"test/cloud","kinds":[{"id":"test/kind.b","lifecycle":"archive","attributes":[]},{"id":"test/kind.a","lifecycle":"mirror","attributes":[{"id":"test/z","type":"string"},{"id":"test/a","type":"string"}]}]}`))
	b := MustParse([]byte(`{"marker":"test/cloud","target":"cloud","version":1,"kinds":[{"attributes":[{"type":"string","id":"test/a"},{"type":"string","id":"test/z"}],"lifecycle":"mirror","id":"test/kind.a"},{"attributes":[],"lifecycle":"archive","id":"test/kind.b"}]}`))
	require.Equal(t, a.Digest(), b.Digest())
}
