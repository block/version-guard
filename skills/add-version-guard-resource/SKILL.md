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
test -f pkg/config/defaults/resources.yaml && echo "✅ Config schema exists" || echo "❌ Missing - see SETUP.md"
test -f pkg/config/loader.go && echo "✅ Config loader exists" || echo "❌ Missing - see SETUP.md"
test -f pkg/inventory/wiz/generic.go && echo "✅ Generic inventory source exists" || echo "❌ Missing - see SETUP.md"
test -f pkg/workflow/detection/activities.go && echo "✅ Detection activities exist" || echo "❌ Missing - see SETUP.md"
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

**If the AWS-flavored product is missing but an upstream product
exists**: use the upstream product directly. ElastiCache Valkey and
Memcached follow this pattern today — `amazon-elasticache-valkey`
and `amazon-elasticache-memcached` don't exist on endoflife.date, so
`pkg/config/defaults/resources.yaml` ships `product: valkey` and
`product: memcached` against the upstream cycles. The standard
adapter handles the upstream "support + eol" shape unchanged.

**If no relevant product exists at all** (neither AWS-flavored nor
upstream):

- **STOP** and inform user:
  ```
  endoflife.date doesn't have coverage for {resource-name} yet.

  You need to create a PR at https://github.com/endoflife-date/endoflife.date first.

  See example PR: https://github.com/endoflife-date/endoflife.date/pull/9534

  Once the PR is merged, return and use this skill to add Version Guard support.
  ```

**If product found**: Note the exact product name (e.g., `amazon-aurora-postgresql`, or upstream `valkey`) and proceed to Step 2.

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

**Mappings are split across two YAML maps**: every CSV column the
parser reads is driven by either `required_mappings` or
`field_mappings`. The key is the *logical field*; the value is the
Wiz CSV column name to read from. The split is purely for clarity —
the parser sees the union — but lets a reader of the YAML
immediately tell what's mandatory vs optional. The loader rejects
any key that appears in both maps and any empty value in
`required_mappings`.

**`required_mappings`** — every entry MUST be present and non-empty.
Validated at config load time so YAML typos fail fast at startup
rather than mid-scan. Each resource self-declares its required set:

- `resource_id` → CSV column for `Resource.ID`. **Always required**
  for every resource. Usually `"externalId"` (the ARN for most AWS
  resources). For **EKS**, map this to `"providerUniqueId"` because
  `externalId` is a Wiz-internal hash.
- `version` → CSV column for the engine/runtime version. Usually
  `"versionDetails.version"`. Required for every resource type
  *except* Lambda (where the runtime is extracted from
  `graphEntity.properties`).
- `engine` → CSV column for the engine type. Usually
  `"typeFields.kind"`. Required for resource types where engine is
  read straight from a column (Aurora, ElastiCache, RDS, …). Omit
  for resources where engine is **implicit** and produced by a
  transform (see Step 5):
  - **lambda** — `transforms.engine.constant: "aws-lambda"`.
  - **eks** — `transforms.engine.default_if_empty: "eks"` (no
    engine column in EKS reports).
  - **opensearch** — `transforms.engine.from_version_major: …`
    (Elasticsearch ≤ 7.x vs OpenSearch).

**`field_mappings`** — optional. Missing values produce empty strings
on the typed `Resource` (for typed keys: `tags`) or absent entries
in `Resource.Extra` (for non-typed keys: `name`, `account_id`,
`region`, `owner`, `cost_center`, …). Common entries (Wiz canonical
defaults shown in parens):

- `name` (`"name"`) → `Resource.Extra["name"]`
- `account_id` (`"cloudAccount.externalId"`) → `Resource.Extra["account_id"]`
- `region` (`"region"`) → `Resource.Extra["region"]`
- `tags` (`"tags"`) → JSON-encoded tags. Typed: populates
  `Resource.Tags` and is used for `service`/`env` extraction.
- Anything else (e.g. `owner: "tags.owner"`) → flows verbatim into
  `Resource.Extra` under the YAML logical name.

**Always read by the parser** (not configurable):
- `nativeType` → used to filter rows by `native_type_pattern`.

Any column referenced by `transforms.version.extract_json_field.from_column`
(e.g. `graphEntity.properties` for Lambda) is automatically added to
the required-columns list — you don't need to declare it separately.

**Identify the native_type_pattern**. The matcher (see
`pkg/inventory/wiz/generic.go`) supports either an exact match or a
pipe-delimited alternation, plus `*` only as a **whole** path
segment — partial-segment globs like `AmazonAurora*` do NOT work.
For per-engine variants where each engine has its own product /
lifecycle, prefer one config per exact pattern over a wildcard +
post-filter (see ElastiCache below).

