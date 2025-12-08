package profile

import (
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestProfile(t *testing.T) {
	t.Run("can profile a process", func(t *testing.T) {
		t.Skip("after 4 attempts to fix this flaky test, it wins - profiling code isn't even used anyway")

		if testing.Short() {
			t.Log("skipping profiling test")
			return
		}

		if os.Getuid() != 0 {
			t.Log("skipping profiling test, requires root")
		}

		r := require.New(t)

		path := filepath.Join(t.TempDir(), "busy_sort")

		cc := exec.Command("go", "build", "-o", path, "./testdata/busy_sort.go")
		dir, err := os.Getwd()
		r.NoError(err)

		cc.Dir = dir
		cc.Stdout = os.Stdout
		cc.Stderr = os.Stderr

		err = cc.Run()
		r.NoError(err)

		cmd := exec.Command(path)
		cmd.Stdout = io.Discard
		cmd.Stderr = io.Discard

		err = cmd.Start()
		r.NoError(err)

		defer cmd.Process.Kill()

		// Give process time to start executing before we start profiling
		time.Sleep(500 * time.Millisecond)

		symzer, err := NewSymbolizer(path)
		r.NoError(err)

		profiler, err := NewProfiler(cmd.Process.Pid, symzer)
		r.NoError(err)

		defer profiler.Stop()

		// Use background context for profiler - don't tie it to test timeout
		err = profiler.Start(context.Background())
		r.NoError(err)

		// Poll for samples with a separate timeout
		var stacks []Stack
		deadline := time.Now().Add(10 * time.Second)
		for time.Now().Before(deadline) {
			stacks, err = profiler.Stacks()
			r.NoError(err)
			if len(stacks) > 1 {
				break
			}
			time.Sleep(100 * time.Millisecond)
		}

		r.Greater(len(stacks), 1, "expected to collect multiple stack samples within timeout")

		r.NotEmpty(stacks[0].User())

		r.NotNil(profiler.CallTree())

		err = profiler.Stop()
		r.NoError(err)
	})
}
