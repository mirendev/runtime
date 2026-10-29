//go:build linux

package server

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.etcd.io/etcd/api/v3/v3rpc/rpctypes"
	clientv3 "go.etcd.io/etcd/client/v3"
	"miren.dev/runtime/components/coordinate"
	"miren.dev/runtime/components/etcd"
	"miren.dev/runtime/pkg/caauth"
	"miren.dev/runtime/pkg/clusterstate"
	"miren.dev/runtime/pkg/etcdaccess"
	"miren.dev/runtime/pkg/registration"
	"miren.dev/runtime/pkg/serverconfig"
	"miren.dev/runtime/pkg/testutils"
	"miren.dev/runtime/pkg/workloadidentity"
)

func TestEmbeddedEtcdBootRestoresCredentialsAndTransportOnFirstStart(t *testing.T) {
	deps, cleanup := testutils.NewTestDeps()
	defer cleanup()
	log := testLogger()
	dir := t.TempDir()
	reg := &registration.StoredRegistration{Status: "approved", ClusterID: "restored-cluster", IdentityIssuerURL: "https://identity.example", IdentityAnchor: registration.AnchorCloud, PrivateKey: "test-key"}
	require.NoError(t, registration.SaveRegistration(filepath.Join(dir, "server"), reg))
	issuer, err := workloadidentity.NewIssuer(workloadidentity.IssuerConfig{DataPath: dir, IssuerURL: reg.IdentityIssuerURL, ClusterID: reg.ClusterID})
	require.NoError(t, err)
	token, err := issuer.IssueToken("app", "sandbox")
	require.NoError(t, err)
	config := serverconfig.DefaultConfig().Etcd
	config.SetClientPort(testutils.GetFreePort(t))
	config.SetHTTPClientPort(testutils.GetFreePort(t))
	config.SetPeerPort(testutils.GetFreePort(t))
	ip := ipDiscoveryBootOutput{ipSet: coordinate.NewIPSet()}
	observability := observabilityBootOutput{log: log}
	first := &etcdBoot{inputs: etcdBootInputs{log: log, config: config, dataPath: dir}}
	out, err := first.startEmbedded(t.Context(), ip, containerdBootOutput{Client: deps.CC, Namespace: deps.Namespace}, observability)
	require.NoError(t, err)
	defer func() { require.NoError(t, first.stop(context.Background())) }()
	ca, err := coordinate.EnsureCA(log, dir)
	require.NoError(t, err)
	runner, err := ca.IssueCertificate(caauth.Options{CommonName: "runner-restored", ValidFor: time.Hour})
	require.NoError(t, err)
	connect := func(endpoint string, credentials *coordinate.EtcdTLSConfig) *clientv3.Client {
		cert, err := tls.X509KeyPair(credentials.CertPEM, credentials.KeyPEM)
		require.NoError(t, err)
		roots := x509.NewCertPool()
		require.True(t, roots.AppendCertsFromPEM(credentials.CACert))
		client, err := clientv3.New(clientv3.Config{Endpoints: []string{endpoint}, TLS: &tls.Config{MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{cert}, RootCAs: roots}, DialTimeout: 5 * time.Second})
		require.NoError(t, err)
		t.Cleanup(func() { require.NoError(t, client.Close()) })
		return client
	}
	admin := connect(out.endpoints[0], out.tls.ClientTLS)
	require.NoError(t, etcdaccess.Runner(t.Context(), admin, "runner-restored"))
	_, err = admin.Put(t.Context(), config.GetPrefix()+"/sub/flannel/subnets/test", "restored-lease")
	require.NoError(t, err)
	snapshot := filepath.Join(t.TempDir(), "metadata.etcd.db")
	require.NoError(t, etcd.Snapshot(t.Context(), log, out.endpoints[0], &etcd.TLSConfig{CertsDir: out.tls.CertsDir}, snapshot))
	require.NoError(t, first.stop(t.Context()))

	restoredDir := t.TempDir()
	sealingKey, err := os.ReadFile(clusterstate.SealingKeyPath(dir))
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(filepath.Join(restoredDir, "server"), 0700))
	require.NoError(t, os.WriteFile(clusterstate.SealingKeyPath(restoredDir), sealingKey, 0600))
	namespace := deps.Namespace + "-restored"
	defer testutils.NukeNamespace(deps.CC, namespace)
	restorer := etcd.NewEtcdComponent(log, deps.CC, namespace, restoredDir)
	require.NoError(t, restorer.Restore(t.Context(), etcd.RestoreOptions{Snapshot: snapshot, Config: embeddedEtcdConfig(config), Label: "metadata"}))
	restored := &etcdBoot{inputs: etcdBootInputs{log: log, config: config, dataPath: restoredDir}}
	after, err := restored.startEmbedded(t.Context(), ip, containerdBootOutput{Client: deps.CC, Namespace: namespace}, observability)
	require.NoError(t, err)
	defer func() { require.NoError(t, restored.stop(context.Background())) }()
	require.Equal(t, ca.GetCACertificate(), after.tls.ClientTLS.CACert)
	runnerClient := connect(after.endpoints[0], &coordinate.EtcdTLSConfig{CertPEM: runner.CertPEM, KeyPEM: runner.KeyPEM, CACert: runner.CACert})
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	lease, err := runnerClient.Get(ctx, config.GetPrefix()+"/sub/flannel/subnets/test")
	require.NoError(t, err)
	require.Equal(t, "restored-lease", string(lease.Kvs[0].Value))
	_, err = runnerClient.Get(ctx, config.GetPrefix()+"/cluster-state/records/authority")
	require.ErrorIs(t, err, rpctypes.ErrPermissionDenied)
	registered := &registrationBoot{inputs: registrationBootInputs{log: log, dataPath: restoredDir}}
	registrationOutput, err := registered.start(t.Context())
	require.NoError(t, err)
	require.Equal(t, reg.ClusterID, registrationOutput.cloudAuth.ClusterID)
	identity := &workloadIdentityBoot{inputs: workloadIdentityBootInputs{log: log, dataPath: restoredDir}}
	identityOutput, err := identity.start(t.Context(), registrationOutput)
	require.NoError(t, err)
	_, err = identityOutput.issuer.VerifyToken(token, workloadidentity.DefaultAudience)
	require.NoError(t, err)
}
