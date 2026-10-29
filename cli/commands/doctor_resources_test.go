package commands

import (
	"context"
	"errors"
	"fmt"
	"io"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"miren.dev/runtime/api/disk/disk_v1alpha"
	"miren.dev/runtime/api/entityserver/entityserver_v1alpha"
	"miren.dev/runtime/api/server/server_v1alpha"
	"miren.dev/runtime/api/storage/storage_v1alpha"
	"miren.dev/runtime/clientconfig"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/entity/testutils"
	"miren.dev/runtime/pkg/oidcauth"
	"miren.dev/runtime/pkg/rpc"
	"miren.dev/runtime/pkg/serverinfo"
	disksrv "miren.dev/runtime/servers/disk"
	serverinfosrv "miren.dev/runtime/servers/serverinfo"
)

type doctorWorkloadAuth struct{ role string }

func (a doctorWorkloadAuth) Authenticate(context.Context, *rpc.Credentials) (*rpc.Identity, error) {
	app := ""
	if a.role == "app-readonly" {
		app = "shop"
	}
	return &rpc.Identity{Method: rpc.AuthMethodWorkload, Metadata: map[string]any{"role": a.role, "app": app}}, nil
}

type doctorFailingHealthPages struct {
	entityserver_v1alpha.EntityAccess
	failure string
}

func (s *doctorFailingHealthPages) ListPage(ctx context.Context, call *entityserver_v1alpha.EntityAccessListPage) error {
	if call.Args().Cursor() != "" {
		if s.failure == "incomplete" {
			call.Results().SetIncomplete(true)
			return nil
		}
		return errors.New("private-error-canary")
	}
	return s.EntityAccess.ListPage(ctx, call)
}

type doctorFailingServerInfo struct{}

func (doctorFailingServerInfo) Version(context.Context, *server_v1alpha.ServerInfoVersion) error {
	return errors.New("version query failed")
}

