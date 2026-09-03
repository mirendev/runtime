//go:build linux

package distributedrunner

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"golang.org/x/sync/errgroup"
	"miren.dev/runtime/clientconfig"
	"miren.dev/runtime/pkg/boot"
	"miren.dev/runtime/pkg/runnerconfig"
)

const (
	// ShutdownTimeout is the deadline passed through the distributed runner
	// graph. Stop functions whose underlying Close APIs do not accept a context
	// cannot be forcibly interrupted by it.
	ShutdownTimeout      = 3 * time.Minute
	componentStopTimeout = 30 * time.Second
)

// StartOptions contains the resolved command inputs used to assemble one fixed
// distributed-runner boot graph.
type StartOptions struct {
	Log          *slog.Logger
	Context      context.Context
	Group        *errgroup.Group
	Config       *runnerconfig.Config
	ClientConfig *clientconfig.Config
	ListenAddr   string
	DataPath     string

	// ContainerdSocket selects an external daemon. When it is empty,
	// ContainerdBinary and ContainerdBinDir describe the embedded daemon the
	// command selected before constructing the graph.
	ContainerdSocket string
	ContainerdBinary string
	ContainerdBinDir string
}

// Runtime owns the started distributed-runner graph.
type Runtime struct {
	graph *boot.Graph
	once  sync.Once

	stopErr error
}

// Start assembles, validates, and starts the distributed-runner dependency
// graph.
func Start(options StartOptions) (*Runtime, error) {
	if options.Config == nil {
		return nil, fmt.Errorf("distributed runner config is required")
	}
	if options.Context == nil {
		return nil, fmt.Errorf("distributed runner context is required")
	}
	if options.Group == nil {
		return nil, fmt.Errorf("distributed runner errgroup is required")
	}

	runtime := &Runtime{graph: boot.NewGraph()}
	components := newStartup(runtime, options)
	if err := components.addComponents(); err != nil {
		return nil, err
	}
	if err := runtime.graph.Validate(); err != nil {
		return nil, err
	}
	if err := runtime.graph.Start(options.Context); err != nil {
		stopCtx, cancel := context.WithTimeout(context.Background(), ShutdownTimeout)
		defer cancel()
		if stopErr := runtime.Stop(stopCtx); stopErr != nil && options.Log != nil {
			options.Log.Error("cleanup after failed startup did not complete", "error", stopErr)
		}
		return nil, err
	}
	return runtime, nil
}

// Stop tears down the graph once, in reverse dependency order.
func (r *Runtime) Stop(ctx context.Context) error {
	r.once.Do(func() {
		r.stopErr = r.graph.Stop(ctx)
	})
	return r.stopErr
}
