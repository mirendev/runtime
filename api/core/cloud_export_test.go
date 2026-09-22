package compute

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
	"miren.dev/runtime/api/core/core_v1alpha"
	"miren.dev/runtime/pkg/entity"
)

// Every schema that names this domain as the owner of its cloud export must
// be merged into the contract generated here, via -export-merge in core.go.
// A contributor's encoder stamps the marker on its kinds regardless, so a
// domain that declares the export but is not merged would put entities in the
// marker index that the contract cannot filter. The exporter skips those with
// a warning rather than stalling, but the mismatch is a build mistake and
// should fail here, not in a log line on a cluster.
func TestCloudExportContractMergesEveryContributor(t *testing.T) {
	schemas, err := filepath.Glob("../*/schema.yml")
	require.NoError(t, err)
	require.NotEmpty(t, schemas)

	type schemaFile struct {
		Domain  string `yaml:"domain"`
		Exports map[string]struct {
			Owner string              `yaml:"owner"`
			Kinds map[string]struct{} `yaml:"kinds"`
		} `yaml:"exports"`
	}

	contributors := 0
	for _, path := range schemas {
		raw, err := os.ReadFile(path)
		require.NoError(t, err)
		var sf schemaFile
		require.NoError(t, yaml.Unmarshal(raw, &sf), path)
		cloud, ok := sf.Exports["cloud"]
		if !ok || cloud.Owner != "dev.miren.core" {
			continue
		}
		contributors++
		for kind := range cloud.Kinds {
			id := entity.Id(sf.Domain + "/kind." + kind)
			_, exported := core_v1alpha.CloudExportContract.Policy(id)
			require.True(t, exported, "%s declares %s for the cloud export but core.go does not -export-merge it", path, id)
		}
	}
	require.Positive(t, contributors, "the compute schema contributes the node kind")
}
