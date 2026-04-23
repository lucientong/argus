package grafana

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"
)

// HTTPClient queries the Grafana HTTP API.
type HTTPClient struct {
	baseURL    string
	apiKey     string
	httpClient *http.Client
}

// NewHTTPClient creates a Grafana client.
func NewHTTPClient(baseURL, apiKey string) *HTTPClient {
	return &HTTPClient{
		baseURL: baseURL,
		apiKey:  apiKey,
		httpClient: &http.Client{
			Timeout: 15 * time.Second,
		},
	}
}

// GetAnnotations returns annotations matching the given tags within the time window.
func (c *HTTPClient) GetAnnotations(ctx context.Context, tags []string, fromMs, toMs int64) ([]DashboardAnnotation, error) {
	u := c.baseURL + "/api/annotations?from=" + strconv.FormatInt(fromMs, 10) + "&to=" + strconv.FormatInt(toMs, 10)
	for _, t := range tags {
		u += "&tags=" + t
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, fmt.Errorf("grafana: build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("grafana: get annotations: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("grafana: unexpected status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("grafana: read body: %w", err)
	}

	var raw []struct {
		Text string   `json:"text"`
		Tags []string `json:"tags"`
		Time int64    `json:"time"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("grafana: decode annotations: %w", err)
	}

	annotations := make([]DashboardAnnotation, 0, len(raw))
	for _, a := range raw {
		annotations = append(annotations, DashboardAnnotation{
			Text: a.Text,
			Tags: a.Tags,
			Time: a.Time,
		})
	}
	return annotations, nil
}
