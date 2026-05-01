# Version Guard - Architecture Documentation

## 📊 Implementation Status

**Production-Tested Resources** (config-driven, zero code changes needed):
- ✅ **Aurora MySQL** - Production tested (config ready, awaiting endoflife.date data)
- ✅ **Aurora PostgreSQL** - Config ready, requires separate Wiz report ID
- ✅ **EKS** - Production tested (policy classification working)
- ✅ **ElastiCache (Redis/Valkey/Memcached)** - Production tested

**Planned Resources** (add ~15 lines to `pkg/config/defaults/resources.yaml`):
- ✅ **OpenSearch** - Production tested (auto-detects legacy Elasticsearch versions)
- 📋 RDS MySQL/PostgreSQL
- 📋 Lambda runtimes

---

## Executive Summary

**Version Guard** is an open-source, Temporal-based service for continuous **multi-cloud** infrastructure version drift detection. It provides a pluggable collector/detector framework, starting with **AWS** resources and designed for extensibility to **GCP, Azure**, and other cloud platforms.

**Key Architecture Principles:**
- **Multi-cloud by design**: Cloud provider abstraction layer (AWS first, GCP/Azure ready)
- **Pluggable inventory sources**: Wiz (multi-cloud scanning) + cloud-native APIs + custom sources
- **Pluggable EOL providers**: endoflife.date today (per-product schema adapters); cloud-native EOL APIs can be added behind the same interface
- **Single responsibility principle**: Each component has one clear purpose
- **Interface-based design**: Easy to test, extend, and customize
- **Extensible storage**: In-memory (included) → SQL database (your implementation)
- **HTTP Admin API**: Trigger scans via `POST /scan`

---

## Multi-Cloud Strategy

**Vision:** Version Guard is a **cloud-agnostic** version drift detection platform supporting multiple cloud providers.

### Phase 1 (Implemented): AWS
- **Resources**: ✅ Aurora MySQL (production tested), ✅ Aurora PostgreSQL (config ready), ✅ EKS (production tested), ✅ ElastiCache (production tested), ✅ OpenSearch (production tested), 📋 RDS, 📋 Lambda
- **Inventory**: Wiz saved reports (primary) + Custom sources (extensible)
- **EOL Data**: endoflife.date API (404 graceful degradation for products not yet listed)

**Architecture Impact:**
- All resource types include `CloudProvider` field (AWS, GCP, Azure, etc.)
- Inventory sources are cloud-specific but share a common interface
- EOL providers are product-specific (endoflife.date today) but share a common interface
- The detection pipeline is generic; per-resource behavior is declared in YAML, not in code
- HTTP Admin API is cloud-agnostic (triggers scans across all providers)

---

## Config-Driven Architecture

**Key Innovation:** Version Guard uses a **declarative YAML configuration** approach that eliminates the need for custom code when adding new cloud resource types.

### Benefits

1. **Zero Code Changes**: Add resources by editing `pkg/config/defaults/resources.yaml` only
2. **Reduced Duplication**: Single generic detection pipeline + generic Wiz inventory source
3. **Better Testing**: Comprehensive test coverage on generic components
4. **Single Source of Truth**: All resource definitions in one place
5. **Scalable Configuration**: Single `WIZ_REPORT_IDS` JSON map for all resources
6. **Multi-Cloud Ready**: AWS/GCP/Azure support built-in
7. **Schema Flexibility**: Adapter pattern handles different EOL provider semantics

### How It Works

```yaml
# pkg/config/defaults/resources.yaml
resources:
  - id: eks                          # Unique identifier
    type: eks                        # Resource type
    cloud_provider: aws              # Cloud provider (aws, gcp, azure)
    inventory:
      source: wiz                    # Inventory source
      native_type_pattern: "cluster" # Wiz nativeType filter (supports wildcards)
      # required_mappings: every entry MUST be present and non-empty.
      # Validated at config load time; missing entries fail startup.
      required_mappings:
        # For EKS, externalId is a Wiz-internal hash; providerUniqueId is the ARN.
        resource_id: "providerUniqueId"
        version: "versionDetails.version"
        # No engine column; produced by transforms.engine below.
      # field_mappings: optional. Typed keys (tags) populate the typed
      # Resource; everything else lands in Resource.Extra under its YAML key.
      field_mappings:
        name: "name"
        account_id: "cloudAccount.externalId"
        region: "region"
        tags: "tags"
    transforms:
      # Per-resource version/engine reshaping declared in YAML —
      # zero Go changes for a new resource that fits any existing op.
      # Full reference: TRANSFORMS.md.
      engine:
        default_if_empty: "eks"
    eol:
      provider: endoflife-date       # EOL data provider
      product: amazon-eks            # endoflife.date product ID
      schema: declarative            # YAML-defined lifecycle semantics
      lifecycle:
        deprecation_date:
          field: eol
        extended_support_end:
          field: extendedSupport
          bool_true_fallback: eol
        deprecated_window: extended_support
        past_extended_support: unsupported
```

