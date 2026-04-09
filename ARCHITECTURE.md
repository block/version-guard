# Version Guard - Architecture Documentation

## 📊 Implementation Status

**Supported Resources**:
- ✅ Aurora (RDS MySQL/PostgreSQL)
- ✅ EKS (Kubernetes)
- 🔜 ElastiCache (Redis/Valkey/Memcached) - implementation available, needs integration
- 🔜 OpenSearch
- 🔜 Lambda runtimes

---

## Executive Summary

**Version Guard** is an open-source, Temporal-based service for continuous **multi-cloud** infrastructure version drift detection. It provides a pluggable collector/detector framework, starting with **AWS** resources and designed for extensibility to **GCP, Azure**, and other cloud platforms.

**Key Architecture Principles:**
- **Multi-cloud by design**: Cloud provider abstraction layer (AWS first, GCP/Azure ready)
- **Pluggable inventory sources**: Wiz (multi-cloud scanning) + cloud-native APIs + custom sources
- **Pluggable EOL providers**: AWS APIs + GCP APIs + Azure APIs + endoflife.date
- **Single responsibility principle**: Each component has one clear purpose
- **Interface-based design**: Easy to test, extend, and customize
- **Extensible storage**: In-memory (included) → SQL database (your implementation)
- **gRPC API**: Query interface for dashboards and integrations

---

## Multi-Cloud Strategy

**Vision:** Version Guard is a **cloud-agnostic** version drift detection platform supporting multiple cloud providers.

### Phase 1 (Implemented): AWS
- **Resources**: ✅ Aurora (RDS), ✅ EKS, 🔜 ElastiCache, 🔜 OpenSearch, 🔜 Lambda
- **Inventory**: Wiz (cross-cloud) + AWS APIs
- **EOL Data**: AWS native APIs (RDS, EKS) + endoflife.date (hybrid enrichment)

**Architecture Impact:**
- All resource types include `CloudProvider` field (AWS, GCP, Azure, etc.)
- Inventory sources are cloud-specific but share a common interface
- EOL providers are cloud-specific but share a common interface
- Detectors are resource-specific, cloud-aware
- gRPC API is cloud-agnostic (filters by cloud provider)

---

## Architecture Overview

```
┌─────────────────────────────────────────────────────────────┐
│                 Version Guard Detector Service              │
│            (Temporal Workflow + gRPC Service)               │
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
  │ Wiz      │       │ AWS APIs │       │ Red/     │
  │ Custom   │       │ endoflife│       │ Yellow/  │
  └──────────┘       └──────────┘       │ Green    │
        │                    │          └──────────┘
        └────────────────────┼────────────────┘
                             ▼
                    ┌────────────── ┐
                    │  Detectors    │
                    │ (Per Resource)│
                    └──────┬─────── ┘
                           ▼
                    ┌──────────────┐
                    │    Store     │
                    │  (Memory/SQL)│
                    └──────┬───────┘
                           │
                    ┌──────▼───────┐
                    │ gRPC Service │
                    │   (Query API)│
                    └──────────────┘
```

**Data Flow:**
1. **Temporal Workflow** executes on schedule (configurable interval)
2. **FetchInventory**: Wiz or custom source → resource list with versions
3. **FetchEOL**: AWS APIs or endoflife.date → version lifecycle data
4. **DetectDrift**: Apply policy → classify Red/Yellow/Green
5. **Store**: Save findings to storage backend
6. **S3 Snapshot**: Create versioned JSON snapshot (optional)
7. **gRPC**: Clients query for compliance data

---

## Repository Structure

