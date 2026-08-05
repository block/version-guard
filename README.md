# Version Guard

**Version Guard** is an open-source cloud infrastructure version monitoring system that continuously scans cloud resources (databases, caches, compute) to detect version drift and compliance issues.

## 🎯 Purpose

Version Guard helps organizations maintain infrastructure security and compliance by:
- **Proactive Detection**: Identifying resources running deprecated or end-of-life (EOL) versions before they become security risks
- **Multi-Cloud Support**: Scanning resources across AWS, GCP, and Azure through unified inventory sources
- **Cost Optimization**: Preventing expensive extended support charges (6x base price for AWS Extended Support)
- **Compliance Tracking**: Providing Red/Yellow/Green classification for compliance dashboards
- **Automation**: Continuously monitoring infrastructure without manual intervention

## 🏗️ Architecture

Version Guard implements a **two-stage detection pipeline**. Manual and scheduled triggers share the same scan pipeline; scheduled triggers go through a small Temporal launcher workflow first so Temporal's schedule-generated workflow IDs do not bypass the singleton scan guard.

```
┌──────────────────────────┐        ┌─────────────────────────────────┐
│ Manual trigger           │        │ Temporal Schedule API            │
│ POST /scan or CLI        │        │ SCHEDULE_* env vars              │
└────────────┬─────────────┘        └────────────────┬────────────────┘
             │                                       │
             │                                       ▼
             │                         ┌──────────────────────────────┐
             │                         │ ScheduledScanWorkflow        │
             │                         │ launcher only; no scanning   │
             │                         └──────────────┬───────────────┘
             │                                        │
             └────────────────────┬───────────────────┘
                                  ▼
┌────────────────────────────────────────────────────────────┐
│  SINGLETON SCAN WORKFLOW                                   │
│  workflow ID: version-guard-active-scan                    │
│  Temporal rejects a second open run with this same ID       │
└────────────────────────────┬───────────────────────────────┘
                             ▼
┌────────────────────────────────────────────────────────────┐
│  STAGE 1: DETECT                                           │
│  Fan-Out: parallel detection per resource type             │
│  Inventory (Wiz) + EOL data + classify                     │
└────────────────────────────┬───────────────────────────────┘
                             ▼
┌────────────────────────────────────────────────────────────┐
│  STAGE 2: STORE                                            │
│  s3://bucket/snapshots/YYYY/MM/DD/{snapshot-id}.json       │
│  s3://bucket/snapshots/latest.json                         │
└────────────────────────────┬───────────────────────────────┘
                             ▼
                   📤 YOUR CUSTOM EMITTERS
                 (See "Extending Version Guard")
```

