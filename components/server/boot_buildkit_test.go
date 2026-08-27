//go:build linux

package server

import (
	"testing"
)

func TestBuildkitWithoutConfiguredDaemonPublishesEmptyOutput(t *testing.T) {
	b := &buildkitBoot{}

	output, err := b.startDisabled(t.Context())
	if err != nil {
		t.Fatalf("start() error = %v", err)
	}
	if output.component != nil {
		t.Fatal("buildkit published a component without an embedded or external daemon")
	}
}
