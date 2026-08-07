package build

import (
	"context"
	"fmt"
	"strings"

	"github.com/moby/buildkit/client"
	"github.com/tonistiigi/fsutil"

	"miren.dev/runtime/api/build/build_v1alpha"
	"miren.dev/runtime/api/core/core_v1alpha"
	"miren.dev/runtime/appconfig"
	"miren.dev/runtime/pkg/deploylifecycle"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/saga"
)

// buildImage runs the actual container build via BuildKit and locates
// the resulting artifact entity. This is the long-running step — for
// real apps it dominates total saga wall time. The undo is a no-op:
// images produced by a failed build linger in the registry but they're
// cheap, idempotent, and the next build either reuses them by digest
// or supersedes them.
//
// Recovery resumes here by re-running the build. That's safe because
// buildkit deduplicates by content digest, so a repeated build of the
// same source produces the same artifact entity (with the same ID).
// The action's output (manifest_digest, artifact_id) round-trips
// through the saga log so prepareConfig and downstream actions see
// stable values across crashes.

type buildImageIn struct {
	AppName     string     `json:"app_name" saga:"app_name"`
	StreamID    string     `json:"stream_id" saga:"stream_id"`
	SourceDir   string     `json:"source_dir" saga:"source_dir"`
	BuildStack  BuildStack `json:"build_stack" saga:"build_stack"`
	VersionName string     `json:"version_name" saga:"version_name"`
	ImageURL    string     `json:"image_url" saga:"image_url"`
	// AppConfig is nil when the app has no app.toml; the loadSource
	// output omits the key in that case, so the saga input is optional.
	AppConfig *appconfig.AppConfig `json:"app_config,omitempty" saga:"app_config,optional"`
	// ExistingConfig comes from getNextVersion; empty string is
	// the marshaled form of a zero ConfigSpec (first deploy).
	ExistingConfig string `json:"existing_config_json" saga:"existing_config_json"`
	// CLIEnvVars is an initial input from the deploy CLI's -e flags.
	// Empty when the user didn't pass any, so optional.
	CLIEnvVars []*build_v1alpha.EnvironmentVariable `json:"cli_env_vars,omitempty" saga:"cli_env_vars,optional"`
	AppID      string                               `json:"app_id" saga:"app_id"`
	// DeploymentID is empty for an untracked build. Consuming it also anchors
	// this action after begin-deployment, so the deploy lock is taken before
	// any buildkit work starts.
	DeploymentID string `json:"deployment_id,omitempty" saga:"deployment_id,optional"`
}

type buildImageOut struct {
	ManifestDigest string       `json:"manifest_digest" saga:"manifest_digest"`
	ArtifactID     string       `json:"artifact_id" saga:"artifact_id"`
	FinalImageURL  string       `json:"final_image_url" saga:"final_image_url"`
	BuildResult    *BuildResult `json:"build_result,omitempty" saga:"build_result"`
}

func buildImage(ctx context.Context, in buildImageIn) (buildImageOut, error) {
	deps := saga.Get[*buildSagaDeps](ctx)
	b := deps.builder
	status := deps.statuses.SenderFor(in.StreamID)

	if b.BuildKit == nil {
		status.SendError("BuildKit not configured - ensure server is running with BuildKit enabled")
		return buildImageOut{}, fmt.Errorf("buildkit component not configured")
	}

	deploy := b.trackDeployment(in.DeploymentID, status)
	deploy.setPhase(ctx, deploylifecycle.PhaseBuilding)

	existing, err := unmarshalConfigSpec(in.ExistingConfig)
	if err != nil {
		return buildImageOut{}, fmt.Errorf("deserializing existing config: %w", err)
	}

	buildLog := &buildLogWriter{
		log:      b.Log,
		writer:   b.LogWriter,
		entityID: in.AppID,
		version:  in.VersionName,
	}

	res, artifactID, finalURL, err := b.runBuildkitBuild(ctx, runBuildkitBuildInputs{
		SourceDir:      in.SourceDir,
		AppName:        in.AppName,
		VersionName:    in.VersionName,
		ImageURL:       in.ImageURL,
		BuildStack:     in.BuildStack,
		AppConfig:      in.AppConfig,
		ExistingConfig: existing,
		CLIEnvVars:     in.CLIEnvVars,
	}, status, buildLog)
	if err != nil {
		return buildImageOut{}, err
	}

	deploy.setPhase(ctx, deploylifecycle.PhasePushing)

	return buildImageOut{
		ManifestDigest: res.ManifestDigest,
		ArtifactID:     artifactID,
		FinalImageURL:  finalURL,
		BuildResult:    res,
	}, nil
}

func undoBuildImage(_ context.Context, _ buildImageIn, _ buildImageOut) error {
	return nil
}

// runBuildkitBuildInputs bundles everything runBuildkitBuild needs.
// Keeping this as a struct (vs. a long parameter list) makes the
// caller readable and lets future arguments slot in without touching
// every call site.
type runBuildkitBuildInputs struct {
	SourceDir      string
	AppName        string
	VersionName    string
	ImageURL       string
	BuildStack     BuildStack
	AppConfig      *appconfig.AppConfig
	ExistingConfig core_v1alpha.ConfigSpec
	CLIEnvVars     []*build_v1alpha.EnvironmentVariable
}

