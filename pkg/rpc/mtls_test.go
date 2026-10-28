package rpc_test

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"miren.dev/runtime/pkg/caauth"
	"miren.dev/runtime/pkg/cond"
	"miren.dev/runtime/pkg/rpc"
	"miren.dev/runtime/pkg/rpc/example"
	"miren.dev/runtime/pkg/rpc/stream"
)

// TestServerVerifiesClientCertChain is the end-to-end regression test for the
// RPC client-cert auth bypass. It stands up a real QUIC/mTLS listener
// configured with a cluster CA and drives three clients through an actual TLS
// handshake:
//
//   - a genuinely CA-issued client cert is accepted and its identity flows
//     through to authorize a non-public method;
//   - a forged client cert (self-signed by an attacker key, with its issuer DN
//     spoofed to match the CA subject exactly -- the mint.go attack) is
//     rejected and gains no access;
//   - a certless caller still completes the handshake and reaches the auth
//     layer (proving JWT/OIDC callers are not broken), where it is cleanly 401'd
//     on a non-public method.
//
// Unlike the unit tests in authenticator_test.go, which fabricate
// r.TLS.VerifiedChains, this test exercises the listener's actual tls.Config --
// the layer where the original bug lived (tls.RequestClientCert never verifies
// the presented cert). A regression to RequestClientCert would let the forged
// client through and fail the "forged" subtest.
func TestServerVerifiesClientCertChain(t *testing.T) {
	ctx := t.Context()
	r := require.New(t)

	ca, err := caauth.New(caauth.Options{CommonName: "test-ca", Organization: "miren", ValidFor: time.Hour})
	r.NoError(err)
	caPEM := ca.GetCACertificate()

	serverCert, err := ca.IssueCertificate(caauth.Options{
		CommonName:   "test-server",
		Organization: "miren",
		ValidFor:     time.Hour,
		DNSNames:     []string{"localhost"},
	})
	r.NoError(err)

	clientCert, err := ca.IssueCertificate(caauth.Options{
		CommonName:   "test-client",
		Organization: "miren",
		ValidFor:     time.Hour,
	})
	r.NoError(err)

	forgedCertPEM, forgedKeyPEM := forgeClientCert(t, caPEM, "attacker")

	// Server: authenticates via TLS client cert and verifies presented certs
	// against the cluster CA.
	ss, err := rpc.NewState(ctx,
		rpc.WithCertPEMs(serverCert.CertPEM, serverCert.KeyPEM),
		rpc.WithCertificateVerification(caPEM),
		rpc.WithAuthenticator(&rpc.LocalOnlyAuthenticator{}),
	)
	r.NoError(err)
	ss.Server().ExposeValue("meter", example.AdaptEmitTemps(&exampleEmit{}))

	// callEmit connects a fresh client (with the given cert options) and invokes
	// Emit, a non-public method that requires an authenticated identity. It
	// returns the error from whichever step fails (handshake or call), or nil on
	// success.
	callEmit := func(clientOpts ...rpc.StateOption) error {
		opts := append([]rpc.StateOption{rpc.WithSkipVerify}, clientOpts...)
		cs, err := rpc.NewState(ctx, opts...)
		r.NoError(err)

		c, err := cs.Connect(ss.ListenAddr(), "meter")
		if err != nil {
			return err
		}

		mc := &example.EmitTempsClient{Client: c}
		recv := stream.StreamRecv(func(float32) error { return nil })
		_, err = mc.Emit(ctx, recv)
		return err
	}

	t.Run("valid CA-issued client cert is accepted", func(t *testing.T) {
		err := callEmit(rpc.WithCertPEMs(clientCert.CertPEM, clientCert.KeyPEM))
		require.NoError(t, err)
	})

	t.Run("forged client cert is rejected", func(t *testing.T) {
		// The forged cert must NOT gain access. With the fix the TLS handshake
		// rejects it (bad chain); without the fix it would be accepted and Emit
		// would succeed, failing this assertion.
		err := callEmit(rpc.WithCertPEMs(forgedCertPEM, forgedKeyPEM))
		require.Error(t, err)
	})

	t.Run("certless caller reaches auth layer and is 401'd", func(t *testing.T) {
		// A clean 401 (rather than a handshake failure) proves the listener still
		// admits certless connections -- JWT/OIDC callers authenticate via the
		// Authorization header and must not be forced to present a cert.
		err := callEmit()
		var denial cond.ErrRemote
		require.ErrorAs(t, err, &denial)
		require.Equal(t, "permission", denial.Category)
		require.Equal(t, "unauthorized", denial.Code)
	})
}

// forgeClientCert mints a client cert whose issuer DN is byte-identical to the
// real cluster CA's subject, but signed by an attacker-controlled key. Go's
// client-cert selection only compares issuer DN bytes (no signature check), so
// the stock client will present it; a server that fails to verify the chain
// would trust its CommonName as identity. This mirrors the mint.go PoC.
func forgeClientCert(t *testing.T, realCAPEM []byte, cn string) (certPEM, keyPEM []byte) {
	t.Helper()
	r := require.New(t)

	blk, _ := pem.Decode(realCAPEM)
	r.NotNil(blk)
	realCA, err := x509.ParseCertificate(blk.Bytes)
	r.NoError(err)

	fpub, fpriv, err := ed25519.GenerateKey(rand.Reader)
	r.NoError(err)
	caTmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		RawSubject:            realCA.RawSubject, // spoof the DN, not the key
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign,
	}
	caDER, err := x509.CreateCertificate(rand.Reader, caTmpl, caTmpl, fpub, fpriv)
	r.NoError(err)
	fakeCA, err := x509.ParseCertificate(caDER)
	r.NoError(err)

	leafPub, leafPriv, err := ed25519.GenerateKey(rand.Reader)
	r.NoError(err)
	leafTmpl := &x509.Certificate{
		SerialNumber: big.NewInt(2),
		Subject:      pkix.Name{CommonName: cn, Organization: []string{"miren"}},
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
		KeyUsage:     x509.KeyUsageDigitalSignature,
	}
	leafDER, err := x509.CreateCertificate(rand.Reader, leafTmpl, fakeCA, leafPub, fpriv)
	r.NoError(err)

	certPEM = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: leafDER})
	keyDER, err := x509.MarshalPKCS8PrivateKey(leafPriv)
	r.NoError(err)
	keyPEM = pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})
	return certPEM, keyPEM
}
