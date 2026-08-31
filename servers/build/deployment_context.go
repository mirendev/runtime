package build

import (
	"context"
	"errors"
	"sync"

	"miren.dev/runtime/pkg/cond"
	"miren.dev/runtime/pkg/deploylifecycle"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/entity/indexwatch"
	"miren.dev/runtime/pkg/rpc"
)

var errDeploymentCancelled = errors.New("deployment cancelled")

type deploymentContextKey struct{}

// deploymentContext separates the server-owned lifetime of a build from the
// context its actions use. CancelDeployment ends the action context while the
// control context remains live for saga persistence and compensation.
type deploymentContext struct {
	control       context.Context
	action        context.Context
	cancelAction  context.CancelCauseFunc
	cancelControl context.CancelFunc

	mu       sync.Mutex
	watchers []*indexwatch.Watcher
	closed   bool
}

func newDeploymentContext(requestCtx context.Context) *deploymentContext {
	control, cancelControl := rpc.Detach(requestCtx)
	action, cancelAction := context.WithCancelCause(control)
	d := &deploymentContext{
		control:       control,
		action:        action,
		cancelAction:  cancelAction,
		cancelControl: cancelControl,
	}
	d.action = context.WithValue(d.action, deploymentContextKey{}, d)
	return d
}

func deploymentContextFrom(ctx context.Context) *deploymentContext {
	d, _ := ctx.Value(deploymentContextKey{}).(*deploymentContext)
	return d
}

func (d *deploymentContext) result(err error) error {
	if errors.Is(context.Cause(d.action), errDeploymentCancelled) {
		return cond.ErrRemote{Category: "deployment", Code: "cancelled", Message: "deployment cancelled"}
	}
	return err
}

func (d *deploymentContext) attach(t *deployTracking) {
	if d == nil || t == nil || t.eac == nil {
		return
	}

	w := indexwatch.New(t.eac, entity.Ref(entity.DBId, entity.Id(t.deploymentID)), indexwatch.Options{
		Logger:     t.log,
		BufferSize: 4,
	})

	d.mu.Lock()
	if d.closed {
		d.mu.Unlock()
		return
	}
	d.watchers = append(d.watchers, w)
	d.mu.Unlock()

	_ = w.Start(d.action)
	go func() {
		for {
			select {
			case <-d.action.Done():
				return
			case _, ok := <-w.Updates():
				if !ok {
					return
				}

				status, err := t.tracker.Store().Status(d.action, t.deploymentID)
				if err != nil {
					if d.action.Err() == nil {
						t.log.Warn("failed to read deployment while watching cancellation", "error", err)
					}
					continue
				}
				if status == deploylifecycle.StatusCancelled {
					t.log.Info("deployment cancellation received")
					d.cancelAction(errDeploymentCancelled)
					return
				}
			}
		}
	}()
}

func (d *deploymentContext) Close() {
	if d == nil {
		return
	}

	d.mu.Lock()
	if d.closed {
		d.mu.Unlock()
		return
	}
	d.closed = true
	watchers := append([]*indexwatch.Watcher(nil), d.watchers...)
	d.mu.Unlock()

	d.cancelAction(nil)
	for _, w := range watchers {
		w.Stop()
	}
	d.cancelControl()
}
