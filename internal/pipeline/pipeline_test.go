package pipeline_test

import (
	"context"
	"testing"
	"time"

	wagentpkg "github.com/lucientong/waggle/pkg/agent"

	"github.com/lucientong/argus/internal/agents"
	"github.com/lucientong/argus/internal/integrations/slack"
	"github.com/lucientong/argus/internal/pipeline"
	"github.com/lucientong/argus/internal/types"
)

// ----- stub agents -----

func stubClassify() wagentpkg.Agent[types.Alert, types.ClassifiedAlert] {
	return wagentpkg.Func[types.Alert, types.ClassifiedAlert]("classify", func(_ context.Context, a types.Alert) (types.ClassifiedAlert, error) {
		return types.ClassifiedAlert{
			Alert:    a,
			Category: types.CategoryInfra,
			Severity: types.SeverityCritical,
		}, nil
	})
}

func stubDiagnose() wagentpkg.Agent[types.ClassifiedAlert, types.Diagnosis] {
	return wagentpkg.Func[types.ClassifiedAlert, types.Diagnosis]("diagnose", func(_ context.Context, ca types.ClassifiedAlert) (types.Diagnosis, error) {
		return types.Diagnosis{Alert: ca, Hypothesis: "CPU saturated", Confidence: 0.9}, nil
	})
}

func stubRunbook() wagentpkg.Agent[types.Diagnosis, types.Runbook] {
	return wagentpkg.Func[types.Diagnosis, types.Runbook]("runbook", func(_ context.Context, _ types.Diagnosis) (types.Runbook, error) {
		return types.Runbook{Title: "High CPU Runbook", Content: "Restart the deployment.", Source: "rag-pipeline"}, nil
	})
}

func stubRemediate() wagentpkg.Agent[agents.RemediationInput, types.RemediationPlan] {
	return wagentpkg.Func[agents.RemediationInput, types.RemediationPlan]("remediate", func(_ context.Context, inp agents.RemediationInput) (types.RemediationPlan, error) {
		return types.RemediationPlan{
			Diagnosis: inp.Diagnosis,
			Runbook:   inp.Runbook,
			Actions: []types.RemediationAction{
				{Type: types.ActionRestart, Description: "Restart", Command: "kubectl rollout restart deployment/api", RiskLevel: "low"},
			},
			Rationale: "Restart clears the issue.",
		}, nil
	})
}

func stubExecute() wagentpkg.Agent[types.ApprovalDecision, types.ExecutionResult] {
	return wagentpkg.Func[types.ApprovalDecision, types.ExecutionResult]("execute", func(_ context.Context, d types.ApprovalDecision) (types.ExecutionResult, error) {
		return types.ExecutionResult{
			Plan:    d.Plan,
			Actions: []types.ActionOutcome{{Action: d.Plan.Actions[0], Output: "restarted", Success: true}},
			Success: true,
		}, nil
	})
}

func stubVerify(recovered bool) wagentpkg.Agent[types.ExecutionResult, types.VerificationResult] {
	return wagentpkg.Func[types.ExecutionResult, types.VerificationResult]("verify", func(_ context.Context, _ types.ExecutionResult) (types.VerificationResult, error) {
		return types.VerificationResult{
			Recovered:   recovered,
			Explanation: "metrics checked",
		}, nil
	})
}

// notifyCalled is a simple counter used by stubNotify.
type notifyCounter struct{ count int }

func stubNotify(counter *notifyCounter) wagentpkg.Agent[types.IncidentReport, types.IncidentReport] {
	return wagentpkg.Func[types.IncidentReport, types.IncidentReport]("notify", func(_ context.Context, r types.IncidentReport) (types.IncidentReport, error) {
		counter.count++
		r.Summary = "stub summary"
		return r, nil
	})
}

func makeTestAlert() types.Alert {
	return types.Alert{
		ID:          "alert-001",
		Source:      types.SourceGrafana,
		Title:       "High CPU",
		Service:     "api",
		Environment: "production",
		Severity:    types.SeverityCritical,
		FiredAt:     time.Now(),
		Labels:      map[string]string{},
		Annotations: map[string]string{},
	}
}

