package commands

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"miren.dev/runtime/clientconfig"
)

type workbenchTestCall struct {
	Name      string
	Arguments string
	Output    string
	Error     bool
}

func workbenchTestContext(t *testing.T, calls []workbenchTestCall) (*Context, *bytes.Buffer, *bytes.Buffer) {
	t.Helper()
	token := freshLoginJWT(t)
	ack := make(chan struct{}, 1)
	ready := make(chan struct{})
	done := make(chan struct{})
	var sessionID string
	callIndex := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "Bearer "+token, r.Header.Get("Authorization"))
		switch {
		case strings.HasSuffix(r.URL.Path, "/ensure-context"):
			assert.Equal(t, "/workbench/api/clusters/cloud-cluster/ensure-context", r.URL.Path)
			fmt.Fprint(w, `{"contextId":"scope"}`)
		case strings.HasSuffix(r.URL.Path, "/threads"):
			var request map[string]string
			require.NoError(t, json.NewDecoder(r.Body).Decode(&request))
			assert.Equal(t, "cluster-ask", request["agent"])
			assert.Equal(t, "web", request["app"])
			fmt.Fprint(w, `{}`)
		case strings.HasSuffix(r.URL.Path, "/client-tools"):
			var request struct {
				SessionID string `json:"sessionId"`
				Tools     []struct {
					Name string `json:"name"`
				} `json:"tools"`
			}
			require.NoError(t, json.NewDecoder(r.Body).Decode(&request))
			require.Len(t, request.Tools, 3)
			sessionID = request.SessionID
			w.Header().Set("Content-Type", "text/event-stream")
			fmt.Fprintf(w, "event: ready\ndata: {\"sessionId\":%q}\n\n", sessionID)
			w.(http.Flusher).Flush()
			close(ready)
			for i, call := range calls {
				fmt.Fprintf(w, "event: call\ndata: {\"sessionId\":%q,\"callId\":%q,\"name\":%q,\"arguments\":%s}\n\n", sessionID, fmt.Sprint(i), call.Name, call.Arguments)
				w.(http.Flusher).Flush()
				select {
				case <-ack:
				case <-r.Context().Done():
					return
				}
			}
			close(done)
			<-r.Context().Done()
		case strings.HasSuffix(r.URL.Path, "/client-tools/result"):
			var result struct {
				SessionID string `json:"sessionId"`
				CallID    string `json:"callId"`
				Output    string `json:"output"`
				Error     string `json:"error"`
			}
			require.NoError(t, json.NewDecoder(r.Body).Decode(&result))
			assert.Equal(t, sessionID, result.SessionID)
			assert.Equal(t, fmt.Sprint(callIndex), result.CallID)
			call := calls[callIndex]
			if call.Name == "local_list" {
				assert.ElementsMatch(t, strings.Fields(call.Output), strings.Fields(result.Output))
			} else {
				assert.Equal(t, call.Output, result.Output)
			}
			if call.Error {
				assert.NotEmpty(t, result.Error)
			} else {
				assert.Empty(t, result.Error)
			}
			callIndex++
			w.WriteHeader(http.StatusNoContent)
			ack <- struct{}{}
		case r.Method == http.MethodPost:
			if len(calls) > 0 {
				<-ready
			}
			var request map[string]string
			require.NoError(t, json.NewDecoder(r.Body).Decode(&request))
			assert.Equal(t, "Why does startup fail?", request["body"])
			assert.NotEmpty(t, request["idempotencyKey"])
			w.WriteHeader(http.StatusAccepted)
			fmt.Fprint(w, `{"offset":"0","submissionId":"question"}`)
		case r.Method == http.MethodGet:
			if len(calls) > 0 {
				select {
				case <-done:
				case <-r.Context().Done():
					return
				}
			}
			assert.Equal(t, "updates", r.URL.Query().Get("view"))
			assert.Equal(t, "long-poll", r.URL.Query().Get("live"))
			switch r.URL.Query().Get("offset") {
			case "0":
				w.Header().Set("Stream-Next-Offset", "7")
				fmt.Fprint(w, `[{"type":"stream-checkpoint","incarnation":"worker-1"},{"type":"message-delta","kind":"reasoning","delta":"private"},{"type":"tool-input","toolName":"local_read","input":{"path":".env"}},{"type":"message-delta","kind":"text","delta":"Startup "},{"type":"submission-settled","submissionId":"other","outcome":"failed"}]`)
			case "7":
				fmt.Fprint(w, `[{"type":"stream-checkpoint","incarnation":"worker-1"},{"type":"message-delta","kind":"text","delta":"needs PORT."},{"type":"submission-settled","submissionId":"question","outcome":"completed"}]`)
			default:
				t.Errorf("unexpected offset %s", r.URL.Query().Get("offset"))
			}
		}
	}))
	t.Cleanup(server.Close)
	cfg := clientconfig.NewConfig()
	cfg.SetIdentity("cloud", &clientconfig.IdentityConfig{Type: clientconfig.IdentityToken, Token: token, Issuer: server.URL})
	out, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	return &Context{Context: ctx, ClientConfig: cfg, ClusterConfig: &clientconfig.ClusterConfig{XID: "cloud-cluster", Identity: "cloud"}, ClusterName: "selected", Stdout: out, Stderr: stderr}, out, stderr
}

