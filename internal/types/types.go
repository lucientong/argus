// Package types defines the core data types shared across all Argus agents.
package types

import "time"

// ------------------- Alert ---------------------------------------------------

// Source indicates which monitoring system produced the alert.
type Source string

const (
	SourceGrafana    Source = "grafana"
	SourcePagerDuty  Source = "pagerduty"
)

// Severity maps to PagerDuty/Grafana severity levels.
type Severity string

const (
	SeverityCritical Severity = "critical"
	SeverityWarning  Severity = "warning"
	SeverityInfo     Severity = "info"
	SeverityUnknown  Severity = "unknown"
)

// Category is the high-level class of incident.
type Category string

const (
	CategoryInfra    Category = "infra"
	CategoryApp      Category = "app"
	CategoryNetwork  Category = "network"
	CategoryDatabase Category = "database"
	CategorySecurity Category = "security"
	CategoryUnknown  Category = "unknown"
)

// Alert is the canonical, normalised form of any incoming alert regardless of source.
type Alert struct {
	ID          string            `json:"id"`
	Source      Source            `json:"source"`
	Title       string            `json:"title"`
	Description string            `json:"description"`
	Severity    Severity          `json:"severity"`
	Service     string            `json:"service"`   // impacted service/app name
	Environment string            `json:"environment"` // prod, staging, dev …
	Labels      map[string]string `json:"labels"`
	Annotations map[string]string `json:"annotations"`
	FiredAt     time.Time         `json:"fired_at"`
	// Raw is the original JSON payload, kept for debugging.
	Raw []byte `json:"-"`
}

// ------------------- ClassifiedAlert ----------------------------------------

// ClassifiedAlert enriches a raw Alert with LLM-assigned classification.
type ClassifiedAlert struct {
	Alert      Alert    `json:"alert"`
	Category   Category `json:"category"`
	Severity   Severity `json:"severity"` // LLM may override source severity
	Confidence float64  `json:"confidence"` // 0–1
	Reasoning  string   `json:"reasoning"`
}

// ------------------- Diagnosis -----------------------------------------------

// MetricSnapshot is a single metric observation captured during diagnosis.
type MetricSnapshot struct {
	Name   string  `json:"name"`
	Value  float64 `json:"value"`
	Labels map[string]string `json:"labels"`
}

// DeployEvent records a recent deployment that may be the root cause.
type DeployEvent struct {
	Service   string    `json:"service"`
	Version   string    `json:"version"`
	DeployedAt time.Time `json:"deployed_at"`
	Author    string    `json:"author"`
}

// K8sInfo holds Kubernetes pod/deployment state at diagnosis time.
type K8sInfo struct {
	Namespace    string   `json:"namespace"`
	Deployment   string   `json:"deployment"`
	ReadyReplicas int32   `json:"ready_replicas"`
	TotalReplicas int32   `json:"total_replicas"`
	RestartCount  int32   `json:"restart_count"`
	Events       []string `json:"events"` // recent k8s events (human-readable)
}

// Diagnosis is the output of the DiagnosticAgent.
type Diagnosis struct {
	Alert          ClassifiedAlert  `json:"alert"`
	Hypothesis     string           `json:"hypothesis"`    // root-cause sentence
	Confidence     float64          `json:"confidence"`    // 0–1
	Metrics        []MetricSnapshot `json:"metrics"`
	RecentDeploys  []DeployEvent    `json:"recent_deploys"`
	K8s            *K8sInfo         `json:"k8s,omitempty"`
	RawContext     string           `json:"raw_context"`   // all gathered data as text
}

// ------------------- Runbook -------------------------------------------------

// Runbook is a matched remediation playbook from the RAG pipeline.
type Runbook struct {
	Title   string `json:"title"`
	Content string `json:"content"` // full markdown text
	Source  string `json:"source"`  // filename / path
}

// ------------------- RemediationPlan ----------------------------------------

// ActionType categorises what kind of change an action will make.
type ActionType string

const (
	ActionRollback     ActionType = "rollback"
	ActionRestart      ActionType = "restart"
	ActionScale        ActionType = "scale"
	ActionConfigChange ActionType = "config_change"
	ActionCustom       ActionType = "custom"
)

