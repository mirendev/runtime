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

// runClusterAdd drives the command the way the CLI wires it, with the context's
// output going to the same place PrintJSON writes, so a stray progress line in
// front of the document fails the test instead of the caller's parser.
//
// Returns the decoded document, raw stdout, and the exit code the CLI would
// exit with.
func runClusterAdd(t *testing.T, opts clusterAddOpts) (clusterAddResult, string, int) {
	t.Helper()

	stdout := os.Stdout
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = w
	defer func() { os.Stdout = stdout }()

	ctx := &Context{Context: context.Background(), Stdout: w, Stderr: &bytes.Buffer{}}

	require.NoError(t, ClusterAdd(ctx, opts), "a JSON run reports failure in the document, not as a returned error")

	require.NoError(t, w.Close())
	printed := &bytes.Buffer{}
	_, err = printed.ReadFrom(r)
	require.NoError(t, err)

	var result clusterAddResult
	require.NoError(t, json.Unmarshal(printed.Bytes(), &result),
		"stdout has to be the result document and nothing else:\n%s", printed.String())

	return result, printed.String(), ctx.exitCode
}

func jsonAddOpts(cluster string) clusterAddOpts {
	return clusterAddOpts{
		FormatOptions: FormatOptions{Format: "json"},
		Cluster:       cluster,
	}
}

// What a successful add tells a caller: the local name to use from here on, and
// how the cluster will be reached.
func TestClusterAddJSONSuccess(t *testing.T) {
	r := require.New(t)

	srv := fakeCloud(t, []ClusterResponse{
		{Name: "prod", XID: "cluster-prod", OrganizationName: "Acme"},
	}, nil)
	configWithIdentity(t, srv.URL)

	opts := jsonAddOpts("prod")
	opts.As = "acme-prod"
	opts.ViaCloud = true

	result, printed, exitCode := runClusterAdd(t, opts)

	r.True(result.OK)
	r.Zero(exitCode)
	r.Nil(result.Error)
	r.NotContains(printed, "Successfully added", "progress belongs on stderr")

	added := result.Cluster
	r.NotNil(added)
	r.Equal("acme-prod", added.Name, "the local name is what every other command takes")
	r.Equal("prod", added.CloudName)
	r.Equal("cluster-prod", added.XID)
	r.Equal("Acme", added.Organization)
	r.True(added.ViaCloud)
	r.Empty(added.Address, "a routed cluster is dialed at no address of its own")
	r.Equal("cloud", added.Identity)
	r.True(added.Insecure, "the test cloud is http, and the entry says so")
	r.True(added.Active, "the first cluster added becomes the active one")
	r.Contains(added.ConfigFile, "acme-prod.yaml")
}

// A failure has to be legible twice over: as a document a caller can branch on,
// and as a non-zero exit status for one that only checks that.
func TestClusterAddJSONReportsFailures(t *testing.T) {
	tests := []struct {
		name     string
		opts     func() clusterAddOpts
		wantCode string
		wantIn   string
	}{
		{
			name:     "flags that cannot mean anything",
			opts:     func() clusterAddOpts { o := jsonAddOpts(""); o.As = "x"; return o },
			wantCode: codeInvalidFlags,
			wantIn:   "--as needs --cluster",
		},
		{
			// The one that matters most for an agent: no list to pick from, so
			// say what to run instead.
			name:     "picking from a list needs a person",
			opts:     func() clusterAddOpts { return jsonAddOpts("") },
			wantCode: codeInteractiveRequired,
			wantIn:   "miren cluster available",
		},
		{
			name:     "no such cluster",
			opts:     func() clusterAddOpts { return jsonAddOpts("produciton") },
			wantCode: codeClusterNotFound,
			wantIn:   `no cluster named "produciton"`,
		},
		{
			name:     "ambiguous name",
			opts:     func() clusterAddOpts { return jsonAddOpts("shared") },
			wantCode: codeAmbiguousCluster,
			wantIn:   "--organization",
		},
		{
			name:     "unknown organization",
			opts:     func() clusterAddOpts { o := jsonAddOpts("prod"); o.Organization = "Initech"; return o },
			wantCode: codeUnknownOrganization,
			wantIn:   `no clusters in organization "Initech"`,
		},
		{
			// Exists, cannot be reached, and worth retrying later — which is
			// why it has a code of its own.
			name:     "cluster nothing can reach",
			opts:     func() clusterAddOpts { return jsonAddOpts("behind-nat") },
			wantCode: codeClusterUnreachable,
			wantIn:   unreachableAddressNote,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			r := require.New(t)

			srv := fakeCloud(t, []ClusterResponse{
				{Name: "prod", XID: "cluster-prod", OrganizationName: "Acme"},
				{Name: "behind-nat", XID: "cluster-nat", OrganizationName: "Acme"},
				{Name: "shared", XID: "cluster-a", OrganizationName: "Acme"},
				{Name: "shared", XID: "cluster-g", OrganizationName: "Globex"},
			}, nil)
			configWithIdentity(t, srv.URL)

			result, _, exitCode := runClusterAdd(t, test.opts())

			r.False(result.OK)
			r.Nil(result.Cluster)
			r.Equal(1, exitCode, "a caller checking only the exit status has to see the failure too")

			r.NotNil(result.Error)
			r.Equal(test.wantCode, result.Error.Code)
			r.Contains(result.Error.Message, test.wantIn)
		})
	}
}

