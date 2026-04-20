---
name: add-version-guard-resource
description: Add new cloud resource types to Version Guard by auto-generating configuration from endoflife.date and Wiz inventory reports. Use when adding, creating, enabling, implementing, configuring, or onboarding support for AWS, GCP, or Azure resources (databases, clusters, runtimes, managed services) in Version Guard. Detects EOL lifecycle data and generates YAML config automatically.
roles: []
metadata:
  version: "1.0.0"
  status: beta
user-invocable: true
disable-model-invocation: false
allowed-tools:
  - Read
  - Write
  - Edit
  - Glob
  - Grep
  - Bash(curl:*)
  - Bash(git add:*)
  - Bash(git commit:*)
  - Bash(go test:*)
  - Bash(make test:*)
  - Bash(cat:*)
  - Bash(ls:*)
  - Bash(pwd:*)
  - Bash(test:*)
  - Bash(echo:*)
  - WebFetch
---

# Adding a Resource to Version Guard

Add new cloud resource types to Version Guard's version drift detection system by auto-generating configuration from endoflife.date API and Wiz inventory schemas.

## Critical Guidance (STOP)

- If Version Guard generic infrastructure is not implemented, **STOP** and direct user to complete Phase 1 first (see SETUP.md)
- If required inputs are missing (resource name, Wiz report ID), **STOP and ask the user**
- If endoflife.date product doesn't exist, **STOP** and inform user to create endoflife.date PR first

## Prerequisites Check

Before starting, verify you're in the Version Guard repository and infrastructure exists:

```bash
# 1. Check current directory
pwd  # Should be in the Version-Guard repo

# 2. Verify generic infrastructure exists
test -f config/resources.yaml && echo "✅ Config schema exists" || echo "❌ Missing - see SETUP.md"
test -f pkg/config/loader.go && echo "✅ Config loader exists" || echo "❌ Missing - see SETUP.md"
test -f pkg/detector/generic/detector.go && echo "✅ Generic detector exists" || echo "❌ Missing - see SETUP.md"
```

**STOP** if any prerequisite check fails. Direct user to SETUP.md.

---

## Step-by-Step Workflow

### Step 1: Validate endoflife.date Product

Query endoflife.date API to verify coverage exists:

```bash
curl -s https://endoflife.date/api/all.json | grep -i "{resource-name}"
```

**Examples of what to search for**:
- For "Aurora PostgreSQL" → search "aurora" or "postgresql"
- For "OpenSearch" → search "opensearch"
- For "RDS MySQL" → search "rds" or "mysql"

**If product NOT found**:
- **STOP** and inform user:
  ```
  endoflife.date doesn't have coverage for {resource-name} yet.

  You need to create a PR at https://github.com/endoflife-date/endoflife.date first.

  See example PR: https://github.com/endoflife-date/endoflife.date/pull/9534

  Once the PR is merged, return and use this skill to add Version Guard support.
  ```

**If product found**: Note the exact product name (e.g., `amazon-aurora-postgresql`) and proceed to Step 2.

---

### Step 2: Gather User Input

Ask user for these required inputs:

1. **Resource ID** (lowercase, hyphens only)
   - This becomes the resource `id` field in config AND the key in WIZ_REPORT_IDS map
   - Examples: `aurora-postgresql`, `opensearch`, `rds-mysql`, `elasticache-redis`

2. **Wiz report ID** (the actual report UUID)
   - This will be added to the WIZ_REPORT_IDS JSON map with the resource ID as key
   - Example: `"your-wiz-report-id-here"`

3. **Cloud provider** (aws, gcp, azure)
   - Most resources will be `aws`

4. **endoflife.date product name** (from Step 1)
   - Examples: `amazon-aurora-postgresql`, `opensearch`, `amazon-rds-mysql`

---

### Step 3: Detect Wiz CSV Schema

Look at existing Wiz inventory test fixtures to understand CSV schema:

```bash
# Find existing CSV fixtures
find pkg/inventory/wiz/testdata -name "*.csv" -type f

# Examine CSV header
head -2 pkg/inventory/wiz/testdata/aurora.csv
head -2 pkg/inventory/wiz/testdata/eks.csv
head -2 pkg/inventory/wiz/testdata/elasticache.csv
```

