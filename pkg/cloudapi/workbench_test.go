package cloudapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"miren.dev/runtime/pkg/cloudapi"
)

func testWorkbench(t *testing.T, handler http.HandlerFunc) *cloudapi.Workbench {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "Bearer access", r.Header.Get("Authorization"))
		switch {
		case strings.HasSuffix(r.URL.Path, "/ensure-context"):
			fmt.Fprint(w, `{"contextId":"context"}`)
		case strings.HasSuffix(r.URL.Path, "/threads"):
			fmt.Fprint(w, `{}`)
		default:
			handler(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	client, err := cloudapi.New(srv.URL, staticToken("access"))
	require.NoError(t, err)
	w, err := client.OpenWorkbench(context.Background(), "cluster", "thread", "")
	require.NoError(t, err)
	return w
}

func TestWorkbenchClientToolsParallelAndCancel(t *testing.T) {
	secondResult := make(chan struct{})
	firstCanceled := make(chan struct{})
	w := testWorkbench(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/client-tools"):
			w.Header().Set("Content-Type", "text/event-stream")
			fmt.Fprint(w, "event: ready\ndata: {\"sessionId\":\"client\"}\n\n")
			fmt.Fprint(w, "event: call\ndata: {\"sessionId\":\"client\",\"callId\":\"first\",\"name\":\"local_read\",\"arguments\":{\"path\":\"blocked\"}}\n\n")
			fmt.Fprint(w, "event: call\ndata: {\"sessionId\":\"client\",\"callId\":\"second\",\"name\":\"local_search\",\"arguments\":{\"query\":\"PORT\"}}\n\n")
			w.(http.Flusher).Flush()
			select {
			case <-secondResult:
			case <-r.Context().Done():
				return
			}
			fmt.Fprint(w, "event: cancel\ndata: {\"sessionId\":\"client\",\"callId\":\"first\"}\n\n")
			w.(http.Flusher).Flush()
			<-r.Context().Done()
		case strings.HasSuffix(r.URL.Path, "/client-tools/result"):
			var result map[string]string
			require.NoError(t, json.NewDecoder(r.Body).Decode(&result))
			assert.Equal(t, "second", result["callId"])
			assert.Equal(t, "client", result["sessionId"])
			assert.Equal(t, "main.go:2: PORT", result["output"])
			w.WriteHeader(http.StatusNoContent)
			close(secondResult)
		}
	})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	tools, err := w.ConnectTools(ctx, "client", []cloudapi.ClientTool{
		{Name: "local_read", Parameters: json.RawMessage(`{"type":"object"}`)},
		{Name: "local_search", Parameters: json.RawMessage(`{"type":"object"}`)},
	})
	require.NoError(t, err)
	defer tools.Close()
	finished := make(chan error, 1)
	go func() {
		finished <- tools.Serve(ctx, func(ctx context.Context, name string, args json.RawMessage) (string, error) {
			if name == "local_read" {
				<-ctx.Done()
				close(firstCanceled)
				return "", ctx.Err()
			}
			assert.JSONEq(t, `{"query":"PORT"}`, string(args))
			return "main.go:2: PORT", nil
		})
	}()
	select {
	case <-firstCanceled:
	case <-ctx.Done():
		t.Fatal("local call was not canceled")
	}
	tools.Close()
	require.Error(t, <-finished)
}

