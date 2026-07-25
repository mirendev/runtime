package commands

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/moby/buildkit/client"
	"github.com/moby/buildkit/util/progress/progresswriter"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
	"go.opentelemetry.io/otel/trace"
	"golang.org/x/term"

	"miren.dev/runtime/api/build/build_v1alpha"
	"miren.dev/runtime/api/deployment/deployment_v1alpha"
	"miren.dev/runtime/appconfig"
	"miren.dev/runtime/clientconfig"
	"miren.dev/runtime/pkg/cond"
	"miren.dev/runtime/pkg/deploygating"
	ephemeralx "miren.dev/runtime/pkg/ephemeral"
	"miren.dev/runtime/pkg/git"
	"miren.dev/runtime/pkg/otelproxy"
	"miren.dev/runtime/pkg/progress/upload"
	"miren.dev/runtime/pkg/rpc/standard"
	"miren.dev/runtime/pkg/rpc/stream"
	"miren.dev/runtime/pkg/tarx"
	"miren.dev/runtime/pkg/theme"
	"miren.dev/runtime/pkg/ui"
)

var deployTracer = otel.Tracer("miren.dev/runtime/cli/deploy")

func Deploy(ctx *Context, opts struct {
	AppCentric

	Version       string   `short:"V" long:"version" description:"Deploy an existing version (skip build)"`
	Analyze       bool     `long:"analyze" description:"Analyze the app without building (show detected stack, services, etc.)"`
	Explain       bool     `short:"x" long:"explain" description:"Explain the build process"`
	ExplainFormat string   `long:"explain-format" description:"Explain format" choice:"auto" choice:"plain" choice:"tty" choice:"rawjson" default:"auto"` //nolint
	Force         bool     `short:"f" long:"force" description:"Skip confirmation prompt"`
	Env           []string `short:"e" long:"env" description:"Set environment variable (KEY=VALUE, KEY=@file, or KEY to prompt)"`
	Sensitive     []string `short:"s" long:"sensitive" description:"Set sensitive environment variable (masked in output)"`
	Ephemeral     string   `long:"ephemeral" description:"Deploy as ephemeral preview with this label (e.g. feat-login)"`
	TTL           string   `long:"ttl" description:"TTL for ephemeral version (e.g. 48h)" default:"24h"`
	SummaryJSON   string   `long:"summary-json" description:"Write a JSON summary of the deploy result (deploy id, version, and route URLs) to this path"`
}) error {
	name := opts.App
	dir := opts.ResolvedDir()

	// Normalize and validate ephemeral label
	var ephemeralLabel, ephemeralTTL string
	if opts.Ephemeral != "" {
		normalized, err := ephemeralx.NormalizeLabel(opts.Ephemeral)
		if err != nil {
			return fmt.Errorf("invalid ephemeral label: %w", err)
		}
		ephemeralLabel = normalized

		if _, err := time.ParseDuration(opts.TTL); err != nil {
			return fmt.Errorf("invalid TTL %q: %w", opts.TTL, err)
		}
		ephemeralTTL = opts.TTL
	}

	if ctx.ClientConfig == nil {
		return fmt.Errorf("no client configuration available; run `miren login` to authenticate or install a server locally")
	}

	isInteractive := term.IsTerminal(int(os.Stdin.Fd()))

	// Check that we have at least one cluster configured
	// Check if we have an identity - if so, offer to add a cluster
	if isInteractive &&
		ctx.ClientConfig.GetClusterCount() == 0 &&
		ctx.ClientConfig.HasIdentities() {
		confirmed, err := ui.Confirm(
			ui.WithMessage("No clusters configured. Would you like to add one now?"),
			ui.WithDefault(true),
			ui.WithIndent("  "),
		)
		if err != nil || !confirmed {
			return fmt.Errorf("no clusters configured; run 'miren login' to authenticate and 'miren cluster add' to configure a cluster, or install a server locally")
		}

		if err := AddClusterInteractive(ctx); err != nil {
			return fmt.Errorf("failed to add cluster: %w", err)
		}

		// Reload the client config
		cfg, err := clientconfig.LoadConfig()
		if err != nil {
			return fmt.Errorf("failed to reload config after adding cluster: %w", err)
		}
		ctx.ClientConfig = cfg

		// Get the active cluster (the one we just added)
		clusterName := cfg.ActiveCluster()
		if clusterName == "" {
			return fmt.Errorf("no active cluster set after adding cluster")
		}

		clusterCfg, err := cfg.GetCluster(clusterName)
		if err == nil && clusterCfg != nil {
			ctx.ClusterConfig = clusterCfg
			ctx.ClusterName = clusterName
		}

		ctx.Info("")
	}

	// Re-check after potential cluster add
	if ctx.ClientConfig.GetClusterCount() == 0 {
		return fmt.Errorf("no clusters configured; run 'miren login' to authenticate and configure a cluster, or install a server locally")
	}

	// Handle --analyze flag: analyze the app without building
	if opts.Analyze {
		cl, err := ctx.RPCClient("dev.miren.runtime/build")
		if err != nil {
			return err
		}

		bc := build_v1alpha.NewBuilderClient(cl)
		return analyzeApp(ctx, bc, dir)
	}

	// Handle --version flag: deploy an existing version (skip build)
	if opts.Version != "" {
		depCl, err := ctx.RPCClient("dev.miren.runtime/deployment")
		if err != nil {
			return fmt.Errorf("failed to connect to deployment service: %w", err)
		}
		depClient := deployment_v1alpha.NewDeploymentClient(depCl)

		var envVars []*deployment_v1alpha.EnvironmentVariable
		if len(opts.Env) > 0 || len(opts.Sensitive) > 0 {
			specs, err := ParseEnvVarSpecs(opts.Env, opts.Sensitive)
			if err != nil {
				return err
			}
			for _, spec := range specs {
				ev := &deployment_v1alpha.EnvironmentVariable{}
				ev.SetKey(spec.Key)
				ev.SetValue(spec.Value)
				ev.SetSensitive(spec.Sensitive)
				envVars = append(envVars, ev)
			}
		}

		result, err := depClient.DeployVersion(ctx, name, ctx.ClusterName, opts.Version, false, envVars, ephemeralLabel, ephemeralTTL)
		if err != nil {
			return fmt.Errorf("failed to deploy version: %w", err)
		}

		if result.HasError() && result.Error() != "" {
			if result.HasLockInfo() && result.LockInfo() != nil {
				lockInfo := result.LockInfo()
				ctx.Printf("\n❌ Deployment blocked:\n\n")
				ctx.Printf("Another deployment is already in progress for app '%s' on cluster '%s'.\n\n",
					lockInfo.AppName(), lockInfo.ClusterId())
				ctx.Printf("Existing deployment details:\n")
				ctx.Printf("  • Deployment ID: %s\n", ui.DisplayShortID(lockInfo.BlockingDeploymentShortId(), lockInfo.BlockingDeploymentId()))
				ctx.Printf("  • Started by: %s\n", lockInfo.StartedBy())
				if lockInfo.HasStartedAt() && lockInfo.StartedAt() != nil {
					startedAt := time.Unix(lockInfo.StartedAt().Seconds(), 0)
					ctx.Printf("  • Started at: %s (%s ago)\n",
						startedAt.Format("2006-01-02 15:04:05 MST"),
						time.Since(startedAt).Round(time.Second))
				}
				ctx.Printf("  • Current phase: %s\n", lockInfo.CurrentPhase())
				if lockInfo.HasLockExpiresAt() && lockInfo.LockExpiresAt() != nil {
					expiresAt := time.Unix(lockInfo.LockExpiresAt().Seconds(), 0)
					ctx.Printf("  • Lock expires in: %s\n\n", time.Until(expiresAt).Round(time.Second))
				}
			} else {
				ctx.Printf("\n❌ Deploy failed: %s\n", result.Error())
			}
			return fmt.Errorf("deploy version failed")
		}

		if result.HasDeployment() && result.Deployment() != nil {
			dep := result.Deployment()
			deployedVersion := dep.AppVersionId()
			if deployedVersion == "" {
				deployedVersion = opts.Version
			}
			versionDisplay := ui.DisplayShortID(dep.AppVersionShortId(), deployedVersion)

			if ephemeralLabel != "" {
				// Ephemeral deploy via --version: show ephemeral info, skip activation wait
				ctx.Printf("Ephemeral version %s created.\n", versionDisplay)
				ctx.Printf("  Label: %s\n", ephemeralLabel)
				ctx.Printf("  TTL:   %s\n", ephemeralTTL)
				if result.HasAccessInfo() && result.AccessInfo() != nil {
					info := result.AccessInfo()
					if info.HasHostnames() {
						for _, h := range *info.Hostnames() {
							ctx.Printf("  URL:   https://%s\n", h)
						}
					}
					if info.HasClusterHostname() && info.ClusterHostname() != "" {
						ctx.Printf("  URL:   https://%s.%s\n", ephemeralLabel, info.ClusterHostname())
					}
				}
			} else {
				ctx.Printf("Deploying version %s to %s\n", versionDisplay, ctx.ClusterName)

				if err := awaitHealthy(ctx, name, deployedVersion, versionDisplay); err != nil {
					return err
				}

				if result.HasAccessInfo() && result.AccessInfo() != nil {
					displayDeployVersionAccessInfo(ctx, name, result.AccessInfo())
				}
			}

			// dep.Id() is empty for ephemeral deploys (no deployment record).
			var summaryURLs []string
			if result.HasAccessInfo() && result.AccessInfo() != nil {
				summaryURLs = deployURLs(ctx, result.AccessInfo(), ephemeralLabel)
			}
			writeDeploySummary(ctx, opts.SummaryJSON, dep.Id(), deployedVersion, summaryURLs)
		}
		return nil
	}

	// Set up OTel tracing via the server's telemetry proxy.
	// Spans are shipped through the existing RPC connection — no client-side
	// OTLP config needed. If the server isn't reachable, tracing is a no-op.
	if proxyClient, err := ctx.RPCClient("dev.miren.runtime/telemetry"); err == nil {
		shutdown, setupErr := otelproxy.SetupProxyTracing(ctx.Context, proxyClient, ctx.Log, semconv.ServiceName("miren-cli"))
		if setupErr != nil {
			ctx.Log.Debug("failed to set up proxy tracing", "error", setupErr)
		} else {
			defer func() {
				shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				_ = shutdown(shutdownCtx)
			}()
		}
	} else {
		ctx.Log.Debug("telemetry proxy unavailable", "error", err)
	}

	// Start root deploy span
	deployCtxTraced, deploySpan := deployTracer.Start(ctx.Context, "deploy",
		trace.WithAttributes(
			attribute.String("miren.app.name", name),
			attribute.String("miren.cluster", ctx.ClusterName),
		),
	)
	defer deploySpan.End()

	// Replace the context so downstream calls inherit the trace
	ctx.Context = deployCtxTraced

	// Confirm deployment unless --force is used or stdin is not a TTY.
	// Always confirm when the app config was found in a parent directory,
	// otherwise skip confirmation when only one cluster is configured.
	hasSingleCluster := ctx.ClientConfig != nil && ctx.ClientConfig.GetClusterCount() == 1
	needsConfirm := opts.foundInParent || !hasSingleCluster
	if !opts.Force && isInteractive && needsConfirm {
		if opts.foundInParent {
			ctx.Printf("  ℹ App config found in parent directory %s\n", dir)
		}
		message := fmt.Sprintf("Deploy app '%s' to cluster '%s'?", name, ctx.ClusterName)
		confirmed, err := ui.Confirm(
			ui.WithMessage(message),
			ui.WithDefault(true),
			ui.WithIndent("  "),
		)
		if err != nil {
			return fmt.Errorf("confirmation cancelled: %w", err)
		}
		if !confirmed {
			ctx.Printf("  deployment cancelled\n")
			return nil
		}
	}

	greenStyle := lipgloss.NewStyle().Foreground(theme.Success)
	faintStyle := lipgloss.NewStyle().Foreground(theme.Muted)
	ctx.Printf("  ✓ %s: %s %s %s\n", greenStyle.Render("Deploying"), name, faintStyle.Render("→"), ctx.ClusterName)

	cl, err := ctx.RPCClient("dev.miren.runtime/build")
	if err != nil {
		return err
	}

	bc := build_v1alpha.NewBuilderClient(cl)

	// Decide who owns the deployment record. A server that advertises the
	// "deployment" parameter on buildFromTar owns the record itself: it creates
	// it, advances it, and settles it as the build runs. Against an older server
	// that lacks the parameter, the CLI falls back to driving the record through
	// the deprecated deployment RPCs, exactly as before. The probe runs before
	// any upload, so an old server costs nothing.
	//
	// Ephemeral deploys never own a record on either path.
	serverOwnsDeployment := ephemeralLabel == "" && cl.HasMethodParam(ctx.Context, "buildFromTar", "deployment")

	// Check if deployment is allowed before proceeding
	remedy, err := deploygating.CheckDeployAllowed(dir)
	if err != nil {
		if remedy != "" {
			ctx.Printf("Error: %s\n%s\n\n", err, remedy)
		}
		return fmt.Errorf("deploy gate check failed: %w", err)
	}

	ctx.Log.Info("building code", "name", name, "dir", dir)

	// Capture git information before creating deployment record
	var gitInfo *git.Info
	gitInfo, gitErr := git.GetInfo(dir)
	if gitErr != nil {
		ctx.Log.Debug("Failed to get git info", "error", gitErr)
		// Don't fail deployment if git info is unavailable
	}

	// Create deployment record early in the process
	depCl, err := ctx.RPCClient("dev.miren.runtime/deployment")
	if err != nil {
		return fmt.Errorf("failed to connect to deployment service: %w", err)
	}
	depClient := deployment_v1alpha.NewDeploymentClient(depCl)

	// Convert git.Info to deployment GitInfo
	var deploymentGitInfo *deployment_v1alpha.GitInfo
	if gitInfo != nil {
		deploymentGitInfo = &deployment_v1alpha.GitInfo{}
		deploymentGitInfo.SetSha(gitInfo.SHA)
		deploymentGitInfo.SetBranch(gitInfo.Branch)
		deploymentGitInfo.SetIsDirty(gitInfo.IsDirty)
		deploymentGitInfo.SetWorkingTreeHash(gitInfo.WorkingTreeHash)
		deploymentGitInfo.SetCommitMessage(gitInfo.CommitMessage)
		deploymentGitInfo.SetCommitAuthorName(gitInfo.CommitAuthor)
		deploymentGitInfo.SetCommitAuthorEmail(gitInfo.CommitEmail)
		deploymentGitInfo.SetRepository(gitInfo.RemoteURL)

		// Convert timestamp string to standard.Timestamp if available
		if gitInfo.CommitTimestamp != "" {
			if ts, err := time.Parse(time.RFC3339, gitInfo.CommitTimestamp); err == nil {
				deploymentGitInfo.SetCommitTimestamp(standard.ToTimestamp(ts))
			}
		}
	}

	// deployReq is sent on the build call when the server owns the record; its
	// presence is what tells the server to take ownership.
	var deployReq *build_v1alpha.DeployRequest
	if serverOwnsDeployment {
		deployReq = &build_v1alpha.DeployRequest{}
		deployReq.SetClusterId(ctx.ClusterName)
		if gi := buildGitInfoFromGit(gitInfo); gi != nil {
			deployReq.SetGitInfo(gi)
		}

		// Advisory pre-flight: surface the rich "deployment blocked" message
		// without wasting an upload. The authoritative acquisition still happens
		// server-side inside the build; this only spares the common case.
		if lockRes, lockErr := depClient.GetDeployLock(ctx, name, ctx.ClusterName); lockErr == nil &&
			lockRes.Held() && lockRes.HasLockInfo() && lockRes.LockInfo() != nil {
			printDeploymentBlocked(ctx, lockRes.LockInfo())
			return fmt.Errorf("deployment blocked by lock")
		}
	}

	// Create deployment record for non-ephemeral deploys only, and only when the
	// server does not own the record. Ephemeral deploys don't participate in
	// deployment tracking or locking.
	var deploymentId string
	if ephemeralLabel == "" && !serverOwnsDeployment {
		createDepCtx, createDepSpan := deployTracer.Start(ctx.Context, "deploy.create_deployment")
		createResult, err := depClient.CreateDeployment(createDepCtx, name, ctx.ClusterName, "pending-build", deploymentGitInfo)
		if err != nil {
			createDepSpan.RecordError(err)
			createDepSpan.SetStatus(codes.Error, err.Error())
			createDepSpan.End()
			return fmt.Errorf("failed to create deployment record: %w", err)
		}
		createDepSpan.End()

		if createResult.HasError() && createResult.Error() != "" {
			if createResult.HasLockInfo() && createResult.LockInfo() != nil {
				printDeploymentBlocked(ctx, createResult.LockInfo())
			} else {
				// Fall back to plain error message
				ctx.Printf("\n❌ Deployment blocked:\n\n%s\n", createResult.Error())
			}
			return fmt.Errorf("deployment blocked by lock")
		}

		if !createResult.HasDeployment() || createResult.Deployment() == nil {
			return fmt.Errorf("deployment creation returned no deployment")
		}

		deploymentId = createResult.Deployment().Id()
		ctx.Log.Info("Created deployment record", "deployment_id", deploymentId)
	}

	// Create a cancellable context for the build that can be cancelled externally
	buildCtx, cancelBuild := context.WithCancel(ctx.Context)
	defer cancelBuild()

	// deploymentId is set up front on the legacy path (from CreateDeployment) and
	// arrives mid-stream on the server-owned path (from the build's deployment
	// status arm). A mutex guards it because that arm is handled in a stream
	// goroutine while the main goroutine reads it for the summary.
	var deploymentMu sync.Mutex
	setDeploymentID := func(id string) {
		deploymentMu.Lock()
		deploymentId = id
		deploymentMu.Unlock()
	}
	getDeploymentID := func() string {
		deploymentMu.Lock()
		defer deploymentMu.Unlock()
		return deploymentId
	}

	// The cancellation poller starts once we know the deployment id, which is
	// immediately on the legacy path and on the first deployment status arm on
	// the server-owned path.
	var (
		pollerOnce          sync.Once
		externallyCancelled atomic.Bool
	)
	startPoller := func(id string) {
		if id == "" {
			return
		}
		pollerOnce.Do(func() {
			statusGetter := newDepClientStatusGetter(depClient, ctx.Log)
			poller := newCancellationPoller(id, statusGetter, 2*time.Second)
			go poller.Start(buildCtx, func() {
				ctx.Log.Info("Deployment cancelled externally, stopping build")
				externallyCancelled.Store(true)
				cancelBuild()
			})
		})
	}
	if !serverOwnsDeployment {
		startPoller(deploymentId)
	}

	// Helper to check if we were externally cancelled
	wasExternallyCancelled := externallyCancelled.Load

	// onDeployment records the server-created deployment on the server-owned
	// path: it captures the id and starts the cancellation poller the first time
	// the build reports it.
	//
	// Degradation note: on the server-owned path this is the ONLY thing that
	// starts the poller and sets deploymentId. The server emits the deployment
	// arm at the very start of the build (right after it takes the lock), so in
	// practice it always arrives. But if it never does — a dropped arm, or a
	// server that advertises the param yet never emits it — external
	// `miren deploy cancel` cannot interrupt this CLI build and --summary-json's
	// deploy_id is empty. The deploy itself is unaffected: the server still owns
	// and settles the record. This is an accepted degradation, not a failure.
	onDeployment := func(id, phase string) {
		if id == "" {
			return
		}
		setDeploymentID(id)
		startPoller(id)
		ctx.Log.Info("Server-owned deployment", "deployment_id", id, "phase", phase)
	}

	// Parse environment variables to pass to build server
	var envVars []*build_v1alpha.EnvironmentVariable
	if len(opts.Env) > 0 || len(opts.Sensitive) > 0 {
		envSpecs, err := ParseEnvVarSpecs(opts.Env, opts.Sensitive)
		if err != nil {
			return err
		}

		// Convert to build_v1alpha.EnvironmentVariable for RPC
		envVars = make([]*build_v1alpha.EnvironmentVariable, len(envSpecs))
		for i, spec := range envSpecs {
			ev := &build_v1alpha.EnvironmentVariable{}
			ev.SetKey(spec.Key)
			ev.SetValue(spec.Value)
			ev.SetSensitive(spec.Sensitive)
			envVars[i] = ev
		}

		ctx.Printf("  Setting %d environment variable(s)...\n", len(envVars))
	}

	// Initialize build error/log/warning tracking. buildStateMu guards the
	// three slices below; the build status callback appends to them from
	// RPC stream-handler goroutines, and updateDeploymentOnError reads them.
	var buildStateMu sync.Mutex
	var buildErrors []string
	var buildLogs []string
	var deployWarnings []*build_v1alpha.LogEntry

	// Helper function to update deployment phase. No-op when the server owns the
	// record — it advances the phase itself as the build progresses.
	updateDeploymentPhase := func(phase string) {
		if serverOwnsDeployment {
			return
		}
		if id := getDeploymentID(); id != "" {
			_, updateErr := depClient.UpdateDeploymentPhase(ctx, id, phase)
			if updateErr != nil {
				ctx.Log.Error("Failed to update deployment phase", "error", updateErr, "phase", phase)
			}
		}
	}

	// snapshotBuildState returns copies of the build state slices under the
	// mutex so readers don't race with append in createBuildStatusCallback's
	// stream-handler goroutines.
	snapshotBuildState := func() ([]string, []string, []*build_v1alpha.LogEntry) {
		buildStateMu.Lock()
		defer buildStateMu.Unlock()
		return slices.Clone(buildErrors), slices.Clone(buildLogs), slices.Clone(deployWarnings)
	}

	// Helper function to update deployment status on failure. No-op when the
	// server owns the record — it settles the deployment as failed itself.
	updateDeploymentOnError := func(errMsg string) {
		if serverOwnsDeployment {
			return
		}
		if id := getDeploymentID(); id != "" {
			// Use a separate context with timeout for status updates to ensure they complete
			// even if the main context is canceled
			statusCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()

			errsSnap, logsSnap, _ := snapshotBuildState()
			logs := strings.Join(logsSnap, "\n")
			if logs == "" && len(errsSnap) > 0 {
				logs = strings.Join(errsSnap, "\n")
			}

			_, updateErr := depClient.UpdateFailedDeployment(statusCtx, id, errMsg, logs)
			if updateErr != nil {
				// Fallback to simple status update
				_, updateErr = depClient.UpdateDeploymentStatus(statusCtx, id, "failed", errMsg)
				if updateErr != nil {
					ctx.Log.Error("Failed to update deployment status to failed", "error", updateErr)
				}
			}
		}
	}

	// If the deploy panics during the build phase (e.g. a stream-callback
	// race), mark the deployment failed so the server-side lock is released
	// immediately instead of waiting for its 30-minute TTL. Once the
	// deployment has been marked active, we no longer want to flip it back to
	// failed on panic — the build succeeded and traffic is already moving.
	var deploymentFinalized bool
	defer func() {
		if r := recover(); r != nil {
			if !deploymentFinalized {
				updateDeploymentOnError(fmt.Sprintf("CLI panic: %v", r))
			}
			panic(r)
		}
	}()

	// Load AppConfig to get include patterns
	var includePatterns []string
	ac, err := appconfig.LoadAppConfigUnder(dir)
	if err != nil {
		updateDeploymentOnError(fmt.Sprintf("Failed to load app config: %v", err))
		return err
	}
	if ac != nil && ac.Include != nil {
		// Validate patterns before using them
		for _, pattern := range ac.Include {
			if err := tarx.ValidatePattern(pattern); err != nil {
				updateDeploymentOnError(fmt.Sprintf("Invalid include pattern: %v", err))
				return fmt.Errorf("invalid include pattern %q: %w", pattern, err)
			}
		}
		includePatterns = ac.Include
	}

	// Update phase to building
	updateDeploymentPhase("building")

	// Start upload span covering tar creation + build
	_, uploadSpan := deployTracer.Start(buildCtx, "deploy.upload")

	// Try optimized delta upload: compute manifest, ask server what's cached
	var (
		sessionID    string
		useOptimized bool
		totalFiles   int
		cachedFiles  int32
		neededPaths  map[string]bool
	)

	manifest, manifestErr := tarx.ComputeManifest(dir, includePatterns)
	if manifestErr == nil {
		totalFiles = len(manifest)

		// Convert to RPC manifest entries
		rpcManifest := make([]*build_v1alpha.FileManifestEntry, len(manifest))
		for i, m := range manifest {
			entry := &build_v1alpha.FileManifestEntry{}
			entry.SetPath(m.Path)
			entry.SetHash(m.Hash)
			entry.SetSize(m.Size)
			entry.SetMode(m.Mode)
			rpcManifest[i] = entry
		}

		prepResult, prepErr := bc.PrepareUpload(buildCtx, name, rpcManifest)
		if prepErr == nil && prepResult.Result() != nil {
			result := prepResult.Result()
			sessionID = result.SessionId()
			cachedFiles = result.CachedCount()
			useOptimized = true

			if result.HasNeededPaths() && result.NeededPaths() != nil {
				neededPaths = make(map[string]bool)
				for _, p := range *result.NeededPaths() {
					neededPaths[p] = true
				}
			}
		} else {
			ctx.Log.Debug("prepareUpload unavailable, falling back to full upload", "error", prepErr)
		}
	} else {
		ctx.Log.Debug("manifest computation failed, falling back to full upload", "error", manifestErr)
	}

	// Compute uncompressed totals for progress estimation and cached bytes for the summary.
	var totalUncompressed int64
	var cachedBytes int64
	if manifest != nil {
		var totalManifestBytes int64
		for _, m := range manifest {
			totalManifestBytes += m.Size
		}
		if useOptimized && neededPaths != nil {
			for _, m := range manifest {
				if neededPaths[m.Path] {
					totalUncompressed += m.Size
				}
			}
			cachedBytes = totalManifestBytes - totalUncompressed
		} else {
			totalUncompressed = totalManifestBytes
		}
	}

	var uncompressedWritten atomic.Int64
	var r io.ReadCloser
	if useOptimized && len(neededPaths) > 0 {
		r, err = tarx.MakeFilteredTar(dir, includePatterns, neededPaths, &uncompressedWritten)
	} else if useOptimized {
		r = tarx.MakeEmptyTar()
	} else {
		r, err = tarx.MakeTar(dir, includePatterns, &uncompressedWritten)
	}
	if err != nil {
		uploadSpan.RecordError(err)
		uploadSpan.SetStatus(codes.Error, err.Error())
		uploadSpan.End()
		updateDeploymentOnError(fmt.Sprintf("Failed to create tar: %v", err))
		return err
	}

	defer r.Close()

	// buildCall wraps either BuildFromTar or BuildFromPrepared
	type buildResults interface {
		Version() string
		HasVersionShortId() bool
		VersionShortId() string
		HasAccessInfo() bool
		AccessInfo() *build_v1alpha.AccessInfo
	}
	buildCall := func(callCtx context.Context, tarReader io.ReadCloser, cb stream.SendStream[*build_v1alpha.Status]) (buildResults, error) {
		// deployReq is non-nil only when the server owns the deployment record;
		// its presence is the signal that hands ownership to the server.
		if useOptimized {
			tarStream := stream.ServeReader(callCtx, tarReader, stream.WithBulkBatching())
			return bc.BuildFromPrepared(callCtx, sessionID, tarStream, cb, envVars, ephemeralLabel, ephemeralTTL, deployReq)
		}
		return bc.BuildFromTar(callCtx, name, stream.ServeReader(callCtx, tarReader, stream.WithBulkBatching()), cb, envVars, ephemeralLabel, ephemeralTTL, deployReq)
	}

	var (
		cb      stream.SendStream[*build_v1alpha.Status]
		results buildResults
	)

	// Detect if we have a TTY - if not, force explain mode
	isTTY := term.IsTerminal(int(os.Stdout.Fd()))
	useExplainMode := opts.Explain || !isTTY

	// When we render the interactive build TUI, the program stays alive past the
	// build so the activate + health steps render as part of the same process.
	// These hold the running program across that span; they stay nil/zero in
	// explain mode. quitBuildUI tears it down (idempotent) before any path that
	// needs to print directly to stdout.
	var (
		buildProg       *tea.Program
		buildModel      *deployInfo
		buildWG         sync.WaitGroup
		buildFinalModel tea.Model
	)
	quitBuildUI := func() {
		if buildProg != nil {
			buildProg.Quit()
			buildWG.Wait()
			buildProg = nil
		}
	}
	defer quitBuildUI()

	if useExplainMode {
		if useOptimized && cachedFiles > 0 {
			ctx.Printf("  %s\n", faintStyle.Render(fmt.Sprintf("Reused %d/%d files from previous deploy, uploading %d", cachedFiles, totalFiles, len(neededPaths))))
		}

		// In explain mode, write to stderr
		pw, err := progresswriter.NewPrinter(ctx, os.Stderr, opts.ExplainFormat)
		if err != nil {
			return err
		}
		safeStatus := newSafeStatusCh(pw.Status())
		defer safeStatus.Close()

		// Add upload progress tracking in explain mode
		uploadStartTime := time.Now()
		var uploadBytes int64
		var lastPrintTime time.Time

		progressReader := upload.NewProgressReader(r, func(progress upload.Progress) {
			enrichUploadProgress(&progress, &uncompressedWritten, totalUncompressed)
			uploadBytes = progress.BytesRead
			// Print progress every 500ms to avoid spamming
			if progress.Fraction > 0 && time.Since(lastPrintTime) >= 500*time.Millisecond {
				lastPrintTime = time.Now()
				fmt.Fprintf(os.Stderr, "\r\033[K") // Clear to end of line
				line := fmt.Sprintf("Uploading artifacts: %d%% — %s at %s",
					int(progress.Fraction*100),
					upload.FormatBytes(progress.BytesRead),
					upload.FormatSpeed(progress.BytesPerSecond))
				if progress.ETA > 0 {
					line += fmt.Sprintf(" (eta ~%s)", upload.FormatDuration(progress.ETA))
				}
				fmt.Fprint(os.Stderr, line)
			}
		})
		r = progressReader

		// Progress handler for explain mode
		progressHandler := func(status *client.SolveStatus) error {
			// Clear the upload progress line when buildkit starts
			if uploadBytes > 0 {
				uploadDuration := time.Since(uploadStartTime)
				avgSpeed := float64(uploadBytes) / uploadDuration.Seconds()
				summary := fmt.Sprintf("\rUpload complete: %s in %.1fs at %s",
					upload.FormatBytes(uploadBytes),
					uploadDuration.Seconds(),
					upload.FormatSpeed(avgSpeed))
				if useOptimized && cachedFiles > 0 {
					summary += fmt.Sprintf(", reused %d/%d files", cachedFiles, totalFiles)
					if cachedBytes > 0 && avgSpeed > 0 {
						savedSec := float64(cachedBytes) / avgSpeed
						summary += fmt.Sprintf(" (saved %s, ~%s)", upload.FormatBytes(cachedBytes), upload.FormatDuration(time.Duration(savedSec*float64(time.Second))))
					} else if cachedBytes > 0 {
						summary += fmt.Sprintf(" (saved %s)", upload.FormatBytes(cachedBytes))
					}
				}
				fmt.Fprintf(os.Stderr, "%s\n", summary)
				uploadBytes = 0 // Only print once
			}

			return safeStatus.Send(buildCtx, status)
		}

		cb = createBuildStatusCallback(buildCtx, nil, nil, &buildStateMu, &buildErrors, nil, &deployWarnings, progressHandler, onDeployment)

		results, err = buildCall(buildCtx, r, cb)
		if err != nil {
			uploadSpan.RecordError(err)
			uploadSpan.SetStatus(codes.Error, err.Error())
			uploadSpan.End()

			// Check if this was a context cancellation
			if buildCtx.Err() != nil {
				ctx.Printf("\n\n❌ Deploy cancelled.\n")
				// Don't update deployment status - it's already cancelled
				if !wasExternallyCancelled() {
					updateDeploymentOnError("Deploy cancelled by user")
				}
				return buildCtx.Err()
			}

			// Check if this was a server panic
			var panicErr cond.ErrPanic
			if errors.As(err, &panicErr) {
				ctx.Printf("\n\n❌ Build failed due to a server panic.\n")
				ctx.Printf("The server encountered a panic: %s\n", panicErr.Message)
				ctx.Printf("Check the server logs for more details.\n")
				updateDeploymentOnError(fmt.Sprintf("Server panic: %s", panicErr.Message))
				return err
			}

			// If the build never got its own record because another deploy held
			// the lock, add the rich blocked banner — but still show any build
			// errors below rather than instead of them.
			maybeReportBlocked(ctx, depClient, serverOwnsDeployment, getDeploymentID(), name)

			ctx.Printf("\n\nBuild failed with the following errors:\n")
			errsSnap, _, _ := snapshotBuildState()
			printBuildErrors(ctx, errsSnap, nil)
			updateDeploymentOnError(fmt.Sprintf("Build failed: %v", err))
			return err
		}

		safeStatus.Close()
		<-pw.Done()

		if pw.Err() != nil {
			uploadSpan.RecordError(pw.Err())
			uploadSpan.SetStatus(codes.Error, pw.Err().Error())
			uploadSpan.End()
			return pw.Err()
		}
	} else {
		var (
			updateCh         = make(chan string, 1)
			buildCh          = make(chan buildProgress, 1)
			uploadProgressCh = make(chan upload.Progress, 1)
		)

		progressReader := upload.NewProgressReader(r, func(progress upload.Progress) {
			enrichUploadProgress(&progress, &uncompressedWritten, totalUncompressed)
			select {
			case uploadProgressCh <- progress:
			default:
			}
		})
		r = progressReader

		// Create a context that can be cancelled by UI (child of buildCtx for external cancellation)
		deployCtx, cancelDeploy := context.WithCancel(buildCtx)
		defer cancelDeploy()

		buildModel = initialModel(updateCh, buildCh, uploadProgressCh, cachedFiles, totalFiles, cachedBytes)
		buildProg = tea.NewProgram(buildModel)

		buildWG.Add(1)
		go func() {
			defer buildWG.Done()
			fm, runErr := buildProg.Run()
			buildFinalModel = fm
			if runErr == nil {
				if dm, ok := fm.(*deployInfo); ok && dm.interrupted {
					cancelDeploy()
				}
			} else {
				// UI died; ensure we don't keep uploading/building
				cancelDeploy()
			}
		}()

		// Progress handler for interactive mode
		progressHandler := func(status *client.SolveStatus) error {
			buildProg.Send(status)
			return nil
		}

		cb = createBuildStatusCallback(deployCtx, updateCh, buildCh, &buildStateMu, &buildErrors, &buildLogs, &deployWarnings, progressHandler, onDeployment)

		results, err = buildCall(deployCtx, r, cb)

		if err != nil {
			// Build failed: shut down the UI before printing anything.
			quitBuildUI()

			uploadSpan.RecordError(err)
			uploadSpan.SetStatus(codes.Error, err.Error())
			uploadSpan.End()

			// Check if this was a user interruption (via UI flag or context cancellation)
			dm, isDeploy := buildFinalModel.(*deployInfo)
			if (isDeploy && dm.interrupted) || deployCtx.Err() != nil {
				ctx.Printf("\n\n❌ Deploy cancelled.\n")
				// Don't update deployment status if externally cancelled - it's already cancelled
				if !wasExternallyCancelled() {
					updateDeploymentOnError("Deploy cancelled by user")
				}
				if deployCtx.Err() != nil {
					return deployCtx.Err()
				}
				if buildCtx.Err() != nil {
					return buildCtx.Err()
				}
				return context.Canceled
			}

			// Check if this was a server panic
			var panicErr cond.ErrPanic
			if errors.As(err, &panicErr) {
				ctx.Printf("\n\n❌ Build failed due to a server panic.\n")
				ctx.Printf("The server encountered a panic: %s\n", panicErr.Message)
				ctx.Printf("Check the server logs for more details.\n")
				updateDeploymentOnError(fmt.Sprintf("Server panic: %s", panicErr.Message))
				return err
			}

			// See the explain-mode branch: only add the blocked banner when we
			// never got our own record, and still print build errors below.
			maybeReportBlocked(ctx, depClient, serverOwnsDeployment, getDeploymentID(), name)

			ctx.Printf("\n\nBuild failed.\n")
			errsSnap, logsSnap, _ := snapshotBuildState()
			printBuildErrors(ctx, errsSnap, logsSnap)
			updateDeploymentOnError(fmt.Sprintf("Build failed: %v", err))
			return err
		}

		// Build succeeded: fold the build phase into the model and keep the
		// program alive, so the activate + health steps render as continuations
		// of the same TUI rather than flat text printed below it.
		buildProg.Send(buildDoneMsg{})
		updateDeploymentPhase("pushing")
	}

	if results.Version() == "" {
		quitBuildUI()
		noVersionErr := fmt.Errorf("build failed: no version returned")
		uploadSpan.RecordError(noVersionErr)
		uploadSpan.SetStatus(codes.Error, noVersionErr.Error())
		uploadSpan.End()
		ctx.Printf("\n\nError detected in building %s. No version returned.\n", name)
		errsSnap, logsSnap, _ := snapshotBuildState()
		printBuildErrors(ctx, errsSnap, logsSnap)
		updateDeploymentOnError("Build failed: no version returned")
		return noVersionErr
	}

	// Upload + build completed successfully
	uploadSpan.End()

	ctx.Log.Debug("Build completed", "version", results.Version())

	appVersionId := results.Version()
	if appVersionId == "" {
		updateDeploymentOnError("Build did not return a version")
		return fmt.Errorf("build did not return a version")
	}

	ctx.Log.Debug("Build completed with version", "version", appVersionId)

	if ephemeralLabel != "" {
		// Ephemeral deploy: no deployment record to update, just show info.
		// Tear down the build TUI first so its final frame doesn't fight our
		// direct output (ephemeral skips the health phase that would own it).
		quitBuildUI()
		versionDisplay := ui.DisplayShortID(results.VersionShortId(), results.Version())
		ctx.Printf("\n\nEphemeral version %s created.\n", versionDisplay)
		ctx.Printf("  Label: %s\n", ephemeralLabel)
		ctx.Printf("  TTL:   %s\n", ephemeralTTL)

		// Show ephemeral access URLs (server returns resolved hostnames)
		if results.HasAccessInfo() && results.AccessInfo() != nil {
			info := results.AccessInfo()
			if info.HasHostnames() {
				for _, h := range *info.Hostnames() {
					ctx.Printf("  URL:   https://%s\n", h)
				}
			}
			if info.HasClusterHostname() && info.ClusterHostname() != "" {
				ctx.Printf("  URL:   https://%s.%s\n", ephemeralLabel, info.ClusterHostname())
			}
		}
	} else if serverOwnsDeployment {
		// The server already advanced and settled the deployment record as the
		// build ran; the CLI is just a viewer. Nothing to finalize.
		deploymentFinalized = true
	} else {
		// Normal deploy: update deployment tracking
		// Update phase to pushing (build completed, now pushing)
		updateDeploymentPhase("pushing")

		legacyDeploymentID := getDeploymentID()

		// Update deployment with actual app version ID
		_, err = depClient.UpdateDeploymentAppVersion(ctx, legacyDeploymentID, appVersionId)
		if err != nil {
			ctx.Log.Error("Failed to update deployment app version", "error", err)
			// Continue anyway - the deployment is proceeding
		}

		// Update phase to activating
		updateDeploymentPhase("activating")

		// Wrap finalization in a span
		finalizeCtx, finalizeSpan := deployTracer.Start(ctx.Context, "deploy.finalize")

		// Mark deployment as active
		_, err = depClient.UpdateDeploymentStatus(finalizeCtx, legacyDeploymentID, "active", "")
		if err != nil {
			// Log error but don't fail - deployment is already done
			ctx.Log.Error("Failed to update deployment status", "error", err)
		}
		finalizeSpan.End()
		deploymentFinalized = true

		// No standalone "deployed" line here: the health step below is the next
		// beat of the same process and reports the version's real state, so an
		// extra announcement would just split the flow.
	}

	printDeployWarnings := func() {
		_, _, warns := snapshotBuildState()
		if len(warns) == 0 {
			return
		}
		warnHeaderStyle := lipgloss.NewStyle().Foreground(theme.Warning).Bold(true)
		ctx.Printf("\n%s\n", warnHeaderStyle.Render("Warnings:"))
		for _, entry := range warns {
			renderDeployWarning(ctx, entry)
		}
	}

	// Wait for the new version to actually become healthy before declaring
	// success, so a broken deploy reports the failure instead of handing out
	// next-steps for an app that isn't serving. On the interactive build path
	// the program is still running, so health renders as the next phase of the
	// same TUI; otherwise we run the plain spinner/line wait. Either way the
	// build warnings and route info come afterward, once stdout is ours and the
	// version's real state is known. Ephemeral deploys don't take over routing,
	// so there's nothing to wait on.
	if ephemeralLabel == "" {
		versionDisplay := ui.DisplayShortID(results.VersionShortId(), results.Version())

		var healthErr error
		if buildProg != nil {
			healthErr = awaitHealthyInProgram(ctx, buildProg, func() *deployInfo {
				buildWG.Wait()
				dm, _ := buildFinalModel.(*deployInfo)
				return dm
			}, name, results.Version(), versionDisplay)
			buildProg = nil // program has quit
		} else {
			healthErr = awaitHealthy(ctx, name, results.Version(), versionDisplay)
		}

		printDeployWarnings()

		if healthErr != nil {
			return healthErr
		}
	} else {
		printDeployWarnings()
	}

	// Show route/access information (the "what's next" guidance) last, once we
	// know the app is actually up.
	displayAccessInfo(ctx, name, results)

	// deploymentId is empty for ephemeral deploys (no deployment record).
	var summaryURLs []string
	if results.HasAccessInfo() && results.AccessInfo() != nil {
		summaryURLs = deployURLs(ctx, results.AccessInfo(), ephemeralLabel)
	}
	writeDeploySummary(ctx, opts.SummaryJSON, getDeploymentID(), appVersionId, summaryURLs)

	return nil
}

