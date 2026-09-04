//go:build linux

package server

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"miren.dev/runtime/components/coordinate"
	"miren.dev/runtime/pkg/boot"
)

func TestBuildSagaRecoveryStartsAfterWorkAdmission(t *testing.T) {
	graph := boot.NewGraph()
	applicationsComponent, applicationsOutput := boot.Provide0("application-management", func(context.Context) (applicationManagementBootOutput, error) {
		return applicationManagementBootOutput{applications: coordinate.NewApplicationManagement(new(coordinate.Foundation), nil)}, nil
	})

	admissionStarted := make(chan struct{})
	releaseAdmission := make(chan struct{})
	workAdmission := boot.Run0("work-admission", func(ctx context.Context) error {
		close(admissionStarted)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-releaseAdmission:
			return nil
		}
	})
	recovery := newBuildSagaRecoveryBoot(buildSagaRecoveryBootInputs{enabled: true}, applicationsOutput, workAdmission)

	for _, component := range []*boot.Component{applicationsComponent, workAdmission, recovery.component} {
		require.NoError(t, graph.Add(component))
	}

	done := make(chan error, 1)
	go func() { done <- graph.Start(t.Context()) }()
	require.Eventually(t, func() bool {
		select {
		case <-admissionStarted:
			return true
		default:
			return false
		}
	}, time.Second, time.Millisecond)

	select {
	case err := <-done:
		t.Fatalf("build saga recovery ran before work admission resolved: %v", err)
	case <-time.After(20 * time.Millisecond):
	}

	close(releaseAdmission)
	require.NoError(t, <-done)
}
