package commands

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"testing"

	"github.com/stretchr/testify/require"

	"miren.dev/runtime/clientconfig"
)

// availableClusterFixtures covers the three states a listing has to tell apart:
// a cluster with an address, one with none, and one already added locally.
func availableClusterFixtures() []ClusterResponse {
	return []ClusterResponse{
		{
			Name: "prod", XID: "cluster-prod", OrganizationName: "Acme", OrganizationXID: "org-acme",
			APIAddresses: []string{"10.0.0.1:8443", "127.0.0.1:8443"}, CACertFingerprint: "abc123",
		},
		{Name: "behind-nat", XID: "cluster-nat", OrganizationName: "Acme", OrganizationXID: "org-acme"},
		{Name: "shared", XID: "cluster-shared", OrganizationName: "Globex", OrganizationXID: "org-globex"},
	}
}

// runClusterAvailable drives the command the way the CLI wires it, with the
// context's own output going to the same place PrintJSON writes. That wiring is
// the point: with a separate buffer for the context, a progress line printed in
// front of the JSON document goes unnoticed here and breaks every caller.
//
// Returns the decoded document (JSON mode only), everything that reached
// stdout, and everything that reached stderr.
func runClusterAvailable(t *testing.T, opts clusterAvailableOpts) ([]availableCluster, string, string, error) {
	t.Helper()

	stdout := os.Stdout
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = w
	defer func() { os.Stdout = stdout }()

	stderr := &bytes.Buffer{}
	ctx := &Context{Context: context.Background(), Stdout: w, Stderr: stderr}

	cmdErr := ClusterAvailable(ctx, opts)

	require.NoError(t, w.Close())
	printed := &bytes.Buffer{}
	_, err = printed.ReadFrom(r)
	require.NoError(t, err)

	if cmdErr != nil || !opts.IsJSON() {
		return nil, printed.String(), stderr.String(), cmdErr
	}

	var clusters []availableCluster
	require.NoError(t, json.Unmarshal(printed.Bytes(), &clusters),
		"stdout has to be the JSON document and nothing else:\n%s", printed.String())

	return clusters, printed.String(), stderr.String(), nil
}

// The JSON a script reads: every cluster cloud knows about, sorted, with enough
// to hand a name straight to `miren cluster add --cluster`.
func TestClusterAvailableJSON(t *testing.T) {
	r := require.New(t)

	srv := fakeCloud(t, availableClusterFixtures(), nil)
	configWithIdentity(t, srv.URL)

	clusters, printed, stderr, err := runClusterAvailable(t, clusterAvailableOpts{
		FormatOptions: FormatOptions{Format: "json"},
	})
	r.NoError(err)
	r.Len(clusters, 3)

	// Adopting the lone identity says so, and that line has to land on stderr.
	// On stdout it would sit in front of the array and break every parser.
	r.Contains(stderr, "Using identity")
	r.NotContains(printed, "Using identity")

	// Sorted by organization, then name, so two runs can be compared.
	r.Equal([]string{"behind-nat", "prod", "shared"}, []string{clusters[0].Name, clusters[1].Name, clusters[2].Name})

	prod := clusters[1]
	r.Equal("cluster-prod", prod.XID)
	r.Equal("Acme", prod.Organization)
	r.Equal("org-acme", prod.OrganizationXID)
	r.Equal([]string{"10.0.0.1:8443", "127.0.0.1:8443"}, prod.APIAddresses)
	r.Equal("abc123", prod.CACertFingerprint)
	r.True(prod.Reachable)
	r.False(prod.Added)

	// Nobody asked cloud anything, so no claim is made either way.
	r.Nil(clusters[0].ViaCloud, "without --check, an address-less cluster's route is unknown, not false")
	r.False(clusters[0].Reachable)
}

