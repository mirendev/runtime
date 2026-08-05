// Package cluster implements the in-cluster secret backend.
//
// It is the counterpart to the external managers RFD-90 anticipates: the same
// reference model, but Miren holds the value. A team gets a secret store
// without standing up Vault first, and because Miren owns retention a pinned
// version is guaranteed to stay resolvable rather than being pruned out from
// under a deploy.
//
// Secrets are modeled the way config already is — a stable identity plus
// immutable versions — so the secret value stops being the one floating input
// in an otherwise fully-pinned deploy record. Each version is independently
// envelope-encrypted, so nothing sits in etcd in plaintext and the value exists
// only transiently, in memory, during a resolve.
package cluster

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"miren.dev/runtime/api/core/core_v1alpha"
	"miren.dev/runtime/api/entityserver"
	"miren.dev/runtime/pkg/cond"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/idgen"
	"miren.dev/runtime/pkg/secret"
	"miren.dev/runtime/pkg/secret/keyring"
)

// maxCASAttempts is a live-lock backstop for the compare-and-set loop that
// swings a secret's current version, not an expected limit. Each conflict just
// means another writer rotated the same secret first, so it is set high enough
// that genuine contention never spuriously fails a write. It matches the
// backstop the env-var mutation path uses for the same reason.
const maxCASAttempts = 100

// Backend stores secret values inside the cluster's entity store.
type Backend struct {
	log  *slog.Logger
	ec   *entityserver.Client
	ring *keyring.Keyring
}

// NewBackend builds the in-cluster backend over an entity store and the
// cluster's keyring.
func NewBackend(log *slog.Logger, ec *entityserver.Client, ring *keyring.Keyring) *Backend {
	return &Backend{
		log:  log.With("module", "secret-cluster"),
		ec:   ec,
		ring: ring,
	}
}

var _ secret.WritableBackend = (*Backend)(nil)

// Name returns the instance name this backend registers under.
func (b *Backend) Name() string { return secret.ClusterBackendName }

// Resolve returns the value for a backend-relative reference along with the
// fully-qualified reference it resolved to.
//
// A version-less reference tracks the secret's current version; a pinned one
// holds exactly what it names. Either way the returned Ref carries a concrete
// version, so a ConfigVersion that records it sees identical bytes on every
// later resolve.
func (b *Backend) Resolve(ctx context.Context, ref string) (secret.SecretValue, error) {
	path, version, err := secret.ParseRef(ref)
	if err != nil {
		return secret.SecretValue{}, err
	}

	sec, _, err := b.loadSecret(ctx, path)
	if err != nil {
		return secret.SecretValue{}, err
	}

	sv, shortID, err := b.loadVersion(ctx, sec, version)
	if err != nil {
		return secret.SecretValue{}, err
	}

	// Fail closed on anything that is no longer enabled rather than falling
	// back to another version — a revoked secret must not silently resolve to
	// a different value than the one that was pinned.
	if sv.State != core_v1alpha.ENABLED {
		return secret.SecretValue{}, fmt.Errorf("%w: %s is %s",
			secret.ErrVersionNotEnabled, secret.FormatRef(path, shortID), stateName(sv.State))
	}

	value, err := b.ring.Open(keyring.Sealed{
		Ciphertext: sv.Ciphertext,
		WrappedDEK: sv.WrappedDek,
		KEKID:      sv.KekId,
	})
	if err != nil {
		return secret.SecretValue{}, fmt.Errorf("decrypting %s: %w", secret.FormatRef(path, shortID), err)
	}

	return secret.SecretValue{
		Ref:   secret.FormatRef(path, shortID),
		Bytes: value,
	}, nil
}

