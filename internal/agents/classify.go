// Package agents contains all waggle agents used by the Argus pipeline.
package agents

import (
	"context"
	"fmt"

	wagentpkg "github.com/lucientong/waggle/pkg/agent"
	"github.com/lucientong/waggle/pkg/llm"
	"github.com/lucientong/waggle/pkg/output"

	"github.com/lucientong/argus/internal/prompts"
	"github.com/lucientong/argus/internal/types"
)

// classifyOutput is the JSON shape the LLM must produce.
type classifyOutput struct {
	Category   string  `json:"category"`
	Severity   string  `json:"severity"`
	Confidence float64 `json:"confidence"`
	Reasoning  string  `json:"reasoning"`
}

// NewClassifyAgent returns an Agent[types.Alert, types.ClassifiedAlert] that uses
// an LLM to assign category, severity, confidence, and reasoning to a raw alert.
func NewClassifyAgent(provider llm.Provider) wagentpkg.Agent[types.Alert, types.ClassifiedAlert] {
	inner := output.NewStructuredAgent[types.Alert, classifyOutput](
		"classify",
		provider,
		prompts.ClassifyPrompt,
		output.WithMaxRetries(2),
	)

	return wagentpkg.Func[types.Alert, types.ClassifiedAlert](
		"classify-wrap",
		func(ctx context.Context, alert types.Alert) (types.ClassifiedAlert, error) {
			out, err := inner.Run(ctx, alert)
			if err != nil {
				return types.ClassifiedAlert{}, fmt.Errorf("classify agent: %w", err)
			}
			return types.ClassifiedAlert{
				Alert:      alert,
				Category:   parseCategory(out.Category),
				Severity:   parseSeverity(out.Severity, alert.Severity),
				Confidence: out.Confidence,
				Reasoning:  out.Reasoning,
			}, nil
		},
	)
}

func parseCategory(s string) types.Category {
	switch s {
	case "infra":
		return types.CategoryInfra
	case "app":
		return types.CategoryApp
	case "network":
		return types.CategoryNetwork
	case "database":
		return types.CategoryDatabase
	case "security":
		return types.CategorySecurity
	default:
		return types.CategoryUnknown
	}
}

func parseSeverity(s string, fallback types.Severity) types.Severity {
	switch s {
	case "critical":
		return types.SeverityCritical
	case "warning":
		return types.SeverityWarning
	case "info":
		return types.SeverityInfo
	default:
		if fallback != "" {
			return fallback
		}
		return types.SeverityUnknown
	}
}