func TestWorkbenchClientToolResultFailure(t *testing.T) {
	w := testWorkbench(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/result") {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		fmt.Fprint(w, "event: ready\ndata: {\"sessionId\":\"client\"}\n\nevent: call\ndata: {\"sessionId\":\"client\",\"callId\":\"one\",\"name\":\"local_read\",\"arguments\":{}}\n\n")
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	tools, err := w.ConnectTools(ctx, "client", []cloudapi.ClientTool{{Name: "local_read", Parameters: json.RawMessage(`{"type":"object"}`)}})
	require.NoError(t, err)
	err = tools.Serve(ctx, func(context.Context, string, json.RawMessage) (string, error) { return "code", nil })
	require.ErrorContains(t, err, "posting local tool result")
	require.ErrorContains(t, err, "401")
}

func TestWorkbenchClientToolReplacementCalls(t *testing.T) {
	for _, mode := range []string{"delayed-ack", "stale-result", "cancel"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			accepted := make(chan struct{}, 8)
			started := make(chan struct{}, 8)
			replacement := make(chan struct{}, 8)
			release := make(chan struct{})
			results := make(chan string, 16)
			w := testWorkbench(t, func(w http.ResponseWriter, r *http.Request) {
				if strings.HasSuffix(r.URL.Path, "/result") {
					var result map[string]string
					if err := json.NewDecoder(r.Body).Decode(&result); err != nil {
						t.Error(err)
						return
					}
					if strings.HasPrefix(result["callId"], "old") {
						if mode == "stale-result" {
							http.Error(w, "stale call", http.StatusConflict)
							w.(http.Flusher).Flush()
							accepted <- struct{}{}
							return
						}
						accepted <- struct{}{}
						select {
						case <-release:
						case <-r.Context().Done():
							return
						}
					}
					if mode != "stale-result" || !strings.HasPrefix(result["callId"], "old") {
						w.WriteHeader(http.StatusNoContent)
					}
					results <- result["callId"]
					return
				}
				fmt.Fprint(w, "event: ready\ndata: {\"sessionId\":\"client\"}\n\n")
				for i := range 8 {
					fmt.Fprintf(w, "event: call\ndata: {\"sessionId\":\"client\",\"callId\":\"old%d\",\"name\":\"local_read\",\"arguments\":{\"old\":true}}\n\n", i)
				}
				w.(http.Flusher).Flush()
				for range 8 {
					ch := accepted
					if mode == "cancel" {
						ch = started
					}
					select {
					case <-ch:
					case <-r.Context().Done():
						return
					}
				}
				for i := range 8 {
					if mode == "cancel" {
						fmt.Fprintf(w, "event: cancel\ndata: {\"sessionId\":\"client\",\"callId\":\"old%d\"}\n\n", i)
					}
					fmt.Fprintf(w, "event: call\ndata: {\"sessionId\":\"client\",\"callId\":\"new%d\",\"name\":\"local_read\",\"arguments\":{}}\n\n", i)
				}
				w.(http.Flusher).Flush()
				<-r.Context().Done()
			})
			tools, err := w.ConnectTools(ctx, "client", []cloudapi.ClientTool{{Name: "local_read", Parameters: json.RawMessage(`{"type":"object"}`)}})
			require.NoError(t, err)
			defer tools.Close()
			finished := make(chan error, 1)
			go func() {
				finished <- tools.Serve(ctx, func(callCtx context.Context, _ string, args json.RawMessage) (string, error) {
					if string(args) == `{"old":true}` {
						if mode == "cancel" {
							started <- struct{}{}
							<-callCtx.Done()
							select {
							case <-release:
							case <-ctx.Done():
							}
							return "", callCtx.Err()
						}
					} else {
						replacement <- struct{}{}
					}
					return "source", nil
				})
			}()
			for range 8 {
				select {
				case <-replacement:
				case err := <-finished:
					t.Fatalf("bridge stopped before replacement execution: %v", err)
				case <-ctx.Done():
					t.Fatal("replacement execution timed out")
				}
			}
			close(release)
			for count := 0; count < 8; {
				select {
				case id := <-results:
					if strings.HasPrefix(id, "new") {
						count++
					}
				case err := <-finished:
					t.Fatalf("bridge stopped before replacement results: %v", err)
				case <-ctx.Done():
					t.Fatal("replacement results timed out")
				}
			}
			cancel()
			<-finished
		})
	}
}

