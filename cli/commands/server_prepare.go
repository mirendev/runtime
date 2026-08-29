//go:build linux

package commands

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"miren.dev/runtime/pkg/serverconfig"
	"miren.dev/runtime/version"
)

// prepareServerConfig resolves command-side installation policy before the
// server runtime freezes its component graph.
func prepareServerConfig(ctx *Context, config *serverconfig.Config) error {
	switch config.GetMode() {
	case "standalone":
		if config.Server.GetReleasePath() == "" {
			if releasePath := FindReleasePath(); releasePath != "" {
				config.Server.SetReleasePath(releasePath)
				ctx.Log.Info("using release path", "path", releasePath)
			} else {
				ctx.Log.Info("no release directory found, downloading release")
				downloadGlobal, downloadPath, err := releaseDownloadDestination()
				if err != nil {
					return err
				}
				branch := "latest"
				if configuredBranch := version.Branch(); configuredBranch != "" {
					branch = configuredBranch
				}
				if err := PerformDownloadRelease(ctx, DownloadReleaseOptions{
					Branch: branch,
					Global: downloadGlobal,
					Force:  false,
					Output: downloadPath,
				}); err != nil {
					return fmt.Errorf("failed to download release: %w", err)
				}
				config.Server.SetReleasePath(downloadPath)
				ctx.Log.Info("using downloaded release", "path", downloadPath)
			}
			ctx.UILog.Info("running in standalone mode - starting all components", "release-path", config.Server.GetReleasePath())
			return nil
		}

		path, err := filepath.Abs(config.Server.GetReleasePath())
		if err != nil {
			return fmt.Errorf("failed to resolve absolute release path: %w", err)
		}
		config.Server.SetReleasePath(path)
		if _, err := os.Stat(path); err != nil {
			return fmt.Errorf("release path does not exist: %s", path)
		}
		return nil
	case "distributed":
		ctx.UILog.Info("running in distributed mode")
		return nil
	default:
		return fmt.Errorf("unknown mode: %s (valid modes: standalone, distributed)", config.GetMode())
	}
}

func releaseDownloadDestination() (global bool, path string, err error) {
	var userPath string
	if homeDir, homeErr := getUserHomeDir(); homeErr == nil {
		userPath = filepath.Join(homeDir, ".miren", "release")
	}

	if err := os.MkdirAll("/var/lib/miren", 0755); err != nil {
		if userPath != "" {
			return false, userPath, nil
		}
		return false, "", fmt.Errorf("unable to create /var/lib/miren and no user path available: %w", err)
	}

	testPath := fmt.Sprintf("/var/lib/miren/.test_%d_%d", os.Getpid(), time.Now().UnixNano())
	file, err := os.OpenFile(testPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0644)
	if err == nil {
		_ = file.Close()
		_ = os.Remove(testPath)
		return true, systemReleasePath, nil
	}
	if userPath != "" {
		return false, userPath, nil
	}
	return false, "", fmt.Errorf("unable to write to /var/lib/miren and no user path available: %w", err)
}
