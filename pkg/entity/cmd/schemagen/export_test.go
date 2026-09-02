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
