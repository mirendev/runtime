package compute

import (
	"context"
	"fmt"
	"sort"

	"miren.dev/runtime/api/addon/addon_v1alpha"
	"miren.dev/runtime/api/core/core_v1alpha"
	"miren.dev/runtime/api/entityserver/entityserver_v1alpha"
	"miren.dev/runtime/pkg/entity"
)

// ResolveRuntimeConfig returns the config an AppVersion runs with: the version's
// stored config, plus the addon bindings the app's associations currently supply.
//
// This reads the app's addon associations, so it costs one indexed List per call
// and fails if that read fails. Callers that write a ConfigVersion must use
// ResolveConfig instead. Storing the result of this function would put addon
// bindings back into a version, which is what this removes.
//
// Addon variables stored on the version are dropped, and the live bindings are
// used instead. This keeps the invariant in one place: an AppVersion records
// user config, and addon state resolves at runtime. A version that stores a copy
// never serves it, whether the copy predates this design or a later change
// writes one.
//
// Precedence:
//
//   - A binding replaces an app.toml declaration of the same key. Declaring
//     DATABASE_URL in app.toml and then attaching a database addon means the
//     addon supplies the value. Required and Description carry from the
//     declaration onto the binding, because `miren env list` shows them and the
//     next build validates against them.
//   - A binding never replaces a variable an operator set. See RFD-59.
//
// Before this, an addon contributed its variables by minting a successor
// version. A version built before the addon existed therefore never contained
// DATABASE_URL, and activating one left the app without its credentials
// permanently (MIR-1579).
func ResolveRuntimeConfig(
	ctx context.Context,
	eac *entityserver_v1alpha.EntityAccessClient,
	ver *core_v1alpha.AppVersion,
) (*core_v1alpha.ConfigSpec, error) {
	spec, err := ResolveConfig(ctx, eac, ver)
	if err != nil {
		return nil, err
	}

	if ver.App == "" {
		return spec, nil
	}

	bindings, err := addonBindings(ctx, eac, ver.App)
	if err != nil {
		return nil, err
	}

	// The common case: no addons, no stored copies. Return the spec untouched
	// rather than rebuilding an identical slice.
	if len(bindings) == 0 && !hasAddonVars(spec.Variables) {
		return spec, nil
	}

	// Only operator-set keys block a binding. An app.toml declaration does not,
	// because blocking it would hand the app a placeholder instead of a credential.
	kept := make([]core_v1alpha.ConfigSpecVariables, 0, len(spec.Variables)+len(bindings))
	blocked := make(map[string]struct{}, len(spec.Variables))
	for _, v := range spec.Variables {
		if v.Source == SourceAddon {
			continue
		}
		kept = append(kept, v)
		if operatorOwned(v.Source) {
			blocked[v.Key] = struct{}{}
		}
	}

	for _, b := range bindings {
		if _, ok := blocked[b.Key]; ok {
			continue
		}
		blocked[b.Key] = struct{}{}

		bound := core_v1alpha.ConfigSpecVariables{
			Key:       b.Key,
			Value:     b.Value,
			Sensitive: b.Sensitive,
			Source:    SourceAddon,
		}

		// Replace a shadowed declaration in place. This keeps one entry per key and
		// preserves its position.
		if i := indexOfKey(kept, b.Key); i >= 0 {
			bound.Required = kept[i].Required
			bound.Description = kept[i].Description
			kept[i] = bound
			continue
		}
		kept = append(kept, bound)
	}

	spec.Variables = kept
	return spec, nil
}

// operatorOwned reports whether a person set this value, in which case an addon
// must not replace it. An empty source predates the field and counts as
// operator-set, matching mergeVariablesFromAppConfig.
func operatorOwned(source string) bool {
	return source == SourceManual || source == ""
}

func indexOfKey(vars []core_v1alpha.ConfigSpecVariables, key string) int {
	for i := range vars {
		if vars[i].Key == key {
			return i
		}
	}
	return -1
}

func hasAddonVars(vars []core_v1alpha.ConfigSpecVariables) bool {
	for _, v := range vars {
		if v.Source == SourceAddon {
			return true
		}
	}
	return false
}

// addonBindings returns the variables every active addon association contributes
// to an app.
//
// Only "active" associations count. A pending or provisioning one has no final
// values yet, and the launcher defers pool creation until they settle (see
// addonsReady). A deprovisioning one is being torn down, so serving its
// credentials would point the app at a database that is about to disappear. An
// "error" one never finished provisioning, so its values were never known good.
func addonBindings(
	ctx context.Context,
	eac *entityserver_v1alpha.EntityAccessClient,
	appID entity.Id,
) ([]addon_v1alpha.Variables, error) {
	results, err := eac.List(ctx, entity.Ref(addon_v1alpha.AddonAssociationAppId, appID))
	if err != nil {
		return nil, fmt.Errorf("listing addon associations for %s: %w", appID, err)
	}

	type contribution struct {
		assoc entity.Id
		vars  []addon_v1alpha.Variables
	}

	var found []contribution
	for _, ent := range results.Values() {
		var assoc addon_v1alpha.AddonAssociation
		assoc.Decode(ent.Entity())

		if assoc.Status != "active" {
			continue
		}
		found = append(found, contribution{assoc: assoc.ID, vars: assoc.Variables})
	}

	// The first contributor to supply a key wins, so sort by association id to make
	// that a function of stored state rather than of List order. Two addons
	// supplying the same key is rare, because AdjustEnvVars asks the second to
	// rename at provision time, but it is reachable.
	sort.Slice(found, func(i, j int) bool { return found[i].assoc < found[j].assoc })

	var out []addon_v1alpha.Variables
	for _, f := range found {
		out = append(out, f.vars...)
	}
	return out, nil
}
