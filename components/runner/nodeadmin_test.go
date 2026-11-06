package runner

import (
	"context"
	"crypto/x509"
	"encoding/pem"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"miren.dev/runtime/api/exec/exec_v1alpha"
	"miren.dev/runtime/api/nodeadmin/nodeadmin_v1alpha"
	"miren.dev/runtime/clientconfig"
	"miren.dev/runtime/pkg/caauth"
	"miren.dev/runtime/pkg/rpc"
)

func TestRequireCoordinatorRejectsAnAnonymousCaller(t *testing.T) {
	err := requireCoordinator(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "presented none")

	err = requireCoordinator(rpc.ContextWithIdentity(context.Background(),
		&rpc.Identity{Method: rpc.AuthMethodAnonymous}))
	require.Error(t, err)
}

func TestRequireCoordinatorRejectsAnotherCertHolder(t *testing.T) {
	// A registered runner holds a valid cluster certificate. Holding one is
	// not the same as being the coordinator.
	err := requireCoordinator(rpc.ContextWithIdentity(context.Background(),
		&rpc.Identity{Method: rpc.AuthMethodCert, Subject: "runner-abc123"}))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "only the coordinator")
}

func TestRequireCoordinatorRejectsANonCertMethod(t *testing.T) {
	// A bearer token or JWT carrying the right subject is still not the
	// coordinator's certificate.
	err := requireCoordinator(rpc.ContextWithIdentity(context.Background(),
		&rpc.Identity{Method: rpc.AuthMethodJWT, Subject: rpc.CoordinatorCertSubject}))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "requires a certificate")
}

func TestRequireCoordinatorAcceptsTheCoordinator(t *testing.T) {
	require.NoError(t, requireCoordinator(rpc.ContextWithIdentity(context.Background(),
		&rpc.Identity{Method: rpc.AuthMethodCert, Subject: rpc.CoordinatorCertSubject})))
}

type recordingExec struct{ calls int }

func (r *recordingExec) Exec(context.Context, *exec_v1alpha.SandboxExecExec) error {
	r.calls++
	return nil
}

func (r *recordingExec) Attach(context.Context, *exec_v1alpha.SandboxExecAttach) error {
	r.calls++
	return nil
}

func TestRunnerExecAcceptsOnlyTheCoordinator(t *testing.T) {
	coordinator := rpc.ContextWithIdentity(context.Background(),
		&rpc.Identity{Method: rpc.AuthMethodCert, Subject: rpc.CoordinatorCertSubject})
	operator := rpc.ContextWithIdentity(context.Background(),
		&rpc.Identity{Method: rpc.AuthMethodCert, Subject: "miren-user"})

	inner := &recordingExec{}
	exec := coordinatorOnlyExec{inner}

	require.ErrorContains(t, exec.Exec(operator, nil), "only the coordinator")
	require.ErrorContains(t, exec.Attach(operator, nil), "only the coordinator")
	require.Zero(t, inner.calls, "a refused call must not reach the exec server")

	require.NoError(t, exec.Exec(coordinator, nil))
	require.NoError(t, exec.Attach(coordinator, nil))
	require.Equal(t, 2, inner.calls)
}

func TestRunnerListenerNeedsAClusterCA(t *testing.T) {
	access, err := NewClusterAccess(slog.Default(), RunnerDeps{}, RunnerConfig{
		Id: "runner", DataPath: t.TempDir(), ListenAddress: "localhost:0",
	})
	require.NoError(t, err)
	_, err = access.newRPCState(t.Context())
	require.ErrorContains(t, err, "cluster config is required")

	cfg := clientconfig.NewConfig()
	cfg.SetCluster("cluster", &clientconfig.ClusterConfig{Hostname: "localhost:0"})
	require.NoError(t, cfg.SetActiveCluster("cluster"))
	access.Config = cfg
	_, err = access.newRPCState(t.Context())
	require.ErrorContains(t, err, "cluster CA is required")
}

