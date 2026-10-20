package sandbox

import (
	"log/slog"
	"testing"
	"time"

	containerd "github.com/containerd/containerd/v2/client"
	"github.com/containerd/containerd/v2/pkg/cio"
	"github.com/stretchr/testify/assert"

	"miren.dev/runtime/pkg/entity"
)

// fakeIO is a cio.IO whose copy finishes when the test says so.
type fakeIO struct {
	cio.IO
	copied chan struct{}
}

func (f *fakeIO) Wait() { <-f.copied }

// ioTask is a containerd.Task that only answers IO, which is all
// waitForTaskIO asks of it.
type ioTask struct {
	containerd.Task
	io cio.IO
}

func (t ioTask) IO() cio.IO { return t.io }

func TestWaitForTaskIOReturnsOnceTheCopyFinishes(t *testing.T) {
	c := &SandboxController{Log: slog.Default()}
	tio := &fakeIO{copied: make(chan struct{})}

	returned := make(chan struct{})
	go func() {
		c.waitForTaskIO(entity.Id("sandbox/sb"), "ctr", ioTask{io: tio})
		close(returned)
	}()

	select {
	case <-returned:
		t.Fatal("returned while containerd was still copying the task's output")
	case <-time.After(50 * time.Millisecond):
	}

	close(tio.copied)
	select {
	case <-returned:
	case <-time.After(2 * time.Second):
		t.Fatal("did not return after the copy finished")
	}
}

// A copy that never finishes -- a wedged log consumer, or something still
// holding the container's stdout -- must not hold up recording the exit
// forever.
func TestWaitForTaskIOIsBounded(t *testing.T) {
	saved := taskIODrainTimeout
	taskIODrainTimeout = 50 * time.Millisecond
	defer func() { taskIODrainTimeout = saved }()

	c := &SandboxController{Log: slog.Default()}
	tio := &fakeIO{copied: make(chan struct{})}
	defer close(tio.copied)

	start := time.Now()
	c.waitForTaskIO(entity.Id("sandbox/sb"), "ctr", ioTask{io: tio})
	assert.Less(t, time.Since(start), 2*time.Second)
}

func TestWaitForTaskIOWithoutIO(t *testing.T) {
	c := &SandboxController{Log: slog.Default()}
	assert.NotPanics(t, func() {
		c.waitForTaskIO(entity.Id("sandbox/sb"), "ctr", ioTask{})
	})
}
