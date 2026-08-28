//go:build blackbox

package blackbox

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"miren.dev/runtime/blackbox/harness"
)

// TestDeployImageOnlyNoDockerfile proves a service image can be the app's
// primary source. The fixture has only app.toml: no Dockerfile, Procfile, or
// language stack for the builder to detect.
func TestDeployImageOnlyNoDockerfile(t *testing.T) {
	c := harness.NewCluster(t)
	m := harness.NewMiren(t, c)

	name := harness.DeployApp(t, m, harness.AppOptions{Testdata: "image-only"})

	r := m.MustRun("sandbox", "list", "--format", "json")
	var sandboxes []struct {
		ID   string `json:"id"`
		Spec struct {
			Container []struct {
				Image string `json:"image"`
			} `json:"container"`
		} `json:"spec"`
	}
	require.NoError(t, json.Unmarshal([]byte(r.Stdout), &sandboxes))

	for _, sb := range sandboxes {
		if len(sb.Spec.Container) > 0 && sb.Spec.Container[0].Image == "docker.io/library/nginx:alpine" {
			assert.Contains(t, sb.ID, name)
			return
		}
	}
	t.Fatal("no sandbox is running the direct nginx image")
}
