package coordinate

import (
	"bytes"
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	clientv3 "go.etcd.io/etcd/client/v3"
	"miren.dev/runtime/pkg/caauth"
	"miren.dev/runtime/pkg/clusterstate"
	"miren.dev/runtime/pkg/etcdtest"
	"miren.dev/runtime/pkg/registration"
	"miren.dev/runtime/pkg/rpc"
	"miren.dev/runtime/pkg/secret"
	"miren.dev/runtime/pkg/secret/keyring"
	"miren.dev/runtime/pkg/workloadidentity"
)

func TestFoundationStopClearsReleasedState(t *testing.T) {
	state, err := rpc.NewState(t.Context(), rpc.WithSkipVerify)
	require.NoError(t, err)
	foundation := &Foundation{state: state}

	shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), time.Second)
	defer cancelShutdown()
	require.NoError(t, foundation.Stop(shutdownCtx))
	require.Nil(t, foundation.state)
	require.Nil(t, foundation.etcdClient)
	require.Nil(t, foundation.store)
	require.Nil(t, foundation.etcdStore)
	require.Nil(t, foundation.eac)
	require.NoError(t, foundation.Stop(shutdownCtx))
}

func TestFoundationRestoresMetadataWithoutOriginalDataDirectory(t *testing.T) {
	client, prefix := etcdtest.TestEtcdClient(t)
	ctx := t.Context()
	log := slog.New(slog.DiscardHandler)
	start := func(dir string) (*Foundation, *SecretStore) {
		issuer, err := workloadidentity.NewIssuer(workloadidentity.IssuerConfig{DataPath: dir, IssuerURL: "https://cluster.example"})
		require.NoError(t, err)
		foundation := NewFoundation(log, CoordinatorConfig{DataPath: dir, Address: "localhost:0", EtcdEndpoints: client.Endpoints(), Prefix: prefix, NoAuth: true, WorkloadIssuer: issuer})
		require.NoError(t, foundation.Start(ctx))
		secrets := NewSecretStore(foundation)
		require.NoError(t, secrets.Start(ctx))
		return foundation, secrets
	}
	stop := func(foundation *Foundation, secrets *SecretStore) {
		secrets.Stop()
		require.NoError(t, foundation.Stop(ctx))
	}
	dir := t.TempDir()
	reg := &registration.StoredRegistration{Status: "pending", ClusterName: "restore-me", PrivateKey: "pending-registration-key"}
	require.NoError(t, registration.SaveRegistration(filepath.Join(dir, "server"), reg))
	first, secrets := start(dir)
	token, err := first.WorkloadIssuer.IssueToken("app", "sandbox")
	require.NoError(t, err)
	ca := first.authority.GetCACertificate()
	apiCert := append([]byte(nil), first.apiCert...)
	cookieKey := first.OIDCSigningKey()
	backend, err := secrets.Registry().Writable(secret.ClusterBackendName)
	require.NoError(t, err)
	version, _, err := backend.Put(ctx, "test/restore", []byte("recoverable-secret"))
	require.NoError(t, err)
	stop(first, secrets)
	sealingKey, err := os.ReadFile(clusterstate.SealingKeyPath(dir))
	require.NoError(t, err)
	_, caKey, err := first.authority.ExportPEM()
	require.NoError(t, err)
	metadata, err := client.Get(ctx, prefix+"/cluster-state/", clientv3.WithPrefix())
	require.NoError(t, err)
	for _, kv := range metadata.Kvs {
		require.False(t, bytes.Contains(kv.Value, sealingKey))
		require.False(t, bytes.Contains(kv.Value, caKey))
		require.False(t, bytes.Contains(kv.Value, []byte(reg.PrivateKey)))
	}
	legacyAuthority, err := client.Get(ctx, prefix+"/cluster-state/authority")
	require.NoError(t, err)
	require.Empty(t, legacyAuthority.Kvs, "authority must not be stored unsealed")
	caOnly, err := clusterstate.New(client, prefix, caKey)
	require.NoError(t, err)
	_, err = caOnly.Get(ctx, "authority")
	require.ErrorContains(t, err, "decrypting cluster state")
	caOnlyRing, err := keyring.NewSharedStore(client, prefix, t.TempDir(), caKey)
	require.NoError(t, err)
	_, _, err = caOnlyRing.Lock(ctx)
	require.ErrorContains(t, err, "decrypting replicated secret keyring")
	restoreDir := func() string {
		dir := t.TempDir()
		require.NoError(t, os.MkdirAll(filepath.Join(dir, "server"), 0700))
		require.NoError(t, os.WriteFile(clusterstate.SealingKeyPath(dir), sealingKey, 0600))
		return dir
	}
	for _, missing := range []bool{true, false} {
		dir := t.TempDir()
		if !missing {
			require.NoError(t, os.MkdirAll(filepath.Join(dir, "server"), 0700))
			require.NoError(t, os.WriteFile(clusterstate.SealingKeyPath(dir), bytes.Repeat([]byte{42}, 32), 0600))
		}
		foundation := NewFoundation(log, CoordinatorConfig{DataPath: dir, EtcdEndpoints: client.Endpoints(), Prefix: prefix})
		err := foundation.PrepareMetadata(ctx)
		if missing {
			require.ErrorContains(t, err, "restore the original key")
			require.NoFileExists(t, clusterstate.SealingKeyPath(dir))
		} else {
			require.ErrorContains(t, err, "decrypting cluster state")
		}
		after, err := client.Get(ctx, prefix+"/cluster-state/", clientv3.WithPrefix())
		require.NoError(t, err)
		require.Equal(t, metadata.Kvs, after.Kvs, "failed restore must not change authoritative metadata")
	}
	require.NoError(t, os.RemoveAll(dir))

	restoredDir := restoreDir()
	// Embedded etcd can provision a temporary transport CA before foundation
	// reads restored state. Neither it nor its TLS cache may replace the root.
	_, err = EnsureCA(log, restoredDir)
	require.NoError(t, err)
	_, err = SetupEtcdTLS(log, restoredDir, nil, nil)
	require.NoError(t, err)
	restored, restoredSecrets := start(restoredDir)
	require.Equal(t, ca, restored.authority.GetCACertificate())
	require.Equal(t, apiCert, restored.apiCert)
	require.Equal(t, cookieKey, restored.OIDCSigningKey())
	_, err = restored.WorkloadIssuer.VerifyToken(token, workloadidentity.DefaultAudience)
	require.NoError(t, err)
	gotReg, err := registration.LoadRegistration(filepath.Join(restoredDir, "server"))
	require.NoError(t, err)
	require.Equal(t, reg, gotReg)
	gotSecret, err := restoredSecrets.Registry().ResolveRef(ctx, secret.ClusterBackendName, secret.FormatRef("test/restore", version))
	require.NoError(t, err)
	require.Equal(t, []byte("recoverable-secret"), gotSecret.Bytes)
	stop(restored, restoredSecrets)

	tlsSetup, err := SetupEtcdTLS(log, restoredDir, nil, nil)
	require.NoError(t, err)
	cert, err := caauth.LoadCertificate(tlsSetup.ClientTLS.CertPEM)
	require.NoError(t, err)
	root, err := caauth.LoadCertificate(ca)
	require.NoError(t, err)
	require.NoError(t, cert.CheckSignatureFrom(root))
	// A second start must not re-import the temporary keys generated on restore.
	restarted, restartedSecrets := start(restoredDir)
	_, err = restarted.WorkloadIssuer.VerifyToken(token, workloadidentity.DefaultAudience)
	require.NoError(t, err)

	// An explicit local administrative change still takes effect on restart.
	reg.ClusterName = "edited-locally"
	require.NoError(t, registration.SaveRegistration(filepath.Join(restoredDir, "server"), reg))
	stop(restarted, restartedSecrets)
	edited, editedSecrets := start(restoredDir)
	stop(edited, editedSecrets)
	final, finalSecrets := start(restoreDir())
	gotReg, err = registration.LoadRegistration(filepath.Join(final.DataPath, "server"))
	require.NoError(t, err)
	require.Equal(t, "edited-locally", gotReg.ClusterName)
	stop(final, finalSecrets)

	// Existing manual key rotation and unregister workflows must not be undone
	// by loading their previously cached etcd records.
	serverDir := filepath.Join(final.DataPath, "server")
	require.NoError(t, os.Rename(filepath.Join(serverDir, "workload-identity.key"), filepath.Join(serverDir, "workload-identity.key.prev")))
	require.NoError(t, registration.ClearRegistration(serverDir))
	rotated, rotatedSecrets := start(final.DataPath)
	rotatedToken, err := rotated.WorkloadIssuer.IssueToken("app", "sandbox")
	require.NoError(t, err)
	stop(rotated, rotatedSecrets)
	afterRotation, afterRotationSecrets := start(restoreDir())
	defer stop(afterRotation, afterRotationSecrets)
	for _, token := range []string{token, rotatedToken} {
		_, err = afterRotation.WorkloadIssuer.VerifyToken(token, workloadidentity.DefaultAudience)
		require.NoError(t, err)
	}
	gotReg, err = registration.LoadRegistration(filepath.Join(afterRotation.DataPath, "server"))
	require.NoError(t, err)
	require.Nil(t, gotReg)
}

