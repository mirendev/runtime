// Package secret serves the cluster's secret store over RPC.
//
// It is the only path by which a value enters or leaves the store. Storing and
// resolving both happen here, where the keyring lives, so a client never holds
// key material — and a node that holds none can still materialize a secret by
// asking for it.
package secret

import (
	"context"
	"fmt"
	"log/slog"

	"miren.dev/runtime/api/secret/secret_v1alpha"
	"miren.dev/runtime/pkg/secret"
)

// Server implements the secret_v1alpha.Secrets RPC interface over a backend
// registry.
type Server struct {
	log      *slog.Logger
	registry *secret.Registry
	rotator  KeyRotator
}

var _ secret_v1alpha.Secrets = (*Server)(nil)

// NewServer builds the secrets RPC server.
func NewServer(log *slog.Logger, registry *secret.Registry, rotator KeyRotator) *Server {
	return &Server{
		log:      log.With("module", "secrets-rpc"),
		registry: registry,
		rotator:  rotator,
	}
}

// Set stores a new version of a secret and returns its handle.
//
// A value identical to the current version reports back as unchanged rather
// than minting a duplicate, so re-running the same command does not invalidate
// every pin.
func (s *Server) Set(ctx context.Context, state *secret_v1alpha.SecretsSet) error {
	args := state.Args()

	path := args.Path()
	if path == "" {
		return fmt.Errorf("secret path is required")
	}
	value := args.Value()
	if len(value) == 0 {
		return fmt.Errorf("secret value is empty")
	}

	backendName := backendOrDefault(args.Backend())
	backend, err := s.registry.Writable(backendName)
	if err != nil {
		return err
	}

	// Whether the write moved anything comes from the write itself. Reading the
	// current version beforehand and comparing handles looks equivalent and is
	// not: the read can be stale by the time the write lands, and it decrypts
	// where the reuse check does not — so an enabled version this server cannot
	// decrypt would be reused by Put and reported as a fresh one here.
	version, reused, err := backend.Put(ctx, path, value)
	if err != nil {
		return err
	}

	s.log.Info("stored secret version",
		"backend", backendName, "path", path, "version", version, "unchanged", reused)

	state.Results().SetVersion(version)
	state.Results().SetUnchanged(reused)
	return nil
}

// List returns every secret a backend holds, without their values.
func (s *Server) List(ctx context.Context, state *secret_v1alpha.SecretsList) error {
	backendName := backendOrDefault(state.Args().Backend())

	backend, err := s.listable(backendName)
	if err != nil {
		return err
	}

	summaries, err := backend.List(ctx)
	if err != nil {
		return err
	}

	infos := make([]*secret_v1alpha.SecretInfo, 0, len(summaries))
	for _, summary := range summaries {
		infos = append(infos, encodeSummary(summary))
	}

	state.Results().SetSecrets(infos)
	return nil
}

// ListVersions returns one secret's versions, without their values.
func (s *Server) ListVersions(ctx context.Context, state *secret_v1alpha.SecretsListVersions) error {
	args := state.Args()

	path := args.Path()
	if path == "" {
		return fmt.Errorf("secret path is required")
	}

	backend, err := s.listable(backendOrDefault(args.Backend()))
	if err != nil {
		return err
	}

	summary, err := backend.ListVersions(ctx, path)
	if err != nil {
		return err
	}

	state.Results().SetSecret(encodeSummary(summary))
	return nil
}

// SetState transitions one version between enabled, disabled and destroyed.
func (s *Server) SetState(ctx context.Context, state *secret_v1alpha.SecretsSetState) error {
	args := state.Args()

	ref := args.Ref()
	if ref == "" {
		return fmt.Errorf("secret reference is required")
	}

	versionState, err := parseState(args.State())
	if err != nil {
		return err
	}

	backendName := backendOrDefault(args.Backend())
	backend, err := s.registry.Writable(backendName)
	if err != nil {
		return err
	}

	if err := backend.SetState(ctx, ref, versionState); err != nil {
		return err
	}

	// A revocation is what an operator goes looking for after an incident, so it
	// belongs at Warn. Enabling a version is an ordinary healthy transition and
	// would only dilute that signal.
	record := s.log.Warn
	if versionState == secret.StateEnabled {
		record = s.log.Info
	}
	record("changed secret version state",
		"backend", backendName, "ref", ref, "state", versionState)
	return nil
}

// listable resolves a backend that can enumerate what it holds.
func (s *Server) listable(name string) (secret.ListableBackend, error) {
	backend, ok := s.registry.Get(name)
	if !ok {
		return nil, fmt.Errorf("%w: %q", secret.ErrUnknownBackend, name)
	}

	listable, ok := backend.(secret.ListableBackend)
	if !ok {
		return nil, fmt.Errorf("backend %q does not support listing", name)
	}
	return listable, nil
}

