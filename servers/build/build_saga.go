package build

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/tonistiigi/fsutil"

	"miren.dev/runtime/api/build/build_v1alpha"
	"miren.dev/runtime/api/core/core_v1alpha"
	"miren.dev/runtime/appconfig"
	"miren.dev/runtime/pkg/deploylifecycle"
	"miren.dev/runtime/pkg/entity"
	ephemeralx "miren.dev/runtime/pkg/ephemeral"
	"miren.dev/runtime/pkg/saga"
	"miren.dev/runtime/pkg/stackbuild"
)

// Saga definition + action names. Keeping them as constants makes them
// easy to search for, and lets recovery logs reference stable action
// identifiers across restarts.
const (
	sagaBuildFromTar      = "build-from-tar"
	actionReceiveTar      = "receive-tar"
	actionLoadSource      = "load-source"
	actionGetNextVer      = "get-next-version"
	actionBuildImage      = "build-image"
	actionPrepareConfig   = "prepare-config"
	actionHandleEphemera  = "handle-ephemeral"
	actionCreateConfigVer = "create-config-version"
	actionCreateVersion   = "create-version"
	actionProvisionAddons = "provision-addons"
	actionRunDeployTasks  = "run-deploy-tasks"
	actionSetActiveVer    = "set-active-version"
	actionFinalize        = "finalize"
	actionBeginDeploy     = "begin-deployment"
	actionRecordVersion   = "record-app-version"
	actionActivateDeploy  = "activate-deployment"
)

// buildSagaDeps holds the collaborators injected into the saga context.
// Actions retrieve it via saga.Get[*buildSagaDeps](ctx) and call through to
// the inner Builder for real operations (entity writes, buildkit calls,
// log writes, etc.), the StreamRegistry for tar staging, or the
// StatusRegistry to emit live progress/log/error updates to the deploy
// CLI for the duration of one saga execution.
type buildSagaDeps struct {
	builder  *Builder
	streams  *StreamRegistry
	statuses *StatusRegistry
}

// receiveTarIn carries the initial inputs the entry point seeds the saga
// with. AppName is mostly used for logging and cache writes; the StreamID
// is the handle that lets us pull bytes out of StreamRegistry.
type receiveTarIn struct {
	AppName  string `json:"app_name" saga:"app_name"`
	StreamID string `json:"stream_id" saga:"stream_id"`
}

// receiveTarOut publishes the staged source directory so downstream
// actions can read app.toml, run buildkit against it, etc.
type receiveTarOut struct {
	SourceDir string `json:"source_dir" saga:"source_dir"`
}

// receiveTar stages the incoming tar stream to disk and (best-effort)
// updates the source code cache so the next delta upload can skip
// unchanged files. The cache write matches existing BuildFromTar
// behavior — failure logs a warning rather than failing the build,
// since cache is a perf optimization, not correctness.
func receiveTar(ctx context.Context, in receiveTarIn) (receiveTarOut, error) {
	deps := saga.Get[*buildSagaDeps](ctx)
	status := deps.statuses.SenderFor(in.StreamID)

	status.SendMessage("Reading application data")

	path, err := deps.streams.Stage(in.StreamID)
	if err != nil {
		status.SendError("Error untaring data: %v", err)
		return receiveTarOut{}, fmt.Errorf("staging tar stream %s: %w", in.StreamID, err)
	}

	if deps.builder.DataPath != "" {
		cache := &sourceCache{
			dataPath: deps.builder.DataPath,
			log:      deps.builder.Log,
			locks:    deps.builder.cacheLocks,
		}
		if err := cache.saveSourceImage(in.AppName, path); err != nil {
			deps.builder.Log.Warn("failed to save source code cache", "app", in.AppName, "error", err)
		}
	}

	status.SendMessage("Preparing deployment")
	return receiveTarOut{SourceDir: path}, nil
}

func undoReceiveTar(ctx context.Context, in receiveTarIn, _ receiveTarOut) error {
	deps := saga.Get[*buildSagaDeps](ctx)
	return deps.streams.Cleanup(in.StreamID)
}

// loadSource reads the staged tree to produce the inputs every later
// action needs: the parsed app.toml (if present), the detected/declared
// BuildStack (with stack detection actually performed when stack=auto so
// we fail fast before launching buildkit), and the Procfile services.
//
// Pure file IO + parsing, no entity writes, so the undo is a no-op.
// Recovery just re-runs Execute against the same staged dir.

type loadSourceIn struct {
	AppName   string `json:"app_name" saga:"app_name"`
	SourceDir string `json:"source_dir" saga:"source_dir"`
	// StreamID is consumed so a config error found here can be reported to the
	// deploying client, which is the whole value of failing fast.
	StreamID string `json:"stream_id" saga:"stream_id"`
}

type loadSourceOut struct {
	AppConfig        *appconfig.AppConfig `json:"app_config,omitempty" saga:"app_config"`
	BuildStack       BuildStack           `json:"build_stack" saga:"build_stack"`
	ProcfileServices map[string]string    `json:"procfile_services,omitempty" saga:"procfile_services"`
}