// enrichUploadProgress fills in Fraction and ETA on a Progress snapshot using
// the atomic uncompressed-byte counter and the known total uncompressed size.
//
// Fraction is computed against uncompressed source bytes (not compressed bytes
// sent over the wire). We deliberately avoid projecting a compressed total:
// the source-bytes counter and the network-bytes counter are sampled on
// different sides of a gzip buffer plus io.Pipe, so their ratio swings wildly
// under back-pressure and produces a misleading "estimated total."
//
// ETA is extrapolated in the time domain — elapsed * (1 - frac) / frac — which
// avoids the unit-mixing that broke the old compressed-total math. It's only
// emitted after a brief warmup so the first few ticks (when Fraction is near
// zero) don't produce nonsense.
func enrichUploadProgress(p *upload.Progress, written *atomic.Int64, totalUncompressed int64) {
	if totalUncompressed <= 0 {
		return
	}
	p.Fraction = float64(written.Load()) / float64(totalUncompressed)
	if p.Fraction > 0 && p.Fraction < 1 && p.Duration >= 5*time.Second {
		remaining := float64(p.Duration) * (1 - p.Fraction) / p.Fraction
		p.ETA = time.Duration(remaining)
	}
}

// Helper function to print build errors and logs
func printBuildErrors(ctx *Context, buildErrors []string, buildLogs []string) {
	if len(buildErrors) > 0 {
		ctx.Printf("\nErrors:\n")
		for _, errMsg := range buildErrors {
			ctx.Printf("  - %s\n", errMsg)
		}
	}

	if len(buildLogs) > 0 {
		ctx.Printf("\nBuild output:\n")
		for _, log := range buildLogs {
			ctx.Printf("%s\n", log)
		}
	}
}

