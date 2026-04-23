package web_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	streampkg "github.com/lucientong/waggle/pkg/stream"

	"github.com/lucientong/argus/internal/types"
	"github.com/lucientong/argus/internal/web"
)

// makeReport builds a minimal IncidentReport for testing.
func makeReport(id string, status types.IncidentStatus) types.IncidentReport {
	return types.IncidentReport{
		ID: id,
		Alert: types.ClassifiedAlert{
			Alert:    types.Alert{Title: "High CPU", Service: "api", Environment: "prod"},
			Severity: types.SeverityCritical,
			Category: types.CategoryInfra,
		},
		Status:         status,
		StartedAt:      time.Now().Add(-30 * time.Second),
		LoopIterations: 1,
	}
}

func TestStore_UpsertAndGet(t *testing.T) {
	store := web.NewStore()
	r := makeReport("inc-001", types.IncidentStatusResolved)
	store.Upsert(r)

	got, ok := store.Get("inc-001")
	if !ok {
		t.Fatal("expected to find inc-001")
	}
	if got.Status != types.IncidentStatusResolved {
		t.Errorf("wrong status: %s", got.Status)
	}
}

func TestStore_All_SortedByStartedAt(t *testing.T) {
	store := web.NewStore()
	now := time.Now()

	r1 := makeReport("inc-001", types.IncidentStatusResolved)
	r1.StartedAt = now.Add(-2 * time.Minute)
	r2 := makeReport("inc-002", types.IncidentStatusFailed)
	r2.StartedAt = now.Add(-1 * time.Minute)

	store.Upsert(r1)
	store.Upsert(r2)

	all := store.All()
	if len(all) != 2 {
		t.Fatalf("expected 2, got %d", len(all))
	}
	// Most recent first.
	if all[0].ID != "inc-002" {
		t.Errorf("expected inc-002 first, got %s", all[0].ID)
	}
}

func TestServer_ListIncidents(t *testing.T) {
	store := web.NewStore()
	store.Upsert(makeReport("inc-001", types.IncidentStatusResolved))
	store.Upsert(makeReport("inc-002", types.IncidentStatusFailed))

	srv := web.NewServer(store)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/incidents")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	var incidents []types.IncidentReport
	if err := json.NewDecoder(resp.Body).Decode(&incidents); err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	if len(incidents) != 2 {
		t.Errorf("expected 2 incidents, got %d", len(incidents))
	}
}

func TestServer_GetIncident_Found(t *testing.T) {
	store := web.NewStore()
	store.Upsert(makeReport("inc-001", types.IncidentStatusResolved))

	srv := web.NewServer(store)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/incidents/inc-001")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	var inc types.IncidentReport
	if err := json.NewDecoder(resp.Body).Decode(&inc); err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	if inc.ID != "inc-001" {
		t.Errorf("expected inc-001, got %s", inc.ID)
	}
}

func TestServer_GetIncident_NotFound(t *testing.T) {
	store := web.NewStore()
	srv := web.NewServer(store)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/incidents/nonexistent")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404, got %d", resp.StatusCode)
	}
}

func TestServer_Dashboard_HTML(t *testing.T) {
	store := web.NewStore()
	store.Upsert(makeReport("inc-001", types.IncidentStatusResolved))

	srv := web.NewServer(store)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/dashboard")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	ct := resp.Header.Get("Content-Type")
	if !strings.HasPrefix(ct, "text/html") {
		t.Errorf("expected text/html content type, got %s", ct)
	}
}

func TestServer_PipelineObserver_UpdatesTimeline(t *testing.T) {
	store := web.NewStore()
	// Pre-populate a report so the observer can find it.
	report := makeReport("inc-001", types.IncidentStatusInProgress)
	store.Upsert(report)

	srv := web.NewServer(store)
	obs := srv.NewPipelineObserver("inc-001")

	// Simulate a completed classify step.
	obs.OnStep(streampkg.Step{
		AgentName: "classify",
		Type:      streampkg.StepCompleted,
		Timestamp: time.Now(),
		Index:     0,
	})

	got, ok := store.Get("inc-001")
	if !ok {
		t.Fatal("report not found")
	}
	if len(got.Timeline) == 0 {
		t.Error("expected timeline to have at least one entry")
	}
	if got.Timeline[0].Event != types.TimelineEventClassified {
		t.Errorf("expected TimelineEventClassified, got %s", got.Timeline[0].Event)
	}
}