func loadSource(ctx context.Context, in loadSourceIn) (loadSourceOut, error) {
	deps := saga.Get[*buildSagaDeps](ctx)
	b := deps.builder

	tr, err := fsutil.NewFS(in.SourceDir)
	if err != nil {
		return loadSourceOut{}, fmt.Errorf("opening source dir %s: %w", in.SourceDir, err)
	}

	ac, err := b.loadAppConfig(tr)
	if err != nil {
		return loadSourceOut{}, fmt.Errorf("loading app config: %w", err)
	}

	stack, err := b.detectBuildStack(in.SourceDir, ac, in.AppName, tr)
	if err != nil {
		return loadSourceOut{}, err
	}

	procfile, err := b.readProcFile(tr)
	if err != nil {
		return loadSourceOut{}, fmt.Errorf("reading procfile: %w", err)
	}

	// Ask about web intent here rather than in prepareConfig. Everything it
	// reads is source-derived and already in hand, so this is the fail-fast this
	// action exists for -- and prepareConfig is too late anyway, since
	// buildVersionConfig stages a synthesized web service into ac.Services and
	// the question can no longer be asked afterwards.
	if err := validateWebIntent(ac, procfile); err != nil {
		deps.statuses.SenderFor(in.StreamID).SendError("%s\n\nSee https://miren.md/app-toml#tasks", err)
		return loadSourceOut{}, err
	}

	return loadSourceOut{
		AppConfig:        ac,
		BuildStack:       stack,
		ProcfileServices: procfile,
	}, nil
}

func undoLoadSource(_ context.Context, _ loadSourceIn, _ loadSourceOut) error {
	return nil
}

// getNextVersion allocates a fresh version id + artifact suffix for the
// build and (idempotently) creates the App entity if this is the very
// first deploy. Mirrors the pre-saga Builder.nextVersion semantics:
// existing App is left alone, current config is loaded for env-var
// carryover, new ids are generated locally and only persisted by later
// actions (createConfigVersion / createAppVersion).
//
// The undo is a no-op. The generated ids are not durable state worth
// freeing, and the App entity might already exist (created by a prior
// deploy) or be needed by other concurrent operations — deleting it on
// build failure is the wrong instinct.

type getNextVersionIn struct {
	AppName string `json:"app_name" saga:"app_name"`
}

type getNextVersionOut struct {
	AppID          string `json:"app_id" saga:"app_id"`
	VersionName    string `json:"version_name" saga:"version_name"`
	ArtifactSuffix string `json:"artifact_suffix" saga:"artifact_suffix"`
	ImageURL       string `json:"image_url" saga:"image_url"`
	AdminToken     string `json:"admin_token" saga:"admin_token"`
	ExistingConfig string `json:"existing_config_json" saga:"existing_config_json"`
}

func getNextVersion(ctx context.Context, in getNextVersionIn) (getNextVersionOut, error) {
	deps := saga.Get[*buildSagaDeps](ctx)

	appRec, mrv, existing, art, err := deps.builder.nextVersion(ctx, in.AppName)
	if err != nil {
		return getNextVersionOut{}, fmt.Errorf("allocating next version for %s: %w", in.AppName, err)
	}

	existingJSON, err := marshalConfigSpec(existing)
	if err != nil {
		return getNextVersionOut{}, fmt.Errorf("serializing existing config: %w", err)
	}

	return getNextVersionOut{
		AppID:          string(appRec.ID),
		VersionName:    mrv.Version,
		ArtifactSuffix: art,
		ImageURL:       mrv.ImageUrl,
		AdminToken:     mrv.AdminToken,
		ExistingConfig: existingJSON,
	}, nil
}

func undoGetNextVersion(_ context.Context, _ getNextVersionIn, _ getNextVersionOut) error {
	return nil
}

// prepareConfig assembles the final ConfigSpec for the new version by
// merging build outputs, app.toml, Procfile, and existing app config,
// then runs every blocking validation (services exist, required vars
// have values, node ports are free, disk references resolve). Pure
// computation + entity reads — no side effects, no undo needed.
//
// Validation failures surface as the saga error and a user-facing
// status update. The pre-saga path called validateNodePorts and
// validateDiskConfigs separately; bundling them with the rest in one
// action keeps the saga DAG simple and matches what the user
// experiences as one logical "config prep" step.

type prepareConfigIn struct {
	AppName          string                               `json:"app_name" saga:"app_name"`
	StreamID         string                               `json:"stream_id" saga:"stream_id"`
	AppID            string                               `json:"app_id" saga:"app_id"`
	BuildResult      *BuildResult                         `json:"build_result,omitempty" saga:"build_result,optional"`
	AppConfig        *appconfig.AppConfig                 `json:"app_config,omitempty" saga:"app_config,optional"`
	ProcfileServices map[string]string                    `json:"procfile_services,omitempty" saga:"procfile_services,optional"`
	ExistingConfig   string                               `json:"existing_config_json" saga:"existing_config_json"`
	CLIEnvVars       []*build_v1alpha.EnvironmentVariable `json:"cli_env_vars,omitempty" saga:"cli_env_vars,optional"`
}

type prepareConfigOut struct {
	ConfigSpec string `json:"config_spec_json" saga:"config_spec_json"`
}

func prepareConfig(ctx context.Context, in prepareConfigIn) (prepareConfigOut, error) {
	deps := saga.Get[*buildSagaDeps](ctx)
	b := deps.builder
	status := deps.statuses.SenderFor(in.StreamID)

	existing, err := unmarshalConfigSpec(in.ExistingConfig)
	if err != nil {
		return prepareConfigOut{}, fmt.Errorf("deserializing existing config: %w", err)
	}

	spec := buildVersionConfig(ConfigInputs{
		BuildResult:      in.BuildResult,
		AppConfig:        in.AppConfig,
		ProcfileServices: in.ProcfileServices,
		ExistingConfig:   existing,
		CliEnvVars:       in.CLIEnvVars,
		Log:              b.Log,
	})

	// Validate the app.toml workload_role here, before the version is created or
	// activated, so a bad role fails the deploy without leaving a new version
	// live. The write happens later, at activation.
	if err := validateWorkloadRole(in.AppConfig); err != nil {
		status.SendError("%s", err)
		return prepareConfigOut{}, err
	}
	if err := validateWorkloadsExist(spec); err != nil {
		status.SendError("%s. See https://miren.md/services", err)
		return prepareConfigOut{}, err
	}
	if err := validateRequiredVars(spec); err != nil {
		status.SendError("%s", err)
		return prepareConfigOut{}, err
	}
	if err := validateNodePorts(ctx, b.ec.EAC(), entity.Id(in.AppID), spec); err != nil {
		status.SendError("Deploy failed: %v", err)
		return prepareConfigOut{}, err
	}
	if err := validateDiskConfigs(ctx, b.ec.EAC(), spec); err != nil {
		status.SendError("Deploy failed: %v", err)
		return prepareConfigOut{}, err
	}

	specJSON, err := json.Marshal(spec)
	if err != nil {
		return prepareConfigOut{}, fmt.Errorf("serializing config spec: %w", err)
	}
	return prepareConfigOut{ConfigSpec: string(specJSON)}, nil
}

