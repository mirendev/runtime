package runner

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"miren.dev/runtime/api/compute"
	"miren.dev/runtime/api/compute/compute_v1alpha"
	"miren.dev/runtime/api/runner/runner_v1alpha"
)

func (s *RegistrationServer) SandboxCounts(ctx context.Context, req *runner_v1alpha.RunnerRegistrationSandboxCounts) error {
	identity, err := requireRunnerCertIdentity(ctx)
	if err != nil {
		return err
	}
	// Anonymous mode cannot supply a node scope; unlike token issuance there
	// is no sandbox argument from which to derive one.
	if identity == nil || !strings.HasPrefix(identity.Subject, "runner-") {
		return fmt.Errorf("caller must authenticate with a runner certificate")
	}
	runnerID := strings.TrimPrefix(identity.Subject, "runner-")
	if runnerID == "" {
		return fmt.Errorf("runner certificate has no runner id")
	}
	nodeID := compute_v1alpha.NewNodeId(runnerID)
	resp, err := s.EAC.Get(ctx, nodeID.String())
	if err != nil {
		return fmt.Errorf("looking up runner node: %w", err)
	}
	var node compute_v1alpha.Node
	node.Decode(resp.Entity().Entity())
	if node.RunnerId != runnerID {
		return fmt.Errorf("caller is not the registered owner of node %s", nodeID)
	}

	sandboxes, err := s.EAC.List(ctx, compute_v1alpha.Index(compute_v1alpha.KindSandbox, nodeID.Id()))
	if err != nil {
		return fmt.Errorf("listing runner sandboxes: %w", err)
	}
	var live, running int64
	others := map[string]int64{}
	for _, value := range sandboxes.Values() {
		var sb compute_v1alpha.Sandbox
		sb.Decode(value.Entity())
		if compute.SandboxDead(sb.Status) {
			continue
		}
		live++
		if sb.Status == compute_v1alpha.RUNNING {
			running++
			continue
		}
		status := strings.TrimPrefix(string(sb.Status), "status.")
		if status == "" {
			status = "unknown"
		}
		others[status]++
	}

	names := make([]string, 0, len(others))
	for name := range others {
		names = append(names, name)
	}
	sort.Strings(names)
	counts := make([]*runner_v1alpha.SandboxStatusCount, 0, len(names))
	for _, name := range names {
		count := &runner_v1alpha.SandboxStatusCount{}
		count.SetStatus(name)
		count.SetCount(others[name])
		counts = append(counts, count)
	}
	res := req.Results()
	res.SetLive(live)
	res.SetRunning(running)
	res.SetOthers(counts)
	return nil
}
