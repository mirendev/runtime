package cloudapi

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const workbenchTimeout = 35 * time.Second

// Workbench is one cluster-scoped conversation, owned by the logged-in user.
type Workbench struct {
	client   *Client
	endpoint string
}

// OpenWorkbench claims a fresh conversation before delivering any messages.
func (c *Client) OpenWorkbench(ctx context.Context, clusterXID, threadID, app string) (*Workbench, error) {
	if clusterXID == "" || threadID == "" {
		return nil, fmt.Errorf("workbench needs a cloud cluster ID and a conversation ID")
	}
	endpoint, err := url.JoinPath(c.baseURL, "workbench/api/clusters", clusterXID, "ensure-context")
	if err != nil {
		return nil, err
	}
	var scope struct {
		ContextID string `json:"contextId"`
	}
	if err := c.workbenchJSON(ctx, http.MethodPost, endpoint, struct{}{}, &scope); err != nil {
		return nil, err
	}
	if scope.ContextID == "" {
		return nil, fmt.Errorf("cloud returned no workbench context ID")
	}
	endpoint, err = url.JoinPath(c.baseURL, "workbench/api/contexts", scope.ContextID, "threads")
	if err != nil {
		return nil, err
	}
	if err := c.workbenchJSON(ctx, http.MethodPost, endpoint, map[string]string{
		"agent": "cluster-ask", "id": threadID, "app": app,
	}, nil); err != nil {
		return nil, err
	}
	endpoint, err = url.JoinPath(c.baseURL, "workbench/api/agents/cluster-ask", threadID)
	if err != nil {
		return nil, err
	}
	return &Workbench{client: c, endpoint: endpoint}, nil
}

