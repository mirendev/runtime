package etcdaccess_test

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"log/slog"
	"os"
	"path"
	"path/filepath"
	"testing"
	"time"

	"github.com/flannel-io/flannel/pkg/ip"
	"github.com/flannel-io/flannel/pkg/lease"
	"github.com/flannel-io/flannel/pkg/subnet"
	fetcd "github.com/flannel-io/flannel/pkg/subnet/etcd"
	"github.com/stretchr/testify/require"
	"go.etcd.io/etcd/api/v3/v3rpc/rpctypes"
	clientv3 "go.etcd.io/etcd/client/v3"
	"miren.dev/runtime/api/compute/compute_v1alpha"
	esv1 "miren.dev/runtime/api/entityserver/entityserver_v1alpha"
	rv1 "miren.dev/runtime/api/runner/runner_v1alpha"
	"miren.dev/runtime/components/coordinate"
	"miren.dev/runtime/components/etcd"
	"miren.dev/runtime/pkg/caauth"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/entity/schema"
	"miren.dev/runtime/pkg/etcdaccess"
	"miren.dev/runtime/pkg/rpc"
	"miren.dev/runtime/pkg/testutils"
	entitysrv "miren.dev/runtime/servers/entityserver"
	runnersrv "miren.dev/runtime/servers/runner"
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
	_, err = admin.Delete(ctx, "/miren/secret-keyring")
	require.NoError(t, err)
	store, err := entity.NewEtcdStore(ctx, log, admin, "/miren/")
	require.NoError(t, err)
	require.NoError(t, schema.Apply(ctx, store))
	node := &compute_v1alpha.Node{RunnerId: "enrolled", Name: "existing-runner"}
	_, err = store.CreateEntity(ctx, entity.New(node.Encode()))
	require.NoError(t, err)
	runnerPrefix := "/miren//sub/flannel"
	newManager := func(name string) subnet.Manager {
		cc, err := ca.IssueCertificate(caauth.Options{CommonName: name, ValidFor: time.Hour})
		require.NoError(t, err)
		dir := t.TempDir()
		certFile, keyFile, caFile := filepath.Join(dir, "client.crt"), filepath.Join(dir, "client.key"), filepath.Join(dir, "ca.crt")
		require.NoError(t, os.WriteFile(certFile, cc.CertPEM, 0600))
		require.NoError(t, os.WriteFile(keyFile, cc.KeyPEM, 0600))
		require.NoError(t, os.WriteFile(caFile, cc.CACert, 0600))
		manager, err := fetcd.NewLocalManager(ctx, &fetcd.EtcdConfig{Endpoints: admin.Endpoints(), Prefix: runnerPrefix, Certfile: certFile, Keyfile: keyFile, CAFile: caFile}, ip.IP4Net{}, ip.IP6Net{}, 60)
		require.NoError(t, err)
		return manager
	}
	_, err = admin.Put(ctx, path.Join(runnerPrefix, "config"), `{"Network":"10.42.0.0/16","Backend":{"Type":"wireguard"}}`)
	require.NoError(t, err)
	attrs := &lease.LeaseAttrs{PublicIP: ip.MustParseIP4("192.0.2.8"), BackendType: "wireguard"}
	beforeUpgrade, err := newManager(etcdaccess.Coordinator).AcquireLease(ctx, attrs)
	require.NoError(t, err)
	leaseKey := path.Join(runnerPrefix, "subnets", subnet.MakeSubnetKey(beforeUpgrade.Subnet, beforeUpgrade.IPv6Subnet))

	foundation := coordinate.NewFoundation(log, coordinate.CoordinatorConfig{DataPath: dir, Address: "localhost:0", NoAuth: true, Prefix: "/miren/", EtcdEndpoints: admin.Endpoints(), EtcdTLS: transport.ClientTLS})
	require.NoError(t, foundation.PrepareMetadata(ctx))
	require.NoError(t, foundation.Start(ctx))
	defer func() { require.NoError(t, foundation.Stop(context.Background())) }()
	manager := newManager("runner-enrolled")
	networkConfig, err := manager.GetNetworkConfig(ctx)
	require.NoError(t, err)
	require.Equal(t, "10.42.0.0/16", networkConfig.Network.String())
	afterUpgrade, err := manager.AcquireLease(ctx, attrs)
	require.NoError(t, err)
	require.Equal(t, beforeUpgrade.Subnet, afterUpgrade.Subnet, "existing runners must retain their leased subnet")
	watchCtx, cancelWatch := context.WithCancel(ctx)
	defer cancelWatch()
	updates := make(chan []lease.LeaseWatchResult, 1)
	watchDone := make(chan error, 1)
	go func() { watchDone <- manager.WatchLeases(watchCtx, updates) }()
	select {
	case snapshot := <-updates:
		require.Len(t, snapshot, 1)
		require.Len(t, snapshot[0].Snapshot, 1)
		require.Equal(t, beforeUpgrade.Subnet, snapshot[0].Snapshot[0].Subnet)
	case err := <-watchDone:
		t.Fatalf("flannel lease watch failed: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("flannel did not read the existing lease snapshot")
	}
	require.NoError(t, manager.RenewLease(ctx, afterUpgrade))
	select {
	case update := <-updates:
		require.Len(t, update, 1)
		require.Len(t, update[0].Events, 1)
		require.Equal(t, lease.EventAdded, update[0].Events[0].Type)
		require.Equal(t, beforeUpgrade.Subnet, update[0].Events[0].Lease.Subnet)
	case err := <-watchDone:
		t.Fatalf("flannel lease watch failed: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("flannel did not receive the lease renewal")
	}
	for _, key := range []string{"/miren/cluster-state/records/authority", "/miren/secret-keyring", "/miren/sub/flannel-other/config", "/miren//sub/flannel/subnets/test"} {
		_, err := runner.Get(ctx, key)
		require.ErrorIs(t, err, rpctypes.ErrPermissionDenied)
		_, err = runner.Put(ctx, key, "overwritten")
		require.ErrorIs(t, err, rpctypes.ErrPermissionDenied)
	}
	_, err = runner.Get(ctx, "/miren", clientv3.WithPrefix())
	require.ErrorIs(t, err, rpctypes.ErrPermissionDenied)

	// Legacy certificates can name a registered runner by its node name rather
	// than its RunnerId. Refresh must provision the actual verified peer CN.
	legacy := connect("runner-existing-runner")
	_, err = legacy.Get(ctx, leaseKey)
	require.ErrorIs(t, err, rpctypes.ErrPermissionDenied)
	cc, err := ca.IssueCertificate(caauth.Options{CommonName: "runner-existing-runner", Organization: "miren", ValidFor: time.Hour})
	require.NoError(t, err)
	cert, err := tls.X509KeyPair(cc.CertPEM, cc.KeyPEM)
	require.NoError(t, err)
	peer, err := x509.ParseCertificate(cert.Certificate[0])
	require.NoError(t, err)
	eac := esv1.NewEntityAccessClient(rpc.LocalClient(esv1.AdaptEntityAccess(&entitysrv.EntityServer{Log: log, Store: store})))
	registration := runnersrv.NewRegistrationServer(runnersrv.RegistrationServerConfig{Log: log, Authority: ca, EAC: eac, EtcdClient: admin})
	registrationClient := rv1.NewRunnerRegistrationClient(rpc.LocalClient(rv1.AdaptRunnerRegistration(registration)))
	response, err := registrationClient.RefreshCertificate(rpc.ContextWithConnectionInfo(ctx, &rpc.CurrentConnectionInfo{PeerCertificate: peer}), "localhost:8444")
	require.NoError(t, err)
	require.False(t, response.HasError())
	storedLease, err := legacy.Get(ctx, leaseKey)
	require.NoError(t, err)
	require.Len(t, storedLease.Kvs, 1)
	_, err = legacy.Get(ctx, "/miren/cluster-state/records/authority")
	require.ErrorIs(t, err, rpctypes.ErrPermissionDenied)

	// Provisioning is idempotent when authentication is already enabled.
	require.NoError(t, etcdaccess.Ensure(ctx, admin, "/miren/"))
	require.NoError(t, etcdaccess.Runner(ctx, admin, "runner-enrolled"))
	authority, err := admin.Get(ctx, "/miren/cluster-state/records/authority")
	require.NoError(t, err)
	require.Len(t, authority.Kvs, 1)
}
