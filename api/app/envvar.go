package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	coreutil "miren.dev/runtime/api/core"
	"miren.dev/runtime/api/core/core_v1alpha"
	"miren.dev/runtime/api/entityserver"
	"miren.dev/runtime/pkg/cond"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/entity/types"
	"miren.dev/runtime/pkg/idgen"
)

// EnvVarInput represents an env var to set.
type EnvVarInput struct {
	Key       string
	Value     string
	Sensitive bool
}

// MutateResult holds the result of an env var mutation.
type MutateResult struct {
	AppVersion *core_v1alpha.AppVersion
	VersionID  string
}

// DeleteResult extends MutateResult with source tracking.
type DeleteResult struct {
	MutateResult
	DeletedSources []string
}

// envMutateMaxAttempts is a live-lock backstop for the optimistic-concurrency
// retry loop shared by SetEnvVars and DeleteEnvVars — it is not an expected
// limit. Each conflict just means another writer swung the app's active version
// first; there are only ever a handful of concurrent env/config writers per app,
// so it is set high enough that genuine contention never spuriously fails a
// write. On exhaustion the caller gets an error rather than looping forever.
const envMutateMaxAttempts = 100

// SetEnvVars resolves the config from baseVersion (or current active if nil),
// merges env vars (with service scope), creates a new ConfigVersion + AppVersion,
// and activates it. Returns the newly created AppVersion and its version string.
//
// The read-merge-write is retried under optimistic concurrency control so two
// parallel env writes (or the addon controller injecting addon vars) cannot
// silently clobber each other's active-version swing — see createNewVersion.
//
// The retry re-reads the app each attempt, so the CAS always lands on the current
// revision. When baseVersion is nil (every caller today) it also re-resolves the
// current active config, so the merge composes onto a concurrent winner's version.
// When a baseVersion is pinned, each attempt re-derives from that fixed version by
// design — the caller asked to base off it — so the retry re-applies the same base
// rather than composing onto the winner.
func SetEnvVars(ctx context.Context, ec *entityserver.Client, appName string,
	baseVersion *core_v1alpha.AppVersion, vars []EnvVarInput, service string) (*MutateResult, error) {

	for _, v := range vars {
		if strings.HasPrefix(v.Key, "MIREN_") {
			return nil, fmt.Errorf("cannot set MIREN_ environment variables")
		}
	}

	for attempt := 0; attempt < envMutateMaxAttempts; attempt++ {
		appVer, spec, appRec, appRev, err := resolveBaseVersion(ctx, ec, appName, baseVersion)
		if err != nil {
			return nil, err
		}

		if err := mergeIntoSpec(spec, vars, service); err != nil {
			return nil, err
		}

		result, err := createNewVersion(ctx, ec, appName, appVer, spec, appRec, appRev)
		if err == nil {
			return result, nil
		}
		if !errors.Is(err, cond.ErrConflict{}) {
			return nil, err
		}
		// Lost the CAS race with a concurrent active-version swing; re-resolve
		// against the winner's version and retry so neither change is dropped.
	}

	return nil, fmt.Errorf("failed to set env vars on app %q after %d attempts due to concurrent writes", appName, envMutateMaxAttempts)
}

