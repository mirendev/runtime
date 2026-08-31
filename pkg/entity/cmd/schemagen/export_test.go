package main

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGenerateExportContractAndMarker(t *testing.T) {
	sf := &schemaFile{
		Domain:  "example.dev",
		Version: "v1",
		Kinds: map[string]schemaAttrs{
			"app": {
				"name": {Type: "string"},
				"actor": {Type: "component", Attrs: map[string]*schemaAttr{
					"subject": {Type: "string"},
					"email":   {Type: "string"},
				}},
			},
		},
		Exports: map[string]exportSpec{
			"cloud": {
				Marker: "example.dev/cloud.export",
				Kinds: map[string]exportKind{
					"app": {
						Lifecycle: "mirror",
						Include: []string{
							"example.dev/app.name",
							"example.dev/actor.subject",
						},
					},
				},
			},
		},
	}

	contracts, err := GenerateExportContracts(sf)
	require.NoError(t, err)
	require.JSONEq(t, `{
      "version": 1,
      "target": "cloud",
      "marker": "example.dev/cloud.export",
      "kinds": [{
        "id": "example.dev/kind.app",
        "lifecycle": "mirror",
        "attributes": [
          {"id":"example.dev/actor.subject","type":"string","parent":"example.dev/app.actor"},
          {"id":"example.dev/app.actor","type":"component"},
          {"id":"example.dev/app.name","type":"string"}
        ]
      }]
    }`, string(contracts["cloud"]))

	code, err := GenerateSchema(sf, "example_v1")
	require.NoError(t, err)
	require.Contains(t, code, `Bool(entity.Id("example.dev/cloud.export"), true)`)
	require.Contains(t, code, "CloudExportContract")
	require.NotContains(t, string(contracts["cloud"]), "actor.email")
}

func TestGenerateExportContractRejectsWholeComponent(t *testing.T) {
	sf := &schemaFile{
		Domain:  "example.dev",
		Version: "v1",
		Kinds: map[string]schemaAttrs{
			"app": {
				"actor": {Type: "component", Attrs: map[string]*schemaAttr{
					"email": {Type: "string"},
				}},
			},
		},
		Exports: map[string]exportSpec{
			"cloud": {
				Marker: "example.dev/cloud.export",
				Kinds: map[string]exportKind{
					"app": {Lifecycle: "mirror", Include: []string{"example.dev/app.actor"}},
				},
			},
		},
	}

	_, err := GenerateExportContracts(sf)
	require.ErrorContains(t, err, "must select component fields")
}

func TestGenerateExportContractRejectsNamedEnum(t *testing.T) {
	sf := &schemaFile{
		Domain:  "example.dev",
		Version: "v1",
		Enums: map[string]schemaEnum{
			"port_protocol": {Values: []string{"tcp", "udp"}},
		},
		Kinds: map[string]schemaAttrs{
			"app": {
				"protocol": {Type: "enum", Enum: "port_protocol"},
			},
		},
		Exports: map[string]exportSpec{
			"cloud": {
				Marker: "example.dev/cloud.export",
				Kinds: map[string]exportKind{
					"app": {Lifecycle: "mirror", Include: []string{"example.dev/app.protocol"}},
				},
			},
		},
	}

	_, err := GenerateExportContracts(sf)
	require.ErrorContains(t, err, `uses named enum "port_protocol"`)
}

func exportOwnerSchema() *schemaFile {
	return &schemaFile{
		Domain:  "example.dev",
		Version: "v1",
		Kinds: map[string]schemaAttrs{
			"app": {"name": {Type: "string"}},
		},
		Exports: map[string]exportSpec{
			"cloud": {
				Marker: "example.dev/cloud.export",
				Kinds: map[string]exportKind{
					"app": {Lifecycle: "mirror", Include: []string{"example.dev/app.name"}},
				},
			},
		},
	}
}

func exportContributorSchema(owner, marker string) *schemaFile {
	return &schemaFile{
		Domain:  "example.compute",
		Version: "v1",
		Kinds: map[string]schemaAttrs{
			"node": {
				"name":       {Type: "string"},
				"scheduling": {Type: "enum", Choices: []string{"schedulable", "cordoned"}},
				"secret":     {Type: "string"},
			},
		},
		Exports: map[string]exportSpec{
			"cloud": {
				Marker: marker,
				Owner:  owner,
				Kinds: map[string]exportKind{
					"node": {Lifecycle: "mirror", Include: []string{
						"example.compute/node.name",
						"example.compute/node.scheduling",
					}},
				},
			},
		},
	}
}

// A kind from another domain folds into the owner's contract under its own
// domain-qualified id, and the merged artifact is one digest, not two.
func TestGenerateExportContractMergesContributorDomains(t *testing.T) {
	owner := exportOwnerSchema()
	contributor := exportContributorSchema("example.dev", "example.dev/cloud.export")

	contracts, err := GenerateExportContracts(owner, contributor)
	require.NoError(t, err)
	require.JSONEq(t, `{
      "version": 1,
      "target": "cloud",
      "marker": "example.dev/cloud.export",
      "kinds": [
        {
          "id": "example.compute/kind.node",
          "lifecycle": "mirror",
          "attributes": [
            {"id":"example.compute/node.name","type":"string"},
            {"id":"example.compute/node.scheduling","type":"enum",
             "enum_values":["example.compute/scheduling.cordoned","example.compute/scheduling.schedulable"]}
          ]
        },
        {
          "id": "example.dev/kind.app",
          "lifecycle": "mirror",
          "attributes": [{"id":"example.dev/app.name","type":"string"}]
        }
      ]
    }`, string(contracts["cloud"]))
	require.NotContains(t, string(contracts["cloud"]), "node.secret")

	// Without the contributor the owner's contract is exactly what it was
	// before merging existed, so a domain that contributes nothing changes no
	// deployed digest.
	alone, err := GenerateExportContracts(owner)
	require.NoError(t, err)
	require.NotContains(t, string(alone["cloud"]), "kind.node")
}

// The contributor marks its own kinds for export but never emits a contract:
// the only digest in the codebase must be the owner's.
func TestContributorDomainMarksKindsWithoutEmittingContract(t *testing.T) {
	contributor := exportContributorSchema("example.dev", "example.dev/cloud.export")

	contracts, err := GenerateExportContracts(contributor)
	require.NoError(t, err)
	require.Empty(t, contracts)

	code, err := GenerateSchema(contributor, "compute_v1")
	require.NoError(t, err)
	require.Contains(t, code, `Bool(entity.Id("example.dev/cloud.export"), true)`)
	require.NotContains(t, code, "CloudExportContract")
}

func TestGenerateExportContractRejectsContributorMismatch(t *testing.T) {
	owner := exportOwnerSchema()

	_, err := GenerateExportContracts(owner, exportContributorSchema("someone.else", "example.dev/cloud.export"))
	require.ErrorContains(t, err, `owned by "someone.else"`)

	_, err = GenerateExportContracts(owner, exportContributorSchema("example.dev", "example.compute/cloud.export"))
	require.ErrorContains(t, err, "uses marker example.compute/cloud.export")
}