**Environment Variable:**
```bash
export WIZ_REPORT_IDS='{
  "eks": "your-eks-report-id"
}'
```

**Result:** At startup, the server walks the YAML config and registers per-resource inventory sources and EOL providers in keyed maps. The generic detection activities (`FetchInventory`, `FetchEOLData`, `DetectDrift`) then dispatch against those maps at scan time.

---

## Architecture Overview

```
┌─────────────────────────────────────────────────────────────┐
│                 Version Guard Detector Service              │
│            (Temporal Workflow + HTTP Admin)                 │
└─────────────────────────────────────────────────────────────┘

                    ┌────────────────────┐
                    │  Temporal Workflow │
                    │  (Periodic Scan)   │
                    └────────┬───────────┘
                             │
        ┌────────────────────┼────────────────────┐
        ▼                    ▼                    ▼
  ┌──────────┐       ┌──────────┐       ┌──────────┐
  │Inventory │       │   EOL    │       │ Policy & │
  │  Layer   │       │  Layer   │       │Classifier│
  └──────────┘       └──────────┘       └──────────┘
  │ Wiz      │       │ endoflife│       │ Red/     │
  │ Custom   │       │ (per     │       │ Yellow/  │
  │          │       │  product)│       │ Green    │
  └──────────┘       └──────────┘       └──────────┘
        │                    │                │
        └────────────────────┼────────────────┘
                             ▼
                ┌────────────────────────┐
                │ Detection Activities   │
                │ (FetchInventory →      │
                │  FetchEOLData →        │
                │  DetectDrift)          │
                │ dispatched per         │
                │ resource by orchestr.  │
                └──────────┬─────────────┘
                           ▼
                    ┌──────────────┐
                    │    Store     │
                    │  (Memory/SQL)│
                    └──────┬───────┘
                           │
                    ┌──────▼───────┐
                    │  HTTP Admin  │
                    │ (POST /scan) │
                    └──────────────┘
```

**Data Flow:**
1. **Temporal Workflow** executes on schedule (configurable interval)
2. **FetchInventory**: Wiz or custom source → resource list with versions
3. **FetchEOL**: endoflife.date → version lifecycle data
4. **DetectDrift**: Apply policy → classify Red/Yellow/Green
5. **Store**: Save findings to storage backend
6. **S3 Snapshot**: Create versioned JSON snapshot (optional)
7. **HTTP Admin**: Trigger scans via `POST /scan`

---

## Repository Structure

