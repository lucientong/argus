package agents_test

import (
	"context"
	"testing"
	"time"

	"github.com/lucientong/argus/internal/agents"
	"github.com/lucientong/argus/internal/types"
)

func TestRemediationAgent_LowRisk(t *testing.T) {
	provider := &mockProvider{
		response: `{
  "actions": [
    {
      "type": "restart",
      "description": "Restart the api deployment to clear CPU spike",
      "command": "kubectl rollout restart deployment/api -n production",
      "risk_level": "low"
    },
    {
      "type": "scale",
      "description": "Scale out to distribute load",
      "command": "kubectl scale deployment api --replicas=5 -n production",
      "risk_level": "low"
    }
  ],
  "rationale": "CPU saturation is best addressed by restarting to clear any leaked goroutines, then scaling out."
}`,
	}

	a := agents.NewRemediationAgent(provider)
	inp := makeRemediationInput(types.SeverityCritical)

	plan, err := a.Run(context.Background(), inp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(plan.Actions) != 2 {
		t.Fatalf("expected 2 actions, got %d", len(plan.Actions))
	}
	if plan.RequiresApproval {
		t.Error("expected RequiresApproval=false for all low-risk actions")
	}
	if plan.Rationale == "" {
		t.Error("expected non-empty rationale")
	}
	if plan.Actions[0].Type != types.ActionRestart {
		t.Errorf("expected restart action, got %s", plan.Actions[0].Type)
	}
	if plan.Actions[1].Type != types.ActionScale {
		t.Errorf("expected scale action, got %s", plan.Actions[1].Type)
	}
	if plan.Diagnosis.Hypothesis == "" {
		t.Error("expected diagnosis hypothesis to be preserved")
	}
}

func TestRemediationAgent_HighRisk(t *testing.T) {
	provider := &mockProvider{
		response: `{
  "actions": [
    {
      "type": "rollback",
      "description": "Rollback to previous version",
      "command": "kubectl rollout undo deployment/api -n production",
      "risk_level": "medium"
    },
    {
      "type": "custom",
      "description": "Deploy PgBouncer connection pooler",
      "command": "helm install pgbouncer ./charts/pgbouncer",
      "risk_level": "high"
    }
  ],
  "rationale": "Rolling back plus adding connection pooling addresses the root cause."
}`,
	}

	a := agents.NewRemediationAgent(provider)
	inp := makeRemediationInput(types.SeverityCritical)

	plan, err := a.Run(context.Background(), inp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !plan.RequiresApproval {
		t.Error("expected RequiresApproval=true when a high-risk action is present")
	}
	if len(plan.Actions) != 2 {
		t.Fatalf("expected 2 actions, got %d", len(plan.Actions))
	}
}

func TestRemediationAgent_UnknownRiskDefaults(t *testing.T) {
	provider := &mockProvider{
		response: `{
  "actions": [
    {
      "type": "unknown_type",
      "description": "Do something unusual",
      "command": "some-command --flag",
      "risk_level": "bogus"
    }
  ],
  "rationale": "Testing unknown risk level handling."
}`,
	}

	a := agents.NewRemediationAgent(provider)
	plan, err := a.Run(context.Background(), makeRemediationInput(types.SeverityWarning))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if plan.Actions[0].RiskLevel != "medium" {
		t.Errorf("expected 'medium' default for unknown risk, got %q", plan.Actions[0].RiskLevel)
	}
	if plan.Actions[0].Type != types.ActionCustom {
		t.Errorf("expected 'custom' fallback action type, got %q", plan.Actions[0].Type)
	}
}

// makeRemediationInput builds a RemediationInput for testing.
func makeRemediationInput(sev types.Severity) agents.RemediationInput {
	return agents.RemediationInput{
		Diagnosis: types.Diagnosis{
			Alert: types.ClassifiedAlert{
				Alert: types.Alert{
					Title:       "High CPU",
					Service:     "api",
					Environment: "production",
					FiredAt:     time.Now(),
					Labels:      map[string]string{},
					Annotations: map[string]string{},
				},
				Category: types.CategoryInfra,
				Severity: sev,
			},
			Hypothesis: "CPU is saturated by excessive goroutines",
			Confidence: 0.9,
		},
		Runbook: types.Runbook{
			Title:   "High CPU Runbook",
			Content: "Restart or scale the deployment to reduce CPU pressure.",
			Source:  "rag-pipeline",
		},
	}
}
