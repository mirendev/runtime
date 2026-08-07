// Package run defines the Run entity: one execution of an app task.
//
// It is its own domain rather than part of core because the schema generator
// turns enum choices into bare package-level identifiers, and PENDING, RUNNING,
// and FAILED are too generic to add to core_v1alpha permanently. Entity ids are
// unaffected -- they derive from the kind, so a run is still run/{name}.
package run

//go:generate go run ../../pkg/entity/cmd/schemagen -input schema.yml -output run_v1alpha/schema.gen.go -pkg run_v1alpha
