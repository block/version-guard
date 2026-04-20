# Version Guard

> ⚠️ **Work in Progress / Experimental** — Version Guard is under active development. APIs, configuration formats, and behavior may change without notice. Use at your own risk in production environments.

**Version Guard** is an open-source cloud infrastructure version monitoring system that continuously scans cloud resources (databases, caches, compute) to detect version drift and compliance issues.

## 🎯 Purpose

Version Guard helps organizations maintain infrastructure security and compliance by:
- **Proactive Detection**: Identifying resources running deprecated or end-of-life (EOL) versions before they become security risks
- **Multi-Cloud Support**: Scanning resources across AWS, GCP, and Azure through unified inventory sources
- **Cost Optimization**: Preventing expensive extended support charges (6x base price for AWS Extended Support)
- **Compliance Tracking**: Providing Red/Yellow/Green classification for compliance dashboards
- **Automation**: Continuously monitoring infrastructure without manual intervention

## 🏗️ Architecture

Version Guard implements a **two-stage detection pipeline**:

```
┌────────────────────────────────────────────────────────────┐
│  STAGE 1: DETECT (Temporal Workflow)                       │
│                                                            │
│   Fan-Out: Parallel Detection per Resource Type            │
│   ┌───────┐  ┌─────────┐  ┌───────┐                        │
│   │Aurora │  │   EKS   │  │ More  │  ...                   │
│   └───┬───┘  └────┬────┘  └───┬───┘                        │
│       └───────────┼───────────┘                            │
│                   ▼                                        │
│   Inventory (Wiz) + EOL Data + Classify                    │
│                   │                                        │
└───────────────────┼────────────────────────────────────────┘
                    │
┌───────────────────┼────────────────────────────────────────┐
│  STAGE 2: STORE                                            │
│                   ▼                                        │
│   Create Versioned JSON Snapshot                           │
│                   │                                        │
│   s3://bucket/snapshots/YYYY/MM/DD/{snapshot-id}.json      │
│   s3://bucket/snapshots/latest.json                        │
│                                                            │
└────────────────────────────────────────────────────────────┘
                    │
                    ▼
          📤 YOUR CUSTOM EMITTERS
        (See "Extending Version Guard")
```

