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

	for _, doc := range listDocs(t, m, "sandbox") {
		if image := sandboxSpecImage(doc); strings.HasPrefix(image, "docker.io/library/nginx@sha256:") {
			assert.Contains(t, doc.Id, name)
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
	for _, doc := range listDocs(t, m, "sandbox") {
		if strings.Contains(doc.Id, appName) {
			if image := sandboxSpecImage(doc); image != "" {
				return doc.Id, image
			}
		}
	}
	t.Fatalf("no sandbox found for app %s", appName)
	return "", ""
}

// sandboxSpecImage deliberately reads the raw debug-entity representation.
// Image-source tests need execution detail; the public sandbox inventory does
// not, and must never regain a SandboxSpec just to serve these assertions.
func sandboxSpecImage(doc entityDoc) string {
	for _, facet := range doc.Facets {
		for _, field := range facet.Fields {
			if field.Name == "spec" {
				if image, ok := findNestedString(field.Value, "image"); ok {
					return image
				}
			}
		}
	}
	return ""
}

func findNestedString(value any, name string) (string, bool) {
	switch value := value.(type) {
	case map[string]any:
		for key, nested := range value {
			if key == name {
				if text, ok := nested.(string); ok {
					return text, true
				}
			}
			if text, ok := findNestedString(nested, name); ok {
				return text, true
			}
		}
	case []any:
		for _, nested := range value {
			if text, ok := findNestedString(nested, name); ok {
				return text, true
			}
		}
	}
	return "", false
}

func TestSandboxSpecImageFromEntityDocument(t *testing.T) {
	var doc entityDoc
	require.NoError(t, json.Unmarshal([]byte(`{
		"facets": [{
			"fields": [{
				"name": "spec",
				"value": {"container": [{"name": "app", "image": "registry.test/app@sha256:abc"}]}
			}]
		}]
	}`), &doc))

	assert.Equal(t, "registry.test/app@sha256:abc", sandboxSpecImage(doc))
}
