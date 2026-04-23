# Changelog

All notable changes to Argus will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added
- Initial planning documents: `PLAN.md` (15-phase implementation plan), `progress.md` (live tracker), and this `CHANGELOG.md`.
- `README.md` with project overview, architecture summary, and links to planning docs.
- **Phase 0 — Bootstrap:** `go.mod` (module `github.com/lucientong/argus`, waggle via local replace), `cmd/argus/main.go` (HTTP server :8080, `/health` endpoint, graceful shutdown), `internal/config` (YAML loader + env-var overrides), `configs/config.yaml`, `Makefile` (`build`/`test`/`run`/`clean`/`lint`), full directory skeleton.
- **Phase 1 — Types & Webhook Ingress:** Core data types (`Alert`, `ClassifiedAlert`, `Diagnosis`, `Runbook`, `RemediationPlan`, `IncidentReport` and supporting structs/enums) in `internal/types`. Grafana Unified Alerting and PagerDuty V2 webhook parsers in `internal/webhook`; routes `/webhook/grafana` and `/webhook/pagerduty` registered.
- **Phase 2 — ClassifyAgent + Router:** `ClassifyAgent` uses `output.NewStructuredAgent` to classify a raw `Alert` into `ClassifiedAlert` (category, severity, confidence, reasoning). `SeverityRouter` dispatches `ClassifiedAlert` to critical/warning/info branches using `waggle.Router`. Stub `DiagnoseAgent` added as placeholder. Prompt templates in `internal/prompts`.
- **Phase 3 — Integration Clients (Mocks):** Interface + mock implementations for Prometheus, Kubernetes, Slack, Grafana, and PagerDuty clients in `internal/integrations/`. All mocks are injectable and test-friendly (configurable responses, error injection, auto-approve for Slack).
- **Phase 4 — DiagnosticAgent:** `DiagnosticAgent` queries Prometheus + Kubernetes, gathers recent deploys, then prompts LLM for a root-cause hypothesis. Integration errors are non-fatal (partial context proceeds). Full `DiagnosePrompt` template added.
- **Phase 5 — Runbook RAG:** Four sample runbooks added (`high-cpu`, `high-error-rate`, `pod-crashloop`, `database-connections`). `internal/runbooks` package loads and ingests markdown runbooks into an in-memory vector store. `NewRunbookSearchAgent` wires `rag.NewPipeline` (TopK=3) to retrieve and return a typed `types.Runbook` given a `types.Diagnosis`.
- **Phase 6 — RemediationAgent:** `NewRemediationAgent` takes a `RemediationInput` (Diagnosis + Runbook) and returns a `RemediationPlan` with an ordered list of typed `RemediationAction` items (type, description, command, risk_level). Sets `RequiresApproval=true` when any action has risk_level="high". Unknown risk levels default to "medium".
- **Phase 7 — Approval Gate:** `NewApprovalAgent` auto-approves low-risk plans and sends a blocking Slack approval request for high-risk plans. Returns `ApprovalDecision` with approved/denied status and approver comment.
- **Phase 8 — ExecuteAgent + Guardrails:** `NewExecuteAgent` executes remediation actions sequentially, stopping on first failure. `guardrail.WithInputExtractGuard` validates each command against `dangerousCommandGuard` (blocks `kubectl delete namespace/node`, `rm -rf /`, `DROP DATABASE`, etc.) before execution. Unapproved plans are short-circuited without running any commands.
- **Phase 9 — VerifyAgent + Loop Wiring:** `NewVerifyAgent` re-queries Prometheus after remediation and asks the LLM to assess recovery based on pre/post metrics. If execution failed, immediately returns `Recovered=false`. `pipeline.Build` wires all agents into a `waggle.Loop` that retries the remediate→approve→execute→verify cycle up to `MaxIterations` times until the incident is confirmed resolved. Failed or max-exceeded loops produce a complete `IncidentReport` with `Status=failed`.
- **Phase 10 — NotifyAgent + IncidentReport:** `NewNotifyAgent` generates a Slack-markdown incident summary and posts it to the configured channel (non-fatal on error), then persists the full `IncidentReport` as JSON to `./incidents/<id>.json` (non-fatal on error). `TimelineEntry`/`TimelineEvent` types added to `IncidentReport` for Phase 11 streaming. `pipeline.Deps.Notify` wired as the terminal step after status stamping.
- **Phase 12 — Real Integration Clients:** Real HTTP clients added for all five integrations (Prometheus, Kubernetes, Slack, Grafana, PagerDuty). `internal/integrations/factory.go` selects mock vs real based on `cfg.Integrations.Mode` (`"mock"` or `"real"`). Slack `RequestApproval` uses Block Kit buttons + in-process callback channel. Kubernetes client supports both in-cluster (service-account) and kubeconfig bearer-token auth. `cmd/argus/main.go` wires `integrations.Build(cfg)` at startup.


### Changed
- _None yet._

### Fixed
- _None yet._

### Removed
- _None yet._