```
Version-Guard/
├── cmd/
│   ├── server/main.go                    # Server with Temporal worker + HTTP admin
│   └── cli/main.go                       # CLI tool for operators
│
├── config/
│   └── resources.yaml                    # Config-driven resource definitions
│
├── pkg/
│   ├── types/
│   │   ├── resource.go                   # Core types: Resource, Finding
│   │   ├── status.go                     # Status enum (Red/Yellow/Green)
│   │   └── cloud.go                      # CloudProvider enum
│   │
│   ├── config/
│   │   ├── types.go                      # Configuration schema
│   │   └── loader.go                     # Config loader and validator
│   │
│   ├── inventory/
│   │   ├── inventory.go                  # InventorySource interface
│   │   ├── wiz/                          # Wiz implementation (multi-cloud)
│   │   │   ├── generic.go                # Generic config-driven inventory source
│   │   │   ├── client.go                 # Wiz HTTP client
│   │   │   └── helpers.go                # CSV parsing, tag extraction
│   │   └── mock/                         # Mock for tests
│   │
│   ├── eol/
│   │   ├── provider.go                   # EOLProvider interface
│   │   ├── endoflife/
│   │   │   ├── client.go                 # endoflife.date HTTP client
│   │   │   ├── provider.go               # endoflife.date provider
│   │   │   ├── adapters.go               # Schema adapters (standard, EKS)
│   │   │   └── ADAPTERS.md               # Why EKS needs its own adapter (gotcha doc)
│   │   └── mock/                         # Mock for tests
│   │
│   ├── policy/
│   │   ├── policy.go                     # VersionPolicy interface
│   │   └── default.go                    # Default Red/Yellow/Green policy
│   │
│   ├── store/
│   │   ├── store.go                      # Store interface
│   │   └── memory/
│   │       └── store.go                  # In-memory implementation
│   │
│   ├── snapshot/
│   │   ├── builder.go                    # Snapshot JSON builder
│   │   └── store.go                      # S3 storage operations
│   │
│   ├── workflow/
│   │   ├── detection/
│   │   │   ├── workflow.go               # Detection workflow (single resource type)
│   │   │   └── activities.go             # Inventory, EOL, detection activities
│   │   └── orchestrator/
│   │       ├── workflow.go               # Main orchestrator (fan-out)
│   │       └── activities.go             # Snapshot storage activity
│   │
│   ├── scan/
│   │   ├── scan.go                       # Scan trigger (HTTP + CLI)
│   │   └── handler.go                    # HTTP handler for POST /scan
│   │
│   ├── emitters/
│   │   ├── emitters.go                   # Emitter interfaces (for custom implementations)
│   │   └── examples/
│   │       └── logging_emitter.go        # Example logging emitter
│   │
│   └── registry/
│       └── client.go                     # Service attribution interface
│
└── docs/
    └── examples/                         # Usage examples
```

---

## Core Interfaces

### 1. InventorySource

Fetches cloud resources with version information.

```go
type InventorySource interface {
    // ListResources returns all resources of a specific type
    ListResources(ctx context.Context, resourceType ResourceType) ([]*Resource, error)

    // GetResource fetches a specific resource by ID
    GetResource(ctx context.Context, id string) (*Resource, error)

    // Name returns the name of this inventory source
    Name() string

    // CloudProvider returns which cloud provider this source covers
    CloudProvider() CloudProvider
}
```

**Implementations:**
- `wiz.GenericInventorySource` - Config-driven Wiz saved reports (handles all resource types)
- `mock.InventorySource` - For testing

**How to extend:**
1. **Config-driven approach (recommended)**: Add resource to `pkg/config/defaults/resources.yaml` with field mappings
2. **Custom implementation**: Implement the `InventorySource` interface for non-Wiz sources

### 2. EOLProvider

Provides End-of-Life data for software versions.

```go
type Provider interface {
    // GetVersionLifecycle returns EOL data for a specific version
    GetVersionLifecycle(ctx context.Context, engine, version string) (*VersionLifecycle, error)

    // ListVersions returns all known versions for an engine
    ListVersions(ctx context.Context, engine string) ([]*VersionLifecycle, error)

    // Name returns the provider name
    Name() string
}
```

**Implementations:**
- `endoflife.Provider` - endoflife.date HTTP API (config-driven via `eol.product`, `eol.schema`, and optional `eol.lifecycle`)
- `mock.EOLProvider` - For testing

**Single-Source Strategy:**
- All EOL data comes from endoflife.date — no cloud provider credentials required for lifecycle lookups.
- Per-product semantics are handled by either the built-in `standard` schema or a YAML-defined `declarative` lifecycle block.

### 3. VersionPolicy

Classifies resource versions based on lifecycle status.

```go
type VersionPolicy interface {
    // Classify determines the compliance status of a resource
    Classify(resource *Resource, lifecycle *VersionLifecycle) Status
}
```

**Default Policy:**
- 🔴 **RED**: Past EOL, deprecated, or extended support expired
- 🟡 **YELLOW**: In extended support or approaching EOL (< 90 days)
- 🟢 **GREEN**: In standard support, current version
- ⚪ **UNKNOWN**: Version not found in EOL database

### 4. Detection Pipeline

