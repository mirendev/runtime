package observability_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"miren.dev/runtime/observability"
	"miren.dev/runtime/pkg/runsc"
	"miren.dev/runtime/pkg/testutils"
)

func TestRunSCMonitor(t *testing.T) {
	t.Run("generates a pod init config", func(t *testing.T) {
		r := require.New(t)

		reg, cleanup := testutils.Registry()
		defer cleanup()

		reg.Register("status-monitor", &observability.StatusMonitor{})

		var m observability.RunSCMonitor

		err := reg.Populate(&m)
		r.NoError(err)

		path := filepath.Join(t.TempDir(), "pod-init.json")

		err = m.WritePodInit(path)
		r.NoError(err)

		f, err := os.Open(path)
		r.NoError(err)

		defer f.Close()

		var cfg runsc.InitConfig

		err = json.NewDecoder(f).Decode(&cfg)
		r.NoError(err)

		r.NotNil(cfg.TraceSession)

		r.Equal("Default", cfg.TraceSession.Name)

		r.Len(cfg.TraceSession.Points, 7)

		r.Equal(runsc.EnterSyscallByName("accept"), cfg.TraceSession.Points[0].Name)
		r.Equal(runsc.EnterSyscallByName("accept4"), cfg.TraceSession.Points[1].Name)

		r.Len(cfg.TraceSession.Sinks, 1)

		r.Equal("remote", cfg.TraceSession.Sinks[0].Name)
		r.Equal("/run/runsc-mon.sock", cfg.TraceSession.Sinks[0].Config["endpoint"])
	})
}
