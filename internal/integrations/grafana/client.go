// Package grafana defines the interface for querying the Grafana API.
package grafana

import "context"

// DashboardAnnotation is a Grafana annotation (e.g. deploy events).
type DashboardAnnotation struct {
	Text string
	Tags []string
	Time int64 // Unix ms
}

// Client is the interface for querying Grafana.
type Client interface {
	// GetAnnotations returns annotations matching the given tags within the time window.
	GetAnnotations(ctx context.Context, tags []string, fromMs, toMs int64) ([]DashboardAnnotation, error)
}

// MockClient returns fixed annotations for tests.
type MockClient struct {
	Annotations []DashboardAnnotation
	Err         error
}

func (m *MockClient) GetAnnotations(_ context.Context, _ []string, _, _ int64) ([]DashboardAnnotation, error) {
	if m.Err != nil {
		return nil, m.Err
	}
	return m.Annotations, nil
}
