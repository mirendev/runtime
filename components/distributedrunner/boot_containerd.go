//go:build linux

package distributedrunner

import (
	"path/filepath"

	containerdcomp "miren.dev/runtime/components/containerd"
)

type containerdBootOutput = containerdcomp.Capability

func containerdBootConfig(options StartOptions) containerdcomp.BootConfig {
	var config containerdcomp.BootConfig
	if options.ContainerdSocket != "" {
		config = containerdcomp.ExternalBootConfig(options.Log, options.DataPath,
			options.ContainerdSocket)
	} else {
		config = containerdcomp.EmbeddedBootConfig(options.Log, options.DataPath,
			options.ContainerdBinary, options.ContainerdBinDir,
			filepath.Join(options.DataPath, "containerd", "containerd.sock"))
	}
	config.StopTimeout = componentStopTimeout
	return config
}
