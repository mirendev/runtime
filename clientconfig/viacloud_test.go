package clientconfig

import (
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/require"
)

// viaCloudConfig builds a config holding one cloud-routed cluster and the token
// identity it authenticates with. The token is fresh, so resolving it takes the
// cached path and touches no network.
func viaCloudConfig(t *testing.T, cluster *ClusterConfig) *Config {
	t.Helper()

	config := NewConfig()
	config.SetIdentity("cloud", &IdentityConfig{
		Type:   IdentityToken,
		Issuer: "https://api.miren.cloud",
		Token: makeToken(t, jwt.MapClaims{
			"sub": "user-1",
			"exp": time.Now().Add(time.Hour).Unix(),
		}),
	})
	config.SetCluster("prod", cluster)

	return config
}

func TestCloudRPCEndpoint(t *testing.T) {
	tests := []struct {
		cloudURL string
		want     string
	}{
		{"https://api.miren.cloud", "wss://api.miren.cloud/api/v1/clusters/cluster-abc/rpc"},
		{"https://api.miren.cloud/", "wss://api.miren.cloud/api/v1/clusters/cluster-abc/rpc"},

		// A development cloud has no certificate, so the scheme carries over
		// rather than being silently upgraded to one that cannot connect.
		{"http://localhost:3001", "ws://localhost:3001/api/v1/clusters/cluster-abc/rpc"},
		{"http://127.0.0.1:3001", "ws://127.0.0.1:3001/api/v1/clusters/cluster-abc/rpc"},

		// No scheme is assumed to be the real thing, which is always TLS.
		{"api.miren.cloud", "wss://api.miren.cloud/api/v1/clusters/cluster-abc/rpc"},
	}

	for _, tt := range tests {
		got, err := cloudRPCEndpoint(tt.cloudURL, "cluster-abc", false)
		require.NoError(t, err)
		require.Equal(t, tt.want, got, "cloudRPCEndpoint(%q)", tt.cloudURL)
	}

	_, err := cloudRPCEndpoint("https://", "cluster-abc", false)
	require.Error(t, err, "a url naming no host should not produce an endpoint")
}

// Every byte of a relayed session's authority is the caller's own credential,
// and over an unencrypted socket all of it is readable by anyone on the path.
// Loopback is the case that justifies plaintext; anywhere else has to be asked
// for, because getting it by accident is silent and costs you the token.
func TestCloudRPCEndpointRefusesRemotePlaintext(t *testing.T) {
	_, err := cloudRPCEndpoint("http://cloud.internal:3001", "cluster-abc", false)
	require.ErrorContains(t, err, "clear")

	// Asked for, it is allowed: a development cloud reachable by name is a real
	// setup, and the documented one for a container that cannot say localhost.
	got, err := cloudRPCEndpoint("http://miren.host:3001", "cluster-abc", true)
	require.NoError(t, err)
	require.Equal(t, "ws://miren.host:3001/api/v1/clusters/cluster-abc/rpc", got)
}

func TestIsLoopbackHost(t *testing.T) {
	loopback := []string{"localhost", "localhost:3001", "LocalHost:80", "127.0.0.1", "127.0.0.1:3001", "[::1]:3001", "::1"}
	for _, h := range loopback {
		require.True(t, isLoopbackHost(h), "%q should be loopback", h)
	}

	remote := []string{"miren.host:3001", "example.com", "10.0.0.1:80", "[2001:db8::1]:443"}
	for _, h := range remote {
		require.False(t, isLoopbackHost(h), "%q should not be loopback", h)
	}
}

// The happy path: a cloud-routed cluster resolves to a relay endpoint and a
// bearer, and nothing else. In particular it must not carry the cluster CA —
// the certificate on the wire is cloud's.
func TestViaCloudOptions(t *testing.T) {
	r := require.New(t)

	config := viaCloudConfig(t, &ClusterConfig{
		ViaCloud: true,
		XID:      "cluster-abc",
		Identity: "cloud",
		// Deliberately set, and deliberately unused: a cloud-routed cluster is
		// one we cannot dial, so neither its address nor its CA is on the wire.
		Hostname: "unreachable.example.com:8443",
		CACert:   testCAPEM,
	})

	cluster, err := config.GetCluster("prod")
	r.NoError(err)

	endpoint, token, err := cluster.cloudRelay(t.Context(), config)
	r.NoError(err)
	r.Equal("wss://api.miren.cloud/api/v1/clusters/cluster-abc/rpc", endpoint)
	r.NotEmpty(token)

	// Endpoint, bind address, bearer — and nothing else. A cloud-routed cluster
	// must not verify against the cluster CA, because the certificate on the
	// wire is cloud's.
	opts, err := cluster.RPCOptionsWithName(t.Context(), config, "prod")
	r.NoError(err)
	r.Len(opts, 3)
}

