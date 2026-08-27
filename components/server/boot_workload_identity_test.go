//go:build linux

package server

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"miren.dev/runtime/components/coordinate"
	"miren.dev/runtime/pkg/boot"
	"miren.dev/runtime/pkg/registration"
)

func TestWorkloadIdentityBootUsesRegisteredCloudAnchor(t *testing.T) {
	registrationOutput := registrationBootOutput{
		anchor: registration.AnchorCloud,
		cloudAuth: coordinate.CloudAuthConfig{
			ClusterID:         "cluster-1",
			IdentityIssuerURL: "https://identity.example",
			Tags:              map[string]string{"organization_id": "org-1"},
		},
	}
	b := &workloadIdentityBoot{inputs: workloadIdentityBootInputs{
		log:      testLogger(),
		dataPath: t.TempDir(),
	}}

	output, err := b.start(context.Background(), registrationOutput)
	if err != nil {
		t.Fatalf("start() error = %v", err)
	}
	if output.issuer == nil {
		t.Fatal("issuer is nil")
	}
	if got := output.issuer.IssuerURL(); got != "https://identity.example" {
		t.Fatalf("IssuerURL() = %q, want %q", got, "https://identity.example")
	}
}

func TestWorkloadIdentityFailureDoesNotPublishOrStartConsumers(t *testing.T) {
	dataPath := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(dataPath, []byte("blocked"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	graph := boot.NewGraph()
	registrationComponent, registrationOutput := boot.Provide0("registration", func(context.Context) (registrationBootOutput, error) {
		return registrationBootOutput{}, nil
	})
	identity := newWorkloadIdentityBoot(workloadIdentityBootInputs{
		log:      testLogger(),
		dataPath: dataPath,
	}, registrationOutput)
	consumerStarted := false
	consumer := boot.Run1("consumer", identity.output, func(context.Context, workloadIdentityBootOutput) error {
		consumerStarted = true
		return nil
	})
	for _, component := range []*boot.Component{registrationComponent, identity.component, consumer} {
		if err := graph.Add(component); err != nil {
			t.Fatalf("Add() error = %v", err)
		}
	}

	err := graph.Start(t.Context())
	if err == nil || !strings.Contains(err.Error(), "initializing workload identity issuer") {
		t.Fatalf("Start() error = %v, want issuer initialization error", err)
	}
	if consumerStarted {
		t.Fatal("consumer started after workload identity failed")
	}
	func() {
		defer func() {
			if recover() == nil {
				t.Error("workload identity output published after startup failure")
			}
		}()
		identity.output.Value()
	}()
}