func TestDoctorGathersSafeResourcePagesAndServerReadiness(t *testing.T) {
	log := testutils.TestLogger(t)
	inmem, cleanup := testutils.NewInMemEntityServer(t)
	t.Cleanup(cleanup)
	for i := range 202 {
		status := storage_v1alpha.ATTACHED
		actual := storage_v1alpha.DV_READY
		if i == 201 {
			status = storage_v1alpha.ERROR
			actual = storage_v1alpha.DV_ERROR
		}
		disk, err := inmem.Client.Create(t.Context(), fmt.Sprintf("disk-%03d", i), &storage_v1alpha.Disk{Status: status})
		require.NoError(t, err)
		_, err = inmem.Client.Create(t.Context(), fmt.Sprintf("volume-%03d", i), &storage_v1alpha.DiskVolume{
			DiskId: disk, ActualState: actual, ErrorMessage: "private-error-canary",
		})
		require.NoError(t, err)
	}

	for _, tc := range []struct {
		name, role  string
		ready       bool
		versionAPI  bool
		healthAPI   bool
		pageFailure string
		versionFail bool
	}{
		{"ready", "cluster-readonly", true, true, true, "", false},
		{"starting", "cluster-readonly", false, true, true, "", false},
		{"old server", "cluster-readonly", false, false, true, "", false},
		{"health unsupported", "cluster-readonly", true, true, false, "", false},
		{"health method unsupported", "cluster-readonly", true, true, false, "", false},
		{"health denied", "app-readonly", true, true, true, "", false},
		{"late backend failure", "cluster-readonly", true, true, true, "backend", false},
		{"late incomplete scan", "cluster-readonly", true, true, true, "incomplete", false},
		{"version query failure", "cluster-readonly", true, true, true, "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var auth rpc.Authenticator = doctorWorkloadAuth{role: tc.role}
			if !tc.healthAPI {
				auth = &rpc.NoOpAuthenticator{}
			}
			server, err := rpc.NewState(t.Context(), rpc.WithLogger(log),
				rpc.WithAuthenticator(auth),
				rpc.WithAuthorizer(oidcauth.NewCompositeAuthorizer(nil)))
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, server.Close()) })
			source := serverinfo.New()
			if tc.ready {
				source.MarkReady()
			}
			if tc.versionAPI {
				var info server_v1alpha.ServerInfo = serverinfosrv.NewServer(source)
				if tc.versionFail {
					info = doctorFailingServerInfo{}
				}
				server.Server().ExposeValue(serverInfoService, server_v1alpha.AdaptServerInfo(info))
			}
			if tc.healthAPI {
				eac := inmem.EAC
				if tc.pageFailure != "" {
					proxy := &doctorFailingHealthPages{EntityAccess: inmem.Server, failure: tc.pageFailure}
					eac = entityserver_v1alpha.NewEntityAccessClient(rpc.LocalClient(entityserver_v1alpha.AdaptEntityAccess(proxy)))
				}
				server.Server().ExposeValue("dev.miren.runtime/disk-health", disk_v1alpha.AdaptDiskHealth(disksrv.NewHealthServer(log, eac)))
			}
			if !tc.healthAPI {
				server.Server().ExposeValue("entities", entityserver_v1alpha.AdaptEntityAccess(inmem.Server))
				if tc.name == "health method unsupported" {
					server.Server().ExposeValue("dev.miren.runtime/disk-health", rpc.NewInterface([]rpc.Method{{Name: "oldMethod", InterfaceName: "DiskHealth"}}, nil))
				}
				schema := inmem.GetEntity("dev.miren.storage/schema.v1alpha")
				inmem.AddEntity(entity.New(schema.Attrs(), entity.Keyword(entity.SchemaKind, "disk"), entity.Keyword(entity.SchemaKind, "disk_volume")))
			}
			ctx := &Context{Context: t.Context(), Log: log, Stdout: io.Discard, Stderr: io.Discard}
			ctx.Config.ServerAddress = server.ListenAddr()
			cfg := clientconfig.NewConfig()
			cfg.SetCluster("test", &clientconfig.ClusterConfig{Hostname: server.ListenAddr()})
			require.NoError(t, cfg.SetActiveCluster("test"))
			env := &doctorEnv{}
			gatherCluster(ctx, ConfigCentric{cfg: cfg, Cluster: "test"}, env)
			require.NoError(t, env.connErr, "a version reply or unsupported endpoint still proves reachability")
			serverCheck := checkServer(env)
			if tc.versionFail {
				require.ErrorContains(t, env.serverVersionErr, "version query failed")
				assert.Equal(t, checkOK, serverCheck.Status)
				assert.Contains(t, checkVersion(env).Summary, "server version unknown")
			} else if tc.versionAPI {
				require.NoError(t, env.serverVersionErr)
				require.NotNil(t, env.serverVersion)
				assert.Equal(t, tc.ready, env.serverVersion.Ready)
				if tc.ready {
					assert.Equal(t, checkOK, serverCheck.Status)
				} else {
					assert.Equal(t, checkWarn, serverCheck.Status)
					assert.Equal(t, "up, but not ready", serverCheck.Summary)
				}
			} else {
				require.ErrorIs(t, env.serverVersionErr, errServerVersionUnsupported)
				assert.Equal(t, checkOK, serverCheck.Status)
				assert.Equal(t, checkWarn, checkVersion(env).Status)
			}
			if tc.role == "app-readonly" || tc.pageFailure != "" {
				require.Error(t, env.resourcesErr)
				assert.Nil(t, env.resources, "an incomplete scan must not report resources healthy")
				assert.Equal(t, checkSkip, checkDisks(env).Status)
				if tc.pageFailure != "" {
					assert.ErrorContains(t, env.resourcesErr, "resource health scan unavailable")
					assert.NotContains(t, checkDisks(env).Summary, "private-error-canary")
				}
				return
			}
			require.NoError(t, env.resourcesErr)
			require.NotNil(t, env.resources)
			assert.Len(t, env.resources.disks, 202)
			assert.Len(t, env.resources.volumes, 202)
			result := checkDisks(env)
			assert.Equal(t, checkFail, result.Status)
			assert.Equal(t, "disk disk/disk-201, volume disk_volume/volume-201: volume lifecycle failed; inspect server logs", result.Summary)
			assert.NotContains(t, result.Summary, "private-error-canary")
		})
	}
}

func TestDoctorRejectsUnusableConnectionConfig(t *testing.T) {
	cfg := clientconfig.NewConfig()
	cfg.SetCluster("test", &clientconfig.ClusterConfig{
		Hostname:   "127.0.0.1:1",
		ClientCert: "-----BEGIN CERTIFICATE-----\nnot a real one\n-----END CERTIFICATE-----\n",
	})
	require.NoError(t, cfg.SetActiveCluster("test"))
	ctx := &Context{
		Context: t.Context(), Log: testutils.TestLogger(t),
		Stdout: io.Discard, Stderr: io.Discard, ClientConfig: cfg,
	}
	env := &doctorEnv{}
	gatherCluster(ctx, ConfigCentric{cfg: cfg, Cluster: "test"}, env)
	require.Error(t, env.connErr)
	assert.Equal(t, checkFail, checkServer(env).Status)
}
