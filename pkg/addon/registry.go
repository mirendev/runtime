package addon

import (
	"context"
	"fmt"
	"maps"
	"strings"

	"miren.dev/runtime/api/addon/addon_v1alpha"
	"miren.dev/runtime/api/entityserver"
)

// Registry holds registered addon providers and their definitions.
type Registry struct {
	providers map[string]*registeredAddon
}

type registeredAddon struct {
	provider   AddonProvider
	definition AddonDefinition
}

// NewRegistry creates a new addon registry.
func NewRegistry() *Registry {
	return &Registry{
		providers: make(map[string]*registeredAddon),
	}
}

// Register adds an addon provider with its definition to the registry.
func (r *Registry) Register(name string, provider AddonProvider, def AddonDefinition) {
	r.providers[name] = &registeredAddon{
		provider:   provider,
		definition: def,
	}
}

// Get returns the provider and definition for the named addon.
func (r *Registry) Get(name string) (AddonProvider, AddonDefinition, bool) {
	ra, ok := r.providers[name]
	if !ok {
		return nil, AddonDefinition{}, false
	}
	return ra.provider, ra.definition, true
}

// ListAddons returns all registered addon definitions.
func (r *Registry) ListAddons() []AddonDefinition {
	var defs []AddonDefinition
	for _, ra := range r.providers {
		defs = append(defs, ra.definition)
	}
	return defs
}

// EnsureEntities creates or updates Addon entities in the entity store
// for each registered addon, so they can be discovered by the CLI and API.
func (r *Registry) EnsureEntities(ctx context.Context, ec *entityserver.Client) error {
	for name, ra := range r.providers {
		def := ra.definition

		var variants []addon_v1alpha.Variants
		for _, vd := range def.Variants {
			var details []addon_v1alpha.Details
			for k, v := range vd.Details {
				details = append(details, addon_v1alpha.Details{
					Key:   k,
					Value: v,
				})
			}
			variants = append(variants, addon_v1alpha.Variants{
				Name:        vd.Name,
				Description: vd.Description,
				Details:     details,
			})
		}

		addonEntity := &addon_v1alpha.Addon{
			Name:           name,
			DisplayName:    def.DisplayName,
			Description:    def.Description,
			DefaultVariant: def.DefaultVariant,
			DefaultVersion: def.DefaultVersion,
			LocalityMode:   string(ra.provider.LocalityMode()),
			Variants:       variants,
		}

		_, err := ec.CreateOrReplace(ctx, name, addonEntity)
		if err != nil {
			return fmt.Errorf("ensuring addon entity %q: %w", name, err)
		}
	}
	return nil
}

// ResolveAddonAndVariant parses a spec like "miren-postgresql:small" into
// addon name and variant name. If no variant is specified, the default variant is used.
func (r *Registry) ResolveAddonAndVariant(spec string) (addonName, variantName string, err error) {
	parts := strings.SplitN(spec, ":", 2)
	addonName = parts[0]

	_, def, ok := r.Get(addonName)
	if !ok {
		return "", "", fmt.Errorf("unknown addon %q", addonName)
	}

	if len(parts) == 2 {
		variantName = parts[1]
	} else {
		variantName = def.DefaultVariant
	}

	// Validate the variant exists
	for _, v := range def.Variants {
		if v.Name == variantName {
			return addonName, variantName, nil
		}
	}

	return "", "", fmt.Errorf("unknown variant %q for addon %q", variantName, addonName)
}

// GetVariantConfig returns the provider-internal config for a specific variant,
// with the resolved container image injected under the ConfigImage key.
// If version is empty, the addon's default version is used.
func (r *Registry) GetVariantConfig(addonName, variantName, version string) (map[string]string, error) {
	_, def, ok := r.Get(addonName)
	if !ok {
		return nil, fmt.Errorf("unknown addon %q", addonName)
	}

	for _, v := range def.Variants {
		if v.Name == variantName {
			// Clone the config so we don't mutate the definition.
			cfg := make(map[string]string, len(v.Config)+1)
			maps.Copy(cfg, v.Config)
			// An InApp addon has no container of its own, so there is no image
			// to resolve. Leaving the key absent is what tells the callers that
			// validate or pull it that there is nothing to check.
			if def.BaseImage != "" {
				cfg[ConfigImage] = ResolveImage(def.BaseImage, def.DefaultVersion, version)
			}
			return cfg, nil
		}
	}

	return nil, fmt.Errorf("unknown variant %q for addon %q", variantName, addonName)
}
