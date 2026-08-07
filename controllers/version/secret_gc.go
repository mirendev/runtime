package version

import (
	"context"
	"fmt"
	"sort"
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

	// Candidates cleared every retention rule on the snapshot. They are
	// collected rather than deleted inline so the pre-delete re-check below can
	// be one fresh read for the whole sweep instead of one per candidate.
	type candidate struct {
		id      entity.Id
		path    string
		version string
		ref     string
	}
	var candidates []candidate

	for _, e := range secretsResp.Values() {
		var sec core_v1alpha.Secret
		sec.Decode(e.Entity())
		sec.ID = e.Entity().Id()

		versionsResp, err := c.EAC.List(gcCtx, entity.Ref(core_v1alpha.SecretVersionSecretId, sec.ID))
		if err != nil {
			return result, fmt.Errorf("failed to list versions of secret %s: %w", sec.ID, err)
		}

		versions := make([]*entity.Entity, 0, len(versionsResp.Values()))
		for _, ve := range versionsResp.Values() {
			versions = append(versions, ve.Entity())
		}
		result.TotalScanned += len(versions)

		// Newest first, so each version's successor is the one before it. A
		// version stopped being usable when its successor was created, which is
		// what the retention window is actually measured from.
		sort.Slice(versions, func(i, j int) bool {
			return versions[i].GetCreatedAt().After(versions[j].GetCreatedAt())
		})

		for i, ve := range versions {
			versionID := ve.Id()

			// The version a floating reference resolves to is never reapable,
			// whatever else is true of it.
			if versionID == sec.CurrentVersion {
				result.RetainedCurrent++
				continue
			}

			ref := secret.FormatRef(sec.Path, ve.ShortId())
			if pinned[ref] {
				result.RetainedPinned++
				continue
			}

			// Measuring from creation would give the case this window exists
			// for no window at all: a credential that sat current for a year
			// and rotated this morning is already long past the cutoff on the
			// day it is superseded. What matters is how long ago it stopped
			// being current, which is when the next version appeared.
			supersededAt := ve.GetCreatedAt()
			if i > 0 {
				supersededAt = versions[i-1].GetCreatedAt()
			}
			if supersededAt.After(cutoff) {
				result.RetainedRecent++
				continue
			}

			candidates = append(candidates, candidate{
				id:      versionID,
				path:    sec.Path,
				version: ve.ShortId(),
				ref:     ref,
			})
		}
	}

	if len(candidates) == 0 {
		return result, nil
	}

	// The snapshot above can be a full pass stale by now, and a ConfigVersion
	// minted in that window pins a version this sweep already judged
	// unreferenced. Unlike an app version there is no rebuilding a secret that
	// gets deleted underneath it, so re-read the pins once here and drop
	// anything that has since been claimed.
	//
	// Once for the sweep, not once per candidate: this walks every
	// ConfigVersion in the cluster, so per-candidate it would be K x N and a
	// first sweep on a cluster with history could exhaust the time budget and
	// abort partway.
	fresh, err := c.pinnedSecretVersions(gcCtx)
	if err != nil {
		c.Log.Warn("failed to re-check secret pins before deleting; retaining this sweep",
			"candidates", len(candidates), "error", err)
		result.FailedVersions += len(candidates)
		return result, nil
	}

	for _, cand := range candidates {
		if fresh[cand.ref] {
			c.Log.Debug("retaining secret version pinned during sweep",
				"secret", cand.path, "version", cand.version)
			result.RetainedPinned++
			continue
		}

		if _, err := c.EAC.Delete(gcCtx, cand.id.String()); err != nil {
			c.Log.Warn("failed to delete secret version",
				"secret", cand.path, "version", cand.version, "error", err)
			result.FailedVersions++
			continue
		}

		c.Log.Info("reaped unreferenced secret version",
			"secret", cand.path, "version", cand.version)
		result.DeletedVersions++
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
