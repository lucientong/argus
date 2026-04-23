package agents

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	wagentpkg "github.com/lucientong/waggle/pkg/agent"
	"github.com/lucientong/waggle/pkg/llm"
	"github.com/lucientong/waggle/pkg/output"

	"github.com/lucientong/argus/internal/integrations/kubernetes"
	"github.com/lucientong/argus/internal/integrations/prometheus"
	"github.com/lucientong/argus/internal/prompts"
	"github.com/lucientong/argus/internal/types"
)

// diagnosisLLMOutput is the JSON shape the LLM must produce.
type diagnosisLLMOutput struct {
	Hypothesis string  `json:"hypothesis"`
	Confidence float64 `json:"confidence"`
	Reasoning  string  `json:"reasoning"`
}

// DiagnosticDeps holds the integration clients required by the DiagnosticAgent.
type DiagnosticDeps struct {
	Prometheus prometheus.Client
	Kubernetes kubernetes.Client
}

// NewDiagnosticAgent returns an Agent[types.ClassifiedAlert, types.Diagnosis] that:
//  1. Queries Prometheus for key metrics
//  2. Queries Kubernetes for deployment/pod state and recent events
//  3. Retrieves recent deploys
//  4. Prompts the LLM to produce a root-cause hypothesis with confidence
func NewDiagnosticAgent(provider llm.Provider, deps DiagnosticDeps) wagentpkg.Agent[types.ClassifiedAlert, types.Diagnosis] {
	inner := output.NewStructuredAgent[prompts.DiagnoseInput, diagnosisLLMOutput](
		"diagnose-llm",
		provider,
		prompts.DiagnosePrompt,
		output.WithMaxRetries(2),
	)

	return wagentpkg.Func[types.ClassifiedAlert, types.Diagnosis](
		"diagnostic",
		func(ctx context.Context, ca types.ClassifiedAlert) (types.Diagnosis, error) {
			// Gather context from integrations.
			metrics, err := gatherMetrics(ctx, deps.Prometheus, ca)
			if err != nil {
				slog.Warn("diagnostic: prometheus query failed, continuing without metrics", "error", err)
			}

			k8sInfo, deploys, err := gatherK8sContext(ctx, deps.Kubernetes, ca)
			if err != nil {
				slog.Warn("diagnostic: k8s query failed, continuing without k8s context", "error", err)
			}

			input := prompts.DiagnoseInput{
				Alert:         ca,
				Metrics:       metrics,
				K8s:           k8sInfo,
				RecentDeploys: deploys,
			}

			llmOut, err := inner.Run(ctx, input)
			if err != nil {
				return types.Diagnosis{}, fmt.Errorf("diagnostic agent: %w", err)
			}

			return types.Diagnosis{
				Alert:         ca,
				Hypothesis:    llmOut.Hypothesis,
				Confidence:    llmOut.Confidence,
				Metrics:       metrics,
				RecentDeploys: deploys,
				K8s:           k8sInfo,
				RawContext:    buildRawContext(input, llmOut.Reasoning),
			}, nil
		},
	)
}

func gatherMetrics(ctx context.Context, prom prometheus.Client, ca types.ClassifiedAlert) ([]types.MetricSnapshot, error) {
	if prom == nil {
		return nil, nil
	}
	return prom.FetchKeyMetrics(ctx, ca.Alert.Service)
}

func gatherK8sContext(ctx context.Context, k8s kubernetes.Client, ca types.ClassifiedAlert) (*types.K8sInfo, []types.DeployEvent, error) {
	if k8s == nil {
		return nil, nil, nil
	}
	info, err := k8s.GetDeployment(ctx, ca.Alert.Environment, ca.Alert.Service)
	if err != nil {
		return nil, nil, err
	}
	deploys, err := k8s.GetRecentDeploys(ctx, ca.Alert.Environment, ca.Alert.Service)
	if err != nil {
		// Non-fatal: return info without deploys.
		return info, nil, nil
	}
	return info, deploys, nil
}

func buildRawContext(input prompts.DiagnoseInput, reasoning string) string {
	var sb strings.Builder
	sb.WriteString(prompts.DiagnosePrompt(input))
	sb.WriteString("\n\n## LLM Reasoning\n")
	sb.WriteString(reasoning)
	return sb.String()
}