func undoPrepareConfig(_ context.Context, _ prepareConfigIn, _ prepareConfigOut) error {
	return nil
}

// handleEphemeral covers the ephemeral-version-specific bookkeeping:
// validating the label, parsing the TTL, deleting any existing version
// with the same label (replace-on-same-label), and enforcing the per-
// app ephemeral version limit. No-op for non-ephemeral deploys.
//
// The undo is intentionally a no-op even though replace-existing
// deletes versions. Those versions were going to be replaced by the
// new build, and an aborted build doesn't make the user want them
// back — they want to retry with new code. Re-creating deleted
// entities would also race with concurrent reconciliation.

type handleEphemeralIn struct {
	AppName        string `json:"app_name" saga:"app_name"`
	StreamID       string `json:"stream_id" saga:"stream_id"`
	AppID          string `json:"app_id" saga:"app_id"`
	EphemeralLabel string `json:"ephemeral_label,omitempty" saga:"ephemeral_label,optional"`
	EphemeralTTL   string `json:"ephemeral_ttl,omitempty" saga:"ephemeral_ttl,optional"`
}

type handleEphemeralOut struct {
	ExpiresAt string `json:"ephemeral_expires_at,omitempty" saga:"ephemeral_expires_at"`
}

func handleEphemeral(ctx context.Context, in handleEphemeralIn) (handleEphemeralOut, error) {
	if in.EphemeralLabel == "" {
		return handleEphemeralOut{}, nil
	}

	deps := saga.Get[*buildSagaDeps](ctx)
	b := deps.builder
	status := deps.statuses.SenderFor(in.StreamID)

	if err := ephemeralx.ValidateLabel(in.EphemeralLabel); err != nil {
		status.SendError("invalid ephemeral label: %v", err)
		return handleEphemeralOut{}, fmt.Errorf("invalid ephemeral label: %w", err)
	}

	ttl := in.EphemeralTTL
	if ttl == "" {
		ttl = "24h"
	}
	ttlDuration, err := time.ParseDuration(ttl)
	if err != nil {
		status.SendError("invalid ephemeral TTL %q: %v", ttl, err)
		return handleEphemeralOut{}, fmt.Errorf("invalid ephemeral TTL %q: %w", ttl, err)
	}
	if ttlDuration <= 0 {
		status.SendError("invalid ephemeral TTL %q: must be greater than 0", ttl)
		return handleEphemeralOut{}, fmt.Errorf("invalid ephemeral TTL %q: must be greater than 0", ttl)
	}

	if err := ephemeralx.ReplaceExisting(ctx, b.ec.EAC(), entity.Id(in.AppID), in.EphemeralLabel, b.Log); err != nil {
		return handleEphemeralOut{}, fmt.Errorf("failed to replace existing ephemeral version %q: %w", in.EphemeralLabel, err)
	}
	if err := ephemeralx.EnforceLimit(ctx, b.ec.EAC(), entity.Id(in.AppID), ephemeralx.DefaultMaxEphemeral, b.Log); err != nil {
		return handleEphemeralOut{}, fmt.Errorf("failed to enforce ephemeral limit: %w", err)
	}

	expiresAt := time.Now().Add(ttlDuration).Format(time.RFC3339)
	return handleEphemeralOut{ExpiresAt: expiresAt}, nil
}

func undoHandleEphemeral(_ context.Context, _ handleEphemeralIn, _ handleEphemeralOut) error {
	return nil
}

// createConfigVersion creates the ConfigVersion entity that holds the
// new app's full ConfigSpec. AppVersion references it by ID, so it
// must exist before createAppVersion runs. Failures here surface as
// the saga error; recovery would retry creation with the same name
// and a deterministic-enough ConfigSpec to be idempotent in practice,
// though the entity store's create-if-missing semantics carry the
// guarantee.

type createConfigVersionIn struct {
	AppID       string `json:"app_id" saga:"app_id"`
	VersionName string `json:"version_name" saga:"version_name"`
	ConfigSpec  string `json:"config_spec_json" saga:"config_spec_json"`
}

type createConfigVersionOut struct {
	ConfigVersionID string `json:"config_version_id" saga:"config_version_id"`
}

func createConfigVersion(ctx context.Context, in createConfigVersionIn) (createConfigVersionOut, error) {
	deps := saga.Get[*buildSagaDeps](ctx)
	b := deps.builder

	spec, err := unmarshalConfigSpec(in.ConfigSpec)
	if err != nil {
		return createConfigVersionOut{}, fmt.Errorf("deserializing config spec: %w", err)
	}

	// Freeze every backend-sourced variable to the version it resolves to right
	// now, so this ConfigVersion records exactly which secret it shipped with.
	// Pinning happens here rather than upstream in the saga because the action
	// that stores the spec must be the one that records the pin: whichever
	// version this attempt resolves is the version the config it writes claims.
	if err := b.pinSecrets(ctx, &spec); err != nil {
		return createConfigVersionOut{}, err
	}

	cv := &core_v1alpha.ConfigVersion{
		App:  entity.Id(in.AppID),
		Spec: spec,
	}
	name := in.VersionName + "-cfg"
	id, err := b.ec.Create(ctx, name, cv)
	if err != nil {
		return createConfigVersionOut{}, fmt.Errorf("creating config version %s: %w", name, err)
	}
	return createConfigVersionOut{ConfigVersionID: string(id)}, nil
}