// Put stores a new version of the secret at path and returns its version
// handle.
//
// Writing a value identical to the current version is a no-op that returns the
// existing handle: the indexed keyed hash makes "is this value already stored?"
// a lookup rather than a decrypt-and-compare, so a re-run of the same
// `secret set` does not churn a new version and invalidate every pin.
func (b *Backend) Put(ctx context.Context, path string, value []byte) (string, error) {
	if err := ValidatePath(path); err != nil {
		return "", err
	}

	mac, err := b.ring.MAC(value)
	if err != nil {
		return "", err
	}

	for attempt := 0; attempt < maxCASAttempts; attempt++ {
		sec, rev, err := b.ensureSecret(ctx, path)
		if err != nil {
			return "", err
		}

		if sec.CurrentVersion != "" {
			var current core_v1alpha.SecretVersion
			ent, err := b.ec.GetByIdWithEntity(ctx, sec.CurrentVersion, &current)
			if err == nil && current.State == core_v1alpha.ENABLED && current.ValueMac == mac {
				return shortIdOf(ent), nil
			}
		}

		sealed, err := b.ring.Seal(value)
		if err != nil {
			return "", err
		}

		versionID, shortID, err := b.createVersion(ctx, sec.ID, sealed)
		if err != nil {
			return "", err
		}

		err = b.ec.Patch(ctx, sec.ID, rev, entity.Ref(core_v1alpha.SecretCurrentVersionId, versionID))
		if err == nil {
			return shortID, nil
		}
		if !errors.Is(err, cond.ErrConflict{}) {
			return "", fmt.Errorf("storing secret %s: %w", path, err)
		}

		// Another writer rotated this secret first, so the version just minted
		// was never made current. Drop it rather than leaving an unreferenced
		// version that would show up in listings.
		if delErr := b.ec.Delete(ctx, versionID); delErr != nil {
			b.log.Warn("failed to delete superseded secret version after conflict",
				"secret", sec.ID, "version", versionID, "error", delErr)
		}
	}

	return "", fmt.Errorf("failed to store secret %s after %d attempts due to concurrent writes", path, maxCASAttempts)
}

// SetState transitions a specific version between enabled, disabled and
// destroyed. Destroying additionally drops the payload, so the value is gone
// rather than merely unreachable.
//
// The reference must name a version: a state change is always about one
// version, and letting it float would make "disable the current one" a
// different secret depending on when it ran.
func (b *Backend) SetState(ctx context.Context, ref string, state secret.VersionState) error {
	path, version, err := secret.ParseRef(ref)
	if err != nil {
		return err
	}
	if version == "" {
		return fmt.Errorf("changing state needs a specific version, e.g. %s@<version>", path)
	}

	sec, _, err := b.loadSecret(ctx, path)
	if err != nil {
		return err
	}

	sv, shortID, err := b.loadVersion(ctx, sec, version)
	if err != nil {
		return err
	}

	encoded, err := encodeState(state)
	if err != nil {
		return err
	}

	attrs := []any{entity.Ref(core_v1alpha.SecretVersionStateId, encoded)}
	if state == secret.StateDestroyed {
		// Overwrite the payload rather than dropping the attributes: a write
		// here merges, and replacing the whole entity to remove them would take
		// its short id with it — which is the handle every pin names it by.
		attrs = append(attrs,
			entity.Bytes(core_v1alpha.SecretVersionCiphertextId, []byte{}),
			entity.Bytes(core_v1alpha.SecretVersionWrappedDekId, []byte{}),
		)
	}

	if err := b.ec.UpdateAttrs(ctx, sv.ID, attrs...); err != nil {
		return fmt.Errorf("updating state of %s: %w", secret.FormatRef(path, shortID), err)
	}

	b.log.Info("changed secret version state",
		"path", path, "version", shortID, "state", state)
	return nil
}

// loadSecret finds a secret by its path, returning the entity revision so a
// caller can swing current_version under optimistic concurrency control.
func (b *Backend) loadSecret(ctx context.Context, path string) (*core_v1alpha.Secret, int64, error) {
	if err := ValidatePath(path); err != nil {
		return nil, 0, err
	}

	var sec core_v1alpha.Secret
	ent, err := b.ec.GetWithEntity(ctx, entityName(path), &sec)
	if err != nil {
		if errors.Is(err, cond.ErrNotFound{}) {
			return nil, 0, fmt.Errorf("%w: %s", secret.ErrNotFound, path)
		}
		return nil, 0, fmt.Errorf("loading secret %s: %w", path, err)
	}

	sec.ID = entity.Id(ent.Id())
	return &sec, ent.Revision(), nil
}

// ensureSecret loads a secret's identity row, creating it on first write.
func (b *Backend) ensureSecret(ctx context.Context, path string) (*core_v1alpha.Secret, int64, error) {
	sec, rev, err := b.loadSecret(ctx, path)
	if err == nil {
		return sec, rev, nil
	}
	if !errors.Is(err, secret.ErrNotFound) {
		return nil, 0, err
	}

	// The identity row carries no payload, so creating it races harmlessly:
	// CreateOrUpdate is keyed on the same name both writers derive from the
	// path, and encoding only the path leaves an existing current_version
	// untouched.
	if _, err := b.ec.CreateOrUpdate(ctx, entityName(path), &core_v1alpha.Secret{Path: path}); err != nil {
		return nil, 0, fmt.Errorf("creating secret %s: %w", path, err)
	}

	return b.loadSecret(ctx, path)
}

