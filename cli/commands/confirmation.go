package commands

import (
	"fmt"
	"os"

	"miren.dev/runtime/pkg/ui"
)

// confirmDeletion keeps the one-release compatibility exceptions separate from
// safety overrides such as runner delete --force.
func confirmDeletion(message string, yes, deprecatedForce, allowUnattended bool) (bool, error) {
	if deprecatedForce {
		fmt.Fprintln(os.Stderr, "Warning: -f/--force for skipping confirmation is deprecated; use --yes instead. This alias will be removed next release.")
	}
	if yes || deprecatedForce {
		return true, nil
	}
	if !ui.IsInteractive() {
		if allowUnattended {
			fmt.Fprintln(os.Stderr, "Warning: non-interactive deletion without --yes is deprecated; --yes will be required next release.")
			return true, nil
		}
		return false, fmt.Errorf("confirmation requires a terminal; pass --yes to confirm deletion")
	}
	return ui.Confirm(ui.WithMessage(message), ui.WithDefault(false))
}
