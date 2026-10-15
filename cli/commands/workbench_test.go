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

	"github.com/charmbracelet/x/ansi"
	"github.com/containerd/console"
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

func workbenchTestContext(t *testing.T, calls []workbenchTestCall, app string, beforeUpdate ...func()) (*Context, *bytes.Buffer, *bytes.Buffer) {
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
			assert.Equal(t, app, request["app"])
			fmt.Fprint(w, `{}`)
		case strings.HasSuffix(r.URL.Path, "/client-tools"):
			if calls == nil {
				t.Error("local tools must not be registered")
				w.WriteHeader(http.StatusForbidden)
				return
			}
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
		case strings.HasSuffix(r.URL.Path, "/abort"):
			w.WriteHeader(http.StatusNoContent)
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
			for _, wait := range beforeUpdate {
				wait()
			}
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
				fmt.Fprint(w, `[{"type":"stream-checkpoint","incarnation":"worker-1"},{"type":"message-delta","kind":"reasoning","delta":"private"},{"type":"tool-input","toolName":"local_read","input":{"path":".env"}},{"type":"message-delta","kind":"text","delta":"**Startup** "},{"type":"submission-settled","submissionId":"other","outcome":"failed"}]`)
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

func TestDetectiveWithoutLocalTools(t *testing.T) {
	ctx, output, stderr := workbenchTestContext(t, nil, "web")
	require.NoError(t, Detective(ctx, workbenchOptions{App: "web", NoLocalTools: true, Dir: filepath.Join(t.TempDir(), "missing"), Question: []string{"Why does startup fail?"}}))
	require.Equal(t, "**Startup** needs PORT.\n", output.String())
	require.Empty(t, stderr.String())
}

func TestDetectiveAppDefault(t *testing.T) {
	for _, tc := range []struct {
		name    string
		app     string
		want    string
		dir     bool
		invalid bool
	}{
		{name: "current directory", want: "configured-app"},
		{name: "parent directory", want: "configured-app"},
		{name: "explicit directory", want: "other-app", dir: true},
		{name: "explicit app", app: "override-app", want: "override-app"},
		{name: "no app"},
		{name: "broken local config", invalid: true},
		{name: "broken explicit config", dir: true, invalid: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			if tc.name != "no app" {
				writeAppToml(t, dir, `name = "configured-app"`)
			}
			if tc.invalid && !tc.dir {
				writeAppToml(t, dir, `[[[`)
			}
			cwd := dir
			if tc.name == "parent directory" {
				cwd = filepath.Join(dir, "sub")
				require.NoError(t, os.Mkdir(cwd, 0700))
			}
			chdir(t, cwd)
			opts := workbenchOptions{App: tc.app, Dir: ".", Question: []string{"Why does startup fail?"}}
			if tc.dir {
				opts.Dir = t.TempDir()
				writeAppToml(t, opts.Dir, `name = "other-app"`)
				if tc.invalid {
					writeAppToml(t, opts.Dir, `[[[`)
				}
			}
			ctx, output, _ := workbenchTestContext(t, []workbenchTestCall{}, tc.want)
			require.NoError(t, Detective(ctx, opts))
			require.Equal(t, "**Startup** needs PORT.\n", output.String())
		})
	}
}

