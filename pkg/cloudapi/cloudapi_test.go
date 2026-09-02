package cloudapi_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"miren.dev/runtime/pkg/cloudapi"
	"miren.dev/runtime/pkg/cloudapi/cloudapitest"
)

func staticToken(token string) cloudapi.TokenFunc {
	return func(context.Context) (string, error) { return token, nil }
}

func TestListClusters(t *testing.T) {
	r := require.New(t)

	srv := cloudapitest.NewServer([]cloudapi.Cluster{
		{Name: "prod", XID: "cluster-prod", OrganizationName: "Acme", APIAddresses: []string{"10.0.0.1:8443"}},
		{Name: "behind-nat", XID: "cluster-nat", OrganizationName: "Acme"},
	}, nil, http.StatusOK)
	defer srv.Close()

	client, err := cloudapi.New(srv.URL, staticToken("t"))
	r.NoError(err)

	clusters, err := client.ListClusters(context.Background())
	r.NoError(err)
	r.Len(clusters, 2)
	r.Equal("cluster-prod", clusters[0].XID)
	r.True(clusters[0].HasReachableAddress())
	r.False(clusters[1].HasReachableAddress(), "a cluster advertising no address cannot be dialed")
}

func TestClusterOnline(t *testing.T) {
	r := require.New(t)

	srv := cloudapitest.NewServer(nil, map[string]bool{"cluster-up": true}, http.StatusOK)
	defer srv.Close()

	client, err := cloudapi.New(srv.URL, staticToken("t"))
	r.NoError(err)

	online, err := client.ClusterOnline(context.Background(), "cluster-up")
	r.NoError(err)
	r.True(online)

	online, err = client.ClusterOnline(context.Background(), "cluster-down")
	r.NoError(err)
	r.False(online)

	r.Equal([]string{"cluster-up", "cluster-down"}, srv.Asked())
}

// A cluster with no id can't be asked about. Reporting it as offline rather
// than as an error keeps callers from having to special-case it, since both
// answers lead to the same decision.
func TestClusterOnlineWithoutAnID(t *testing.T) {
	srv := cloudapitest.NewServer(nil, nil, http.StatusOK)
	defer srv.Close()

	client, err := cloudapi.New(srv.URL, staticToken("t"))
	require.NoError(t, err)

	online, err := client.ClusterOnline(context.Background(), "")
	require.NoError(t, err)
	require.False(t, online)
	require.Empty(t, srv.Asked(), "no request is made for a cluster with no id")
}

// A cloud that refuses the question has not answered it, and the caller has to
// be able to tell those apart.
func TestRefusalIsAnError(t *testing.T) {
	srv := cloudapitest.NewServer(nil, nil, http.StatusForbidden)
	defer srv.Close()

	client, err := cloudapi.New(srv.URL, staticToken("t"))
	require.NoError(t, err)

	_, err = client.ClusterOnline(context.Background(), "cluster-1")
	require.Error(t, err)
	require.Contains(t, err.Error(), "403")
}

func TestSendsTheBearerToken(t *testing.T) {
	var seen string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = r.Header.Get("Authorization")
		_, _ = w.Write([]byte(`{"clusters":[]}`))
	}))
	defer srv.Close()

	client, err := cloudapi.New(srv.URL, staticToken("secret-token"))
	require.NoError(t, err)

	_, err = client.ListClusters(context.Background())
	require.NoError(t, err)
	require.Equal(t, "Bearer secret-token", seen)
}

// The token is fetched per request rather than held, so a refreshed one is
// picked up without rebuilding the client.
func TestTokenIsFetchedPerRequest(t *testing.T) {
	calls := 0

	srv := cloudapitest.NewServer(nil, nil, http.StatusOK)
	defer srv.Close()

	client, err := cloudapi.New(srv.URL, func(context.Context) (string, error) {
		calls++
		return "t", nil
	})
	require.NoError(t, err)

	_, err = client.ListClusters(context.Background())
	require.NoError(t, err)
	_, err = client.ListClusters(context.Background())
	require.NoError(t, err)

	require.Equal(t, 2, calls)
}

func TestNewRejectsAnUnusableClient(t *testing.T) {
	_, err := cloudapi.New("", staticToken("t"))
	require.Error(t, err, "a client with no cloud to talk to is not usable")

	_, err = cloudapi.New("https://cloud.example", nil)
	require.Error(t, err, "a client with no way to authenticate is not usable")
}
