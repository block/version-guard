# Version Guard - Usage Guide

Complete guide for operators, developers, and consumers of Version Guard.

---

## Table of Contents

1. [For Service Owners](#for-service-owners)
2. [For Platform Operators](#for-platform-operators)
3. [API Reference](#api-reference)
4. [Service Attribution](#service-attribution)
5. [Configuration](#configuration)
6. [Troubleshooting](#troubleshooting)
7. [Operational Runbooks](#operational-runbooks)

---

## For Service Owners

### Viewing Compliance Status via CLI

```bash
# Get fleet-wide summary
./bin/version-guard-cli fleet summary

# With filters
./bin/version-guard-cli fleet summary --cloud-provider=aws --resource-type=aurora

# List all findings
./bin/version-guard-cli finding list

# Filter by status
./bin/version-guard-cli finding list --status=red

# Filter by service
./bin/version-guard-cli finding list --service=my-service

# Export findings to CSV
./bin/version-guard-cli finding export --output=findings.csv
```

### Understanding Findings

Each finding includes:
- **Resource**: Which database/cache/cluster
- **Current Version**: What's running now
- **Status**: RED/YELLOW/GREEN
- **Message**: What's wrong
- **Recommendation**: What to do
- **EOL Date**: When version support ends

Examples:
```
# Aurora
Resource: arn:aws:rds:us-east-1:123456:cluster:my-db
Current Version: aurora-mysql 5.6.10a
Status: RED
Message: Version is past End-of-Life (EOL since Nov 2024)
Recommendation: Upgrade to aurora-mysql 8.0.35 immediately
EOL Date: 2024-11-01

# EKS
Resource: arn:aws:eks:us-west-2:123456:cluster/my-cluster
Current Version: k8s-1.27
Status: YELLOW
Message: Version in extended support (6x cost), ends 2025-11-24
Recommendation: Upgrade to k8s-1.31 to exit extended support
EOL Date: 2025-11-24
```

### Fixing Issues

#### RED Issues (Urgent)
**Action**: Upgrade immediately

1. **Aurora MySQL 5.6 → 8.0**:
   ```bash
   # Via AWS Console or CLI
   aws rds modify-db-cluster \
     --db-cluster-identifier my-db \
     --engine-version 8.0.35 \
     --apply-immediately
   ```

2. **ElastiCache Redis 4.x → 7.x**:
   ```bash
   aws elasticache modify-replication-group \
     --replication-group-id my-cache \
     --engine-version 7.0 \
     --apply-immediately
   ```

3. **EKS Kubernetes 1.27 → 1.31**:
   ```bash
   aws eks update-cluster-version \
     --name my-cluster \
     --kubernetes-version 1.31
   ```

#### YELLOW Issues (Plan Soon)
**Action**: Schedule upgrade within 90 days

1. Review upgrade path
2. Test in staging environment
3. Schedule maintenance window
4. Upgrade during low-traffic period

---

## For Platform Operators

### Deploying Version Guard

#### Prerequisites

1. **Wiz Access** (optional - can use mock inventory):
   - Client ID and Client Secret credentials
   - Access to saved reports for Aurora, ElastiCache, EKS

2. **Temporal Cluster**:
   - Temporal server running locally or remote
   - Namespace configured for Version Guard

3. **AWS Credentials** (for EOL API access):
   - IAM permissions for RDS and EKS describe operations

#### Run Locally

```bash
# Build the server
make build

# Set environment variables
export TEMPORAL_ENDPOINT=localhost:7233
export TEMPORAL_NAMESPACE=version-guard-dev
export AWS_REGION=us-west-2
export GRPC_PORT=8080

# Optional: Configure Wiz (otherwise uses mock data)
export WIZ_CLIENT_ID_SECRET=your-client-id
export WIZ_CLIENT_SECRET_SECRET=your-client-secret
export WIZ_REPORT_IDS='{
  "aurora-mysql":"your-aurora-report-id",
  "eks":"your-eks-report-id",
  "elasticache-redis":"your-elasticache-report-id"
}'

# Optional: Configure S3 snapshots
export S3_BUCKET=version-guard-snapshots
export S3_PREFIX=snapshots/

# Run the server
./bin/version-guard
```

#### Run with Docker

```bash
# Build Docker image
make docker-build

# Run container
docker run -p 8080:8080 \
  -e TEMPORAL_ENDPOINT=host.docker.internal:7233 \
  -e TEMPORAL_NAMESPACE=version-guard-dev \
  -e AWS_REGION=us-west-2 \
  version-guard:latest
```

#### Run Full Stack with docker-compose (Recommended for Testing)

The easiest way to run a complete Version Guard environment locally with Temporal, MinIO (S3), and the detector service:

**1. Configure environment variables:**

```bash
# Copy example file
cp .env.example .env

# Edit .env and set your Wiz credentials:
WIZ_CLIENT_ID_SECRET=your-client-id-here
WIZ_CLIENT_SECRET_SECRET=your-client-secret-here
WIZ_REPORT_IDS={"aurora-mysql":"report-id","eks":"report-id","elasticache-redis":"report-id"}
```

**2. Start the stack:**

```bash
# Start all services (Temporal, MinIO, Version Guard)
docker compose up --build -d

# Check that all services are running
docker compose ps

# Expected output:
# NAME                                    STATUS
# oss-version-guard-temporal-1            Up (healthy)
# oss-version-guard-minio-1               Up
# oss-version-guard-version-guard-1       Up
```

**3. Verify services are ready:**

```bash
# Check Version Guard logs
docker compose logs version-guard

# Look for these lines:
# ✓ Configuration loaded: 4 resource(s) defined
# ✓ Wiz credentials configured — using live inventory
# ✓ Total detectors initialized: 3
# ✓ Temporal worker starting on queue: version-guard-detection
# Version Guard is ready!
```

**4. Access web interfaces:**

- **Temporal Web UI**: http://localhost:8233
- **MinIO Console**: http://localhost:9001 (credentials: minioadmin/minioadmin)

**5. Trigger a detection workflow:**

```bash
# Start workflow via Temporal CLI
docker compose exec temporal temporal workflow start \
  --task-queue version-guard-detection \
  --type OrchestratorWorkflow \
  --input '{}' \
  --address localhost:7233 \
  --namespace version-guard-dev

# Output:
# Running execution:
#   WorkflowId  56c94211-e88d-4b9b-bda9-8acf39b73329
#   RunId       019d9654-498c-79b8-83c8-ad6ee0de4d7f
#   Type        OrchestratorWorkflow
```

**6. Monitor workflow execution:**

```bash
# Watch Version Guard logs in real-time
docker compose logs --follow version-guard

# Check workflow status (use WorkflowId from step 5)
docker compose exec temporal temporal workflow describe \
  --workflow-id 56c94211-e88d-4b9b-bda9-8acf39b73329 \
  --namespace version-guard-dev

# Or view in Temporal Web UI:
# http://localhost:8233 → Workflows → Select your workflow
```

**7. View workflow results:**

Example successful execution:
```
Status: COMPLETED
Total Findings: 8,386 resources scanned
Compliance: 45.36%
Runtime: 29.35 seconds

Resource Breakdown:
  aurora: 4,257 findings
  eks: 155 findings (65 GREEN, 90 YELLOW)
  elasticache: 3,974 findings (3,739 GREEN, 138 YELLOW, 97 UNKNOWN)

Snapshot created: s3://version-guard-snapshots/snapshots/YYYY/MM/DD/{workflow-id}.json
```

**8. Verify snapshot in MinIO:**

```bash
# Check logs for snapshot confirmation
docker compose logs version-guard | grep "Snapshot created"

# Or browse snapshots in MinIO Console:
# http://localhost:9001 → Buckets → version-guard-snapshots → snapshots
```

**9. Stop the stack:**

```bash
# Stop and remove containers
docker compose down

# Stop and remove containers + volumes (clean slate)
docker compose down -v
```

**Troubleshooting local workflow execution:**

```bash
# Check if Temporal is healthy
docker compose ps temporal

# Check if Version Guard connected to Temporal
docker compose logs version-guard | grep "Connected to Temporal"

# View detailed workflow errors
docker compose exec temporal temporal workflow describe \
  --workflow-id <WORKFLOW_ID> \
  --namespace version-guard-dev

# Check activity errors
docker compose logs version-guard | grep -i error
```

### Starting the Detector Workflow

The orchestrator workflow automatically triggers detection workflows for all configured resource types in parallel.

**Manual Trigger via Temporal UI:**
1. Navigate to your Temporal UI (e.g., http://localhost:8233)
2. Start workflow: `OrchestratorWorkflow`
3. Task Queue: `version-guard-detection`
4. Input: `{}` (empty for all resources, or specify: `{"ResourceTypes":["aurora","eks"]}`)
5. Monitor workflow execution

**Manual Trigger via Temporal CLI:**
```bash
# Scan all configured resources
temporal workflow start \
  --task-queue version-guard-detection \
  --type OrchestratorWorkflow \
  --input '{}' \
  --namespace version-guard-dev

# Scan specific resources only
temporal workflow start \
  --task-queue version-guard-detection \
  --type OrchestratorWorkflow \
  --input '{"ResourceTypes":["eks","elasticache"]}' \
  --namespace version-guard-dev
```

**Monitor Progress:**
```bash
# List all workflows
temporal workflow list --namespace version-guard-dev

# View workflow details
temporal workflow describe --workflow-id <workflow-id> --namespace version-guard-dev

# Watch workflow execution in real-time
temporal workflow observe --workflow-id <workflow-id> --namespace version-guard-dev
```

### Monitoring

#### Metrics to Track

Version Guard emits the following metrics (if Datadog enabled):
- `version_guard.findings.red` - Critical issues count
- `version_guard.findings.yellow` - Warning issues count
- `version_guard.findings.total` - Total resources scanned
- `version_guard.compliance_percentage` - Fleet compliance %
- `version_guard.detection.duration_ms` - Scan duration
- `version_guard.inventory.fetch` - Inventory fetch success rate

#### Logs

```bash
# If running directly
# Logs output to stdout/stderr

# If running in Docker
docker logs <container-id> -f

# If running in Kubernetes
kubectl logs -n version-guard deployment/version-guard -f
```

### Scaling

**Temporal Workers**:
Increase the number of Temporal worker replicas for higher throughput.

**Detection Frequency**:
Configure the orchestrator workflow schedule via Temporal schedules or cron triggers.

---

## API Reference

### gRPC Service

**Default Endpoint**: `localhost:8080`

#### GetServiceScore

Get compliance score for a specific service.

**Request**:
```protobuf
message GetServiceScoreRequest {
  string service = 1;           // Required: service name
  ResourceType resource_type = 2; // Optional: filter by resource type
  CloudProvider cloud_provider = 3; // Optional: filter by cloud
}
```

**Response**:
```protobuf
message GetServiceScoreResponse {
  string service = 1;
  ComplianceGrade grade = 2;     // BRONZE/SILVER/GOLD
  int32 total_resources = 3;
  int32 red_count = 4;
  int32 yellow_count = 5;
  int32 green_count = 6;
  float compliance_percentage = 7;
}
```

**Example (grpcurl)**:
```bash
grpcurl \
  -plaintext \
  -d '{"service": "payments"}' \
  localhost:8080 \
  block.versionguard.service.VersionGuard/GetServiceScore
```

#### ListFindings

List all findings with filters.

**Request**:
```protobuf
message ListFindingsRequest {
  CloudProvider cloud_provider = 1; // Optional: filter by cloud (AWS/GCP/AZURE)
  ResourceType resource_type = 2;   // Optional: filter by type (AURORA/ELASTICACHE/etc)
  string service = 3;               // Optional: filter by service name
  Status status = 4;                // Optional: filter by status (RED/YELLOW/GREEN)
  string brand = 5;                 // Optional: filter by brand
  string cloud_account_id = 6;      // Optional: filter by AWS account/GCP project
  string cloud_region = 7;          // Optional: filter by region (us-east-1/us-west-2)
  int32 limit = 8;                  // Optional: max results to return
}
```

**Response**:
```protobuf
message ListFindingsResponse {
  repeated Finding findings = 1;
  int32 total_count = 2;
}
```

**Example**:
```bash
# Get all RED findings for payments service
grpcurl \
  -plaintext \
  -d '{"service": "payments", "status": "RED"}' \
  localhost:8080 \
  block.versionguard.service.VersionGuard/ListFindings
```

#### GetFleetSummary

Get aggregate statistics across the fleet.

**Request**:
```protobuf
message GetFleetSummaryRequest {
  CloudProvider cloud_provider = 1; // Optional
  ResourceType resource_type = 2;   // Optional
}
```

**Response**:
```protobuf
message GetFleetSummaryResponse {
  int32 total_resources = 1;
  int32 red_count = 2;
  int32 yellow_count = 3;
  int32 green_count = 4;
  int32 unknown_count = 5;
  float compliance_percentage = 6;
  google.protobuf.Timestamp last_scan = 7;
  map<string, int32> by_service = 8;       // Resources grouped by service
  map<string, int32> by_brand = 9;         // Resources grouped by brand
  map<string, int32> by_cloud_provider = 10; // Resources by cloud (aws/gcp/azure)
}
```

**Example**:
```bash
# Get AWS Aurora fleet summary
grpcurl \
  -plaintext \
  -d '{"cloud_provider": "AWS", "resource_type": "AURORA"}' \
  localhost:8080 \
  block.versionguard.service.VersionGuard/GetFleetSummary
```

---

## Service Attribution

Version Guard attributes infrastructure resources to services using a **3-tier fallback approach** to ensure accurate ownership mapping even when resources are poorly tagged.

### How Resources Are Attributed to Services

**Priority Order** (highest to lowest):

1. **Resource Tags** (Primary, Fastest)
   - Checks AWS tags (configurable via `TAG_APP_KEYS` environment variable)
   - Default tag keys: `app`, `application`, `service` (tried in order)
   - Customize to match your organization's tagging conventions
   - **Speed**: Instant (already in CSV data)
   - **Accuracy**: Depends on tagging discipline

2. **Registry Lookup** (Fallback, Optional)
   - Maps Cloud Account ID + Region → Service Name
   - **Speed**: ~100ms per resource (if configured)
   - **Accuracy**: Authoritative (from your service registry)
   - **Enabled**: Only if registry client implemented and configured

3. **Resource Name Parsing** (Last Resort)
   - Extracts service from cluster name (e.g., `payments-prod-cluster-1` → `payments`)
   - **Speed**: Instant (regex parsing)
   - **Accuracy**: Best effort

### Example Attribution Flow

```
Aurora Cluster: arn:aws:rds:us-east-1:123456789012:cluster:untagged-db

Step 1: Check tags
  → Tags: {} (empty)
  → Result: ❌ No service found

Step 2: Registry lookup (if configured)
  → AWS Account: 123456789012
  → Region: us-east-1
  → Registry returns: "payments"
  → Result: ✅ Service = "payments"

(If registry not configured or fails)
Step 3: Parse name
  → Cluster name: "untagged-db"
  → Parse result: "untagged"
  → Result: ⚠️ Service = "untagged" (may be inaccurate)
```

### Implementing Custom Registry Integration

Version Guard defines a `registry.Client` interface that you can implement:

```go
// pkg/registry/client.go
type Client interface {
    GetServiceByCloudAccount(ctx context.Context, cloudAccountID, region string) (string, error)
}
```

**Example implementation**:

```go
package myregistry

import (
    "context"
    "fmt"
)

type MyRegistryClient struct {
    endpoint string
    apiKey   string
}

func NewClient(endpoint, apiKey string) *MyRegistryClient {
    return &MyRegistryClient{
        endpoint: endpoint,
        apiKey:   apiKey,
    }
}

func (c *MyRegistryClient) GetServiceByCloudAccount(ctx context.Context, accountID, region string) (string, error) {
    // Call your internal service registry API
    // Return service name or error if not found
    return "my-service", nil
}
```

### Best Practices for Service Owners

1. **Tag your resources properly**:
   ```bash
   # Use one of the configured tag keys (default: app, application, or service)
   aws rds add-tags-to-resource \
     --resource-name arn:aws:rds:us-east-1:123:cluster:my-db \
     --tags Key=app,Value=my-service

   # Or match your organization's custom tag convention
   # (if you've customized TAG_APP_KEYS environment variable)
   ```

   **Note**: If your organization uses different tag keys (e.g., `team`, `component`), configure Version Guard to match by setting the `TAG_APP_KEYS` environment variable.

2. **Ensure registry data is current** (if using registry):
   - Keep cloud account mappings up-to-date
   - Update when services migrate across accounts

3. **Use consistent naming**:
   - Include service name in cluster names: `{service}-{env}-cluster-{n}`
   - Example: `payments-prod-cluster-1`, `billing-staging-cluster-2`

---

## Configuration

### Environment Variables

```bash
# Temporal Configuration
TEMPORAL_ENDPOINT=localhost:7233
TEMPORAL_NAMESPACE=version-guard-dev
TEMPORAL_TASK_QUEUE=version-guard-detection

# Wiz Configuration (Optional - falls back to mock data if not provided)
WIZ_CLIENT_ID_SECRET=your-wiz-client-id-here
WIZ_CLIENT_SECRET_SECRET=your-wiz-client-secret-here
WIZ_CACHE_TTL_HOURS=1

# Wiz Saved Report IDs
WIZ_REPORT_IDS='{"aurora-mysql":"your-report-id","eks":"your-report-id","elasticache-redis":"your-report-id"}'

# AWS Configuration
AWS_REGION=us-west-2

# S3 Snapshot Storage
S3_BUCKET=version-guard-snapshots
S3_PREFIX=snapshots/

# gRPC Service
GRPC_PORT=8080

# Tag Configuration (customize AWS resource tag keys)
TAG_APP_KEYS=app,application,service
TAG_ENV_KEYS=environment,env
TAG_BRAND_KEYS=brand

# Logging
LOG_LEVEL=info
```

**Customizing Tag Keys:**

Version Guard extracts metadata from AWS resource tags to determine service ownership, environment, and brand. By default, it looks for tags like `app`, `application`, `service`, etc. Customize these to match your organization's tagging conventions:

```bash
# Example: Organization uses "cost-center" for business units
TAG_BRAND_KEYS=cost-center,department,business-unit

# Example: Organization uses "team" for service attribution
TAG_APP_KEYS=team,squad,component,application

# Example: Organization uses "env" exclusively
TAG_ENV_KEYS=env
```

Tag keys are tried in order — the first matching tag wins.

See `.env.example` for a complete template.

---

## Troubleshooting

### Common Issues

#### 1. Workflow Not Running

**Symptom**: No new findings appearing

**Debug**:
```bash
# Check Temporal workflow status
temporal workflow describe \
  --workflow-id version-guard-orchestrator-v1 \
  --namespace version-guard-dev

# Check server logs for errors
```

**Fix**:
- Ensure Temporal server is running and accessible
- Verify workflow is registered (check server startup logs)
- Check that workflow schedule exists (if using scheduled runs)

#### 2. Wiz API Errors

**Symptom**: `failed to fetch Wiz report`

**Debug**:
- Check Wiz credentials are correctly configured
- Verify report IDs are correct
- Check Wiz API status

**Fix**:
- If Wiz is unavailable, server will automatically fall back to mock inventory
- Verify credentials: `echo $WIZ_CLIENT_ID_SECRET`
- Check report ID configuration

#### 3. AWS API Throttling

**Symptom**: `TooManyRequestsException` in logs

**Fix**:
- Exponential backoff is already implemented in EOL providers
- Reduce scan frequency
- Request AWS service quota increase

#### 4. No Findings for Known Resources

**Symptom**: Resource exists but not detected

**Debug**:
- Check if resource appears in Wiz report CSV
- Verify resource tags are correctly formatted
- Check EOL provider has version data

**Fix**:
- Ensure resource is in Wiz report (or mock inventory if testing)
- Verify resource tags (app, service, brand)
- Check EOL provider implementation for version coverage

---

## Operational Runbooks

### Runbook 1: Onboarding New Resource Type

**Example: Adding a new database type**

1. **Create EOL Provider**:
   ```bash
   touch pkg/eol/custom/mydb.go
   # Implement EOLProvider interface
   ```

2. **Create Inventory Source**:
   ```bash
   touch pkg/inventory/wiz/mydb.go
   # Implement InventorySource interface
   ```

3. **Create Detector**:
   ```bash
   mkdir pkg/detector/mydb
   touch pkg/detector/mydb/detector.go
   # Implement Detector interface
   ```

4. **Register in Server Main**:
   ```go
   // cmd/server/main.go
   eolProviders[types.ResourceTypeMyDB] = myeol.NewProvider(...)
   invSources[types.ResourceTypeMyDB] = myinv.NewSource(...)
   detectors[types.ResourceTypeMyDB] = mydb.NewDetector(...)
   ```

5. **Add Configuration**:
   - Add to `.env.example`
   - Update orchestrator workflow to include new resource type

6. **Test & Deploy**:
   ```bash
   make test
   make lint
   make build
   ./bin/version-guard
   ```

### Runbook 2: Implementing Custom Emitter

Version Guard provides emitter interfaces for you to implement. See [ARCHITECTURE.md - Custom Emitters](./ARCHITECTURE.md#implementing-custom-emitters) for details.

**Quick Start**:

1. **Implement the interface**:
   ```go
   package myemitter

   import (
       "context"
       "github.com/block/Version-Guard/pkg/emitters"
       "github.com/block/Version-Guard/pkg/types"
   )

   type MyEmitter struct {
       endpoint string
   }

   func (e *MyEmitter) Emit(ctx context.Context, snapshotID string, findings []*types.Finding) (*emitters.IssueTrackerResult, error) {
       // Send findings to your issue tracker, dashboard, etc.
       return &emitters.IssueTrackerResult{IssuesCreated: len(findings)}, nil
   }
   ```

2. **Wire it up in your workflow**:
   Create a custom workflow that reads snapshots from S3 and calls your emitter.

### Runbook 3: Adding New Cloud Provider

**Example: Adding GCP support**

See [ARCHITECTURE.md - Multi-Cloud Support](./ARCHITECTURE.md#multi-cloud-support) for detailed steps.

Summary:
1. Add `CloudProviderGCP` to enum
2. Create GCP inventory sources (Wiz + GCP Asset Inventory)
3. Create GCP EOL providers for CloudSQL, Memorystore, GKE
4. Create GCP detectors
5. Update workflow orchestrator
6. Update configuration
7. Test end-to-end
8. Deploy

---

## CLI Reference

### version-guard-cli Commands

**Global Flags:**
```bash
--endpoint=STRING    # gRPC endpoint (env: VERSION_GUARD_ENDPOINT)
-v, --verbose        # Enable verbose logging
-h, --help          # Show help
```

#### Fleet Commands

```bash
# Get fleet-wide summary
./bin/version-guard-cli fleet summary \
  [--cloud-provider=aws|gcp|azure] \
  [--resource-type=aurora|elasticache|eks] \
  [--output-format=text|json|yaml]
```

#### Finding Commands

```bash
# List findings with filters
./bin/version-guard-cli finding list \
  [--service=STRING] \
  [--status=red|yellow|green] \
  [--resource-type=STRING] \
  [--cloud-provider=STRING] \
  [--limit=INT] \
  [--output-format=text|json|yaml]

# Show finding details
./bin/version-guard-cli finding show <resource-id> \
  [--output-format=text|json|yaml]

# Export findings to CSV
./bin/version-guard-cli finding export \
  [--output=findings.csv] \
  [--service=STRING] \
  [--status=red|yellow|green]
```

---

## FAQ

**Q: How often does Version Guard scan resources?**
A: Depends on how you configure the orchestrator workflow schedule in Temporal.

**Q: Can I force a scan immediately?**
A: Yes, manually trigger the orchestrator workflow via Temporal UI or CLI.

**Q: What happens if I upgrade a resource?**
A: Next scan will detect the new version and auto-resolve the finding.

**Q: Does Version Guard automatically upgrade resources?**
A: No, Version Guard only detects and reports. You must upgrade manually.

**Q: What if my resource version isn't in the EOL database?**
A: Finding will show status UNKNOWN. You can extend the EOL provider to add version data.

**Q: How do I add a new resource type?**
A: See [Runbook 1](#runbook-1-onboarding-new-resource-type) above.

---

For more information:
- **Architecture**: See [ARCHITECTURE.md](./ARCHITECTURE.md)
- **GitHub Issues**: https://github.com/block/Version-Guard/issues
- **Contributing**: See [CONTRIBUTING.md](./CONTRIBUTING.md)
