package etcdaccess_test

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.etcd.io/etcd/api/v3/v3rpc/rpctypes"
	clientv3 "go.etcd.io/etcd/client/v3"
	"miren.dev/runtime/api/compute/compute_v1alpha"
	"miren.dev/runtime/components/coordinate"
	"miren.dev/runtime/components/etcd"
	"miren.dev/runtime/pkg/caauth"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/entity/schema"
	"miren.dev/runtime/pkg/etcdaccess"
	"miren.dev/runtime/pkg/testutils"
)

func TestRunnerCertificatesCannotAccessCoordinatorCredentials(t *testing.T) {
	deps, cleanup := testutils.NewTestDeps()
	defer cleanup()
	log := slog.New(slog.DiscardHandler)
	dir := t.TempDir()
	ca, err := coordinate.EnsureCA(log, dir)
	require.NoError(t, err)
	transport, err := coordinate.SetupEtcdTLS(log, dir, nil, nil)
	require.NoError(t, err)
	member := etcd.NewEtcdComponent(log, deps.CC, fmt.Sprintf("metadata-rbac-%d", time.Now().UnixNano()), dir)
	config := etcd.EtcdConfig{Name: "metadata-test", ClientPort: testutils.GetFreePort(t), HTTPClientPort: testutils.GetFreePort(t), PeerPort: testutils.GetFreePort(t), TLS: &etcd.TLSConfig{CertsDir: transport.CertsDir}}
	require.NoError(t, member.Start(t.Context(), config))
	defer func() { require.NoError(t, member.Stop(context.Background())) }()
	connect := func(name string) *clientv3.Client {
		cc, err := ca.IssueCertificate(caauth.Options{CommonName: name, ValidFor: time.Hour})
		require.NoError(t, err)
		cert, err := tls.X509KeyPair(cc.CertPEM, cc.KeyPEM)
		require.NoError(t, err)
		roots := x509.NewCertPool()
		require.True(t, roots.AppendCertsFromPEM(cc.CACert))
		client, err := clientv3.New(clientv3.Config{Endpoints: []string{member.ClientEndpoint()}, TLS: &tls.Config{MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{cert}, RootCAs: roots}, DialTimeout: 5 * time.Second})
		require.NoError(t, err)
		t.Cleanup(func() { require.NoError(t, client.Close()) })
		return client
	}
	admin := connect(etcdaccess.Coordinator)
	runner := connect("runner-enrolled")
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()
	// mTLS alone lets any enrolled runner read coordinator-only keys.
	_, err = admin.Put(ctx, "/miren/secret-keyring", "private-canary")
	require.NoError(t, err)
	before, err := runner.Get(ctx, "/miren/secret-keyring")
	require.NoError(t, err)
	require.Len(t, before.Kvs, 1)
	store, err := entity.NewEtcdStore(ctx, log, admin, "/miren")
	require.NoError(t, err)
	require.NoError(t, schema.Apply(ctx, store))
	node := &compute_v1alpha.Node{RunnerId: "enrolled", Name: "existing-runner"}
	_, err = store.CreateEntity(ctx, entity.New(node.Encode()))
	require.NoError(t, err)

	foundation := coordinate.NewFoundation(log, coordinate.CoordinatorConfig{DataPath: dir, Address: "localhost:0", NoAuth: true, Prefix: "/miren", EtcdEndpoints: admin.Endpoints(), EtcdTLS: transport.ClientTLS})
	require.NoError(t, foundation.PrepareMetadata(ctx))
	require.NoError(t, foundation.Start(ctx))
	defer func() { require.NoError(t, foundation.Stop(context.Background())) }()
	for _, key := range []string{"/miren/cluster-state/authority", "/miren/secret-keyring", "/miren/sub/flannel-other/config"} {
		_, err := runner.Get(ctx, key)
		require.ErrorIs(t, err, rpctypes.ErrPermissionDenied)
		_, err = runner.Put(ctx, key, "overwritten")
		require.ErrorIs(t, err, rpctypes.ErrPermissionDenied)
	}
	_, err = runner.Get(ctx, "/miren", clientv3.WithPrefix())
	require.ErrorIs(t, err, rpctypes.ErrPermissionDenied)
	_, err = runner.Put(ctx, "/miren/sub/flannel/subnets/test", "network-lease")
	require.NoError(t, err)
	lease, err := runner.Get(ctx, "/miren/sub/flannel/subnets/test")
	require.NoError(t, err)
	require.Equal(t, "network-lease", string(lease.Kvs[0].Value))

	// Provisioning is idempotent when authentication is already enabled.
	require.NoError(t, etcdaccess.Ensure(ctx, admin, "/miren"))
	require.NoError(t, etcdaccess.Runner(ctx, admin, "runner-enrolled"))
	authority, err := admin.Get(ctx, "/miren/cluster-state/authority")
	require.NoError(t, err)
	require.Len(t, authority.Kvs, 1)
}
