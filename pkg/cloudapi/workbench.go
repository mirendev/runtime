package cloudapi

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
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

// Ask delivers a question and renders text deltas until this submission settles.
// Tool activity is observational, never an instruction to execute a local tool.
func (w *Workbench) Ask(ctx context.Context, question, key string, output io.Writer) error {
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
	for {
		endpoint := w.endpoint + "?" + url.Values{
			"view": {"updates"}, "live": {"long-poll"}, "offset": {receipt.Offset},
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
		}
		err = json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&chunks)
		resp.Body.Close()
		if err != nil {
			return fmt.Errorf("failed to parse workbench updates: %w", err)
		}
		for _, chunk := range chunks {
			switch chunk.Type {
			case "stream-checkpoint":
				if incarnation != "" && incarnation != chunk.Incarnation {
					return fmt.Errorf("workbench worker changed; start a new conversation")
				}
				incarnation = chunk.Incarnation
			case "message-delta":
				if chunk.Kind == "text" {
					if _, err := io.WriteString(output, chunk.Delta); err != nil {
						return err
					}
				}
			case "submission-settled":
				if chunk.SubmissionID == receipt.SubmissionID {
					settled = true
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
		receipt.Offset = next
	}
}
