package profile

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/davecgh/go-spew/spew"
	"github.com/stretchr/testify/require"
)

func TestProfile(t *testing.T) {
	t.Run("can profile a process", func(t *testing.T) {
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
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr

		err = cmd.Start()
		r.NoError(err)

		defer cmd.Process.Kill()

		symzer, err := NewSymbolizer(path)
		r.NoError(err)

		profiler, err := NewProfiler(cmd.Process.Pid, symzer)
		r.NoError(err)

		defer profiler.Stop()

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		err = profiler.Start(ctx)
		r.NoError(err)

		time.Sleep(30 * time.Second)

		stacks, err := profiler.Stacks()
		r.NoError(err)

		r.Greater(len(stacks), 1)

		spew.Dump(stacks[0].User())

		spew.Dump(profiler.CallTree())

		err = profiler.Stop()
		r.NoError(err)
	})
}