```
Version-Guard/
├── cmd/
│   ├── server/main.go                    # Server with Temporal worker + gRPC
│   └── cli/main.go                       # CLI tool for operators
│
├── pkg/
│   ├── types/
│   │   ├── resource.go                   # Core types: Resource, Finding
│   │   ├── status.go                     # Status enum (Red/Yellow/Green)
│   │   └── cloud.go                      # CloudProvider enum
│   │
│   ├── inventory/
│   │   ├── inventory.go                  # InventorySource interface
│   │   ├── wiz/                          # Wiz implementation (multi-cloud)
│   │   │   ├── aurora.go                 # AWS Aurora inventory
│   │   │   ├── elasticache.go            # AWS ElastiCache inventory
│   │   │   ├── eks.go                    # AWS EKS inventory
│   │   │   └── client.go                 # Wiz HTTP client
│   │   └── mock/                         # Mock for tests
│   │
│   ├── eol/
│   │   ├── provider.go                   # EOLProvider interface
│   │   ├── aws/
│   │   │   ├── rds.go                    # AWS RDS EOL provider
│   │   │   └── eks.go                    # AWS EKS EOL provider
│   │   ├── endoflife/
│   │   │   ├── client.go                 # endoflife.date HTTP client
│   │   │   └── provider.go               # endoflife.date provider
│   │   └── mock/                         # Mock for tests
│   │
│   ├── policy/
│   │   ├── policy.go                     # VersionPolicy interface
│   │   └── default.go                    # Default Red/Yellow/Green policy
│   │
│   ├── detector/
│   │   ├── detector.go                   # Detector interface
│   │   ├── aurora/
│   │   │   └── detector.go               # Aurora detector
│   │   └── eks/
│   │       └── detector.go               # EKS detector
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
│   ├── service/
│   │   └── grpc/
│   │       ├── service.go                # gRPC service implementation
│   │       └── types.go                  # Type converters
│   │
│   ├── emitters/
│   │   ├── emitters.go                   # Emitter interfaces (for custom implementations)
│   │   └── examples/
│   │       └── logging_emitter.go        # Example logging emitter
│   │
│   └── registry/
│       └── client.go                     # Service attribution interface
│
├── protos/
│   └── block/versionguard/
│       └── service.proto                 # gRPC service definition
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
- `wiz.AuroraInventorySource` - Wiz saved reports for Aurora
- `wiz.EKSInventorySource` - Wiz saved reports for EKS
- `mock.MockInventorySource` - For testing

**How to extend:**
1. Implement the `InventorySource` interface
2. Register it in your server main
3. Use it in detectors

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
- `aws.RDSProvider` - AWS RDS DescribeDBEngineVersions API
- `aws.EKSProvider` - AWS EKS DescribeAddonVersions API
- `endoflife.Provider` - endoflife.date HTTP API
- `mock.EOLProvider` - For testing

**Hybrid Strategy:**
- Use cloud-native APIs when available (more accurate, real-time)
- Fall back to endoflife.date for broader coverage

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

### 4. Detector

Detects version drift for a specific resource type.

```go
type Detector interface {
    // Detect scans resources and generates findings
    Detect(ctx context.Context) ([]*Finding, error)

    // ResourceType returns which resource type this detector handles
    ResourceType() ResourceType
}
```

**Pattern:**
```go
func (d *Detector) Detect(ctx context.Context) ([]*Finding, error) {
    // 1. Fetch inventory
    resources, err := d.inventory.ListResources(ctx, d.ResourceType())

    // 2. For each resource, fetch EOL data
    for _, resource := range resources {
        lifecycle, err := d.eolProvider.GetVersionLifecycle(ctx, resource.Engine, resource.CurrentVersion)

        // 3. Apply policy to classify
        status := d.policy.Classify(resource, lifecycle)

        // 4. Create finding
        finding := &Finding{
            Resource: resource,
            Status:   status,
            // ...
        }

        // 5. Store finding
        d.store.Save(ctx, finding)
    }

    return findings, nil
}
```

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

## gRPC Service

Version Guard exposes a gRPC API for querying compliance data.

### Endpoints

1. **GetServiceScore** - Get compliance grade for a specific service
   ```protobuf
   rpc GetServiceScore(GetServiceScoreRequest) returns (ServiceScore)
   ```
   - Input: Service name, optional filters
   - Output: Bronze/Silver/Gold grade, resource counts

2. **ListFindings** - Query findings with filters
   ```protobuf
   rpc ListFindings(ListFindingsRequest) returns (ListFindingsResponse)
   ```
   - Input: Filters (status, service, cloud provider, etc.)
   - Output: List of findings

3. **GetFleetSummary** - Fleet-wide statistics
   ```protobuf
   rpc GetFleetSummary(GetFleetSummaryRequest) returns (FleetSummary)
   ```
   - Input: Optional filters
   - Output: Total counts, compliance %, breakdowns

### Compliance Grades

- 🥉 **Bronze**: Service tracked, versions known (has data)
- 🥈 **Silver**: No RED issues (no EOL/deprecated resources)
- 🥇 **Gold**: No YELLOW issues (fully compliant)

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
  "version": "v1",
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
                    Description: finding.Recommendation,
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
3. **From gRPC** - Query findings, emit on-demand

---

## Testing

### Unit Tests

```bash
# Run all tests
make test