func undoCreateConfigVersion(ctx context.Context, _ createConfigVersionIn, out createConfigVersionOut) error {
	if out.ConfigVersionID == "" {
		return nil
	}
	deps := saga.Get[*buildSagaDeps](ctx)
	if err := deps.builder.ec.Delete(ctx, entity.Id(out.ConfigVersionID)); err != nil {
		return fmt.Errorf("deleting config version %s: %w", out.ConfigVersionID, err)
	}
	return nil
}

// createVersion creates the AppVersion entity that pins together the
// artifact, image URL, config version, and ephemeral metadata. This is
// the durable "the build succeeded" record — once it exists, the
// downstream activate / addon-provisioning steps have something to
// reference. Failures compensate by deleting it.

type createVersionIn struct {
	AppID              string `json:"app_id" saga:"app_id"`
	VersionName        string `json:"version_name" saga:"version_name"`
	FinalImageURL      string `json:"final_image_url" saga:"final_image_url"`
	ArtifactID         string `json:"artifact_id" saga:"artifact_id,optional"`
	AdminToken         string `json:"admin_token" saga:"admin_token"`
	ConfigVersionID    string `json:"config_version_id" saga:"config_version_id"`
	EphemeralLabel     string `json:"ephemeral_label,omitempty" saga:"ephemeral_label,optional"`
	EphemeralTTL       string `json:"ephemeral_ttl,omitempty" saga:"ephemeral_ttl,optional"`
	EphemeralExpiresAt string `json:"ephemeral_expires_at,omitempty" saga:"ephemeral_expires_at,optional"`
}

type createVersionOut struct {
	AppVersionID string `json:"app_version_id" saga:"app_version_id"`
}

func createVersion(ctx context.Context, in createVersionIn) (createVersionOut, error) {
	deps := saga.Get[*buildSagaDeps](ctx)
	b := deps.builder

	av := &core_v1alpha.AppVersion{
		App:           entity.Id(in.AppID),
		Version:       in.VersionName,
		ImageUrl:      in.FinalImageURL,
		AdminToken:    in.AdminToken,
		Artifact:      entity.Id(in.ArtifactID),
		ConfigVersion: entity.Id(in.ConfigVersionID),
		Config:        core_v1alpha.Config{},
	}
	if in.EphemeralLabel != "" {
		av.EphemeralLabel = in.EphemeralLabel
		av.EphemeralTtl = in.EphemeralTTL
		if in.EphemeralExpiresAt != "" {
			if t, err := time.Parse(time.RFC3339, in.EphemeralExpiresAt); err == nil {
				av.EphemeralExpiresAt = t
			}
		}
	}

	id, err := b.ec.Create(ctx, in.VersionName, av)
	if err != nil {
		return createVersionOut{}, fmt.Errorf("creating app version %s: %w", in.VersionName, err)
	}
	return createVersionOut{AppVersionID: string(id)}, nil
}

func undoCreateVersion(ctx context.Context, _ createVersionIn, out createVersionOut) error {
	if out.AppVersionID == "" {
		return nil
	}
	deps := saga.Get[*buildSagaDeps](ctx)
	if err := deps.builder.ec.Delete(ctx, entity.Id(out.AppVersionID)); err != nil {
		return fmt.Errorf("deleting app version %s: %w", out.AppVersionID, err)
	}
	return nil
}

// provisionAddons calls into the addons client to materialize the
// addons declared in app.toml. Skipped for ephemeral deploys (which
// don't get addons) and when there's no app config at all. The undo
// is a no-op: provisionAddons handles "already attached" gracefully
// on retry, and removing addons created during a build would surprise
// users running concurrent ops against the same app.

type provisionAddonsIn struct {
	AppName        string               `json:"app_name" saga:"app_name"`
	AppConfig      *appconfig.AppConfig `json:"app_config,omitempty" saga:"app_config,optional"`
	EphemeralLabel string               `json:"ephemeral_label,omitempty" saga:"ephemeral_label,optional"`
	// AppVersionID is consumed only to anchor this action after
	// createVersion in the saga DAG; addons are scoped to the app,
	// not the version, so we don't actually need the ID at runtime.
	AppVersionID string `json:"app_version_id" saga:"app_version_id"`
}

type provisionAddonsOut struct {
	Done saga.Edge `saga:"addons_provisioned"`
}

func provisionAddons(ctx context.Context, in provisionAddonsIn) (provisionAddonsOut, error) {
	if in.EphemeralLabel != "" || in.AppConfig == nil {
		return provisionAddonsOut{}, nil
	}
	deps := saga.Get[*buildSagaDeps](ctx)
	if deps.builder.addonsClient == nil {
		return provisionAddonsOut{}, nil
	}
	if err := deps.builder.provisionAddons(ctx, in.AppName, in.AppConfig); err != nil {
		return provisionAddonsOut{}, fmt.Errorf("addon provisioning failed: %w", err)
	}
	return provisionAddonsOut{}, nil
}

func undoProvisionAddons(_ context.Context, _ provisionAddonsIn, _ provisionAddonsOut) error {
	return nil
}