// safeStatusCh coordinates concurrent Send and Close on a buildkit status
// channel so the build status callback (invoked from RPC stream-handler
// goroutines that can outlive the parent RPC call — see pkg/rpc/client.go
// callInline) cannot race with the deploy command closing the channel.
//
// Close uses a stop channel to wake any in-flight Send rather than holding a
// mutex across the blocking channel send, so Close cannot deadlock even if
// the channel's consumer has stopped draining.
type safeStatusCh struct {
	ch     chan *client.SolveStatus
	stop   chan struct{}
	mu     sync.Mutex
	wg     sync.WaitGroup
	closed bool
}

func newSafeStatusCh(ch chan *client.SolveStatus) *safeStatusCh {
	return &safeStatusCh{ch: ch, stop: make(chan struct{})}
}

func (s *safeStatusCh) Send(ctx context.Context, v *client.SolveStatus) error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.wg.Add(1)
	s.mu.Unlock()
	defer s.wg.Done()

	select {
	case <-s.stop:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case s.ch <- v:
		return nil
	}
}

func (s *safeStatusCh) Close() {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.closed = true
	close(s.stop)
	s.mu.Unlock()

	s.wg.Wait()
	close(s.ch)
}

// maybeReportBlocked prints the rich "deployment blocked" banner when a
// server-owned build failed because it never got its own record — another
// deployment held the lock the whole time (the pre-flight-passed-then-lost-the-
// race case, where the build returns a lock conflict instead of a build error).
//
// It is deliberately gated on ownDeploymentID being empty. Once our build has a
// record, the failure is ours: the server releases our lock on failure, so a
// held lock afterward just means an unrelated deploy started in that window, and
// reporting "blocked" would wrongly hide the compiler/build errors that actually
// failed this deploy. It never suppresses those errors either way — the caller
// prints them regardless; this only adds context when the real cause was
// contention.
func maybeReportBlocked(ctx *Context, depClient *deployment_v1alpha.DeploymentClient, serverOwnsDeployment bool, ownDeploymentID, name string) {
	if !serverOwnsDeployment || ownDeploymentID != "" {
		return
	}
	lockRes, err := depClient.GetDeployLock(ctx, name, ctx.ClusterName)
	if err != nil || !lockRes.Held() || !lockRes.HasLockInfo() || lockRes.LockInfo() == nil {
		return
	}
	printDeploymentBlocked(ctx, lockRes.LockInfo())
}

