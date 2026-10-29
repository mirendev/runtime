package cloudapi

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

// ClientTool declares a model-callable tool that executes beside the CLI.
type ClientTool struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"`
}

type clientToolEvent struct {
	SessionID string          `json:"sessionId"`
	CallID    string          `json:"callId"`
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

// ClientTools holds the dedicated tool stream, separate from chat activity.
type ClientTools struct {
	workbench *Workbench
	sessionID string
	response  *http.Response
	scanner   *bufio.Scanner
	cancel    context.CancelFunc
	names     map[string]bool
}

// ConnectTools waits for Cloud to register the tools before the first question.
// Closing the connection removes them; reconnects must use a new session ID.
func (w *Workbench) ConnectTools(ctx context.Context, sessionID string, tools []ClientTool) (*ClientTools, error) {
	ctx, cancel := context.WithCancel(ctx)
	readyTimeout := time.AfterFunc(workbenchTimeout, cancel)
	defer readyTimeout.Stop()
	resp, err := w.client.request(ctx, 0, http.MethodPost, w.endpoint+"/client-tools", struct {
		SessionID string       `json:"sessionId"`
		Tools     []ClientTool `json:"tools"`
	}{sessionID, tools})
	if err != nil {
		cancel()
		return nil, err
	}
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 4096), 512<<10)
	names := make(map[string]bool, len(tools))
	for _, tool := range tools {
		names[tool.Name] = true
	}
	t := &ClientTools{workbench: w, sessionID: sessionID, response: resp, scanner: scanner, cancel: cancel, names: names}
	kind, event, err := t.next()
	if err != nil {
		t.Close()
		return nil, fmt.Errorf("cloud did not register local tools: %w", err)
	}
	if kind != "ready" || event.SessionID != sessionID {
		t.Close()
		return nil, fmt.Errorf("cloud returned an invalid local tool registration")
	}
	return t, nil
}

func (t *ClientTools) Close() error {
	t.cancel()
	return t.response.Body.Close()
}

func (t *ClientTools) next() (string, clientToolEvent, error) {
	kind := ""
	var data strings.Builder
	for t.scanner.Scan() {
		line := t.scanner.Text()
		if line == "" {
			if data.Len() == 0 {
				continue
			}
			var event clientToolEvent
			err := json.Unmarshal([]byte(data.String()), &event)
			return kind, event, err
		}
		if value, ok := strings.CutPrefix(line, "event:"); ok {
			kind = strings.TrimSpace(value)
		}
		if value, ok := strings.CutPrefix(line, "data:"); ok {
			if data.Len() > 0 {
				data.WriteByte('\n')
			}
			data.WriteString(strings.TrimPrefix(value, " "))
			if data.Len() > 512<<10 {
				return "", clientToolEvent{}, fmt.Errorf("local tool event too large")
			}
		}
	}
	if err := t.scanner.Err(); err != nil {
		return "", clientToolEvent{}, err
	}
	return "", clientToolEvent{}, io.EOF
}

// Serve executes only dedicated call events and returns correlated results.
// Calls run independently so a cancel event can interrupt one still executing.
func (t *ClientTools) Serve(ctx context.Context, execute func(context.Context, string, json.RawMessage) (string, error)) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	var wg sync.WaitGroup
	var mu sync.Mutex
	pending := make(map[string]context.CancelFunc)
	running := make(map[string]bool)
	uploads := make(chan struct{}, 8)
	var resultError error
	defer func() { cancel(); t.Close(); wg.Wait() }()
	go func() {
		<-ctx.Done()
		t.Close()
	}()
	for {
		kind, event, err := t.next()
		if err != nil {
			mu.Lock()
			failure := resultError
			mu.Unlock()
			if failure != nil {
				return fmt.Errorf("posting local tool result: %w", failure)
			}
			return fmt.Errorf("local tool connection closed: %w", err)
		}
		if event.SessionID != t.sessionID {
			return fmt.Errorf("local tool session mismatch")
		}
		switch kind {
		case "cancel":
			mu.Lock()
			if stop := pending[event.CallID]; stop != nil {
				stop()
				delete(running, event.CallID)
			}
			mu.Unlock()
		case "call":
			if event.CallID == "" || !t.names[event.Name] || !strings.HasPrefix(event.Name, "local_") {
				return fmt.Errorf("invalid local tool call")
			}
			mu.Lock()
			if _, exists := pending[event.CallID]; exists || len(running) >= 8 {
				mu.Unlock()
				return fmt.Errorf("duplicate or excessive pending local tool calls")
			}
			callCtx, stop := context.WithTimeout(ctx, 60*time.Second)
			pending[event.CallID] = stop
			running[event.CallID] = true
			mu.Unlock()
			wg.Add(1)
			go func() {
				defer wg.Done()
				defer stop()
				defer func() {
					mu.Lock()
					delete(pending, event.CallID)
					delete(running, event.CallID)
					mu.Unlock()
				}()
				output, callErr := execute(callCtx, event.Name, event.Arguments)
				mu.Lock()
				delete(running, event.CallID)
				mu.Unlock()
				if callCtx.Err() != nil {
					return
				}
				// Result acknowledgments can lag replacement calls on the SSE stream.
				// Bound uploads separately from calls still executing locally.
				select {
				case uploads <- struct{}{}:
					defer func() { <-uploads }()
				case <-callCtx.Done():
					return
				}
				result := struct {
					SessionID string `json:"sessionId"`
					CallID    string `json:"callId"`
					Output    string `json:"output"`
					Error     string `json:"error,omitempty"`
				}{SessionID: t.sessionID, CallID: event.CallID, Output: output}
				if callErr != nil {
					result.Error = callErr.Error()
				}
				if err := t.workbench.client.workbenchJSON(callCtx, http.MethodPost, t.workbench.endpoint+"/client-tools/result", result, nil); err != nil && callCtx.Err() == nil {
					var response *responseError
					if errors.As(err, &response) && response.status == http.StatusConflict {
						return
					}
					mu.Lock()
					resultError = err
					mu.Unlock()
					cancel()
				}
			}()
		}
	}
}