// setActiveVersion makes the newly-created AppVersion the app's active
// one. Skipped for ephemeral deploys (their whole point is to coexist
// with the active version, not replace it). Records the previous
// active version so undo can restore it on failure.

// runDeployTasksIn gates the version flip on every deploy-triggered task.
//
// It consumes addons_provisioned so it runs only once the app's backing
// services exist -- a migration needs its database -- and produces an edge
// setActiveVersion consumes, which is what puts it strictly before any traffic
// moves.
type runDeployTasksIn struct {
	AppName        string    `json:"app_name" saga:"app_name"`
	StreamID       string    `json:"stream_id" saga:"stream_id"`
	AppVersionID   string    `json:"app_version_id" saga:"app_version_id"`
	ConfigSpec     string    `json:"config_spec_json" saga:"config_spec_json"`
	EphemeralLabel string    `json:"ephemeral_label,omitempty" saga:"ephemeral_label,optional"`
	AddonsReady    saga.Edge `saga:"addons_provisioned"`
}

type runDeployTasksOut struct {
	Done saga.Edge `saga:"deploy_tasks_ran"`
}

func runDeployTasks(ctx context.Context, in runDeployTasksIn) (runDeployTasksOut, error) {
	// An ephemeral preview shares the app's addons and is torn down on its own
	// schedule; running its migrations again would be at best redundant and at
	// worst a second writer against the same database.
	if in.EphemeralLabel != "" {
		return runDeployTasksOut{}, nil
	}

	deps := saga.Get[*buildSagaDeps](ctx)
	b := deps.builder
	status := deps.statuses.SenderFor(in.StreamID)

	spec, err := unmarshalConfigSpec(in.ConfigSpec)
	if err != nil {
		return runDeployTasksOut{}, fmt.Errorf("deserializing config: %w", err)
	}

	if len(deployTriggeredTasks(&spec)) == 0 {
		return runDeployTasksOut{}, nil
	}

	appRec, err := b.appClient.GetByName(ctx, in.AppName)
	if err != nil {
		return runDeployTasksOut{}, fmt.Errorf("reading app: %w", err)
	}

	err = b.runDeployTasks(ctx, in.AppName, appRec.ID, entity.Id(in.AppVersionID), &spec, status)
	if err != nil {
		status.SendError("%s", err)
		return runDeployTasksOut{}, err
	}

	return runDeployTasksOut{}, nil
}

// undoRunDeployTasks deliberately does nothing. The runs happened; a deploy
// task's effects are the app's to reverse, and the platform does not run
// down-migrations. Nothing was brought up to tear down either, since this gate
// sits ahead of the rollout.
func undoRunDeployTasks(_ context.Context, _ runDeployTasksIn, _ runDeployTasksOut) error {
	return nil
}

type setActiveVersionIn struct {
	AppName        string               `json:"app_name" saga:"app_name"`
	StreamID       string               `json:"stream_id" saga:"stream_id"`
	AppVersionID   string               `json:"app_version_id" saga:"app_version_id"`
	EphemeralLabel string               `json:"ephemeral_label,omitempty" saga:"ephemeral_label,optional"`
	AppConfig      *appconfig.AppConfig `json:"app_config,omitempty" saga:"app_config,optional"`
	AddonsReady    saga.Edge            `saga:"addons_provisioned"`
	// DeployTasksRan puts the version flip strictly after every
	// deploy-triggered task has succeeded, so a failed task means a failed
	// deploy rather than a half-promoted one.
	DeployTasksRan saga.Edge `saga:"deploy_tasks_ran"`
	// DeploymentID is empty for an untracked build. Consuming it also anchors
	// this action after begin-deployment, which is where the record is created.
	DeploymentID string `json:"deployment_id,omitempty" saga:"deployment_id,optional"`
}

type setActiveVersionOut struct {
	PreviousVersionID string `json:"previous_version_id,omitempty" saga:"previous_version_id"`
	Skipped           bool   `json:"skipped" saga:"set_active_skipped"`
}

func setActiveVersion(ctx context.Context, in setActiveVersionIn) (setActiveVersionOut, error) {
	if in.EphemeralLabel != "" {
		return setActiveVersionOut{Skipped: true}, nil
	}

	deps := saga.Get[*buildSagaDeps](ctx)
	b := deps.builder

	b.trackDeployment(in.DeploymentID, deps.statuses.SenderFor(in.StreamID)).
		setPhase(ctx, deploylifecycle.PhaseActivating)

	app, err := b.appClient.GetByName(ctx, in.AppName)
	if err != nil {
		return setActiveVersionOut{}, fmt.Errorf("looking up app %s: %w", in.AppName, err)
	}
	previous := string(app.ActiveVersion)

	if err := b.appClient.SetActiveVersion(ctx, in.AppName, in.AppVersionID); err != nil {
		return setActiveVersionOut{}, fmt.Errorf("setting active version on %s: %w", in.AppName, err)
	}

	// Apply a workload_role declared in app.toml (app-scoped only) so it takes
	// effect on this deploy. A cluster-scoped value fails the deploy.
	if err := b.applyWorkloadRole(ctx, in.AppName, in.AppConfig); err != nil {
		return setActiveVersionOut{}, err
	}
	return setActiveVersionOut{PreviousVersionID: previous}, nil
}

func undoSetActiveVersion(ctx context.Context, in setActiveVersionIn, out setActiveVersionOut) error {
	if out.Skipped {
		return nil
	}
	deps := saga.Get[*buildSagaDeps](ctx)
	if err := deps.builder.appClient.SetActiveVersion(ctx, in.AppName, out.PreviousVersionID); err != nil {
		return fmt.Errorf("restoring previous active version on %s: %w", in.AppName, err)
	}
	return nil
}

