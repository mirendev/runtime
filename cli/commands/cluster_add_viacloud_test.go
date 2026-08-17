package commands

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"miren.dev/runtime/clientconfig"
)

// cloudPresenceServer stands in for cloud's per-cluster presence endpoint,
// recording which clusters were asked about.
func cloudPresenceServer(t *testing.T, online map[string]bool, status int) (*httptest.Server, *[]string) {
	t.Helper()

	var asked []string

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
		asked = append(asked, xid)

		w.Header().Set("Content-Type", "application/json")
		if online[xid] {
			_, _ = w.Write([]byte(`{"cluster_xid":"` + xid + `","online":true}`))
			return
		}
		_, _ = w.Write([]byte(`{"cluster_xid":"` + xid + `","online":false}`))
	}))
	t.Cleanup(srv.Close)

	return srv, &asked
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

	r.Equal([]string{"cluster-up", "cluster-down"}, *asked)
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

	r.Equal([]string{"cluster-nat", "cluster-gone"}, *asked,
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
