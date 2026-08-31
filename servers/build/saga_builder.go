package build

import (
	"context"
	"fmt"
	"log/slog"

	"miren.dev/runtime/api/build/build_v1alpha"
	"miren.dev/runtime/api/core/core_v1alpha"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/idgen"
	"miren.dev/runtime/pkg/rpc"
	"miren.dev/runtime/pkg/rpc/stream"
	"miren.dev/runtime/pkg/saga"
	"miren.dev/runtime/pkg/tarx"
)

// buildArgs abstracts the shape both BuildFromTarArgs and
// BuildFromPreparedArgs share. Lets the saga entry points share the
// "pull initial inputs from args" logic.
type buildArgs interface {
	EnvVars() []*build_v1alpha.EnvironmentVariable
	HasEphemeralLabel() bool
	EphemeralLabel() string
	HasEphemeralTtl() bool
	EphemeralTtl() string
}

// buildResults is the shared shape of both Results types we set on
// completion. Generated types don't share an interface, so we declare
// the minimum surface here.
type buildResults interface {
	SetVersion(string)
	SetVersionShortId(string)
	SetAccessInfo(**build_v1alpha.AccessInfo)
}

// SagaBuilder is the saga-backed implementation of the build_v1alpha
// Builder RPC interface. It wraps the existing *Builder (which keeps
// shared collaborators like the entity client, app client, BuildKit
// component, and source cache locks), adds a per-instance saga
// executor + registry, and exposes BuildFromTar / BuildFromPrepared
// implementations that drive the build-from-tar saga end to end.
//
// PrepareUpload and AnalyzeApp delegate straight through to the inner
// Builder. Prepare is just session management, not a build, and
// AnalyzeApp is read-only — neither benefits from saga durability.
type SagaBuilder struct {
	inner    *Builder
	executor *saga.Executor
	registry *saga.Registry
	streams  *StreamRegistry
	statuses *StatusRegistry
	log      *slog.Logger
}

// NewSagaBuilder constructs a SagaBuilder over an existing Builder. The
// caller still owns the Builder's collaborators (clients, log writer,
// BuildKit component); SagaBuilder layers saga execution + crash
// recovery on top without forking the underlying state.
func NewSagaBuilder(inner *Builder, sagaStorage saga.Storage, log *slog.Logger) *SagaBuilder {
	registry := saga.NewRegistry()
	executor := saga.NewExecutor(
		sagaStorage,
		saga.WithRegistry(registry),
		saga.WithLogger(log.With("module", "saga-build")),
	)
	streams := NewStreamRegistry(inner.TempDir, log)
	statuses := NewStatusRegistry()
	return &SagaBuilder{
		inner:    inner,
		executor: executor,
		registry: registry,
		streams:  streams,
		statuses: statuses,
		log:      log.With("module", "saga-builder"),
	}
}

// Init registers the build-from-tar saga definition with the executor.
// It must be called once at startup, before serving RPC requests, so new
// builds can run. It deliberately does not recover in-flight sagas:
// recovery resumes a build's image push, which needs the cluster
// registry and the cluster.local name mapping, and those are established
// late in boot (after the coordinator's own startup). Recovery is split
// into Recover and driven from the boot sequence once those dependencies
// exist. See MIR-1285.
func (s *SagaBuilder) Init() error {
	if err := registerBuildSaga(s.registry, s.inner, s.streams, s.statuses, s.log); err != nil {
		return fmt.Errorf("registering build-from-tar saga: %w", err)
	}
	return nil
}

// Recover resumes any incomplete sagas left over from a previous
// process. It must run only after a resumed build's runtime dependencies
// (the registry listener and the cluster.local name mapping) are ready;
// running it mid-boot resumes the image push before those exist, so the
// build fails and rolls back (MIR-1285). The error is returned for the
// caller to log; recovery failures are not fatal, since new build
// requests can still be served.
func (s *SagaBuilder) Recover(ctx context.Context) error {
	return s.executor.Recover(ctx)
}

