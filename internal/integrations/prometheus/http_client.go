package prometheus

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/lucientong/argus/internal/types"
)

// HTTPClient queries a real Prometheus instance via the HTTP API v1.
type HTTPClient struct {
	baseURL    string
	httpClient *http.Client
}

// NewHTTPClient creates a Prometheus client that talks to the given base URL
// (e.g. "http://prometheus:9090").
func NewHTTPClient(baseURL string) *HTTPClient {
	return &HTTPClient{
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// prometheusResponse is the envelope returned by the Prometheus HTTP API.
type prometheusResponse struct {
	Status string             `json:"status"`
	Data   prometheusData     `json:"data"`
	Error  string             `json:"error,omitempty"`
}

type prometheusData struct {
	ResultType string              `json:"resultType"`
	Result     []prometheusResult  `json:"result"`
}

type prometheusResult struct {
	Metric map[string]string `json:"metric"`
	Value  []any             `json:"value"` // [timestamp, "value_string"]
	Values [][]any           `json:"values"` // [[timestamp, "value_string"], ...]
}

// Query executes an instant PromQL query.
func (c *HTTPClient) Query(ctx context.Context, promql string) ([]QueryResult, error) {
	params := url.Values{"query": {promql}}
	body, err := c.get(ctx, "/api/v1/query", params)
	if err != nil {
		return nil, err
	}

	var resp prometheusResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("prometheus: decode query response: %w", err)
	}
	if resp.Status != "success" {
		return nil, fmt.Errorf("prometheus: query failed: %s", resp.Error)
	}

	var results []QueryResult
	for _, r := range resp.Data.Result {
		if len(r.Value) < 2 {
			continue
		}
		ts, _ := toFloat64(r.Value[0])
		val, _ := parseFloat(fmt.Sprint(r.Value[1]))
		results = append(results, QueryResult{
			Metric:    r.Metric,
			Value:     val,
			Timestamp: ts,
		})
	}
	return results, nil
}

// QueryRange executes a range PromQL query.
func (c *HTTPClient) QueryRange(ctx context.Context, promql string, start, end int64, stepSeconds int) ([]types.MetricSnapshot, error) {
	params := url.Values{
		"query": {promql},
		"start": {strconv.FormatInt(start, 10)},
		"end":   {strconv.FormatInt(end, 10)},
		"step":  {strconv.Itoa(stepSeconds) + "s"},
	}
	body, err := c.get(ctx, "/api/v1/query_range", params)
	if err != nil {
		return nil, err
	}

	var resp prometheusResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("prometheus: decode range response: %w", err)
	}
	if resp.Status != "success" {
		return nil, fmt.Errorf("prometheus: range query failed: %s", resp.Error)
	}

	var snapshots []types.MetricSnapshot
	for _, r := range resp.Data.Result {
		name := r.Metric["__name__"]
		if name == "" {
			name = promql
		}
		// Return only the last point of each series.
		if len(r.Values) > 0 {
			last := r.Values[len(r.Values)-1]
			if len(last) >= 2 {
				val, _ := parseFloat(fmt.Sprint(last[1]))
				snapshots = append(snapshots, types.MetricSnapshot{
					Name:   name,
					Value:  val,
					Labels: r.Metric,
				})
			}
		}
	}
	return snapshots, nil
}

// FetchKeyMetrics queries CPU usage, memory usage, and error rate for a service.
func (c *HTTPClient) FetchKeyMetrics(ctx context.Context, service string) ([]types.MetricSnapshot, error) {
	queries := []string{
		fmt.Sprintf(`rate(container_cpu_usage_seconds_total{container="%s"}[5m])`, service),
		fmt.Sprintf(`container_memory_usage_bytes{container="%s"}`, service),
		fmt.Sprintf(`rate(http_requests_total{service="%s",code=~"5.."}[5m])`, service),
	}

	var snapshots []types.MetricSnapshot
	for _, q := range queries {
		results, err := c.Query(ctx, q)
		if err != nil {
			// Non-fatal: skip this metric.
			continue
		}
		for _, r := range results {
			name := r.Metric["__name__"]
			if name == "" {
				name = q
			}
			snapshots = append(snapshots, types.MetricSnapshot{
				Name:   name,
				Value:  r.Value,
				Labels: r.Metric,
			})
		}
	}
	return snapshots, nil
}

// get performs an HTTP GET to path?params and returns the body bytes.
func (c *HTTPClient) get(ctx context.Context, path string, params url.Values) ([]byte, error) {
	u := c.baseURL + path
	if len(params) > 0 {
		u += "?" + params.Encode()
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, fmt.Errorf("prometheus: build request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("prometheus: http get %s: %w", path, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("prometheus: unexpected status %d for %s", resp.StatusCode, path)
	}

	var buf []byte
	buf = make([]byte, 0, 4096)
	tmp := make([]byte, 4096)
	for {
		n, readErr := resp.Body.Read(tmp)
		buf = append(buf, tmp[:n]...)
		if readErr != nil {
			break
		}
	}
	return buf, nil
}

// toFloat64 converts a Prometheus timestamp (JSON number) to float64.
func toFloat64(v any) (float64, bool) {
	switch x := v.(type) {
	case float64:
		return x, true
	case json.Number:
		f, err := x.Float64()
		return f, err == nil
	}
	return 0, false
}

// parseFloat parses a string as float64.
func parseFloat(s string) (float64, error) {
	return strconv.ParseFloat(s, 64)
}
