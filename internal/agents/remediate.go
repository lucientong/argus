package agents

import (
	"context"
	"fmt"
	"strings"

	wagentpkg "github.com/lucientong/waggle/pkg/agent"
	"github.com/lucientong/waggle/pkg/llm"
	"github.com/lucientong/waggle/pkg/output"

	"github.com/lucientong/argus/internal/types"
)

// RemediationInput bundles the two upstream outputs that RemediationAgent needs.
type RemediationInput struct {
	Diagnosis types.Diagnosis
	Runbook   types.Runbook
}

// remediationLLMOutput mirrors the JSON structure the LLM is asked to return.
type remediationLLMOutput struct {
	Actions   []remediationActionLLM `json:"actions"`
	Rationale string                 `json:"rationale"`
}

type remediationActionLLM struct {
	Type        string `json:"type"`
	Description string `json:"description"`
	Command     string `json:"command"`
	RiskLevel   string `json:"risk_level"`
}

const remediationSystemPrompt = `You are an expert SRE. Given a diagnosis and a remediation runbook,
produce a concrete, ordered list of remediation actions.

Respond ONLY with JSON matching exactly this schema:
{
  "actions": [
    {
      "type": "<rollback|restart|scale|config_change|custom>",
      "description": "<human readable step>",
      "command": "<exact kubectl / SQL / shell command to run>",
      "risk_level": "<low|medium|high>"
    }
  ],
  "rationale": "<why these actions address the root cause>"
}`

// NewRemediationAgent returns an Agent[RemediationInput, types.RemediationPlan].
// It prompts the LLM with the diagnosis + runbook and parses a structured action list.
// RequiresApproval is set to true when any action has risk_level == "high".
func NewRemediationAgent(provider llm.Provider) wagentpkg.Agent[RemediationInput, types.RemediationPlan] {
	inner := output.NewStructuredAgent[RemediationInput, remediationLLMOutput](
		"remediation-llm",
		provider,
		remediationPrompt,
		output.WithMaxRetries(2),
	)

	return wagentpkg.Func[RemediationInput, types.RemediationPlan](
		"remediation",
		func(ctx context.Context, inp RemediationInput) (types.RemediationPlan, error) {
			raw, err := inner.Run(ctx, inp)
			if err != nil {
				return types.RemediationPlan{}, fmt.Errorf("remediation agent: %w", err)
			}

			actions := make([]types.RemediationAction, 0, len(raw.Actions))
			requiresApproval := false
			for _, a := range raw.Actions {
				at := parseActionType(a.Type)
				rl := parseRiskLevel(a.RiskLevel)
				if rl == "high" {
					requiresApproval = true
				}
				actions = append(actions, types.RemediationAction{
					Type:        at,
					Description: a.Description,
					Command:     a.Command,
					RiskLevel:   rl,
				})
			}

			return types.RemediationPlan{
				Diagnosis:        inp.Diagnosis,
				Runbook:          inp.Runbook,
				Actions:          actions,
				Rationale:        raw.Rationale,
				RequiresApproval: requiresApproval,
			}, nil
		},
	)
}

// remediationPrompt builds the user-facing prompt for the LLM.
func remediationPrompt(inp RemediationInput) string {
	diag := inp.Diagnosis
	rb := inp.Runbook
	return fmt.Sprintf(`## Incident Diagnosis

Alert: %s
Service: %s
Severity: %s
Category: %s
Hypothesis: %s
Confidence: %.0f%%

### Metrics
%s

### Kubernetes State
%s

### Recent Deploys
%s

---

## Matched Runbook: %s

%s

---

Based on the diagnosis and runbook above, produce the ordered list of remediation actions.
Prefer low-risk options first. Only include high-risk options if lower-risk ones are insufficient.
For each action include the exact command to run.`,
		diag.Alert.Alert.Title,
		diag.Alert.Alert.Service,
		diag.Alert.Severity,
		diag.Alert.Category,
		diag.Hypothesis,
		diag.Confidence*100,
		formatMetricsText(diag.Metrics),
		formatK8sText(diag.K8s),
		formatDeploysText(diag.RecentDeploys),
		rb.Title,
		rb.Content,
	)
}

// parseActionType maps a string to ActionType, defaulting to ActionCustom.
func parseActionType(s string) types.ActionType {
	switch s {
	case "rollback":
		return types.ActionRollback
	case "restart":
		return types.ActionRestart
	case "scale":
		return types.ActionScale
	case "config_change":
		return types.ActionConfigChange
	default:
		return types.ActionCustom
	}
}

// parseRiskLevel normalises to "low", "medium", or "high".
func parseRiskLevel(s string) string {
	switch s {
	case "low", "medium", "high":
		return s
	default:
		return "medium" // safe default
	}
}

// formatMetricsText renders MetricSnapshot slice as readable text for the prompt.
func formatMetricsText(metrics []types.MetricSnapshot) string {
	if len(metrics) == 0 {
		return "  (none)"
	}
	var sb strings.Builder
	for _, m := range metrics {
		fmt.Fprintf(&sb, "  %s = %.4f %v\n", m.Name, m.Value, m.Labels)
	}
	return sb.String()
}

// formatK8sText renders K8sInfo as readable text for the prompt.
func formatK8sText(k8s *types.K8sInfo) string {
	if k8s == nil {
		return "  (unavailable)"
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "  Deployment: %s/%s\n", k8s.Namespace, k8s.Deployment)
	fmt.Fprintf(&sb, "  Replicas: %d/%d ready\n", k8s.ReadyReplicas, k8s.TotalReplicas)
	fmt.Fprintf(&sb, "  Restart count: %d\n", k8s.RestartCount)
	for _, e := range k8s.Events {
		fmt.Fprintf(&sb, "  Event: %s\n", e)
	}
	return sb.String()
}

// formatDeploysText renders DeployEvent slice as readable text for the prompt.
func formatDeploysText(deploys []types.DeployEvent) string {
	if len(deploys) == 0 {
		return "  (none)"
	}
	var sb strings.Builder
	for _, d := range deploys {
		fmt.Fprintf(&sb, "  %s v%s by %s at %s\n",
			d.Service, d.Version, d.Author, d.DeployedAt.Format("2006-01-02 15:04:05 UTC"))
	}
	return sb.String()
}
