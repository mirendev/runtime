package commands

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"miren.dev/runtime/clientconfig"
)

// fakeCloud serves the two endpoints `miren cluster add` asks about in
// discovery mode: the list of clusters on the account, and whether cloud
// currently holds a link to a given one.
func fakeCloud(t *testing.T, clusters []ClusterResponse, online map[string]bool) *httptest.Server {
	t.Helper()

	mux := http.NewServeMux()

	mux.HandleFunc("/api/v1/users/clusters", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"clusters": clusters})
	})

	mux.HandleFunc("/api/v1/clusters/{xid}/online", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"online": online[r.PathValue("xid")]})
	})

	srv := httptest.NewServer(mux)
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

	err := addCluster(presenceContext(t), addClusterOptions{
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

	err := addCluster(presenceContext(t), addClusterOptions{clusterName: "prod"})
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

	err := addCluster(presenceContext(t), addClusterOptions{clusterName: "produciton"})
	r.Error(err)
	r.Contains(err.Error(), `no cluster named "produciton"`)
	r.Contains(err.Error(), "prod (Acme)")
}