func TestWorkbenchApprovalRequestIDs(t *testing.T) {
	requestIDs := []string{" request\n one ", "second-request"}
	turn, prompts := 0, 0
	w := testWorkbench(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			var request map[string]string
			require.NoError(t, json.NewDecoder(r.Body).Decode(&request))
			if request["kind"] == "signal" {
				require.Less(t, turn, len(requestIDs))
				assert.Equal(t, "workbench.approval", request["type"])
				var decision map[string]string
				require.NoError(t, json.Unmarshal([]byte(request["body"]), &decision))
				assert.Equal(t, map[string]string{"answer": "once", "key": "restart:web", "fingerprint": "same-operation", "requestId": requestIDs[turn]}, decision)
				turn++
			}
			fmt.Fprintf(w, `{"offset":"%d","submissionId":"submission-%d"}`, turn, turn)
			return
		}
		assert.Equal(t, fmt.Sprint(turn), r.URL.Query().Get("offset"))
		var chunks []map[string]any
		if turn < len(requestIDs) {
			payload, err := json.Marshal(map[string]string{
				"kind": "approval", "tool": "miren_restart_app", "key": "restart:web", "fingerprint": "same-operation", "requestId": requestIDs[turn], "summary": "Restart web",
			})
			require.NoError(t, err)
			output := "Waiting for permission: Restart web.\nApprove here: https://cloud.example/ask\n[approval-request] " + string(payload)
			chunks = append(chunks,
				map[string]any{"type": "tool-input", "toolCallId": "write", "toolName": "miren_restart_app"},
				map[string]any{"type": "tool-output", "toolCallId": "write", "output": output},
				map[string]any{"type": "tool-output", "toolCallId": "write", "output": output})
		}
		chunks = append(chunks,
			map[string]any{"type": "message-delta", "kind": "text", "delta": fmt.Sprintf("turn%d ", turn)},
			map[string]any{"type": "submission-settled", "submissionId": fmt.Sprintf("submission-%d", turn), "outcome": "completed"},
		)
		require.NoError(t, json.NewEncoder(w).Encode(chunks))
	})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var out bytes.Buffer
	err := w.AskWithApprovals(ctx, "restart twice", "key", &out, func(_ context.Context, request cloudapi.WorkbenchApproval) (string, error) {
		require.Less(t, prompts, len(requestIDs))
		assert.Equal(t, requestIDs[prompts], request.RequestID)
		prompts++
		return "once", nil
	})
	require.NoError(t, err)
	assert.Equal(t, 2, turn)
	assert.Equal(t, 2, prompts)
	assert.Equal(t, "turn0 turn1 turn2 ", out.String())
}

func TestWorkbenchAskCancellationAndFailure(t *testing.T) {
	for _, mode := range []string{"cancel", "failed"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			aborted := make(chan struct{}, 1)
			w := testWorkbench(t, func(w http.ResponseWriter, r *http.Request) {
				switch {
				case strings.HasSuffix(r.URL.Path, "/abort"):
					aborted <- struct{}{}
					fmt.Fprint(w, `{"aborted":true}`)
				case r.Method == http.MethodPost:
					w.WriteHeader(http.StatusAccepted)
					fmt.Fprint(w, `{"offset":"0","submissionId":"question"}`)
				default:
					if mode == "cancel" {
						cancel()
						<-r.Context().Done()
						return
					}
					fmt.Fprint(w, `[{"type":"submission-settled","submissionId":"question","outcome":"failed","error":{"message":"model failed"}}]`)
				}
			})
			var out bytes.Buffer
			err := w.Ask(ctx, "question", "key", &out)
			if mode == "cancel" {
				require.ErrorIs(t, err, context.Canceled)
				select {
				case <-aborted:
				default:
					t.Fatal("canceled submission not aborted")
				}
			} else {
				require.ErrorContains(t, err, "model failed")
				select {
				case <-aborted:
					t.Fatal("already-settled submission aborted")
				default:
				}
			}
		})
	}
}

