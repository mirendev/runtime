package runner

import (
	"context"
	"encoding/json"
	"log/slog"
	"net"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/require"
	"miren.dev/runtime/api/compute/compute_v1alpha"
	"miren.dev/runtime/api/nodeadmin/nodeadmin_v1alpha"
	"miren.dev/runtime/api/runner/runner_v1alpha"
	"miren.dev/runtime/pkg/caauth"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/entity/testutils"
	query "miren.dev/runtime/pkg/portalquery"
	"miren.dev/runtime/pkg/rpc"
	runnerserver "miren.dev/runtime/servers/runner"
)

type observedNodeAdminServer struct {
	*nodeAdminServer
	queries chan string
}

func (s *observedNodeAdminServer) Query(ctx context.Context, req *nodeadmin_v1alpha.NodeAdminQuery) error {
	s.queries <- req.Args().Expression()
	return s.nodeAdminServer.Query(ctx, req)
}

func TestHostQuery(t *testing.T) {
	client := nodeadmin_v1alpha.NewNodeAdminClient(rpc.LocalClient(
		nodeadmin_v1alpha.AdaptNodeAdmin(&nodeAdminServer{log: slog.Default()})))
	ctx := rpc.ContextWithIdentity(t.Context(), &rpc.Identity{
		Method: rpc.AuthMethodCert, Subject: rpc.CoordinatorCertSubject,
	})

	t.Run("snapshot", func(t *testing.T) {
		res, err := client.Query(ctx, "memory")
		require.NoError(t, err)
		require.Empty(t, res.Error())
		var snapshot struct {
			Source string `json:"source"`
			Memory struct {
				Total uint64 `json:"total"`
			} `json:"memory"`
		}
		require.NoError(t, json.Unmarshal(res.Data(), &snapshot))
		require.Equal(t, "memory", snapshot.Source)
		require.Positive(t, snapshot.Memory.Total)
	})

	t.Run("aggregate", func(t *testing.T) {
		res, err := client.Query(ctx, "memory avg(total) over 300ms every 100ms")
		require.NoError(t, err)
		require.Empty(t, res.Error())
		var snapshot struct {
			Aggregation struct {
				Values []struct {
					Value float64 `json:"value"`
				} `json:"values"`
			} `json:"aggregation"`
		}
		require.NoError(t, json.Unmarshal(res.Data(), &snapshot))
		require.Len(t, snapshot.Aggregation.Values, 1)
		require.Positive(t, snapshot.Aggregation.Values[0].Value)
	})

	t.Run("invalid", func(t *testing.T) {
		res, err := client.Query(ctx, "SELECT * FROM memory")
		require.NoError(t, err)
		require.NotEmpty(t, res.Error())
		require.False(t, res.HasData())
	})

	t.Run("deadline", func(t *testing.T) {
		bounded, cancel := context.WithTimeout(ctx, 30*time.Millisecond)
		defer cancel()
		res, err := client.Query(bounded, "memory avg(used) over 10s every 100ms")
		require.NoError(t, err)
		require.Contains(t, res.Error(), "context deadline exceeded")
		require.False(t, res.HasData())
	})

	t.Run("authorization before parsing", func(t *testing.T) {
		for _, tc := range []struct {
			identity  *rpc.Identity
			errorText string
		}{
			{nil, "presented none"},
			{&rpc.Identity{Method: rpc.AuthMethodCert, Subject: "runner-other"}, "only the coordinator"},
			{&rpc.Identity{Method: rpc.AuthMethodJWT, Subject: rpc.CoordinatorCertSubject}, "requires a certificate"},
		} {
			res, err := client.Query(rpc.ContextWithIdentity(t.Context(), tc.identity), "invalid")
			require.NoError(t, err)
			require.Contains(t, res.Error(), tc.errorText)
			require.False(t, res.HasData())
		}
	})
}