Detection is **not** packaged behind a `Detector` interface anymore. It runs
directly as a sequence of Temporal activities in
[`pkg/workflow/detection/activities.go`](./pkg/workflow/detection/activities.go),
dispatched per resource type by the orchestrator. Each activity looks up the
right `InventorySource`, `EOLProvider`, and `VersionPolicy` from the
per-resource maps that `cmd/server/main.go` builds at startup from
`pkg/config/defaults/resources.yaml`.

**Activities (per child workflow):**

1. `FetchInventory` — calls `inventorySources[resourceID].ListResources(...)`
2. `FetchEOLData` — calls `eolProviders[resourceID].ListVersions(...)` once per engine in the inventory result
3. `DetectDrift` — applies `policy.Classify` to each resource against the EOL lifecycle and produces `[]*Finding`

**Pseudocode:**

```go
// pkg/workflow/detection/workflow.go
func DetectionWorkflow(ctx workflow.Context, in WorkflowInput) (*WorkflowOutput, error) {
    var resources []*types.Resource
    workflow.ExecuteActivity(ctx, FetchInventory, in.ResourceID).Get(ctx, &resources)

    var eol map[string][]*types.VersionLifecycle
    workflow.ExecuteActivity(ctx, FetchEOLData, in.ResourceID, engines(resources)).Get(ctx, &eol)

    var findings []*types.Finding
    workflow.ExecuteActivity(ctx, DetectDrift, in.ResourceID, resources, eol).Get(ctx, &findings)

    return &WorkflowOutput{Findings: findings}, nil
}
```

**Config-Driven Approach:**
There is a single, generic detection pipeline. Resource types are added by
declaring them in `pkg/config/defaults/resources.yaml` (and providing a Wiz
report ID via `WIZ_REPORT_IDS`); the pipeline picks them up with no Go
changes. See [TRANSFORMS.md](./TRANSFORMS.md) for how to reshape raw inventory
fields (version/engine) without writing code.

### 5. Store

Persists findings for querying.

```go
type Store interface {
    // Save stores a finding
    Save(ctx context.Context, finding *Finding) error

    // List retrieves findings with filters
    List(ctx context.Context, filters Filters) ([]*Finding, error)

    // Get retrieves a specific finding by ID
    Get(ctx context.Context, id string) (*Finding, error)
}
```

**Implementations:**
- `memory.Store` - In-memory (included)
- SQL store - Your implementation (interface provided)

---

## Temporal Workflows

### DetectionWorkflow

Handles detection for a **single resource type**.

```go
func DetectionWorkflow(ctx workflow.Context, input WorkflowInput) (*WorkflowOutput, error) {
    // Activity 1: Fetch inventory
    inventory := workflow.ExecuteActivity(ctx, FetchInventoryActivity, ...)

    // Activity 2: Fetch EOL data
    eolData := workflow.ExecuteActivity(ctx, FetchEOLActivity, ...)

    // Activity 3: Detect drift (apply policy, create findings)
    findings := workflow.ExecuteActivity(ctx, DetectDriftActivity, ...)

    return &WorkflowOutput{FindingsCount: len(findings)}, nil
}
```

### OrchestratorWorkflow

Fans out detection across **all resource types** in parallel.

```go
func OrchestratorWorkflow(ctx workflow.Context, input WorkflowInput) (*WorkflowOutput, error) {
    // Stage 1: DETECT - Fan out to child workflows
    futures := []workflow.ChildWorkflowFuture{}
    for _, resourceType := range resourceTypes {
        future := workflow.ExecuteChildWorkflow(ctx, DetectionWorkflow, ...)
        futures = append(futures, future)
    }

    // Wait for all to complete
    for _, future := range futures {
        future.Get(ctx, &result)
    }

    // Stage 2: STORE - Create S3 snapshot
    workflow.ExecuteActivity(ctx, CreateSnapshotActivity, ...)

    return output, nil
}
```

**Scheduling:**
- Run on a schedule (e.g., every 6 hours)
- Or trigger manually via Temporal CLI/API

---

## S3 Snapshots

Version Guard creates versioned JSON snapshots in S3 for audit trail and downstream consumption.

### Storage Pattern

