// Package sqlite provides an embedded SQLite database as an addon.
//
// Unlike every other addon, this one runs no server. SQLite is an in-process
// library, so there is nothing to connect to: the addon arranges for a database
// to be mounted into the app's own container and hands back its path. That is
// what addon.InApp means, and why this package has no sandbox pool, no service,
// and no provisioning saga.
//
// The database itself is a sqlite-provider disk (see
// controllers/sandbox/volume.go), created in WAL mode and replicated to the
// coordinator by components/sqlitedisk. This package only decides that the app
// should have one and where it goes.
package sqlite

import "miren.dev/runtime/pkg/addon"

const (
	// AddonName is how the addon is declared in app.toml.
	AddonName = "miren-sqlite"

	// VariantStandard is the only variant. SQLite has no sizing to choose:
	// the database grows into the node's free space.
	VariantStandard = "standard"

	// DiskName names the disk contributed to each service. It identifies the
	// attachment, not the database: backup identity comes from the volume's
	// sqlite id, which this addon leaves unset so it takes the default.
	DiskName = "sqlite"

	// MountPath is where the database directory appears in the container.
	MountPath = "/data"

	// DbFile is the database's filename inside MountPath.
	DbFile = "data.db"
)

// DatabasePath is the full path the app opens.
const DatabasePath = MountPath + "/" + DbFile

// Definition describes the addon for the catalog and the CLI.
//
// BaseImage and DefaultVersion are deliberately empty: an InApp addon has no
// container, and the registry skips image resolution and validation when there
// is no base image to resolve.
func Definition() addon.AddonDefinition {
	return addon.AddonDefinition{
		Name:           AddonName,
		DisplayName:    "Miren SQLite",
		Description:    "Embedded SQLite database, continuously backed up to the coordinator",
		DefaultVariant: VariantStandard,
		Variants: []addon.VariantDefinition{
			{
				Name:        VariantStandard,
				Description: "Embedded database mounted into your app, replicated as you write",
				Details: map[string]string{
					"Path":    DatabasePath,
					"Storage": "Shares the node's disk",
					"Writers": "One",
				},
			},
		},
	}
}