# Run specific package
go test ./pkg/detector/aurora -v

# Run with coverage
make test-coverage
```

### Integration Tests

Tag integration tests with `// +build integration`:

```go
// +build integration

func TestAuroraDetector_Integration(t *testing.T) {
    // Requires real Wiz credentials
    // Requires AWS credentials
}
```

Run with:
```bash
go test -tags=integration ./...
```

### Mocking

All interfaces have mock implementations in `*/mock/` packages:
- `mock.MockInventorySource`
- `mock.EOLProvider`
- `mock.MockStore`

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
   - AWS: Standard AWS credential chain
   - S3: `S3_BUCKET`, `AWS_REGION`
4. **Schedule workflows**:
   ```bash
   temporal schedule create \
     --schedule-id version-guard-scan \
     --interval 6h \
     --workflow-type VersionGuardOrchestratorWorkflow
   ```

### Monitoring

- **Metrics**: Expose Prometheus metrics from gRPC service
- **Logs**: Structured JSON logging
- **Alerts**: Based on RED/YELLOW finding counts
- **Dashboards**: Query gRPC API for real-time data

---

## Adding a New Resource Type

Step-by-step guide to adding support for a new resource type (e.g., Lambda):

### 1. Define Resource Type

```go
// pkg/types/resource.go
const ResourceTypeLambda ResourceType = "LAMBDA"
```

### 2. Create Inventory Source

```go
// pkg/inventory/wiz/lambda.go
type LambdaInventorySource struct {
    client   *Client
    reportID string
}

func (s *LambdaInventorySource) ListResources(ctx context.Context, resourceType ResourceType) ([]*Resource, error) {
    // Fetch from Wiz saved report
    // Parse CSV/JSON
    // Convert to Resource structs
    return resources, nil
}
```

### 3. Create or Use EOL Provider

```go
// pkg/eol/aws/lambda.go (if AWS provides Lambda runtime EOL API)
// OR use endoflife.Provider("nodejs", "python", etc.)
```

### 4. Create Detector

```go
// pkg/detector/lambda/detector.go
type Detector struct {
    inventory inventory.InventorySource
    eol       eol.Provider
    policy    policy.VersionPolicy
    store     store.Store
}

func (d *Detector) Detect(ctx context.Context) ([]*types.Finding, error) {
    // 1. Fetch Lambda functions from inventory
    // 2. For each function, get runtime EOL data
    // 3. Apply policy
    // 4. Store findings
    return findings, nil
}
```

### 5. Add Tests

```go
// pkg/detector/lambda/detector_test.go
func TestLambdaDetector_Detect(t *testing.T) {
    // Use mocks for inventory, eol, store
    // Verify findings are created correctly
}
```

### 6. Register in Orchestrator

```go
// pkg/workflow/orchestrator/workflow.go
resourceTypes := []types.ResourceType{
    types.ResourceTypeAurora,
    types.ResourceTypeEKS,
    types.ResourceTypeLambda, // <-- Add here
}
```

### 7. Wire in Server

```go
// cmd/server/main.go
detectors[types.ResourceTypeLambda] = lambda.NewDetector(
    invSources[types.ResourceTypeLambda],
    eolProviders[types.ResourceTypeLambda],
    policyEngine,
    st,
)
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
2. **AWS APIs** > endoflife.date when available (more accurate)
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
- AWS: `rds:DescribeDBEngineVersions`, `eks:DescribeAddonVersions`, `s3:PutObject`
- gRPC: Implement authentication (TLS, JWT, mTLS)

### Data Privacy

- Findings may contain resource IDs, service names
- S3 buckets should be private, encrypted
- Audit snapshot access

---

## FAQ

**Q: Can I use this without Wiz?**
A: Yes! Implement a custom `InventorySource` that queries AWS APIs directly, or any other cloud inventory system.

**Q: Can I use this without Temporal?**
A: The core detection logic (detectors, policies, EOL providers) can be used standalone. Temporal provides scheduling and reliability.

**Q: How do I add a new cloud provider?**
A: Implement `InventorySource` and `EOLProvider` for that cloud, add to `CloudProvider` enum, create detectors.

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
