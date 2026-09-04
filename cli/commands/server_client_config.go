//go:build linux

package commands

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	"miren.dev/runtime/clientconfig"
	"miren.dev/runtime/components/coordinate"
	runtimeserver "miren.dev/runtime/components/server"
	"miren.dev/runtime/pkg/caauth"
	"miren.dev/runtime/pkg/serverconfig"
)

func configureServerClient(ctx *Context, config *serverconfig.Config, foundation *coordinate.Foundation) error {
	if config.Server.GetConfigClusterName() == "" {
		config.Server.SetConfigClusterName("local")
	}
	if config.GetMode() != "standalone" || config.Server.GetSkipClientConfig() {
		return nil
	}
	certificate, err := foundation.IssueCertificate("miren-server")
	if err != nil {
		ctx.Log.Error("failed to issue server certificate", "error", err)
		return fmt.Errorf("failed to issue server certificate: %w", err)
	}

	address := runtimeserver.LocalClientAddress(ctx.Log, config.Server.GetAddress())
	ctx.Log.Info("writing local cluster config", "cluster-name", config.Server.GetConfigClusterName(), "server-address", address)
	if err := writeLocalClusterConfig(ctx, certificate, address, config.Server.GetConfigClusterName()); err != nil {
		ctx.Log.Warn("failed to write local cluster config", "error", err)
	}
	return nil
}

func writeLocalClusterConfig(ctx *Context, certificate *caauth.ClientCertificate, address, clusterName string) error {
	config, err := clientconfig.LoadConfig()
	if err != nil {
		if !errors.Is(err, clientconfig.ErrNoConfig) {
			return fmt.Errorf("failed to load existing client config: %w", err)
		}
		ctx.Log.Info("error loading existing client config, creating new one", "error", err)
		config = clientconfig.NewConfig()
	}

	config.SetLeafConfig("50-local", &clientconfig.ConfigData{
		Clusters: map[string]*clientconfig.ClusterConfig{
			clusterName: {
				Hostname:   address,
				CACert:     string(certificate.CACert),
				ClientCert: string(certificate.CertPEM),
				ClientKey:  string(certificate.KeyPEM),
			},
		},
	})
	if config.ActiveCluster() == "" {
		config.SetActiveCluster(clusterName)
	}
	if err := config.Save(); err != nil {
		return fmt.Errorf("failed to save local cluster leaf config: %w", err)
	}

	sourcePath := config.SourcePath()
	var pathsToFix []string
	if sourcePath == "" {
		ctx.Log.Warn("client config source path is empty, cannot fix ownership or permissions")
	} else {
		pathsToFix = []string{
			filepath.Join(filepath.Dir(sourcePath), "clientconfig.d"),
			filepath.Join(filepath.Dir(sourcePath), "clientconfig.d", "50-local.yaml"),
			sourcePath,
		}
	}

	for _, entry := range pathsToFix {
		if err := fixOwnershipIfSudo(entry); err != nil {
			ctx.Log.Warn("failed to fix directory ownership", "dir", entry, "error", err)
		}
		info, err := os.Stat(entry)
		if err != nil {
			ctx.Log.Warn("failed to stat directory for permission fix", "dir", entry, "error", err)
			continue
		}
		if info.IsDir() {
			continue
		}
		if err := os.Chmod(entry, 0600); err != nil {
			ctx.Log.Warn("failed to set main config file permissions", "error", err)
		}
	}

	ctx.Log.Info("wrote local cluster config", "path", sourcePath, "name", clusterName, "address", address)
	return nil
}

// fixOwnershipIfSudo gives files created under sudo back to the invoking user.
func fixOwnershipIfSudo(path string) error {
	if os.Geteuid() != 0 {
		return nil
	}
	sudoUID := os.Getenv("SUDO_UID")
	sudoGID := os.Getenv("SUDO_GID")
	if sudoUID == "" || sudoGID == "" {
		return nil
	}

	uid, err := strconv.Atoi(sudoUID)
	if err != nil {
		return fmt.Errorf("failed to parse SUDO_UID: %w", err)
	}
	gid, err := strconv.Atoi(sudoGID)
	if err != nil {
		return fmt.Errorf("failed to parse SUDO_GID: %w", err)
	}
	if err := os.Chown(path, uid, gid); err != nil {
		return fmt.Errorf("failed to chown %s to %d:%d: %w", path, uid, gid, err)
	}
	return nil
}
