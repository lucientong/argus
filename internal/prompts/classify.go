// Package prompts contains all LLM prompt template functions used by Argus agents.
package prompts

import (
	"fmt"
	"strings"

	"github.com/lucientong/argus/internal/types"
)

// ClassifyPrompt returns the user prompt for the ClassifyAgent.
func ClassifyPrompt(alert types.Alert) string {
	var sb strings.Builder
	sb.WriteString("You are an expert SRE triage system. Classify the following alert.\n\n")
	sb.WriteString("## Alert\n")
	fmt.Fprintf(&sb, "Source:      %s\n", alert.Source)
	fmt.Fprintf(&sb, "Title:       %s\n", alert.Title)
	fmt.Fprintf(&sb, "Description: %s\n", alert.Description)
	fmt.Fprintf(&sb, "Service:     %s\n", alert.Service)
	fmt.Fprintf(&sb, "Environment: %s\n", alert.Environment)
	if len(alert.Labels) > 0 {
		sb.WriteString("Labels:\n")
		for k, v := range alert.Labels {
			fmt.Fprintf(&sb, "  %s: %s\n", k, v)
		}
	}
	if len(alert.Annotations) > 0 {
		sb.WriteString("Annotations:\n")
		for k, v := range alert.Annotations {
			fmt.Fprintf(&sb, "  %s: %s\n", k, v)
		}
	}
	sb.WriteString("\n## Task\n")
	sb.WriteString("Assign:\n")
	sb.WriteString("1. category: one of [infra, app, network, database, security, unknown]\n")
	sb.WriteString("2. severity: one of [critical, warning, info, unknown]\n")
	sb.WriteString("   - critical: service is down or at imminent risk of failure; data loss possible\n")
	sb.WriteString("   - warning: degraded performance or elevated error rate; not yet down\n")
	sb.WriteString("   - info: informational, no immediate action needed\n")
	sb.WriteString("3. confidence: float 0.0–1.0 representing your certainty\n")
	sb.WriteString("4. reasoning: one concise sentence explaining your classification\n")
	return sb.String()
}

// DiagnoseStubPrompt is a placeholder prompt used by the stub DiagnosticAgent in Phase 2.
// It is replaced by the full diagnostic prompt in Phase 4.
func DiagnoseStubPrompt(ca types.ClassifiedAlert) string {
	return fmt.Sprintf(
		"Alert: %s\nCategory: %s\nSeverity: %s\n\nProvide a one-sentence hypothesis for the root cause.",
		ca.Alert.Title, ca.Category, ca.Severity,
	)
}

// DiagnosePrompt builds the full diagnostic prompt including all gathered context.
func DiagnosePrompt(input DiagnoseInput) string {
	var sb strings.Builder
	sb.WriteString("You are an expert SRE diagnosing a production incident.\n\n")
	sb.WriteString("## Alert\n")
	fmt.Fprintf(&sb, "Title:       %s\n", input.Alert.Alert.Title)
	fmt.Fprintf(&sb, "Description: %s\n", input.Alert.Alert.Description)
	fmt.Fprintf(&sb, "Category:    %s\n", input.Alert.Category)
	fmt.Fprintf(&sb, "Severity:    %s\n", input.Alert.Severity)
	fmt.Fprintf(&sb, "Service:     %s\n", input.Alert.Alert.Service)
	fmt.Fprintf(&sb, "Environment: %s\n", input.Alert.Alert.Environment)

	if len(input.Metrics) > 0 {
		sb.WriteString("\n## Key Metrics\n")
		for _, m := range input.Metrics {
			fmt.Fprintf(&sb, "  %s = %.4f %v\n", m.Name, m.Value, m.Labels)
		}
	}

	if input.K8s != nil {
		sb.WriteString("\n## Kubernetes State\n")
		fmt.Fprintf(&sb, "  Deployment: %s/%s\n", input.K8s.Namespace, input.K8s.Deployment)
		fmt.Fprintf(&sb, "  Replicas: %d/%d ready\n", input.K8s.ReadyReplicas, input.K8s.TotalReplicas)
		fmt.Fprintf(&sb, "  Restart count: %d\n", input.K8s.RestartCount)
		if len(input.K8s.Events) > 0 {
			sb.WriteString("  Recent events:\n")
			for _, e := range input.K8s.Events {
				fmt.Fprintf(&sb, "    - %s\n", e)
			}
		}
	}

	if len(input.RecentDeploys) > 0 {
		sb.WriteString("\n## Recent Deploys\n")
		for _, d := range input.RecentDeploys {
			fmt.Fprintf(&sb, "  %s v%s by %s at %s\n",
				d.Service, d.Version, d.Author, d.DeployedAt.Format("2006-01-02 15:04:05 UTC"))
		}
	}

	sb.WriteString("\n## Task\n")
	sb.WriteString("Based on the above context, provide:\n")
	sb.WriteString("1. hypothesis: a single clear sentence stating the most likely root cause\n")
	sb.WriteString("2. confidence: float 0.0–1.0 representing your certainty\n")
	sb.WriteString("3. reasoning: 2-4 sentences expanding on the hypothesis and evidence\n")
	return sb.String()
}

// DiagnoseInput groups all gathered context passed to DiagnosePrompt.
type DiagnoseInput struct {
	Alert         types.ClassifiedAlert
	Metrics       []types.MetricSnapshot
	K8s           *types.K8sInfo
	RecentDeploys []types.DeployEvent
}