func TestDetectiveTerminal(t *testing.T) {
	for _, mode := range []string{"question", "cancel", "chat"} {
		t.Run(mode, func(t *testing.T) {
			master, path, err := console.NewPty()
			require.NoError(t, err)
			t.Cleanup(func() { master.Close() })
			require.NoError(t, master.Resize(console.WinSize{Width: 80, Height: 24}))
			terminal, err := os.OpenFile(path, os.O_RDWR, 0)
			require.NoError(t, err)
			t.Cleanup(func() { terminal.Close() })
			var captured syncBuffer
			go func() { _, _ = io.Copy(&captured, master) }()
			release := make(chan struct{})
			ctx, _, _ := workbenchTestContext(t, []workbenchTestCall{{Name: "local_list", Arguments: `{}`, Output: "main.go\n"}}, "web", func() { <-release })
			t.Cleanup(func() {
				select {
				case <-release:
				default:
					close(release)
				}
			})
			var cancelContext context.CancelFunc
			ctx.Context, cancelContext = context.WithCancel(ctx.Context)
			t.Cleanup(cancelContext)
			ctx.Stdout, ctx.Stderr = terminal, terminal
			dir := t.TempDir()
			require.NoError(t, os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n"), 0600))
			opts := workbenchOptions{App: "web", Dir: dir, Question: []string{"Why does startup fail?"}}
			if mode == "chat" {
				stdin := os.Stdin
				os.Stdin = terminal
				t.Cleanup(func() { os.Stdin = stdin })
				opts.Question = nil
			}
			finished := make(chan error, 1)
			go func() {
				finished <- Detective(ctx, opts)
			}()
			if mode == "chat" {
				require.Eventually(t, func() bool { return strings.Contains(captured.String(), "> ") }, 3*time.Second, 10*time.Millisecond)
				_, err := master.Write([]byte("Why does startup fail?\n"))
				require.NoError(t, err)
			}
			require.Eventually(t, func() bool {
				return strings.Contains(captured.String(), "Waiting for Miren Cloud") && strings.Contains(ansi.Strip(captured.String()), "Listing .")
			}, 3*time.Second, 10*time.Millisecond)
			if mode == "cancel" {
				cancelContext()
			}
			close(release)
			if mode == "chat" {
				for turn := 1; turn <= 2; turn++ {
					require.Eventually(t, func() bool {
						return strings.Count(ansi.Strip(captured.String()), "Startup needs PORT.") >= turn && strings.Count(captured.String(), "> ") >= turn+1
					}, 3*time.Second, 10*time.Millisecond)
					question := "Why does startup fail?\n"
					if turn == 2 {
						question = "/quit\n"
					}
					_, err := master.Write([]byte(question))
					require.NoError(t, err)
				}
			}
			if mode == "cancel" {
				require.ErrorIs(t, <-finished, context.Canceled)
			} else {
				require.NoError(t, <-finished)
				require.Eventually(t, func() bool {
					return strings.Contains(ansi.Strip(captured.String()), "Startup needs PORT.")
				}, time.Second, time.Millisecond)
				require.NotContains(t, captured.String(), "**Startup**")
			}
		})
	}
}

func TestDetectiveApprovals(t *testing.T) {
	for _, mode := range []string{"once", "session", "deny", "chat", "narrow", "hostile", "noninteractive", "redirected", "cancel", "cancel-active", "eof", "eof-active", "eof-idle", "cancel-idle"} {
		t.Run(mode, func(t *testing.T) {
			master, path, err := console.NewPty()
			require.NoError(t, err)
			defer master.Close()
			require.NoError(t, master.Resize(console.WinSize{Width: 80, Height: 24}))
			if mode == "narrow" {
				require.NoError(t, master.Resize(console.WinSize{Width: 40, Height: 24}))
			}
			terminal, err := os.OpenFile(path, os.O_RDWR, 0)
			require.NoError(t, err)
			defer terminal.Close()
			var captured syncBuffer
			go func() { _, _ = io.Copy(&captured, master) }()
			decisions := make(chan string, 4)
			polling, aborted := make(chan struct{}, 1), make(chan struct{}, 1)
			fallback := "Waiting for permission: Restart web.\nOpen https://cloud.example/organizations/org/clusters/cluster/ask?thread=owned\n[approval-request] " + `{"kind":"approval","tool":"miren_restart_app","key":"restart:web","fingerprint":"exact fingerprint","summary":"Restart web","detail":{"app":"web","instances":2},"consequence":"Instances stop and start again."}`
			if mode == "narrow" {
				fallback = strings.Replace(fallback, `"instances":2`, `"instances":2,"notes":"`+strings.Repeat("review this argument ", 60)+`","zz_end":true`, 1)
			}
			if mode == "hostile" {
				fallback = strings.Replace(fallback, `"instances":2`, "\"instances\":2,\"notes\":\"before\u202eafter\u2066isolated\u2069\u0085\"", 1)
			}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch {
				case strings.HasSuffix(r.URL.Path, "/ensure-context"):
					fmt.Fprint(w, `{"contextId":"scope"}`)
				case strings.HasSuffix(r.URL.Path, "/threads"):
					fmt.Fprint(w, `{}`)
				case strings.HasSuffix(r.URL.Path, "/abort"):
					aborted <- struct{}{}
					w.WriteHeader(http.StatusNoContent)
				case r.Method == http.MethodPost:
					var request map[string]string
					require.NoError(t, json.NewDecoder(r.Body).Decode(&request))
					if request["kind"] == "signal" {
						assert.Equal(t, "workbench.approval", request["type"])
						var decision map[string]string
						require.NoError(t, json.Unmarshal([]byte(request["body"]), &decision))
						assert.Equal(t, "restart:web", decision["key"])
						assert.Equal(t, "exact fingerprint", decision["fingerprint"])
						decisions <- decision["answer"]
						fmt.Fprint(w, `{"offset":"20","submissionId":"decision"}`)
					} else {
						assert.Equal(t, "Restart web", request["body"])
						fmt.Fprint(w, `{"offset":"0","submissionId":"question"}`)
					}
				default:
					if mode == "cancel-active" || mode == "eof-active" {
						polling <- struct{}{}
						<-r.Context().Done()
						return
					}
					if r.URL.Query().Get("offset") == "20" {
						fmt.Fprint(w, `[{"type":"message-delta","kind":"text","delta":"Decision response followed."},{"type":"submission-settled","submissionId":"decision","outcome":"completed"}]`)
						return
					}
					_ = json.NewEncoder(w).Encode([]map[string]any{
						{"type": "tool-input", "toolCallId": "write", "toolName": "miren_restart_app"},
						{"type": "tool-output", "toolCallId": "write", "output": fallback},
						{"type": "tool-output", "toolCallId": "write", "output": fallback},
						{"type": "submission-settled", "submissionId": "question", "outcome": "completed"},
					})
				}
			}))
			defer server.Close()
			cfg := clientconfig.NewConfig()
			cfg.SetIdentity("cloud", &clientconfig.IdentityConfig{Type: clientconfig.IdentityToken, Token: freshLoginJWT(t), Issuer: server.URL})
			deadline, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			ctx := &Context{Context: deadline, ClientConfig: cfg, ClusterConfig: &clientconfig.ClusterConfig{XID: "cluster", Identity: "cloud"}, Stdout: terminal, Stderr: terminal}
			stdin := os.Stdin
			os.Stdin = terminal
			defer func() { os.Stdin = stdin }()
			var plain bytes.Buffer
			if mode == "noninteractive" {
				reader, writer, err := os.Pipe()
				require.NoError(t, err)
				defer reader.Close()
				_, err = writer.WriteString("once\n")
				require.NoError(t, err)
				require.NoError(t, writer.Close())
				os.Stdin = reader
			}
			if mode == "redirected" {
				ctx.Stdout, ctx.Stderr = &plain, &plain
			}
			opts := workbenchOptions{NoLocalTools: true, Question: []string{"Restart web"}}
			if mode == "chat" || strings.HasSuffix(mode, "-idle") {
				opts.Question = nil
			}
			finished := make(chan error, 1)
			go func() { finished <- Detective(ctx, opts) }()
			if strings.HasSuffix(mode, "-idle") {
				require.Eventually(t, func() bool { return strings.Contains(captured.String(), "> ") }, 3*time.Second, 10*time.Millisecond)
				key := "\x04"
				if mode == "cancel-idle" {
					key = "\x03"
				}
				_, err = master.Write([]byte(key))
				require.NoError(t, err)
				if mode == "cancel-idle" {
					require.ErrorIs(t, <-finished, context.Canceled)
				} else {
					require.NoError(t, <-finished)
				}
				assert.Empty(t, decisions)
				return
			}
			if mode == "cancel-active" || mode == "eof-active" {
				select {
				case <-polling:
				case <-deadline.Done():
					t.Fatal("question did not start")
				}
				key := "\x03"
				if mode == "eof-active" {
					key = "\x04"
				}
				_, err = master.Write([]byte(key))
				require.NoError(t, err)
				require.ErrorIs(t, <-finished, context.Canceled)
				require.Len(t, aborted, 1, "active response must be aborted before CLI exit")
				return
			}
			if mode == "chat" {
				require.Eventually(t, func() bool { return strings.Contains(captured.String(), "> ") }, 3*time.Second, 10*time.Millisecond)
				_, err = master.Write([]byte("Restart web\r"))
				require.NoError(t, err)
			}
			interactive := mode != "noninteractive" && mode != "redirected"
			if interactive {
				require.Eventually(t, func() bool { return strings.Contains(captured.String(), "Enter confirm") }, 3*time.Second, 10*time.Millisecond)
				for _, text := range []string{"Restart web", `"app": "web"`, "Consequences: Instances stop", "Allow once", "Allow for conversation", "Deny"} {
					assert.Contains(t, ansi.Strip(captured.String()), text)
				}
				if mode == "hostile" {
					assert.Contains(t, captured.String(), `before\u202eafter\u2066isolated\u2069\u0085`)
					assert.NotContains(t, captured.String(), "\u202e")
					assert.NotContains(t, captured.String(), "\u0085")
				}
				if mode == "narrow" {
					_, err = master.Write([]byte(strings.Repeat("\x1b[6~", 10)))
					require.NoError(t, err)
					require.Eventually(t, func() bool { return strings.Contains(captured.String(), `"zz_end": true`) }, time.Second, time.Millisecond)
				}
				// Chat prose and a bracketed paste cannot select an allow choice.
				_, err = master.Write([]byte("yes allow once\x1b[200~once\n\x1b[201~down"))
				require.NoError(t, err)
				select {
				case got := <-decisions:
					t.Fatalf("text admitted a decision: %s", got)
				case <-time.After(80 * time.Millisecond):
				}
				keys := "\r"
				switch mode {
				case "once":
					keys = "\x1b[B\r"
				case "session":
					keys = "\x1b[A\r"
				case "cancel":
					keys = "\x03"
				case "eof":
					keys = "\x04"
				}
				_, err = master.Write([]byte(keys))
				require.NoError(t, err)
				if mode != "cancel" && mode != "eof" {
					want := mode
					if mode == "chat" || mode == "narrow" || mode == "hostile" {
						want = "deny"
					}
					select {
					case got := <-decisions:
						assert.Equal(t, want, got)
					case <-deadline.Done():
						t.Fatal("decision not admitted")
					}
					require.Eventually(t, func() bool { return strings.Contains(ansi.Strip(captured.String()), "Decision response followed.") }, 3*time.Second, 10*time.Millisecond)
				}
				if mode == "chat" {
					_, err = master.Write([]byte("/quit\r"))
					require.NoError(t, err)
				}
			}
			if mode == "cancel" || mode == "eof" {
				require.ErrorIs(t, <-finished, context.Canceled)
			} else {
				require.NoError(t, <-finished)
			}
			select {
			case got := <-decisions:
				t.Fatalf("unexpected additional decision: %s", got)
			default:
			}
			if !interactive {
				require.Eventually(t, func() bool {
					return strings.Contains(captured.String()+plain.String(), "https://cloud.example/organizations/org/clusters/cluster/ask?thread=owned")
				}, time.Second, time.Millisecond)
				assert.NotContains(t, captured.String()+plain.String(), "Approval required:")
			}
		})
	}
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
		{Name: "local_read", Arguments: `{"path":"main.go","start_line":2,"lines":2}`, Output: "2: listen(PORT)\n3: return nil\n[truncated; request a later start_line]\n"},
		{Name: "local_search", Arguments: `{"query":"PORT"}`, Output: "main.go:2: listen(PORT)\nother/private.go:1: public PORT\nsub/code.go:1: use(PORT)\n"},
		{Name: "local_search", Arguments: `{"query":"PORT\n\u001b[2J"}`, Output: ""},
		{Name: "local_list", Arguments: `{"path":"sub"}`, Output: "code.go\n.gitignore\n"},
		{Name: "local_read", Arguments: `{"path":".miren/app.toml"}`, Output: "1: name = 'web'\n2: \n"},
	}
	for _, path := range []string{"../outside", dir + "/main.go", ".env", "key.pem", "alias.go", "ignored.go", "sub/private.go", "binary"} {
		data, err := json.Marshal(map[string]string{"path": path})
		require.NoError(t, err)
		calls = append(calls, workbenchTestCall{Name: "local_read", Arguments: string(data), Error: true})
	}
	ctx, output, stderr := workbenchTestContext(t, calls, "web")
	require.NoError(t, Detective(ctx, workbenchOptions{Dir: dir, Question: []string{"Why does startup fail?"}}))
	require.Equal(t, "**Startup** needs PORT.\n", output.String())
	require.NotContains(t, stderr.String(), "Detective can read source")
	require.NotContains(t, stderr.String(), "Requested content is sent")
	require.Contains(t, stderr.String(), "Reading main.go")
	require.Contains(t, stderr.String(), "line 2")
	require.Contains(t, stderr.String(), "lines 2–3")
	require.Contains(t, stderr.String(), `Searching "PORT"`)
	require.Contains(t, stderr.String(), `Searching "PORT\n\x1b[2J"`)
	require.NotContains(t, stderr.String(), "\x1b[2J")
	require.Contains(t, stderr.String(), "Listing sub")
	require.NotContains(t, stderr.String(), "local_read {")
	require.NotContains(t, stderr.String(), "listen(PORT)")
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
			ctx, _, _ := workbenchTestContext(t, calls, "web")
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
			ctx, _, _ := workbenchTestContext(t, calls, "web")
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
			ctx, _, _ := workbenchTestContext(t, calls, "web")
			require.NoError(t, Detective(ctx, workbenchOptions{App: "web", Dir: dir, Question: []string{"Why does startup fail?"}}))
		})
	}
}

func TestWorkbenchRequiresSelectedCloudCluster(t *testing.T) {
	ctx := &Context{Context: context.Background(), Stdout: io.Discard, Stderr: io.Discard}
	require.ErrorContains(t, Detective(ctx, workbenchOptions{}), "selected cluster")
	ctx.ClientConfig = clientconfig.NewConfig()
	ctx.ClusterConfig = &clientconfig.ClusterConfig{Hostname: "local"}
	require.ErrorContains(t, Detective(ctx, workbenchOptions{}), "no Cloud ID")
}
