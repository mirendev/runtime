package addon

import (
	"context"
	"errors"
	"fmt"

	"miren.dev/runtime/api/addon/addon_v1alpha"
	"miren.dev/runtime/api/entityserver/entityserver_v1alpha"
	"miren.dev/runtime/pkg/addon"
	"miren.dev/runtime/pkg/cond"
	"miren.dev/runtime/pkg/entity"
)

// setAssociationVars replaces the association's record of what the addon
// supplies. Every other attribute on the entity is left as it was.
//
// This rewrites the whole entity instead of patching it. Patch and Update both
// merge multi-valued attributes, so patching `variables` would append the new
// credential next to the superseded one. Replace is the only write that can
// overwrite a many-valued attribute (see CreateOrReplace in
// api/entityserver/client.go). Replace does not retry, so the CAS loop is here.
func setAssociationVars(
	ctx context.Context,
	eac *entityserver_v1alpha.EntityAccessClient,
	assocID entity.Id,
	envVars []addon.Variable,
) error {
	// A live-lock backstop, not an expected limit. Only one rotation runs per
	// addon at a time, so contention on a single association is rare.
	const maxAttempts = 100

	// Collapse repeated keys, last one wins. A container environment is a map, so
	// a repeated key has no meaning to store. It also has to go before the
	// comparison below, which assumes one entry per key: with duplicates, two
	// different sets can have the same length and the same key-to-value map, and
	// the write would be skipped as a no-op.
	desired := make([]addon_v1alpha.Variables, 0, len(envVars))
	at := make(map[string]int, len(envVars))
	for _, v := range envVars {
		next := addon_v1alpha.Variables{Key: v.Key, Value: v.Value, Sensitive: v.Sensitive}
		if i, ok := at[v.Key]; ok {
			desired[i] = next
			continue
		}
		at[v.Key] = len(desired)
		desired = append(desired, next)
	}

	for attempt := 1; ; attempt++ {
		resp, err := eac.Get(ctx, assocID.String())
		if err != nil {
			return fmt.Errorf("reading association %s: %w", assocID, err)
		}
		ent := resp.Entity().Entity()
		rev := resp.Entity().Revision()

		var current addon_v1alpha.AddonAssociation
		current.Decode(ent)
		if assocVarsEqual(current.Variables, desired) {
			return nil
		}

		// Remove drops every value of the attribute, which is what makes this a
		// replacement instead of an append.
		ent.Remove(addon_v1alpha.AddonAssociationVariablesId)
		attrs := ent.Attrs()
		for i := range desired {
			attrs = append(attrs, entity.Component(
				addon_v1alpha.AddonAssociationVariablesId, desired[i].Encode()))
		}

		if _, err := eac.Replace(ctx, attrs, rev); err == nil {
			return nil
		} else if !errors.Is(err, cond.ErrConflict{}) {
			return fmt.Errorf("recording association variables: %w", err)
		}

		if attempt >= maxAttempts {
			return fmt.Errorf("recording association variables on %s: too many conflicts after %d attempts",
				assocID, maxAttempts)
		}
	}
}

// assocVarsEqual reports whether two variable sets match, ignoring order.
// Providers build their result sets independently per call, so order carries no
// meaning. A positional comparison would rewrite the entity on every retry.
//
// b must have no repeated keys; setAssociationVars collapses them first. Given
// that, a stored set with a repeat can never compare equal, because it holds
// fewer distinct keys than b in the same length, so it is rewritten and cleaned
// up rather than mistaken for a match.
func assocVarsEqual(a, b []addon_v1alpha.Variables) bool {
	if len(a) != len(b) {
		return false
	}

	byKey := make(map[string]addon_v1alpha.Variables, len(a))
	for _, v := range a {
		byKey[v.Key] = v
	}
	for _, v := range b {
		prev, ok := byKey[v.Key]
		if !ok || prev != v {
			return false
		}
	}
	return true
}