// printDeploymentBlocked renders the rich "deployment blocked" message shared by
// the server-owned pre-flight and the legacy CreateDeployment path.
func printDeploymentBlocked(ctx *Context, lockInfo *deployment_v1alpha.DeploymentLockInfo) {
	ctx.Printf("\n❌ Deployment blocked:\n\n")
	ctx.Printf("Another deployment is already in progress for app '%s' on cluster '%s'.\n\n",
		lockInfo.AppName(), lockInfo.ClusterId())

	ctx.Printf("Existing deployment details:\n")
	ctx.Printf("  • Deployment ID: %s\n", ui.DisplayShortID(lockInfo.BlockingDeploymentShortId(), lockInfo.BlockingDeploymentId()))
	ctx.Printf("  • Started by: %s\n", lockInfo.StartedBy())

	if lockInfo.HasStartedAt() && lockInfo.StartedAt() != nil {
		startedAt := time.Unix(lockInfo.StartedAt().Seconds(), 0)
		ctx.Printf("  • Started at: %s (%s ago)\n",
			startedAt.Format("2006-01-02 15:04:05 MST"),
			time.Since(startedAt).Round(time.Second))
	}

	ctx.Printf("  • Current phase: %s\n", lockInfo.CurrentPhase())

	if lockInfo.HasLockExpiresAt() && lockInfo.LockExpiresAt() != nil {
		expiresAt := time.Unix(lockInfo.LockExpiresAt().Seconds(), 0)
		timeRemaining := time.Until(expiresAt).Round(time.Second)
		ctx.Printf("  • Lock expires in: %s\n\n", timeRemaining)
	}

	if lockInfo.StartedBy() != "-" {
		ctx.Printf("Please wait for it to complete or contact %s to coordinate.\n", lockInfo.StartedBy())
	} else {
		ctx.Printf("Please wait for it to complete.\n")
	}
}