```
s3://your-bucket/snapshots/
├── YYYY/MM/DD/
│   ├── scan-{timestamp}-{uuid}.json
│   ├── scan-{timestamp}-{uuid}.json
│   └── ...
└── latest.json (symlink to most recent)
```

### Snapshot Schema

```json
{
  "snapshot_id": "scan-2026-04-09-123456",
  "version": "v3",
  "generated_at": "2026-04-09T12:34:56Z",
  "scan_start_time": "2026-04-09T12:00:00Z",
  "scan_end_time": "2026-04-09T12:34:56Z",
  "findings_by_type": {
    "AURORA": [...],
    "EKS": [...]
  },
  "summary": {
    "total_resources": 150,
    "red_count": 12,
    "yellow_count": 35,
    "green_count": 103,
    "compliance_percentage": 68.7,
    "by_service": {...},
    "by_cloud_provider": {...}
  }
}
```

### Consuming Snapshots

**Option 1: S3 Event Trigger**
```python
# Lambda function triggered on new snapshot
def handler(event, context):
    snapshot_key = event['Records'][0]['s3']['object']['key']
    snapshot = s3.get_object(Bucket='bucket', Key=snapshot_key)
    data = json.loads(snapshot['Body'].read())

    # Send to your issue tracker, dashboard, etc.
    for finding in data['findings_by_type']['AURORA']:
        if finding['status'] == 'RED':
            create_jira_ticket(finding)
```

**Option 2: Scheduled Reader**
```bash
# Cron job reading latest.json every hour
0 * * * * curl -s s3://bucket/snapshots/latest.json | jq '.summary'
```

**Option 3: Custom Temporal Workflow**
```go
// Implement your own "Act" workflow
func CustomActWorkflow(ctx workflow.Context, snapshotID string) error {
    // Read snapshot from S3
    snapshot := workflow.ExecuteActivity(ctx, LoadSnapshotActivity, snapshotID)

    // Your custom emitters
    workflow.ExecuteActivity(ctx, EmitToJiraActivity, snapshot)
    workflow.ExecuteActivity(ctx, EmitToSlackActivity, snapshot)
    workflow.ExecuteActivity(ctx, EmitToDatadogActivity, snapshot)

    return nil
}
```

---

## Implementing Custom Emitters

Version Guard provides **emitter interfaces** for integration with your systems.

### Emitter Interfaces

```go
// IssueTrackerEmitter - Issue tracking integration
type IssueTrackerEmitter interface {
    Emit(ctx context.Context, snapshotID string, findings []*Finding) (*IssueTrackerResult, error)
}

// DashboardEmitter - Dashboard integration
type DashboardEmitter interface {
    Emit(ctx context.Context, snapshotID string, summary *SnapshotSummary) (*DashboardResult, error)
}
```

### Example: Jira Emitter

```go
type JiraEmitter struct {
    client *jira.Client
}

func (e *JiraEmitter) Emit(ctx context.Context, snapshotID string, findings []*types.Finding) (*emitters.IssueTrackerResult, error) {
    created := 0

    for _, finding := range findings {
        if finding.Status == types.StatusRed || finding.Status == types.StatusYellow {
            issue := &jira.Issue{
                Fields: &jira.IssueFields{
                    Project:     jira.Project{Key: "INFRA"},
                    Summary:     finding.Message,
                    Description: finding.Message,
                    Priority:    e.mapPriority(finding.Status),
                },
            }

            _, _, err := e.client.Issue.Create(issue)
            if err == nil {
                created++
            }
        }
    }

    return &emitters.IssueTrackerResult{IssuesCreated: created}, nil
}
```

### Integration Points

1. **In workflows** - Call emitters from activities
2. **From snapshots** - Read S3, emit independently
3. **From HTTP admin** - Trigger scans on-demand via `POST /scan`

---

## Testing

### Unit Tests

```bash
# Run all tests
make test

# Run specific package
go test ./pkg/workflow/detection -v

# Run with coverage
make test-coverage
```

### Integration Tests

Tag integration tests with `// +build integration`:

```go
// +build integration

func TestAuroraDetection_Integration(t *testing.T) {
    // Requires real Wiz credentials (WIZ_CLIENT_ID_SECRET, WIZ_CLIENT_SECRET_SECRET)
    // Drives the FetchInventory → FetchEOLData → DetectDrift activity chain
}
```