**Key Components:**
- **Inventory Sources**: [Wiz](https://wiz.io) saved reports for resource discovery (multi-cloud)
- **EOL Data**: [endoflife.date](https://endoflife.date) API — no cloud provider credentials needed
- **Classification**: Red (EOL/deprecated), Yellow (extended support/approaching EOL), Green (current)
- **S3 Snapshots**: Versioned JSON storage for audit trail and downstream consumption
- **gRPC API**: Query interface for compliance dashboards

## ✨ Features

- ✅ **Multi-Cloud Inventory**: Wiz integration for AWS, GCP, Azure resource discovery
- ✅ **Open EOL Data**: All EOL data from [endoflife.date](https://endoflife.date) — no cloud provider credentials needed
- ✅ **Parallel Detection**: Temporal-based workflows for scalable scanning
- ✅ **Versioned Snapshots**: S3 storage with full audit history
- ✅ **Local Development**: Full docker-compose setup with MinIO (S3) and Temporal
- ✅ **Extensible Architecture**: Plugin your own emitters for issue tracking, dashboards, notifications

## 📦 Supported Resources

Version Guard uses a **config-driven approach** - resources are defined in `config/resources.yaml`:

| Resource | Inventory | EOL Source | Status |
|----------|-----------|------------|--------|
| **EKS** (Kubernetes) | Wiz | [amazon-eks](https://endoflife.date/amazon-eks) | ✅ Production tested |
| **ElastiCache** (Redis/Valkey/Memcached) | Wiz | [amazon-elasticache-redis](https://endoflife.date/amazon-elasticache-redis), [valkey](https://endoflife.date/valkey) | ✅ Production tested |
| **Aurora MySQL** | Wiz | [amazon-aurora-mysql](https://endoflife.date/amazon-aurora-mysql) | ⚠️ Production tested, EOL data pending [endoflife.date#9534](https://github.com/endoflife-date/endoflife.date/pull/9534) |
| **Aurora PostgreSQL** | Wiz | [amazon-aurora-postgresql](https://endoflife.date/amazon-aurora-postgresql) | 🔜 Config ready, needs Wiz report ID |
| **OpenSearch** | Wiz | [amazon-opensearch](https://endoflife.date/amazon-opensearch), [elasticsearch](https://endoflife.date/elasticsearch) | ✅ Production tested |
| **RDS MySQL** | — | [amazon-rds-mysql](https://endoflife.date/amazon-rds-mysql) | 📋 Planned (add to config) |
| **RDS PostgreSQL** | — | [amazon-rds-postgresql](https://endoflife.date/amazon-rds-postgresql) | 📋 Planned (add to config) |
| **Lambda** | — | [aws-lambda](https://endoflife.date/aws-lambda) | 📋 Planned (add to config) |

**Adding a new resource type requires:**
1. A Wiz saved report for the resource type
2. Adding ~15 lines to `config/resources.yaml`
3. Adding the report ID to `WIZ_REPORT_IDS` environment variable

**No code changes needed!** See [USAGE.md](./USAGE.md) for details.

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
  "elasticache-redis":"your-elasticache-report-id"
}'
docker compose up --build
```

**Services started:**

| Service | Purpose | Port |
|---------|---------|------|
| `temporal` | Workflow orchestration | `7233` (gRPC), `8233` (Web UI) |
| `minio` | S3-compatible snapshot storage | `9000` (API), `9001` (Console) |
| `endoflife` | Local EOL data override (nginx) | `8082` |
| `version-guard` | The server | `8080` (gRPC), `8081` (HTTP admin) |

The `endoflife` service serves patched EOL data for products with pending upstream PRs on [endoflife.date](https://endoflife.date), and proxies everything else to the live API. See [`deploy/endoflife-override/README.md`](./deploy/endoflife-override/README.md) for details on adding or updating overrides.

Once running, open the Temporal Web UI at http://localhost:8233 to trigger and monitor workflows.

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
export WIZ_REPORT_IDS='{"aurora-mysql":"report-id","eks":"report-id","elasticache-redis":"report-id"}'
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
# Temporal CLI (from inside the temporal container if using docker-compose)
docker compose exec temporal temporal workflow start \
  --task-queue version-guard-detection \
  --type OrchestratorWorkflow \
  --input '{}' \
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

Snapshots are stored in MinIO (local S3) at `s3://version-guard-snapshots/snapshots/YYYY/MM/DD/{workflow-id}.json`:

```bash
# List snapshots (from logs)
docker compose logs version-guard | grep "Snapshot created"

# Access MinIO Console to browse snapshots
# Open http://localhost:9001 (default credentials: minioadmin/minioadmin)
```

### Query Findings

```bash
# Using gRPC
grpcurl -plaintext localhost:8080 list
grpcurl -plaintext localhost:8080 \
  block.versionguard.VersionGuard/GetFleetSummary

# Using the CLI
./bin/version-guard-cli service list
./bin/version-guard-cli finding list
```

### Run Tests

```bash
# Run all tests
make test

# Run specific package tests
go test ./pkg/detector/generic -v
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
| `GRPC_PORT` | gRPC service port | `8080` |
| `HTTP_PORT` | HTTP admin port (`POST /scan`) | `8081` |
| `S3_BUCKET` | S3 bucket for snapshots | `version-guard-snapshots` |
| `AWS_REGION` | AWS region (for S3 snapshots) | `us-west-2` |
| `WIZ_CLIENT_ID_SECRET` | Wiz client ID (optional) | - |
| `WIZ_CLIENT_SECRET_SECRET` | Wiz client secret (optional) | - |
| `WIZ_REPORT_IDS` | JSON map of resource ID to Wiz report ID (optional) | - |
| `EOL_BASE_URL` | Custom endoflife.date API base URL (optional) | `https://endoflife.date/api` |
| `CONFIG_PATH` | Path to resources config file | `config/resources.yaml` |
| `TAG_APP_KEYS` | Comma-separated AWS tag keys for app/service | `app,application,service` |
| `TAG_ENV_KEYS` | Comma-separated AWS tag keys for environment | `environment,env` |
| `TAG_BRAND_KEYS` | Comma-separated AWS tag keys for brand/business unit | `brand` |
| `SCHEDULE_ENABLED` | Enable automatic scheduled scanning | `false` |
| `SCHEDULE_CRON` | Cron expression for scan schedule | `0 6 * * *` (daily 06:00 UTC) |
| `SCHEDULE_ID` | Temporal schedule ID (stable across restarts) | `version-guard-scan` |
| `SCHEDULE_JITTER` | Random jitter to prevent thundering herd | `5m` |
| `--verbose` / `-v` | Enable debug-level logging | `false` |

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

The schedule uses a create-or-update pattern — safe to restart the server without creating duplicate schedules. If the cron expression changes, the existing schedule is updated automatically.

```bash
# Verify the schedule
temporal schedule list --namespace version-guard-dev
temporal schedule describe --schedule-id version-guard-scan --namespace version-guard-dev
```

**Customizing AWS Tag Keys:**

Version Guard extracts metadata (service name, environment, brand) from AWS resource tags. By default, it looks for tags like `app`, `application`, or `service`. You can customize these to match your organization's tagging conventions:

```bash
# Example: Your organization uses "cost-center" instead of "brand"
export TAG_BRAND_KEYS="cost-center,department,business-unit"

# Example: Your organization uses "team" for service attribution
export TAG_APP_KEYS="team,squad,application"
```

The tag keys are tried in order — the first matching tag wins.

**Wiz Report IDs:**

Version Guard uses a single JSON map to configure all Wiz report IDs:

```bash
export WIZ_REPORT_IDS='{
  "aurora-mysql": "7bac4838-cf54-46c4-93a2-f63cced1735a",
  "eks": "ea80a1a4-fd1d-4c8c-9a69-0726f626040b",
  "elasticache-redis": "d8084ea6-cdcc-4ee7-bb46-067b52982c11"
}'
```

The keys correspond to resource IDs in `config/resources.yaml`. This approach:
- ✅ Scales to dozens of resources without env var sprawl
- ✅ Single environment variable to manage
- ✅ Easy to add new resources (just add to JSON map)

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

### 2. Consuming S3 Snapshots

Snapshots are stored as JSON in S3:
```
s3://your-bucket/snapshots/YYYY/MM/DD/{snapshot-id}.json
s3://your-bucket/snapshots/latest.json
```

**Snapshot Schema:**
```json
{
  "snapshot_id": "scan-2026-04-09-123456",
  "version": "v1",
  "generated_at": "2026-04-09T12:34:56Z",
  "findings_by_type": {
    "aurora": [
      {
        "resource_id": "db-cluster-1",
        "status": "red",
        "message": "Running deprecated version 13.3 (EOL: 2025-03-01)",
        "recommendation": "Upgrade to version 15.5 or later"
      }
    ]
  },
  "summary": {
    "total_resources": 150,
    "red_count": 12,
    "yellow_count": 35,
    "green_count": 103,
    "compliance_percentage": 68.7
  }
}
```

**Consume snapshots with:**
- AWS Lambda triggered on S3 events
- Scheduled cron job reading `latest.json`
- Custom Temporal workflow (implement `Stage 3: ACT`)

### 3. Using the gRPC API

Version Guard exposes a gRPC API for querying compliance data:

```bash
# List services and their compliance scores
grpcurl -plaintext localhost:8080 \
  block.versionguard.VersionGuard/GetFleetSummary

# Get specific service score
grpcurl -plaintext -d '{"service":"my-service"}' \
  localhost:8080 block.versionguard.VersionGuard/GetServiceScore

# List all RED/YELLOW findings
grpcurl -plaintext -d '{"status":"red"}' \
  localhost:8080 block.versionguard.VersionGuard/ListFindings
```

## 📖 Documentation

- [ARCHITECTURE.md](./ARCHITECTURE.md) - Detailed system architecture
- [CONTRIBUTING.md](./CONTRIBUTING.md) - How to contribute
- [pkg/detector/](./pkg/detector/) - Detector implementation examples

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
- **Security issues**: Please email security@block.xyz (do not open public issues)

## 🙏 Acknowledgments

Version Guard is maintained by Block, Inc. and the open-source community.

Special thanks to:
- [Temporal](https://temporal.io) for the workflow orchestration framework
- [Wiz](https://wiz.io) for multi-cloud security scanning
- [endoflife.date](https://endoflife.date) for open EOL data

---

**Note**: Version Guard is designed as a **collector/detector** system. The emission of findings to issue trackers, dashboards, or notification systems is left to implementers. See "Extending Version Guard" above for integration patterns.
