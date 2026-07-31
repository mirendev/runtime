package rpc_test

import (
	"crypto/tls"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/quic-go/quic-go/http3"
	"github.com/stretchr/testify/require"
	"miren.dev/runtime/pkg/caauth"
	"miren.dev/runtime/pkg/rpc"
)

// TestMountedHTTPHandler covers the seam that lets a cluster-internal service
// ride the RPC listener instead of opening a port of its own. Two properties
// matter and neither is obvious from the option's signature: a mounted route is
// reachable over the same QUIC/mTLS listener as RPC, and it inherits the
// listener's authentication, so a caller without a cluster certificate is
// refused before the handler runs.
//
// The client here is deliberately a bare http3.Transport rather than an RPC
// client, because that is exactly the shape a caller takes when it ships
// something that is not an RPC call.
func TestMountedHTTPHandler(t *testing.T) {
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

	var sawRequest bool
	handler := http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		sawRequest = true
		body, _ := io.ReadAll(req.Body)
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write(body)
	})

	ss, err := rpc.NewState(ctx,
		rpc.WithCertPEMs(serverCert.CertPEM, serverCert.KeyPEM),
		rpc.WithCertificateVerification(caPEM),
		rpc.WithAuthenticator(&rpc.LocalOnlyAuthenticator{}),
		rpc.WithHTTPHandler("POST /_telemetry/probe", handler),
	)
	r.NoError(err)

	post := func(t *testing.T, certs []tls.Certificate) (int, string) {
		t.Helper()
		tr := &http3.Transport{
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: true,
				Certificates:       certs,
				NextProtos:         []string{http3.NextProtoH3},
			},
		}
		defer tr.Close()

		client := &http.Client{Transport: tr}
		resp, err := client.Post("https://"+ss.LoopbackAddr()+"/_telemetry/probe",
			"text/plain", strings.NewReader("hello"))
		require.NoError(t, err)
		defer resp.Body.Close()

		body, err := io.ReadAll(resp.Body)
		require.NoError(t, err)
		return resp.StatusCode, string(body)
	}

	t.Run("reachable with a cluster certificate", func(t *testing.T) {
		pair, err := tls.X509KeyPair(clientCert.CertPEM, clientCert.KeyPEM)
		r.NoError(err)

		status, body := post(t, []tls.Certificate{pair})
		require.Equal(t, http.StatusAccepted, status)
		require.Equal(t, "hello", body)
		require.True(t, sawRequest, "handler should have run")
	})

	t.Run("refused without a certificate", func(t *testing.T) {
		// A mounted path is not an RPC path, so the server requires an identity
		// before dispatch. If this ever starts returning 202 the seam has become
		// an unauthenticated hole, which is the whole thing it exists to avoid.
		status, _ := post(t, nil)
		require.Equal(t, http.StatusUnauthorized, status)
	})
}

func TestMountedHTTPHandlerRejectsReservedPattern(t *testing.T) {
	ctx := t.Context()

	_, err := rpc.NewState(ctx,
		rpc.WithSkipVerify,
		rpc.WithHTTPHandler("/_rpc/call/hijack", http.NotFoundHandler()),
	)
	require.Error(t, err)
	require.Contains(t, err.Error(), "reserved for RPC")
}

func TestMountedHTTPHandlerRejectsNilHandler(t *testing.T) {
	ctx := t.Context()

	_, err := rpc.NewState(ctx,
		rpc.WithSkipVerify,
		rpc.WithHTTPHandler("/_telemetry/probe", nil),
	)
	require.Error(t, err)
	require.Contains(t, err.Error(), "nil")
}
