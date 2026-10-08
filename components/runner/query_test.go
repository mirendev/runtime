package runner

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/require"
	"miren.dev/runtime/api/compute/compute_v1alpha"
	"miren.dev/runtime/api/nodeadmin/nodeadmin_v1alpha"
	"miren.dev/runtime/api/runner/runner_v1alpha"
	"miren.dev/runtime/internal/runnerquery"
	"miren.dev/runtime/pkg/caauth"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/entity/testutils"
	query "miren.dev/runtime/pkg/portalquery"
	"miren.dev/runtime/pkg/rpc"
	runnerserver "miren.dev/runtime/servers/runner"
)

type observedNodeAdminServer struct {
	*nodeAdminServer
	queries        chan string
	engineRevision string
}

func (s *observedNodeAdminServer) Query(ctx context.Context, req *nodeadmin_v1alpha.NodeAdminQuery) error {
	s.queries <- req.Args().Expression()
	err := s.nodeAdminServer.Query(ctx, req)
	req.Results().SetEngineRevision(s.engineRevision)
	return err
}

func (s *observedNodeAdminServer) QueryInfo(ctx context.Context, req *nodeadmin_v1alpha.NodeAdminQueryInfo) error {
	if err := s.nodeAdminServer.QueryInfo(ctx, req); err != nil {
		return err
	}
	reference, err := runnerquery.Reference()
	if err != nil {
		return err
	}
	req.Results().SetEngineRevision(s.engineRevision)
	req.Results().SetReference("runner-only reference\n" + reference)
	return nil
}

func (s *observedNodeAdminServer) ValidateQuery(ctx context.Context, req *nodeadmin_v1alpha.NodeAdminValidateQuery) error {
	err := s.nodeAdminServer.ValidateQuery(ctx, req)
	req.Results().SetEngineRevision(s.engineRevision)
	return err
}

func TestHostQueryInspection(t *testing.T) {
	snapshots := func(context.Context, query.MonitorRequest) (query.Snapshot, error) {
		t.Error("validation must not collect snapshots")
		return query.Snapshot{}, errors.New("unexpected collection")
	}
	events := func(context.Context, query.MonitorRequest, func(query.Event) error) error {
		t.Error("validation must not collect events")
		return errors.New("unexpected collection")
	}
	client := nodeadmin_v1alpha.NewNodeAdminClient(rpc.LocalClient(nodeadmin_v1alpha.AdaptNodeAdmin(&nodeAdminServer{
		queryEngine: query.Engine{Snapshots: snapshots, Events: events, Sources: map[string]query.CustomSource{
			"sandboxes": {Fields: []string{"app", "cgroup"}, Snapshots: snapshots},
			"jobs":      {Fields: []string{"queue", "bytes"}, NumericFields: []string{"bytes"}, Events: events},
		}},
	})))
	ctx := rpc.ContextWithIdentity(t.Context(), &rpc.Identity{Method: rpc.AuthMethodCert, Subject: rpc.CoordinatorCertSubject})
	info, err := client.QueryInfo(ctx)
	require.NoError(t, err)
	require.Empty(t, info.Error())
	require.Equal(t, query.Revision, info.EngineRevision())
	reference, err := runnerquery.Reference()
	require.NoError(t, err)
	require.Equal(t, reference, info.Reference())

	for _, tc := range []struct {
		expression string
		errorText  string
	}{
		{"memory", ""},
		{"memory avg(total) over 2m every 1s", ""},
		{"jobs where bytes > 3 sum(bytes) over 1s by queue", ""},
		{"jobs { @jobs[queue] = sum(bytes) } after 1s { emit @jobs }", ""},
		{`cgroups using (sandboxes where app = "my-app") on path = cgroup rate(io.write_bytes), rate(io.write_ios) over 10s every 1s by inventory.app`, ""},
		{"syscalls where syscall = :fsync count over 1s", ""},
		{"", "query"},
		{"SELECT * FROM memory", "query"},
		{"unknown_source", "source"},
		{"jobs sum(missing) over 1s", "missing"},
		{"syscalls where syscall = :nonexistent_miren_syscall count over 1s", "unknown syscall"},
		{"syscalls where syscall = :nonexistent_miren_syscall { @calls[] = count() } after 1s { emit @calls }", "unknown syscall"},
		{strings.Repeat("x", 4097), "4096"},
	} {
		t.Run(tc.expression[:min(len(tc.expression), 100)], func(t *testing.T) {
			res, err := client.ValidateQuery(ctx, tc.expression)
			require.NoError(t, err)
			require.Equal(t, query.Revision, res.EngineRevision())
			require.Equal(t, tc.errorText == "", res.Valid())
			if tc.errorText == "" {
				require.Empty(t, res.Error())
			} else {
				require.Contains(t, res.Error(), tc.errorText)
			}
		})
	}
	for _, identity := range []*rpc.Identity{
		nil,
		{Method: rpc.AuthMethodCert, Subject: "runner-other"},
		{Method: rpc.AuthMethodJWT, Subject: rpc.CoordinatorCertSubject},
	} {
		deniedCtx := rpc.ContextWithIdentity(t.Context(), identity)
		info, err := client.QueryInfo(deniedCtx)
		require.NoError(t, err)
		require.NotEmpty(t, info.Error())
		require.False(t, info.HasEngineRevision())
		require.False(t, info.HasReference())
		validation, err := client.ValidateQuery(deniedCtx, "invalid")
		require.NoError(t, err)
		require.Equal(t, info.Error(), validation.Error())
		require.False(t, validation.HasEngineRevision())
		require.False(t, validation.Valid())
	}
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
		require.Equal(t, query.Revision, res.EngineRevision())
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
		require.Equal(t, query.Revision, res.EngineRevision())
		require.False(t, res.HasData())
	})

	t.Run("deadline", func(t *testing.T) {
		bounded, cancel := context.WithTimeout(ctx, 30*time.Millisecond)
		defer cancel()
		res, err := client.Query(bounded, "memory avg(used) over 10s every 100ms")
		require.NoError(t, err)
		require.Contains(t, res.Error(), "context deadline exceeded")
		require.Equal(t, query.Revision, res.EngineRevision())
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
			require.False(t, res.HasEngineRevision())
			require.False(t, res.HasData())
		}
	})
}

