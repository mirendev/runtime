//go:build e2e

package e2e

import (
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
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

	cfgPath, err := testserver.TestServerConfig(t)
	r.NoError(err)

	t.Log("putting entities")
	// Spin up combo
	putCode := cli.Run([]string{"miren", "entity", "put", "-v", "--config", cfgPath, "-p", filePath})
	r.Equal(0, putCode)

	// Ensure we can route to nginx container
	t.Log("running HTTP GET")
	resp, err := http.Get("http://localhost")
	r.NoError(err)
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	r.NoError(err)

	r.Equal(200, resp.StatusCode)
	r.Contains(string(body), "Welcome to nginx!")

	//time.Sleep( time.Second)

	require.Eventually(t, func() bool {
		// Fetch logs from app
		var logsCode int
		output, err := testutils.CaptureStdout(func() {
			logsCode = cli.Run([]string{"miren", "logs", "--config", cfgPath, "-a", "nginx"})
		})
		if err != nil {
			return false
		}
		if logsCode != 0 {
			return false
		}

		return strings.Contains(output, `"GET / HTTP/1.1"`)
	}, 30*time.Second, time.Second, "waiting for logs to appear")
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
  name: sample
spec:
  owner: mbot@miren.dev
---
kind: dev.miren.core/app
version: v1alpha
metadata:
  name: nginx
spec:
  project: project/sample
---
kind: dev.miren.core/app_version
version: v1alpha
metadata:
  name: abcdef
spec:
  app: app/nginx
  version: abcdef
  image_url: docker.io/library/nginx:latest
  config:
    port: 80
    services:
      - name: web
        service_concurrency:
          mode: auto
          requests_per_instance: 10
          scale_down_delay: "15m"
---
kind: dev.miren.ingress/http_route
version: v1alpha
metadata:
  name: localhost
spec:
  host: localhost
  app: app/nginx
---
kind: dev.miren.core/app
version: v1alpha
metadata:
  name: nginx
spec:
  project: project/sample
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
