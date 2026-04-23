// Package webhook provides HTTP handlers that receive alert payloads from
// Grafana and PagerDuty and normalise them into types.Alert.
package webhook

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/lucientong/argus/internal/types"
)

// AlertHandler is called with a normalised alert after a webhook is parsed.
type AlertHandler func(alert types.Alert)

// grafanaPayload mirrors the Grafana Alerting webhook JSON schema.
// https://grafana.com/docs/grafana/latest/alerting/manage-notifications/webhook-notifier/
type grafanaPayload struct {
	Title   string `json:"title"`
	Message string `json:"message"`
	State   string `json:"state"` // "alerting" | "ok" | "no_data" | "paused"
	Alerts  []struct {
		Status      string            `json:"status"` // "firing" | "resolved"
		Labels      map[string]string `json:"labels"`
		Annotations map[string]string `json:"annotations"`
		StartsAt    time.Time         `json:"startsAt"`
		Fingerprint string            `json:"fingerprint"`
	} `json:"alerts"`
	// Unified Alerting (Grafana 8+) top-level fields
	Status      string            `json:"status"`
	CommonLabels map[string]string `json:"commonLabels"`
	CommonAnnotations map[string]string `json:"commonAnnotations"`
	GroupLabels  map[string]string `json:"groupLabels"`
}

// GrafanaHandler returns an http.HandlerFunc that parses Grafana webhook payloads.
func GrafanaHandler(h AlertHandler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20)) // 1 MiB limit
		if err != nil {
			slog.Error("grafana webhook: read body", "error", err)
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}

		var p grafanaPayload
		if err := json.Unmarshal(body, &p); err != nil {
			slog.Error("grafana webhook: unmarshal", "error", err)
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}

		// Grafana Unified Alerting sends one payload per group; each alert in
		// the .alerts array is an individual firing instance.
		if len(p.Alerts) == 0 {
			// Fallback: treat the root-level payload as a single alert.
			alert := grafanaRootToAlert(p, body)
			slog.Info("grafana webhook received (root-level)", "id", alert.ID, "severity", alert.Severity)
			h(alert)
		} else {
			for _, a := range p.Alerts {
				if a.Status == "resolved" {
					continue // ignore resolve events for now
				}
				labels := mergeLabels(p.CommonLabels, a.Labels)
				annotations := mergeLabels(p.CommonAnnotations, a.Annotations)

				alert := types.Alert{
					ID:          a.Fingerprint,
					Source:      types.SourceGrafana,
					Title:       labelOr(annotations, "summary", p.Title),
					Description: labelOr(annotations, "description", p.Message),
					Severity:    grafanaSeverity(labels),
					Service:     labelOr(labels, "service", labelOr(labels, "job", "")),
					Environment: labelOr(labels, "env", labelOr(labels, "environment", "")),
					Labels:      labels,
					Annotations: annotations,
					FiredAt:     a.StartsAt,
					Raw:         body,
				}
				slog.Info("grafana webhook received", "id", alert.ID, "severity", alert.Severity, "title", alert.Title)
				h(alert)
			}
		}

		w.WriteHeader(http.StatusOK)
		fmt.Fprintln(w, `{"status":"ok"}`)
	}
}

func grafanaRootToAlert(p grafanaPayload, raw []byte) types.Alert {
	return types.Alert{
		ID:          fmt.Sprintf("grafana-%d", time.Now().UnixNano()),
		Source:      types.SourceGrafana,
		Title:       p.Title,
		Description: p.Message,
		Severity:    grafanaStateSeverity(p.State),
		Labels:      p.CommonLabels,
		Annotations: p.CommonAnnotations,
		FiredAt:     time.Now(),
		Raw:         raw,
	}
}

func grafanaSeverity(labels map[string]string) types.Severity {
	switch labels["severity"] {
	case "critical", "page":
		return types.SeverityCritical
	case "warning", "warn":
		return types.SeverityWarning
	case "info":
		return types.SeverityInfo
	default:
		return types.SeverityUnknown
	}
}

func grafanaStateSeverity(state string) types.Severity {
	switch state {
	case "alerting":
		return types.SeverityWarning
	default:
		return types.SeverityUnknown
	}
}

// pagerdutyPayload mirrors PagerDuty V2 webhook schema.
// https://developer.pagerduty.com/docs/ZG9jOjQ1MTg4ODQ0-overview
type pagerdutyPayload struct {
	Messages []struct {
		Event   string `json:"event"` // "incident.trigger" | "incident.acknowledge" | …
		Incident struct {
			ID          string `json:"id"`
			Title       string `json:"title"`
			Description string `json:"description"`
			Status      string `json:"status"` // "triggered" | "acknowledged" | "resolved"
			Urgency     string `json:"urgency"` // "high" | "low"
			Service     struct {
				Summary string `json:"summary"`
			} `json:"service"`
			CreatedAt time.Time `json:"created_at"`
			Body      struct {
				Details string `json:"details"`
			} `json:"body"`
		} `json:"incident"`
	} `json:"messages"`
}

// PagerDutyHandler returns an http.HandlerFunc that parses PagerDuty V2 webhook payloads.
func PagerDutyHandler(h AlertHandler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		if err != nil {
			slog.Error("pagerduty webhook: read body", "error", err)
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}

		var p pagerdutyPayload
		if err := json.Unmarshal(body, &p); err != nil {
			slog.Error("pagerduty webhook: unmarshal", "error", err)
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}

		for _, msg := range p.Messages {
			if msg.Event != "incident.trigger" {
				continue
			}
			inc := msg.Incident
			desc := inc.Description
			if desc == "" {
				desc = inc.Body.Details
			}
			alert := types.Alert{
				ID:          inc.ID,
				Source:      types.SourcePagerDuty,
				Title:       inc.Title,
				Description: desc,
				Severity:    pagerDutySeverity(inc.Urgency),
				Service:     inc.Service.Summary,
				Labels:      map[string]string{"urgency": inc.Urgency, "status": inc.Status},
				Annotations: map[string]string{},
				FiredAt:     inc.CreatedAt,
				Raw:         body,
			}
			slog.Info("pagerduty webhook received", "id", alert.ID, "severity", alert.Severity, "title", alert.Title)
			h(alert)
		}

		w.WriteHeader(http.StatusOK)
		fmt.Fprintln(w, `{"status":"ok"}`)
	}
}

func pagerDutySeverity(urgency string) types.Severity {
	switch urgency {
	case "high":
		return types.SeverityCritical
	case "low":
		return types.SeverityWarning
	default:
		return types.SeverityUnknown
	}
}

// mergeLabels returns a new map with base overridden by overrides.
func mergeLabels(base, overrides map[string]string) map[string]string {
	merged := make(map[string]string, len(base)+len(overrides))
	for k, v := range base {
		merged[k] = v
	}
	for k, v := range overrides {
		merged[k] = v
	}
	return merged
}

// labelOr returns labels[key] if present and non-empty, otherwise fallback.
func labelOr(labels map[string]string, key, fallback string) string {
	if v, ok := labels[key]; ok && v != "" {
		return v
	}
	return fallback
}
