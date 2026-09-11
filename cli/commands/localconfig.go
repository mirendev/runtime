package commands

import (
	"path/filepath"
	"sync"

	"miren.dev/runtime/appconfig"
)

// localAppConfig is the .miren/app.toml found by walking up from the working
// directory, loaded at most once per CLI invocation. Alias expansion, per-app
// cluster pinning, and app-name defaulting all want it, so the load is
// memoized and a broken file is reported by whichever caller hits it first.
// Package state rather than a Context field because alias expansion runs
// before any Context exists. reported is a plain bool: flag validation and
// the command body run on one goroutine, and only load is reached from
// anywhere else.
type localAppConfig struct {
	once     sync.Once
	config   *appconfig.AppConfig
	path     string
	err      error
	reported bool
}

var local localAppConfig

func (l *localAppConfig) load() (*appconfig.AppConfig, string, error) {
	l.once.Do(func() {
		l.config, l.path, l.err = appconfig.LoadAppConfigWithPath()
	})
	return l.config, l.path, l.err
}

// warn prints the load error as a warning unless it has already been
// reported, either as a warning or as a command's own error. A successful
// load is a no-op.
func (l *localAppConfig) warn() {
	if _, _, err := l.load(); err == nil || l.reported {
		return
	}
	l.reported = true
	printConfigWarning(l.err)
}

// fatal records that a command is failing with the load error itself, so
// the file is not warned about again. When path is given it must name the
// same file; an error from some other app.toml leaves a pending warning for
// this one in place.
func (l *localAppConfig) fatal(path string) {
	_, own, err := l.load()
	if err == nil {
		return
	}
	if path != "" {
		abs, err := filepath.Abs(path)
		if err != nil || abs != own {
			return
		}
	}
	l.reported = true
}

// LoadLocalAppConfig returns the app config from the working directory, or
// (nil, nil) when there isn't one. A load error is returned without being
// printed; callers that carry on regardless should call WarnLocalAppConfig.
func LoadLocalAppConfig() (*appconfig.AppConfig, error) {
	ac, _, err := local.load()
	return ac, err
}

// WarnLocalAppConfig prints a warning for a broken local app config, once per
// invocation. Safe to call whether or not the load failed.
func WarnLocalAppConfig() {
	local.warn()
}

// loadLocalAppConfigOrWarn is the common shape for commands that would like
// the local config but can do without it: a broken file is reported once and
// treated as absent.
func loadLocalAppConfigOrWarn() *appconfig.AppConfig {
	ac, err := LoadLocalAppConfig()
	if err != nil {
		WarnLocalAppConfig()
		return nil
	}
	return ac
}

// resetLocalAppConfig forgets the memoized load so a test can point the
// working directory somewhere else.
func resetLocalAppConfig() {
	local = localAppConfig{}
}