// DeleteEnvVars resolves the config from baseVersion (or current active if nil),
// removes the specified keys, creates a new ConfigVersion + AppVersion, and
// activates it. Returns the new version plus the source of each deleted var.
//
// Like SetEnvVars, the read-merge-write is retried under optimistic concurrency
// control so a concurrent active-version swing cannot silently clobber the delete.
// The same baseVersion caveat applies: the retry composes onto a concurrent winner
// only when baseVersion is nil; a pinned baseVersion is re-applied as-is.
func DeleteEnvVars(ctx context.Context, ec *entityserver.Client, appName string,
	baseVersion *core_v1alpha.AppVersion, keys []string, service string) (*DeleteResult, error) {

	for attempt := 0; attempt < envMutateMaxAttempts; attempt++ {
		appVer, spec, appRec, appRev, err := resolveBaseVersion(ctx, ec, appName, baseVersion)
		if err != nil {
			return nil, err
		}

		if appRec.ActiveVersion == "" {
			return nil, fmt.Errorf("app has no active version")
		}

		var deletedSources []string

		for _, key := range keys {
			if service == "" {
				found := false
				newVars := make([]core_v1alpha.ConfigSpecVariables, 0, len(spec.Variables))
				for _, v := range spec.Variables {
					if v.Key == key {
						found = true
						deletedSources = append(deletedSources, v.Source)
						continue
					}
					newVars = append(newVars, v)
				}
				if !found {
					return nil, fmt.Errorf("environment variable %q not found", key)
				}
				spec.Variables = newVars
			} else {
				svcFound := false
				for i := range spec.Services {
					if spec.Services[i].Name == service {
						svcFound = true
						envFound := false
						newEnvs := make([]core_v1alpha.ConfigSpecServicesEnv, 0, len(spec.Services[i].Env))
						for _, e := range spec.Services[i].Env {
							if e.Key == key {
								envFound = true
								deletedSources = append(deletedSources, e.Source)
								continue
							}
							newEnvs = append(newEnvs, e)
						}
						if !envFound {
							return nil, fmt.Errorf("environment variable %q not found in service %q", key, service)
						}
						spec.Services[i].Env = newEnvs
						break
					}
				}
				if !svcFound {
					return nil, fmt.Errorf("service %q not found", service)
				}
			}
		}

		result, err := createNewVersion(ctx, ec, appName, appVer, spec, appRec, appRev)
		if err == nil {
			return &DeleteResult{
				MutateResult:   *result,
				DeletedSources: deletedSources,
			}, nil
		}
		if !errors.Is(err, cond.ErrConflict{}) {
			return nil, err
		}
		// Lost the CAS race; re-resolve and retry.
	}

	return nil, fmt.Errorf("failed to delete env vars on app %q after %d attempts due to concurrent writes", appName, envMutateMaxAttempts)
}

// resolveBaseVersion loads the app, resolves the base version and config spec.
// If baseVersion is nil, the current active version is used. It also returns the
// app entity's revision at read time, so the caller can swing active_version
// under optimistic concurrency control (see createNewVersion).
func resolveBaseVersion(ctx context.Context, ec *entityserver.Client, appName string,
	baseVersion *core_v1alpha.AppVersion) (*core_v1alpha.AppVersion, *core_v1alpha.ConfigSpec, *core_v1alpha.App, int64, error) {

	appEnt, err := ec.EAC().Get(ctx, "app/"+appName)
	if err != nil {
		return nil, nil, nil, 0, err
	}

	var appRec core_v1alpha.App
	appRec.Decode(appEnt.Entity().Entity())
	appRev := appEnt.Entity().Revision()

	var appVer core_v1alpha.AppVersion
	var spec core_v1alpha.ConfigSpec

	if baseVersion != nil {
		appVer = *baseVersion
		resolvedCfg, err := coreutil.ResolveConfig(ctx, ec.EAC(), &appVer)
		if err != nil {
			return nil, nil, nil, 0, fmt.Errorf("failed to resolve config: %w", err)
		}
		spec = *resolvedCfg
	} else if appRec.ActiveVersion != "" {
		err = ec.GetById(ctx, appRec.ActiveVersion, &appVer)
		if err != nil {
			return nil, nil, nil, 0, err
		}
		resolvedCfg, err := coreutil.ResolveConfig(ctx, ec.EAC(), &appVer)
		if err != nil {
			return nil, nil, nil, 0, fmt.Errorf("failed to resolve config: %w", err)
		}
		spec = *resolvedCfg
	} else {
		appVer.App = appRec.ID
	}

	return &appVer, &spec, &appRec, appRev, nil
}