// BuildFromTar is the saga-backed implementation of the RPC method.
// Stream registration, status sender registration, and saga start all
// key off the same generated stream ID so the saga's actions can find
// the live reader and status sink by ID alone.
func (s *SagaBuilder) BuildFromTar(ctx context.Context, state *build_v1alpha.BuilderBuildFromTar) (retErr error) {
	args := state.Args()
	name := args.Application()
	if !rpc.AllowApp(ctx, name) {
		return rpc.AppAccessError(ctx, name)
	}
	work := newDeploymentContext(ctx)
	defer work.Close()
	defer func() { retErr = work.result(retErr) }()

	streamID := idgen.Gen("bt")
	status := args.Status()
	sender := NewRPCStatusSender(status, s.inner.Log)
	s.statuses.Register(streamID, sender)
	defer s.statuses.Unregister(streamID)

	tardata := args.Tardata()
	s.streams.Register(streamID, stream.ToReader(ctx, tardata))
	// streams.Cleanup is idempotent and finalize calls it on success;
	// the deferred call covers crash paths between Execute return and
	// the next request.
	defer func() {
		if err := s.streams.Cleanup(streamID); err != nil {
			s.log.Warn("post-saga stream cleanup", "stream", streamID, "error", err)
		}
	}()

	var deployReq *build_v1alpha.DeployRequest
	if args.HasDeployment() {
		deployReq = args.Deployment()
	}

	executionID := "build-from-tar-" + streamID
	if err := s.startBuild(work.control, work.action, executionID, name, streamID, args.EnvVars(), ephemeralFromArgs(args), deployReq); err != nil {
		return err
	}

	return s.populateResults(work.control, executionID, name, state.Results(), ephemeralLabelFromArgs(args))
}

// BuildFromPrepared mirrors BuildFromTar but starts from an upload
// session whose source tree is already on disk. The session lookup
// stays out of the saga (it's a pre-flight check, not a step we'd
// want to retry on recovery), then MarkStaged tells the registry the
// path is ready so the saga's receive-tar action returns it directly.
func (s *SagaBuilder) BuildFromPrepared(ctx context.Context, state *build_v1alpha.BuilderBuildFromPrepared) (retErr error) {
	args := state.Args()
	sessionID := args.SessionId()

	val, ok := s.inner.sessions.LoadAndDelete(sessionID)
	if !ok {
		return fmt.Errorf("unknown or expired upload session: %s", sessionID)
	}
	sess := val.(*buildSession)
	sess.cancelFunc()

	name := sess.appName
	if !rpc.AllowApp(ctx, name) {
		return rpc.AppAccessError(ctx, name)
	}
	work := newDeploymentContext(ctx)
	defer work.Close()
	defer func() { retErr = work.result(retErr) }()

	status := args.Status()
	sender := NewRPCStatusSender(status, s.inner.Log)
	s.statuses.Register(sessionID, sender)
	defer s.statuses.Unregister(sessionID)

	// If the client is sending an incremental tar of changed files,
	// extract it into the session dir before the saga starts. This
	// matches the pre-saga flow and keeps the receive-tar action
	// uniform (it just sees a populated directory).
	if td := args.Tardata(); td != nil {
		sender.SendMessage("Receiving changed files")
		if err := extractTarIntoDir(ctx, td, sess.dir); err != nil {
			sender.SendError("Error extracting changed files: %v", err)
			return fmt.Errorf("error extracting changed files: %w", err)
		}
	}

	if s.inner.DataPath != "" {
		cache := &sourceCache{dataPath: s.inner.DataPath, log: s.inner.Log, locks: s.inner.cacheLocks}
		if err := cache.saveSourceImage(name, sess.dir); err != nil {
			s.inner.Log.Warn("failed to save source code cache", "error", err, "app", name)
		}
	}

	s.streams.MarkStaged(sessionID, sess.dir)
	defer func() {
		if err := s.streams.Cleanup(sessionID); err != nil {
			s.log.Warn("post-saga stream cleanup", "stream", sessionID, "error", err)
		}
	}()

	var deployReq *build_v1alpha.DeployRequest
	if args.HasDeployment() {
		deployReq = args.Deployment()
	}

	executionID := "build-from-prepared-" + sessionID
	if err := s.startBuild(work.control, work.action, executionID, name, sessionID, args.EnvVars(), ephemeralFromArgs(args), deployReq); err != nil {
		return err
	}

	return s.populateResults(work.control, executionID, name, state.Results(), ephemeralLabelFromArgs(args))
}

// PrepareUpload is a session-management op, not a build, so it routes
// straight to the inner Builder.
func (s *SagaBuilder) PrepareUpload(ctx context.Context, state *build_v1alpha.BuilderPrepareUpload) error {
	return s.inner.PrepareUpload(ctx, state)
}

// AnalyzeApp is read-only.
func (s *SagaBuilder) AnalyzeApp(ctx context.Context, state *build_v1alpha.BuilderAnalyzeApp) error {
	return s.inner.AnalyzeApp(ctx, state)
}

