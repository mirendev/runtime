//go:build e2e

package e2e

import (
	"io"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"miren.dev/runtime/cli"
	"miren.dev/runtime/pkg/testserver"
	"miren.dev/runtime/pkg/testutils"
)

func TestSandboxLifecycleEndToEnd(t *testing.T) {
	r := require.New(t)

	// Start a test server that we expect to be shut down properly by t.Cleanup functions at the end of the test
	t.Log("starting test")
	err := testserver.TestServer(t)
	r.NoError(err)

	// Give the dev server time to spin up
	time.Sleep(5 * time.Second)

	// Write a combo.yaml
	filePath := writeTempContents(t, "combo.yaml", comboYaml)

	t.Log("putting entities")
	// Spin up combo
	putCode := cli.Run([]string{"runtime", "entity", "put", "-p", filePath})
	r.Equal(0, putCode)

	// Ensure we can route to nginx container, via the default route
	t.Log("running HTTP GET for nginx")
	resp, err := http.Get("http://localhost:8989")
	r.NoError(err)
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	r.NoError(err)

	r.Equal(200, resp.StatusCode)
	r.Contains(string(body), "Welcome to nginx!")

	// Fetch logs from app
	var logsCode int
	logsOut, err := testutils.CaptureStdout(func() {
		logsCode = cli.Run([]string{"runtime", "logs", "-a", "nginx"})
	})
	r.NoError(err)
	r.Equal(0, logsCode)
	r.Contains(logsOut, `"GET / HTTP/1.1"`)

	// Deploy a second app by building it
	bunDir := testutils.GetTestFilePath("..", "testdata", "bun")

	t.Logf("deploying bun from %s", bunDir)
	var deployCode int
	deployOut, err := testutils.CaptureStdout(func() {
		deployCode = cli.Run([]string{"runtime", "deploy", "-d", bunDir, "--explain", "--explain-format", "plain"})
	})
	r.NoError(err)
	r.Equal(0, deployCode)
	r.Contains(deployOut, "All traffic moved to new version.")

	// Set its hostname
	setHostCode := cli.Run([]string{"runtime", "set", "host", "-a", "hw-bun", "--host", "bunbunbun"})
	r.Equal(0, setHostCode)

	t.Log("running HTTP GET for bun")
	client := &http.Client{}
	req, err := http.NewRequest("GET", "http://localhost:8989", nil)
	r.NoError(err)

	req.Host = "bunbunbun" // This sets the Host header

	resp, err = client.Do(req)
	r.NoError(err)
	defer resp.Body.Close()

	body, err = io.ReadAll(resp.Body)
	r.NoError(err)

	r.Equal(200, resp.StatusCode)
	r.Contains(string(body), "Welcome to Bun!")
}

func writeTempContents(t *testing.T, filename, contents string) string {
	r := require.New(t)
	tempDir := t.TempDir()
	filePath := filepath.Join(tempDir, filename)
	file, err := os.Create(filePath)
	r.NoError(err)
	_, err = file.Write([]byte(contents))
	r.NoError(err)
	defer file.Close()
	return filePath
}

var comboYaml = `kind: dev.miren.core/project
version: v1alpha
metadata:
  name: default
spec:
  owner: mbot@miren.dev
---
kind: dev.miren.core/app
version: v1alpha
metadata:
  name: nginx
spec:
  project: project/default
---
kind: dev.miren.core/app_version
version: v1alpha
metadata:
  name: abcdef
spec:
  app: app/nginx
  version: abcdef
  image_url: docker.io/library/nginx:latest
  concurrency: 10
  config:
    port: 80
---
kind: dev.miren.core/app
version: v1alpha
metadata:
  name: nginx
spec:
  project: project/default
  active_version: app_version/abcdef
---
kind: dev.miren.network/service
version: v1alpha
metadata:
  name: nginx
spec:
  match:
    - app=nginx
  port:
    - port: 80
      target_port: 80
      name: http
      node_port: 8080
`
