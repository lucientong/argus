// Package prometheus defines the interface for querying Prometheus metrics.
package prometheus

import (
	"context"

	"github.com/lucientong/argus/internal/types"
)

// QueryResult is a single instant vector result from a Prometheus query.
type QueryResult struct {
	Metric map[string]string
	Value  float64
	// Timestamp is the Unix timestamp in seconds.
	Timestamp float64
}

// Client is the interface for querying Prometheus.
type Client interface {
	// Query executes an instant PromQL query and returns matching time series.
	Query(ctx context.Context, promql string) ([]QueryResult, error)

	// QueryRange executes a range PromQL query.
	QueryRange(ctx context.Context, promql string, start, end int64, stepSeconds int) ([]types.MetricSnapshot, error)

	// FetchKeyMetrics returns a curated set of metrics relevant to the given service.
	FetchKeyMetrics(ctx context.Context, service string) ([]types.MetricSnapshot, error)
}

// MockClient is an in-memory Client for unit tests.
// Populate Snapshots before use.
type MockClient struct {
	Snapshots []types.MetricSnapshot
	Err       error
}

func (m *MockClient) Query(_ context.Context, _ string) ([]QueryResult, error) {
	if m.Err != nil {
		return nil, m.Err
	}
	results := make([]QueryResult, 0, len(m.Snapshots))
	for _, s := range m.Snapshots {
		results = append(results, QueryResult{
			Metric: s.Labels,
			Value:  s.Value,
		})
	}
	return results, nil
}

func (m *MockClient) QueryRange(_ context.Context, _ string, _, _ int64, _ int) ([]types.MetricSnapshot, error) {
	if m.Err != nil {
		return nil, m.Err
	}
	return m.Snapshots, nil
}

func (m *MockClient) FetchKeyMetrics(_ context.Context, _ string) ([]types.MetricSnapshot, error) {
	if m.Err != nil {
		return nil, m.Err
	}
	return m.Snapshots, nil
}
