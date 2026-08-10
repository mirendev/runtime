package cluster

import (
	"context"
	"fmt"
	"sort"

	"miren.dev/runtime/api/core/core_v1alpha"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/secret"
)

var _ secret.ListableBackend = (*Backend)(nil)

// List returns every secret the cluster holds, with its versions, and never a
// value. An operator uses it to see the blast radius of a rotation or a
// revocation before acting.
func (b *Backend) List(ctx context.Context) ([]secret.Summary, error) {
	res, err := b.ec.List(ctx, entity.Ref(entity.EntityKind, core_v1alpha.KindSecret))
	if err != nil {
		return nil, fmt.Errorf("listing secrets: %w", err)
	}

	var summaries []secret.Summary
	for res.Next() {
		var sec core_v1alpha.Secret
		if err := res.Read(&sec); err != nil {
			return nil, fmt.Errorf("reading secret: %w", err)
		}
		sec.ID = res.Entity().Id()

		summary, err := b.summarize(ctx, &sec)
		if err != nil {
			return nil, err
		}
		summaries = append(summaries, summary)
	}

	sort.Slice(summaries, func(i, j int) bool { return summaries[i].Path < summaries[j].Path })
	return summaries, nil
}

// ListVersions returns one secret's versions, without their values.
func (b *Backend) ListVersions(ctx context.Context, path string) (secret.Summary, error) {
	sec, _, err := b.loadSecret(ctx, path)
	if err != nil {
		return secret.Summary{}, err
	}
	return b.summarize(ctx, sec)
}

// summarize collects a secret's versions, newest first, marking which one a
// floating reference resolves to.
func (b *Backend) summarize(ctx context.Context, sec *core_v1alpha.Secret) (secret.Summary, error) {
	summary := secret.Summary{
		Path:    sec.Path,
		Backend: b.Name(),
	}

	res, err := b.ec.List(ctx, entity.Ref(core_v1alpha.SecretVersionSecretId, sec.ID))
	if err != nil {
		return secret.Summary{}, fmt.Errorf("listing versions of %s: %w", sec.Path, err)
	}

	for res.Next() {
		var sv core_v1alpha.SecretVersion
		if err := res.Read(&sv); err != nil {
			return secret.Summary{}, fmt.Errorf("reading version of %s: %w", sec.Path, err)
		}

		ent := res.Entity()
		current := ent.Id() == sec.CurrentVersion
		if current {
			summary.CurrentVersion = ent.ShortId()
		}

		summary.Versions = append(summary.Versions, secret.VersionSummary{
			Version:   ent.ShortId(),
			State:     stateName(sv.State),
			CreatedAt: ent.GetCreatedAt(),
			Current:   current,
		})
	}

	// Newest first, so the version an operator most likely cares about leads.
	// Short ids are opaque rather than ordered, so creation time is what orders
	// them; ties fall back to the handle to keep output stable.
	sort.Slice(summary.Versions, func(i, j int) bool {
		a, z := summary.Versions[i], summary.Versions[j]
		if a.CreatedAt.Equal(z.CreatedAt) {
			return a.Version < z.Version
		}
		return a.CreatedAt.After(z.CreatedAt)
	})

	return summary, nil
}
