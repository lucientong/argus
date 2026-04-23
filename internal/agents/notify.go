package agents

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	wagentpkg "github.com/lucientong/waggle/pkg/agent"

	"github.com/lucientong/argus/internal/integrations/slack"
	"github.com/lucientong/argus/internal/types"
)

// NotifyDeps holds the dependencies needed by the NotifyAgent.
type NotifyDeps struct {
	// Slack is used to post the incident summary.
	Slack slack.Client
	// Channel is the Slack channel to post to (e.g. "#incidents").
	Channel string
	// IncidentsDir is the directory where JSON reports are persisted.
	// Defaults to "./incidents" when empty.
	IncidentsDir string
}

// NewNotifyAgent returns an Agent[types.IncidentReport, types.IncidentReport] that:
//  1. Generates a human-readable Slack summary of the incident.
//  2. Posts the summary to the configured Slack channel (non-fatal on failure).
//  3. Persists the full IncidentReport as JSON to <IncidentsDir>/<id>.json (non-fatal on failure).
//
// The agent passes through the (possibly slightly enriched) report unchanged so it can be
// chained at the end of the pipeline.
func NewNotifyAgent(deps NotifyDeps) wagentpkg.Agent[types.IncidentReport, types.IncidentReport] {
	dir := deps.IncidentsDir
	if dir == "" {
		dir = "./incidents"
	}

	return wagentpkg.Func[types.IncidentReport, types.IncidentReport](
		"notify",
		func(ctx context.Context, report types.IncidentReport) (types.IncidentReport, error) {
			// Build the Slack message.
			text := buildIncidentSummary(report)
			report.Summary = text

			// Post to Slack (non-fatal).
			if deps.Slack != nil {
				msg := slack.Message{
					Channel: deps.Channel,
					Text:    text,
				}
				if err := deps.Slack.PostMessage(ctx, msg); err != nil {
					slog.Warn("notify: failed to post Slack message", "error", err, "incident_id", report.ID)
				} else {
					slog.Info("notify: Slack message posted", "channel", deps.Channel, "incident_id", report.ID)
				}
			}

			// Persist to disk (non-fatal).
			if err := persistReport(dir, report); err != nil {
				slog.Warn("notify: failed to persist incident report", "error", err, "incident_id", report.ID)
			} else {
				slog.Info("notify: incident report persisted", "dir", dir, "incident_id", report.ID)
			}

			return report, nil
		},
	)
}

// buildIncidentSummary produces a Slack-friendly markdown summary of the incident.
func buildIncidentSummary(report types.IncidentReport) string {
	statusEmoji := "🔴"
	if report.Status == types.IncidentStatusResolved {
		statusEmoji = "✅"
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("%s *Incident Report — %s*\n", statusEmoji, report.ID))
	sb.WriteString(fmt.Sprintf("*Status:* %s\n", report.Status))

	// Alert details
	alert := report.Alert.Alert
	sb.WriteString(fmt.Sprintf("*Alert:* %s (`%s` · `%s`)\n",
		alert.Title, alert.Service, alert.Environment))
	sb.WriteString(fmt.Sprintf("*Severity:* %s  *Category:* %s\n",
		report.Alert.Severity, report.Alert.Category))

	// Diagnosis
	if report.Diagnosis.Hypothesis != "" {
		sb.WriteString(fmt.Sprintf("*Root Cause:* %s (confidence: %.0f%%)\n",
			report.Diagnosis.Hypothesis, report.Diagnosis.Confidence*100))
	}

	// Remediation plan
	if len(report.Plan.Actions) > 0 {
		sb.WriteString("*Actions Taken:*\n")
		for i, action := range report.Plan.Actions {
			sb.WriteString(fmt.Sprintf("  %d. `%s` — %s (%s risk)\n",
				i+1, action.Command, action.Description, action.RiskLevel))
		}
	}

	// Execution outcome
	if report.Execution != nil {
		outcome := "✓ succeeded"
		if !report.Execution.Success {
			outcome = "✗ failed"
			if report.Execution.Error != "" {
				outcome += ": " + report.Execution.Error
			}
		}
		sb.WriteString(fmt.Sprintf("*Execution:* %s\n", outcome))
	}

	// Verification
	if report.Verification != nil {
		recoveredStr := "not recovered"
		if report.Verification.Recovered {
			recoveredStr = "recovered"
		}
		sb.WriteString(fmt.Sprintf("*Verification:* %s — %s\n",
			recoveredStr, report.Verification.Explanation))
	}

	// Timing
	duration := ""
	if report.ResolvedAt != nil {
		d := report.ResolvedAt.Sub(report.StartedAt).Truncate(time.Second)
		duration = fmt.Sprintf(" (resolved in %s)", d)
	}
	sb.WriteString(fmt.Sprintf("*Started:* %s%s  *Iterations:* %d\n",
		report.StartedAt.UTC().Format(time.RFC3339), duration, report.LoopIterations))

	return sb.String()
}

// persistReport writes report as pretty-printed JSON to <dir>/<id>.json.
func persistReport(dir string, report types.IncidentReport) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", dir, err)
	}

	path := filepath.Join(dir, report.ID+".json")
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal report: %w", err)
	}

	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}
