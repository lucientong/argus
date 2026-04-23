package agents_test

import (
	"context"
	"testing"
	"time"

	"github.com/lucientong/waggle/pkg/agent"
	"github.com/lucientong/waggle/pkg/llm"

	"github.com/lucientong/argus/internal/agents"
	"github.com/lucientong/argus/internal/types"
)

// mockProvider implements llm.Provider and returns a fixed response string.
type mockProvider struct {
	response string
	err      error
}

func (m *mockProvider) Info() llm.ProviderInfo { return llm.ProviderInfo{Name: "mock"} }
func (m *mockProvider) Chat(_ context.Context, _ []llm.Message) (string, error) {
	return m.response, m.err
}
func (m *mockProvider) ChatStream(_ context.Context, _ []llm.Message) (<-chan string, error) {
	ch := make(chan string, 1)
	ch <- m.response
	close(ch)
	return ch, m.err
}

// ---- Helpers -----------------------------------------------------------------

func makeAlert(severity types.Severity, title string) types.Alert {
	return types.Alert{
		ID:          "test-1",
		Source:      types.SourceGrafana,
		Title:       title,
		Description: "test description",
		Severity:    severity,
		Service:     "api",
		Environment: "prod",
		Labels:      map[string]string{"severity": string(severity)},
		Annotations: map[string]string{},
		FiredAt:     time.Now(),
	}
}

// ---- ClassifyAgent tests -----------------------------------------------------

func TestClassifyAgent_Critical(t *testing.T) {
	provider := &mockProvider{
		response: `{"category":"infra","severity":"critical","confidence":0.9,"reasoning":"CPU is saturated"}`,
	}
	a := agents.NewClassifyAgent(provider)
	alert := makeAlert(types.SeverityCritical, "High CPU usage")

	got, err := a.Run(context.Background(), alert)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Category != types.CategoryInfra {
		t.Errorf("category: want infra, got %s", got.Category)
	}
	if got.Severity != types.SeverityCritical {
		t.Errorf("severity: want critical, got %s", got.Severity)
	}
	if got.Confidence != 0.9 {
		t.Errorf("confidence: want 0.9, got %f", got.Confidence)
	}
	if got.Reasoning != "CPU is saturated" {
		t.Errorf("reasoning: want 'CPU is saturated', got %s", got.Reasoning)
	}
	if got.Alert.ID != "test-1" {
		t.Errorf("alert.ID: want test-1, got %s", got.Alert.ID)
	}
}

func TestClassifyAgent_UnknownCategory(t *testing.T) {
	provider := &mockProvider{
		response: `{"category":"foobar","severity":"warning","confidence":0.4,"reasoning":"unclear"}`,
	}
	a := agents.NewClassifyAgent(provider)
	got, err := a.Run(context.Background(), makeAlert(types.SeverityWarning, "Mystery alert"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Category != types.CategoryUnknown {
		t.Errorf("unknown category should map to CategoryUnknown, got %s", got.Category)
	}
}

// ---- SeverityRouter tests ----------------------------------------------------

func stubBranch(status types.IncidentStatus) agent.Agent[types.ClassifiedAlert, types.IncidentReport] {
	return agent.Func[types.ClassifiedAlert, types.IncidentReport](
		"stub-"+string(status),
		func(_ context.Context, ca types.ClassifiedAlert) (types.IncidentReport, error) {
			return types.IncidentReport{
				ID:     ca.Alert.ID,
				Alert:  ca,
				Status: status,
			}, nil
		},
	)
}

func TestSeverityRouter_Critical(t *testing.T) {
	router := agents.NewSeverityRouter(agents.SeverityBranches{
		Critical: stubBranch(types.IncidentStatusOpen),
		Warning:  stubBranch(types.IncidentStatusInProgress),
		Info:     stubBranch(types.IncidentStatusResolved),
	})

	ca := types.ClassifiedAlert{
		Alert:    makeAlert(types.SeverityCritical, "DB down"),
		Severity: types.SeverityCritical,
		Category: types.CategoryDatabase,
	}
	got, err := router.Run(context.Background(), ca)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Critical branch returns IncidentStatusOpen
	if got.Status != types.IncidentStatusOpen {
		t.Errorf("expected critical branch (open), got %s", got.Status)
	}
}

func TestSeverityRouter_Warning(t *testing.T) {
	router := agents.NewSeverityRouter(agents.SeverityBranches{
		Critical: stubBranch(types.IncidentStatusOpen),
		Warning:  stubBranch(types.IncidentStatusInProgress),
		Info:     stubBranch(types.IncidentStatusResolved),
	})

	ca := types.ClassifiedAlert{
		Alert:    makeAlert(types.SeverityWarning, "High latency"),
		Severity: types.SeverityWarning,
		Category: types.CategoryApp,
	}
	got, err := router.Run(context.Background(), ca)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Status != types.IncidentStatusInProgress {
		t.Errorf("expected warning branch (in_progress), got %s", got.Status)
	}
}

func TestSeverityRouter_InfoDefault(t *testing.T) {
	router := agents.NewSeverityRouter(agents.SeverityBranches{
		Critical: stubBranch(types.IncidentStatusOpen),
		Warning:  stubBranch(types.IncidentStatusInProgress),
		Info:     stubBranch(types.IncidentStatusResolved),
	})

	ca := types.ClassifiedAlert{
		Alert:    makeAlert(types.SeverityUnknown, "Unknown alert"),
		Severity: types.SeverityUnknown,
		Category: types.CategoryUnknown,
	}
	got, err := router.Run(context.Background(), ca)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Unknown severity falls through to info branch
	if got.Status != types.IncidentStatusResolved {
		t.Errorf("expected info branch (resolved), got %s", got.Status)
	}
}
