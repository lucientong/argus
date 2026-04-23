// Package pipeline wires all Argus agents into a complete incident-response
// pipeline using waggle primitives (Chain, Loop, Func).
package pipeline

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	wagentpkg "github.com/lucientong/waggle/pkg/agent"
	waggle "github.com/lucientong/waggle/pkg/waggle"

	"github.com/lucientong/argus/internal/agents"
	"github.com/lucientong/argus/internal/types"
)

// Deps holds all agent dependencies needed to build the pipeline.
type Deps struct {
	Classify  wagentpkg.Agent[types.Alert, types.ClassifiedAlert]
	Diagnose  wagentpkg.Agent[types.ClassifiedAlert, types.Diagnosis]
	Runbook   wagentpkg.Agent[types.Diagnosis, types.Runbook]
	Remediate wagentpkg.Agent[agents.RemediationInput, types.RemediationPlan]
	Approve   wagentpkg.Agent[types.RemediationPlan, types.ApprovalDecision]
	Execute   wagentpkg.Agent[types.ApprovalDecision, types.ExecutionResult]
	Verify    wagentpkg.Agent[types.ExecutionResult, types.VerificationResult]
	Notify    wagentpkg.Agent[types.IncidentReport, types.IncidentReport]

	// MaxIterations caps the remediation retry loop (default 5).
	MaxIterations int
}

// Build returns an Agent[types.Alert, types.IncidentReport] that runs the full
// incident-response pipeline with retry loop.
//
// Pipeline:
//
//	Alert
//	  → ClassifyAgent          (Alert → ClassifiedAlert)
//	  → DiagnosticAgent        (ClassifiedAlert → Diagnosis)
//	  → RunbookSearchAgent      (Diagnosis → Runbook)
//	  → RemediationAgent        (RemediationInput → RemediationPlan)
//	  → ApprovalAgent           (RemediationPlan → ApprovalDecision)
//	  → ExecuteAgent            (ApprovalDecision → ExecutionResult)
//	  → VerifyAgent             (ExecutionResult → VerificationResult)
//	  → IncidentReport
//
// The verify→remediate segment is wrapped in a Loop that retries up to
// MaxIterations times if Verification.Recovered == false.
func Build(deps Deps) wagentpkg.Agent[types.Alert, types.IncidentReport] {
	maxIter := deps.MaxIterations
	if maxIter <= 0 {
		maxIter = 5
	}

	// initAgent: Alert → IncidentReport (first full pass)
	initAgent := buildInitAgent(deps)

	// bodyAgent: IncidentReport → IncidentReport (re-remediate if not recovered)
	bodyAgent := buildBodyAgent(deps)

	// condition: continue looping while not recovered
	condition := waggle.LoopCondition[types.IncidentReport](func(report types.IncidentReport) bool {
		if report.Verification == nil {
			return false // no verification data — don't loop
		}
		return !report.Verification.Recovered
	})

	looped := waggle.Loop[types.Alert, types.IncidentReport](
		"incident-response-loop",
		initAgent,
		bodyAgent,
		condition,
		waggle.WithMaxIterations[types.Alert, types.IncidentReport](maxIter),
	)

	// Wrap the loop to stamp the final status and resolved time, then notify.
	return wagentpkg.Func[types.Alert, types.IncidentReport](
		"incident-pipeline",
		func(ctx context.Context, alert types.Alert) (types.IncidentReport, error) {
			report, err := looped.Run(ctx, alert)
			if err != nil {
				slog.Error("pipeline loop error", "error", err)
				// Return a failed report rather than a bare error so callers
				// always get a complete IncidentReport.
				report.Status = types.IncidentStatusFailed
				now := time.Now()
				report.ResolvedAt = &now
			} else {
				now := time.Now()
				if report.Verification != nil && report.Verification.Recovered {
					report.Status = types.IncidentStatusResolved
					report.ResolvedAt = &now
				} else {
					report.Status = types.IncidentStatusFailed
					report.ResolvedAt = &now
				}
			}

			// Notify (non-fatal: if Notify is nil or fails, still return the report).
			if deps.Notify != nil {
				notified, notifyErr := deps.Notify.Run(ctx, report)
				if notifyErr != nil {
					slog.Warn("pipeline: notify agent error", "error", notifyErr)
				} else {
					report = notified
				}
			}

			return report, nil
		},
	)
}