// finalize is the saga's terminal step. It writes the deployment log
// entry, runs the local-storage-migration check (non-ephemeral only),
// and tells the StreamRegistry it's safe to remove the staged source.
// Everything here is best-effort or idempotent; the undo is a no-op
// because once we got this far the build succeeded — there's nothing
// to roll back.

type finalizeIn struct {
	AppName        string    `json:"app_name" saga:"app_name"`
	StreamID       string    `json:"stream_id" saga:"stream_id"`
	AppID          string    `json:"app_id" saga:"app_id"`
	VersionName    string    `json:"version_name" saga:"version_name"`
	ArtifactID     string    `json:"artifact_id,omitempty" saga:"artifact_id,optional"`
	FinalImageURL  string    `json:"final_image_url" saga:"final_image_url"`
	ConfigSpec     string    `json:"config_spec_json" saga:"config_spec_json"`
	EphemeralLabel string    `json:"ephemeral_label,omitempty" saga:"ephemeral_label,optional"`
	ActiveReady    saga.Edge `saga:"set_active_skipped"`
}

type finalizeOut struct{}

func finalize(ctx context.Context, in finalizeIn) (finalizeOut, error) {
	deps := saga.Get[*buildSagaDeps](ctx)
	b := deps.builder

	if in.EphemeralLabel == "" {
		spec, err := unmarshalConfigSpec(in.ConfigSpec)
		if err == nil {
			status := deps.statuses.SenderFor(in.StreamID)
			b.checkLocalStorageMigration(entity.Id(in.AppID), spec, status)
			b.checkAliasedLocalDisks(spec, status)
		}
	}

	artifactName := strings.TrimPrefix(in.ArtifactID, "artifact/")
	b.logDeployment(ctx, in.AppName, in.VersionName, artifactName, in.FinalImageURL)

	if err := deps.streams.Cleanup(in.StreamID); err != nil {
		b.Log.Warn("cleanup staged tar", "stream", in.StreamID, "error", err)
	}
	return finalizeOut{}, nil
}

func undoFinalize(_ context.Context, _ finalizeIn, _ finalizeOut) error {
	return nil
}

// --- server-owned deployment record lifecycle ---
//
// Three actions mirror the plain path's tracker calls, so the record is a
// byproduct of the saga rather than something the client narrates. Ordering
// comes from data dependencies, not registration order: begin-deployment
// outputs deployment_id; record-app-version consumes deployment_id +
// app_version_id (so it lands after both begin and create-version);
// activate-deployment consumes deployment_id and the set_active_skipped edge
// (so it lands after set-active-version).
//
// All three no-op when the build is not server-tracked. A build is tracked only
// when the client sent a DeployRequest, which seeds deploy_cluster_id; without
// it deployment_id stays empty and every downstream action skips. Ephemeral
// builds are never tracked.
//
// Only begin-deployment may fail the saga. The record describes the deploy; it
// must never be able to undo one. Once begin has succeeded the later actions
// log their failures and return nil, because returning an error here would
// compensate the saga, and the compensation for set-active-version restores the
// previous app version — so a failed bookkeeping write would roll back a deploy
// that is already live. The plain path enforces the same rule in
// deployTracking.activate; these match it.
//
// For the same reason the settles run on a context detached from the saga's:
// the most likely cause of failure is the client disconnecting, which is
// exactly when the record most needs to reach a terminal state and drop the
// deploy lock.

type beginDeploymentIn struct {
	AppName   string `json:"app_name" saga:"app_name"`
	StreamID  string `json:"stream_id" saga:"stream_id"`
	ClusterID string `json:"deploy_cluster_id,omitempty" saga:"deploy_cluster_id,optional"`
	GitInfo   string `json:"deploy_git_info_json,omitempty" saga:"deploy_git_info_json,optional"`
}

type beginDeploymentOut struct {
	DeploymentID string `json:"deployment_id" saga:"deployment_id"`
}

func beginDeployment(ctx context.Context, in beginDeploymentIn) (beginDeploymentOut, error) {
	// No cluster id means the client did not ask the server to own a record.
	if in.ClusterID == "" {
		return beginDeploymentOut{}, nil
	}

	deps := saga.Get[*buildSagaDeps](ctx)
	b := deps.builder

	var gitInfo core_v1alpha.GitInfo
	if in.GitInfo != "" {
		if err := json.Unmarshal([]byte(in.GitInfo), &gitInfo); err != nil {
			b.Log.Warn("ignoring unparseable git info on deployment", "error", err)
		}
	}

	rec, err := b.deploy.Begin(ctx, deploylifecycle.BeginParams{
		AppName:   in.AppName,
		ClusterID: in.ClusterID,
		GitInfo:   gitInfo,
	})
	if err != nil {
		return beginDeploymentOut{}, fmt.Errorf("begin deployment for %s: %w", in.AppName, err)
	}

	id := string(rec.Deployment.ID)
	b.trackDeployment(id, deps.statuses.SenderFor(in.StreamID)).
		emit(string(deploylifecycle.PhasePreparing))
	return beginDeploymentOut{DeploymentID: id}, nil
}

func undoBeginDeployment(ctx context.Context, in beginDeploymentIn, out beginDeploymentOut) error {
	if out.DeploymentID == "" {
		return nil
	}
	deps := saga.Get[*buildSagaDeps](ctx)

	// Settle on a detached context: compensation often runs because the client
	// disconnected, and that must not leave the record in_progress with the lock
	// held until its TTL.
	settleCtx, cancel := settleContext(ctx)
	defer cancel()

	// FailIfUnsettled: if a later action already activated the record, leave it
	// active — the version is live, and undoSetActiveVersion handles reverting
	// that separately.
	if err := deps.builder.deploy.FailIfUnsettled(settleCtx, out.DeploymentID, "build rolled back", ""); err != nil {
		return fmt.Errorf("failing rolled-back deployment %s: %w", out.DeploymentID, err)
	}
	return nil
}

