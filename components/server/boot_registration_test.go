//go:build linux

package server

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"miren.dev/runtime/components/coordinate"
	"miren.dev/runtime/pkg/boot"
	"miren.dev/runtime/pkg/clusterstate"
	"miren.dev/runtime/pkg/etcdtest"
	"miren.dev/runtime/pkg/registration"
	"miren.dev/runtime/pkg/serverconfig"
	"miren.dev/runtime/pkg/workloadidentity"
)

func TestRegistrationBootPublishesApprovedRegistration(t *testing.T) {
	dataPath := t.TempDir()
	stored := &registration.StoredRegistration{
		Status:            "approved",
		ClusterID:         "cluster-1",
		ClusterName:       "test-cluster",
		OrganizationID:    "org-1",
		CloudURL:          "https://cloud.example",
		DNSHostname:       "cluster.example",
		IdentityIssuerURL: "https://identity.example",
		IdentityAnchor:    registration.AnchorCloud,
		PrivateKey:        "private-key",
		Tags:              map[string]string{"existing": "tag"},
	}
	if err := registration.SaveRegistration(filepath.Join(dataPath, "server"), stored); err != nil {
		t.Fatalf("SaveRegistration() error = %v", err)
	}

	b := &registrationBoot{inputs: registrationBootInputs{log: testLogger(), dataPath: dataPath}}
	output, err := b.start(context.Background())
	if err != nil {
		t.Fatalf("start() error = %v", err)
	}
	if !output.cloudAuth.Enabled || output.cloudAuth.ClusterID != stored.ClusterID {
		t.Fatalf("cloud auth = %#v", output.cloudAuth)
	}
	if got := output.cloudAuth.Tags["organization_id"]; got != stored.OrganizationID {
		t.Fatalf("organization tag = %q, want %q", got, stored.OrganizationID)
	}
	if output.anchor != registration.AnchorCloud {
		t.Fatalf("anchor = %q, want %q", output.anchor, registration.AnchorCloud)
	}
}

func TestRegistrationBootRestoresBeforeIdentityAndCloudConsumers(t *testing.T) {
	client, prefix := etcdtest.TestEtcdClient(t)
	dir := t.TempDir()
	reg := &registration.StoredRegistration{Status: "approved", ClusterID: "restored-cluster", ClusterName: "original", OrganizationID: "original-org", IdentityAnchor: registration.AnchorCloud, IdentityIssuerURL: "https://identity.example", PrivateKey: "test-key"}
	require.NoError(t, registration.SaveRegistration(filepath.Join(dir, "server"), reg))
	issuer, err := workloadidentity.NewIssuer(workloadidentity.IssuerConfig{DataPath: dir, IssuerURL: reg.IdentityIssuerURL, ClusterID: reg.ClusterID, OrganizationID: reg.OrganizationID})
	require.NoError(t, err)
	token, err := issuer.IssueToken("app", "sandbox")
	require.NoError(t, err)
	foundation := coordinate.NewFoundation(testLogger(), coordinate.CoordinatorConfig{DataPath: dir, Prefix: prefix, EtcdEndpoints: client.Endpoints()})
	require.NoError(t, foundation.PrepareMetadata(t.Context()))
	sealingKey, err := os.ReadFile(clusterstate.SealingKeyPath(dir))
	require.NoError(t, err)

	for _, interrupted := range []bool{false, true} {
		t.Run(map[bool]string{false: "empty-directory", true: "interrupted-hydration"}[interrupted], func(t *testing.T) {
			restoredDir := t.TempDir()
			require.NoError(t, os.MkdirAll(filepath.Join(restoredDir, "server"), 0700))
			require.NoError(t, os.WriteFile(clusterstate.SealingKeyPath(restoredDir), sealingKey, 0600))
			if interrupted {
				restoredDir = dir
				require.NoError(t, os.WriteFile(filepath.Join(dir, "server", "workload-identity.etcd-hydrating"), nil, 0600))
				require.NoError(t, os.WriteFile(filepath.Join(dir, "server", "workload-identity.key"), []byte("truncated"), 0600))
			}
			// Stand in for embedded etcd with the shared test server, running
			// the same metadata preparation startEmbedded does.
			etcd := &etcdBoot{inputs: etcdBootInputs{log: testLogger(), dataPath: restoredDir, config: serverconfig.EtcdConfig{Prefix: &prefix}}}
			etcdComponent, etcdOutput := boot.Provide0("etcd", func(ctx context.Context) (etcdBootOutput, error) {
				output := etcdBootOutput{endpoints: client.Endpoints()}
				return output, etcd.prepareMetadata(ctx, testLogger(), output)
			})
			registered := newRegistrationBoot(registrationBootInputs{log: testLogger(), dataPath: restoredDir}, etcdOutput)
			identity := newWorkloadIdentityBoot(workloadIdentityBootInputs{log: testLogger(), dataPath: restoredDir}, registered.output)
			graph := boot.NewGraph()
			for _, component := range []*boot.Component{etcdComponent, registered.component, identity.component} {
				require.NoError(t, graph.Add(component))
			}
			require.NoError(t, graph.Start(t.Context()))
			cloud := registered.output.Value().cloudAuth
			require.True(t, cloud.Enabled)
			require.Equal(t, reg.ClusterID, cloud.ClusterID)
			require.Equal(t, reg.OrganizationID, cloud.Tags["organization_id"])
			require.Equal(t, reg.IdentityIssuerURL, identity.output.Value().issuer.IssuerURL())
			_, err := identity.output.Value().issuer.VerifyToken(token, workloadidentity.DefaultAudience)
			require.NoError(t, err)
			require.NoError(t, graph.Stop(t.Context()))
		})
	}
}
