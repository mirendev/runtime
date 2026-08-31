//go:build blackbox

package blackbox

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/distribution/reference"
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

	imageDir := m.ContainerPath(filepath.Join(c.TestdataDir, "image-only"))
	analysis := m.MustRun("deploy", "--analyze", "-d", imageDir)
	assert.Contains(t, analysis.Stdout, "Source: image docker.io/library/nginx:alpine")

	name := harness.DeployApp(t, m, harness.AppOptions{Testdata: "image-only"})

	status := m.MustRun("app", "status", "-a", name)
	assert.Contains(t, status.Stdout, "Source: image docker.io/library/nginx:alpine")

	statusJSON := m.MustRun("app", "status", "-a", name, "--format", "json")
	var appStatus struct {
		Source struct {
			Kind  string `json:"kind"`
			Value string `json:"value"`
		} `json:"source"`
	}
	require.NoError(t, json.Unmarshal([]byte(statusJSON.Stdout), &appStatus))
	assert.Equal(t, "image", appStatus.Source.Kind)
	assert.Equal(t, "docker.io/library/nginx:alpine", appStatus.Source.Value)

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
		if len(sb.Spec.Container) > 0 && strings.HasPrefix(sb.Spec.Container[0].Image, "docker.io/library/nginx@sha256:") {
			assert.Contains(t, sb.ID, name)
			return
		}
	}
	t.Fatal("no sandbox is running the digest-pinned nginx image")
}

// TestDeployImageInheritsMetadata builds a fixture into Miren's own registry,
// then deploys that tag as a first-class image. The second app must inherit the
// image config while launching the exact manifest that supplied it.
func TestDeployImageInheritsMetadata(t *testing.T) {
	c := harness.NewCluster(t)
	m := harness.NewMiren(t, c)

	sourceName := harness.DeployApp(t, m, harness.AppOptions{Testdata: "direct-image-metadata"})
	_, sourceImage := sandboxForImageSource(t, m, sourceName)
	require.NotContains(t, sourceImage, "@", "the source fixture should expose the mutable registry tag")

	dir, err := os.MkdirTemp(c.RepoRoot, ".blackbox-direct-image-")
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, os.RemoveAll(dir)) })
	require.NoError(t, os.MkdirAll(filepath.Join(dir, ".miren"), 0o755))
	appConfig := fmt.Sprintf("name = \"direct-runtime\"\n\n[services.web]\nimage = %q\n", sourceImage)
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".miren", "app.toml"), []byte(appConfig), 0o644))

	name := harness.UniqueAppName(t, "direct-runtime")
	t.Cleanup(func() {
		if r := m.Run("app", "delete", name, "-f"); !r.Success() {
			t.Errorf("failed to delete app %s during cleanup: %s", name, strings.TrimSpace(r.Stderr))
		}
	})
	m.MustRun("deploy", "-a", name, "-d", m.ContainerPath(dir), "-f")
	harness.WaitForAppReady(t, m, name, 2*time.Minute)

	sandboxID, runtimeImage := sandboxForImageSource(t, m, name)
	named, err := reference.ParseNormalizedNamed(sourceImage)
	require.NoError(t, err)
	repository := reference.TrimNamed(named).String()
	assert.True(t, strings.HasPrefix(runtimeImage, repository+"@sha256:"),
		"runtime image %q should pin source repository %q", runtimeImage, repository)

	pwd := m.MustRun("sandbox", "exec", sandboxID, "--", "pwd")
	assert.Equal(t, "/srv/direct-image", strings.TrimSpace(pwd.Stdout))
	port := m.MustRun("sandbox", "exec", sandboxID, "--", "printenv", "PORT")
	assert.Equal(t, "4321", strings.TrimSpace(port.Stdout))

	statusJSON := m.MustRun("app", "status", "-a", name, "--format", "json")
	var status struct {
		Source struct {
			Kind  string `json:"kind"`
			Value string `json:"value"`
		} `json:"source"`
	}
	require.NoError(t, json.Unmarshal([]byte(statusJSON.Stdout), &status))
	assert.Equal(t, "image", status.Source.Kind)
	assert.Equal(t, sourceImage, status.Source.Value)
}

func sandboxForImageSource(t *testing.T, m *harness.Miren, appName string) (string, string) {
	t.Helper()
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
		if strings.Contains(sb.ID, appName) && len(sb.Spec.Container) > 0 {
			return sb.ID, sb.Spec.Container[0].Image
		}
	}
	t.Fatalf("no sandbox found for app %s", appName)
	return "", ""
}