func TestWorkbenchQuestion(t *testing.T) {
	ctx, output, _ := workbenchTestContext(t, nil)
	require.NoError(t, Workbench(ctx, workbenchOptions{App: "web", Question: []string{"Why does startup fail?"}}))
	require.Equal(t, "Startup needs PORT.\n", output.String())
}

func TestDetectiveLocalSourceTools(t *testing.T) {
	dir := t.TempDir()
	files := map[string]string{
		"main.go":          "package main\nlisten(PORT)\nreturn nil\n",
		"sub/code.go":      "use(PORT)\n",
		"sub/.gitignore":   "private.go\n",
		"sub/private.go":   "secret PORT\n",
		"other/private.go": "public PORT\n",
		".gitignore":       "ignored.go\n",
		"ignored.go":       "secret PORT\n",
		".env":             "PORT=secret\n",
		"key.pem":          "secret PORT\n",
		".miren/app.toml":  "name = 'web'\n",
		"binary":           "PORT\x00\n",
	}
	for name, value := range files {
		path := filepath.Join(dir, name)
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0700))
		require.NoError(t, os.WriteFile(path, []byte(value), 0600))
	}
	require.NoError(t, os.Symlink(filepath.Join(dir, ".env"), filepath.Join(dir, "alias.go")))
	calls := []workbenchTestCall{
		{Name: "local_read", Arguments: `{"path":"main.go","start_line":2,"lines":1}`, Output: "2: listen(PORT)\n[truncated; request a later start_line]\n"},
		{Name: "local_search", Arguments: `{"query":"PORT"}`, Output: "main.go:2: listen(PORT)\nother/private.go:1: public PORT\nsub/code.go:1: use(PORT)\n"},
		{Name: "local_list", Arguments: `{"path":"sub"}`, Output: "code.go\n.gitignore\n"},
		{Name: "local_read", Arguments: `{"path":".miren/app.toml"}`, Output: "1: name = 'web'\n2: \n"},
	}
	for _, path := range []string{"../outside", dir + "/main.go", ".env", "key.pem", "alias.go", "ignored.go", "sub/private.go", "binary"} {
		data, err := json.Marshal(map[string]string{"path": path})
		require.NoError(t, err)
		calls = append(calls, workbenchTestCall{Name: "local_read", Arguments: string(data), Error: true})
	}
	ctx, output, stderr := workbenchTestContext(t, calls)
	require.NoError(t, Detective(ctx, workbenchOptions{App: "web", Dir: dir, Question: []string{"Why does startup fail?"}}))
	require.Equal(t, "Startup needs PORT.\n", output.String())
	require.Contains(t, stderr.String(), "no local writes or shell execution")
}

func TestDetectiveNestedSourceRoot(t *testing.T) {
	for _, ending := range []string{"\n", "\r\n"} {
		t.Run(fmt.Sprintf("ending-%q", ending), func(t *testing.T) {
			repo := t.TempDir()
			dir := filepath.Join(repo, "services", "app")
			require.NoError(t, os.MkdirAll(dir, 0700))
			output, err := exec.Command("git", "init", repo).CombinedOutput()
			require.NoError(t, err, string(output))
			for name, data := range map[string]string{
				".gitignore":                          "# ancestor comment\n\nservices/app/config.production.json\nservices/app/*.go\n",
				"services/.gitignore":                 "app/intermediate.json\n!app/main.go\n",
				"services/app/.gitignore":             "local.json\n",
				"services/app/config.production.json": "PRIVATE_MARKER",
				"services/app/intermediate.json":      "PRIVATE_MARKER",
				"services/app/local.json":             "PRIVATE_MARKER",
				"services/app/main.go":                "public MARKER",
			} {
				if filepath.Base(name) == ".gitignore" {
					data = strings.ReplaceAll(data, "\n", ending)
				}
				require.NoError(t, os.WriteFile(filepath.Join(repo, name), []byte(data), 0600))
			}
			calls := []workbenchTestCall{
				{Name: "local_list", Arguments: `{}`, Output: ".gitignore\nmain.go\n"},
				{Name: "local_search", Arguments: `{"query":"MARKER"}`, Output: "main.go:1: public MARKER\n"},
				{Name: "local_read", Arguments: `{"path":"config.production.json"}`, Error: true},
				{Name: "local_read", Arguments: `{"path":"intermediate.json"}`, Error: true},
				{Name: "local_read", Arguments: `{"path":"local.json"}`, Error: true},
				{Name: "local_read", Arguments: `{"path":"main.go"}`, Output: "1: public MARKER\n"},
			}
			ctx, _, _ := workbenchTestContext(t, calls)
			require.NoError(t, Detective(ctx, workbenchOptions{App: "web", Dir: dir, Question: []string{"Why does startup fail?"}}))
		})
	}
}