func TestHostQueryMaximumDeadline(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		client := nodeadmin_v1alpha.NewNodeAdminClient(rpc.LocalClient(
			nodeadmin_v1alpha.AdaptNodeAdmin(&nodeAdminServer{log: slog.Default()})))
		ctx := rpc.ContextWithIdentity(t.Context(), &rpc.Identity{
			Method: rpc.AuthMethodCert, Subject: rpc.CoordinatorCertSubject,
		})
		start := time.Now()
		res, err := client.Query(ctx, "memory avg(total) over 2m every 1s")
		require.NoError(t, err)
		require.Contains(t, res.Error(), "context deadline exceeded")
		require.False(t, res.HasData())
		require.Equal(t, time.Minute, time.Since(start))
	})
}

func TestHostQueryCustomSourceAggregates(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		engine := query.Engine{Sources: map[string]query.CustomSource{
			"jobs": {
				Fields: []string{"queue", "bytes"}, NumericFields: []string{"bytes"},
				Events: func(ctx context.Context, _ query.MonitorRequest, emit func(query.Event) error) error {
					for _, job := range []struct {
						queue string
						bytes uint64
					}{{"web", 3}, {"worker", 19}, {"web", 7}} {
						if err := emit(query.Event{Fields: map[string]any{"queue": job.queue, "bytes": job.bytes}}); err != nil {
							return err
						}
					}
					<-ctx.Done()
					return ctx.Err()
				},
			},
		}}
		client := nodeadmin_v1alpha.NewNodeAdminClient(rpc.LocalClient(
			nodeadmin_v1alpha.AdaptNodeAdmin(&nodeAdminServer{log: slog.Default(), queryEngine: engine})))
		ctx := rpc.ContextWithIdentity(t.Context(), &rpc.Identity{
			Method: rpc.AuthMethodCert, Subject: rpc.CoordinatorCertSubject,
		})
		for _, expression := range []string{
			"jobs where queue = web and bytes > 3 and result.format = rows sum(bytes) over 1s by queue",
			"jobs where queue = web and bytes > 3 { @selected[queue] = sum(bytes) } after 1s { emit @selected }",
		} {
			res, err := client.Query(ctx, expression)
			require.NoError(t, err)
			require.Empty(t, res.Error())
			var snapshot query.Snapshot
			require.NoError(t, json.Unmarshal(res.Data(), &snapshot))
			require.Len(t, snapshot.Aggregation.Rows, 1)
			require.Equal(t, json.RawMessage(`"web"`), snapshot.Aggregation.Rows[0].Group["queue"])
			require.Equal(t, []json.RawMessage{json.RawMessage("7")}, snapshot.Aggregation.Rows[0].Values)
		}
		res, err := client.Query(ctx, `jobs { @jobs[] = sum(bytes) } memory { @ram[] = avg(total) } after 1s { emit @jobs; emit @ram }`)
		require.NoError(t, err)
		require.Empty(t, res.Error())
		var snapshot query.Snapshot
		require.NoError(t, json.Unmarshal(res.Data(), &snapshot))
		require.Equal(t, "script", snapshot.Source)
		require.Len(t, snapshot.Tables, 2)
		require.Equal(t, json.RawMessage("29"), snapshot.Tables[0].Aggregation.Rows[0].Values[0])
		require.Equal(t, "memory", snapshot.Tables[1].Source)
		require.NotEqual(t, json.RawMessage("0"), snapshot.Tables[1].Aggregation.Rows[0].Values[0])
	})
}