func TestMetadataMigrationRetriesAfterRollbackWithHydrationMarker(t *testing.T) {
	client, prefix := etcdtest.TestEtcdClient(t)
	dir := t.TempDir()
	serverDir := filepath.Join(dir, "server")
	reg := &registration.StoredRegistration{Status: "pending", ClusterName: "retry-migration", PrivateKey: "original-local-key"}
	require.NoError(t, registration.SaveRegistration(serverDir, reg))
	foundation := NewFoundation(slog.New(slog.DiscardHandler), CoordinatorConfig{DataPath: dir, EtcdEndpoints: client.Endpoints(), Prefix: prefix})
	require.NoError(t, foundation.PrepareMetadata(t.Context()))
	// An interrupted hydration leaves its marker on the host, while rollback
	// restores the pre-migration snapshot with no metadata records.
	require.NoError(t, os.WriteFile(filepath.Join(serverDir, "registration.etcd-hydrating"), nil, 0600))
	_, err := client.Delete(t.Context(), prefix+"/cluster-state/", clientv3.WithPrefix())
	require.NoError(t, err)
	require.NoError(t, foundation.PrepareMetadata(t.Context()))
	require.NoFileExists(t, filepath.Join(serverDir, "registration.etcd-hydrating"))
	got, err := registration.LoadRegistration(serverDir)
	require.NoError(t, err)
	require.Equal(t, reg, got)
	require.NoError(t, foundation.PrepareMetadata(t.Context()))
}
