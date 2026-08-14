package sandbox

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	compute "miren.dev/runtime/api/compute/compute_v1alpha"
	"miren.dev/runtime/observability"
	"miren.dev/runtime/pkg/controller"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/saga"
)

// SagaSandboxController implements SandboxLifecycle using the saga pattern
// for crash-recoverable sandbox creation. It wraps an inner SandboxController
// and delegates most operations to it, replacing only createSandbox with a
// saga-based implementation.
type SagaSandboxController struct {
	inner    *SandboxController
	ops      *sandboxOps
	executor *saga.Executor
	storage  saga.Storage
	registry *saga.Registry
	log      *slog.Logger
}

// NewSagaSandboxController creates a saga-based sandbox controller.
func NewSagaSandboxController(
	cfg SandboxControllerDeps,
	storage saga.Storage,
	log *slog.Logger,
) (*SagaSandboxController, error) {
	inner, err := NewSandboxController(cfg)
	if err != nil {
		return nil, fmt.Errorf("creating inner controller: %w", err)
	}

	registry := saga.NewRegistry()
	executor := saga.NewExecutor(storage,
		saga.WithRegistry(registry),
		saga.WithLogger(log.With("module", "saga-sandbox")),
	)

	return &SagaSandboxController{
		inner:    inner,
		ops:      &sandboxOps{ctrl: inner},
		executor: executor,
		storage:  storage,
		registry: registry,
		log:      log.With("module", "saga-sandbox-controller"),
	}, nil
}

// Init initializes the sandbox controller and registers saga definitions.
func (s *SagaSandboxController) Init(ctx context.Context) error {
	if err := s.inner.Init(ctx); err != nil {
		return err
	}

	if err := registerCreateSandboxSaga(s.registry, s.ops, s.ops, s.ops, s.ops, s.log); err != nil {
		return fmt.Errorf("registering create-sandbox saga: %w", err)
	}

	// Recover any incomplete sagas from a previous crash
	if err := s.executor.Recover(ctx); err != nil {
		s.log.Error("saga recovery completed with errors", "error", err)
	}

	return nil
}

// Create handles sandbox create/update events. For new sandboxes, it uses
// the saga-based creation flow.
func (s *SagaSandboxController) Create(ctx context.Context, co *compute.Sandbox, meta *entity.Meta) error {
	switch co.Status {
	case compute.DEAD:
		return nil
	case compute.STOPPED:
		s.log.Debug("sandbox is stopped, verifying it is no longer running")
		return s.inner.StopSandbox(ctx, co.ID, co)
	case "", compute.PENDING, compute.RUNNING:
		searchRes, err := s.inner.CheckSandbox(ctx, co, meta)
		if err != nil {
			s.log.Error("error checking sandbox, proceeding with create", "err", err)
		} else {
			switch searchRes {
			case same:
				// Same adoption gap as the non-saga controller: healthy
				// containers this process didn't boot need their metrics
				// re-registered here (MIR-1013).
				if err := s.inner.ensureMetrics(ctx, co); err != nil {
					s.log.Warn("failed to ensure metrics for existing sandbox",
						"id", co.ID, "error", err)
				}

				if co.Status == compute.PENDING {
					createdAt := meta.GetCreatedAt()
					age := time.Since(createdAt)
					const staleThreshold = 2 * time.Minute

					if age > staleThreshold {
						s.log.Info("sandbox exists and is healthy but status is PENDING (stale), updating to RUNNING",
							"id", co.ID, "createdAt", createdAt, "age", age)
						patchAttrs := entity.New(
							entity.Ref(entity.DBId, co.ID),
							(&compute.Sandbox{Status: compute.RUNNING}).Encode,
						)
						_, err := s.ops.PatchSandbox(ctx, patchAttrs.Attrs(), meta.Revision)
						if err != nil {
							return fmt.Errorf("failed to update sandbox status to RUNNING: %w", err)
						}
						return nil
					}
					s.log.Debug("sandbox is PENDING but recently created, skipping",
						"id", co.ID, "age", age)
					return nil
				}
				return nil
			case unhealthy:
				s.log.Info("sandbox container exists but is unhealthy", "id", co.ID)

				// Mirrors SandboxController.Create: a sandbox whose command
				// must execute at most once is finished the moment its
				// containers stop being healthy.
				if shouldRetireInsteadOfRestart(co) {
					return s.inner.markDeadNoRestart(ctx, co, "unhealthy")
				}

				if co.Status == compute.RUNNING {
					s.log.Info("marking unhealthy sandbox as DEAD", "id", co.ID)
					patchAttrs := entity.New(
						entity.Ref(entity.DBId, co.ID),
						(&compute.Sandbox{Status: compute.DEAD}).Encode,
					)
					_, err := s.ops.PatchSandbox(ctx, patchAttrs.Attrs(), 0)
					if err != nil {
						return fmt.Errorf("failed to mark sandbox as DEAD: %w", err)
					}
				}

				if err := s.inner.StopSandbox(ctx, co.ID, co); err != nil {
					return fmt.Errorf("failed to cleanup unhealthy sandbox: %w", err)
				}
				return nil
			}
		}

		// Mirrors SandboxController.Create: the containers are gone, and for a
		// sandbox that must not re-run its command that is the end of the road
		// rather than a reboot.
		if shouldRetireInsteadOfRestart(co) {
			return s.inner.markDeadNoRestart(ctx, co, "containers missing")
		}

		return s.createSandboxViaSaga(ctx, co)
	case compute.NOT_READY:
		// Transient boot state; nothing to reconcile until it resolves.
		fallthrough
	default:
		s.log.Warn("ignoring sandbox status", "status", co.Status)
		return nil
	}
}

