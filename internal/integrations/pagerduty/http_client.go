package pagerduty

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// HTTPClient interacts with the PagerDuty REST API v2.
type HTTPClient struct {
	integrationKey string
	httpClient     *http.Client
}

// NewHTTPClient creates a PagerDuty client.
// integrationKey is the Events API v2 integration key used for triggering
// and resolving incidents.
func NewHTTPClient(integrationKey string) *HTTPClient {
	return &HTTPClient{
		integrationKey: integrationKey,
		httpClient: &http.Client{
			Timeout: 15 * time.Second,
		},
	}
}

// AddNote posts a note to a PagerDuty incident via the REST API.
// Note: adding notes requires a REST API key (not an integration key).
// This implementation posts via the Events API v2 as a custom event detail.
func (c *HTTPClient) AddNote(ctx context.Context, incidentID string, note IncidentNote) error {
	payload := map[string]any{
		"routing_key":  c.integrationKey,
		"event_action": "trigger",
		"dedup_key":    incidentID,
		"payload": map[string]any{
			"summary":  note.Content,
			"severity": "info",
			"source":   "argus",
		},
	}
	return c.eventsAPICall(ctx, payload)
}

// Resolve sends a resolve event to PagerDuty.
func (c *HTTPClient) Resolve(ctx context.Context, incidentID string) error {
	payload := map[string]any{
		"routing_key":  c.integrationKey,
		"event_action": "resolve",
		"dedup_key":    incidentID,
	}
	return c.eventsAPICall(ctx, payload)
}

// eventsAPICall posts an event to the PagerDuty Events API v2.
func (c *HTTPClient) eventsAPICall(ctx context.Context, payload map[string]any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("pagerduty: marshal payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		"https://events.pagerduty.com/v2/enqueue", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("pagerduty: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("pagerduty: events api: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusAccepted && resp.StatusCode != http.StatusOK {
		return fmt.Errorf("pagerduty: unexpected status %d: %s", resp.StatusCode, respBody)
	}
	return nil
}
