package commands

import (
	"context"
	"time"

	"miren.dev/runtime/api/deployment/deployment_v1alpha"
)

// deploymentStatusGetter is an interface for getting deployment status
// This allows mocking the deployment client in tests
type deploymentStatusGetter interface {
	GetStatus(ctx context.Context, deploymentId string) (string, error)
}

// cancellationPoller polls for deployment cancellation
type cancellationPoller struct {
	deploymentId string
	getter       deploymentStatusGetter
	pollInterval time.Duration
}

func newCancellationPoller(deploymentId string, getter deploymentStatusGetter, pollInterval time.Duration) *cancellationPoller {
	return &cancellationPoller{
		deploymentId: deploymentId,
		getter:       getter,
		pollInterval: pollInterval,
	}
}

// Start begins polling for cancellation. It returns when ctx is done or cancellation is detected.
// When cancellation is detected, it calls cancelFunc.
func (p *cancellationPoller) Start(ctx context.Context, cancelFunc func()) {
	ticker := time.NewTicker(p.pollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			// Use a fresh context for polling to avoid issues with cancelled parent
			pollCtx, pollCancel := context.WithTimeout(context.Background(), 5*time.Second)
			status, err := p.getter.GetStatus(pollCtx, p.deploymentId)
			pollCancel()
			if err != nil {
				continue
			}
			if status == "cancelled" {
				cancelFunc()
				return
			}
		}
	}
}

// logger is an interface for logging (allows mocking in tests)
type logger interface {
	Debug(msg string, args ...any)
}

// depClientStatusGetter wraps a deployment client to implement deploymentStatusGetter
type depClientStatusGetter struct {
	client *deployment_v1alpha.DeploymentClient
	log    logger
}

func newDepClientStatusGetter(client *deployment_v1alpha.DeploymentClient, log logger) *depClientStatusGetter {
	return &depClientStatusGetter{client: client, log: log}
}

func (d *depClientStatusGetter) GetStatus(ctx context.Context, deploymentId string) (string, error) {
	result, err := d.client.GetDeploymentById(ctx, deploymentId)
	if err != nil {
		if d.log != nil {
			d.log.Debug("Failed to poll deployment status", "error", err)
		}
		return "", err
	}
	if result.HasDeployment() && result.Deployment() != nil {
		return result.Deployment().Status(), nil
	}
	return "", nil
}
