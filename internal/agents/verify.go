package agents

import (
	"context"
	"fmt"
	"log/slog"

	wagentpkg "github.com/lucientong/waggle/pkg/agent"
	"github.com/lucientong/waggle/pkg/llm"
	"github.com/lucientong/waggle/pkg/output"

	"github.com/lucientong/argus/internal/integrations/prometheus"
	"github.com/lucientong/argus/internal/types"
)

// verifyLLMOutput is the JSON shape the LLM must produce.
type verifyLLMOutput struct {
	Recovered   bool   `json:"recovered"`
	Explanation string `json:"explanation"`
}

// VerifyDeps holds the integration clients needed by the VerifyAgent.
type VerifyDeps struct {
	Prometheus prometheus.Client
}

// NewVerifyAgent returns an Agent[types.ExecutionResult, types.VerificationResult].
//
// Behaviour:
//   - Re-queries Prometheus to check whether the incident metrics have normalised.
//   - If execution itself failed (Success=false), marks as not recovered immediately.
//   - Asks the LLM to assess recovery based on pre-execution metrics vs. post-execution metrics.
func NewVerifyAgent(provider llm.Provider, deps VerifyDeps) wagentpkg.Agent[types.ExecutionResult, types.VerificationResult] {
	inner := output.NewStructuredAgent[verifyInput, verifyLLMOutput](
		"verify-llm",
		provider,
		verifyPrompt,
		output.WithMaxRetries(2),
	)

	return wagentpkg.Func[types.ExecutionResult, types.VerificationResult](
		"verify",
		func(ctx context.Context, execResult types.ExecutionResult) (types.VerificationResult, error) {
			// If execution failed, skip LLM check.
			if !execResult.Success {
				return types.VerificationResult{
					Recovered:   false,
					Explanation: fmt.Sprintf("execution failed: %s", execResult.Error),
				}, nil
			}

			// Re-query metrics after remediation.
			var postMetrics []types.MetricSnapshot
			if deps.Prometheus != nil {
				service := execResult.Plan.Diagnosis.Alert.Alert.Service
				var err error
				postMetrics, err = deps.Prometheus.FetchKeyMetrics(ctx, service)
				if err != nil {
					slog.Warn("verify: prometheus re-query failed", "error", err)
				}
			}

			inp := verifyInput{
				Alert:        execResult.Plan.Diagnosis.Alert,
				Hypothesis:   execResult.Plan.Diagnosis.Hypothesis,
				PreMetrics:   execResult.Plan.Diagnosis.Metrics,
				PostMetrics:  postMetrics,
				ActionsRan:   execResult.Actions,
			}

			llmOut, err := inner.Run(ctx, inp)
			if err != nil {
				return types.VerificationResult{}, fmt.Errorf("verify agent: %w", err)
			}

			return types.VerificationResult{
				Recovered:   llmOut.Recovered,
				Metrics:     postMetrics,
				Explanation: llmOut.Explanation,
			}, nil
		},
	)
}

// verifyInput is the prompt context for the verify LLM call.
type verifyInput struct {
	Alert       types.ClassifiedAlert
	Hypothesis  string
	PreMetrics  []types.MetricSnapshot
	PostMetrics []types.MetricSnapshot
	ActionsRan  []types.ActionOutcome
}

// verifyPrompt builds the LLM prompt for recovery verification.
func verifyPrompt(inp verifyInput) string {
	pre := formatMetricsText(inp.PreMetrics)
	post := formatMetricsText(inp.PostMetrics)

	actions := ""
	for i, a := range inp.ActionsRan {
		status := "✓"
		if !a.Success {
			status = "✗"
		}
		actions += fmt.Sprintf("%d. [%s] %s\n   Output: %s\n", i+1, status, a.Action.Command, a.Output)
	}
	if actions == "" {
		actions = "  (none ran)"
	}

	return fmt.Sprintf(`You are an SRE verifying incident recovery.

## Incident
Alert: %s
Service: %s
Original hypothesis: %s

## Pre-Remediation Metrics
%s

## Remediation Actions Executed
%s

## Post-Remediation Metrics
%s

## Task
Based on the metrics and actions above, determine:
1. recovered: true if the incident appears resolved, false if still ongoing or unclear
2. explanation: 2-3 sentences explaining your assessment`,
		inp.Alert.Alert.Title,
		inp.Alert.Alert.Service,
		inp.Hypothesis,
		pre,
		actions,
		post,
	)
}