// ----- tests -----

func TestPipeline_Resolves(t *testing.T) {
	deps := pipeline.Deps{
		Classify:      stubClassify(),
		Diagnose:      stubDiagnose(),
		Runbook:       stubRunbook(),
		Remediate:     stubRemediate(),
		Approve:       agents.NewApprovalAgent(&slack.MockClient{AutoApprove: true}, "#incidents"),
		Execute:       stubExecute(),
		Verify:        stubVerify(true),
		MaxIterations: 3,
	}

	p := pipeline.Build(deps)
	report, err := p.Run(context.Background(), makeTestAlert())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if report.Status != types.IncidentStatusResolved {
		t.Errorf("expected resolved, got %s", report.Status)
	}
	if report.Verification == nil || !report.Verification.Recovered {
		t.Error("expected Verification.Recovered=true")
	}
	if report.LoopIterations < 1 {
		t.Errorf("expected LoopIterations >= 1, got %d", report.LoopIterations)
	}
	if report.ResolvedAt == nil {
		t.Error("expected ResolvedAt to be stamped")
	}
}

func TestPipeline_FailsAfterMaxIterations(t *testing.T) {
	deps := pipeline.Deps{
		Classify:      stubClassify(),
		Diagnose:      stubDiagnose(),
		Runbook:       stubRunbook(),
		Remediate:     stubRemediate(),
		Approve:       agents.NewApprovalAgent(&slack.MockClient{AutoApprove: true}, "#incidents"),
		Execute:       stubExecute(),
		Verify:        stubVerify(false), // never recovers
		MaxIterations: 2,
	}

	p := pipeline.Build(deps)
	report, err := p.Run(context.Background(), makeTestAlert())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if report.Status != types.IncidentStatusFailed {
		t.Errorf("expected failed after max iterations, got %s", report.Status)
	}
}

func TestPipeline_ApprovalDeniedNotFatal(t *testing.T) {
	// Denied approval → execution skipped → verification marks not recovered.
	deps := pipeline.Deps{
		Classify:  stubClassify(),
		Diagnose:  stubDiagnose(),
		Runbook:   stubRunbook(),
		Remediate: stubRemediate(),
		Approve:   agents.NewApprovalAgent(&slack.MockClient{AutoApprove: false, ApproveComment: "too risky"}, "#incidents"),
		Execute: wagentpkg.Func[types.ApprovalDecision, types.ExecutionResult]("execute", func(_ context.Context, d types.ApprovalDecision) (types.ExecutionResult, error) {
			return types.ExecutionResult{Plan: d.Plan, Success: false, Error: "not approved"}, nil
		}),
		Verify:        stubVerify(false),
		MaxIterations: 1,
	}

	p := pipeline.Build(deps)
	report, err := p.Run(context.Background(), makeTestAlert())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Denied → not recovered → loop exhausts → failed
	if report.Status != types.IncidentStatusFailed {
		t.Errorf("expected failed when approval denied, got %s", report.Status)
	}
}

func TestPipeline_NotifyCalledOnce(t *testing.T) {
	counter := &notifyCounter{}
	deps := pipeline.Deps{
		Classify:      stubClassify(),
		Diagnose:      stubDiagnose(),
		Runbook:       stubRunbook(),
		Remediate:     stubRemediate(),
		Approve:       agents.NewApprovalAgent(&slack.MockClient{AutoApprove: true}, "#incidents"),
		Execute:       stubExecute(),
		Verify:        stubVerify(true),
		Notify:        stubNotify(counter),
		MaxIterations: 3,
	}

	p := pipeline.Build(deps)
	report, err := p.Run(context.Background(), makeTestAlert())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if counter.count != 1 {
		t.Errorf("expected Notify called once, got %d", counter.count)
	}
	if report.Summary != "stub summary" {
		t.Errorf("expected Summary from notify agent, got %q", report.Summary)
	}
}

