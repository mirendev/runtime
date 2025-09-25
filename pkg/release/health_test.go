package release

import (
	"context"
	"testing"
	"time"
)

func TestNoOpHealthVerifier(t *testing.T) {
	verifier := &NoOpHealthVerifier{}
	err := verifier.VerifyHealth(context.Background(), 1*time.Second)
	if err != nil {
		t.Errorf("NoOpHealthVerifier should always return nil, got: %v", err)
	}
}

func TestIsServerRunning(t *testing.T) {
	// This test just verifies the function doesn't panic
	// The actual result depends on whether systemd is available
	// and whether the miren service is installed
	_ = IsServerRunning()
	// If we get here without panicking, the test passes
}