// Overwriting an existing entry is a decision to hand back, not one to make
// because nobody was watching.
func TestClusterAddJSONRefusesToOverwrite(t *testing.T) {
	r := require.New(t)

	srv := fakeCloud(t, []ClusterResponse{
		{Name: "prod", XID: "cluster-prod", OrganizationName: "Acme"},
	}, nil)
	configWithIdentity(t, srv.URL)

	cfg, err := clientconfig.LoadConfig()
	r.NoError(err)
	cfg.SetCluster("prod", &clientconfig.ClusterConfig{Hostname: "10.0.0.9:8443", Identity: "cloud"})
	r.NoError(cfg.Save())

	opts := jsonAddOpts("prod")
	opts.ViaCloud = true

	result, _, exitCode := runClusterAdd(t, opts)

	r.False(result.OK)
	r.Equal(1, exitCode)
	r.Equal(codeClusterExists, result.Error.Code)
	r.Contains(result.Error.Message, "--force")

	// And the existing entry is untouched.
	cfg, err = clientconfig.LoadConfig()
	r.NoError(err)
	cluster, err := cfg.GetCluster("prod")
	r.NoError(err)
	r.Equal("10.0.0.9:8443", cluster.Hostname)
	r.False(cluster.ViaCloud)

	// --force is how a caller says to go ahead.
	opts.Force = true
	result, _, exitCode = runClusterAdd(t, opts)
	r.True(result.OK)
	r.Zero(exitCode)
	r.True(result.Cluster.ViaCloud)
}

// An error that was never given a code still has to report one, or a caller
// switching on it falls through silently.
func TestErrorCodeFallsBackToUnknown(t *testing.T) {
	require.Equal(t, codeUnknown, errorCode(context.Canceled))
	require.Equal(t, codeInvalidFlags, errorCode(codedErrorf(codeInvalidFlags, "nope")))
}

// The flag checks run before anything is loaded, so a bad combination is
// reported as itself. This used to be checked after identity resolution, where
// an account with several identities got told to pick one instead.
func TestClusterAddRejectsAddressWithoutClusterBeforeIdentities(t *testing.T) {
	r := require.New(t)

	srv := fakeCloud(t, []ClusterResponse{
		{Name: "prod", XID: "cluster-prod", OrganizationName: "Acme"},
	}, nil)
	configWithIdentity(t, srv.URL)

	cfg, err := clientconfig.LoadConfig()
	r.NoError(err)
	cfg.SetIdentity("second", &clientconfig.IdentityConfig{
		Type:   clientconfig.IdentityToken,
		Issuer: srv.URL,
		Token:  freshLoginJWT(t),
	})
	r.NoError(cfg.Save())

	opts := clusterAddOpts{FormatOptions: FormatOptions{Format: "json"}, Address: "10.0.0.1:8443"}
	result, _, exitCode := runClusterAdd(t, opts)

	r.False(result.OK)
	r.Equal(1, exitCode)
	r.Equal(codeInvalidFlags, result.Error.Code)
	r.Contains(result.Error.Message, "--address needs --cluster")
}