func TestHostQueryAdmission(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		started := make(chan struct{}, 10)
		server := &nodeAdminServer{log: slog.Default(), queryEngine: query.Engine{Sources: map[string]query.CustomSource{
			"hold": {Snapshots: func(ctx context.Context, _ query.MonitorRequest) (query.Snapshot, error) {
				started <- struct{}{}
				<-ctx.Done()
				return query.Snapshot{}, ctx.Err()
			}},
		}}}
		client := nodeadmin_v1alpha.NewNodeAdminClient(rpc.LocalClient(nodeadmin_v1alpha.AdaptNodeAdmin(server)))
		ctx := rpc.ContextWithIdentity(context.Background(), &rpc.Identity{Method: rpc.AuthMethodCert, Subject: rpc.CoordinatorCertSubject})
		cancels := make([]context.CancelFunc, 10)
		done := make(chan string, 10)
		for i := range 10 {
			queryCtx, cancel := context.WithCancel(ctx)
			cancels[i] = cancel
			defer cancel()
			go func() {
				res, err := client.Query(queryCtx, "hold")
				if err != nil {
					done <- err.Error()
					return
				}
				done <- res.Error()
			}()
		}
		for range 10 {
			<-started
		}
		res, err := client.Query(ctx, "memory")
		require.NoError(t, err)
		require.Equal(t, "runner already has 10 queries in progress", res.Error())
		require.False(t, res.HasData())
		info, err := client.QueryInfo(ctx)
		require.NoError(t, err)
		require.Empty(t, info.Error())
		require.NotEmpty(t, info.Reference())
		validation, err := client.ValidateQuery(ctx, "hold")
		require.NoError(t, err)
		require.Empty(t, validation.Error())
		require.True(t, validation.Valid())
		unauthorized := rpc.ContextWithIdentity(ctx, &rpc.Identity{Method: rpc.AuthMethodCert, Subject: "runner-other"})
		res, err = client.Query(unauthorized, "memory")
		require.NoError(t, err)
		require.Contains(t, res.Error(), "only the coordinator")
		cancels[0]()
		require.Contains(t, <-done, "context canceled")
		res, err = client.Query(ctx, "invalid")
		require.NoError(t, err)
		require.NotEmpty(t, res.Error())
		require.NotContains(t, res.Error(), "in progress")
		for range 2 {
			res, err = client.Query(ctx, "memory")
			require.NoError(t, err)
			require.Empty(t, res.Error())
			require.True(t, res.HasData())
		}
		for _, cancel := range cancels[1:] {
			cancel()
		}
		for range 9 {
			require.Contains(t, <-done, "context canceled")
		}
	})
}