Run with:
```bash
go test -tags=integration ./...
```

### Mocking

Pluggable interfaces have mock or in-memory implementations for tests:
- `pkg/inventory/mock.InventorySource`
- `pkg/eol/mock.EOLProvider`
- `pkg/store/memory.Store` (production-grade in-memory store, also used by tests)

---

## Deployment

### Local Development

```bash
# 1. Start Temporal
make temporal

# 2. Run server
make dev  # Auto-reload
# OR
make run-locally  # One-shot
```

### Production (Your Infrastructure)

1. **Deploy Temporal cluster** (or use Temporal Cloud)
2. **Deploy Version Guard server**:
   - Binary: `./bin/version-guard`
   - Container: Build from Dockerfile
   - Configuration: Via environment variables
3. **Configure credentials**:
   - Wiz: `WIZ_CLIENT_ID_SECRET`, `WIZ_CLIENT_SECRET_SECRET`
   - AWS: Standard AWS credential chain (only needed for writing snapshots to S3)
   - S3: `S3_BUCKET`, `AWS_REGION`
4. **Schedule workflows**:
   ```bash
   temporal schedule create \
     --schedule-id version-guard-scan \
     --interval 6h \
     --workflow-type VersionGuardOrchestratorWorkflow
   ```

### Monitoring

- **Metrics**: Expose Prometheus metrics from HTTP admin service
- **Logs**: Structured JSON logging via `log/slog`
  - Machine-readable JSON format for log aggregation tools (Datadog, Splunk, CloudWatch Insights)
  - Context-aware logging with typed fields for queryable log data
  - Configurable log levels (Info/Debug via `--verbose` flag)
  - All components (detectors, inventory sources, EOL providers) use structured logging
  - Example log entry:
    ```json
    {
      "time": "2024-01-15T10:30:45Z",
      "level": "WARN",
      "msg": "failed to parse resource from CSV row",
      "row_number": 42,
      "error": "missing ARN"
    }
    ```
- **Alerts**: Based on RED/YELLOW finding counts
- **Dashboards**: Consume S3 snapshots for real-time data

---

## Adding a New Resource Type

**With the config-driven approach, adding a new resource type requires ZERO code changes!**

Step-by-step guide to adding support for a new resource type (e.g., RDS PostgreSQL):

### 1. Create a Wiz Saved Report

In the Wiz console:
1. Create a query for your resource type (e.g., RDS PostgreSQL instances)
2. Save it as a report
3. Copy the report ID from the URL

### 2. Add Resource Configuration

Edit `pkg/config/defaults/resources.yaml` and add ~15 lines:

```yaml
resources:
  # ... existing resources ...

  - id: rds-postgresql
    type: rds
    cloud_provider: aws
    inventory:
      source: wiz
      native_type_pattern: "rds/PostgreSQL/instance"
      required_mappings:
        resource_id: "externalId"
        version: "versionDetails.version"
        engine: "typeFields.engine"
      field_mappings:
        name: "name"
        account_id: "cloudAccount.externalId"
        region: "region"
        tags: "tags"
    eol:
      provider: endoflife-date
      product: postgresql
      schema: standard
```

**Mappings:** Wiz CSV column names are split into `required_mappings`
(must be present; validated at config load time) and `field_mappings`
(optional; missing keys produce empty Extra entries).

**Native Type Pattern:** The Wiz `nativeType` to filter (supports wildcards like `elastiCache/*/cluster`).