type recordAppVersionIn struct {
	DeploymentID string `json:"deployment_id,omitempty" saga:"deployment_id,optional"`
	AppVersionID string `json:"app_version_id" saga:"app_version_id"`
}

type recordAppVersionOut struct {
	Recorded saga.Edge `saga:"app_version_recorded"`
}

func recordAppVersion(ctx context.Context, in recordAppVersionIn) (recordAppVersionOut, error) {
	if in.DeploymentID == "" {
		return recordAppVersionOut{}, nil
	}
	deps := saga.Get[*buildSagaDeps](ctx)

	// Delegated so the detached context and the log-rather-than-fail policy live
	// in one place. A failure here is not fatal: activate-deployment will refuse
	// to settle a record with no version and release the lock instead.
	deps.builder.trackDeployment(in.DeploymentID, nil).setAppVersion(ctx, in.AppVersionID)
	return recordAppVersionOut{}, nil
}

func undoRecordAppVersion(_ context.Context, _ recordAppVersionIn, _ recordAppVersionOut) error {
	// Nothing to undo: begin's undo settles the record.
	return nil
}

type activateDeploymentIn struct {
	DeploymentID     string `json:"deployment_id,omitempty" saga:"deployment_id,optional"`
	SetActiveSkipped bool   `json:"set_active_skipped" saga:"set_active_skipped"`
	// Anchors this action after record-app-version.
	Recorded saga.Edge `saga:"app_version_recorded"`
}

type activateDeploymentOut struct{}

func activateDeployment(ctx context.Context, in activateDeploymentIn) (activateDeploymentOut, error) {
	// Not tracked, or an ephemeral build whose activation was skipped: nothing
	// to activate.
	if in.DeploymentID == "" || in.SetActiveSkipped {
		return activateDeploymentOut{}, nil
	}
	deps := saga.Get[*buildSagaDeps](ctx)

	// Delegated: the version is live by now, so activate detaches its context,
	// logs rather than returns, and drops the lock as a backstop. See the
	// failure-policy note at the top of this section.
	deps.builder.trackDeployment(in.DeploymentID, nil).activate(ctx)
	return activateDeploymentOut{}, nil
}

func undoActivateDeployment(_ context.Context, _ activateDeploymentIn, _ activateDeploymentOut) error {
	// The record is already active and the lock released. If a later step fails,
	// undoSetActiveVersion reverts the running version; the record is left as the
	// historical account of a deploy that did go live. No compensation here.
	return nil
}

// buildSourceResolution is the shared source-selection result for deploy and
// analyze. DetectionErr is populated only when no buildable source or fallback
// image was found: deploy turns that into an error, while analyze reports the
// source as unknown.
type buildSourceResolution struct {
	BuildStack    BuildStack
	DetectedStack stackbuild.Stack
	DetectionErr  error
}

// resolveBuildSource owns the source-precedence policy shared by deploy and
// analyze: configured Dockerfile, discovered Dockerfile.miren, explicit web
// image, detected source, then a lone non-web image fallback.
func (b *Builder) resolveBuildSource(path string, ac *appconfig.AppConfig, name string) (buildSourceResolution, error) {
	var stack BuildStack
	stack.CodeDir = path

	if ac != nil && ac.Build != nil {
		stack.OnBuild = ac.Build.OnBuild
		stack.Version = ac.Build.Version
		stack.AlpineImage = ac.Build.AlpineImage

		if ac.Build.Dockerfile != "" {
			stack.Stack = "dockerfile"
			stack.Input = ac.Build.Dockerfile
			b.Log.Info("using dockerfile from app config", "dockerfile", ac.Build.Dockerfile)
			return buildSourceResolution{BuildStack: stack}, nil
		}
	}

	// Look on disk rather than through fsutil so test/error paths don't have to
	// fabricate an fsutil.FS just to peek for a file.
	if _, err := osStat(path, "Dockerfile.miren"); err == nil {
		stack.Stack = "dockerfile"
		stack.Input = "Dockerfile.miren"
		return buildSourceResolution{BuildStack: stack}, nil
	}
	stack.Stack = "auto"

	if image := webImageSource(ac); image != "" {
		stack.Stack = "image"
		stack.Input = image
		b.Log.Info("using explicitly configured web image as build source", "image", image, "app", name)
		return buildSourceResolution{BuildStack: stack}, nil
	}

	detectOpts := stackbuild.BuildOptions{
		Log:         b.Log,
		Name:        name,
		OnBuild:     stack.OnBuild,
		Version:     stack.Version,
		AlpineImage: stack.AlpineImage,
	}
	detected, detectErr := stackbuild.DetectStack(stack.CodeDir, detectOpts)
	if detectErr == nil {
		b.Log.Debug("stack detection successful")
		return buildSourceResolution{BuildStack: stack, DetectedStack: detected}, nil
	}

	image, imageErr := fallbackImageSource(ac)
	if imageErr != nil {
		return buildSourceResolution{BuildStack: stack, DetectionErr: detectErr}, imageErr
	}
	if image != "" {
		stack.Stack = "image"
		stack.Input = image
		b.Log.Info("using service image as build source", "image", image, "app", name)
		return buildSourceResolution{BuildStack: stack}, nil
	}

	return buildSourceResolution{BuildStack: stack, DetectionErr: detectErr}, nil
}

