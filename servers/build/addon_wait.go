package build

import (
	"context"
	"sort"
	"strings"

	"miren.dev/runtime/appconfig"
	"miren.dev/runtime/pkg/addon"
	"miren.dev/runtime/pkg/entity"
)

var addonWaitCeiling = addon.WaitCeiling
var cloneWaitCeiling = addon.CloneWaitCeiling

type expectedAddon = addon.ExpectedAddon

// expectedAddons lists the addons app.toml declares, sorted by name so the
// deploy's messages come out in a stable order. The key in app.toml is the
// addon name, optionally with a ":variant" suffix that CreateInstance
// splits the same way.
func expectedAddons(ac *appconfig.AppConfig) []expectedAddon {
	if ac == nil {
		return nil
	}
	out := make([]expectedAddon, 0, len(ac.Addons))
	for key, cfg := range ac.Addons {
		name, suffix, _ := strings.Cut(key, ":")
		variant := suffix
		if cfg != nil && cfg.Variant != "" {
			variant = cfg.Variant
		}
		out = append(out, expectedAddon{Name: name, Variant: variant})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func (b *Builder) awaitAddons(ctx context.Context, appName string, appID, versionID entity.Id, expected []expectedAddon, status StatusSender) error {
	ceiling := addonWaitCeiling
	if versionID != "" {
		ceiling = cloneWaitCeiling
	}
	return addon.WaitForAssociations(ctx, b.EAS, b.Log, appName, appID, versionID, expected, ceiling, status)
}
