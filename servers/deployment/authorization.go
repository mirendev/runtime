package deployment

import (
	"context"

	appclient "miren.dev/runtime/api/app"
	"miren.dev/runtime/api/core/core_v1alpha"
	"miren.dev/runtime/pkg/rpc"
)

func (d *DeploymentServer) authorizeApp(ctx context.Context, name, action string) error {
	if name == "" && action == "read" {
		// An unfiltered history request requires an unscoped read grant.
		return rpc.AuthorizeResource(ctx, rpc.ResourceRequest{Resource: "app", Action: action})
	}
	return appclient.Authorize(ctx, d.EAC, name, action)
}

func (d *DeploymentServer) authorizeDeployment(ctx context.Context, deployment *core_v1alpha.Deployment, action string) error {
	if !rpc.HasResourceAuthorizer(ctx) {
		return nil // Existing AllowApp guards still apply to trusted internal calls.
	}
	if deployment.App == "" {
		// Pre-attempt deployment records only stored the app name. Resolve
		// that name through the app store rather than denying legacy history.
		if deployment.AppName == "" {
			return rpc.ErrUnauthorized
		}
		return appclient.Authorize(ctx, d.EAC, deployment.AppName, action)
	}
	return appclient.Authorize(ctx, d.EAC, deployment.App.String(), action)
}
