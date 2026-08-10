package cluster

import (
	"context"
	"fmt"

	"miren.dev/runtime/api/core/core_v1alpha"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/secret"
	"miren.dev/runtime/pkg/secret/keyring"
)

// CountOnKey reports how many stored versions are still wrapped by a given key.
//
// This is what decides when a retiring key can be dropped: while it is above
// zero, retiring the key would strand those versions with nothing able to
// unwrap them. kek_id is indexed, so this is a single lookup rather than a
// scan of every secret.
func (b *Backend) CountOnKey(ctx context.Context, kekID string) (int, error) {
	res, err := b.ec.List(ctx, entity.String(core_v1alpha.SecretVersionKekIdId, kekID))
	if err != nil {
		return 0, fmt.Errorf("counting versions on key %s: %w", kekID, err)
	}
	return res.Length(), nil
}

// RewrapBatch moves up to limit versions off kekID and onto the ring's current
// key, returning how many it moved.
//
// The work is defined by a query rather than a cursor — "versions still on the
// old key" — so it needs no progress bookkeeping. An interrupted backfill
// resumes simply by asking again, and a version rewrapped twice is a no-op.
// That matters because the alternative, tracking position, is state that can
// disagree with reality after a crash.
//
// Only wrapped_dek and kek_id change. The ciphertext is never rewritten, so
// the cost per version is a few dozen bytes regardless of how large the secret
// is, and a value cannot be corrupted by a partially applied rewrap.
func (b *Backend) RewrapBatch(ctx context.Context, kekID string, limit int) (int, error) {
	ring := b.ring.Load()
	if ring.CurrentID() == kekID {
		return 0, fmt.Errorf("refusing to rewrap %s onto itself", kekID)
	}

	res, err := b.ec.List(ctx, entity.String(core_v1alpha.SecretVersionKekIdId, kekID))
	if err != nil {
		return 0, fmt.Errorf("listing versions on key %s: %w", kekID, err)
	}

	moved := 0
	for res.Next() {
		if moved >= limit {
			break
		}

		var sv core_v1alpha.SecretVersion
		if err := res.Read(&sv); err != nil {
			return moved, fmt.Errorf("reading version on key %s: %w", kekID, err)
		}
		sv.ID = res.Entity().Id()

		// A destroyed version has no payload left to move. It still carries the
		// old kek_id, so skipping it silently would leave the count above zero
		// forever and block retirement; clearing the id is what lets the key go.
		if len(sv.Ciphertext) == 0 || len(sv.WrappedDek) == 0 {
			if err := b.ec.UpdateAttrs(ctx, sv.ID,
				entity.String(core_v1alpha.SecretVersionKekIdId, ring.CurrentID()),
			); err != nil {
				return moved, fmt.Errorf("clearing key id on empty version %s: %w", sv.ID, err)
			}
			moved++
			continue
		}

		rewrapped, err := ring.Rewrap(keyring.Sealed{
			Ciphertext: sv.Ciphertext,
			WrappedDEK: sv.WrappedDek,
			KEKID:      sv.KekId,
			ValueMAC:   sv.ValueMac,
		})
		if err != nil {
			return moved, fmt.Errorf("rewrapping version %s: %w", sv.ID, err)
		}

		if err := b.ec.UpdateAttrs(ctx, sv.ID,
			entity.Bytes(core_v1alpha.SecretVersionWrappedDekId, rewrapped.WrappedDEK),
			entity.String(core_v1alpha.SecretVersionKekIdId, rewrapped.KEKID),
			entity.String(core_v1alpha.SecretVersionValueMacId, rewrapped.ValueMAC),
		); err != nil {
			return moved, fmt.Errorf("storing rewrapped version %s: %w", sv.ID, err)
		}

		moved++
	}

	return moved, nil
}

var _ secret.KeyringReporter = (*Backend)(nil)

// KeyringReport describes the cluster's keys and any rotation in flight.
//
// The per-key version count is what makes rotation legible: a non-current key
// with versions still on it is a backfill that has not finished, and is exactly
// why that key cannot be dropped yet.
func (b *Backend) KeyringReport(ctx context.Context) (secret.KeyringReport, error) {
	ring := b.ring.Load()
	current := ring.CurrentID()

	report := secret.KeyringReport{}
	for _, k := range ring.Keys() {
		count, err := b.CountOnKey(ctx, k.ID)
		if err != nil {
			return secret.KeyringReport{}, err
		}
		report.Keys = append(report.Keys, secret.KeyState{
			ID:        k.ID,
			Current:   k.ID == current,
			CreatedAt: k.CreatedAt,
			Versions:  count,
		})
	}

	// The rotation record is the authority on whether one is in flight; the key
	// counts alone cannot distinguish "backfill running" from "extra key that a
	// crash left behind before its rotation was recorded".
	for _, status := range []entity.Id{
		core_v1alpha.KeyRotationStatusRewrappingId,
		core_v1alpha.KeyRotationStatusRetiringId,
	} {
		res, err := b.ec.List(ctx, entity.Ref(core_v1alpha.KeyRotationStatusId, status))
		if err != nil {
			return secret.KeyringReport{}, fmt.Errorf("reading rotation state: %w", err)
		}
		for res.Next() {
			var rec core_v1alpha.KeyRotation
			if err := res.Read(&rec); err != nil {
				return secret.KeyringReport{}, err
			}
			report.Rotating = true
			report.RotatingFrom = rec.FromKey
			report.Rewrapped = int(rec.Rewrapped)
			return report, nil
		}
	}

	return report, nil
}