// RemediationAction is one step in the remediation plan.
type RemediationAction struct {
	Type        ActionType `json:"type"`
	Description string     `json:"description"` // human-readable
	Command     string     `json:"command"`     // actual command / API call to run
	RiskLevel   string     `json:"risk_level"`  // "low" | "medium" | "high"
}

// RemediationPlan is the output of the RemediationAgent.
type RemediationPlan struct {
	Diagnosis  Diagnosis           `json:"diagnosis"`
	Runbook    Runbook             `json:"runbook"`
	Actions    []RemediationAction `json:"actions"`
	Rationale  string              `json:"rationale"`
	// RequiresApproval is true if any action has risk_level == "high".
	RequiresApproval bool `json:"requires_approval"`
}

// ------------------- ApprovalDecision ----------------------------------------

// ApprovalDecision is the result of the human-in-the-loop approval step.
type ApprovalDecision struct {
	Plan     RemediationPlan `json:"plan"`
	Approved bool            `json:"approved"`
	Comment  string          `json:"comment"` // approver's note
}

// ------------------- ExecutionResult -----------------------------------------

// ExecutionResult records what happened when the remediation was applied.
type ExecutionResult struct {
	Plan     RemediationPlan     `json:"plan"`
	Actions  []ActionOutcome     `json:"actions"`
	Success  bool                `json:"success"`
	Error    string              `json:"error,omitempty"`
}

// ActionOutcome records the result of one remediation action.
type ActionOutcome struct {
	Action  RemediationAction `json:"action"`
	Output  string            `json:"output"`
	Success bool              `json:"success"`
	Error   string            `json:"error,omitempty"`
}

// ------------------- IncidentReport ------------------------------------------

// IncidentStatus tracks the overall lifecycle of the incident.
type IncidentStatus string

const (
	IncidentStatusOpen       IncidentStatus = "open"
	IncidentStatusInProgress IncidentStatus = "in_progress"
	IncidentStatusResolved   IncidentStatus = "resolved"
	IncidentStatusFailed     IncidentStatus = "failed"
)

// VerificationResult is the output of the VerifyAgent.
type VerificationResult struct {
	Recovered   bool             `json:"recovered"`
	Metrics     []MetricSnapshot `json:"metrics"`
	Explanation string           `json:"explanation"`
	Iteration   int              `json:"iteration"`
}

// TimelineEvent names a discrete step in the incident lifecycle.
type TimelineEvent string

const (
	TimelineEventClassified  TimelineEvent = "classified"
	TimelineEventDiagnosed   TimelineEvent = "diagnosed"
	TimelineEventRunbook     TimelineEvent = "runbook_found"
	TimelineEventPlanned     TimelineEvent = "plan_created"
	TimelineEventApproved    TimelineEvent = "approved"
	TimelineEventDenied      TimelineEvent = "denied"
	TimelineEventExecuted    TimelineEvent = "executed"
	TimelineEventVerified    TimelineEvent = "verified"
	TimelineEventResolved    TimelineEvent = "resolved"
	TimelineEventFailed      TimelineEvent = "failed"
)

// TimelineEntry records a single moment in the incident lifecycle.
type TimelineEntry struct {
	Event     TimelineEvent `json:"event"`
	Timestamp time.Time     `json:"timestamp"`
	Detail    string        `json:"detail,omitempty"`
}

// IncidentReport is the terminal output of the full pipeline; persisted to disk and posted to Slack.
type IncidentReport struct {
	ID              string              `json:"id"`
	Alert           ClassifiedAlert     `json:"alert"`
	Diagnosis       Diagnosis           `json:"diagnosis"`
	Plan            RemediationPlan     `json:"plan"`
	Execution       *ExecutionResult    `json:"execution,omitempty"`
	Verification    *VerificationResult `json:"verification,omitempty"`
	Status          IncidentStatus      `json:"status"`
	StartedAt       time.Time           `json:"started_at"`
	ResolvedAt      *time.Time          `json:"resolved_at,omitempty"`
	LoopIterations  int                 `json:"loop_iterations"`
	Summary         string              `json:"summary"` // LLM-written plain-English summary
	Timeline        []TimelineEntry     `json:"timeline,omitempty"`
}
