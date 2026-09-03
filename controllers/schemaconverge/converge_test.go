package schemaconverge

import (
	"log/slog"
	"testing"

	"github.com/mr-tron/base58"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/etcdtest"
)

func TestControllerConvergesAndRecordsTarget(t *testing.T) {
	client, prefix := etcdtest.TestEtcdClient(t)
	store, err := entity.NewEtcdStore(t.Context(), slog.Default(), client, prefix)
	require.NoError(t, err)

	const (
		modeAttr = entity.Id("test/controller-mode")
		readyID  = entity.Id("test/controller-mode.ready")
	)
	_, err = store.CreateEntity(t.Context(), entity.New(entity.Ident, readyID))
	require.NoError(t, err)
	_, err = store.CreateEntity(t.Context(), entity.New(
		entity.Ident, modeAttr,
		entity.Cardinality, entity.CardinalityOne,
		entity.Type, entity.TypeStr,
	))
	require.NoError(t, err)
	created, err := store.CreateEntity(t.Context(), entity.New(
		entity.Ident, "test/controller-entity",
		entity.String(modeAttr, "ready"),
	))
	require.NoError(t, err)

	_, err = store.CreateEntity(t.Context(), entity.New(
		entity.Ident, modeAttr,
		entity.Cardinality, entity.CardinalityOne,
		entity.Type, entity.TypeEnum,
		entity.EntityElemType, entity.TypeRef,
		entity.EnumValues, entity.ArrayValue(readyID),
	), entity.WithOverwrite)
	require.NoError(t, err)
	store.ClearSchemaCache()

	plan, err := entity.BuildConvergencePlan([]entity.ConvergenceRule{{
		Attribute: modeAttr,
		From:      entity.StringValue("ready"),
		To:        entity.RefValue(readyID),
	}})
	require.NoError(t, err)

	controller := Controller{
		Log:         slog.Default(),
		Store:       store,
		CurrentPlan: func() (entity.ConvergencePlan, error) { return plan, nil },
		Config: Config{
			MaxEntitiesPerPass: 1000,
		},
	}
	assert.False(t, controller.step(t.Context()), "one unbounded pass should complete")

	storedHash, err := store.LoadConvergenceHash(t.Context())
	require.NoError(t, err)
	assert.Equal(t, plan.Hash(), storedHash)
	state, err := store.LoadConvergenceState(t.Context(), slog.Default())
	require.NoError(t, err)
	assert.Nil(t, state)

	got, err := store.GetEntity(t.Context(), created.Id())
	require.NoError(t, err)
	mode, ok := got.Get(modeAttr)
	require.True(t, ok)
	assert.True(t, mode.Value.Equal(entity.RefValue(readyID)))
}

func TestControllerBacksOffAfterRepeatedEntityFailures(t *testing.T) {
	client, prefix := etcdtest.TestEtcdClient(t)
	store, err := entity.NewEtcdStore(t.Context(), slog.Default(), client, prefix)
	require.NoError(t, err)

	_, err = client.Put(t.Context(),
		store.Prefix()+"/entity/"+base58.Encode([]byte("corrupt-entity")),
		"this is not a valid encoded entity")
	require.NoError(t, err)

	plan, err := entity.BuildConvergencePlan([]entity.ConvergenceRule{{
		Attribute: "test/mode",
		From:      entity.StringValue("ready"),
		To:        entity.RefValue("test/mode.ready"),
	}})
	require.NoError(t, err)

	controller := Controller{
		Log:         slog.Default(),
		Store:       store,
		CurrentPlan: func() (entity.ConvergencePlan, error) { return plan, nil },
		Config: Config{
			MaxEntitiesPerPass: 1000,
		},
	}
	assert.True(t, controller.step(t.Context()), "the first failed pass should get one active retry")
	assert.False(t, controller.step(t.Context()), "a repeated failed pass should use the idle interval")

	state, err := store.LoadConvergenceState(t.Context(), slog.Default())
	require.NoError(t, err)
	require.NotNil(t, state)
	assert.Equal(t, 2, state.ConsecutiveFailedPasses)
}
