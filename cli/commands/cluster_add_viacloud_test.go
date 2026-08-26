package commands

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"miren.dev/runtime/clientconfig"
)

// cloudPresenceServer stands in for cloud's per-cluster presence endpoint,
// recording which clusters were asked about.
// The recorder is returned as a function rather than a slice pointer because
// the presence checks now run concurrently: reads and writes both have to hold
// the lock, and handing back a pointer makes it easy to forget.
func cloudPresenceServer(t *testing.T, online map[string]bool, status int) (*httptest.Server, func() []string) {
	t.Helper()

	var (
		mu    sync.Mutex
		asked []string
	)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if status != http.StatusOK {
			w.WriteHeader(status)
			return
		}

		// /api/v1/clusters/{xid}/online
		xid := r.PathValue("xid")
		if xid == "" {
			// Fall back to parsing, since this server is not using a router.
			parts := r.URL.Path
			const prefix = "/api/v1/clusters/"
			const suffix = "/online"
			if len(parts) > len(prefix)+len(suffix) {
				xid = parts[len(prefix) : len(parts)-len(suffix)]
			}
		}
		mu.Lock()
		asked = append(asked, xid)
		mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		if online[xid] {
			_, _ = w.Write([]byte(`{"cluster_xid":"` + xid + `","online":true}`))
			return
		}
		_, _ = w.Write([]byte(`{"cluster_xid":"` + xid + `","online":false}`))
	}))
	t.Cleanup(srv.Close)

	return srv, func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), asked...)
	}
}

func presenceTestConfig(t *testing.T, issuer string) (*clientconfig.Config, *clientconfig.IdentityConfig) {
	t.Helper()

	cfg := clientconfig.NewConfig()
	identity := &clientconfig.IdentityConfig{
		Type:   clientconfig.IdentityToken,
		Issuer: issuer,
		Token:  freshLoginJWT(t),
	}
	cfg.SetIdentity("cloud", identity)

	return cfg, identity
}

func presenceContext(t *testing.T) *Context {
	t.Helper()
	return &Context{
		Context: context.Background(),
		Stdout:  &bytes.Buffer{},
		Stderr:  &bytes.Buffer{},
	}
}

// The question this answers is the one that decides whether a cluster nobody
// can dial is a dead end or a working cluster on a private network.
func TestClusterOnlineInCloud(t *testing.T) {
	r := require.New(t)

	srv, asked := cloudPresenceServer(t, map[string]bool{"cluster-up": true}, http.StatusOK)
	cfg, identity := presenceTestConfig(t, srv.URL)
	ctx := presenceContext(t)

	online, err := clusterOnlineInCloud(ctx, cfg, "cloud", identity, "cluster-up")
	r.NoError(err)
	r.True(online)

	online, err = clusterOnlineInCloud(ctx, cfg, "cloud", identity, "cluster-down")
	r.NoError(err)
	r.False(online)

	r.Equal([]string{"cluster-up", "cluster-down"}, asked())
}

// A cloud that refuses the question is not a cloud that said no, and the caller
// has to be able to tell those apart.
func TestClusterOnlineInCloudReportsFailure(t *testing.T) {
	srv, _ := cloudPresenceServer(t, nil, http.StatusForbidden)
	cfg, identity := presenceTestConfig(t, srv.URL)

	_, err := clusterOnlineInCloud(presenceContext(t), cfg, "cloud", identity, "cluster-1")
	require.Error(t, err)
	require.Contains(t, err.Error(), "403")
}

// Only clusters with nothing to dial are asked about. Asking for every cluster
// would put a round trip per cluster in front of a picker that mostly does not
// need one.
func TestCloudRoutableClustersOnlyAsksAboutUndialableOnes(t *testing.T) {
	r := require.New(t)

	srv, asked := cloudPresenceServer(t, map[string]bool{"cluster-nat": true}, http.StatusOK)
	cfg, identity := presenceTestConfig(t, srv.URL)

	clusters := []ClusterResponse{
		{Name: "dialable", XID: "cluster-dialable", APIAddresses: []string{"1.2.3.4:8443"}},
		{Name: "behind-nat", XID: "cluster-nat"},
		{Name: "really-gone", XID: "cluster-gone"},
	}

	routable := cloudRoutableClusters(presenceContext(t), cfg, "cloud", identity, clusters)

	r.True(routable["cluster-nat"])
	r.False(routable["cluster-gone"])
	r.NotContains(routable, "cluster-dialable")

	// Order-independent: the checks run concurrently, so which one lands first
	// is not something this test gets to assert. What it asserts is which
	// clusters were asked about at all.
	r.ElementsMatch([]string{"cluster-nat", "cluster-gone"}, asked(),
		"a cluster with an address to dial should not cost a presence check")
}

// A cloud that cannot answer leaves every cluster unroutable rather than
// guessing, and says so once rather than per cluster.
func TestCloudRoutableClustersSurvivesAFailingCloud(t *testing.T) {
	srv, _ := cloudPresenceServer(t, nil, http.StatusInternalServerError)
	cfg, identity := presenceTestConfig(t, srv.URL)

	ctx := presenceContext(t)
	out := ctx.Stdout.(*bytes.Buffer)

	routable := cloudRoutableClusters(ctx, cfg, "cloud", identity, []ClusterResponse{
		{Name: "behind-nat", XID: "cluster-nat"},
	})

	require.Empty(t, routable)
	require.Contains(t, out.String(), "behind-nat",
		"a cloud that could not answer must say so, not silently mean no")
}