// buildGitInfoFromGit converts the local git.Info into the build-service GitInfo
// carried on a DeployRequest. Returns nil when no git info is available.
func buildGitInfoFromGit(gitInfo *git.Info) *build_v1alpha.GitInfo {
	if gitInfo == nil {
		return nil
	}

	gi := &build_v1alpha.GitInfo{}
	gi.SetSha(gitInfo.SHA)
	gi.SetBranch(gitInfo.Branch)
	gi.SetIsDirty(gitInfo.IsDirty)
	gi.SetWorkingTreeHash(gitInfo.WorkingTreeHash)
	gi.SetCommitMessage(gitInfo.CommitMessage)
	gi.SetCommitAuthorName(gitInfo.CommitAuthor)
	gi.SetCommitAuthorEmail(gitInfo.CommitEmail)
	gi.SetRepository(gitInfo.RemoteURL)

	if gitInfo.CommitTimestamp != "" {
		if ts, err := time.Parse(time.RFC3339, gitInfo.CommitTimestamp); err == nil {
			gi.SetCommitTimestamp(standard.ToTimestamp(ts))
		}
	}
	return gi
}

// createBuildStatusCallback creates a callback for handling build status updates.
// stateMu must be non-nil and guards buildErrors, buildLogs, and deployWarnings
// — the callback runs from RPC stream-handler goroutines that race with
// readers in Deploy.
func createBuildStatusCallback(
	ctx context.Context,
	updateCh chan<- string,
	buildCh chan<- buildProgress,
	stateMu *sync.Mutex,
	buildErrors *[]string,
	buildLogs *[]string,
	deployWarnings *[]*build_v1alpha.LogEntry,
	progressHandler func(*client.SolveStatus) error,
	onDeployment func(deploymentID, phase string),
) stream.SendStream[*build_v1alpha.Status] {
	vertices := map[string]bool{} // digest → completed
	return stream.Callback(func(su *build_v1alpha.Status) error {
		update := su.Update()

		switch update.Which() {
		case "deployment":
			// The server-owned deployment record announced itself. This arm only
			// ever arrives when the server was handed a DeployRequest (an older
			// server never sends it, and a newer one only emits it for a build it
			// owns), so on the legacy path it is simply never received. onDeployment
			// is non-nil on every path — the guard is defensive only.
			if onDeployment != nil {
				if dp := update.Deployment(); dp != nil {
					onDeployment(dp.DeploymentId(), dp.Phase())
				}
			}
		case "buildkit":
			sj := update.Buildkit()

			var status client.SolveStatus
			if err := json.Unmarshal(sj, &status); err != nil {
				return err
			}

			// Track build step progress via vertices
			if buildCh != nil {
				var updated bool
				for _, v := range status.Vertexes {
					d := v.Digest.String()
					if _, seen := vertices[d]; !seen {
						updated = true
					}
					done := v.Completed != nil
					if done != vertices[d] {
						updated = true
					}
					vertices[d] = done
				}

				if updated {
					var completed int
					for _, done := range vertices {
						if done {
							completed++
						}
					}
					select {
					case <-ctx.Done():
					case buildCh <- buildProgress{total: len(vertices), completed: completed}:
					default:
					}
				}
			}

			// Call the progress handler if provided
			if progressHandler != nil {
				if err := progressHandler(&status); err != nil {
					return err
				}
			}

			stateMu.Lock()
			for _, vertex := range status.Vertexes {
				if vertex.Error != "" {
					*buildErrors = append(*buildErrors, vertex.Error)
				}
			}
			if buildLogs != nil {
				for _, log := range status.Logs {
					if log.Data != nil {
						logStr := strings.TrimSpace(string(log.Data))
						if logStr != "" {
							*buildLogs = append(*buildLogs, logStr)
						}
					}
				}
			}
			errCount := len(*buildErrors)
			stateMu.Unlock()

			if errCount > 0 {
				return fmt.Errorf("build failed with %d error(s)", errCount)
			}

			return nil
		case "message":
			msg := update.Message()
			if updateCh != nil {
				select {
				case updateCh <- msg:
					// sent successfully
				default:
					// drop if UI isn't consuming
				}
			}
		case "error":
			stateMu.Lock()
			*buildErrors = append(*buildErrors, update.Error())
			stateMu.Unlock()
		case "log":
			if entry := update.Log(); entry != nil {
				switch entry.Level() {
				case "warn":
					if deployWarnings != nil {
						stateMu.Lock()
						*deployWarnings = append(*deployWarnings, entry)
						stateMu.Unlock()
					}
				case "info":
					if updateCh != nil {
						select {
						case updateCh <- entry.Text():
						default:
						}
					}
				}
			}
		}

		return nil
	})
}

