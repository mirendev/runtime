package e2e

import (
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"miren.dev/runtime/cli"
)

func TestCustomPortEndToEnd(t *testing.T) {
	r := require.New(t)

	// Start dev server
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	t.Log("starting test")

	exitCode := make(chan int, 1)
	go func() {
		t.Log("starting dev")
		exitCode <- cli.Run([]string{"runtime", "dev"})
	}()

	// Give the dev server time to spin up
	time.Sleep(5 * time.Second)

	// Write a combo.yaml
	filePath := writeTempContents(t, "combo.yaml", comboYaml)

	// Spin up combo
	putCode := cli.Run([]string{"runtime", "entity", "put", "-p", filePath})
	r.Equal(0, putCode)

	select {
	case code := <-exitCode:
		t.Logf("got code: %d", code)
	case <-ctx.Done():
		t.Logf("got done")
	}

	// Ensure we can route to nginx container
	resp, err := http.Get("http://localhost:8989")
	r.NoError(err)
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	r.NoError(err)

	r.Equal(200, resp.StatusCode)
	r.Contains(string(body), "Welcome to nginx!")
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
