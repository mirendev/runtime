package rpc_test

import (
	"context"
	"crypto/x509"
	"encoding/pem"
	"math/big"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"miren.dev/runtime/pkg/caauth"
	"miren.dev/runtime/pkg/cond"
	"miren.dev/runtime/pkg/rpc"
	"miren.dev/runtime/pkg/rpc/example"
	"miren.dev/runtime/pkg/rpc/stream"
)

// heldEmit emits one reading and then holds the stream open until its context
// ends, standing in for exec, attach, or a log follow.
type heldEmit struct {
	started chan struct{}
	ended   chan error
}

func (h *heldEmit) Emit(ctx context.Context, call *example.EmitTempsEmit) error {
	close(h.started)
	<-ctx.Done()
	h.ended <- context.Cause(ctx)
	return ctx.Err()
}

type revocationHarness struct {
	t           *testing.T
	ca          *caauth.Authority
	server      *rpc.State
	revocations *rpc.CertRevocations
}

func newRevocationHarness(t *testing.T, service func(*rpc.State), opts ...rpc.StateOption) *revocationHarness {
	t.Helper()
	r := require.New(t)

	ca, err := caauth.New(caauth.Options{CommonName: "test-ca", Organization: "miren", ValidFor: time.Hour})
	r.NoError(err)
	serverCert, err := ca.IssueCertificate(caauth.Options{
		CommonName:   "test-server",
		Organization: "miren",
		ValidFor:     time.Hour,
		DNSNames:     []string{"localhost"},
	})
	r.NoError(err)

	revocations := rpc.NewCertRevocations()
	ss, err := rpc.NewState(t.Context(), append([]rpc.StateOption{
		rpc.WithCertPEMs(serverCert.CertPEM, serverCert.KeyPEM),
		rpc.WithCertificateVerification(ca.GetCACertificate()),
		rpc.WithAuthenticator(&rpc.LocalOnlyAuthenticator{}),
		rpc.WithCertRevocations(revocations),
	}, opts...)...)
	r.NoError(err)
	service(ss)

	return &revocationHarness{t: t, ca: ca, server: ss, revocations: revocations}
}

// issue returns a client certificate generated for this test and its serial.
func (h *revocationHarness) issue(cn string) (*caauth.ClientCertificate, string) {
	h.t.Helper()
	cc, err := h.ca.IssueCertificate(caauth.Options{CommonName: cn, Organization: "miren", ValidFor: time.Hour})
	require.NoError(h.t, err)

	block, _ := pem.Decode(cc.CertPEM)
	require.NotNil(h.t, block)
	cert, err := x509.ParseCertificate(block.Bytes)
	require.NoError(h.t, err)
	return cc, rpc.CertSerial(cert.SerialNumber)
}

func (h *revocationHarness) connect(cc *caauth.ClientCertificate) (*example.EmitTempsClient, error) {
	h.t.Helper()
	opts := []rpc.StateOption{rpc.WithSkipVerify}
	if cc != nil {
		opts = append(opts, rpc.WithCertPEMs(cc.CertPEM, cc.KeyPEM))
	}
	cs, err := rpc.NewState(h.t.Context(), opts...)
	require.NoError(h.t, err)

	c, err := cs.Connect(h.server.ListenAddr(), "meter")
	if err != nil {
		return nil, err
	}
	return &example.EmitTempsClient{Client: c}, nil
}

func emit(ctx context.Context, c *example.EmitTempsClient) error {
	_, err := c.Emit(ctx, stream.StreamRecv(func(float32) error { return nil }))
	return err
}

func TestRevokedCertificateIsRefused(t *testing.T) {
	h := newRevocationHarness(t, func(s *rpc.State) {
		s.Server().ExposeValue("meter", example.AdaptEmitTemps(&exampleEmit{}))
	})
	h.revocations.Load(nil)

	kept, _ := h.issue("kept")
	revoked, serial := h.issue("revoked")
	h.revocations.Add(serial)

	t.Run("a new connection is told why", func(t *testing.T) {
		_, err := h.connect(revoked)
		var resolve *rpc.ResolveError
		require.ErrorAs(t, err, &resolve)
		require.Equal(t, rpc.AuthErrorCertRevoked, resolve.Code)
		require.Contains(t, resolve.Detail, serial)
	})

	t.Run("other certificates are unaffected", func(t *testing.T) {
		c, err := h.connect(kept)
		require.NoError(t, err)
		require.NoError(t, emit(t.Context(), c))
	})
}

// An established QUIC connection must not grandfather a certificate in: the
// next request on it is refused.
func TestRevocationReachesEstablishedConnection(t *testing.T) {
	h := newRevocationHarness(t, func(s *rpc.State) {
		s.Server().ExposeValue("meter", example.AdaptEmitTemps(&exampleEmit{}))
	})
	h.revocations.Load(nil)

	cc, serial := h.issue("connected")
	c, err := h.connect(cc)
	require.NoError(t, err)
	require.NoError(t, emit(t.Context(), c))

	h.revocations.Add(serial)

	err = emit(t.Context(), c)
	var denial cond.ErrRemote
	require.ErrorAs(t, err, &denial)
	require.Equal(t, "unauthorized", denial.Code)
}