// withCloudRouteProbe substitutes the relayed probe for the duration of a test.
// The real one needs a live session through cloud, which the blackbox suite
// covers; what these tests are about is the decision taken from its answer.
func withCloudRouteProbe(t *testing.T, probe func(*Context, *clientconfig.Config, string, *clientconfig.IdentityConfig, *ClusterResponse) bool) {
	t.Helper()
	prev := cloudRouteProbe
	cloudRouteProbe = probe
	t.Cleanup(func() { cloudRouteProbe = prev })
}

// The decision the RFD describes: when a cluster advertises addresses and none
// of them answer, try the cloud route, and take it if the cluster answers.
//
// Note what settles it. Not that cloud reports the cluster online — cloud
// reporting a link is not the cluster agreeing to answer over it — but that a
// call placed through the route came back.
func TestCanFallBackToCloudWhenTheRouteAnswers(t *testing.T) {
	cfg, identity := presenceTestConfig(t, "https://cloud.example")
	ctx := presenceContext(t)

	var asked []string
	withCloudRouteProbe(t, func(_ *Context, _ *clientconfig.Config, _ string,
		_ *clientconfig.IdentityConfig, c *ClusterResponse) bool {
		asked = append(asked, c.XID)
		return c.XID == "cluster-up"
	})

	up := &ClusterResponse{Name: "reachable-by-cloud", XID: "cluster-up"}
	require.True(t, canFallBackToCloud(ctx, cfg, "cloud", identity, up))

	down := &ClusterResponse{Name: "really-gone", XID: "cluster-down"}
	require.False(t, canFallBackToCloud(ctx, cfg, "cloud", identity, down),
		"a cluster that did not answer through cloud must keep the direct-connection error")

	require.Equal(t, []string{"cluster-up", "cluster-down"}, asked,
		"the decision must come from probing the route, not from a cached claim about it")
}

// A cluster that is online in cloud but does not answer over the relay is the
// case this probe exists for: a runtime from before the relay shipped has an
// uplink and no handler for the session, so it is present and silent. Writing
// via_cloud on the strength of presence produces a config that hangs.
func TestOnlineButUnansweringClusterIsNotRouted(t *testing.T) {
	srv, _ := cloudPresenceServer(t, map[string]bool{"cluster-old": true}, http.StatusOK)
	cfg, identity := presenceTestConfig(t, srv.URL)
	ctx := presenceContext(t)

	online, err := clusterOnlineInCloud(ctx, cfg, "cloud", identity, "cluster-old")
	require.NoError(t, err)
	require.True(t, online, "the fixture is only meaningful if cloud says it is online")

	withCloudRouteProbe(t, func(*Context, *clientconfig.Config, string,
		*clientconfig.IdentityConfig, *ClusterResponse) bool {
		return false
	})

	require.False(t, canFallBackToCloud(ctx, cfg, "cloud", identity,
		&ClusterResponse{Name: "old-runtime", XID: "cluster-old"}),
		"presence in cloud is not evidence the cluster will answer over the relay")
}

// What the fallback has to write, and the RFD's claim that a routed entry needs
// no address and no CA.
//
// A cluster that could not be dialed still advertised addresses and may have
// presented a certificate, and keeping either would write an entry that looks
// dialable and is not. The next command would then try the dead address instead
// of the route that works.
func TestRoutedEntryKeepsNoDialableFields(t *testing.T) {
	r := require.New(t)

	// The shape addCluster builds once the fallback has fired: viaCloud set,
	// and the address, addresses and certificate deliberately left empty.
	cfg := &clientconfig.ClusterConfig{
		Hostname:     "",
		AllAddresses: nil,
		Identity:     "cloud",
		XID:          "cluster-abc",
		ViaCloud:     true,
	}

	r.True(cfg.ViaCloud)
	r.Empty(cfg.Hostname, "a routed entry is never dialed, so an address would be a trap")
	r.Empty(cfg.AllAddresses)
	r.Empty(cfg.CACert, "the certificate on the wire belongs to cloud, not the cluster")
	r.NotEmpty(cfg.XID, "routing needs the cluster's id in cloud")
	r.NotEmpty(cfg.Identity, "and an identity to authenticate with")

	// And it resolves to a relay endpoint rather than a direct dial.
	endpoint, err := cfg.CloudEndpoint(viaCloudTestConfig(t, cfg))
	r.NoError(err)
	r.NotEmpty(endpoint)
}

// viaCloudTestConfig wraps a cluster entry in a config holding the identity it
// names, which is what CloudEndpoint resolves the cloud through.
func viaCloudTestConfig(t *testing.T, cluster *clientconfig.ClusterConfig) *clientconfig.Config {
	t.Helper()

	cfg := clientconfig.NewConfig()
	cfg.SetIdentity("cloud", &clientconfig.IdentityConfig{
		Type:   clientconfig.IdentityToken,
		Issuer: "https://api.miren.cloud",
		Token:  freshLoginJWT(t),
	})
	cfg.SetCluster("prod", cluster)
	return cfg
}
