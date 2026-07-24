package coordinate_test

import (
	"crypto/x509"
	"encoding/pem"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"miren.dev/runtime/components/coordinate"
)

// TestEtcdTLSColdBoot pins the ordering invariant behind MIR-1464: a fresh data
// dir has no CA, so SetupEtcdTLS must fail until the CA is materialized, and
// EnsureCA (which the server now calls before the etcd TLS block) is what makes
// it succeed. This reproduces a fresh `server install` with distributed runners
// enabled, where nothing has bootstrapped a CA yet.
func TestEtcdTLSColdBoot(t *testing.T) {
	r := require.New(t)
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug}))

	dataPath := t.TempDir()

	// Cold boot: no CA on disk yet, so SetupEtcdTLS can't proceed.
	_, err := coordinate.SetupEtcdTLS(log, dataPath, nil, nil)
	r.Error(err, "SetupEtcdTLS should fail before a CA exists")

	// EnsureCA is the early bootstrap step. It should create the CA on disk.
	ca, err := coordinate.EnsureCA(log, dataPath)
	r.NoError(err)
	r.NotNil(ca)

	caCert := filepath.Join(dataPath, "server", "ca.crt")
	caKey := filepath.Join(dataPath, "server", "ca.key")
	r.FileExists(caCert)
	r.FileExists(caKey)

	// With the CA in place, etcd TLS setup now succeeds.
	result, err := coordinate.SetupEtcdTLS(log, dataPath, nil, nil)
	r.NoError(err, "SetupEtcdTLS should succeed once EnsureCA has run")
	r.NotNil(result)

	// The issued etcd server cert must chain to the CA we created.
	roots := x509.NewCertPool()
	caPEM, err := os.ReadFile(result.CAFile)
	r.NoError(err)
	r.True(roots.AppendCertsFromPEM(caPEM), "CA cert should be parseable")

	serverCert := parseCert(t, filepath.Join(result.CertsDir, "server.crt"))
	_, err = serverCert.Verify(x509.VerifyOptions{
		Roots:     roots,
		DNSName:   "localhost",
		KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	})
	r.NoError(err, "etcd server cert should chain to the CA and cover localhost")

	// The coordinator client cert should chain to the same CA.
	clientCert := parseCert(t, result.ClientCertFile)
	_, err = clientCert.Verify(x509.VerifyOptions{
		Roots:     roots,
		KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	})
	r.NoError(err, "etcd client cert should chain to the CA")
}

// TestEnsureCAIdempotent confirms EnsureCA reuses an existing CA rather than
// regenerating it, so calling it early (server boot) and again later
// (auth generate, Coordinator.Start) all see the same authority.
func TestEnsureCAIdempotent(t *testing.T) {
	r := require.New(t)
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug}))

	dataPath := t.TempDir()

	_, err := coordinate.EnsureCA(log, dataPath)
	r.NoError(err)

	caCert := filepath.Join(dataPath, "server", "ca.crt")
	first, err := os.ReadFile(caCert)
	r.NoError(err)

	_, err = coordinate.EnsureCA(log, dataPath)
	r.NoError(err)

	second, err := os.ReadFile(caCert)
	r.NoError(err)
	r.Equal(first, second, "EnsureCA should reuse the existing CA on subsequent calls")
}

// TestEnsureCADoesNotOverwriteUnreadableCA guards against silently regenerating
// a CA when its cert exists but can't be read. A transient read error must not
// be mistaken for absence, since regenerating would clobber a valid CA and
// invalidate every cert it issued. We simulate an unreadable cert by making
// ca.crt a directory, which yields a non-ErrNotExist read error on every OS and
// even as root (unlike chmod 0000).
func TestEnsureCADoesNotOverwriteUnreadableCA(t *testing.T) {
	r := require.New(t)
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug}))

	dataPath := t.TempDir()
	caCert := filepath.Join(dataPath, "server", "ca.crt")
	r.NoError(os.MkdirAll(caCert, 0755))

	_, err := coordinate.EnsureCA(log, dataPath)
	r.Error(err, "EnsureCA should refuse to proceed when the CA cert is unreadable")

	// The path must be left untouched: still a directory, no key written next to it.
	info, statErr := os.Stat(caCert)
	r.NoError(statErr)
	r.True(info.IsDir(), "EnsureCA must not overwrite the existing path")
	r.NoFileExists(filepath.Join(dataPath, "server", "ca.key"))
}

func parseCert(t *testing.T, path string) *x509.Certificate {
	t.Helper()
	pemBytes, err := os.ReadFile(path)
	require.NoError(t, err)
	block, _ := pem.Decode(pemBytes)
	require.NotNil(t, block, "expected a PEM block in %s", path)
	cert, err := x509.ParseCertificate(block.Bytes)
	require.NoError(t, err)
	return cert
}