// buildInitAgent creates Agent[types.Alert, types.IncidentReport] for the first loop pass.
func buildInitAgent(deps Deps) wagentpkg.Agent[types.Alert, types.IncidentReport] {
	return wagentpkg.Func[types.Alert, types.IncidentReport](
		"init-pass",
		func(ctx context.Context, alert types.Alert) (types.IncidentReport, error) {
			report := types.IncidentReport{
				ID:        alert.ID,
				StartedAt: time.Now(),
				Status:    types.IncidentStatusInProgress,
			}

			// Classify
			ca, err := deps.Classify.Run(ctx, alert)
			if err != nil {
				return report, fmt.Errorf("classify: %w", err)
			}
			report.Alert = ca

			// Diagnose
			diag, err := deps.Diagnose.Run(ctx, ca)
			if err != nil {
				return report, fmt.Errorf("diagnose: %w", err)
			}
			report.Diagnosis = diag

			// Runbook search
			rb, err := deps.Runbook.Run(ctx, diag)
			if err != nil {
				return report, fmt.Errorf("runbook: %w", err)
			}

			// Remediation planning
			plan, err := deps.Remediate.Run(ctx, agents.RemediationInput{
				Diagnosis: diag,
				Runbook:   rb,
			})
			if err != nil {
				return report, fmt.Errorf("remediate: %w", err)
			}
			report.Plan = plan

			// Approval gate
			decision, err := deps.Approve.Run(ctx, plan)
			if err != nil {
				return report, fmt.Errorf("approve: %w", err)
			}

			// Execute
			execResult, err := deps.Execute.Run(ctx, decision)
			if err != nil {
				return report, fmt.Errorf("execute: %w", err)
			}
			report.Execution = &execResult

			// Verify
			verifyResult, err := deps.Verify.Run(ctx, execResult)
			if err != nil {
				return report, fmt.Errorf("verify: %w", err)
			}
			report.Verification = &verifyResult
			report.LoopIterations++

			return report, nil
		},
	)
}

// buildBodyAgent creates Agent[types.IncidentReport, types.IncidentReport] for
// subsequent loop iterations (re-remediate if not recovered).
func buildBodyAgent(deps Deps) wagentpkg.Agent[types.IncidentReport, types.IncidentReport] {
	return wagentpkg.Func[types.IncidentReport, types.IncidentReport](
		"retry-pass",
		func(ctx context.Context, prev types.IncidentReport) (types.IncidentReport, error) {
			// Re-use the existing diagnosis — it's still valid.
			diag := prev.Diagnosis

			// Fresh runbook search (diagnosis unchanged, same result expected but we re-query).
			rb, err := deps.Runbook.Run(ctx, diag)
			if err != nil {
				return prev, fmt.Errorf("retry runbook: %w", err)
			}

			plan, err := deps.Remediate.Run(ctx, agents.RemediationInput{
				Diagnosis: diag,
				Runbook:   rb,
			})
			if err != nil {
				return prev, fmt.Errorf("retry remediate: %w", err)
			}
			prev.Plan = plan

			decision, err := deps.Approve.Run(ctx, plan)
			if err != nil {
				return prev, fmt.Errorf("retry approve: %w", err)
			}

			execResult, err := deps.Execute.Run(ctx, decision)
			if err != nil {
				return prev, fmt.Errorf("retry execute: %w", err)
			}
			prev.Execution = &execResult

			verifyResult, err := deps.Verify.Run(ctx, execResult)
			if err != nil {
				return prev, fmt.Errorf("retry verify: %w", err)
			}
			prev.Verification = &verifyResult
			prev.LoopIterations++

			return prev, nil
		},
	)
}
