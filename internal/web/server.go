// Package web provides the Argus incident dashboard HTTP server.
//
// It exposes:
//   - GET /dashboard       — minimal HTML UI listing live and recent incidents
//   - GET /api/incidents   — JSON list of all known incidents (most-recent first)
//   - GET /api/incidents/{id} — JSON for a single incident
//   - GET /api/events      — Server-Sent Events stream of pipeline step updates
package web

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	streampkg "github.com/lucientong/waggle/pkg/stream"

	"github.com/lucientong/argus/internal/types"
)

// Store is a thread-safe in-memory store for IncidentReports.
type Store struct {
	mu        sync.RWMutex
	incidents map[string]*types.IncidentReport
}

// NewStore returns an empty incident Store.
func NewStore() *Store {
	return &Store{incidents: make(map[string]*types.IncidentReport)}
}

// Upsert stores or replaces the given report.
func (s *Store) Upsert(report types.IncidentReport) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := report // copy
	s.incidents[report.ID] = &cp
}

// Get returns the report for the given ID, or false if not found.
func (s *Store) Get(id string) (types.IncidentReport, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	r, ok := s.incidents[id]
	if !ok {
		return types.IncidentReport{}, false
	}
	return *r, true
}

// All returns all reports sorted by StartedAt descending (most recent first).
func (s *Store) All() []types.IncidentReport {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]types.IncidentReport, 0, len(s.incidents))
	for _, r := range s.incidents {
		out = append(out, *r)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].StartedAt.After(out[j].StartedAt)
	})
	return out
}

// sseClient is a single connected SSE subscriber.
type sseClient struct {
	ch   chan string
	done <-chan struct{}
}

// hub manages SSE clients and broadcasts events.
type hub struct {
	mu      sync.Mutex
	clients map[*sseClient]struct{}
}

func newHub() *hub {
	return &hub{clients: make(map[*sseClient]struct{})}
}

func (h *hub) add(c *sseClient) {
	h.mu.Lock()
	h.clients[c] = struct{}{}
	h.mu.Unlock()
}

func (h *hub) remove(c *sseClient) {
	h.mu.Lock()
	delete(h.clients, c)
	h.mu.Unlock()
}

func (h *hub) broadcast(msg string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for c := range h.clients {
		select {
		case c.ch <- msg:
		default:
			// Slow client — drop rather than block.
		}
	}
}

// Server is the Argus incident dashboard HTTP server.
type Server struct {
	store *Store
	hub   *hub
	mux   *http.ServeMux
}

// NewServer creates a new dashboard Server wired to the given incident Store.
func NewServer(store *Store) *Server {
	s := &Server{
		store: store,
		hub:   newHub(),
		mux:   http.NewServeMux(),
	}
	s.mux.HandleFunc("GET /dashboard", s.handleDashboard)
	s.mux.HandleFunc("GET /api/incidents", s.handleListIncidents)
	s.mux.HandleFunc("GET /api/incidents/{id}", s.handleGetIncident)
	s.mux.HandleFunc("GET /api/events", s.handleSSE)
	return s
}

// Handler returns the underlying http.Handler for mounting into a parent mux or httptest.
func (s *Server) Handler() http.Handler {
	return s.mux
}

// NewPipelineObserver returns a stream.Observer that appends TimelineEntries to
// the in-flight incident (looked up by incidentID) and fans SSE step events out
// to all connected dashboard clients.
//
// The returned Observer should be used with ObservableChain wrappers around the
// pipeline agents for the given incident.
func (s *Server) NewPipelineObserver(incidentID string) streampkg.Observer {
	return streampkg.ObserverFunc(func(step streampkg.Step) {
		// Build a compact SSE message.
		payload := map[string]any{
			"incident_id": incidentID,
			"agent":       step.AgentName,
			"type":        string(step.Type),
			"index":       step.Index,
			"timestamp":   step.Timestamp.UTC().Format(time.RFC3339),
		}
		if step.Content != "" {
			// Truncate large outputs to keep SSE payloads small.
			content := step.Content
			if len(content) > 256 {
				content = content[:256] + "…"
			}
			payload["content"] = content
		}
		data, err := json.Marshal(payload)
		if err == nil {
			s.hub.broadcast(fmt.Sprintf("data: %s\n\n", string(data)))
		}

		// Append a TimelineEntry for completed / error steps.
		if step.Type == streampkg.StepCompleted || step.Type == streampkg.StepError {
			event := types.TimelineEventClassified // default; caller can enrich
			detail := ""
			switch step.AgentName {
			case "classify":
				event = types.TimelineEventClassified
			case "diagnose":
				event = types.TimelineEventDiagnosed
			case "runbook":
				event = types.TimelineEventRunbook
			case "remediate":
				event = types.TimelineEventPlanned
			case "approve":
				if step.Type == streampkg.StepError {
					event = types.TimelineEventDenied
				} else {
					event = types.TimelineEventApproved
				}
			case "execute":
				event = types.TimelineEventExecuted
			case "verify":
				event = types.TimelineEventVerified
			case "notify":
				event = types.TimelineEventResolved
			}
			if step.Type == streampkg.StepError {
				detail = step.Content
			}

			entry := types.TimelineEntry{
				Event:     event,
				Timestamp: step.Timestamp,
				Detail:    detail,
			}

			// Update the store (if the report is already there).
			existing, ok := s.store.Get(incidentID)
			if ok {
				existing.Timeline = append(existing.Timeline, entry)
				s.store.Upsert(existing)
			}
		}

		slog.Debug("pipeline step", "incident", incidentID, "agent", step.AgentName, "type", step.Type)
	})
}