func TestDetectiveGitignoreDoesNotExcludeSourceRoot(t *testing.T) {
	for _, pattern := range []string{".*\n", "*\n!main.go\n"} {
		t.Run(strings.TrimSpace(pattern), func(t *testing.T) {
			dir := t.TempDir()
			require.NoError(t, os.WriteFile(filepath.Join(dir, ".gitignore"), []byte(pattern), 0600))
			require.NoError(t, os.WriteFile(filepath.Join(dir, "main.go"), []byte("public MARKER"), 0600))
			calls := []workbenchTestCall{
				{Name: "local_list", Arguments: `{}`, Output: "main.go\n"},
				{Name: "local_search", Arguments: `{"query":"MARKER"}`, Output: "main.go:1: public MARKER\n"},
				{Name: "local_read", Arguments: `{"path":".gitignore"}`, Error: true},
			}
			ctx, _, _ := workbenchTestContext(t, calls)
			require.NoError(t, Detective(ctx, workbenchOptions{App: "web", Dir: dir, Question: []string{"Why does startup fail?"}}))
		})
	}
}

func TestDetectiveGitExclusionSources(t *testing.T) {
	for _, layout := range []string{"root-xdg", "nested-included-config", "linked-worktree", "git-unavailable"} {
		t.Run(layout, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("HOME", home)
			t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "xdg"))
			t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
			repo := t.TempDir()
			runGit := func(args ...string) {
				output, err := exec.Command("git", append([]string{"-C", repo}, args...)...).CombinedOutput()
				require.NoError(t, err, string(output))
			}
			runGit("init")
			dir := repo
			switch layout {
			case "nested-included-config":
				dir = filepath.Join(repo, "services", "app")
				require.NoError(t, os.MkdirAll(dir, 0700))
			case "linked-worktree":
				runGit("-c", "user.name=Test", "-c", "user.email=test@example.com", "commit", "--allow-empty", "-m", "initial")
				dir = filepath.Join(t.TempDir(), "worktree")
				runGit("worktree", "add", "--detach", dir)
			}
			global := filepath.Join(home, "xdg", "git", "ignore")
			require.NoError(t, os.MkdirAll(filepath.Dir(global), 0700))
			if layout == "nested-included-config" || layout == "linked-worktree" {
				global = filepath.Join(home, "custom-ignore")
				included := filepath.Join(home, "included-config")
				require.NoError(t, os.WriteFile(included, []byte("[core]\nexcludesFile = ~/custom-ignore\n"), 0600))
				runGit("config", "include.path", included)
			}
			require.NoError(t, os.WriteFile(global, []byte("global.go\r\noverride.go\r\ninfo-override.go\r\n"), 0600))
			require.NoError(t, os.WriteFile(filepath.Join(repo, ".git", "info", "exclude"), []byte("info.go\r\noverride.go\r\n!info-override.go\r\n"), 0600))
			for name, data := range map[string]string{
				".gitignore":       "!override.go\n",
				"global.go":        "PRIVATE_MARKER",
				"info.go":          "PRIVATE_MARKER",
				"override.go":      "public MARKER",
				"info-override.go": "allowed MARKER",
			} {
				require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(data), 0600))
			}
			calls := []workbenchTestCall{
				{Name: "local_list", Arguments: `{}`, Output: ".gitignore\ninfo-override.go\noverride.go\n"},
				{Name: "local_search", Arguments: `{"query":"MARKER"}`, Output: "info-override.go:1: allowed MARKER\noverride.go:1: public MARKER\n"},
				{Name: "local_read", Arguments: `{"path":"info.go"}`, Error: true},
				{Name: "local_read", Arguments: `{"path":"global.go"}`, Error: true},
				{Name: "local_read", Arguments: `{"path":"override.go"}`, Output: "1: public MARKER\n"},
			}
			if layout == "git-unavailable" {
				t.Setenv("PATH", t.TempDir())
				for i := range calls {
					calls[i].Error = true
					calls[i].Output = ""
				}
			}
			ctx, _, _ := workbenchTestContext(t, calls)
			require.NoError(t, Detective(ctx, workbenchOptions{App: "web", Dir: dir, Question: []string{"Why does startup fail?"}}))
		})
	}
}

func TestWorkbenchRequiresSelectedCloudCluster(t *testing.T) {
	ctx := &Context{Context: context.Background(), Stdout: io.Discard, Stderr: io.Discard}
	require.ErrorContains(t, Workbench(ctx, workbenchOptions{}), "selected cluster")
	ctx.ClientConfig = clientconfig.NewConfig()
	ctx.ClusterConfig = &clientconfig.ClusterConfig{Hostname: "local"}
	require.ErrorContains(t, Detective(ctx, workbenchOptions{}), "no Cloud ID")
}