func (c *Client) workbenchJSON(ctx context.Context, method, endpoint string, body, out any) error {
	resp, err := c.request(ctx, workbenchTimeout, method, endpoint, body)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if out == nil {
		return nil
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(out); err != nil {
		return fmt.Errorf("failed to parse workbench response: %w", err)
	}
	return nil
}

// WorkbenchApproval is a Cloud write request, not a local tool invocation.
type WorkbenchApproval struct {
	Kind        string          `json:"kind"`
	Tool        string          `json:"tool"`
	Key         string          `json:"key"`
	Fingerprint string          `json:"fingerprint"`
	RequestID   string          `json:"requestId,omitempty"`
	Summary     string          `json:"summary"`
	Detail      json.RawMessage `json:"detail,omitempty"`
	Consequence string          `json:"consequence,omitempty"`
}

// Ask delivers a question without admitting any approval decisions.
func (w *Workbench) Ask(ctx context.Context, question, key string, output io.Writer) error {
	return w.AskWithApprovals(ctx, question, key, output, nil)
}

// AskWithApprovals follows both the question and any human decision submissions.
// A nil approval handler leaves writes gated and displays the browser fallback.
func (w *Workbench) AskWithApprovals(ctx context.Context, question, key string, output io.Writer, approve func(context.Context, WorkbenchApproval) (string, error)) error {
	var receipt struct {
		Offset       string `json:"offset"`
		SubmissionID string `json:"submissionId"`
	}
	if err := w.client.workbenchJSON(ctx, http.MethodPost, w.endpoint, map[string]string{
		"kind": "user", "body": question, "idempotencyKey": key,
	}, &receipt); err != nil {
		return err
	}
	if receipt.SubmissionID == "" || receipt.Offset == "" {
		return fmt.Errorf("cloud returned an invalid workbench admission receipt")
	}
	settled := false
	defer func() {
		if !settled {
			cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_ = w.client.workbenchJSON(cleanup, http.MethodPost, w.endpoint+"/abort", struct{}{}, nil)
		}
	}()
	incarnation := ""
	seen := make(map[[3]string]bool)
	var pending []WorkbenchApproval
	for {
		if err := w.follow(ctx, &receipt.Offset, receipt.SubmissionID, &incarnation, &settled, output, func(raw json.RawMessage) error {
			var text string
			if json.Unmarshal(raw, &text) != nil {
				return nil
			}
			_, payload, found := strings.Cut(text, "[approval-request] ")
			if !found {
				return nil
			}
			var request WorkbenchApproval
			if json.Unmarshal([]byte(strings.TrimSpace(payload)), &request) != nil || request.Kind != "approval" || request.Key == "" || request.Fingerprint == "" || request.Summary == "" {
				return nil
			}
			id := [3]string{request.Key, request.Fingerprint, request.RequestID}
			if seen[id] {
				return nil
			}
			seen[id] = true
			if approve == nil {
				_, err := fmt.Fprintln(output, text)
				return err
			}
			pending = append(pending, request)
			return nil
		}); err != nil {
			return err
		}
		if len(pending) == 0 {
			return nil
		}
		request := pending[0]
		pending = pending[1:]
		answer, err := approve(ctx, request)
		if err != nil {
			return err
		}
		if answer != "once" && answer != "session" && answer != "deny" {
			return fmt.Errorf("invalid workbench approval decision")
		}
		decision := map[string]string{"answer": answer, "key": request.Key, "fingerprint": request.Fingerprint}
		if request.RequestID != "" {
			decision["requestId"] = request.RequestID
		}
		body, err := json.Marshal(decision)
		if err != nil {
			return err
		}
		receipt.Offset, receipt.SubmissionID = "", ""
		// Admission may succeed even if receiving its receipt fails.
		settled = false
		if err := w.client.workbenchJSON(ctx, http.MethodPost, w.endpoint, map[string]string{
			"kind": "signal", "type": "workbench.approval", "body": string(body),
		}, &receipt); err != nil {
			return err
		}
		if receipt.SubmissionID == "" || receipt.Offset == "" {
			return fmt.Errorf("cloud returned an invalid workbench admission receipt")
		}
	}
}

func (w *Workbench) follow(ctx context.Context, offset *string, submissionID string, incarnation *string, settled *bool, output io.Writer, toolOutput func(json.RawMessage) error) error {
	for {
		endpoint := w.endpoint + "?" + url.Values{
			"view": {"updates"}, "live": {"long-poll"}, "offset": {*offset},
		}.Encode()
		resp, err := w.client.request(ctx, workbenchTimeout, http.MethodGet, endpoint, nil)
		if err != nil {
			return err
		}
		var chunks []struct {
			Type         string          `json:"type"`
			Kind         string          `json:"kind"`
			Delta        string          `json:"delta"`
			SubmissionID string          `json:"submissionId"`
			Outcome      string          `json:"outcome"`
			Incarnation  string          `json:"incarnation"`
			Error        json.RawMessage `json:"error"`
			Output       json.RawMessage `json:"output"`
		}
		err = json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&chunks)
		resp.Body.Close()
		if err != nil {
			return fmt.Errorf("failed to parse workbench updates: %w", err)
		}
		for _, chunk := range chunks {
			switch chunk.Type {
			case "stream-checkpoint":
				if *incarnation != "" && *incarnation != chunk.Incarnation {
					return fmt.Errorf("workbench worker changed; start a new conversation")
				}
				*incarnation = chunk.Incarnation
			case "tool-output":
				if err := toolOutput(chunk.Output); err != nil {
					return err
				}
			case "message-delta":
				if chunk.Kind == "text" {
					if _, err := io.WriteString(output, chunk.Delta); err != nil {
						return err
					}
				}
			case "submission-settled":
				if chunk.SubmissionID == submissionID {
					*settled = true
					if chunk.Outcome != "completed" {
						return fmt.Errorf("workbench response %s: %s", chunk.Outcome, chunk.Error)
					}
					return nil
				}
			}
		}
		next := resp.Header.Get("Stream-Next-Offset")
		if next == "" {
			return fmt.Errorf("cloud returned no workbench stream offset")
		}
		*offset = next
	}
}