func TestWorkbenchApprovals(t *testing.T) {
	for _, answer := range []string{"once", "session", "deny", "noninteractive", "prose-only", "invalid-answer", "missing-receipt", "malformed-receipt", "canceled-receipt"} {
		t.Run(answer, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			const payload = `{"kind":"approval","tool":"miren_restart_app","key":"restart:web\nexact","fingerprint":" fingerprint:123 ","summary":"Restart web","detail":{"app":"web","count":2},"consequence":"Instances stop and start again."}`
			const fallback = "Waiting for permission: Restart web. The person has been shown this.\nOpen https://cloud.example/organizations/org/clusters/cluster/ask?thread=thread\n[approval-request] " + payload
			signals, prompts := 0, 0
			receiptFailure := answer == "missing-receipt" || answer == "malformed-receipt" || answer == "canceled-receipt"
			aborted := make(chan struct{}, 1)
			w := testWorkbench(t, func(w http.ResponseWriter, r *http.Request) {
				switch {
				case strings.HasSuffix(r.URL.Path, "/abort"):
					aborted <- struct{}{}
					w.WriteHeader(http.StatusNoContent)
				case r.Method == http.MethodPost:
					var request map[string]string
					require.NoError(t, json.NewDecoder(r.Body).Decode(&request))
					if request["kind"] == "user" {
						fmt.Fprint(w, `{"offset":"0","submissionId":"question"}`)
						return
					}
					signals++
					assert.Equal(t, "signal", request["kind"])
					assert.Equal(t, "workbench.approval", request["type"])
					want := answer
					if receiptFailure {
						want = "once"
					}
					assert.JSONEq(t, fmt.Sprintf(`{"answer":%q,"key":"restart:web\nexact","fingerprint":" fingerprint:123 "}`, want), request["body"])
					switch answer {
					case "missing-receipt":
						fmt.Fprint(w, `{}`)
						return
					case "malformed-receipt":
						w.WriteHeader(http.StatusAccepted)
						fmt.Fprint(w, `{"offset":`)
						return
					case "canceled-receipt":
						w.WriteHeader(http.StatusAccepted)
						w.(http.Flusher).Flush()
						cancel()
						<-r.Context().Done()
						return
					}
					fmt.Fprint(w, `{"offset":"42","submissionId":"decision"}`)
				default:
					if r.URL.Query().Get("offset") == "42" {
						// The original turn settling again must not end the decision turn.
						fmt.Fprint(w, `[{"type":"submission-settled","submissionId":"question","outcome":"completed"},{"type":"message-delta","kind":"text","delta":"Decision followed."},{"type":"submission-settled","submissionId":"decision","outcome":"completed"}]`)
						return
					}
					if r.URL.Query().Get("offset") == "0" {
						w.Header().Set("Stream-Next-Offset", "1")
						fmt.Fprint(w, `[{"type":"tool-input","toolCallId":"write","toolName":"miren_restart_app"}]`)
						return
					}
					assert.Equal(t, "1", r.URL.Query().Get("offset"))
					chunks := []map[string]any{
						{"type": "message-delta", "kind": "text", "delta": "Assistant says: " + fallback},
						{"type": "tool-input", "output": fallback},
						{"type": "tool-output-error", "output": fallback},
						{"type": "tool-output", "output": `[approval-request] {"kind":"approval","key":"incomplete"}`},
						{"type": "tool-input", "toolCallId": "read", "toolName": "miren_get_logs"},
						{"type": "tool-output", "toolCallId": "read", "output": fallback},
						{"type": "tool-output", "toolCallId": "unknown", "output": fallback},
						{"type": "tool-input", "toolCallId": "mismatch", "toolName": "miren_deploy"},
						{"type": "tool-output", "toolCallId": "mismatch", "output": fallback},
						{"type": "tool-output", "toolCallId": "write", "output": "App output\n[approval-request] " + payload},
						{"type": "tool-output", "toolCallId": "write", "output": strings.Replace(fallback, "Waiting for permission: Restart web.", "Waiting for permission: Another operation.", 1)},
					}
					if answer != "prose-only" {
						chunks = append(chunks, map[string]any{"type": "tool-output", "toolCallId": "write", "output": fallback}, map[string]any{"type": "tool-output", "toolCallId": "write", "output": fallback})
					}
					chunks = append(chunks, map[string]any{"type": "submission-settled", "submissionId": "question", "outcome": "completed"})
					require.NoError(t, json.NewEncoder(w).Encode(chunks))
				}
			})
			var approve func(context.Context, cloudapi.WorkbenchApproval) (string, error)
			if answer != "noninteractive" {
				approve = func(_ context.Context, request cloudapi.WorkbenchApproval) (string, error) {
					prompts++
					assert.Equal(t, "restart:web\nexact", request.Key)
					assert.Equal(t, " fingerprint:123 ", request.Fingerprint)
					assert.JSONEq(t, `{"app":"web","count":2}`, string(request.Detail))
					if receiptFailure {
						return "once", nil
					}
					return answer, nil
				}
			}
			var out bytes.Buffer
			err := w.AskWithApprovals(ctx, "restart web", "key", &out, approve)
			switch answer {
			case "invalid-answer":
				require.ErrorContains(t, err, "invalid workbench approval decision")
				assert.Zero(t, signals)
			case "missing-receipt":
				require.ErrorContains(t, err, "invalid workbench admission receipt")
			case "malformed-receipt":
				require.ErrorContains(t, err, "failed to parse workbench response")
			case "canceled-receipt":
				require.ErrorIs(t, err, context.Canceled)
			case "noninteractive", "prose-only":
				require.NoError(t, err)
				assert.Zero(t, prompts)
				assert.Zero(t, signals)
				if answer == "noninteractive" {
					assert.Contains(t, out.String(), fallback)
				}
			default:
				require.NoError(t, err)
				assert.Equal(t, 1, prompts)
				assert.Equal(t, 1, signals)
				assert.Contains(t, out.String(), "Decision followed.")
			}
			if receiptFailure {
				assert.Equal(t, 1, prompts)
				assert.Equal(t, 1, signals, "approval signals must not be retried")
				assert.Len(t, aborted, 1, "abort cleanup must finish before Ask returns")
			}
		})
	}
}