// SetInitialEnvVars stages env vars on an app's initial ConfigVersion,
// before any AppVersion exists. Used during `miren init` to record secrets
// and other config that the first deploy will pick up. The app's
// initial_config field is updated to point at the new ConfigVersion; no
// AppVersion is created and active_version is left untouched.
//
// Subsequent calls merge with the existing initial config rather than
// replacing it, mirroring the SetEnvVars behaviour for active versions.
//
// The app update uses optimistic concurrency control via Replace+revision so
// that two parallel SetInitialEnvVars calls (or a deploy slipping in between
// the read and the write) cannot silently drop staged vars.
func SetInitialEnvVars(ctx context.Context, ec *entityserver.Client, appName string,
	vars []EnvVarInput, service string) (entity.Id, error) {

	for _, v := range vars {
		if strings.HasPrefix(v.Key, "MIREN_") {
			return "", fmt.Errorf("cannot set MIREN_ environment variables")
		}
	}

	const maxAttempts = 5
	for attempt := 0; attempt < maxAttempts; attempt++ {
		appEnt, err := ec.EAC().Get(ctx, "app/"+appName)
		if err != nil {
			return "", err
		}

		var appRec core_v1alpha.App
		appRec.Decode(appEnt.Entity().Entity())
		appRec.ID = entity.Id(appEnt.Entity().Id())

		if appRec.ActiveVersion != "" {
			return "", fmt.Errorf("app %q already has a deployed version; use SetEnvVars instead", appName)
		}

		var spec core_v1alpha.ConfigSpec
		if appRec.InitialConfig != "" {
			var prev core_v1alpha.ConfigVersion
			if err := ec.GetById(ctx, appRec.InitialConfig, &prev); err != nil {
				return "", fmt.Errorf("failed to load existing initial config: %w", err)
			}
			spec = prev.Spec
		}

		if err := mergeIntoSpec(&spec, vars, service); err != nil {
			return "", err
		}

		configVer := &core_v1alpha.ConfigVersion{
			App:  appRec.ID,
			Spec: spec,
		}
		cvName := appName + "-initial-" + idgen.Gen("c")
		cvid, err := ec.Create(ctx, cvName, configVer)
		if err != nil {
			return "", fmt.Errorf("error creating initial config version: %w", err)
		}

		appRec.InitialConfig = cvid

		// Build full attrs (metadata + identity + decoded fields) for Replace.
		var meta core_v1alpha.Metadata
		meta.Decode(appEnt.Entity().Entity())
		fullAttrs := entity.New(
			meta.Encode,
			appRec.Encode,
			entity.DBId, appRec.ID,
			entity.Ident, types.Keyword("app/"+appName),
		).Attrs()

		_, err = ec.EAC().Replace(ctx, fullAttrs, appEnt.Entity().Revision())
		if err == nil {
			return cvid, nil
		}
		if !errors.Is(err, cond.ErrConflict{}) {
			return "", fmt.Errorf("error updating app initial_config: %w", err)
		}
		// Lost the CAS race; retry. The just-created ConfigVersion is
		// orphaned but immutable, so this is correctness-safe.
	}

	return "", fmt.Errorf("failed to update app %q initial_config after %d attempts due to concurrent writes", appName, maxAttempts)
}