// runBuildkitBuild connects to BuildKit, runs the actual image build
// with the right transform options + status callbacks, and locates the
// resulting Artifact entity. Returns the build result, artifact ID,
// and the registry image URL adjusted to match the artifact (which
// may have been reused via content-digest deduplication).
//
// Extracted here so both the pre-saga buildFromDir path and the
// buildImage saga action share a single implementation. status may be
// noop and buildLog may have a noop writer.
func (b *Builder) runBuildkitBuild(
	ctx context.Context,
	in runBuildkitBuildInputs,
	status StatusSender,
	buildLog *buildLogWriter,
) (*BuildResult, string, string, error) {
	tr, err := fsutil.NewFS(in.SourceDir)
	if err != nil {
		return nil, "", "", fmt.Errorf("opening source dir %s: %w", in.SourceDir, err)
	}

	b.Log.Info("connecting to buildkit daemon")
	bkc, err := b.BuildKit.Client(ctx)
	if err != nil {
		b.Log.Error("failed to get buildkit client", "error", err)
		status.SendError("Failed to connect to BuildKit: %v", err)
		return nil, "", "", err
	}
	defer bkc.Close()

	// Pre-flight health check. A daemon whose socket answers the dial but can't
	// service requests (e.g. a stale buildkitd left bound to a previous miren
	// process after a restart) fails here, so the build errors out immediately
	// instead of proceeding into a confusing "NotFound: no such job" partway
	// through the solve.
	ci, err := bkc.Info(ctx)
	if err != nil {
		b.Log.Error("buildkit daemon health check failed", "error", err)
		status.SendError("BuildKit daemon health check failed: %v", err)
		return nil, "", "", fmt.Errorf("buildkit daemon health check failed: %w", err)
	}
	b.Log.Debug("buildkitd info", "version", ci.BuildkitVersion.Version, "rev", ci.BuildkitVersion.Revision)

	bk := &Buildkit{Client: bkc, Log: b.Log, WorkloadIssuer: b.WorkloadIssuer}

	buildEnvVars := computeBuildEnvVars(in.ExistingConfig.Variables, in.AppConfig, in.CLIEnvVars)
	if len(buildEnvVars) > 0 {
		b.Log.Info("injecting env vars into build", "count", len(buildEnvVars))
	}

	tos := []TransformOptions{
		WithBuildArg("MIREN_VERSION", in.VersionName),
	}
	if len(buildEnvVars) > 0 {
		tos = append(tos, WithBuildArgs(buildEnvVars))
	}

	// Pass env vars for auto-stack builds. We have to copy the stack so
	// we don't mutate the saga input.
	stack := in.BuildStack
	stack.EnvVars = buildEnvVars

	tos = append(tos, WithPhaseUpdates(func(phase string) {
		status.SendPhase(phase)
	}))

	vertexStarted := map[string]bool{}
	vertexCompleted := map[string]bool{}
	tos = append(tos, WithStatusUpdates(func(ss *client.SolveStatus, sj []byte) {
		for _, v := range ss.Vertexes {
			digestStr := v.Digest.String()
			if v.Started != nil && !vertexStarted[digestStr] {
				vertexStarted[digestStr] = true
				buildLog.write(fmt.Sprintf("[buildkit] %s", v.Name))
			}
			if v.Completed != nil && !vertexCompleted[digestStr] {
				vertexCompleted[digestStr] = true
				if v.Cached {
					buildLog.write(fmt.Sprintf("[buildkit] %s CACHED", v.Name))
				}
			}
		}
		for _, log := range ss.Logs {
			if log.Data != nil {
				lines := strings.SplitSeq(string(log.Data), "\n")
				for line := range lines {
					line = strings.TrimRight(line, " \t\r\n")
					if strings.TrimSpace(line) != "" {
						buildLog.write(line)
					}
				}
			}
		}
		status.SendBuildkit(sj)
	}))

	status.SendMessage("Calculating build")

	res, err := bk.BuildImage(ctx, tr, stack, in.AppName, in.ImageURL, tos...)
	if err != nil {
		b.Log.Error("error building image", "error", err)
		status.SendError("Error building image: %v", err)
		return nil, "", "", err
	}

	for _, event := range res.DetectionEvents {
		buildLog.write(fmt.Sprintf("[detect] %s: %s", event.Name, event.Message))
	}

	if res.ManifestDigest == "" {
		b.Log.Error("build did not return manifest digest")
		status.SendError("Build did not return manifest digest")
		return nil, "", "", fmt.Errorf("build did not return manifest digest")
	}

	var artifact core_v1alpha.Artifact
	if err := b.ec.OneAtIndex(ctx,
		entity.String(core_v1alpha.ArtifactManifestDigestId, res.ManifestDigest),
		&artifact); err != nil {
		b.Log.Error("error locating artifact by digest", "digest", res.ManifestDigest, "error", err)
		return nil, "", "", fmt.Errorf("error locating artifact by digest %s: %w", res.ManifestDigest, err)
	}
	b.Log.Debug("located stored artifact", "artifact", artifact.ID, "digest", res.ManifestDigest)

	// The artifact may have been reused by digest, so adjust the image
	// URL to point at the canonical artifact name in the registry.
	artifactName := strings.TrimPrefix(string(artifact.ID), "artifact/")
	finalURL := "cluster.local:5000/" + in.AppName + ":" + artifactName

	return res, string(artifact.ID), finalURL, nil
}
