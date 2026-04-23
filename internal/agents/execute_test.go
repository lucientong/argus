package agents_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/lucientong/argus/internal/agents"
	"github.com/lucientong/argus/internal/types"
)

// mockExecutor records commands and returns configured outputs.
type mockExecutor struct {
	outputs map[string]string
	errors  map[string]error
	ran     []string
}

func newMockExecutor() *mockExecutor {
	return &mockExecutor{
		outputs: make(map[string]string),
		errors:  make(map[string]error),
	}
}

func (m *mockExecutor) Execute(_ context.Context, cmd string) (string, error) {
	m.ran = append(m.ran, cmd)
	if err, ok := m.errors[cmd]; ok {
		return "", err
	}
	if out, ok := m.outputs[cmd]; ok {
		return out, nil
	}
	return "ok", nil
}

func TestExecuteAgent_ApprovedSuccess(t *testing.T) {
	exec := newMockExecutor()
	exec.outputs["kubectl rollout restart deployment/api -n production"] = "deployment.apps/api restarted"

	a := agents.NewExecuteAgent(exec)
	decision := makeApprovalDecision(true, "LGTM")

	result, err := a.Run(context.Background(), decision)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Success {
		t.Errorf("expected Success=true, got error: %s", result.Error)
	}
	if len(result.Actions) != 1 {
		t.Fatalf("expected 1 action outcome, got %d", len(result.Actions))
	}
	if result.Actions[0].Output != "deployment.apps/api restarted" {
		t.Errorf("unexpected output: %q", result.Actions[0].Output)
	}
	if len(exec.ran) != 1 {
		t.Errorf("expected 1 command executed, got %d", len(exec.ran))
	}
}

func TestExecuteAgent_NotApproved(t *testing.T) {
	exec := newMockExecutor()
	a := agents.NewExecuteAgent(exec)
	decision := makeApprovalDecision(false, "too risky")

	result, err := a.Run(context.Background(), decision)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Success {
		t.Error("expected Success=false when plan not approved")
	}
	if len(exec.ran) != 0 {
		t.Error("expected no commands run when plan not approved")
	}
}

func TestExecuteAgent_CommandFailure(t *testing.T) {
	exec := newMockExecutor()
	cmd := "kubectl rollout restart deployment/api -n production"
	exec.errors[cmd] = fmt.Errorf("connection refused")

	a := agents.NewExecuteAgent(exec)
	decision := makeApprovalDecision(true, "")

	result, err := a.Run(context.Background(), decision)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Success {
		t.Error("expected Success=false when command fails")
	}
	if result.Actions[0].Success {
		t.Error("expected action outcome Success=false")
	}
	if result.Actions[0].Error == "" {
		t.Error("expected non-empty error in action outcome")
	}
}

func TestExecuteAgent_DangerousCommandBlocked(t *testing.T) {
	exec := newMockExecutor()
	a := agents.NewExecuteAgent(exec)

	decision := types.ApprovalDecision{
		Plan: types.RemediationPlan{
			Diagnosis: makeDiagnosis(),
			Actions: []types.RemediationAction{
				{
					Type:        types.ActionCustom,
					Description: "Delete everything",
					Command:     "kubectl delete namespace production",
					RiskLevel:   "high",
				},
			},
			RequiresApproval: true,
		},
		Approved: true,
		Comment:  "approved",
	}

	result, err := a.Run(context.Background(), decision)
	if err != nil {
		t.Fatalf("unexpected top-level error: %v", err)
	}
	if result.Success {
		t.Error("expected Success=false when dangerous command is blocked")
	}
	if len(exec.ran) != 0 {
		t.Error("expected 0 commands executed when guardrail blocks")
	}
	if len(result.Actions) != 1 {
		t.Fatalf("expected 1 outcome, got %d", len(result.Actions))
	}
	if result.Actions[0].Error == "" {
		t.Error("expected error message in blocked action outcome")
	}
}

func TestExecuteAgent_StopsAfterFirstFailure(t *testing.T) {
	exec := newMockExecutor()
	cmd1 := "kubectl rollout undo deployment/api"
	exec.errors[cmd1] = fmt.Errorf("rollback failed")

	a := agents.NewExecuteAgent(exec)

	decision := types.ApprovalDecision{
		Plan: types.RemediationPlan{
			Diagnosis: makeDiagnosis(),
			Actions: []types.RemediationAction{
				{Type: types.ActionRollback, Description: "rollback", Command: cmd1, RiskLevel: "medium"},
				{Type: types.ActionRestart, Description: "restart", Command: "kubectl rollout restart deployment/api", RiskLevel: "low"},
			},
		},
		Approved: true,
	}

	result, err := a.Run(context.Background(), decision)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Success {
		t.Error("expected failure when first action fails")
	}
	if len(exec.ran) != 1 {
		t.Errorf("expected only 1 command run (stop after first failure), got %d", len(exec.ran))
	}
}

// makeApprovalDecision builds an ApprovalDecision for testing.
func makeApprovalDecision(approved bool, comment string) types.ApprovalDecision {
	return types.ApprovalDecision{
		Plan: types.RemediationPlan{
			Diagnosis: makeDiagnosis(),
			Actions: []types.RemediationAction{
				{
					Type:        types.ActionRestart,
					Description: "Restart the deployment",
					Command:     "kubectl rollout restart deployment/api -n production",
					RiskLevel:   "low",
				},
			},
			Rationale:        "Restart clears the issue.",
			RequiresApproval: false,
		},
		Approved: approved,
		Comment:  comment,
	}
}

// makeDiagnosis builds a minimal Diagnosis for testing.
func makeDiagnosis() types.Diagnosis {
	return types.Diagnosis{
		Alert: types.ClassifiedAlert{
			Alert: types.Alert{
				ID:          "test-001",
				Title:       "High CPU",
				Service:     "api",
				Environment: "production",
				FiredAt:     time.Now(),
				Labels:      map[string]string{},
				Annotations: map[string]string{},
			},
			Category: types.CategoryInfra,
			Severity: types.SeverityCritical,
		},
		Hypothesis: "CPU saturated",
		Confidence: 0.9,
	}
}
