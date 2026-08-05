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
}

var _ secret_v1alpha.Secrets = (*Server)(nil)

// NewServer builds the secrets RPC server.
func NewServer(log *slog.Logger, registry *secret.Registry) *Server {
	return &Server{
		log:      log.With("module", "secrets-rpc"),
		registry: registry,
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

	// Read the current version first so the caller can be told whether their
	// write actually moved anything. Put is idempotent for an unchanged value,
	// but only the comparison here can distinguish the two outcomes.
	var before string
	if existing, err := backend.Resolve(ctx, path); err == nil {
		_, before, _ = secret.ParseRef(existing.Ref)
	}

	version, err := backend.Put(ctx, path, value)
	if err != nil {
		return err
	}

	s.log.Info("stored secret version",
		"backend", backendName, "path", path, "version", version, "unchanged", version == before)

	state.Results().SetVersion(version)
	state.Results().SetUnchanged(version == before)
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