Current patterns in `pkg/config/defaults/resources.yaml`:

| Resource | `native_type_pattern` |
|---|---|
| Aurora PostgreSQL | `"rds/AmazonAuroraPostgreSQL/cluster"` |
| Aurora MySQL | `"rds/AmazonAuroraMySQL/cluster"` |
| RDS MySQL | `"rds/MySQL/instance"` |
| RDS PostgreSQL | `"rds/PostgreSQL/instance"` |
| EKS | `"cluster"` |
| ElastiCache Redis | `"elastiCache/Redis/cluster"` |
| ElastiCache Valkey | `"elastiCache/Valkey/cluster"` |
| ElastiCache Memcached | `"elastiCache/Memcached/cluster"` |
| OpenSearch | `"elasticSearchService\|OpenSearch Domain"` |
| Lambda | `"lambda"` |

---

### Step 4: Check for Non-Standard Schema

**Most resources use `schema: standard`.** The standard adapter
already understands the three real-world cycle shapes seen on
endoflife.date:

| Shape | Example product | Cycle fields present |
|---|---|---|
| Plain OSS (no extended support) | `postgresql`, `valkey`, `memcached` | `support` + `eol` |
| Standard + extended support | `aurora-postgresql`, `rds-mysql` | `support` + `eol` + `extendedSupport` |
| AWS-without-`support` | `amazon-elasticache-redis` | `eol` + `extendedSupport` |

**Use `schema: declarative` (with an `eol.lifecycle` block) only when
the product's field semantics fall outside those three shapes.**
Currently shipped declarative resources:

- **EKS (`amazon-eks`)** — `cycle.eol` is the end of *standard*
  support, not true EOL. Past extended support, AWS keeps the
  cluster running but stops patching, so it's classified as
  `unsupported` rather than EOL.
- **Lambda (`aws-lambda`)** — uses "Standard Support" /
  "Deprecated Support" instead of `extendedSupport`. The
  deprecated-support window maps onto Version Guard's YELLOW
  extended-support state.

Inline shape (the EKS block from `resources.yaml`):

```yaml
    eol:
      provider: endoflife-date
      product: amazon-eks
      schema: declarative
      lifecycle:
        deprecation_date:
          field: eol                      # cycle.eol = end of standard support
        extended_support_end:
          field: extendedSupport
          bool_true_fallback: eol         # archived data used boolean true
        deprecated_window: extended_support
        past_extended_support: unsupported
```

Supported lifecycle field names: `support`, `eol`, `extendedSupport`.
Supported actions: `extended_support`, `unsupported`, `eol`,
`supported`.
**Full reference** with every supported field/action and the
classification semantics: [`pkg/eol/endoflife/ADAPTERS.md`](../../pkg/eol/endoflife/ADAPTERS.md).
Worked example file: [`examples/eks.yaml`](examples/eks.yaml).

Loader behavior (`pkg/config/loader.go`):
- `schema: declarative` without a `lifecycle` block → rejected.
- A `lifecycle` block without `schema:` set → loader auto-fills
  `schema: declarative`. Setting `schema: standard` alongside
  `lifecycle:` is rejected.
- A `lifecycle` block with no date sources at all → rejected.
- Lifecycle fields/actions outside the supported sets → rejected.

**Default**: use `schema: standard`. Reach for `declarative` only if
the upstream cycle data doesn't match one of the three standard
shapes above.

---

### Step 5: Decide if a transform is needed

If the raw column values for `version` and `engine` aren't already
the canonical strings endoflife.date expects, declare a `transforms`
block. The DSL is intentionally narrow — see [TRANSFORMS.md](../../TRANSFORMS.md)
for the full operation reference, anti-patterns, and examples. Quick
chooser:

| Need to … | Use |
|---|---|
| Strip a fixed engine prefix from version (e.g. `OpenSearch_2.13` → `2.13`) | `transforms.version.strip_prefixes` |
| Read version (e.g. runtime) from a JSON column | `transforms.version.extract_json_field` (set `skip_if_empty: true` to drop rows with no value) |
| Force engine to a constant for every row | `transforms.engine.constant` |
| Default engine to a constant when the column is empty / unmapped | `transforms.engine.default_if_empty` |
| Canonicalize a free-form engine column value | `transforms.engine.substring_lookup` |
| Pick engine based on the version's major (legacy/modern split) | `transforms.engine.from_version_major` |
| The raw column already matches | _no transforms block; baseline lowercases+trims engine_ |