// A callstream is authenticated only when it opens, so revoking must end the
// ones already running rather than wait for them to finish.
func TestRevocationEndsOpenStream(t *testing.T) {
	held := &heldEmit{started: make(chan struct{}), ended: make(chan error, 1)}
	h := newRevocationHarness(t, func(s *rpc.State) {
		s.Server().ExposeValue("meter", example.AdaptEmitTemps(held))
	})
	h.revocations.Load(nil)

	cc, serial := h.issue("streaming")
	c, err := h.connect(cc)
	require.NoError(t, err)

	clientDone := make(chan error, 1)
	go func() { clientDone <- emit(t.Context(), c) }()

	select {
	case <-held.started:
	case <-time.After(10 * time.Second):
		t.Fatal("stream never started")
	}

	h.revocations.Add(serial)

	select {
	case cause := <-held.ended:
		var revoked *rpc.CertRevokedError
		require.ErrorAs(t, cause, &revoked)
	case <-time.After(10 * time.Second):
		t.Fatal("handler outlived the revocation")
	}

	select {
	case err := <-clientDone:
		require.Error(t, err)
	case <-time.After(10 * time.Second):
		t.Fatal("client stream outlived the revocation")
	}
}

// detachedEmit keeps sending from a detached context, ignoring cancellation the
// way a running build does.
type detachedEmit struct {
	started chan struct{}
	stopped chan struct{}
}

func (d *detachedEmit) Emit(ctx context.Context, call *example.EmitTempsEmit) error {
	detached, cancel := rpc.Detach(ctx)
	defer cancel()
	defer close(d.stopped)

	emitter := call.Args().Emitter()
	close(d.started)
	for {
		if _, err := emitter.Send(detached, 1); err != nil {
			return err
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// Cancellation only reaches handlers that honor it, so revoking has to cut the
// transport as well.
func TestRevocationStopsDetachedStream(t *testing.T) {
	detached := &detachedEmit{started: make(chan struct{}), stopped: make(chan struct{})}
	h := newRevocationHarness(t, func(s *rpc.State) {
		s.Server().ExposeValue("meter", example.AdaptEmitTemps(detached))
	})
	h.revocations.Load(nil)

	cc, serial := h.issue("building")
	c, err := h.connect(cc)
	require.NoError(t, err)

	clientDone := make(chan error, 1)
	go func() { clientDone <- emit(t.Context(), c) }()

	select {
	case <-detached.started:
	case <-time.After(10 * time.Second):
		t.Fatal("stream never started")
	}

	h.revocations.Add(serial)

	select {
	case <-clientDone:
	case <-time.After(10 * time.Second):
		t.Fatal("client kept receiving after the revocation")
	}
	select {
	case <-detached.stopped:
	case <-time.After(10 * time.Second):
		t.Fatal("handler kept sending after the revocation")
	}
}

func TestCertsWaitForRevocationsToLoad(t *testing.T) {
	h := newRevocationHarness(t, func(s *rpc.State) {
		s.Server().ExposeValue("meter", example.AdaptEmitTemps(&exampleEmit{}))
	})
	cc, _ := h.issue("early")

	_, err := h.connect(cc)
	var resolve *rpc.ResolveError
	require.ErrorAs(t, err, &resolve)
	require.Equal(t, 503, resolve.StatusCode, "a cert must not get in before the set loads")

	// Certless callers authenticate some other way, so the gate leaves them be.
	_, err = h.connect(nil)
	require.NoError(t, err)

	h.revocations.Load(nil)

	c, err := h.connect(cc)
	require.NoError(t, err)
	require.NoError(t, emit(t.Context(), c))
}

type alwaysAuthenticated struct{}

func (alwaysAuthenticated) Authenticate(context.Context, *rpc.Credentials) (*rpc.Identity, error) {
	return &rpc.Identity{Subject: "bearer-user", Method: rpc.AuthMethodJWT}, nil
}

// The revocation check sits ahead of authentication, so a credential that would
// otherwise succeed cannot carry a revoked certificate through.
func TestRevokedCertificateBeatsOtherCredentials(t *testing.T) {
	h := newRevocationHarness(t, func(s *rpc.State) {
		s.Server().ExposeValue("meter", example.AdaptEmitTemps(&exampleEmit{}))
	}, rpc.WithAuthenticator(alwaysAuthenticated{}))
	h.revocations.Load(nil)

	cc, serial := h.issue("both")
	h.revocations.Add(serial)

	_, err := h.connect(cc)
	var resolve *rpc.ResolveError
	require.ErrorAs(t, err, &resolve)
	require.Equal(t, rpc.AuthErrorCertRevoked, resolve.Code)
}

func TestCertSerialIsCanonical(t *testing.T) {
	serial, ok := new(big.Int).SetString("0ABC", 16)
	require.True(t, ok)
	require.Equal(t, "abc", rpc.CertSerial(serial))
}

func TestParseCertSerial(t *testing.T) {
	serial, err := rpc.ParseCertSerial("17CAFA9B5C6FE138F6E0113E5686A658")
	require.NoError(t, err)
	require.Equal(t, "17cafa9b5c6fe138f6e0113e5686a658", serial)

	for _, bad := range []string{"", "0", "-1", "+1", "zz", "1:2", strings.Repeat("f", 41)} {
		_, err := rpc.ParseCertSerial(bad)
		require.Error(t, err, "serial %q", bad)
	}
}
