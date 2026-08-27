//go:build linux

package server

import (
	"context"
	"testing"

	"miren.dev/runtime/components/coordinate"
	"miren.dev/runtime/pkg/registration"
)

func TestWorkloadIdentityBootUsesRegisteredCloudAnchor(t *testing.T) {
	registrationBoot := newRegistrationBoot(registrationBootInputs{log: testLogger(), dataPath: t.TempDir()})
	registrationBoot.result = registrationBootOutput{
		anchor: registration.AnchorCloud,
		cloudAuth: coordinate.CloudAuthConfig{
			ClusterID:         "cluster-1",
			IdentityIssuerURL: "https://identity.example",
			Tags:              map[string]string{"organization_id": "org-1"},
		},
	}
	boot := newWorkloadIdentityBoot(workloadIdentityBootInputs{
		log:      testLogger(),
		dataPath: t.TempDir(),
	}, registrationBoot)

	if err := boot.start(context.Background(), nil); err != nil {
		t.Fatalf("start() error = %v", err)
	}
	if boot.output().issuer == nil {
		t.Fatal("issuer is nil")
	}
	if got := boot.output().issuer.IssuerURL(); got != "https://identity.example" {
		t.Fatalf("IssuerURL() = %q, want %q", got, "https://identity.example")
	}
}