func renderDeployWarning(ctx *Context, entry *build_v1alpha.LogEntry) {
	headerStyle := lipgloss.NewStyle().Foreground(theme.Warning).Bold(true)
	linkStyle := lipgloss.NewStyle().Foreground(theme.Warning)

	// Compute wrap width: terminal width minus indent (4 chars), capped at 76
	const indent = 4
	const maxWidth = 76
	detailWidth := maxWidth
	if tw := ui.TerminalWidth(); tw > 0 {
		if available := tw - indent; available > 0 {
			detailWidth = min(available, maxWidth)
		}
	}
	detailStyle := lipgloss.NewStyle().Foreground(theme.Warning).Width(detailWidth).PaddingLeft(indent)

	ctx.Printf("  %s\n", headerStyle.Render("⚠ "+entry.Text()))

	// Index fields by key for controlled rendering order
	fields := make(map[string]string)
	for _, f := range entry.Fields() {
		fields[f.Key()] = f.Value()
	}

	if detail, ok := fields["detail"]; ok {
		ctx.Printf("%s\n", detailStyle.Render(detail))
	}
	if link, ok := fields["link"]; ok {
		ctx.Printf("    %s%s\n", linkStyle.Render("See: "), ui.RenderMarkdownLink(link, theme.Warning))
	}
}