// backendOrDefault falls back to the built-in cluster store, which is always
// registered, so the common case needs no --backend flag.
func backendOrDefault(name string) string {
	if name == "" {
		return secret.ClusterBackendName
	}
	return name
}

func parseState(s string) (secret.VersionState, error) {
	switch secret.VersionState(s) {
	case secret.StateEnabled:
		return secret.StateEnabled, nil
	case secret.StateDisabled:
		return secret.StateDisabled, nil
	case secret.StateDestroyed:
		return secret.StateDestroyed, nil
	}
	return "", fmt.Errorf("unknown secret version state %q, want enabled, disabled or destroyed", s)
}

func encodeSummary(summary secret.Summary) *secret_v1alpha.SecretInfo {
	info := &secret_v1alpha.SecretInfo{}
	info.SetPath(summary.Path)
	info.SetBackend(summary.Backend)
	info.SetCurrentVersion(summary.CurrentVersion)

	versions := make([]*secret_v1alpha.SecretVersionInfo, 0, len(summary.Versions))
	for _, v := range summary.Versions {
		vi := &secret_v1alpha.SecretVersionInfo{}
		vi.SetVersion(v.Version)
		vi.SetState(string(v.State))
		vi.SetCreatedAt(v.CreatedAt.UnixMilli())
		vi.SetCurrent(v.Current)
		versions = append(versions, vi)
	}
	info.SetVersions(versions)

	return info
}

// KeyRotator starts and reports on cluster-key rotations. The server holds it
// as an interface so the secrets service does not depend on the controller
// package, and so a cluster without rotation wired up simply has none.
type KeyRotator interface {
	// Begin starts a rotation now, failing if one is already in flight.
	Begin(ctx context.Context) error
}

// Keyring reports the cluster keyring and any rotation in flight.
//
// This is the only view an operator has of rotation. Without it a stalled
// backfill and a finished one look identical, and there is no way to tell
// whether retiring the old key is safe yet.
func (s *Server) Keyring(ctx context.Context, state *secret_v1alpha.SecretsKeyring) error {
	backend, ok := s.registry.Get(secret.ClusterBackendName)
	if !ok {
		return fmt.Errorf("%w: %q", secret.ErrUnknownBackend, secret.ClusterBackendName)
	}

	reporter, ok := backend.(secret.KeyringReporter)
	if !ok {
		return fmt.Errorf("backend %q does not hold a keyring", secret.ClusterBackendName)
	}

	report, err := reporter.KeyringReport(ctx)
	if err != nil {
		return err
	}

	keys := make([]*secret_v1alpha.KeyInfo, 0, len(report.Keys))
	for _, k := range report.Keys {
		ki := &secret_v1alpha.KeyInfo{}
		ki.SetId(k.ID)
		ki.SetCurrent(k.Current)
		if !k.CreatedAt.IsZero() {
			ki.SetCreatedAt(k.CreatedAt.UnixMilli())
		}
		ki.SetVersions(int64(k.Versions))
		keys = append(keys, ki)
	}

	state.Results().SetKeys(keys)
	state.Results().SetRotating(report.Rotating)
	state.Results().SetRotatingFrom(report.RotatingFrom)
	state.Results().SetRewrapped(int64(report.Rewrapped))
	return nil
}

// RotateKey starts a rotation now.
func (s *Server) RotateKey(ctx context.Context, state *secret_v1alpha.SecretsRotateKey) error {
	if s.rotator == nil {
		return fmt.Errorf("key rotation is not enabled on this cluster")
	}

	backend, ok := s.registry.Get(secret.ClusterBackendName)
	if !ok {
		return fmt.Errorf("%w: %q", secret.ErrUnknownBackend, secret.ClusterBackendName)
	}
	reporter, ok := backend.(secret.KeyringReporter)
	if !ok {
		return fmt.Errorf("backend %q does not hold a keyring", secret.ClusterBackendName)
	}

	before, err := reporter.KeyringReport(ctx)
	if err != nil {
		return err
	}
	var from string
	for _, k := range before.Keys {
		if k.Current {
			from = k.ID
		}
	}

	if err := s.rotator.Begin(ctx); err != nil {
		return err
	}

	after, err := reporter.KeyringReport(ctx)
	if err != nil {
		return err
	}
	var to string
	for _, k := range after.Keys {
		if k.Current {
			to = k.ID
		}
	}

	// Rotation is exactly the kind of thing an operator goes looking for
	// afterwards, so it belongs in the record.
	s.log.Warn("started cluster key rotation", "from_key", from, "to_key", to)

	state.Results().SetFromKey(from)
	state.Results().SetToKey(to)
	return nil
}
