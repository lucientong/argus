// Package pagerduty defines the interface for interacting with PagerDuty.
package pagerduty

import "context"

// IncidentNote is a note attached to a PagerDuty incident.
type IncidentNote struct {
	Content string
}

// Client is the interface for PagerDuty operations.
type Client interface {
	// AddNote posts a note to the specified PagerDuty incident.
	AddNote(ctx context.Context, incidentID string, note IncidentNote) error

	// Resolve marks the incident as resolved.
	Resolve(ctx context.Context, incidentID string) error
}

// MockClient records operations for assertions in tests.
type MockClient struct {
	Notes    []IncidentNote
	Resolved []string
	Err      error
}

func (m *MockClient) AddNote(_ context.Context, _ string, note IncidentNote) error {
	if m.Err != nil {
		return m.Err
	}
	m.Notes = append(m.Notes, note)
	return nil
}

func (m *MockClient) Resolve(_ context.Context, incidentID string) error {
	if m.Err != nil {
		return m.Err
	}
	m.Resolved = append(m.Resolved, incidentID)
	return nil
}