// --check is what turns "advertises no address" into a usable answer, and the
// two possible answers have to be distinguishable in the output.
func TestClusterAvailableCheckReportsCloudRoutes(t *testing.T) {
	r := require.New(t)

	srv := fakeCloud(t, availableClusterFixtures(), map[string]bool{"cluster-nat": true})
	configWithIdentity(t, srv.URL)

	clusters, _, _, err := runClusterAvailable(t, clusterAvailableOpts{
		FormatOptions: FormatOptions{Format: "json"},
		Check:         true,
	})
	r.NoError(err)

	byName := map[string]availableCluster{}
	for _, cluster := range clusters {
		byName[cluster.Name] = cluster
	}

	r.NotNil(byName["behind-nat"].ViaCloud)
	r.True(*byName["behind-nat"].ViaCloud, "cloud holds a link to it, so it can be added")

	r.NotNil(byName["shared"].ViaCloud)
	r.False(*byName["shared"].ViaCloud, "cloud was asked and said no")

	r.Nil(byName["prod"].ViaCloud, "a cluster with an address is never asked about")
}

// Already-added clusters are matched by their id in cloud, not their name, so
// the answer survives being added under a different local name.
func TestClusterAvailableMarksAddedClusters(t *testing.T) {
	r := require.New(t)

	srv := fakeCloud(t, availableClusterFixtures(), nil)
	configWithIdentity(t, srv.URL)

	cfg, err := clientconfig.LoadConfig()
	r.NoError(err)
	cfg.SetCluster("acme-prod", &clientconfig.ClusterConfig{
		Hostname: "10.0.0.1:8443",
		Identity: "cloud",
		XID:      "cluster-prod",
	})
	// One added by address, which carries no id and so cannot be matched.
	cfg.SetCluster("shared", &clientconfig.ClusterConfig{
		Hostname: "192.168.1.5:8443",
		Identity: "cloud",
	})
	r.NoError(cfg.Save())

	clusters, _, _, err := runClusterAvailable(t, clusterAvailableOpts{
		FormatOptions: FormatOptions{Format: "json"},
	})
	r.NoError(err)

	byName := map[string]availableCluster{}
	for _, cluster := range clusters {
		byName[cluster.Name] = cluster
	}

	r.True(byName["prod"].Added)
	r.Equal("acme-prod", byName["prod"].LocalName, "the local name is what you would pass to other commands")

	r.False(byName["shared"].Added, "a local entry with no cluster id cannot be claimed as this cluster")
	r.Empty(byName["shared"].LocalName)
}

func TestClusterAvailableFiltersByOrganization(t *testing.T) {
	r := require.New(t)

	srv := fakeCloud(t, availableClusterFixtures(), nil)
	configWithIdentity(t, srv.URL)

	clusters, _, _, err := runClusterAvailable(t, clusterAvailableOpts{
		FormatOptions: FormatOptions{Format: "json"},
		Organization:  "globex",
	})
	r.NoError(err)
	r.Len(clusters, 1)
	r.Equal("shared", clusters[0].Name)

	// A misspelled organization is a mistake worth reporting, not an empty
	// list that reads as "you have no clusters there".
	_, _, _, err = runClusterAvailable(t, clusterAvailableOpts{
		FormatOptions: FormatOptions{Format: "json"},
		Organization:  "Initech",
	})
	r.Error(err)
	r.Contains(err.Error(), `no clusters in organization "Initech"`)
	r.Contains(err.Error(), "Acme, Globex")
}

// The table has to say which clusters are unresolved and how to resolve them,
// or somebody reads "no direct address" as a verdict.
func TestClusterAvailableTableExplainsUncheckedClusters(t *testing.T) {
	r := require.New(t)

	srv := fakeCloud(t, availableClusterFixtures(), nil)
	configWithIdentity(t, srv.URL)

	_, printed, _, err := runClusterAvailable(t, clusterAvailableOpts{})
	r.NoError(err)

	r.Contains(printed, "prod")
	r.Contains(printed, noDirectAddressNote)
	r.Contains(printed, "--check")
	r.Contains(printed, "miren cluster add --cluster")
}

// Nothing can be asked of cloud without an identity, and the error has to say
// how to get one.
func TestClusterAvailableNeedsAnIdentity(t *testing.T) {
	t.Setenv(clientconfig.EnvConfigPath, t.TempDir())

	_, _, _, err := runClusterAvailable(t, clusterAvailableOpts{})
	require.Error(t, err)
	require.Contains(t, err.Error(), "miren login")
}
