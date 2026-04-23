package agents

import (
	"context"

	"github.com/lucientong/waggle/pkg/agent"
	"github.com/lucientong/waggle/pkg/llm"
	"github.com/lucientong/waggle/pkg/output"
	"github.com/lucientong/waggle/pkg/waggle"

	"github.com/lucientong/argus/internal/prompts"
	"github.com/lucientong/argus/internal/types"
)

// SeverityBranches holds the three downstream agents that handle critical,
// warning, and info classified alerts. Each returns an IncidentReport stub
// that will be fully populated later in the pipeline.
//
// In Phase 2 these are simple pass-through stubs; in Phase 9 they are wired
// to the full diagnose-fix-verify Loop.
type SeverityBranches struct {
	Critical agent.Agent[types.ClassifiedAlert, types.IncidentReport]
	Warning  agent.Agent[types.ClassifiedAlert, types.IncidentReport]
	Info     agent.Agent[types.ClassifiedAlert, types.IncidentReport]
}

// NewSeverityRouter returns a waggle.Router that dispatches a ClassifiedAlert
// to the correct severity branch.
func NewSeverityRouter(branches SeverityBranches) agent.Agent[types.ClassifiedAlert, types.IncidentReport] {
	routeFn := waggle.RouterFunc[types.ClassifiedAlert](
		func(_ context.Context, ca types.ClassifiedAlert) (string, error) {
			switch ca.Severity {
			case types.SeverityCritical:
				return "critical", nil
			case types.SeverityWarning:
				return "warning", nil
			default:
				return "info", nil
			}
		},
	)

	return waggle.Router[types.ClassifiedAlert, types.IncidentReport](
		"severity-router",
		routeFn,
		map[string]agent.Agent[types.ClassifiedAlert, types.IncidentReport]{
			"critical": branches.Critical,
			"warning":  branches.Warning,
			"info":     branches.Info,
		},
	)
}

// diagnoseStubOutput is used by stub DiagnoseAgent in Phase 2.
type diagnoseStubOutput struct {
	Summary string `json:"summary"`
}

// NewDiagnoseStubAgent returns a placeholder DiagnosticAgent for Phase 2.
// It is replaced by the real DiagnosticAgent in Phase 4.
func NewDiagnoseStubAgent(provider llm.Provider) agent.Agent[types.ClassifiedAlert, types.Diagnosis] {
	inner := output.NewStructuredAgent[types.ClassifiedAlert, diagnoseStubOutput](
		"diagnose-stub",
		provider,
		func(ca types.ClassifiedAlert) string {
			return prompts.DiagnoseStubPrompt(ca)
		},
	)
	return agent.Func[types.ClassifiedAlert, types.Diagnosis](
		"diagnose-stub-wrap",
		func(ctx context.Context, ca types.ClassifiedAlert) (types.Diagnosis, error) {
			out, err := inner.Run(ctx, ca)
			if err != nil {
				return types.Diagnosis{}, err
			}
			return types.Diagnosis{
				Alert:      ca,
				Hypothesis: out.Summary,
				Confidence: 0.5,
				RawContext: out.Summary,
			}, nil
		},
	)
}