**Key Components:**
- **Inventory Sources**: [Wiz](https://wiz.io) saved reports for resource discovery (multi-cloud)
- **EOL Data**: [endoflife.date](https://endoflife.date) API — no cloud provider credentials needed
- **Classification**: Red (EOL/deprecated), Yellow (extended support/approaching EOL), Green (current)
- **S3 Snapshots**: Versioned JSON storage for audit trail and downstream consumption
- **HTTP Admin API**: Trigger scans and query status

## ✨ Features

- ✅ **Multi-Cloud Inventory**: Wiz integration for AWS, GCP, Azure resource discovery
- ✅ **Open EOL Data**: All EOL data from [endoflife.date](https://endoflife.date) — no cloud provider credentials needed
- ✅ **Parallel Detection**: Temporal-based workflows for scalable scanning
- ✅ **Versioned Snapshots**: S3 storage with full audit history
- ✅ **Local Development**: Full docker-compose setup with MinIO (S3) and Temporal
- ✅ **Extensible Architecture**: Plugin your own emitters for issue tracking, dashboards, notifications

## 🤖 AI Skills for Resource Management

Version Guard includes AI agent skills that automate common tasks. No manual configuration editing required — AI agents can autonomously add and manage resources.

### add-version-guard-resource Skill

Autonomously add new cloud resource types to Version Guard using any AI agent (Claude Code, Goose, Amp, etc.).

**What it does:**
- Queries [endoflife.date](https://endoflife.date) API to validate EOL data coverage
- Auto-detects Wiz CSV schema from existing test fixtures
- Generates `pkg/config/defaults/resources.yaml` entries with proper field mappings
- Runs tests to verify configuration works
- Creates properly formatted git commits

**Quick Start:**
```bash
# With Claude Code (when in this repository)
claude "Use the add-version-guard-resource skill to add OpenSearch support"

# With Amp (when in this repository)
amp "Add OpenSearch to Version Guard"
```

**Time saved**: ~30-60 minutes per resource type reduced to 2-3 minutes of autonomous execution.

📖 **See [SKILLS.md](SKILLS.md) for comprehensive documentation**, including:
- Installation for different AI platforms
- Detailed usage examples (OpenSearch, Aurora PostgreSQL, EKS)
- Troubleshooting guide
- Creating your own skills

## 📦 Supported Resources

Version Guard uses a **config-driven approach** - resources are defined in `pkg/config/defaults/resources.yaml`:

| Resource | Inventory | EOL Source | Status |
|----------|-----------|------------|--------|
| **EKS** (Kubernetes) | Wiz | [amazon-eks](https://endoflife.date/amazon-eks) | ✅ Production tested |
| **ElastiCache** (Redis/Valkey/Memcached) | Wiz | [amazon-elasticache-redis](https://endoflife.date/amazon-elasticache-redis), [valkey](https://endoflife.date/valkey) | ✅ Production tested |
| **Aurora MySQL** | Wiz | [amazon-aurora-mysql](https://endoflife.date/amazon-aurora-mysql) | ⚠️ Production tested via the [`deploy/endoflife-override`](./deploy/endoflife-override/) shim while [endoflife.date#9534](https://github.com/endoflife-date/endoflife.date/pull/9534) is still open upstream |
| **Aurora PostgreSQL** | Wiz | [amazon-aurora-postgresql](https://endoflife.date/amazon-aurora-postgresql) | ✅ Production tested |
| **OpenSearch** | Wiz | [amazon-opensearch](https://endoflife.date/amazon-opensearch), [elasticsearch](https://endoflife.date/elasticsearch) | ✅ Production tested |
| **RDS MySQL** | Wiz | [amazon-rds-mysql](https://endoflife.date/amazon-rds-mysql) | ✅ Production tested |
| **RDS PostgreSQL** | Wiz | [amazon-rds-postgresql](https://endoflife.date/amazon-rds-postgresql) | ✅ Production tested |
| **Lambda** | Wiz | [aws-lambda](https://endoflife.date/aws-lambda) | ✅ Production tested |

**Adding a new resource type requires:**
1. A Wiz saved report for the resource type
2. Adding ~15 lines to `pkg/config/defaults/resources.yaml`
3. Adding the report ID to `WIZ_REPORT_IDS` environment variable

**No code changes needed** for any resource whose inventory shape matches one of the existing patterns. If the resource needs version/engine reshaping (extracting a value from a JSON column, normalizing engine names, deriving the engine from the version, …) declare it via the YAML transforms DSL — see [TRANSFORMS.md](./TRANSFORMS.md). See [USAGE.md](./USAGE.md) for the broader workflow.

## 🚀 Quick Start

### Prerequisites

- **Go 1.24+**
- **Docker** (for docker-compose local setup)
- **Wiz API access** (optional — falls back to mock data)

### Installation

```bash
git clone https://github.com/block/Version-Guard.git
cd Version-Guard

# Build binaries
make build-all

# Verify build
./bin/version-guard --help
./bin/version-guard-cli --help
```

### Run Locally (docker-compose)

The easiest way to run Version Guard locally. This starts Temporal, MinIO (S3-compatible storage), and the Version Guard server in one command:

```bash
# With mock inventory (no Wiz credentials needed)
docker compose up --build

# With real Wiz inventory
export WIZ_CLIENT_ID_SECRET="your-client-id"
export WIZ_CLIENT_SECRET_SECRET="your-client-secret"
export WIZ_REPORT_IDS='{
  "aurora-mysql":"your-aurora-mysql-report-id",
  "eks":"your-eks-report-id",
  "elasticache-redis":"your-elasticache-report-id",
  "elasticache-valkey":"your-elasticache-report-id",
  "elasticache-memcached":"your-elasticache-report-id",
  "opensearch":"your-opensearch-report-id",
  "lambda":"your-lambda-report-id"
}'
docker compose up --build
```

**Services started:**

| Service | Purpose | Port |
|---------|---------|------|
| `temporal` | Workflow orchestration | `7233` (gRPC), `8233` (Web UI) |
| `minio` | S3-compatible snapshot storage | `9000` (API), `9001` (Console) |
| `endoflife` | Local EOL data override (nginx) | `8082` |
| `version-guard` | The server | `8081` (HTTP admin), `9090` (OpenMetrics) |

The `endoflife` service serves patched EOL data for products with pending upstream PRs on [endoflife.date](https://endoflife.date), and proxies everything else to the live API. See [`deploy/endoflife-override/README.md`](./deploy/endoflife-override/README.md) for details on adding or updating overrides.

Once running, open the Temporal Web UI at http://localhost:8233 to trigger and monitor workflows.

Temporal SDK metrics and Version Guard application metrics are enabled by
default and exposed at http://localhost:9090/metrics. Set
`TEMPORAL_METRICS_ENABLED=false` to disable them, or set
`TEMPORAL_METRICS_LISTEN_ADDRESS` to use a different address.

The same OpenMetrics endpoint exports `temporal_*`, `version_guard_*`,
`go_*`, and `process_*` series. Datadog/BPCI scrape configuration must allow
all four families for the RCA dashboard panels to populate.

#### End-to-end with `make compose-*`

The same commands work for everyone — they auto-detect whether a webhook-style emitter is present and adjust accordingly:

```bash
make compose-e2e    # build → up → POST /scan → tail logs
make compose-down   # tear everything down
```

- **Open-source users (no emitter):** detector + Temporal + MinIO + endoflife come up. The orchestrator still posts to `EMITTER_WEBHOOK_URL`; with no listener it logs a single non-fatal failure and the snapshot still lands in MinIO. Use this to verify the DETECT → STORE pipeline.
- **Block (or anyone with a webhook emitter):** drop a sibling checkout at `../version-guard-emitter`, or set `EMITTER_PATH=/path/to/your/emitter`, and the same `make compose-e2e` brings up the emitter alongside via Compose's [`with-emitter` profile](https://docs.docker.com/compose/profiles/) and exercises the full DETECT → STORE → ACT flow.

##### Emitter integration model

Block runs an internal companion service that consumes snapshots and posts findings to its security tooling (private repo, not publicly available). The orchestrator's optional emitter webhook (`EMITTER_WEBHOOK_URL`) is the link between detector and that service. **Most open-source users don't need it** — implement an in-process emitter against the `pkg/emitters` interfaces instead (see [Extending Version Guard](#-extending-version-guard)). The webhook path is for users who prefer to keep their emitter in a separate process or repository.

### Run Locally (manual)

If you prefer running components individually:

1. **Start local Temporal server:**
```bash
make temporal
# Opens Web UI at http://localhost:8233
```

2. **Run Version Guard server** (in a separate terminal):
```bash
# With mock inventory data (no Wiz credentials needed)
make dev

# Or with real Wiz inventory (requires credentials)
export WIZ_CLIENT_ID_SECRET="your-client-id"
export WIZ_CLIENT_SECRET_SECRET="your-client-secret"
export WIZ_REPORT_IDS='{"aurora-mysql":"report-id","eks":"report-id","elasticache-redis":"report-id","elasticache-valkey":"report-id","elasticache-memcached":"report-id","opensearch":"report-id","lambda":"report-id"}'
make dev
```

### Trigger a Scan

**Via the HTTP admin endpoint (recommended):**

```bash
# Full fleet scan
curl -X POST http://localhost:8081/scan

# Targeted scan (specific resource types only)
curl -X POST http://localhost:8081/scan \
  -H 'Content-Type: application/json' \
  -d '{"resource_types":["aurora-mysql","eks"]}'
```

**Via the CLI:**

```bash
# Full fleet scan
./bin/version-guard-cli scan start

# Targeted scan, wait for completion
./bin/version-guard-cli scan start \
  --resource-type aurora-mysql --resource-type eks \
  --wait
```

**Via Temporal directly:**

```bash
# Manual scan through the singleton orchestrator workflow. Use the fixed
# workflow ID so Temporal enforces "only one active scan at a time".
docker compose exec temporal temporal workflow start \
  --workflow-id version-guard-active-scan \
  --task-queue version-guard-detection \
  --type OrchestratorWorkflow \
  --input '{"ResourceTypes":["aurora-mysql","eks"],"ScanScope":"full"}' \
  --address localhost:7233 \
  --namespace version-guard-dev

# Or via the Temporal Web UI at http://localhost:8233 → Start Workflow
```

**Monitor workflow execution:**

```bash
# Watch Version Guard logs in real-time
docker compose logs --follow version-guard

# View Temporal Web UI for detailed workflow execution
# Open http://localhost:8233 → Workflows → Select your workflow
```

**Example successful workflow output:**
```
Status: COMPLETED
Total Findings: 8,386 resources scanned
Compliance: 45.36%
Runtime: 29.35 seconds

Resource Breakdown:
- aurora: 4,257 findings
- eks: 155 findings (65 GREEN, 90 YELLOW)
- elasticache: 3,974 findings (3,739 GREEN, 138 YELLOW, 97 UNKNOWN)
```

**Verify snapshot creation:**

Snapshots are stored in MinIO (local S3) at `s3://version-guard-snapshots/snapshots/YYYY/MM/DD/{snapshot-id}.json`. For scheduled runs, the snapshot ID remains the schedule-generated run ID (for example `version-guard-scheduled-scan-2026-06-11T18:00:00Z`) even though the underlying scan workflow uses the fixed singleton ID:

```bash
# List snapshots (from logs)
docker compose logs version-guard | grep "Snapshot created"

# Access MinIO Console to browse snapshots
# Open http://localhost:9001 (default credentials: minioadmin/minioadmin)
```

### Query Findings

```bash
# Using the CLI
./bin/version-guard-cli service list
./bin/version-guard-cli finding list
```

### Run Tests

```bash
# Run all tests
make test

# Run specific package tests
go test ./pkg/workflow/detection -v
go test ./pkg/inventory/wiz -v
go test ./pkg/policy -v

# Run with coverage
make test-coverage
```

## 🔧 Configuration

Version Guard is configured via environment variables or CLI flags:

| Variable | Description | Default |
|----------|-------------|---------|
| `TEMPORAL_ENDPOINT` | Temporal server address | `localhost:7233` |
| `TEMPORAL_NAMESPACE` | Temporal namespace | `version-guard-dev` |
| `TEMPORAL_TASK_QUEUE` | Temporal task queue used by the worker | `version-guard-detection` |
| `TEMPORAL_METRICS_ENABLED` | Enable the Temporal Go SDK Prometheus/OpenMetrics endpoint | `true` |
| `TEMPORAL_METRICS_LISTEN_ADDRESS` | Prometheus/OpenMetrics listen address for Temporal SDK and application metrics | `0.0.0.0:9090` |
| `HTTP_PORT` | HTTP admin port (`POST /scan`) | `8081` |
| `S3_BUCKET` | S3 bucket for snapshots | `version-guard-snapshots` |
| `AWS_REGION` | AWS region (for S3 snapshots) | `us-west-2` |
| `WIZ_CLIENT_ID_SECRET` | Wiz client ID (optional) | - |
| `WIZ_CLIENT_SECRET_SECRET` | Wiz client secret (optional) | - |
| `WIZ_REPORT_IDS` | JSON map of resource ID to Wiz report ID (optional) | - |
| `EOL_BASE_URL` | Custom endoflife.date API base URL (optional) | `https://endoflife.date/api` |
| `CONFIG_PATH` | Path to a custom resources config file (overrides the embedded default; empty = use embedded) | _(empty)_ |
| `TAG_APP_KEYS` | Comma-separated AWS tag keys for app/service | `app,application,service` |
| `SCHEDULE_ENABLED` | Enable automatic scheduled scanning | `false` |
| `SCHEDULE_CRON` | Cron expression for scan schedule | `0 6 * * *` (daily 06:00 UTC) |
| `SCHEDULE_ID` | Temporal schedule ID (stable across restarts) | `version-guard-scan` |
| `SCHEDULE_JITTER` | Random jitter to prevent thundering herd | `5m` |
| `SNAPSHOT_STORE` | Snapshot backend: `s3` or `memory` (in-process; for laptop dev / CI smoke tests) | `s3` |
| `INVENTORY_FALLBACK` | When Wiz creds are missing: empty (skip resource and fail-fast) or `mock` (synthesize 1 fake resource per config — dev only, never set in production) | _(empty)_ |
| `EMITTER_WEBHOOK_URL` | Optional. Base URL of an out-of-process emitter that exposes `POST /trigger-act`. When set, the orchestrator workflow notifies it after each snapshot is persisted. Empty disables the webhook — Version Guard still ships findings via in-process emitters and S3. See [Extending Version Guard](#-extending-version-guard) below. | _(empty)_ |
| `--verbose` / `-v` | Enable debug-level logging | `false` |

**Custom Resource Catalog:**

Version Guard ships with a canonical `resources.yaml` embedded into the binary at build time, so the default install scans the standard catalog with no extra files. Override it by writing your own YAML and pointing `CONFIG_PATH` at it — your file fully replaces the embedded default (no merge, no overlay), so you can change resource IDs, field mappings, EOL providers, or drop resources entirely without rebuilding:

```bash
# Use the embedded default (no CONFIG_PATH set)
./version-guard

# Ship a custom catalog
./version-guard --config-path /etc/version-guard/my-resources.yaml
# or
CONFIG_PATH=/etc/version-guard/my-resources.yaml ./version-guard
```

Use `pkg/config/defaults/resources.yaml` as a starting template — copy it, edit, and bind-mount the file into your container. For per-resource version/engine reshaping (JSON extraction, prefix stripping, engine normalization, version-derived engine), see the dedicated [TRANSFORMS.md](./TRANSFORMS.md) — it covers the whole DSL, when to use each operation, and when not to use a transform at all.

**Scheduled Scanning:**

Version Guard can automatically run scans on a cron schedule using the Temporal Schedule API. Disabled by default — enable with `SCHEDULE_ENABLED=true`:

```bash
# Enable daily scans at 06:00 UTC (default)
export SCHEDULE_ENABLED=true

# Or customize the schedule
export SCHEDULE_ENABLED=true
export SCHEDULE_CRON="*/30 * * * *"  # Every 30 minutes
export SCHEDULE_JITTER="2m"
```

The public configuration has not changed: `SCHEDULE_ENABLED`, `SCHEDULE_CRON`, `SCHEDULE_ID`, `SCHEDULE_JITTER`, and `TEMPORAL_TASK_QUEUE` are still the only knobs. The schedule uses a create-or-update pattern — safe to restart the server without creating duplicate schedules. If the cron expression changes, the existing schedule is updated automatically.

Under the hood, the schedule starts `ScheduledScanWorkflow`. Temporal schedules may append the scheduled fire time to that workflow ID (for example `version-guard-scheduled-scan-2026-06-11T08:00:00Z`). That launcher then starts the real scan as child workflow ID `version-guard-active-scan`. Manual scans use the same fixed `version-guard-active-scan` ID directly. Because both paths converge on the same fixed workflow ID, Temporal rejects overlapping scans while still allowing each completed daily schedule to create a unique snapshot.

In short:

- **Config stays the same** — no new env vars, chart values, ports, task queues, or schedule IDs.
- **Manual scans stay the same** — `POST /scan` and the CLI start `version-guard-active-scan`.
- **Scheduled scans keep unique snapshot IDs** — the scheduled launcher ID is passed through as `ScanID`.
- **Only one collector scan can run at a time** — scheduled and manual triggers both contend on `version-guard-active-scan`.

```bash
# Verify the schedule
temporal schedule list --namespace version-guard-dev
temporal schedule describe --schedule-id version-guard-scan --namespace version-guard-dev
```

**Customizing AWS Tag Keys:**

Version Guard extracts the service name from AWS resource tags. By default, it looks for tags like `app`, `application`, or `service`. You can customize these to match your organization's tagging conventions:

```bash
# Example: Your organization uses "team" for service attribution
export TAG_APP_KEYS="team,squad,application"
```

The tag keys are tried in order — the first matching tag wins.

**Wiz Report IDs:**

Version Guard uses a single JSON map to configure all Wiz report IDs:

```bash
export WIZ_REPORT_IDS='{
  "aurora-mysql": "your-aurora-mysql-report-id",
  "eks": "your-eks-report-id",
  "elasticache-redis": "your-elasticache-report-id",
  "elasticache-valkey": "your-elasticache-report-id",
  "elasticache-memcached": "your-elasticache-report-id",
  "opensearch": "your-opensearch-report-id",
  "lambda": "your-lambda-report-id"
}'
```

The keys correspond to resource IDs in `pkg/config/defaults/resources.yaml`. This approach:
- ✅ Scales to dozens of resources without env var sprawl
- ✅ Single environment variable to manage
- ✅ Easy to add new resources (just add to JSON map)

At scan time, Version Guard verifies each configured report's identity, completed
run status, schedule-based freshness, expected row count, and required CSV
columns. A header-only CSV is accepted only when Wiz reports zero results; stale,
truncated, or schema-incompatible output fails that resource's inventory fetch.

**Logging:**

Version Guard uses structured JSON logging via Go's `log/slog` package for production observability:

```bash
# Run with debug-level logging
./bin/version-guard --verbose

# Production mode (info-level logging only)
./bin/version-guard
```

Logs are output in JSON format for easy parsing by log aggregation tools (Datadog, Splunk, CloudWatch Insights):

```json
{
  "time": "2024-01-15T10:30:45Z",
  "level": "WARN",
  "msg": "failed to detect drift for resource",
  "resource_id": "arn:aws:rds:us-west-2:123456789012:cluster:my-db",
  "error": "version not found in EOL database"
}
```

Benefits:
- Machine-readable structured data with typed fields
- Context-aware logging with trace IDs
- Queryable logs (e.g., filter by `resource_id` or `error`)
- Integrates seamlessly with observability platforms

See `./bin/version-guard --help` for all options.

## 🎨 Classification Policy

| Status | Criteria | Typical Action |
|--------|----------|----------------|
| 🔴 **RED** | Past EOL, deprecated, extended support expired | Urgent upgrade required |
| 🟡 **YELLOW** | In extended support (costly), approaching EOL (< 90 days) | Plan upgrade soon |
| 🟢 **GREEN** | In standard support, current version | Compliant |
| ⚪ **UNKNOWN** | Version not found in EOL database | Investigate |

## 🔌 Extending Version Guard

Version Guard provides **interfaces for custom emitters** so you can integrate with your own systems:

### 1. Implementing Custom Emitters

See `pkg/emitters/emitters.go` for interface definitions:

```go
type IssueTrackerEmitter interface {
    Emit(ctx context.Context, snapshotID string, findings []*types.Finding) (*IssueTrackerResult, error)
}

type DashboardEmitter interface {
    Emit(ctx context.Context, snapshotID string, summary *types.SnapshotSummary) (*DashboardResult, error)
}
```

**Example implementations:**
- `pkg/emitters/examples/logging_emitter.go` - Logs findings to stdout (included)
- **Your custom emitter** - Send findings to Jira, ServiceNow, Slack, PagerDuty, etc.

### 2. Out-of-process Emitter via Webhook (Optional)

For users who already run a separate service that consumes snapshots (e.g. a long-running worker that writes to a different system), Version Guard can **notify** that service every time a snapshot is persisted, instead of (or in addition to) calling in-process emitters. Set `EMITTER_WEBHOOK_URL=https://your-emitter.example.com` and the orchestrator workflow will:

1. POST `{"snapshot_id": "<id>"}` to `<EMITTER_WEBHOOK_URL>/trigger-act`.
2. Expect a `2xx` response (the body is logged but not required to follow any schema).
3. Treat any failure as **non-fatal** — the snapshot is already durable in your snapshot store, and Temporal's retry policy will handle transient errors.

**You build the receiver.** Any HTTP server that handles `POST /trigger-act` works. Block runs an internal companion service for this (private repo, not publicly available) — for OSS, a 30-line Go/Python/Node handler that starts your own workflow / job is enough. Replying with `2xx` is the only contract.

**When to choose this vs. in-process emitters:**

| You want… | Use |
|---|---|
| A pluggable callback inside the detector pod (logging, Slack, Jira, simple webhooks) | In-process emitter via `pkg/emitters` (see §1 above) |
| A separate long-running service with its own deployment cadence, scaling, or runtime | Out-of-process webhook emitter |
| Both | Set `EMITTER_WEBHOOK_URL` AND register an in-process emitter — they run independently |
| Neither (just consume snapshots out-of-band) | Skip both; read the JSON from S3 (see §3 below) |

### 3. Consuming S3 Snapshots

Snapshots are stored as JSON in S3:
```
s3://your-bucket/snapshots/YYYY/MM/DD/{snapshot-id}.json
s3://your-bucket/snapshots/latest.json
```

**Snapshot Schema:**

Snapshots are produced via Go's `encoding/json` defaults. Top-level fields on
`Snapshot` and `SnapshotSummary` carry explicit `snake_case` tags (see
[pkg/types/snapshot.go](./pkg/types/snapshot.go)); most per-`Finding` fields
serialize as PascalCase. Required `eol` is the intentionally snake_case
exception for downstream enrichment metadata and includes every support boundary
the EOL provider exposes, plus metadata dates such as `release_date`,
`latest_release_date`, and date-valued `lts_date`. `eol.actionable_date` is the
first lifecycle date used for yellow warning windows. The `findings_by_type` map is keyed
by the resource config ID (e.g. `aurora-mysql`, `eks`), not by the `ResourceType`
constants used in tests.

```json
{
  "snapshot_id": "scan-2026-04-09-123456",
  "version": "v4",
  "generated_at": "2026-04-09T12:34:56Z",
  "scan_start_time": "2026-04-09T12:00:00Z",
  "scan_end_time": "2026-04-09T12:34:56Z",
  "scan_duration_sec": 2096,
  "findings_by_type": {
    "aurora-mysql": [
      {
        "ResourceID": "db-cluster-1",
        "ResourceType": "aurora-mysql",
        "CloudProvider": "aws",
        "CurrentVersion": "5.6.10a",
        "Engine": "aurora-mysql",
        "Status": "RED",
        "Message": "Running deprecated version 5.6.10a (EOL: 2024-02-29)",
        "eol": {
          "standard_support_end": "2024-02-29T00:00:00Z",
          "extended_support_end": "2027-02-28T00:00:00Z",
          "eol_date": "2027-02-28T00:00:00Z",
          "actionable_date": "2024-02-29T00:00:00Z",
          "release_date": "2016-02-22T00:00:00Z",
          "latest_release_date": "2025-02-13T00:00:00Z",
          "version": "5.7",
          "engine": "mysql",
          "source": "endoflife-date-api",
          "is_supported": true,
          "is_deprecated": true,
          "is_extended_support": true,
          "is_eol": false
        },
        "DetectedAt": "2026-04-09T12:34:56Z",
        "UpdatedAt": "2026-04-09T12:34:56Z"
      }
    ]
  },
  "summary": {
    "total_resources": 150,
    "red_count": 12,
    "yellow_count": 35,
    "green_count": 103,
    "unknown_count": 0,
    "compliance_percentage": 68.7
  }
}
```

**Consume snapshots with:**
- AWS Lambda triggered on S3 events
- Scheduled cron job reading `latest.json`
- Custom Temporal workflow (implement your own follow-up workflow)

## 📖 Documentation

- [ARCHITECTURE.md](./ARCHITECTURE.md) - Detailed system architecture
- [CONTRIBUTING.md](./CONTRIBUTING.md) - How to contribute
- [pkg/workflow/detection/](./pkg/workflow/detection/) - Per-resource detection activities (inventory → EOL → classify) driven by `resources.yaml`

## 🤝 Contributing

Contributions are welcome! Please see [CONTRIBUTING.md](./CONTRIBUTING.md) for:
- Code of conduct
- Development setup
- Testing guidelines
- Pull request process

## 📜 License

This project is licensed under the Apache License 2.0 - see the [LICENSE](./LICENSE) file for details.

## 🐛 Issues & Support

- **Bug reports**: [GitHub Issues](https://github.com/block/Version-Guard/issues)
- **Feature requests**: [GitHub Discussions](https://github.com/block/Version-Guard/discussions)
- **Security issues**: Please report privately via GitHub's [security advisory form](https://github.com/block/Version-Guard/security/advisories/new) (do not open public issues)

## 🙏 Acknowledgments

Version Guard is maintained by the open-source community.

Special thanks to:
- [Temporal](https://temporal.io) for the workflow orchestration framework
- [Wiz](https://wiz.io) for multi-cloud security scanning
- [endoflife.date](https://endoflife.date) for open EOL data

---

**Note**: Version Guard is designed as a **collector/detector** system. The emission of findings to issue trackers, dashboards, or notification systems is left to implementers. See "Extending Version Guard" above for integration patterns.
