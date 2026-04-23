package webhook_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/lucientong/argus/internal/types"
	"github.com/lucientong/argus/internal/webhook"
)

// ---- Grafana ----------------------------------------------------------------

func TestGrafanaHandler_FiringAlert(t *testing.T) {
	payload := map[string]any{
		"title":   "High CPU",
		"message": "CPU usage > 90%",
		"status":  "firing",
		"commonLabels": map[string]string{
			"severity":    "critical",
			"service":     "api-server",
			"environment": "prod",
		},
		"commonAnnotations": map[string]string{
			"summary": "API server CPU critical",
		},
		"alerts": []map[string]any{
			{
				"status":      "firing",
				"fingerprint": "abc123",
				"startsAt":    time.Now().UTC().Format(time.RFC3339),
				"labels": map[string]string{
					"instance": "api-server-1",
				},
				"annotations": map[string]string{},
			},
		},
	}
	body, _ := json.Marshal(payload)

	var got []types.Alert
	h := webhook.GrafanaHandler(func(a types.Alert) { got = append(got, a) })

	req := httptest.NewRequest(http.MethodPost, "/webhook/grafana", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	h(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 alert, got %d", len(got))
	}
	a := got[0]
	if a.ID != "abc123" {
		t.Errorf("id: want abc123, got %s", a.ID)
	}
	if a.Severity != types.SeverityCritical {
		t.Errorf("severity: want critical, got %s", a.Severity)
	}
	if a.Service != "api-server" {
		t.Errorf("service: want api-server, got %s", a.Service)
	}
	if a.Source != types.SourceGrafana {
		t.Errorf("source: want grafana, got %s", a.Source)
	}
}

func TestGrafanaHandler_SkipsResolved(t *testing.T) {
	payload := map[string]any{
		"alerts": []map[string]any{
			{"status": "resolved", "fingerprint": "xyz", "startsAt": time.Now().UTC().Format(time.RFC3339),
				"labels": map[string]string{}, "annotations": map[string]string{}},
		},
	}
	body, _ := json.Marshal(payload)

	var got []types.Alert
	h := webhook.GrafanaHandler(func(a types.Alert) { got = append(got, a) })

	req := httptest.NewRequest(http.MethodPost, "/webhook/grafana", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	h(rec, req)

	if len(got) != 0 {
		t.Errorf("expected 0 alerts for resolved, got %d", len(got))
	}
}

func TestGrafanaHandler_MethodNotAllowed(t *testing.T) {
	h := webhook.GrafanaHandler(func(types.Alert) {})
	req := httptest.NewRequest(http.MethodGet, "/webhook/grafana", nil)
	rec := httptest.NewRecorder()
	h(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", rec.Code)
	}
}

// ---- PagerDuty --------------------------------------------------------------

func TestPagerDutyHandler_TriggerEvent(t *testing.T) {
	now := time.Now().UTC()
	payload := map[string]any{
		"messages": []map[string]any{
			{
				"event": "incident.trigger",
				"incident": map[string]any{
					"id":          "P123ABC",
					"title":       "Database down",
					"description": "Primary DB unresponsive",
					"status":      "triggered",
					"urgency":     "high",
					"created_at":  now.Format(time.RFC3339),
					"service": map[string]string{
						"summary": "database-service",
					},
					"body": map[string]string{"details": ""},
				},
			},
		},
	}
	body, _ := json.Marshal(payload)

	var got []types.Alert
	h := webhook.PagerDutyHandler(func(a types.Alert) { got = append(got, a) })

	req := httptest.NewRequest(http.MethodPost, "/webhook/pagerduty", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	h(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 alert, got %d", len(got))
	}
	a := got[0]
	if a.ID != "P123ABC" {
		t.Errorf("id: want P123ABC, got %s", a.ID)
	}
	if a.Severity != types.SeverityCritical {
		t.Errorf("severity: want critical, got %s", a.Severity)
	}
	if a.Service != "database-service" {
		t.Errorf("service: want database-service, got %s", a.Service)
	}
	if a.Source != types.SourcePagerDuty {
		t.Errorf("source: want pagerduty, got %s", a.Source)
	}
}

func TestPagerDutyHandler_SkipsNonTrigger(t *testing.T) {
	payload := map[string]any{
		"messages": []map[string]any{
			{
				"event": "incident.acknowledge",
				"incident": map[string]any{
					"id": "P999", "title": "X", "status": "acknowledged",
					"urgency": "low", "created_at": time.Now().UTC().Format(time.RFC3339),
					"service": map[string]string{"summary": "svc"},
					"body":    map[string]string{"details": ""},
				},
			},
		},
	}
	body, _ := json.Marshal(payload)

	var got []types.Alert
	h := webhook.PagerDutyHandler(func(a types.Alert) { got = append(got, a) })
	req := httptest.NewRequest(http.MethodPost, "/webhook/pagerduty", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	h(rec, req)

	if len(got) != 0 {
		t.Errorf("expected 0 alerts for acknowledge event, got %d", len(got))
	}
}
