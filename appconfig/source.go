package appconfig

import (
	"path/filepath"

	"miren.dev/runtime/pkg/tarx"
)

// SourceDir resolves the build context relative to the directory containing .miren.
func (ac *AppConfig) SourceDir(configDir string) string {
	if ac == nil || ac.Build == nil || ac.Build.SourceRoot == "" {
		return configDir
	}
	if filepath.IsAbs(ac.Build.SourceRoot) {
		return filepath.Clean(ac.Build.SourceRoot)
	}
	return filepath.Join(configDir, ac.Build.SourceRoot)
}

// UploadFiles projects this app's config into the build context without changing
// the source tree or using another app's config from the source root.
func (ac *AppConfig) UploadFiles(configDir string) []tarx.FileOverride {
	if filepath.Clean(ac.SourceDir(configDir)) == filepath.Clean(configDir) {
		return nil
	}
	return []tarx.FileOverride{{Path: AppConfigPath, Source: filepath.Join(configDir, AppConfigPath)}}
}
