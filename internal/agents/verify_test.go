package agents_test

import (
	"context"
	"testing"

	"github.com/lucientong/argus/internal/agents"
	"github.com/lucientong/argus/internal/integrations/prometheus"
	"github.com/lucientong/argus/internal/types"
)

func TestVerifyAgent_Recovered(t *testing.T) {
	provider := &mockProvider{
		response: `{"recovered": true, "explanation": "CPU dropped below 50% after scaling. All replicas healthy."}`,
	}
	prom := &prometheus.MockClient{
		Snapshots: []types.MetricSnapshot{
			{Name: "cpu_usage", Value: 0.45, Labels: map[string]string{"service": "api"}},
		},
	}

	a := agents.NewVerifyAgent(provider, agents.VerifyDeps{Prometheus: prom})

	execResult := makeExecResult(true)
	result, err := a.Run(context.Background(), execResult)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Recovered {
		t.Error("expected Recovered=true")
	}
	if result.Explanation == "" {
		t.Error("expected non-empty explanation")
	}
	if len(result.Metrics) == 0 {
		t.Error("expected post-remediation metrics to be populated")
	}
}

func TestVerifyAgent_NotRecovered(t *testing.T) {
	provider := &mockProvider{
		response: `{"recovered": false, "explanation": "CPU still at 95%. Scaling did not help. Further investigation needed."}`,
	}

	a := agents.NewVerifyAgent(provider, agents.VerifyDeps{})

	result, err := a.Run(context.Background(), makeExecResult(true))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Recovered {
		t.Error("expected Recovered=false")
	}
}

func TestVerifyAgent_ExecutionFailed(t *testing.T) {
	provider := &mockProvider{response: "unused"}
	a := agents.NewVerifyAgent(provider, agents.VerifyDeps{})

	execResult := types.ExecutionResult{
		Plan:    makeRemediationPlan(false),
		Success: false,
		Error:   "connection refused",
	}

	result, err := a.Run(context.Background(), execResult)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Recovered {
		t.Error("expected Recovered=false when execution failed")
	}
}

func TestVerifyAgent_PrometheusError(t *testing.T) {
	provider := &mockProvider{
		response: `{"recovered": true, "explanation": "Assumed recovered based on action logs."}`,
	}
	prom := &prometheus.MockClient{Err: context.DeadlineExceeded}

	a := agents.NewVerifyAgent(provider, agents.VerifyDeps{Prometheus: prom})
	result, err := a.Run(context.Background(), makeExecResult(true))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Should proceed even when Prometheus fails.
	if result.Explanation == "" {
		t.Error("expected non-empty explanation even when Prometheus errors")
	}
}

// makeExecResult builds an ExecutionResult for testing.
func makeExecResult(success bool) types.ExecutionResult {
	errMsg := ""
	if !success {
		errMsg = "simulated failure"
	}
	return types.ExecutionResult{
		Plan: makeRemediationPlan(false),
		Actions: []types.ActionOutcome{
			{
				Action:  types.RemediationAction{Type: types.ActionRestart, Command: "kubectl rollout restart deployment/api", RiskLevel: "low"},
				Output:  "restarted",
				Success: success,
				Error:   errMsg,
			},
		},
		Success: success,
	}
}
