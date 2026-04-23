package agents

import (
	"context"
	"fmt"

	wagentpkg "github.com/lucientong/waggle/pkg/agent"
	"github.com/lucientong/waggle/pkg/llm"
	"github.com/lucientong/waggle/pkg/rag"

	"github.com/lucientong/argus/internal/types"
)

const runbookSystemPrompt = `You are an expert SRE incident response assistant.
Given the incident context, identify the most relevant remediation playbook.
Return the full playbook content that best matches the incident, including all steps.
If multiple playbooks are relevant, combine the most applicable sections.`

// NewRunbookSearchAgent returns an Agent[types.Diagnosis, types.Runbook] that:
//  1. Converts the Diagnosis into a search query
//  2. Runs the RAG pipeline to retrieve relevant runbook content
//  3. Wraps the result in a types.Runbook
func NewRunbookSearchAgent(provider llm.Provider, embedder rag.Embedder, store rag.VectorStore) wagentpkg.Agent[types.Diagnosis, types.Runbook] {
	pipeline := rag.NewPipeline(
		"runbook-rag",
		embedder,
		store,
		provider,
		rag.WithTopK(3),
		rag.WithSystemPrompt(runbookSystemPrompt),
	)

	return wagentpkg.Func[types.Diagnosis, types.Runbook](
		"runbook-search",
		func(ctx context.Context, diag types.Diagnosis) (types.Runbook, error) {
			query := buildRunbookQuery(diag)
			content, err := pipeline.Run(ctx, query)
			if err != nil {
				return types.Runbook{}, fmt.Errorf("runbook search: %w", err)
			}
			return types.Runbook{
				Title:   fmt.Sprintf("Runbook for: %s", diag.Alert.Alert.Title),
				Content: content,
				Source:  "rag-pipeline",
			}, nil
		},
	)
}

// buildRunbookQuery converts a Diagnosis into a concise search query.
func buildRunbookQuery(diag types.Diagnosis) string {
	return fmt.Sprintf(
		"Incident: %s. Category: %s. Severity: %s. Hypothesis: %s",
		diag.Alert.Alert.Title,
		diag.Alert.Category,
		diag.Alert.Severity,
		diag.Hypothesis,
	)
}