**Transforms (optional):** When the raw column values for `version` or
`engine` aren't already the canonical strings endoflife.date expects,
declare a `transforms` block. Available named operations cover JSON
extraction (Lambda runtime), prefix stripping (OpenSearch versions),
constants (Lambda's `aws-lambda` engine), defaults-when-empty (EKS),
substring lookups (Aurora engine canonicalization), and version-major
lookups (legacy Elasticsearch vs OpenSearch). The DSL is deliberately
narrow — no expressions, one named op per field. Full reference:
[TRANSFORMS.md](./TRANSFORMS.md).

**EOL Configuration:**
- `provider`: Currently only `endoflife-date` supported
- `product`: The endoflife.date product ID (e.g., `postgresql`, `amazon-eks`)
- `schema`: EOL data semantics (`standard` or `declarative`)
- `lifecycle`: Required for `schema: declarative`; maps upstream fields into deprecation, extended-support, and EOL boundaries

### 3. Add Report ID to Environment Variable

Update the `WIZ_REPORT_IDS` JSON map:

```bash
export WIZ_REPORT_IDS='{
  "aurora-mysql": "your-aurora-mysql-report-id",
  "eks": "your-eks-report-id",
  "elasticache-redis": "your-elasticache-report-id",
  "elasticache-valkey": "your-elasticache-report-id",
  "elasticache-memcached": "your-elasticache-report-id",
  "rds-postgresql": "your-new-report-id"
}'
```

The key must match the `id` field in `resources.yaml`.

### 4. Restart Server

```bash
./bin/version-guard
```

The server will automatically:
- Load the new resource configuration
- Register a generic Wiz inventory source and an endoflife.date EOL provider for it
- Include it in the orchestrator workflow's resource list
- Start scanning on the next scheduled run

### 5. Verify

```bash
# Check that the resource is registered
./bin/version-guard-cli service list

# Trigger a scan
temporal workflow start \
  --task-queue version-guard-detection \
  --type OrchestratorWorkflow \
  --input '{}'

# Query findings
./bin/version-guard-cli finding list --type rds
```

**That's it!** No Go code changes, no compilation, no new files.

---

### Advanced: Custom Inventory Source

If you need a non-Wiz inventory source (e.g., direct AWS API calls):

```go
// pkg/inventory/custom/my_source.go
type MyInventorySource struct {
    // Your fields
}

func (s *MyInventorySource) ListResources(ctx context.Context, resourceType ResourceType) ([]*Resource, error) {
    // Your implementation
}

// cmd/server/main.go
// Register your custom source
invSources["my-resource"] = custom.NewMyInventorySource(...)
```

---

## Performance Considerations

### Scaling

- **Parallel detection**: Each resource type scans in parallel via child workflows
- **Worker scaling**: Run multiple Temporal workers for horizontal scaling
- **Cache EOL data**: 1-hour TTL reduces API calls
- **Batch processing**: Process resources in batches within activities

### Optimization Tips

1. **Wiz saved reports** > GraphQL API (faster, cached)
2. **Per-`reportID` Wiz cache** avoids re-fetching the same CSV across resources that share a report
3. **In-memory store** for < 10K findings, SQL for more
4. **Activity heartbeats** for long-running scans
5. **Workflow replay safe**: Avoid non-deterministic code

---

## Security

### Credentials

- Store credentials in secrets manager (AWS Secrets Manager, HashiCorp Vault, etc.)
- Never commit credentials to git
- Use least-privilege IAM policies

### API Access

- Wiz: Read-only saved report access
- AWS: `s3:PutObject` on the snapshot bucket (no other AWS API access required — EOL data comes from endoflife.date)
- HTTP Admin: Consider authentication for scan trigger endpoint

### Data Privacy

- Findings may contain resource IDs, service names
- S3 buckets should be private, encrypted
- Audit snapshot access

---

## FAQ

**Q: Can I use this without Wiz?**
A: Yes! Implement a custom `InventorySource` that queries AWS APIs directly, or any other cloud inventory system.

**Q: Can I use this without Temporal?**
A: The core building blocks (inventory sources, EOL providers, policies) can be invoked directly. Temporal provides scheduling, retries, and the parallel fan-out across resource types.

**Q: How do I add a new cloud provider?**
A: Implement `InventorySource` and `EOLProvider` for that cloud, add to `CloudProvider` enum, declare resources of that provider in `pkg/config/defaults/resources.yaml`. The generic detection pipeline picks them up.

**Q: What if my organization uses a different issue tracker?**
A: Implement the `IssueTrackerEmitter` interface for your system (Jira, ServiceNow, Linear, etc.).

**Q: Can I customize the Red/Yellow/Green policy?**
A: Yes! Implement the `VersionPolicy` interface with your own rules.

---

## Contributing

See [CONTRIBUTING.md](./CONTRIBUTING.md) for:
- Development setup
- Code style
- Testing guidelines
- Pull request process

---

## License

Apache License 2.0 - See [LICENSE](./LICENSE) for details.
