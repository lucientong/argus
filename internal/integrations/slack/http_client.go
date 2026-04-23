package slack

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"
)

// HTTPClient posts messages to Slack via the Web API and handles interactive
// approval callbacks via a simple in-process polling mechanism.
//
// For RequestApproval, the client posts a message with action buttons and then
// blocks until a response is received via ReceiveCallback (which must be called
// from the webhook handler that receives Slack interactive payloads).
type HTTPClient struct {
	token      string
	httpClient *http.Client

	mu        sync.Mutex
	callbacks map[string]chan ApprovalResponse // keyed by callbackID
}

// NewHTTPClient creates a Slack client that uses the given bot token.
func NewHTTPClient(token string) *HTTPClient {
	return &HTTPClient{
		token: token,
		httpClient: &http.Client{
			Timeout: 15 * time.Second,
		},
		callbacks: make(map[string]chan ApprovalResponse),
	}
}

// PostMessage sends a plain-text (or Block Kit) message to Slack.
func (c *HTTPClient) PostMessage(ctx context.Context, msg Message) error {
	payload := map[string]any{
		"channel": msg.Channel,
		"text":    msg.Text,
	}
	if msg.Blocks != "" {
		var blocks any
		if err := json.Unmarshal([]byte(msg.Blocks), &blocks); err == nil {
			payload["blocks"] = blocks
		}
	}
	return c.apiCall(ctx, "chat.postMessage", payload)
}

// RequestApproval sends an approval-request message and blocks until
// ReceiveCallback is called with the matching callbackID, or the context
// is cancelled.
//
// In production the Slack interactive-components webhook must call
// ReceiveCallback when it receives an action payload.
func (c *HTTPClient) RequestApproval(ctx context.Context, req ApprovalRequest) (ApprovalResponse, error) {
	ch := make(chan ApprovalResponse, 1)
	c.mu.Lock()
	c.callbacks[req.CallbackID] = ch
	c.mu.Unlock()

	defer func() {
		c.mu.Lock()
		delete(c.callbacks, req.CallbackID)
		c.mu.Unlock()
	}()

	// Build a simple Block Kit message with Approve / Deny buttons.
	blocks := buildApprovalBlocks(req)
	blocksJSON, err := json.Marshal(blocks)
	if err != nil {
		return ApprovalResponse{}, fmt.Errorf("slack: marshal blocks: %w", err)
	}

	if postErr := c.PostMessage(ctx, Message{
		Channel: req.Channel,
		Text:    req.Text,
		Blocks:  string(blocksJSON),
	}); postErr != nil {
		return ApprovalResponse{}, postErr
	}

	select {
	case resp := <-ch:
		return resp, nil
	case <-ctx.Done():
		return ApprovalResponse{}, fmt.Errorf("slack: approval timed out: %w", ctx.Err())
	}
}

// ReceiveCallback delivers an interactive-component response to a waiting
// RequestApproval call. Call this from the Slack webhook handler.
func (c *HTTPClient) ReceiveCallback(resp ApprovalResponse) {
	c.mu.Lock()
	ch, ok := c.callbacks[resp.CallbackID]
	c.mu.Unlock()
	if ok {
		select {
		case ch <- resp:
		default:
		}
	}
}

// apiCall POSTs to a Slack Web API method and checks the response.
func (c *HTTPClient) apiCall(ctx context.Context, method string, payload map[string]any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("slack: marshal payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		"https://slack.com/api/"+method, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("slack: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	req.Header.Set("Authorization", "Bearer "+c.token)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("slack: %s: %w", method, err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)

	var slackResp struct {
		OK    bool   `json:"ok"`
		Error string `json:"error,omitempty"`
	}
	if jsonErr := json.Unmarshal(respBody, &slackResp); jsonErr != nil {
		return fmt.Errorf("slack: decode %s response: %w", method, jsonErr)
	}
	if !slackResp.OK {
		return fmt.Errorf("slack: %s failed: %s", method, slackResp.Error)
	}
	return nil
}

// buildApprovalBlocks constructs a simple Block Kit payload for an approval request.
func buildApprovalBlocks(req ApprovalRequest) []map[string]any {
	return []map[string]any{
		{
			"type": "section",
			"text": map[string]any{
				"type": "mrkdwn",
				"text": req.Text,
			},
		},
		{
			"type": "actions",
			"elements": []map[string]any{
				{
					"type":  "button",
					"style": "primary",
					"text":  map[string]any{"type": "plain_text", "text": "Approve"},
					"value": req.CallbackID + ":approved",
					"action_id": "approve",
				},
				{
					"type":  "button",
					"style": "danger",
					"text":  map[string]any{"type": "plain_text", "text": "Deny"},
					"value": req.CallbackID + ":denied",
					"action_id": "deny",
				},
			},
		},
	}
}
