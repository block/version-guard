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
- **Inventory Sources**: Wiz (multi-cloud scanning), mock sources for testing
- **EOL Providers**: AWS APIs (RDS, EKS) + endoflife.date (fallback)
- **Detectors**: Resource-specific detection logic (Aurora, EKS currently implemented)
- **Classification**: Red (EOL/deprecated), Yellow (extended support/approaching EOL), Green (current)
- **S3 Snapshots**: Versioned JSON storage for audit trail and downstream consumption
- **gRPC API**: Query interface for compliance dashboards

## ✨ Features

- ✅ **Multi-Cloud Inventory**: Wiz integration for AWS, GCP, Azure resource discovery
- ✅ **Hybrid EOL Data**: AWS native APIs + endoflife.date for comprehensive coverage
- ✅ **Parallel Detection**: Temporal-based workflows for scalable scanning
- ✅ **Versioned Snapshots**: S3 storage with full audit history
- ✅ **gRPC Query API**: 3 endpoints for compliance scoring, finding details, fleet summaries
- ✅ **Extensible Architecture**: Plugin your own emitters for issue tracking, dashboards, notifications

## 📦 Supported Resources

Currently implemented:
- **Aurora** (RDS MySQL/PostgreSQL) - AWS RDS EOL API + Wiz inventory
- **EKS** (Kubernetes) - AWS EKS API + endoflife.date (hybrid) + Wiz inventory

Easily extensible to:
- ElastiCache (Redis/Valkey/Memcached)
- OpenSearch
- Lambda (Node.js, Python, Java)
- Cloud SQL (GCP)
- GKE (GCP)
- Azure resources

## 🚀 Quick Start

### Prerequisites

- **Go 1.24+**
- **Docker** (for local Temporal server)
- **AWS credentials** (for EOL APIs - optional but recommended)
- **Wiz API access** (optional - falls back to mock data)

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

### Run Locally

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
export WIZ_AURORA_REPORT_ID="your-report-id"
make dev
```

3. **Trigger a scan:**
```bash
# Via Temporal CLI
temporal workflow start \
  --task-queue version-guard-detection \
  --type VersionGuardOrchestratorWorkflow \
  --input '{}'

# Via Temporal Web UI
# Navigate to http://localhost:8233 → Start Workflow
```

4. **Query findings:**
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
go test ./pkg/detector/aurora -v
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
| `S3_BUCKET` | S3 bucket for snapshots | `version-guard-snapshots` |
| `AWS_REGION` | AWS region for EOL APIs | `us-west-2` |
| `WIZ_CLIENT_ID_SECRET` | Wiz client ID (optional) | - |
| `WIZ_CLIENT_SECRET_SECRET` | Wiz client secret (optional) | - |
| `TAG_APP_KEYS` | Comma-separated AWS tag keys for app/service | `app,application,service` |
| `TAG_ENV_KEYS` | Comma-separated AWS tag keys for environment | `environment,env` |
| `TAG_BRAND_KEYS` | Comma-separated AWS tag keys for brand/business unit | `brand` |

**Customizing AWS Tag Keys:**

Version Guard extracts metadata (service name, environment, brand) from AWS resource tags. By default, it looks for tags like `app`, `application`, or `service`. You can customize these to match your organization's tagging conventions:

```bash
# Example: Your organization uses "cost-center" instead of "brand"
export TAG_BRAND_KEYS="cost-center,department,business-unit"

# Example: Your organization uses "team" for service attribution
export TAG_APP_KEYS="team,squad,application"
```

The tag keys are tried in order — the first matching tag wins.

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
- [endoflife.date](https://endoflife.date) for EOL data API
- AWS for native EOL APIs (RDS, EKS)

---

**Note**: Version Guard is designed as a **collector/detector** system. The emission of findings to issue trackers, dashboards, or notification systems is left to implementers. See "Extending Version Guard" above for integration patterns.