// startBuild fans out the initial inputs and runs the saga.
func (s *SagaBuilder) startBuild(
	ctx, actionCtx context.Context,
	executionID, appName, streamID string,
	cliEnvVars []*build_v1alpha.EnvironmentVariable,
	eph *ephemeralOpts,
	deployReq *build_v1alpha.DeployRequest,
) error {
	sb := s.executor.Start(sagaBuildFromTar).
		Input("app_name", appName).
		Input("stream_id", streamID).
		WithActionContext(actionCtx).
		WithID(executionID)

	if len(cliEnvVars) > 0 {
		sb = sb.Input("cli_env_vars", cliEnvVars)
	}
	if eph != nil {
		sb = sb.Input("ephemeral_label", eph.label)
		if eph.ttl != "" {
			sb = sb.Input("ephemeral_ttl", eph.ttl)
		}
	}

	// Every non-ephemeral build is an attempt. Seed authenticated identity into
	// the durable saga inputs because a recovered action no longer has the
	// initiating RPC context.
	if eph == nil || eph.label == "" {
		if deployReq != nil {
			sb = sb.Input("deploy_cluster_id", deployReq.ClusterId())
			if gitJSON := marshalDeployGitInfo(deployReq); gitJSON != "" {
				sb = sb.Input("deploy_git_info_json", gitJSON)
			}
		}
		if identity := rpc.IdentityFromContext(ctx); identity != nil && identity.Method != rpc.AuthMethodAnonymous {
			sb = sb.Input("deploy_subject", identity.Subject).
				Input("deploy_auth_method", string(identity.Method))
		}
	}

	if err := sb.Execute(ctx); err != nil {
		return fmt.Errorf("build saga: %w", err)
	}
	return nil
}

// populateResults fishes the saga's outputs back out of storage to
// populate the RPC response. The version short ID and access info
// stay outside the saga since they're cheap reads against entities
// the saga already created.
func (s *SagaBuilder) populateResults(
	ctx context.Context,
	executionID, appName string,
	results buildResults,
	ephemeralLabel string,
) error {
	out, err := s.executor.ExecutionOutputs(ctx, executionID)
	if err != nil {
		return fmt.Errorf("loading saga outputs for %s: %w", executionID, err)
	}

	var versionName string
	if err := out.Get("version_name", &versionName); err != nil {
		return fmt.Errorf("reading version_name from saga: %w", err)
	}
	results.SetVersion(versionName)

	var appVersionID string
	_ = out.Get("app_version_id", &appVersionID)
	if appVersionID != "" {
		if shortID, ok := s.lookupVersionShortID(ctx, appVersionID); ok {
			results.SetVersionShortId(shortID)
		}
	}

	accessInfo := s.inner.getAccessInfo(ctx, appName, ephemeralLabel)
	results.SetAccessInfo(&accessInfo)
	return nil
}

// lookupVersionShortID fetches the short ID for a created AppVersion
// entity. Returns false if the entity isn't found or doesn't have a
// short ID attribute — callers should fall back to omitting the field.
func (s *SagaBuilder) lookupVersionShortID(ctx context.Context, appVersionID string) (string, bool) {
	var av core_v1alpha.AppVersion
	ent, err := s.inner.ec.GetByIdWithEntity(ctx, entity.Id(appVersionID), &av)
	if err != nil {
		return "", false
	}
	for _, attr := range ent.Attrs() {
		if entity.Id(attr.ID) == entity.DBShortId {
			return attr.Value.String(), true
		}
	}
	return "", false
}

// ephemeralFromArgs builds the ephemeralOpts the saga needs from
// either BuildFromTar or BuildFromPrepared args. Returns nil when no
// label is present — the saga treats nil as "regular deploy".
func ephemeralFromArgs(args buildArgs) *ephemeralOpts {
	if !args.HasEphemeralLabel() || args.EphemeralLabel() == "" {
		return nil
	}
	ttl := "24h"
	if args.HasEphemeralTtl() && args.EphemeralTtl() != "" {
		ttl = args.EphemeralTtl()
	}
	return &ephemeralOpts{label: args.EphemeralLabel(), ttl: ttl}
}

// ephemeralLabelFromArgs returns just the label string, "" for
// non-ephemeral deploys. Used for the post-saga access-info lookup
// where the label affects route resolution.
func ephemeralLabelFromArgs(args buildArgs) string {
	if !args.HasEphemeralLabel() {
		return ""
	}
	return args.EphemeralLabel()
}

// extractTarIntoDir reads the RPC tar stream into dir, the same shape
// BuildFromPrepared's pre-saga path uses for incremental uploads.
func extractTarIntoDir(ctx context.Context, td *stream.RecvStreamClient[[]byte], dir string) error {
	r := stream.ToReader(ctx, td)
	_, err := tarx.TarFS(r, dir)
	return err
}
