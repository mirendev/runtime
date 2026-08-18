package compute

// Variable sources: who owns a variable. The schema documents this field as
// "config or manual", but the addon controller writes a third value and the
// deprovision path matches on it exactly, so name them here.
//
// This is the ownership axis, not RFD-55's storage-backend axis. Storage lives
// on the `backend` field. See RFD-90, which settled that split.
const (
	// SourceConfig marks a variable declared in .miren/app.toml. It belongs to
	// the version built from that app.toml.
	SourceConfig = "config"

	// SourceManual marks a variable an operator set.
	SourceManual = "manual"

	// SourceAddon marks a variable an addon contributed. Deprovision strips only
	// keys whose source is exactly this, so the value must never be rewritten to
	// anything else.
	SourceAddon = "addon"
)