// detectBuildStack adapts the shared source resolution to deploy's fail-fast
// contract. Analyze uses the same resolution but may report an unknown stack.
func (b *Builder) detectBuildStack(path string, ac *appconfig.AppConfig, name string, _ fsutil.FS) (BuildStack, error) {
	resolution, err := b.resolveBuildSource(path, ac, name)
	if err != nil {
		return resolution.BuildStack, err
	}
	if resolution.DetectionErr != nil {
		b.Log.Error("stack detection failed", "error", resolution.DetectionErr, "app", name, "codeDir", path)
		return resolution.BuildStack, fmt.Errorf("no supported stack detected for app %s: %w", name, resolution.DetectionErr)
	}
	return resolution.BuildStack, nil
}

// webImageSource captures explicit primary-image intent. Unlike a non-web
// service image, it outranks auto-detection: a user who names what web should
// run should not have an incidental package.json or main.go turn that request
// into a source build whose artifact is never used by web.
func webImageSource(ac *appconfig.AppConfig) string {
	if ac == nil {
		return ""
	}
	if web := ac.Services["web"]; web != nil && web.Image != "" {
		return web.Image
	}
	return ""
}

// fallbackImageSource chooses an image only after auto-detection found no
// buildable source. This is what lets a worker-only image app deploy without
// allowing a database sidecar image to suppress a perfectly good source build.
// Refuse to guess among several images: map iteration order is not a
// source-selection policy.
func fallbackImageSource(ac *appconfig.AppConfig) (string, error) {
	if ac == nil {
		return "", nil
	}
	images := make(map[string]struct{})
	for _, svc := range ac.Services {
		if svc != nil && svc.Image != "" {
			images[svc.Image] = struct{}{}
		}
	}
	if len(images) == 0 {
		return "", nil
	}
	if len(images) == 1 {
		for image := range images {
			return image, nil
		}
	}

	refs := make([]string, 0, len(images))
	for image := range images {
		refs = append(refs, image)
	}
	slices.Sort(refs)
	return "", fmt.Errorf("app has no buildable source and declares multiple service images (%s); set services.web.image to choose the app's primary image", strings.Join(refs, ", "))
}

// registerBuildSaga assembles the build-from-tar saga definition with all
// actions wired into the given registry. Mirrors the registerCreateSandboxSaga
// shape from controllers/sandbox/create_saga.go so both sagas register the
// same way at server startup.
func registerBuildSaga(
	registry *saga.Registry,
	builder *Builder,
	streams *StreamRegistry,
	statuses *StatusRegistry,
	log *slog.Logger,
) error {
	deps := &buildSagaDeps{
		builder:  builder,
		streams:  streams,
		statuses: statuses,
	}

	return saga.Define(sagaBuildFromTar).
		Using(deps).
		Using(log).
		Action(actionReceiveTar, receiveTar).Undo(undoReceiveTar).
		Action(actionLoadSource, loadSource).Undo(undoLoadSource).
		Action(actionGetNextVer, getNextVersion).Undo(undoGetNextVersion).
		Action(actionBuildImage, buildImage).Undo(undoBuildImage).
		Action(actionPrepareConfig, prepareConfig).Undo(undoPrepareConfig).
		Action(actionHandleEphemera, handleEphemeral).Undo(undoHandleEphemeral).
		Action(actionCreateConfigVer, createConfigVersion).Undo(undoCreateConfigVersion).
		Action(actionCreateVersion, createVersion).Undo(undoCreateVersion).
		Action(actionProvisionAddons, provisionAddons).Undo(undoProvisionAddons).
		Action(actionRunDeployTasks, runDeployTasks).Undo(undoRunDeployTasks).
		Action(actionSetActiveVer, setActiveVersion).Undo(undoSetActiveVersion).
		Action(actionFinalize, finalize).Undo(undoFinalize).
		Action(actionBeginDeploy, beginDeployment).Undo(undoBeginDeployment).
		Action(actionRecordVersion, recordAppVersion).Undo(undoRecordAppVersion).
		Action(actionActivateDeploy, activateDeployment).Undo(undoActivateDeployment).
		RegisterTo(registry)
}

// osStat returns os.Stat for a sub-path under base. Tiny wrapper so the
// stack detection logic stays readable.
func osStat(base, name string) (os.FileInfo, error) {
	return os.Stat(filepath.Join(base, name))
}

// marshalConfigSpec serializes a ConfigSpec to JSON for transport through
// the saga's input/output map. Returns "" for the zero value so downstream
// actions can branch on emptiness without unmarshaling.
func marshalConfigSpec(spec core_v1alpha.ConfigSpec) (string, error) {
	// Treat the zero value as empty to keep the saga log compact for
	// first-deploy cases.
	zero := core_v1alpha.ConfigSpec{}
	if isEmptyConfigSpec(spec, zero) {
		return "", nil
	}
	data, err := json.Marshal(spec)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// unmarshalConfigSpec is the round-trip partner of marshalConfigSpec.
// Empty string means the caller passed a zero ConfigSpec.
func unmarshalConfigSpec(s string) (core_v1alpha.ConfigSpec, error) {
	if s == "" {
		return core_v1alpha.ConfigSpec{}, nil
	}
	var spec core_v1alpha.ConfigSpec
	if err := json.Unmarshal([]byte(s), &spec); err != nil {
		return core_v1alpha.ConfigSpec{}, err
	}
	return spec, nil
}

// isEmptyConfigSpec checks whether spec equals zero by JSON shape. Using
// JSON comparison sidesteps the unexported-field issues reflect.DeepEqual
// hits with generated types.
func isEmptyConfigSpec(spec, zero core_v1alpha.ConfigSpec) bool {
	a, err := json.Marshal(spec)
	if err != nil {
		return false
	}
	b, err := json.Marshal(zero)
	if err != nil {
		return false
	}
	return string(a) == string(b)
}
