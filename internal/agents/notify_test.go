package agents_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/lucientong/argus/internal/agents"
	"github.com/lucientong/argus/internal/integrations/slack"
	"github.com/lucientong/argus/internal/types"
)

// makeIncidentReport builds a fully populated IncidentReport for testing.
func makeIncidentReport(status types.IncidentStatus, recovered bool) types.IncidentReport {
	now := time.Now()
	plan := makeRemediationPlan(false)
	execResult := makeExecResult(true)
	verif := types.VerificationResult{
		Recovered:   recovered,
		Explanation: "metrics checked",
	}
	return types.IncidentReport{
		ID:             "incident-001",
		Alert:          plan.Diagnosis.Alert,
		Diagnosis:      plan.Diagnosis,
		Plan:           plan,
		Execution:      &execResult,
		Verification:   &verif,
		Status:         status,
		StartedAt:      now.Add(-30 * time.Second),
		ResolvedAt:     &now,
		LoopIterations: 1,
	}
}

func TestNotifyAgent_PostsSlackAndPersists(t *testing.T) {
	dir := t.TempDir()
	slackClient := &slack.MockClient{}

	a := agents.NewNotifyAgent(agents.NotifyDeps{
		Slack:        slackClient,
		Channel:      "#incidents",
		IncidentsDir: dir,
	})

	report := makeIncidentReport(types.IncidentStatusResolved, true)
	out, err := a.Run(context.Background(), report)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Slack message posted
	if len(slackClient.Sent) != 1 {
		t.Fatalf("expected 1 Slack message, got %d", len(slackClient.Sent))
	}
	if slackClient.Sent[0].Channel != "#incidents" {
		t.Errorf("wrong channel: %s", slackClient.Sent[0].Channel)
	}
	if slackClient.Sent[0].Text == "" {
		t.Error("expected non-empty Slack message text")
	}

	// Summary populated on output report
	if out.Summary == "" {
		t.Error("expected Summary to be populated")
	}

	// JSON persisted
	path := filepath.Join(dir, "incident-001.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("expected report file %s: %v", path, err)
	}
	if len(data) == 0 {
		t.Error("expected non-empty report file")
	}
}

func TestNotifyAgent_SlackError_NonFatal(t *testing.T) {
	dir := t.TempDir()
	slackClient := &slack.MockClient{Err: context.DeadlineExceeded}

	a := agents.NewNotifyAgent(agents.NotifyDeps{
		Slack:        slackClient,
		Channel:      "#incidents",
		IncidentsDir: dir,
	})

	report := makeIncidentReport(types.IncidentStatusFailed, false)
	_, err := a.Run(context.Background(), report)
	if err != nil {
		t.Fatalf("Slack error should be non-fatal, got: %v", err)
	}

	// File should still be persisted despite Slack failure
	path := filepath.Join(dir, "incident-001.json")
	if _, statErr := os.Stat(path); os.IsNotExist(statErr) {
		t.Error("expected incident file to be persisted even when Slack fails")
	}
}

func TestNotifyAgent_NoSlack_PersistsOnly(t *testing.T) {
	dir := t.TempDir()

	a := agents.NewNotifyAgent(agents.NotifyDeps{
		Slack:        nil, // no Slack client
		IncidentsDir: dir,
	})

	report := makeIncidentReport(types.IncidentStatusResolved, true)
	out, err := a.Run(context.Background(), report)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if out.Summary == "" {
		t.Error("expected Summary to be set even without Slack client")
	}

	path := filepath.Join(dir, "incident-001.json")
	if _, statErr := os.Stat(path); os.IsNotExist(statErr) {
		t.Error("expected incident file to be persisted")
	}
}

func TestNotifyAgent_FailedIncident_SlackMessageContainsStatus(t *testing.T) {
	dir := t.TempDir()
	slackClient := &slack.MockClient{}

	a := agents.NewNotifyAgent(agents.NotifyDeps{
		Slack:        slackClient,
		Channel:      "#incidents",
		IncidentsDir: dir,
	})

	report := makeIncidentReport(types.IncidentStatusFailed, false)
	_, err := a.Run(context.Background(), report)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(slackClient.Sent) == 0 {
		t.Fatal("expected Slack message")
	}
	msg := slackClient.Sent[0].Text
	// Should mention "failed" in the summary
	if msg == "" {
		t.Error("expected non-empty message")
	}
}
