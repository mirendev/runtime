package std

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	containerd "github.com/containerd/containerd/v2/client"
	"github.com/stretchr/testify/require"
	"miren.dev/runtime/discovery"
	"miren.dev/runtime/health"
	"miren.dev/runtime/network"
	"miren.dev/runtime/observability"
	"miren.dev/runtime/pkg/asm"
	"miren.dev/runtime/pkg/testutils"
	"miren.dev/runtime/run"
)

type Std struct {
	context.Context
	*asm.Registry

	cleanup func()

	CC          *containerd.Client
	CR          *run.ContainerRunner
	Mon         *observability.RunSCMonitor
	RunSCBinary string
}

func (s *Std) Cleanup() {
	s.Mon.Close()
	s.cleanup()
}

func Setup(t *testing.T) *Std {
	t.Helper()

	ctx, cancel := context.WithTimeout(t.Context(), 120*time.Second)

	reg, cleanup := testutils.Registry(
		observability.TestInject,
		discovery.TestInject,
		run.TestInject,
		network.TestInject,
	)

	r := require.New(t)

	var cc *containerd.Client
	err := reg.Init(&cc)
	r.NoError(err)

	var (
		lm  observability.LogsMaintainer
		cm  health.ContainerMonitor
		mon observability.RunSCMonitor
	)

	err = reg.Populate(&lm)
	r.NoError(err)

	err = lm.Setup(ctx)
	r.NoError(err)

	err = reg.Populate(&cm)
	r.NoError(err)

	go cm.MonitorEvents(ctx)

	reg.Register("ports", observability.PortTracker(&cm))

	err = reg.Populate(&mon)
	r.NoError(err)

	mon.SetEndpoint(filepath.Join(t.TempDir(), "runsc-mon.sock"))

	runscBin, podInit := testutils.SetupRunsc(t.TempDir())

	err = mon.WritePodInit(podInit)
	r.NoError(err)

	err = mon.Monitor(ctx)
	r.NoError(err)

	var cr *run.ContainerRunner
	err = reg.Init(&cr)

	cr.RunscBinary = runscBin

	reg.Register("containerd", cc)
	reg.Register("container-runner", cr)
	reg.Register("runsc-monitor", &mon)
	reg.Register("logs-maintainer", lm)
	reg.Register("container-monitor", &cm)

	return &Std{
		Context:  ctx,
		Registry: reg,
		cleanup:  func() { cancel(); cleanup() },

		CC:          cc,
		CR:          cr,
		Mon:         &mon,
		RunSCBinary: runscBin,
	}
}

func (s *Std) CleanContainers(t *testing.T) func() {
	t.Helper()

	require.NoError(t, testutils.ClearContainers(s.CC, s.CR.Namespace))
	return func() {
		testutils.ClearContainers(s.CC, s.CR.Namespace)
	}
}
