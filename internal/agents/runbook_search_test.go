package agents_test

import (
	"context"
	"math"
	"testing"
	"time"

	"github.com/lucientong/waggle/pkg/rag"

	"github.com/lucientong/argus/internal/agents"
	"github.com/lucientong/argus/internal/types"
)

// mockEmbedder returns a fixed-dimension random-ish vector derived from text length.
// It is deterministic enough for unit tests and satisfies the rag.Embedder interface.
type mockEmbedder struct {
	dims int
}

func (m *mockEmbedder) Dimensions() int { return m.dims }
func (m *mockEmbedder) Embed(_ context.Context, texts []string) ([][]float64, error) {
	out := make([][]float64, len(texts))
	for i, t := range texts {
		vec := make([]float64, m.dims)
		for j := 0; j < m.dims; j++ {
			// Simple deterministic value based on text and dimension index.
			vec[j] = math.Sin(float64(len(t)+j) * 0.1)
		}
		// Normalise so cosine similarity is well-defined.
		norm := 0.0
		for _, v := range vec {
			norm += v * v
		}
		norm = math.Sqrt(norm)
		if norm > 0 {
			for j := range vec {
				vec[j] /= norm
			}
		}
		out[i] = vec
	}
	return out, nil
}

func TestRunbookSearchAgent(t *testing.T) {
	embedder := &mockEmbedder{dims: 8}
	store := rag.NewInMemoryStore()

	// Ingest a small test runbook.
	splitter := rag.NewTokenSplitter(200, 20)
	runbookText := `# High CPU Runbook

Symptoms: CPU > 90%.

Remediation:
- Scale out: kubectl scale deployment api --replicas=5
- Rollback: kubectl rollout undo deployment/api`

	err := rag.Ingest(context.Background(), runbookText, "high-cpu.md", embedder, store, splitter)
	if err != nil {
		t.Fatalf("ingest failed: %v", err)
	}

	provider := &mockProvider{
		response: "Based on the context, run: kubectl scale deployment api --replicas=5",
	}

	a := agents.NewRunbookSearchAgent(provider, embedder, store)

	diag := types.Diagnosis{
		Alert: types.ClassifiedAlert{
			Alert: types.Alert{
				Title:   "High CPU",
				Service: "api",
				FiredAt: time.Now(),
				Labels:  map[string]string{},
				Annotations: map[string]string{},
			},
			Category: types.CategoryInfra,
			Severity: types.SeverityCritical,
		},
		Hypothesis: "CPU is saturated by the api-server",
		Confidence: 0.85,
	}

	got, err := a.Run(context.Background(), diag)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Content == "" {
		t.Error("expected non-empty runbook content")
	}
	if got.Title == "" {
		t.Error("expected non-empty runbook title")
	}
	if got.Source != "rag-pipeline" {
		t.Errorf("expected source 'rag-pipeline', got %s", got.Source)
	}
}