// handleDashboard serves a minimal HTML page listing incidents.
func (s *Server) handleDashboard(w http.ResponseWriter, r *http.Request) {
	incidents := s.store.All()
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprint(w, dashboardHTML(incidents))
}

// handleListIncidents serves GET /api/incidents as JSON.
func (s *Server) handleListIncidents(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, s.store.All())
}

// handleGetIncident serves GET /api/incidents/{id} as JSON.
func (s *Server) handleGetIncident(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	report, ok := s.store.Get(id)
	if !ok {
		http.Error(w, `{"error":"not found"}`, http.StatusNotFound)
		return
	}
	writeJSON(w, report)
}

// handleSSE is the SSE endpoint for live pipeline step events.
func (s *Server) handleSSE(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	client := &sseClient{
		ch:   make(chan string, 32),
		done: r.Context().Done(),
	}
	s.hub.add(client)
	defer s.hub.remove(client)

	// Initial ping.
	fmt.Fprint(w, "data: {\"type\":\"connected\"}\n\n")
	flusher.Flush()

	for {
		select {
		case <-r.Context().Done():
			return
		case msg, ok := <-client.ch:
			if !ok {
				return
			}
			fmt.Fprint(w, msg)
			flusher.Flush()
		}
	}
}

// writeJSON encodes v as JSON to w.
func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-cache")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		http.Error(w, "json encode error", http.StatusInternalServerError)
	}
}

// dashboardHTML generates a minimal HTML page for the given incidents.
func dashboardHTML(incidents []types.IncidentReport) string {
	var sb strings.Builder
	sb.WriteString(`<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width,initial-scale=1">
  <title>Argus — Incident Dashboard</title>
  <style>
    body { font-family: system-ui, sans-serif; margin: 2rem; background: #f8f9fa; color: #212529; }
    h1   { font-size: 1.5rem; margin-bottom: 1rem; }
    table { width: 100%; border-collapse: collapse; background: #fff; border-radius: 8px; overflow: hidden; box-shadow: 0 1px 3px rgba(0,0,0,.12); }
    th, td { padding: .6rem 1rem; text-align: left; border-bottom: 1px solid #dee2e6; }
    th { background: #343a40; color: #fff; font-weight: 600; }
    tr:last-child td { border-bottom: none; }
    .resolved { color: #198754; font-weight: 600; }
    .failed   { color: #dc3545; font-weight: 600; }
    .in_progress { color: #0d6efd; font-weight: 600; }
    #events { margin-top: 1.5rem; padding: 1rem; background: #fff; border-radius: 8px; box-shadow: 0 1px 3px rgba(0,0,0,.12); max-height: 300px; overflow-y: auto; font-family: monospace; font-size: .8rem; }
  </style>
</head>
<body>
<h1>🔍 Argus — Incident Dashboard</h1>
`)
	if len(incidents) == 0 {
		sb.WriteString("<p>No incidents yet.</p>\n")
	} else {
		sb.WriteString(`<table>
  <tr><th>ID</th><th>Title</th><th>Service</th><th>Status</th><th>Severity</th><th>Started</th><th>Iterations</th></tr>
`)
		for _, inc := range incidents {
			statusClass := string(inc.Status)
			started := inc.StartedAt.UTC().Format("2006-01-02 15:04:05")
			sb.WriteString(fmt.Sprintf(
				`  <tr><td><a href="/api/incidents/%s">%s</a></td><td>%s</td><td>%s</td><td class="%s">%s</td><td>%s</td><td>%s</td><td>%d</td></tr>
`,
				inc.ID, inc.ID,
				htmlEscape(inc.Alert.Alert.Title),
				htmlEscape(inc.Alert.Alert.Service),
				statusClass, inc.Status,
				inc.Alert.Severity,
				started,
				inc.LoopIterations,
			))
		}
		sb.WriteString("</table>\n")
	}

	sb.WriteString(`<div id="events"><em>Live pipeline events appear here…</em></div>
<script>
const el = document.getElementById('events');
const src = new EventSource('/api/events');
src.onmessage = e => {
  const d = JSON.parse(e.data);
  if (d.type === 'connected') return;
  const line = document.createElement('div');
  line.textContent = '[' + (d.timestamp||'') + '] ' + (d.incident_id||'') + ' · ' + (d.agent||'') + ' → ' + (d.type||'');
  el.appendChild(line);
  el.scrollTop = el.scrollHeight;
};
</script>
</body>
</html>
`)
	return sb.String()
}

// htmlEscape escapes the five XML-significant characters.
func htmlEscape(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	s = strings.ReplaceAll(s, `"`, "&#34;")
	s = strings.ReplaceAll(s, "'", "&#39;")
	return s
}