// loadVersion resolves a version handle against a secret, defaulting to the
// secret's current version when the handle is empty.
func (b *Backend) loadVersion(ctx context.Context, sec *core_v1alpha.Secret, version string) (*core_v1alpha.SecretVersion, string, error) {
	if version == "" {
		if sec.CurrentVersion == "" {
			return nil, "", fmt.Errorf("%w: %s has no versions", secret.ErrNotFound, sec.Path)
		}

		var sv core_v1alpha.SecretVersion
		ent, err := b.ec.GetByIdWithEntity(ctx, sec.CurrentVersion, &sv)
		if err != nil {
			return nil, "", fmt.Errorf("loading current version of %s: %w", sec.Path, err)
		}
		sv.ID = entity.Id(ent.Id())
		return &sv, shortIdOf(ent), nil
	}

	var sv core_v1alpha.SecretVersion
	ent, err := b.ec.GetByIdWithEntity(ctx, entity.Id(version), &sv)
	if err != nil {
		return nil, "", fmt.Errorf("%w: %s", secret.ErrNotFound, secret.FormatRef(sec.Path, version))
	}

	// Version handles are globally unique short ids, so a reference could name
	// a version belonging to some other secret. Refuse rather than hand back
	// bytes the reference did not ask for.
	if sv.Secret != sec.ID {
		return nil, "", fmt.Errorf("%w: %s", secret.ErrNotFound, secret.FormatRef(sec.Path, version))
	}

	sv.ID = entity.Id(ent.Id())
	return &sv, shortIdOf(ent), nil
}

// createVersion mints an immutable version row and returns its id along with
// the short id that serves as its version handle.
func (b *Backend) createVersion(ctx context.Context, secretID entity.Id, sealed keyring.Sealed) (entity.Id, string, error) {
	sv := &core_v1alpha.SecretVersion{
		Secret:     secretID,
		State:      core_v1alpha.ENABLED,
		Ciphertext: sealed.Ciphertext,
		WrappedDek: sealed.WrappedDEK,
		KekId:      sealed.KEKID,
		ValueMac:   sealed.ValueMAC,
	}

	id, err := b.ec.Create(ctx, idgen.GenNS("sv"), sv)
	if err != nil {
		return "", "", fmt.Errorf("creating secret version: %w", err)
	}

	var stored core_v1alpha.SecretVersion
	ent, err := b.ec.GetByIdWithEntity(ctx, id, &stored)
	if err != nil {
		return "", "", fmt.Errorf("reading back secret version: %w", err)
	}

	shortID := shortIdOf(ent)
	if shortID == "" {
		return "", "", fmt.Errorf("secret version %s has no short id to reference it by", id)
	}

	return id, shortID, nil
}

// shortIdOf pulls an entity's short id, which is the handle a reference names a
// version by. There is no explicit version field: the short id is the version.
func shortIdOf(ent interface{ Attrs() []entity.Attr }) string {
	for _, attr := range ent.Attrs() {
		if attr.ID == entity.DBShortId {
			return attr.Value.String()
		}
	}
	return ""
}

func encodeState(state secret.VersionState) (entity.Id, error) {
	switch state {
	case secret.StateEnabled:
		return core_v1alpha.SecretVersionStateEnabledId, nil
	case secret.StateDisabled:
		return core_v1alpha.SecretVersionStateDisabledId, nil
	case secret.StateDestroyed:
		return core_v1alpha.SecretVersionStateDestroyedId, nil
	}
	return "", fmt.Errorf("unknown secret version state %q", state)
}

// stateName renders a stored state for an error message.
func stateName(state core_v1alpha.SecretVersionState) secret.VersionState {
	switch state {
	case core_v1alpha.ENABLED:
		return secret.StateEnabled
	case core_v1alpha.DISABLED:
		return secret.StateDisabled
	case core_v1alpha.DESTROYED:
		return secret.StateDestroyed
	}
	return secret.VersionState(state)
}
