package commands

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha1"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	"math/big"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"miren.dev/runtime/clientconfig"
)

func generateTestCert(t *testing.T) (string, string) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)

	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "test-ca"},
		NotBefore:    time.Now(),
		NotAfter:     time.Now().Add(10 * 365 * 24 * time.Hour),
		IsCA:         true,
		KeyUsage:     x509.KeyUsageCertSign,
	}
	derBytes, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	require.NoError(t, err)

	pemBytes := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: derBytes})
	sum := sha1.Sum(derBytes)
	fingerprint := hex.EncodeToString(sum[:])

	return string(pemBytes), fingerprint
}

func testContext(t *testing.T) (*Context, *bytes.Buffer) {
	t.Helper()
	var stdout bytes.Buffer
	ctx := &Context{
		Stdout: &stdout,
		Stderr: &bytes.Buffer{},
	}
	return ctx, &stdout
}

func TestClusterExportAddress(t *testing.T) {
	t.Run("active cluster", func(t *testing.T) {
		r := require.New(t)
		certPEM, fingerprint := generateTestCert(t)

		cfg := clientconfig.NewConfig()
		cfg.SetCluster("my-cluster", &clientconfig.ClusterConfig{
			Hostname: "10.0.0.1:8443",
			CACert:   certPEM,
		})
		cfg.SetActiveCluster("my-cluster")

		ctx, stdout := testContext(t)
		err := ClusterExportAddress(ctx, struct{ ConfigCentric }{
			ConfigCentric: ConfigCentric{cfg: cfg},
		})
		r.NoError(err)
		r.Equal("10.0.0.1:8443;sha1:"+fingerprint+"\n", stdout.String())
	})

	t.Run("cluster via -C flag", func(t *testing.T) {
		r := require.New(t)
		certPEM, fingerprint := generateTestCert(t)

		cfg := clientconfig.NewConfig()
		cfg.SetCluster("other-cluster", &clientconfig.ClusterConfig{
			Hostname: "10.0.0.2:8443",
			CACert:   certPEM,
		})

		ctx, stdout := testContext(t)
		err := ClusterExportAddress(ctx, struct{ ConfigCentric }{
			ConfigCentric: ConfigCentric{Cluster: "other-cluster", cfg: cfg},
		})
		r.NoError(err)
		r.Equal("10.0.0.2:8443;sha1:"+fingerprint+"\n", stdout.String())
	})

	t.Run("-C takes priority over active cluster", func(t *testing.T) {
		r := require.New(t)
		certPEM, fingerprint := generateTestCert(t)

		cfg := clientconfig.NewConfig()
		cfg.SetCluster("active-cluster", &clientconfig.ClusterConfig{
			Hostname: "10.0.0.1:8443",
			CACert:   certPEM,
		})
		cfg.SetCluster("target-cluster", &clientconfig.ClusterConfig{
			Hostname: "10.0.0.2:8443",
			CACert:   certPEM,
		})
		cfg.SetActiveCluster("active-cluster")

		ctx, stdout := testContext(t)
		err := ClusterExportAddress(ctx, struct{ ConfigCentric }{
			ConfigCentric: ConfigCentric{Cluster: "target-cluster", cfg: cfg},
		})
		r.NoError(err)
		r.Equal("10.0.0.2:8443;sha1:"+fingerprint+"\n", stdout.String())
	})

	t.Run("no cluster specified and no active", func(t *testing.T) {
		r := require.New(t)

		cfg := clientconfig.NewConfig()

		ctx, _ := testContext(t)
		err := ClusterExportAddress(ctx, struct{ ConfigCentric }{
			ConfigCentric: ConfigCentric{cfg: cfg},
		})
		r.Error(err)
		r.Contains(err.Error(), "miren cluster add")
	})

	t.Run("cluster not found", func(t *testing.T) {
		r := require.New(t)

		cfg := clientconfig.NewConfig()

		ctx, _ := testContext(t)
		err := ClusterExportAddress(ctx, struct{ ConfigCentric }{
			ConfigCentric: ConfigCentric{Cluster: "nonexistent", cfg: cfg},
		})
		r.Error(err)
		r.Contains(err.Error(), "nonexistent")
	})

	t.Run("cluster with invalid CA cert", func(t *testing.T) {
		r := require.New(t)

		cfg := clientconfig.NewConfig()
		cfg.SetCluster("bad-cert", &clientconfig.ClusterConfig{
			Hostname: "10.0.0.3:8443",
			CACert:   "not-a-pem",
		})

		ctx, _ := testContext(t)
		err := ClusterExportAddress(ctx, struct{ ConfigCentric }{
			ConfigCentric: ConfigCentric{Cluster: "bad-cert", cfg: cfg},
		})
		r.Error(err)
		r.Contains(err.Error(), "no valid CA certificate")
	})
}