func TestCoordinatorQueryAdmission(t *testing.T) {
	client := runner_v1alpha.NewRunnerRegistrationClient(rpc.LocalClient(
		runner_v1alpha.AdaptRunnerRegistration(&runnerserver.RegistrationServer{})))
	for _, tc := range []struct {
		identity *rpc.Identity
		allowed  bool
	}{
		{nil, false},
		{&rpc.Identity{Method: rpc.AuthMethodCert, Subject: "runner-target"}, false},
		{&rpc.Identity{Method: rpc.AuthMethodCert, Subject: "miren-runner"}, false},
		{&rpc.Identity{Method: rpc.AuthMethodCert, Subject: "miren-services"}, false},
		{&rpc.Identity{Method: rpc.AuthMethodCert, Subject: "custom-name"}, false},
		{&rpc.Identity{Method: rpc.AuthMethodWorkload, Subject: "miren-user"}, false},
		{&rpc.Identity{Method: rpc.AuthMethodOIDC, Subject: "miren-user"}, false},
		{&rpc.Identity{Method: rpc.AuthMethodCert, Subject: "miren-user"}, true},
		{&rpc.Identity{Method: rpc.AuthMethodCert, Subject: "miren-server"}, true},
		{&rpc.Identity{Method: rpc.AuthMethodCert, Subject: rpc.CoordinatorCertSubject}, true},
		{&rpc.Identity{Method: rpc.AuthMethodJWT, Subject: "cloud-user"}, true},
		{&rpc.Identity{Method: rpc.AuthMethodAnonymous}, true},
	} {
		ctx := rpc.ContextWithIdentity(t.Context(), tc.identity)
		res, err := client.Query(ctx, "target", "memory")
		require.NoError(t, err)
		if tc.allowed {
			require.Equal(t, "no rpc state to reach the runner with", res.Error())
		} else {
			require.Equal(t, "host queries require an operator identity", res.Error())
		}
		require.False(t, res.HasData())
		info, err := client.QueryInfo(ctx, "target")
		require.NoError(t, err)
		require.Equal(t, res.Error(), info.Error())
		require.False(t, info.HasReference())
		validation, err := client.ValidateQuery(ctx, "target", "memory")
		require.NoError(t, err)
		require.Equal(t, res.Error(), validation.Error())
		require.False(t, validation.Valid())
	}
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
			}, queries: queries, engineRevision: "different-runner-engine",
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
	user := newState("miren-user")
	cl, err := user.Connect(coordinator.ListenAddr(), rpc.ServiceRunner)
	require.NoError(t, err)
	defer cl.Close()
	client := runner_v1alpha.NewRunnerRegistrationClient(cl)

	reference, err := runnerquery.Reference()
	require.NoError(t, err)
	for _, target := range []string{"target", "target-id", "node/target-id"} {
		info, err := client.QueryInfo(t.Context(), target)
		require.NoError(t, err)
		require.Empty(t, info.Error())
		require.Equal(t, "target", info.Name())
		require.Equal(t, "different-runner-engine", info.EngineRevision())
		require.Equal(t, "runner-only reference\n"+reference, info.Reference())
		for _, expression := range []string{"inventory", "unknown_source"} {
			validation, err := client.ValidateQuery(t.Context(), target, expression)
			require.NoError(t, err)
			require.Equal(t, "target", validation.Name())
			require.Equal(t, "different-runner-engine", validation.EngineRevision())
			require.Equal(t, expression == "inventory", validation.Valid())
			if validation.Valid() {
				require.Empty(t, validation.Error())
			} else {
				require.Contains(t, validation.Error(), "source")
			}
		}
	}
	select {
	case expression := <-queries:
		t.Fatalf("query inspection executed a query: %s", expression)
	default:
	}
	for _, tc := range []struct{ target, errorText string }{
		{" ", "runner name or ID is required"},
		{"missing", "not found"},
		{"offline", "no address"},
	} {
		info, err := client.QueryInfo(t.Context(), tc.target)
		require.NoError(t, err)
		require.Contains(t, info.Error(), tc.errorText)
		require.False(t, info.HasEngineRevision())
		validation, err := client.ValidateQuery(t.Context(), tc.target, "memory")
		require.NoError(t, err)
		require.Equal(t, info.Error(), validation.Error())
		require.False(t, validation.Valid())
		require.False(t, validation.HasEngineRevision())
	}

	for _, target := range []string{"target", "target-id", "node/target-id"} {
		res, err := client.Query(t.Context(), target, "network where name = lo")
		require.NoError(t, err)
		require.Empty(t, res.Error())
		require.Equal(t, "target", res.Name())
		require.Equal(t, "different-runner-engine", res.EngineRevision())
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
	require.Equal(t, "inventory", <-queries)
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
		if tc.expression == "SELECT * FROM memory" {
			require.Equal(t, "different-runner-engine", res.EngineRevision())
		} else {
			require.False(t, res.HasEngineRevision())
		}
	}
	require.Equal(t, "SELECT * FROM memory", <-queries)

	for _, subject := range []string{"runner-other", "miren-runner", "miren-services", "custom-name"} {
		other := newState(subject)
		otherClient, err := other.Connect(coordinator.ListenAddr(), rpc.ServiceRunner)
		require.NoError(t, err)
		res, err := runner_v1alpha.NewRunnerRegistrationClient(otherClient).Query(t.Context(), "target", "memory")
		require.NoError(t, err)
		require.Equal(t, "host queries require an operator identity", res.Error())
		require.False(t, res.HasData())
		require.NoError(t, otherClient.Close())
	}
	select {
	case expression := <-queries:
		t.Fatalf("denied caller reached the runner: %s", expression)
	default:
	}

	// Even an operator certificate cannot bypass coordinator forwarding.
	cl, err = user.Connect(runner.ListenAddr(), rpc.ServiceNodeAdmin)
	require.NoError(t, err)
	defer cl.Close()
	res, err := nodeadmin_v1alpha.NewNodeAdminClient(cl).Query(t.Context(), "memory")
	require.NoError(t, err)
	require.Contains(t, res.Error(), "only the coordinator")
	require.False(t, res.HasData())
}
