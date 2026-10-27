package build

import (
	"compress/gzip"
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"miren.dev/runtime/api/build/build_v1alpha"
	coreutil "miren.dev/runtime/api/core"
	"miren.dev/runtime/api/core/core_v1alpha"
	"miren.dev/runtime/api/ingress"
	"miren.dev/runtime/pkg/deploylifecycle"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/rpc"
	"miren.dev/runtime/pkg/rpc/stream"
	"miren.dev/runtime/pkg/tarx"
)

func TestRebuildDeployedSource(t *testing.T) {
	for _, kind := range []string{"dockerfile", "stack"} {
		t.Run(kind, func(t *testing.T) {
			ctx := t.Context()
			h := newDeploySagaHarness(t)
			h.builder.DataPath = t.TempDir()
			h.builder.ingressClient = ingress.NewClient(h.builder.Log, h.inmem.EAC.Client)
			s := &SagaBuilder{inner: h.builder, executor: h.executor, streams: h.streams, statuses: h.statuses, log: h.builder.Log}
			client := build_v1alpha.NewBuilderClient(rpc.LocalClient(build_v1alpha.AdaptBuilder(s)))
			status := stream.Callback(func(*build_v1alpha.Status) error { return nil })
			files := dockerfileTarball(t)
			if kind == "stack" {
				files = autoStackTarball(t)
			}
			files["deployed.txt"] = "deployed source"
			req := &build_v1alpha.DeployRequest{}
			git := &build_v1alpha.GitInfo{}
			git.SetSha("original-commit")
			req.SetGitInfo(git)
			_, err := client.BuildFromTar(ctx, "demo", stream.ServeReader(ctx, makeTar(t, files)), status, nil, "", "", req)
			require.NoError(t, err)
			var app core_v1alpha.App
			require.NoError(t, h.builder.ec.Get(ctx, "demo", &app))
			var original core_v1alpha.AppVersion
			require.NoError(t, h.builder.ec.GetById(ctx, app.ActiveVersion, &original))
			require.NotEmpty(t, original.SourceArchive)

			// A later preview also retains source, but must not become
			// the input to a rebuild of the deployed app.
			files["deployed.txt"] = "unreleased preview source"
			_, err = client.BuildFromTar(ctx, "demo", stream.ServeReader(ctx, makeTar(t, files)), status, nil, "preview", "24h", nil)
			require.NoError(t, err)

			var config core_v1alpha.ConfigVersion
			require.NoError(t, h.builder.ec.GetById(ctx, original.ConfigVersion, &config))
			config.Spec.Variables = []core_v1alpha.ConfigSpecVariables{{Key: "LIVE_SETTING", Value: "changed since upload", Source: "manual"}}
			config.Spec.Services[0].Command = "custom runtime command"
			require.NoError(t, h.builder.ec.Update(ctx, &config))
			req.SetMessage("Refresh base images")
			result, err := client.Rebuild(ctx, "demo", status, req)
			require.NoError(t, err)
			require.NotEqual(t, original.Version, result.Version())
			require.NoError(t, h.builder.ec.Get(ctx, "demo", &app))
			var rebuilt core_v1alpha.AppVersion
			require.NoError(t, h.builder.ec.GetById(ctx, app.ActiveVersion, &rebuilt))
			require.Equal(t, result.Version(), rebuilt.Version)
			require.Equal(t, original.Source, rebuilt.Source)
			archive, err := h.builder.openSource(rebuilt.SourceArchive)
			require.NoError(t, err)
			defer archive.Close()
			gz, err := gzip.NewReader(archive)
			require.NoError(t, err)
			defer gz.Close()
			contents, err := tarx.TarToMap(gz)
			require.NoError(t, err)
			require.Equal(t, "deployed source", string(contents["deployed.txt"]))
			spec, err := coreutil.ResolveConfig(ctx, h.inmem.EAC, &rebuilt)
			require.NoError(t, err)
			require.Equal(t, config.Spec, *spec)
			records, err := h.builder.deploy.Store().List(ctx, deploylifecycle.Query{AppName: "demo"})
			require.NoError(t, err)
			require.Len(t, records, 2)
			var recorded bool
			for _, rec := range records {
				if rec.Deployment.Version == app.ActiveVersion {
					require.Equal(t, "Refresh base images", rec.Deployment.Message)
					recorded = true
				}
			}
			require.True(t, recorded)
		})
	}
}

func TestRebuildRejectsUnavailableSource(t *testing.T) {
	for _, tc := range []struct {
		name, kind, archive, want string
	}{
		{"legacy", "dockerfile", "", "deploy it once"},
		{"image only", "image", "", "not built from"},
		{"missing archive", "stack", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "deploy again"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := t.Context()
			h := newDeploySagaHarness(t)
			h.builder.DataPath = t.TempDir()
			versionID, err := h.builder.ec.Create(ctx, "deployed", &core_v1alpha.AppVersion{Source: core_v1alpha.Source{Kind: tc.kind}, SourceArchive: tc.archive})
			require.NoError(t, err)
			_, err = h.builder.ec.Create(ctx, "demo", &core_v1alpha.App{ActiveVersion: versionID})
			require.NoError(t, err)
			s := &SagaBuilder{inner: h.builder}
			client := build_v1alpha.NewBuilderClient(rpc.LocalClient(build_v1alpha.AdaptBuilder(s)))
			_, err = client.Rebuild(ctx, "demo", stream.Callback(func(*build_v1alpha.Status) error { return nil }), nil)
			require.ErrorContains(t, err, tc.want)
			var app core_v1alpha.App
			require.NoError(t, h.builder.ec.Get(ctx, "demo", &app))
			require.Equal(t, versionID, app.ActiveVersion)
		})
	}
}

func TestRebuildRejectsChangedActiveVersion(t *testing.T) {
	ctx := context.Background()
	h := newDeploySagaHarness(t)
	h.streams.Register("source", makeTar(t, dockerfileTarball(t)))
	err := h.executor.Start(sagaBuildFromTar).
		Input("app_name", "demo").Input("stream_id", "source").
		Input("rebuild_version", "app_version/no-longer-active").
		WithID("stale-rebuild").Execute(ctx)
	require.ErrorContains(t, err, "deployed version changed")
	var app core_v1alpha.App
	require.NoError(t, h.builder.ec.Get(ctx, "demo", &app))
	require.Empty(t, app.ActiveVersion)
	_, err = h.builder.deploy.Begin(ctx, deploylifecycle.BeginParams{AppName: "demo", AppID: entity.Id(app.ID), Operation: deploylifecycle.OperationBuild})
	require.NoError(t, err, "failed rebuild must release the deployment lock")
}
