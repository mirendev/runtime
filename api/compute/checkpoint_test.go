package compute

import (
	"testing"

	compute "miren.dev/runtime/api/compute/compute_v1alpha"
	"miren.dev/runtime/pkg/entity/types"
	"miren.dev/runtime/pkg/secret"

	"github.com/stretchr/testify/require"
)

func TestCheckpointEligibility(t *testing.T) {
	for name, mutate := range map[string]func(*compute.Sandbox){
		"addon":               func(s *compute.Sandbox) { s.Spec.LogAttribute = types.LabelSet("addon", "postgres") },
		"task":                func(s *compute.Sandbox) { s.Spec.RestartPolicy = compute.SandboxSpecNEVER },
		"host network":        func(s *compute.Sandbox) { s.Spec.HostNetwork = true },
		"multiple containers": func(s *compute.Sandbox) { s.Spec.Container = append(s.Spec.Container, compute.SandboxSpecContainer{}) },
		"disk":                func(s *compute.Sandbox) { s.Spec.Volume = []compute.SandboxSpecVolume{{Provider: "miren"}} },
		"sqlite":              func(s *compute.Sandbox) { s.Spec.Volume = []compute.SandboxSpecVolume{{Provider: "sqlite"}} },
		"bind mount": func(s *compute.Sandbox) {
			s.Spec.Container[0].Mount = []compute.SandboxSpecContainerMount{{Source: "data"}}
		},
		"config file": func(s *compute.Sandbox) {
			s.Spec.Container[0].ConfigFile = []compute.SandboxSpecContainerConfigFile{{Path: "/config"}}
		},
		"tty":        func(s *compute.Sandbox) { s.Spec.Container[0].Tty = true },
		"stdin":      func(s *compute.Sandbox) { s.Spec.Container[0].Stdin = true },
		"privileged": func(s *compute.Sandbox) { s.Spec.Container[0].Privileged = true },
		"secret reference": func(s *compute.Sandbox) {
			s.Spec.Container[0].Env = []string{"TOKEN=" + secret.FormatSentinel("cluster", "token@v1")}
		},
		"admin token": func(s *compute.Sandbox) {
			s.Spec.Container[0].Env = []string{"ADMIN_TOKEN=generated-value"}
		},
		"instance credential": func(s *compute.Sandbox) {
			s.Spec.Container[0].Env = []string{"MIREN_IDENTITY_TOKEN_SECRET=generated-value"}
		},
		"node port": func(s *compute.Sandbox) { s.Spec.Container[0].Port[0].NodePort = 8080 },
		"udp":       func(s *compute.Sandbox) { s.Spec.Container[0].Port[0].Protocol = compute.SandboxSpecContainerPortUDP },
	} {
		t.Run(name, func(t *testing.T) {
			sb := &compute.Sandbox{Spec: compute.SandboxSpec{
				Version: "version/v1", LogAttribute: types.LabelSet("miren.stage", "app-run"),
				Container: []compute.SandboxSpecContainer{{Name: "web", Image: "app:v1", Env: []string{"PORT=3000"},
					Port: []compute.SandboxSpecContainerPort{{Port: 3000, Protocol: compute.SandboxSpecContainerPortTCP}}}},
			}}
			require.True(t, CheckpointEligible(sb))
			mutate(sb)
			require.False(t, CheckpointEligible(sb))
		})
	}
}

func TestCheckpointStatusesDoNotPublishDNS(t *testing.T) {
	for _, status := range []compute.SandboxStatus{compute.HIBERNATING, compute.HIBERNATED, compute.RESTORING} {
		require.False(t, SandboxActive(status))
		require.False(t, SandboxDead(status))
		require.True(t, SandboxHibernation(status))
		require.Equal(t, status == compute.RESTORING, SandboxWaking(status))
	}
}