**At most one operation per field** is allowed. The loader rejects
multiple sibling ops at startup. Don't include rules that can't
fire (a `mysql` substring rule on the postgres resource is dead).

If your case doesn't fit any existing op, **don't try to compose
existing ops or sneak conditionals into YAML**. Add a new named op
to `pkg/config/transforms.go` instead — see the "Adding a new
operation" section in TRANSFORMS.md.

---

### Step 6: Generate YAML Config

**Generate config entry** and append to `pkg/config/defaults/resources.yaml`:

Example for OpenSearch (uses both a version transform and an engine transform):

```yaml
  - id: opensearch
    type: opensearch
    cloud_provider: aws
    inventory:
      source: wiz
      native_type_pattern: "elasticSearchService|OpenSearch Domain"
      required_mappings:
        # engine is produced by transforms.engine.from_version_major,
        # so it's NOT required here.
        resource_id: "externalId"
        version: "versionDetails.version"
      field_mappings:
        name: "name"
        account_id: "cloudAccount.externalId"
        region: "region"
        tags: "tags"
    transforms:
      version:
        strip_prefixes: ["OpenSearch_", "Elasticsearch_"]
      engine:
        from_version_major:
          majors:
            "5": "elasticsearch"
            "6": "elasticsearch"
            "7": "elasticsearch"
          default: "opensearch"
    eol:
      provider: endoflife-date
      product: amazon-opensearch
      schema: standard
```

**Key points**:
- Resource `id` is the key in `WIZ_REPORT_IDS` environment variable
- Append as new entry, don't overwrite existing resources
- Use `schema: standard` unless resource has non-standard semantics (see Step 4)
- Transforms are optional — omit the block entirely when the raw column values are already canonical

**Examples**: Load specific example files when:
- `examples/elasticache.yaml` — adding a per-engine cache resource
  (Redis / Valkey / Memcached). Demonstrates the pattern of multiple
  configs reading the SAME Wiz report, narrowed by exact
  per-engine `native_type_pattern`. Does NOT use a wildcard
  pattern — folding engines together silently routes rows to the
  wrong endoflife.date product.
- `examples/eks.yaml` — adding a resource that needs `schema:
  declarative` + a YAML `lifecycle:` block (no Go adapter required).
- `examples/aurora-pg.yaml` — adding an RDS-family database resource
  with the standard field mapping shape and an `engine`
  `substring_lookup` transform.

---

### Step 7: Run Tests

Run tests to verify the config is valid:

```bash
# Test the YAML loader (validates the new resource block parses correctly)
go test ./pkg/config -v

# Test the generic inventory source (parses Wiz CSV using the new mappings)
go test ./pkg/inventory/wiz -v

# Test the detection activities (the path the orchestrator drives at runtime)
go test ./pkg/workflow/detection -v

# Full test suite (optional, takes longer)
make test
```

**If tests fail**:
- Verify field mappings match Wiz CSV schema from Step 3
- Confirm native_type_pattern matches actual nativeType values in Wiz report
- Report error to user and **STOP**

**If tests pass**: Proceed to Step 8.

---

### Step 8: Create Commit

Create a properly formatted commit:

```bash
git add pkg/config/defaults/resources.yaml

git commit -m "Add {resource-type} support to Version Guard

- Added config entry with id: {resource-id}
- Uses endoflife.date product: {eol-product-name}
- Cloud provider: {cloud-provider}
- Schema: {standard|declarative}

NOTE: Add Wiz report ID to WIZ_REPORT_IDS environment variable:
  '{\"resource-id\":\"wiz-report-uuid\"}'

Generated via add-version-guard-resource skill"
```

---

## Completion

After successfully adding the resource, provide a concise summary
covering: the resource ID added, which endoflife.date product it
uses, the schema type (`standard` or `declarative`), and confirmation
that tests passed. Remind the user to add the Wiz report ID to their
`WIZ_REPORT_IDS` environment variable. If the product needs
non-standard lifecycle semantics, mention that they're declared in
the YAML `eol.lifecycle` block (no Go change required for the cases
the declarative DSL covers — see ADAPTERS.md). A custom Go adapter
in `pkg/eol/endoflife/adapters.go` is only needed when the
declarative DSL can't express the product's semantics, which is
exceptional. Keep the response brief and focused on actionable next
steps.

---

## References

**Detailed Examples**: Load `references/detailed-examples.md` when you need to see how Aurora PostgreSQL, RDS MySQL, or similar resources were added end-to-end

**Troubleshooting**: Load `references/troubleshooting.md` when encountering test failures, YAML parsing errors, missing dependencies, or API connectivity issues
