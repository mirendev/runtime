//go:build linux

package server

import (
	"context"
	"path/filepath"
	"testing"

	"miren.dev/runtime/pkg/registration"
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

	boot := newRegistrationBoot(registrationBootInputs{log: testLogger(), dataPath: dataPath})
	if err := boot.start(context.Background(), nil); err != nil {
		t.Fatalf("start() error = %v", err)
	}
	output := boot.output()
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