// createSandboxViaSaga runs sandbox creation as a saga for crash recovery.
func (s *SagaSandboxController) createSandboxViaSaga(ctx context.Context, co *compute.Sandbox) error {
	s.log.Info("creating sandbox via saga", "id", co.ID)

	execID := fmt.Sprintf("create-sandbox-%s", co.ID)

	// We only get here once CheckSandbox has found the containers missing, so a
	// record of a previous successful creation describes resources that are no
	// longer there. Left in place it would resume straight to success and the
	// sandbox would never be rebuilt, which the old overwrite-on-start hid.
	if err := saga.DropIfCompleted(ctx, s.storage, execID); err != nil {
		return fmt.Errorf("clearing stale creation record: %w", err)
	}

	err := s.executor.Start(sagaCreateSandbox).
		Input("sandbox_id", co.ID.String()).
		WithID(execID).
		Execute(ctx)

	if errors.Is(err, saga.ErrExecutionInProgress) {
		// This controller keeps one executor for its lifetime, so the claim is
		// real here: an earlier pass is still driving this creation. Nothing
		// has failed, so leave the sandbox PENDING for the reconciler rather
		// than killing it over work that is still going.
		s.log.Debug("sandbox creation already in flight", "id", co.ID)
		return nil
	}

	if err != nil {
		s.log.Error("saga sandbox creation failed, marking DEAD", "id", co.ID, "error", err)

		// Saga compensating actions handle resource cleanup. The controller
		// owns the domain-level outcome: mark the sandbox DEAD so the pool
		// replaces it rather than retrying the same entity.
		// NOTE: this runs at the call site, so a crash between saga completion
		// and this patch leaves the entity PENDING (retried by reconciler).
		// Durable saga outcome declaration is future work.
		patchAttrs := entity.New(
			entity.Ref(entity.DBId, co.ID),
			(&compute.Sandbox{Status: compute.DEAD}).Encode,
		)
		if _, patchErr := s.ops.PatchSandbox(ctx, patchAttrs.Attrs(), 0); patchErr != nil {
			s.log.Error("failed to mark sandbox DEAD after saga failure", "id", co.ID, "error", patchErr)
		}

		return fmt.Errorf("saga sandbox creation failed: %w", err)
	}

	return nil
}

// Delete delegates to the inner controller.
func (s *SagaSandboxController) Delete(ctx context.Context, id entity.Id, sb *compute.Sandbox) error {
	return s.inner.Delete(ctx, id, sb)
}

// Close shuts down the inner controller.
func (s *SagaSandboxController) Close() error {
	return s.inner.Close()
}

// Periodic delegates to the inner controller.
func (s *SagaSandboxController) Periodic(ctx context.Context, timeHorizon time.Duration) error {
	return s.inner.Periodic(ctx, timeHorizon)
}

// SetWriteTracker sets the write tracker on both the saga controller and inner controller.
func (s *SagaSandboxController) SetWriteTracker(wt controller.WriteTracker) {
	s.inner.SetWriteTracker(wt)
}

// SetPortStatus delegates to the inner controller.
func (s *SagaSandboxController) SetPortStatus(id string, port observability.BoundPort, status observability.PortStatus) {
	s.inner.SetPortStatus(id, port, status)
}
