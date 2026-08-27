//go:build linux

package server

import (
	"testing"

	"miren.dev/runtime/pkg/readiness"
)

type recordingReporter struct {
	notReady bool
}

func (*recordingReporter) Started() {}
func (*recordingReporter) Ready()   {}

func (r *recordingReporter) NotReady() {
	r.notReady = true
}

func TestBuildkitWithoutConfiguredDaemonStaysNotReady(t *testing.T) {
	boot := &buildkitBoot{
		containerd:    &containerdBoot{},
		observability: &observabilityBoot{},
	}
	reporter := &recordingReporter{}

	if err := boot.start(t.Context(), reporter); err != nil {
		t.Fatalf("start() error = %v", err)
	}
	if !reporter.notReady {
		t.Fatal("buildkit reported ready without an embedded or external daemon")
	}
}

var _ readiness.Reporter = (*recordingReporter)(nil)
