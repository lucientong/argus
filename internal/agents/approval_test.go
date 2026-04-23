package agents_test

import (
	"context"
	"testing"
	"time"

	"github.com/lucientong/argus/internal/agents"
	"github.com/lucientong/argus/internal/integrations/slack"
	"github.com/lucientong/argus/internal/types"
)

func TestApprovalAgent_AutoApprove(t *testing.T) {
	mock := &slack.MockClient{AutoApprove: true}
	a := agents.NewApprovalAgent(mock, "#incidents")

	plan := makeRemediationPlan(false) // RequiresApproval = false
	decision, err := a.Run(context.Background(), plan)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !decision.Approved {
		t.Error("expected auto-approved=true for low-risk plan")
	}
	if len(mock.Approvals) != 0 {
		t.Error("expected no Slack approval request for auto-approved plan")
	}
}

func TestApprovalAgent_SlackApproved(t *testing.T) {
	mock := &slack.MockClient{AutoApprove: true, ApproveComment: "LGTM"}
	a := agents.NewApprovalAgent(mock, "#incidents")

	plan := makeRemediationPlan(true) // RequiresApproval = true
	decision, err := a.Run(context.Background(), plan)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !decision.Approved {
		t.Error("expected approved=true from mock")
	}
	if decision.Comment != "LGTM" {
		t.Errorf("expected comment 'LGTM', got %q", decision.Comment)
	}
	if len(mock.Approvals) != 1 {
		t.Errorf("expected 1 Slack approval request, got %d", len(mock.Approvals))
	}
}

func TestApprovalAgent_SlackDenied(t *testing.T) {
	mock := &slack.MockClient{AutoApprove: false, ApproveComment: "too risky"}
	a := agents.NewApprovalAgent(mock, "#incidents")

	plan := makeRemediationPlan(true)
	decision, err := a.Run(context.Background(), plan)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if decision.Approved {
		t.Error("expected approved=false when mock denies")
	}
	if decision.Comment != "too risky" {
		t.Errorf("expected comment 'too risky', got %q", decision.Comment)
	}
}

func TestApprovalAgent_SlackError(t *testing.T) {
	mock := &slack.MockClient{Err: context.DeadlineExceeded}
	a := agents.NewApprovalAgent(mock, "#incidents")

	plan := makeRemediationPlan(true)
	_, err := a.Run(context.Background(), plan)
	if err == nil {
		t.Fatal("expected error when Slack client fails")
	}
}

// makeRemediationPlan builds a RemediationPlan for testing.
func makeRemediationPlan(requiresApproval bool) types.RemediationPlan {
	riskLevel := "low"
	if requiresApproval {
		riskLevel = "high"
	}
	return types.RemediationPlan{
		Diagnosis: types.Diagnosis{
			Alert: types.ClassifiedAlert{
				Alert: types.Alert{
					ID:      "test-alert-001",
					Title:   "High CPU",
					Service: "api",
					FiredAt: time.Now(),
					Labels:      map[string]string{},
					Annotations: map[string]string{},
				},
				Category: types.CategoryInfra,
				Severity: types.SeverityCritical,
			},
			Hypothesis: "CPU saturated by leaked goroutines",
			Confidence: 0.9,
		},
		Runbook: types.Runbook{
			Title:   "High CPU Runbook",
			Content: "Scale or restart the deployment.",
			Source:  "rag-pipeline",
		},
		Actions: []types.RemediationAction{
			{
				Type:        types.ActionRestart,
				Description: "Restart the deployment",
				Command:     "kubectl rollout restart deployment/api -n production",
				RiskLevel:   riskLevel,
			},
		},
		Rationale:        "Restart clears leaked goroutines causing CPU saturation.",
		RequiresApproval: requiresApproval,
	}
}