// An explicit cloud_url wins over the identity's issuer, which is what makes a
// cluster registered with one cloud reachable through another.
func TestViaCloudOptionsHonorsExplicitCloudURL(t *testing.T) {
	r := require.New(t)

	config := viaCloudConfig(t, &ClusterConfig{
		ViaCloud: true,
		XID:      "cluster-abc",
		Identity: "cloud",
		CloudURL: "http://miren.host:3001",
		// Reaching a development cloud by a name rather than by loopback is
		// the documented setup for a container, and it is plaintext, so it has
		// to say so.
		Insecure: true,
	})

	cluster, err := config.GetCluster("prod")
	r.NoError(err)

	endpoint, _, err := cluster.cloudRelay(t.Context(), config)
	r.NoError(err)
	r.Equal("ws://miren.host:3001/api/v1/clusters/cluster-abc/rpc", endpoint)
}

// The same entry without that admission is refused, which is what makes the
// admission mean something.
func TestViaCloudRefusesRemotePlaintextByDefault(t *testing.T) {
	config := viaCloudConfig(t, &ClusterConfig{
		ViaCloud: true,
		XID:      "cluster-abc",
		Identity: "cloud",
		CloudURL: "http://miren.host:3001",
	})

	cluster, err := config.GetCluster("prod")
	require.NoError(t, err)

	_, err = cluster.RPCOptionsWithName(t.Context(), config, "prod")
	require.ErrorContains(t, err, "clear")
}

// Each of these is a config that cannot produce a working connection. Failing
// here names the missing piece; failing later would surface as a 404 or a 401
// from a hop the user did not know was in the path.
func TestViaCloudOptionsRejectsIncompleteConfigs(t *testing.T) {
	tests := []struct {
		name    string
		cluster *ClusterConfig
		wants   string
	}{
		{
			name:    "no xid",
			cluster: &ClusterConfig{ViaCloud: true, Identity: "cloud"},
			wants:   "xid",
		},
		{
			name:    "no identity",
			cluster: &ClusterConfig{ViaCloud: true, XID: "cluster-abc"},
			wants:   "identity",
		},
		{
			name:    "unknown identity",
			cluster: &ClusterConfig{ViaCloud: true, XID: "cluster-abc", Identity: "nope"},
			wants:   "nope",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := viaCloudConfig(t, tt.cluster)

			cluster, err := config.GetCluster("prod")
			require.NoError(t, err)

			_, err = cluster.RPCOptionsWithName(t.Context(), config, "prod")
			require.ErrorContains(t, err, tt.wants)
		})
	}
}

// A certificate identity cannot be relayed: the client certificate would
// terminate at cloud, which is not the cluster. The error has to say that
// rather than letting the connection fail as unauthorized.
func TestViaCloudOptionsRejectsCertificateIdentity(t *testing.T) {
	config := NewConfig()
	config.SetIdentity("cert", &IdentityConfig{
		Type:       IdentityCertificate,
		Issuer:     "https://api.miren.cloud",
		ClientCert: testCAPEM,
		ClientKey:  testCAPEM,
	})
	config.SetCluster("prod", &ClusterConfig{
		ViaCloud: true,
		XID:      "cluster-abc",
		Identity: "cert",
	})

	cluster, err := config.GetCluster("prod")
	require.NoError(t, err)

	_, err = cluster.RPCOptionsWithName(t.Context(), config, "prod")
	require.ErrorContains(t, err, "certificate")
}

// A cluster with no cloud named anywhere has nowhere to route to.
func TestViaCloudOptionsRequiresACloud(t *testing.T) {
	config := NewConfig()
	config.SetIdentity("cloud", &IdentityConfig{
		Type:  IdentityToken,
		Token: makeToken(t, jwt.MapClaims{"exp": time.Now().Add(time.Hour).Unix()}),
	})
	config.SetCluster("prod", &ClusterConfig{
		ViaCloud: true,
		XID:      "cluster-abc",
		Identity: "cloud",
	})

	cluster, err := config.GetCluster("prod")
	require.NoError(t, err)

	// "cloud" appears in most errors on this path, including the one for
	// failing to authenticate, so matching it would pass on the wrong failure.
	_, err = cluster.RPCOptionsWithName(t.Context(), config, "prod")
	require.ErrorContains(t, err, "neither it nor identity")
}
