package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	coreutil "miren.dev/runtime/api/core"
	"miren.dev/runtime/api/core/core_v1alpha"
	"miren.dev/runtime/api/entityserver"
	"miren.dev/runtime/pkg/cond"
	"miren.dev/runtime/pkg/entity/testutils"
)

func newEnvTestClient(t *testing.T) (context.Context, *entityserver.Client) {
	t.Helper()
	ctx := context.Background()
	inmem, cleanup := testutils.NewInMemEntityServer(t)
	t.Cleanup(cleanup)
	return ctx, entityserver.NewClient(slog.Default(), inmem.EAC)
}

// createEnvTestApp creates an app with an active AppVersion whose inline config
// carries the given variables.
func createEnvTestApp(t *testing.T, ctx context.Context, ec *entityserver.Client, name string, vars []core_v1alpha.Variable) {
	t.Helper()
	verID, err := ec.Create(ctx, name+"-v0", &core_v1alpha.AppVersion{
		Version: name + "-v0",
		Config:  core_v1alpha.Config{Variable: append([]core_v1alpha.Variable(nil), vars...)},
	})
	require.NoError(t, err)
	_, err = ec.Create(ctx, name, &core_v1alpha.App{ActiveVersion: verID})
	require.NoError(t, err)
}

// activeEnvVars resolves the variables on an app's current active version.
func activeEnvVars(t *testing.T, ctx context.Context, ec *entityserver.Client, name string) map[string]string {
	t.Helper()
	var app core_v1alpha.App
	require.NoError(t, ec.Get(ctx, name, &app))
	var ver core_v1alpha.AppVersion
	require.NoError(t, ec.GetById(ctx, app.ActiveVersion, &ver))
	spec, err := coreutil.ResolveConfig(ctx, ec.EAC(), &ver)
	require.NoError(t, err)
	out := make(map[string]string, len(spec.Variables))
	for _, v := range spec.Variables {
		out[v.Key] = v.Value
	}
	return out
}

// TestCreateNewVersionCASGuardsActiveVersion is the regression test for the
// active-version clobber (the same class as MIR-1458). An env mutation resolves
// the base config at one revision, then a competing writer swings active_version
// before the mutation's write lands. Without a CAS the mutation's unconditional
// Put would overwrite active_version and silently drop the competitor's version;
// the CAS on the app revision must reject the stale write instead.
func TestCreateNewVersionCASGuardsActiveVersion(t *testing.T) {
	ctx, ec := newEnvTestClient(t)
	createEnvTestApp(t, ctx, ec, "myapp", []core_v1alpha.Variable{{Key: "BASE", Value: "1", Source: "config"}})

	// Resolve the base config at the current app revision, as SetEnvVars does.
	appVer, spec, appRec, appRev, err := resolveBaseVersion(ctx, ec, "myapp", nil)
	require.NoError(t, err)

	// A competing writer swings active_version in the meantime (e.g. a parallel
	// env set, or the addon controller injecting DATABASE_URL).
	_, err = SetEnvVars(ctx, ec, "myapp", nil, []EnvVarInput{{Key: "COMPETITOR", Value: "c"}}, "")
	require.NoError(t, err)

	// Our write, built from the now-stale revision, must be rejected.
	require.NoError(t, mergeIntoSpec(spec, []EnvVarInput{{Key: "MINE", Value: "m"}}, ""))
	_, err = createNewVersion(ctx, ec, "myapp", appVer, spec, appRec, appRev)
	require.Error(t, err)
	require.True(t, errors.Is(err, cond.ErrConflict{}), "stale write must conflict, got %v", err)

	// The competitor's var survives; the clobbering write did not land.
	vars := activeEnvVars(t, ctx, ec, "myapp")
	require.Equal(t, "c", vars["COMPETITOR"], "competing writer's var must survive")
	require.NotContains(t, vars, "MINE", "clobbering write must not have activated")
}

// TestSetEnvVarsComposesSequentially confirms the retry-loop refactor preserves
// the normal accumulate-across-calls behavior.
func TestSetEnvVarsComposesSequentially(t *testing.T) {
	ctx, ec := newEnvTestClient(t)
	createEnvTestApp(t, ctx, ec, "myapp", []core_v1alpha.Variable{{Key: "BASE", Value: "1", Source: "config"}})

	_, err := SetEnvVars(ctx, ec, "myapp", nil, []EnvVarInput{{Key: "FOO", Value: "a"}}, "")
	require.NoError(t, err)
	_, err = SetEnvVars(ctx, ec, "myapp", nil, []EnvVarInput{{Key: "BAR", Value: "b"}}, "")
	require.NoError(t, err)

	vars := activeEnvVars(t, ctx, ec, "myapp")
	require.Equal(t, "1", vars["BASE"])
	require.Equal(t, "a", vars["FOO"])
	require.Equal(t, "b", vars["BAR"])
}

// TestSetEnvVarsConcurrentComposeAll drives the retry loop through real mid-flight
// conflicts: several writers set distinct keys on the same app at once. With the
// OCC retry each loser re-reads the winner's active version and composes onto it,
// so every key must survive. Without the CAS+retry (the old unconditional Put),
// concurrent writers each start from the base version and the last write wins,
// dropping the rest.
func TestSetEnvVarsConcurrentComposeAll(t *testing.T) {
	ctx, ec := newEnvTestClient(t)
	createEnvTestApp(t, ctx, ec, "myapp", []core_v1alpha.Variable{{Key: "BASE", Value: "1", Source: "config"}})

	const n = 8
	var wg sync.WaitGroup
	errs := make([]error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, errs[i] = SetEnvVars(ctx, ec, "myapp", nil,
				[]EnvVarInput{{Key: fmt.Sprintf("K%d", i), Value: strconv.Itoa(i)}}, "")
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		require.NoError(t, err, "concurrent SetEnvVars %d failed", i)
	}

	vars := activeEnvVars(t, ctx, ec, "myapp")
	require.Equal(t, "1", vars["BASE"], "base config var must survive")
	for i := 0; i < n; i++ {
		key := fmt.Sprintf("K%d", i)
		require.Equal(t, strconv.Itoa(i), vars[key],
			"concurrent writer %d's var must be present (would be clobbered without OCC retry)", i)
	}
}

// TestDeleteEnvVarsComposesSequentially confirms the DeleteEnvVars retry-loop
// refactor still removes the requested key and preserves the rest.
func TestDeleteEnvVarsComposesSequentially(t *testing.T) {
	ctx, ec := newEnvTestClient(t)
	createEnvTestApp(t, ctx, ec, "myapp", []core_v1alpha.Variable{{Key: "BASE", Value: "1", Source: "config"}})

	_, err := SetEnvVars(ctx, ec, "myapp", nil, []EnvVarInput{{Key: "FOO", Value: "a"}}, "")
	require.NoError(t, err)

	res, err := DeleteEnvVars(ctx, ec, "myapp", nil, []string{"FOO"}, "")
	require.NoError(t, err)
	require.NotNil(t, res)

	vars := activeEnvVars(t, ctx, ec, "myapp")
	require.Equal(t, "1", vars["BASE"])
	require.NotContains(t, vars, "FOO")
}
