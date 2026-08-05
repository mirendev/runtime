// Package secret models secrets that are referenced rather than copied.
//
// A reference names the backend a secret lives in and a backend-relative path
// to the value. Resolving a reference produces the bytes in memory and the
// fully-qualified reference — always carrying a concrete version — that
// produced them. Callers materialize those bytes into a process environment or
// an in-memory file and never persist them.
//
// The backend interface is deliberately small so that the in-cluster store and
// external managers (Vault, AWS Secrets Manager, GCP Secret Manager) look the
// same to every consumer. Only backends Miren itself manages implement
// WritableBackend; an external manager stays the source of truth for its own
// secrets, which is a type-level guarantee that Miren never writes into one.
package secret

import (
	"context"
	"errors"
	"time"
)

// Errors callers can branch on. Resolution failures deliberately carry the
// reference but never the value, and never the underlying store's error text,
// which can quote surrounding context.
var (
	// ErrUnknownBackend means no backend instance is registered under the name
	// a reference asked for.
	ErrUnknownBackend = errors.New("unknown secret backend")

	// ErrNotFound means the backend has no secret at that path, or no such
	// version of it.
	ErrNotFound = errors.New("secret not found")

	// ErrVersionNotEnabled means the version exists but has been disabled or
	// destroyed. Resolving it fails closed rather than falling back to another
	// version.
	ErrVersionNotEnabled = errors.New("secret version is not enabled")

	// ErrReadOnlyBackend means a write was attempted against a backend that
	// only implements SecretBackend — an external manager Miren references but
	// does not own.
	ErrReadOnlyBackend = errors.New("secret backend is read-only")
)

// VersionState is the lifecycle state of a single secret version. Only
// StateEnabled resolves; the other two fail closed.
type VersionState string

const (
	StateEnabled   VersionState = "enabled"
	StateDisabled  VersionState = "disabled"
	StateDestroyed VersionState = "destroyed"
)

// SecretValue is a resolved secret held in memory.
type SecretValue struct {
	// Ref is the fully-resolved reference, always with a concrete version on
	// the end (e.g. "payments/stripe-key@x1A"), whether or not the input ref
	// carried one. ConfigVersion formulation stores Ref as the variable's
	// value, freezing which version that config resolves — so every sandbox
	// built from it sees the same bytes even after the secret rotates, and the
	// version advances only when a new ConfigVersion is minted.
	Ref string

	// Bytes is the raw secret value for Ref. Callers must not persist it.
	Bytes []byte
}

// A SecretBackend resolves references to secret values. Implementations talk to
// a specific kind of store — the in-cluster store, Vault, a cloud manager.
type SecretBackend interface {
	// Name returns the instance name this backend was registered under (e.g.
	// "cluster", "prod-vault"). It matches a variable's backend field.
	Name() string

	// Resolve returns the value for a backend-relative reference along with the
	// fully-qualified reference it resolved to. The input may float ("path") or
	// already be pinned ("path@version"); either way the returned
	// SecretValue.Ref carries a concrete version. Resolve never writes to disk.
	Resolve(ctx context.Context, ref string) (SecretValue, error)
}

// A WritableBackend additionally supports storing and managing secrets.
// External read-only backends do not implement this: their source of truth is
// managed outside Miren, so Miren has no code path that writes into one.
type WritableBackend interface {
	SecretBackend

	// Put stores a new version of the secret at path and returns its version
	// handle. It is compare-and-set on the secret's current version, so
	// concurrent writers cannot silently lose an update. Storing a value
	// identical to the current version is a no-op that returns the existing
	// handle rather than minting a duplicate.
	Put(ctx context.Context, path string, value []byte) (version string, err error)

	// SetState transitions a specific version between enabled, disabled and
	// destroyed. Anything still pinned to a version that leaves enabled fails
	// closed on its next resolve.
	SetState(ctx context.Context, ref string, state VersionState) error
}

// VersionSummary describes one version of a secret without its payload.
type VersionSummary struct {
	// Version is the handle a reference names this version by, after the "@".
	Version string

	// State is the version's lifecycle state. Only StateEnabled resolves.
	State VersionState

	// CreatedAt is when the version was stored.
	CreatedAt time.Time

	// Current reports whether a floating reference resolves to this version.
	Current bool
}

// Summary describes a secret and its versions, for operators deciding whether a
// rotation or a revocation is safe. It never carries a value.
type Summary struct {
	Path           string
	Backend        string
	CurrentVersion string
	Versions       []VersionSummary
}

// A ListableBackend can enumerate what it holds. It is optional: an external
// manager may not expose enumeration, or may not expose it to the identity
// Miren reads with, in which case Miren can still resolve known references
// without being able to list them.
type ListableBackend interface {
	SecretBackend

	// List returns every secret the backend holds, without their values.
	List(ctx context.Context) ([]Summary, error)

	// ListVersions returns one secret's versions, without their values.
	ListVersions(ctx context.Context, path string) (Summary, error)
}

// A Resolver resolves a fully-qualified reference that names its own backend,
// as carried in a sandbox spec. Consumers that materialize secrets depend on
// this rather than on Registry directly, so a node that holds no key material
// can satisfy it over RPC against the control plane instead.
type Resolver interface {
	ResolveRef(ctx context.Context, backend, ref string) (SecretValue, error)
}
