# Argus

[![CI](https://github.com/lucientong/argus/actions/workflows/ci.yml/badge.svg)](https://github.com/lucientong/argus/actions/workflows/ci.yml)
[![codecov](https://codecov.io/gh/lucientong/argus/branch/main/graph/badge.svg?token=CODECOV_TOKEN)](https://codecov.io/gh/lucientong/argus)
[![Docker](https://img.shields.io/docker/v/lucientong/argus/latest?logo=docker&label=Docker%20Hub)](https://hub.docker.com/r/lucientong/argus)
[![Go Version](https://img.shields.io/badge/go-1.26-blue.svg)](https://golang.org)
[![Go Report Card](https://goreportcard.com/badge/github.com/lucientong/argus)](https://goreportcard.com/report/github.com/lucientong/argus)
[![License](https://img.shields.io/github/license/lucientong/argus)](LICENSE)

> **AI-driven incident response.** Argus ingests alerts, diagnoses root causes, plans and executes remediations, verifies recovery — and keeps a human in the loop at every dangerous step.
>
> Built on [waggle](https://github.com/lucientong/waggle) — the Go AI agent orchestration engine.
> Named after **Argus Panoptes**, the hundred-eyed giant of Greek mythology who never sleeps.

---

## What it does

```
Grafana / PagerDuty alert
         │
         ▼
  ┌─────────────┐    ┌──────────────┐    ┌─────────────────┐
  │  Classify   │───▶│   Diagnose   │───▶│  Search Runbook │
  │  (severity, │    │ (Prometheus  │    │  (RAG over      │
  │  category)  │    │  + K8s +     │    │  markdown       │
  └─────────────┘    │  deploys)    │    │  playbooks)     │
                     └──────────────┘    └─────────────────┘
                                                  │
                     ┌──────────────┐             ▼
                     │   Verify     │    ┌─────────────────┐
                     │ (re-check    │    │   Remediate     │
                     │  metrics,    │    │ (ordered action │
                     │  loop if     │◀───│  plan + risk    │
                     │  not fixed)  │    │  assessment)    │
                     └──────────────┘    └─────────────────┘
                            │                     │
                            │            ┌────────▼────────┐
                            │            │ Approve (Slack) │
                            │            │ human-in-loop   │
                            │            │ for high-risk   │
                            │            └────────┬────────┘
                            │                     │
                            │            ┌────────▼────────┐
                            │            │    Execute      │
                            │            │ (guardrails     │
                            └────────────│  block danger)  │
                                         └─────────────────┘
                                                  │
                                         ┌────────▼────────┐
                                         │     Notify      │
                                         │ (Slack summary  │
                                         │  + JSON persist)│
                                         └─────────────────┘
```

---

## Quick Start

### Run locally

```bash
# Prerequisites: Go 1.23+
git clone https://github.com/lucientong/argus
cd argus
make run
# → argus listening on :8080

# Health check
curl http://localhost:8080/health
# {"status":"ok"}

# Live incident dashboard
open http://localhost:8080/dashboard

# Send a test alert
curl -X POST http://localhost:8080/webhook/grafana \
  -H 'Content-Type: application/json' \
  -d '{
    "alerts": [{
      "status": "firing",
      "labels": {"alertname":"HighCPU","severity":"critical","service":"api-server"},
      "annotations": {"summary":"CPU above 90% for 5m"},
      "startsAt": "2026-01-01T00:00:00Z"
    }]
  }'
```

### Run with Docker

```bash
docker run --rm \
  -p 8080:8080 \
  -e ARGUS_LLM_API_KEY=sk-... \
  lucientong/argus:latest
```

### Run against real infrastructure

Edit `configs/config.yaml` and set `integrations.mode: "real"`, then supply the following environment variables:

| Variable | Purpose |
|---|---|
| `ARGUS_LLM_API_KEY` | Anthropic / OpenAI API key |
| `ARGUS_LLM_FALLBACK_API_KEY` | Fallback LLM API key |
| `ARGUS_SLACK_TOKEN` | Slack bot token (`xoxb-...`) |
| `ARGUS_GRAFANA_API_KEY` | Grafana service account token |
| `ARGUS_GRAFANA_WEBHOOK_SECRET` | Grafana webhook signature secret |
| `ARGUS_PAGERDUTY_WEBHOOK_SECRET` | PagerDuty webhook signature secret |

---

## Configuration

Default config lives at `configs/config.yaml`. All secrets can stay out of the file using environment variables (see table above).

```yaml
integrations:
  mode: "mock"        # "mock" = no external deps | "real" = live HTTP clients

llm:
  provider: "anthropic"
  model: "claude-opus-4-5"
  fallback_provider: "openai"
  fallback_model: "gpt-4o"

prometheus:
  url: "http://prometheus:9090"

kubernetes:
  kubeconfig_path: ""   # empty = in-cluster service-account
  namespace: "default"

slack:
  alerts_channel: "#incidents"
  approval_channel: "#ops-approvals"
```

---

## Project Layout

```
argus/
├── cmd/argus/              # HTTP server + webhook entrypoint
├── configs/                # config.yaml
├── internal/
│   ├── agents/             # classify, diagnose, remediate, approve,
│   │                       #   execute, verify, notify
│   ├── config/             # YAML loader + env-var overrides
│   ├── integrations/       # Client interfaces + mock and real HTTP impls
│   │   ├── prometheus/     #   Prometheus HTTP API v1
│   │   ├── kubernetes/     #   K8s API (in-cluster or kubeconfig)
│   │   ├── slack/          #   Slack Web API (chat.postMessage + approvals)
│   │   ├── grafana/        #   Grafana annotations API
│   │   └── pagerduty/      #   PagerDuty Events API v2
│   ├── pipeline/           # waggle.Loop wiring (classify→…→notify)
│   ├── prompts/            # LLM prompt templates
│   ├── runbooks/           # Markdown loader + RAG ingest
│   ├── types/              # Shared structs (Alert, Diagnosis, Plan…)
│   ├── web/                # Incident store, SSE hub, dashboard HTTP server
│   └── webhook/            # Grafana + PagerDuty payload parsers
├── runbooks/               # Markdown playbooks (RAG data source)
├── Dockerfile
├── Makefile
└── k8s/                    # Kubernetes manifests (Phase 15)
```

---

## Architecture

Argus is built on **[waggle](https://github.com/lucientong/waggle)** — a typed, composable Go AI agent framework. Key primitives used:

| Waggle primitive | Used for |
|---|---|
| `agent.Func[I,O]` | Every pipeline step |
| `output.StructuredAgent` | Typed LLM outputs (JSON schema enforcement) |
| `waggle.Router` | Critical / warning / info severity dispatch |
| `waggle.Loop` | Diagnose → fix → verify retry cycle (max 5 iterations) |
| `rag.Pipeline` | Runbook semantic search (TopK=3) |
| `guardrail.WithInputExtractGuard` | Block dangerous commands pre-execution |
| `stream.ObserverFunc` | Live SSE events to the dashboard |

---

## CI / CD

| Workflow | 触发条件 | 内容 |
|---|---|---|
| **CI** (`ci.yml`) | push to `main` / PR | `go vet` + `go test -race` + Codecov 上传 |
| **Release** (`release.yml`) | push tag `v*.*.*` | 重跑测试 → 构建并推送 Docker 镜像 → 创建 GitHub Release |

打 tag 即发布：

```bash
git tag v1.0.0
git push origin v1.0.0
```

Docker Hub 镜像会自动打上 `1.0.0`、`1.0`、`latest` 三个 tag。

Required GitHub repository secrets:

| Secret | Value |
|---|---|
| `DOCKERHUB_USERNAME` | Docker Hub username |
| `DOCKERHUB_TOKEN` | Docker Hub access token |
| `CODECOV_TOKEN` | Codecov upload token |

---

## Development

```bash
# Build
make build

# Test with race detector
make test

# Lint
make lint

# Local Docker build (requires waggle sibling)
make docker-build

# Run
make run
```

### Dependency note

waggle is published on the Go module proxy. `go mod download` resolves it automatically — no manual setup required.

---

## Documentation

| File | Purpose |
|---|---|
| [`PLAN.md`](./PLAN.md) | 15-phase implementation plan |
| [`progress.md`](./progress.md) | Live progress tracker |
| [`CHANGELOG.md`](./CHANGELOG.md) | User-facing change log |
| [`WAGGLE_FEEDBACK.md`](./WAGGLE_FEEDBACK.md) | Friction log for the waggle project |

---

## License

[MIT](LICENSE)