func buildStepsSummary(count int) string {
	if count == 0 {
		return "cached"
	}
	if count == 1 {
		return "1 step completed"
	}
	return fmt.Sprintf("%d steps completed", count)
}

// deploySummary is the machine-readable result written by --summary-json. It's
// a side artifact for CI and tooling (e.g. the mirendev/actions deploy action),
// giving consumers the values that are otherwise awkward to recover after a
// deploy without re-parsing human output. deploy_id is empty for ephemeral
// deploys, which have no deployment record.
type deploySummary struct {
	DeployID   string   `json:"deploy_id"`
	AppVersion string   `json:"app_version"`
	URLs       []string `json:"urls"`
}

// accessInfoLike is the subset of the generated AccessInfo types shared by the
// build and deployment flavors, letting deployURLs serve both deploy paths.
type accessInfoLike interface {
	HasHostnames() bool
	Hostnames() *[]string
	DefaultRoute() bool
	ClusterHostname() string
}

// deployURLs derives the app's reachable https URLs from the server-provided
// access info, mirroring what the human-readable deploy output prints. It is the
// authoritative source for route URLs, so tooling doesn't have to scrape the
// deploy log or reconcile a separate `route list` call.
func deployURLs(ctx *Context, accessInfo accessInfoLike, ephemeralLabel string) []string {
	if accessInfo == nil {
		return nil
	}

	var urls []string
	seen := map[string]bool{}
	add := func(u string) {
		if u != "" && !seen[u] {
			seen[u] = true
			urls = append(urls, u)
		}
	}

	if accessInfo.HasHostnames() && accessInfo.Hostnames() != nil {
		for _, h := range *accessInfo.Hostnames() {
			if h != "" {
				add("https://" + h)
			}
		}
	}

	if ephemeralLabel != "" {
		// Ephemeral previews are additionally reachable at <label>.<cluster-host>.
		if accessInfo.ClusterHostname() != "" {
			add("https://" + ephemeralLabel + "." + accessInfo.ClusterHostname())
		}
	} else if len(urls) == 0 && accessInfo.DefaultRoute() {
		// A stable deploy with no explicit route is the default route, reachable
		// at the cluster hostname.
		addr := accessInfo.ClusterHostname()
		if addr == "" && ctx.ClusterConfig != nil {
			addr = stripPort(ctx.ClusterConfig.Hostname)
		}
		if addr != "" {
			add("https://" + addr)
		}
	}

	return urls
}

// writeDeploySummary writes the deploy summary to path (a no-op if path is
// empty). It runs after the deploy has already succeeded, so a failure here must
// never fail the command: it is logged and swallowed.
func writeDeploySummary(ctx *Context, path, deployID, appVersion string, urls []string) {
	if path == "" {
		return
	}
	// Keep urls a stable array (never JSON null) so consumers see a fixed schema.
	if urls == nil {
		urls = []string{}
	}
	summary := deploySummary{
		DeployID:   deployID,
		AppVersion: appVersion,
		URLs:       urls,
	}
	data, err := json.MarshalIndent(summary, "", "  ")
	if err != nil {
		ctx.Log.Error("Failed to marshal deploy summary", "error", err)
		return
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o644); err != nil {
		ctx.Log.Error("Failed to write deploy summary", "path", path, "error", err)
		return
	}
	ctx.Log.Debug("Wrote deploy summary", "path", path, "deploy_id", deployID, "app_version", appVersion, "urls", urls)
}

// deployAccessInfo provides access to build result access info for display purposes.
type deployAccessInfo interface {
	HasAccessInfo() bool
	AccessInfo() *build_v1alpha.AccessInfo
}