**Common field mappings** (used across all resources):
- `externalId` → Resource ID (ARN for AWS)
- `name` → Resource name
- `versionDetails.version` → Current version
- `region` → Cloud region
- `cloudAccount.externalId` → Account ID
- `tags` → Extract service/brand/env metadata
- `nativeType` → Used for filtering (pattern matching)
- `typeFields.kind` → Engine type (e.g., "Redis", "AuroraMySQL")

**Identify the native_type_pattern**:
- Aurora: `"rds/AmazonAurora*/cluster"`
- EKS: `"eks/Cluster"`
- ElastiCache: `"elastiCache/*/cluster"`

Use similar patterns for new resources.

---

### Step 4: Check for Non-Standard Schema

**Most resources use standard endoflife.date schema** where:
- `cycle.support` → End of standard support
- `cycle.eol` → True end of life
- `cycle.extendedSupport` → End of extended support

**Known non-standard schemas** (require custom adapters):
- **EKS (amazon-eks)**: `cycle.eol` means "end of extended support" NOT true EOL
  - Use `schema: eks_adapter` in config

**Default**: Use `schema: standard` unless you know it's non-standard like EKS.

---

### Step 5: Generate YAML Config

**Generate config entry** and append to `config/resources.yaml`:

Example for OpenSearch:

```yaml
  - id: opensearch
    type: opensearch
    cloud_provider: aws
    inventory:
      source: wiz
      native_type_pattern: "opensearch/Domain"
      field_mappings:
        engine: "typeFields.kind"
        version: "versionDetails.version"
        region: "region"
        account_id: "cloudAccount.externalId"
        name: "name"
        external_id: "externalId"
    eol:
      provider: endoflife-date
      product: amazon-opensearch
      schema: standard
```

**Key points**:
- Resource `id` is the key in `WIZ_REPORT_IDS` environment variable
- Append as new entry, don't overwrite existing resources
- Use `schema: standard` unless resource has non-standard semantics (like EKS)

**Examples**: Load specific example files when:
- `examples/elasticache.yaml` - Adding cache/Redis/Valkey resources with wildcard native_type_patterns
- `examples/eks.yaml` - Adding resources requiring non-standard EOL schema adapters
- `examples/aurora-pg.yaml` - Adding RDS database resources with standard field mappings

---

### Step 6: Run Tests

Run tests to verify the config is valid:

```bash
# Test generic detector
go test ./pkg/detector/generic -v

# Test generic inventory
go test ./pkg/inventory/wiz -v

# Full test suite (optional, takes longer)
make test
```

**If tests fail**:
- Verify field mappings match Wiz CSV schema from Step 3
- Confirm native_type_pattern matches actual nativeType values in Wiz report
- Report error to user and **STOP**

**If tests pass**: Proceed to Step 7.

---

### Step 7: Create Commit

Create a properly formatted commit:

```bash
git add config/resources.yaml

git commit -m "Add {resource-type} support to Version Guard

- Added config entry with id: {resource-id}
- Uses endoflife.date product: {eol-product-name}
- Cloud provider: {cloud-provider}
- Schema: {standard|eks_adapter}

NOTE: Add Wiz report ID to WIZ_REPORT_IDS environment variable:
  '{\"resource-id\":\"wiz-report-uuid\"}'

Generated via add-version-guard-resource skill"
```

---

## Completion

After successfully adding the resource, provide a concise summary covering: the resource ID added, which endoflife.date product it uses, the schema type (standard or custom adapter), and confirmation that tests passed. Remind the user to add the Wiz report ID to their WIZ_REPORT_IDS environment variable. If a custom schema adapter is needed, mention it requires implementation in pkg/eol/endoflife/adapters.go. Keep the response brief and focused on actionable next steps.

---

## References

**Detailed Examples**: Load `references/detailed-examples.md` when you need to see how Aurora PostgreSQL, RDS MySQL, or similar resources were added end-to-end

**Troubleshooting**: Load `references/troubleshooting.md` when encountering test failures, YAML parsing errors, missing dependencies, or API connectivity issues
