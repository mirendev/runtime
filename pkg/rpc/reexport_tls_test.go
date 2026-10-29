package rpc_test

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"miren.dev/runtime/pkg/caauth"
	"miren.dev/runtime/pkg/rpc"
	"miren.dev/runtime/pkg/rpc/example"
)

// strictAdjuster reports whether using the handed-in setter worked, which the
// plain exampleAT swallows.
type strictAdjuster struct{}

func (strictAdjuster) Adjust(ctx context.Context, call *example.AdjustTempAdjust) error {
	_, err := call.Args().Setter().SetTemp(ctx, 72)
	return err
}

// TestReexportedCapabilityVerifiesIssuer covers the server side of a
// third-party capability: a caller hands the server a capability issued by
// another node, and the server dials that node itself. The dial must verify
// the issuer against the server's trust roots. It used to skip verification
// whenever the caller presented no client cert, so any address in the
// capability was trusted.
func TestReexportedCapabilityVerifiesIssuer(t *testing.T) {
	ctx := t.Context()

	ca, err := caauth.New(caauth.Options{CommonName: "test-ca", Organization: "miren", ValidFor: time.Hour})
	require.NoError(t, err)
	caPEM := ca.GetCACertificate()

	issue := func(cn string) (certPEM, keyPEM []byte) {
		c, err := ca.IssueCertificate(caauth.Options{
			CommonName:   cn,
			Organization: "miren",
			ValidFor:     time.Hour,
			DNSNames:     []string{"localhost"},
			IPs:          []net.IP{net.ParseIP("127.0.0.1")},
		})
		require.NoError(t, err)
		return c.CertPEM, c.KeyPEM
	}

	// run stands up an issuer with issuerOpts, then has a certless caller pass
	// the issuer's setter capability to a CA-verifying middle server.
	run := func(t *testing.T, issuerOpts ...rpc.StateOption) (float32, error) {
		r := require.New(t)

		var em exampleMeter
		em.temp = 42

		issuer, err := rpc.NewState(ctx, issuerOpts...)
		r.NoError(err)
		issuer.Server().ExposeValue("meter", example.AdaptMeter(&em))

		middleCert, middleKey := issue("middle")
		middle, err := rpc.NewState(ctx,
			rpc.WithCertPEMs(middleCert, middleKey),
			rpc.WithCertificateVerification(caPEM),
		)
		r.NoError(err)
		middle.Server().ExposeValue("adjust", example.AdaptAdjustTemp(strictAdjuster{}))

		caller, err := rpc.NewState(ctx, rpc.WithSkipVerify)
		r.NoError(err)

		mc, err := caller.Connect(issuer.ListenAddr(), "meter")
		r.NoError(err)

		res, err := (&example.MeterClient{Client: mc}).GetSetter(ctx, "test")
		r.NoError(err)

		ac, err := caller.Connect(middle.ListenAddr(), "adjust")
		r.NoError(err)

		_, err = (&example.AdjustTempClient{Client: ac}).Adjust(ctx, res.Setter().Export())
		return em.temp, err
	}

	t.Run("issuer with a CA-issued cert is reached", func(t *testing.T) {
		cert, key := issue("issuer")
		temp, err := run(t, rpc.WithCertPEMs(cert, key), rpc.WithSkipVerify)
		require.NoError(t, err)
		require.Equal(t, float32(72), temp)
	})

	t.Run("issuer with an untrusted cert is refused", func(t *testing.T) {
		temp, err := run(t, rpc.WithSkipVerify)
		require.Error(t, err)
		require.Equal(t, float32(42), temp)
	})
}