func TestNodeAdminAuthenticatesCoordinatorOverWire(t *testing.T) {
	ca, err := caauth.New(caauth.Options{CommonName: "cluster-ca", Organization: "miren", ValidFor: time.Hour})
	require.NoError(t, err)
	runnerCert, err := ca.IssueCertificate(caauth.Options{
		CommonName: "runner", Organization: "miren", ValidFor: time.Hour, DNSNames: []string{"localhost"},
	})
	require.NoError(t, err)
	coordinatorCert, err := ca.IssueCertificate(caauth.Options{
		CommonName: rpc.CoordinatorCertSubject, Organization: "miren", ValidFor: time.Hour,
	})
	require.NoError(t, err)
	otherCert, err := ca.IssueCertificate(caauth.Options{
		CommonName: "another-runner", Organization: "miren", ValidFor: time.Hour,
	})
	require.NoError(t, err)
	foreignCA, err := caauth.New(caauth.Options{CommonName: "cluster-ca", Organization: "miren", ValidFor: time.Hour})
	require.NoError(t, err)
	forgedCert, err := foreignCA.IssueCertificate(caauth.Options{
		CommonName: rpc.CoordinatorCertSubject, Organization: "miren", ValidFor: time.Hour,
	})
	require.NoError(t, err)

	for _, insecure := range []bool{false, true} {
		name := "verified-outbound"
		if insecure {
			name = "insecure-outbound"
		}
		t.Run(name, func(t *testing.T) {
			cfg := clientconfig.NewConfig()
			cfg.SetCluster("cluster", &clientconfig.ClusterConfig{
				Hostname: "localhost:0", CACert: string(ca.GetCACertificate()),
				ClientCert: string(runnerCert.CertPEM), ClientKey: string(runnerCert.KeyPEM), Insecure: insecure,
			})
			require.NoError(t, cfg.SetActiveCluster("cluster"))
			access, err := NewClusterAccess(slog.Default(), RunnerDeps{}, RunnerConfig{
				Id: "runner", DataPath: t.TempDir(), ListenAddress: "localhost:0", Config: cfg,
			})
			require.NoError(t, err)
			server, err := access.newRPCState(t.Context())
			require.NoError(t, err)
			defer server.Close()
			server.Server().ExposeValue(rpc.ServiceNodeAdmin, nodeadmin_v1alpha.AdaptNodeAdmin(&nodeAdminServer{log: slog.Default()}))

			call := func(cert, key []byte) (*nodeadmin_v1alpha.NodeAdminClientInstallDiskAcceleratorResults, error) {
				opts := []rpc.StateOption{rpc.WithSkipVerify}
				if cert != nil {
					opts = append(opts, rpc.WithCertPEMs(cert, key))
				}
				client, err := rpc.NewState(t.Context(), opts...)
				if err != nil {
					return nil, err
				}
				defer client.Close()
				cl, err := client.Connect(server.ListenAddr(), string(rpc.ServiceNodeAdmin))
				if err != nil {
					return nil, err
				}
				defer cl.Close()
				return nodeadmin_v1alpha.NewNodeAdminClient(cl).InstallDiskAccelerator(t.Context(), "foreign/image:tag", false)
			}

			_, err = call(coordinatorCert.CertPEM, coordinatorCert.KeyPEM)
			require.Error(t, err, "certificates wait for the revocation list to arrive")
			access.certRevocations.Load(nil)

			// The foreign image stops authorized callers before any host work.
			result, err := call(coordinatorCert.CertPEM, coordinatorCert.KeyPEM)
			require.NoError(t, err)
			assert.Contains(t, result.Error(), "not this cluster's lbd toolchain image")

			result, err = call(otherCert.CertPEM, otherCert.KeyPEM)
			require.NoError(t, err)
			assert.Contains(t, result.Error(), "only the coordinator")

			_, err = call(forgedCert.CertPEM, forgedCert.KeyPEM)
			require.Error(t, err)

			// This listener applies the same revocations as the coordinator's.
			block, _ := pem.Decode(coordinatorCert.CertPEM)
			require.NotNil(t, block)
			cert, err := x509.ParseCertificate(block.Bytes)
			require.NoError(t, err)
			access.certRevocations.Add(rpc.CertSerial(cert.SerialNumber))
			_, err = call(coordinatorCert.CertPEM, coordinatorCert.KeyPEM)
			require.ErrorContains(t, err, "revoked")

			_, err = call(nil, nil)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "401")
		})
	}
}
