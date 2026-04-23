package agents_test

import (
	"context"
	"testing"
	"time"

	"github.com/lucientong/argus/internal/agents"
	"github.com/lucientong/argus/internal/integrations/kubernetes"
	"github.com/lucientong/argus/internal/integrations/prometheus"
	"github.com/lucientong/argus/internal/types"
)

func classifiedAlert() types.ClassifiedAlert {
	return types.ClassifiedAlert{
		Alert: types.Alert{
			ID:          "diag-1",
			Source:      types.SourceGrafana,
			Title:       "API high error rate",
			Description: "5xx errors > 10%",
			Service:     "api-server",
			Environment: "default",
			Labels:      map[string]string{"severity": "critical"},
			Annotations: map[string]string{},
			FiredAt:     time.Now(),
		},
		Category:   types.CategoryApp,
		Severity:   types.SeverityCritical,
		Confidence: 0.9,
		Reasoning:  "CPU is saturated",
	}
}

func TestDiagnosticAgent_HappyPath(t *testing.T) {
	provider := &mockProvider{
		response: `{"hypothesis":"Recent deploy broke the /checkout endpoint","confidence":0.85,"reasoning":"Deploy 2 hours ago + spike in 5xx errors correlated"}`,
	}

	mockProm := &prometheus.MockClient{
		Snapshots: []types.MetricSnapshot{
			{Name: "http_errors_total", Value: 42.0, Labels: map[string]string{"service": "api-server"}},
		},
	}
	mockK8s := &kubernetes.MockClient{
		Info: &types.K8sInfo{
			Namespace: "default", Deployment: "api-server",
			ReadyReplicas: 2, TotalReplicas: 3, RestartCount: 5,
			Events: []string{"pod api-server-abc restarted 3 times"},
		},
		Deploys: []types.DeployEvent{
			{Service: "api-server", Version: "v2.3.1", Author: "alice", DeployedAt: time.Now().Add(-2 * time.Hour)},
		},
	}

	a := agents.NewDiagnosticAgent(provider, agents.DiagnosticDeps{
		Prometheus: mockProm,
		Kubernetes: mockK8s,
	})

	got, err := a.Run(context.Background(), classifiedAlert())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Hypothesis != "Recent deploy broke the /checkout endpoint" {
		t.Errorf("hypothesis mismatch: %s", got.Hypothesis)
	}
	if got.Confidence != 0.85 {
		t.Errorf("confidence: want 0.85, got %f", got.Confidence)
	}
	if len(got.Metrics) != 1 {
		t.Errorf("expected 1 metric snapshot, got %d", len(got.Metrics))
	}
	if got.K8s == nil {
		t.Error("expected K8s info, got nil")
	}
	if len(got.RecentDeploys) != 1 {
		t.Errorf("expected 1 deploy event, got %d", len(got.RecentDeploys))
	}
	if got.RawContext == "" {
		t.Error("expected non-empty RawContext")
	}
}

func TestDiagnosticAgent_PrometheusError(t *testing.T) {
	// Even if prometheus fails, the agent should proceed with LLM diagnosis.
	provider := &mockProvider{
		response: `{"hypothesis":"Unknown cause","confidence":0.3,"reasoning":"No metrics available"}`,
	}
	mockProm := &prometheus.MockClient{Err: &testError{"prometheus unreachable"}}
	mockK8s := &kubernetes.MockClient{Info: &types.K8sInfo{Namespace: "default", Deployment: "api-server"}}

	a := agents.NewDiagnosticAgent(provider, agents.DiagnosticDeps{
		Prometheus: mockProm,
		Kubernetes: mockK8s,
	})

	got, err := a.Run(context.Background(), classifiedAlert())
	if err != nil {
		t.Fatalf("prometheus error should not fail the agent: %v", err)
	}
	if got.Hypothesis == "" {
		t.Error("hypothesis should still be populated")
	}
	if len(got.Metrics) != 0 {
		t.Errorf("expected 0 metrics on prom error, got %d", len(got.Metrics))
	}
}

func TestDiagnosticAgent_NilClients(t *testing.T) {
	// Nil integration clients should be silently tolerated.
	provider := &mockProvider{
		response: `{"hypothesis":"Unknown","confidence":0.2,"reasoning":"No context"}`,
	}
	a := agents.NewDiagnosticAgent(provider, agents.DiagnosticDeps{})
	got, err := a.Run(context.Background(), classifiedAlert())
	if err != nil {
		t.Fatalf("nil clients should not error: %v", err)
	}
	if got.Hypothesis == "" {
		t.Error("hypothesis should be populated even with nil clients")
	}
}

type testError struct{ msg string }

func (e *testError) Error() string { return e.msg }
