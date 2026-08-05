package version

import (
	"context"
	"fmt"
	"time"

	coreutil "miren.dev/runtime/api/core"
	"miren.dev/runtime/api/core/core_v1alpha"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/secret"
)

// SecretGCResult reports what a secret-version sweep did.
type SecretGCResult struct {
	// DeletedVersions is the number of secret versions hard-deleted.
	DeletedVersions int
	// FailedVersions is the number that failed to delete.
	FailedVersions int
	// RetainedCurrent is the number kept because they are their secret's
	// current version.
	RetainedCurrent int
	// RetainedPinned is the number kept because a live ConfigVersion still
	// pins them.
	RetainedPinned int
	// RetainedRecent is the number kept because they are younger than the
	// retention window.
	RetainedRecent int
	// TotalScanned is the number of secret versions evaluated.
	TotalScanned int
}

// secretVersionRetention is how long a superseded secret version is kept even
// when nothing references it.
//
// A rotation is exactly when someone might need the old value back — a service
// that has not finished picking up the new one, a deploy that has to be rolled
// back by hand. Reaping the instant the last reference drops would make that
// window zero. This is deliberately generous: a superseded version is a few
// hundred bytes of ciphertext, and the cost of keeping one too long is far
// below the cost of destroying one still needed.
const secretVersionRetention = 30 * 24 * time.Hour

// RunSecretGC reaps secret versions that nothing can reach any more.
//
// A version is reapable only when it is neither its secret's current version
// nor pinned by any live ConfigVersion. That second condition is what makes the
// cluster backend's retention promise real: a reference pinned in a
// ConfigVersion stays resolvable, so a rollback comes up on the bytes that
// version originally shipped with rather than failing or, worse, silently
// resolving something else.
//
// Unlike an app version, a secret version cannot be rebuilt. So this is
// conservative in every direction: any error listing the pins abandons the
// sweep rather than proceeding on a partial picture, which would look exactly
// like "nothing references this."
func (c *GCController) RunSecretGC(ctx context.Context) (*SecretGCResult, error) {
	result := &SecretGCResult{}

	gcCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()

	// Collect every pin first. A partial read here is indistinguishable from
	// "unreferenced", so a failure has to abandon the sweep rather than delete
	// on incomplete information.
	pinned, err := c.pinnedSecretVersions(gcCtx)
	if err != nil {
		return result, err
	}

	secretsResp, err := c.EAC.List(gcCtx, entity.Ref(entity.EntityKind, core_v1alpha.KindSecret))
	if err != nil {
		return result, fmt.Errorf("failed to list secrets: %w", err)
	}

	cutoff := time.Now().Add(-secretVersionRetention)

	for _, e := range secretsResp.Values() {
		var sec core_v1alpha.Secret
		sec.Decode(e.Entity())
		sec.ID = e.Entity().Id()

		versionsResp, err := c.EAC.List(gcCtx, entity.Ref(core_v1alpha.SecretVersionSecretId, sec.ID))
		if err != nil {
			return result, fmt.Errorf("failed to list versions of secret %s: %w", sec.ID, err)
		}

		for _, ve := range versionsResp.Values() {
			result.TotalScanned++

			ent := ve.Entity()
			versionID := ent.Id()

			// The version a floating reference resolves to is never reapable,
			// whatever else is true of it.
			if versionID == sec.CurrentVersion {
				result.RetainedCurrent++
				continue
			}

			if pinned[secret.FormatRef(sec.Path, ent.ShortId())] {
				result.RetainedPinned++
				continue
			}

			if ent.GetCreatedAt().After(cutoff) {
				result.RetainedRecent++
				continue
			}

			if _, err := c.EAC.Delete(gcCtx, versionID.String()); err != nil {
				c.Log.Warn("failed to delete secret version",
					"secret", sec.Path, "version", ent.ShortId(), "error", err)
				result.FailedVersions++
				continue
			}

			c.Log.Info("reaped unreferenced secret version",
				"secret", sec.Path, "version", ent.ShortId())
			result.DeletedVersions++
		}
	}

	return result, nil
}

// pinnedSecretVersions collects every secret reference pinned by a live
// ConfigVersion, as a set of fully-qualified references.
//
// Every ConfigVersion counts, not only those belonging to active app versions:
// a rollback target is by definition not active, and it is precisely the case
// this protection exists for.
func (c *GCController) pinnedSecretVersions(ctx context.Context) (map[string]bool, error) {
	resp, err := c.EAC.List(ctx, entity.Ref(entity.EntityKind, core_v1alpha.KindConfigVersion))
	if err != nil {
		return nil, fmt.Errorf("failed to list config versions: %w", err)
	}

	pinned := make(map[string]bool)
	for _, e := range resp.Values() {
		var cv core_v1alpha.ConfigVersion
		cv.Decode(e.Entity())

		for _, ref := range coreutil.SecretReferences(&cv.Spec) {
			pinned[ref.Ref] = true
		}
	}

	return pinned, nil
}
