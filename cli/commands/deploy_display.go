package commands

import (
	"time"

	"miren.dev/runtime/api/deployment/deployment_v1alpha"
)

// displayLockInfo shows a deployment lock error message with structured details.
func displayLockInfo(ctx *Context, operation string, lockInfo *deployment_v1alpha.DeploymentLockInfo) {
	ctx.Printf("\n❌ %s blocked:\n\n", operation)
	ctx.Printf("Another deployment is already in progress for app '%s' on cluster '%s'.\n\n",
		lockInfo.AppName(), lockInfo.ClusterId())
	ctx.Printf("  • Started by: %s\n", lockInfo.StartedBy())
	if lockInfo.HasStartedAt() && lockInfo.StartedAt() != nil {
		startedAt := time.Unix(lockInfo.StartedAt().Seconds(), 0)
		ctx.Printf("  • Started at: %s (%s ago)\n",
			startedAt.Format("2006-01-02 15:04:05 MST"),
			time.Since(startedAt).Round(time.Second))
	}
	ctx.Printf("  • Current phase: %s\n", lockInfo.CurrentPhase())
}

// displayDeployVersionAccessInfo shows route/access information from the
// DeployVersion RPC response, mirroring the displayAccessInfo function
// used by the full deploy path.
func displayDeployVersionAccessInfo(ctx *Context, appName string, accessInfo *deployment_v1alpha.AccessInfo) {
	var hostnames []string
	if accessInfo.HasHostnames() && accessInfo.Hostnames() != nil {
		hostnames = *accessInfo.Hostnames()
	}
	hasDefaultRoute := accessInfo.DefaultRoute()

	var clusterAddr string
	if accessInfo.ClusterHostname() != "" {
		clusterAddr = accessInfo.ClusterHostname()
	} else if ctx.ClusterConfig != nil && ctx.ClusterConfig.Hostname != "" {
		clusterAddr = stripPort(ctx.ClusterConfig.Hostname)
	}

	if len(hostnames) > 0 {
		ctx.Printf("\nYour app is available at:\n")
		for _, host := range hostnames {
			ctx.Printf("  https://%s\n", host)
		}
		if hasDefaultRoute {
			ctx.Printf("  (also the default route)\n")
		}
	} else if hasDefaultRoute {
		if clusterAddr != "" {
			ctx.Printf("\nYour app is the default route, available at:\n")
			ctx.Printf("  https://%s\n", clusterAddr)
		} else {
			ctx.Printf("\nYour app is the default route and will receive all unmatched traffic.\n")
		}
		suggestRoute(ctx, appName)
	} else {
		ctx.Printf("\nNo routes configured for this app.\n")
		suggestRoute(ctx, appName)
		ctx.Printf("To make it the default route: miren route set-default %s\n", appName)
	}
}