// displayAccessInfo shows how to access the deployed app using server-provided access info
func displayAccessInfo(ctx *Context, appName string, results deployAccessInfo) {
	// Check if we have access info from the server
	if !results.HasAccessInfo() {
		ctx.Log.Debug("No access info returned from server")
		return
	}

	accessInfo := results.AccessInfo()

	// Get hostnames and default route status from server
	var hostnames []string
	if accessInfo.HasHostnames() && accessInfo.Hostnames() != nil {
		hostnames = *accessInfo.Hostnames()
	}
	hasDefaultRoute := accessInfo.DefaultRoute()

	// Get cluster address for default route display
	// Prefer the cloud-provisioned DNS hostname from the server if available
	var clusterAddr string
	if accessInfo.ClusterHostname() != "" {
		// Use the cloud-provisioned DNS hostname (e.g., cluster-abc.org-123.miren.systems)
		clusterAddr = accessInfo.ClusterHostname()
	} else if ctx.ClusterConfig != nil && ctx.ClusterConfig.Hostname != "" {
		// Fall back to the client's cluster address
		// Strip any API port (e.g. :8443) since HTTP ingress is on 443
		clusterAddr = stripPort(ctx.ClusterConfig.Hostname)
	}

	// Display access information
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

// suggestRoute prints a hint for giving the app a hostname. It intentionally
// does not synthesize a subdomain of the cluster's own hostname (e.g.
// app.cluster-x.miren.systems): Miren Anywhere serves the cluster apex but not
// subdomains beneath it, so such a hostname would not route today. See MIR-1450.
func suggestRoute(ctx *Context, appName string) {
	ctx.Printf("To set a hostname, try: miren route set <hostname> %s\n", appName)
}

// stripPort removes any port suffix from a host string
func stripPort(host string) string {
	if idx := strings.LastIndex(host, ":"); idx != -1 {
		return host[:idx]
	}
	return host
}

// Styles for analyze output
var (
	analyzeTitleStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(theme.Header) // yellow

	analyzeLabelStyle = lipgloss.NewStyle().
				Foreground(theme.Muted).
				Width(12).
				Align(lipgloss.Right)

	analyzeValueStyle = lipgloss.NewStyle().
				Bold(true)

	// Badge styles for different event kinds
	badgeFile = lipgloss.NewStyle().
			Foreground(theme.Info).
			Bold(true)
	badgePackage = lipgloss.NewStyle().
			Foreground(theme.Success).
			Bold(true)
	badgeFramework = lipgloss.NewStyle().
			Foreground(theme.Highlight).
			Bold(true)
	badgeConfig = lipgloss.NewStyle().
			Foreground(theme.Warning).
			Bold(true)
	// dir shares Info with file, and script shares Warning with config; italic
	// keeps each pair visually distinct now that the palette is role-based.
	badgeDir = lipgloss.NewStyle().
			Foreground(theme.Info).
			Bold(true).
			Italic(true)
	badgeScript = lipgloss.NewStyle().
			Foreground(theme.Warning).
			Bold(true).
			Italic(true)
)

func eventKindBadge(kind string) string {
	badge := fmt.Sprintf("[%s]", kind)
	switch kind {
	case "file":
		return badgeFile.Render(badge)
	case "package":
		return badgePackage.Render(badge)
	case "framework":
		return badgeFramework.Render(badge)
	case "config":
		return badgeConfig.Render(badge)
	case "dir":
		return badgeDir.Render(badge)
	case "script":
		return badgeScript.Render(badge)
	default:
		return lipgloss.NewStyle().Foreground(theme.Muted).Render(badge)
	}
}

// analyzeApp calls the AnalyzeApp API and displays the results
func analyzeApp(ctx *Context, bc *build_v1alpha.BuilderClient, dir string) error {
	if dir == "" || dir == "." {
		var err error
		dir, err = os.Getwd()
		if err != nil {
			return fmt.Errorf("failed to get current directory: %w", err)
		}
	}

	ctx.Printf("Analyzing app in %s...\n\n", dir)

	// Load AppConfig to get include patterns
	var includePatterns []string
	ac, err := appconfig.LoadAppConfigUnder(dir)
	if err != nil {
		return fmt.Errorf("failed to load app config: %w", err)
	}
	if ac != nil && ac.Include != nil {
		for _, pattern := range ac.Include {
			if err := tarx.ValidatePattern(pattern); err != nil {
				return fmt.Errorf("invalid include pattern %q: %w", pattern, err)
			}
		}
		includePatterns = ac.Include
	}

	r, err := tarx.MakeTar(dir, includePatterns, nil)
	if err != nil {
		return fmt.Errorf("failed to create tar: %w", err)
	}

	defer r.Close()

	result, err := bc.AnalyzeApp(ctx, stream.ServeReader(ctx, r, stream.WithBulkBatching()))
	if err != nil {
		return fmt.Errorf("analysis failed: %w", err)
	}

	analysisResult := result.Result()
	if analysisResult == nil {
		return fmt.Errorf("no analysis result returned")
	}

	// Stack
	ctx.Printf("%s %s\n",
		analyzeLabelStyle.Render("Stack:"),
		analyzeValueStyle.Render(analysisResult.Stack()))

	// App name (if from app.toml)
	if analysisResult.AppName() != "" {
		ctx.Printf("%s %s\n",
			analyzeLabelStyle.Render("App Name:"),
			analyzeValueStyle.Render(analysisResult.AppName()))
	}

	// Working directory
	ctx.Printf("%s %s\n",
		analyzeLabelStyle.Render("Directory:"),
		analysisResult.WorkingDir())

	// Entrypoint
	if analysisResult.Entrypoint() != "" {
		ctx.Printf("%s %s\n",
			analyzeLabelStyle.Render("Entrypoint:"),
			analyzeValueStyle.Render(analysisResult.Entrypoint()))
	}

	// Dockerfile (if using dockerfile stack)
	if analysisResult.BuildDockerfile() != "" {
		ctx.Printf("%s %s\n",
			analyzeLabelStyle.Render("Dockerfile:"),
			analysisResult.BuildDockerfile())
	}

	// Services
	if analysisResult.HasServices() && analysisResult.Services() != nil {
		services := *analysisResult.Services()
		if len(services) > 0 {
			ctx.Printf("\n%s\n", analyzeTitleStyle.Render("Services"))
			for _, svc := range services {
				sourceInfo := ""
				if svc.Source() != "" {
					sourceInfo = lipgloss.NewStyle().Foreground(theme.Muted).Render(fmt.Sprintf(" (%s)", svc.Source()))
				}

				command := svc.Command()
				if command == "" {
					// Service uses Dockerfile CMD (image default)
					command = lipgloss.NewStyle().Foreground(theme.Muted).Italic(true).Render("image default")
				}

				ctx.Printf("  %s: %s%s\n",
					analyzeValueStyle.Render(svc.Name()),
					command,
					sourceInfo)
			}
		}
	}

	// Environment variables with local detection
	if analysisResult.HasEnvVars() && analysisResult.EnvVars() != nil {
		envVars := *analysisResult.EnvVars()
		if len(envVars) > 0 {
			// Cross-reference with local environment
			localDetection := DetectLocalEnvVars(envVars)

			ctx.Printf("\n%s\n", analyzeTitleStyle.Render("Environment Variables"))

			// Show available (detected + found locally)
			if len(localDetection.Available) > 0 {
				ctx.Printf("  %s\n", lipgloss.NewStyle().Foreground(theme.Success).Render("Available locally:"))
				for _, ev := range localDetection.Available {
					valueDisplay := maskEnvValue(ev.Value, ev.Sensitive, false)
					if ev.Sensitive {
						ctx.Printf("    %s %s=%s\n",
							lipgloss.NewStyle().Foreground(theme.Success).Render("✓"),
							ev.Key,
							lipgloss.NewStyle().Foreground(theme.Muted).Render(valueDisplay))
					} else {
						ctx.Printf("    %s %s=%s\n",
							lipgloss.NewStyle().Foreground(theme.Success).Render("✓"),
							ev.Key,
							valueDisplay)
					}
				}
			}

			// Show missing (detected but not found locally)
			if len(localDetection.Missing) > 0 {
				ctx.Printf("  %s\n", lipgloss.NewStyle().Foreground(theme.Warning).Render("Not set locally:"))
				for _, ev := range localDetection.Missing {
					ctx.Printf("    %s %s\n",
						lipgloss.NewStyle().Foreground(theme.Warning).Render("○"),
						ev.Key)
				}
			}

			// Show additional app-related env vars found locally
			if len(localDetection.Additional) > 0 {
				ctx.Printf("  %s\n", lipgloss.NewStyle().Foreground(theme.Info).Render("Also found locally (may be relevant):"))
				for _, ev := range localDetection.Additional {
					valueDisplay := maskEnvValue(ev.Value, ev.Sensitive, false)
					if ev.Sensitive {
						ctx.Printf("    %s %s=%s\n",
							lipgloss.NewStyle().Foreground(theme.Info).Render("?"),
							ev.Key,
							lipgloss.NewStyle().Foreground(theme.Muted).Render(valueDisplay))
					} else {
						ctx.Printf("    %s %s=%s\n",
							lipgloss.NewStyle().Foreground(theme.Info).Render("?"),
							ev.Key,
							valueDisplay)
					}
				}
			}
		}
	} else {
		// No detected env vars - still scan local environment for suggestions
		localDetection := DetectLocalEnvVars(nil)
		if len(localDetection.Additional) > 0 {
			ctx.Printf("\n%s\n", analyzeTitleStyle.Render("Environment Variables"))
			ctx.Printf("  %s\n", lipgloss.NewStyle().Foreground(theme.Info).Render("Found locally (may be relevant):"))
			for _, ev := range localDetection.Additional {
				valueDisplay := maskEnvValue(ev.Value, ev.Sensitive, false)
				if ev.Sensitive {
					ctx.Printf("    %s %s=%s\n",
						lipgloss.NewStyle().Foreground(theme.Info).Render("?"),
						ev.Key,
						lipgloss.NewStyle().Foreground(theme.Muted).Render(valueDisplay))
				} else {
					ctx.Printf("    %s %s=%s\n",
						lipgloss.NewStyle().Foreground(theme.Info).Render("?"),
						ev.Key,
						valueDisplay)
				}
			}
		}
	}

	// Detection events
	if analysisResult.HasEvents() && analysisResult.Events() != nil {
		events := *analysisResult.Events()
		if len(events) > 0 {
			ctx.Printf("\n%s\n", analyzeTitleStyle.Render("Detection"))
			for _, event := range events {
				badge := eventKindBadge(event.Kind())
				if event.Name() != "" {
					ctx.Printf("  %s %s: %s\n",
						badge,
						analyzeValueStyle.Render(event.Name()),
						event.Message())
				} else {
					ctx.Printf("  %s %s\n", badge, event.Message())
				}
			}
		}
	}

	return nil
}