func TestCoordinatorQueryOverWire(t *testing.T) {
	ca, err := caauth.New(caauth.Options{CommonName: "query-test-ca", ValidFor: time.Hour})
	require.NoError(t, err)
	newState := func(subject string) *rpc.State {
		t.Helper()
		cert, err := ca.IssueCertificate(caauth.Options{
			CommonName: subject, DNSNames: []string{"localhost"}, ValidFor: time.Hour,
			IPs: []net.IP{net.ParseIP("127.0.0.1"), net.ParseIP("::1")},
		})
		require.NoError(t, err)
		state, err := rpc.NewState(t.Context(), rpc.WithBindAddr("localhost:0"),
			rpc.WithCertPEMs(cert.CertPEM, cert.KeyPEM),
			rpc.WithCertificateVerification(ca.GetCACertificate()),
			rpc.WithAuthenticator(&rpc.LocalOnlyAuthenticator{}))
		require.NoError(t, err)
		t.Cleanup(func() { require.NoError(t, state.Close()) })
		return state
	}
	runner := newState("runner-target")
	queries := make(chan string, 8)
	runner.Server().ExposeValue(rpc.ServiceNodeAdmin,
		nodeadmin_v1alpha.AdaptNodeAdmin(&observedNodeAdminServer{
			nodeAdminServer: &nodeAdminServer{
				log: slog.Default(),
				queryEngine: query.Engine{Sources: map[string]query.CustomSource{
					"inventory": {
						Snapshots: func(_ context.Context, req query.MonitorRequest) (query.Snapshot, error) {
							return query.Snapshot{Source: req.Source, Data: struct {
								Bytes uint64 `json:"bytes"`
							}{9007199254740993}}, nil
						},
					},
				}},
			}, queries: queries,
		}))
	coordinator := newState(rpc.CoordinatorCertSubject)
	es, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()
	for _, node := range []*compute_v1alpha.Node{
		{RunnerId: "target-id", Name: "target", ApiAddress: runner.ListenAddr()},
		{RunnerId: "offline-id", Name: "offline"},
	} {
		_, err := es.EAC.Create(t.Context(), entity.New(
			entity.DBId, compute_v1alpha.NewNodeId(node.RunnerId).Id(), node.Encode,
		).Attrs())
		require.NoError(t, err)
	}
	coordinator.Server().ExposeValue(rpc.ServiceRunner, runner_v1alpha.AdaptRunnerRegistration(
		runnerserver.NewRegistrationServer(runnerserver.RegistrationServerConfig{
			Log: slog.Default(), EAC: es.EAC, RPC: coordinator,
		})))
	user := newState("operator")
	cl, err := user.Connect(coordinator.ListenAddr(), rpc.ServiceRunner)
	require.NoError(t, err)
	defer cl.Close()
	client := runner_v1alpha.NewRunnerRegistrationClient(cl)

	for _, target := range []string{"target", "target-id", "node/target-id"} {
		res, err := client.Query(t.Context(), target, "network where name = lo")
		require.NoError(t, err)
		require.Empty(t, res.Error())
		require.Equal(t, "target", res.Name())
		select {
		case expression := <-queries:
			require.Equal(t, "network where name = lo", expression)
		default:
			t.Fatal("query did not execute on the target runner")
		}
		var snapshot struct {
			Source  string `json:"source"`
			Network []struct {
				Name string `json:"name"`
			} `json:"network"`
		}
		require.NoError(t, json.Unmarshal(res.Data(), &snapshot))
		require.Equal(t, "network", snapshot.Source)
		require.Len(t, snapshot.Network, 1)
		require.Equal(t, "lo", snapshot.Network[0].Name)
	}
	custom, err := client.Query(t.Context(), "target", "inventory")
	require.NoError(t, err)
	require.Empty(t, custom.Error())
	require.Equal(t, "target", custom.Name())
	var inventory struct {
		Source string `json:"source"`
		Data   struct {
			Bytes uint64 `json:"bytes"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(custom.Data(), &inventory))
	require.Equal(t, "inventory", inventory.Source)
	require.Equal(t, uint64(9007199254740993), inventory.Data.Bytes)

	for _, tc := range []struct{ target, expression, errorText string }{
		{" ", "memory", "runner name or ID is required"},
		{"target", " ", "query expression is required"},
		{"missing", "memory", "not found"},
		{"offline", "memory", "no address"},
		{"target", "SELECT * FROM memory", "query"},
	} {
		res, err := client.Query(t.Context(), tc.target, tc.expression)
		require.NoError(t, err)
		require.Contains(t, res.Error(), tc.errorText)
		require.False(t, res.HasData())
	}

	// A different CA-issued runner certificate cannot execute host queries.
	cl, err = user.Connect(runner.ListenAddr(), rpc.ServiceNodeAdmin)
	require.NoError(t, err)
	defer cl.Close()
	res, err := nodeadmin_v1alpha.NewNodeAdminClient(cl).Query(t.Context(), "memory")
	require.NoError(t, err)
	require.Contains(t, res.Error(), "only the coordinator")
	require.False(t, res.HasData())
}