// mergeIntoSpec applies the env var inputs onto the spec in place. service
// scopes the merge to a named service (creating its entry if needed) when
// non-empty, otherwise the vars are merged onto the global Variables list.
func mergeIntoSpec(spec *core_v1alpha.ConfigSpec, vars []EnvVarInput, service string) error {
	for _, v := range vars {
		if service == "" {
			found := false
			for i, ev := range spec.Variables {
				if ev.Key == v.Key {
					spec.Variables[i].Value = v.Value
					spec.Variables[i].Sensitive = v.Sensitive
					spec.Variables[i].Source = "manual"
					found = true
					break
				}
			}
			if !found {
				spec.Variables = append(spec.Variables, core_v1alpha.ConfigSpecVariables{
					Key:       v.Key,
					Value:     v.Value,
					Sensitive: v.Sensitive,
					Source:    "manual",
				})
			}
			continue
		}

		svcFound := false
		for i := range spec.Services {
			if spec.Services[i].Name == service {
				svcFound = true
				envFound := false
				for j, e := range spec.Services[i].Env {
					if e.Key == v.Key {
						spec.Services[i].Env[j].Value = v.Value
						spec.Services[i].Env[j].Sensitive = v.Sensitive
						spec.Services[i].Env[j].Source = "manual"
						envFound = true
						break
					}
				}
				if !envFound {
					spec.Services[i].Env = append(spec.Services[i].Env, core_v1alpha.ConfigSpecServicesEnv{
						Key:       v.Key,
						Value:     v.Value,
						Sensitive: v.Sensitive,
						Source:    "manual",
					})
				}
				break
			}
		}
		if !svcFound {
			// Fresh app or first var for this service — append a new entry
			// rather than rejecting. SetEnvVars staging predates any deploy,
			// so service entries are only filled in by the build step.
			spec.Services = append(spec.Services, core_v1alpha.ConfigSpecServices{
				Name: service,
				Env: []core_v1alpha.ConfigSpecServicesEnv{{
					Key:       v.Key,
					Value:     v.Value,
					Sensitive: v.Sensitive,
					Source:    "manual",
				}},
			})
		}
	}
	return nil
}

// createNewVersion creates a ConfigVersion + AppVersion from the mutated spec and
// activates it. The active_version pointer is swung with a CAS on appRev (the app
// revision captured when the base config was resolved), so a concurrent writer
// that swings active_version first — another env mutation, or the addon
// controller injecting addon vars — is detected and the caller retries against
// the winner's version instead of silently clobbering it. Returns cond.ErrConflict
// on a lost race.
func createNewVersion(ctx context.Context, ec *entityserver.Client, appName string,
	appVer *core_v1alpha.AppVersion, spec *core_v1alpha.ConfigSpec, appRec *core_v1alpha.App, appRev int64) (*MutateResult, error) {

	appVer.Version = appName + "-" + idgen.Gen("v")

	configVer := &core_v1alpha.ConfigVersion{
		App:  appVer.App,
		Spec: *spec,
	}
	cvName := appVer.Version + "-cfg"
	cvid, err := ec.Create(ctx, cvName, configVer)
	if err != nil {
		return nil, fmt.Errorf("error creating config version: %w", err)
	}
	appVer.ConfigVersion = cvid
	appVer.Config = core_v1alpha.Config{}

	avid, err := ec.Create(ctx, appVer.Version, appVer)
	if err != nil {
		return nil, err
	}

	if err := ec.Patch(ctx, appRec.ID, appRev,
		entity.Ref(core_v1alpha.AppActiveVersionId, avid),
	); err != nil {
		if errors.Is(err, cond.ErrConflict{}) {
			// This attempt lost the race, so the version pair we just minted was
			// never activated. Best-effort delete it (AppVersion first, so it
			// never dangles past its ConfigVersion) rather than leaving one pair
			// per retry for the version GC to reap later.
			if delErr := ec.Delete(ctx, avid); delErr != nil {
				slog.Warn("failed to delete superseded app version after conflict",
					"app", appRec.ID, "version", avid, "error", delErr)
			}
			if delErr := ec.Delete(ctx, cvid); delErr != nil {
				slog.Warn("failed to delete superseded config version after conflict",
					"app", appRec.ID, "config_version", cvid, "error", delErr)
			}
		}
		return nil, err
	}

	return &MutateResult{
		AppVersion: appVer,
		VersionID:  appVer.Version,
	}, nil
}
