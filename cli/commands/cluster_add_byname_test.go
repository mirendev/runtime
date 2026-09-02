package commands

import (
	"bytes"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

	"miren.dev/runtime/clientconfig"
	"miren.dev/runtime/pkg/cloudapi/cloudapitest"
)

// fakeCloud stands up a cloud holding the given clusters, reporting online for
// the ones set to true.
func fakeCloud(t *testing.T, clusters []ClusterResponse, online map[string]bool) *cloudapitest.Server {
	t.Helper()

	srv := cloudapitest.NewServer(clusters, online, http.StatusOK)
	t.Cleanup(srv.Close)

	return srv
}

// configWithIdentity points the client config at a scratch directory holding a
// single identity, which is the state someone is in right after `miren login`.
func configWithIdentity(t *testing.T, issuer string) {
	t.Helper()

	t.Setenv(clientconfig.EnvConfigPath, t.TempDir())

	cfg := clientconfig.NewConfig()
	cfg.SetIdentity("cloud", &clientconfig.IdentityConfig{
		Type:   clientconfig.IdentityToken,
		Issuer: issuer,
		Token:  freshLoginJWT(t),
	})
	require.NoError(t, cfg.Save())
}

// The case this feature exists for: the name is already known, so no picker is
// involved, and the entry still comes out shaped by the same routing decision
// the picker path would have made.
func TestAddClusterByNameRoutedThroughCloud(t *testing.T) {
	r := require.New(t)

	srv := fakeCloud(t, []ClusterResponse{
		{Name: "prod", XID: "cluster-prod", OrganizationName: "Acme"},
	}, nil)
	configWithIdentity(t, srv.URL)

	_, err := addCluster(presenceContext(t), addClusterOptions{
		clusterName: "prod",
		localName:   "acme-prod",
		viaCloud:    true,
	})
	r.NoError(err)

	cfg, err := clientconfig.LoadConfig()
	r.NoError(err)

	cluster, err := cfg.GetCluster("acme-prod")
	r.NoError(err, "--as decides the local name, and nothing prompted for one")
	r.True(cluster.ViaCloud)
	r.Equal("cluster-prod", cluster.XID)
	r.Empty(cluster.Hostname, "a routed entry is never dialed, so an address would be a trap")
	r.Empty(cluster.CACert)
	r.True(cluster.Insecure, "the test cloud is http, so the entry has to admit it sends credentials in the clear")

	_, err = cfg.GetCluster("prod")
	r.Error(err, "the cloud name is not also written")
}

// A cluster nothing can dial and cloud cannot reach is a dead end. The picker
// greys it out; named, it has to be refused outright — falling through would
// probe localhost and could pin whatever is listening there under a remote
// cluster's name.
func TestAddClusterByNameRefusesAnUnreachableCluster(t *testing.T) {
	r := require.New(t)

	srv := fakeCloud(t, []ClusterResponse{
		{Name: "prod", XID: "cluster-prod", OrganizationName: "Acme"},
	}, map[string]bool{"cluster-prod": false})
	configWithIdentity(t, srv.URL)

	_, err := addCluster(presenceContext(t), addClusterOptions{clusterName: "prod"})
	r.Error(err)
	r.Contains(err.Error(), unreachableAddressNote)
	r.Contains(err.Error(), unreachableAddressHelp)

	cfg, err := clientconfig.LoadConfig()
	r.NoError(err)
	_, err = cfg.GetCluster("prod")
	r.Error(err, "nothing is written for a cluster that could not be reached")
}

// The name is matched against cloud's list, so a typo is caught before any
// connection is attempted and the error says what was actually available.
func TestAddClusterByNameReportsAnUnknownName(t *testing.T) {
	r := require.New(t)

	srv := fakeCloud(t, []ClusterResponse{
		{Name: "prod", XID: "cluster-prod", OrganizationName: "Acme"},
	}, nil)
	configWithIdentity(t, srv.URL)

	_, err := addCluster(presenceContext(t), addClusterOptions{clusterName: "produciton"})
	r.Error(err)
	r.Contains(err.Error(), `no cluster named "produciton"`)
	r.Contains(err.Error(), "prod (Acme)")
}

// `--via-cloud` records how a cluster is reached, not that it is up, so the
// entry is written even when the route does not answer. The warning is what
// keeps that from surfacing later as an unrelated command failing.
func TestAddClusterByNameWarnsWhenTheCloudRouteIsSilent(t *testing.T) {
	r := require.New(t)

	srv := fakeCloud(t, []ClusterResponse{
		{Name: "prod", XID: "cluster-prod", OrganizationName: "Acme"},
	}, nil)
	configWithIdentity(t, srv.URL)

	ctx := presenceContext(t)
	added, err := addCluster(ctx, addClusterOptions{clusterName: "prod", viaCloud: true})
	r.NoError(err)
	r.True(added.ViaCloud)

	said := ctx.Stdout.(*bytes.Buffer).String()
	r.Contains(said, "did not answer through cloud")
	r.Contains(said, "Adding it anyway")

	cfg, err := clientconfig.LoadConfig()
	r.NoError(err)
	cluster, err := cfg.GetCluster("prod")
	r.NoError(err, "the entry is written even though the route did not answer")
	r.True(cluster.ViaCloud)
}
