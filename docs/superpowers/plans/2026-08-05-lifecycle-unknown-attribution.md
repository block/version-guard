# Lifecycle UNKNOWN Attribution Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Attribute every lifecycle `UNKNOWN` finding to a bounded actionable cause, expose safe aggregate metrics and snapshot drill-down, and govern local endoflife.date overrides with source and review metadata.

**Architecture:** Add closed cause/source values to the existing lifecycle object, enrich the endoflife.date client result with bounded response metadata, and preserve partial diagnostics through the provider and detection activity. Aggregate only cause and source in Prometheus; retain engine/version detail in the existing snapshot finding. Nginx marks local versus proxied responses, while a standalone manifest validator enforces override metadata structure and warns—without failing—when review is overdue.

**Tech Stack:** Go 1.24, Temporal Go SDK payloads, Prometheus client_golang, nginx, JSON, testify, standard `testing`/`httptest`.

## Global Constraints

- Preserve all Temporal workflow/activity names, ordering, and input/output types.
- Keep snapshot schema `v4`; new lifecycle fields are optional additions inside `eol`.
- Keep existing `VersionLifecycle.Source` semantics and value `endoflife-date-api`.
- Never put engine, version, product, URL, owner, or error text in Prometheus labels.
- Normalize arbitrary or absent causes to `unattributed` and data sources to `unknown`.
- Never mutate lifecycle pointers returned from the provider cache.
- An overdue override review emits a warning only; malformed or inconsistent override metadata is an error.
- Use focused package tests while iterating, then repository Makefile targets.

## File Structure

- `pkg/types/resource.go`: closed lifecycle cause/source types and lifecycle fields.
- `pkg/types/lifecycle_details.go`: additive snapshot-facing propagation.
- `pkg/policy/default.go`: pure UNKNOWN cause fallback based on classification semantics.
- `pkg/eol/endoflife/client.go`: product response envelope and trusted source-header handling.
- `pkg/eol/endoflife/provider.go`: product/cycle/source failure attribution and precise malformed-cycle tracking.
- `pkg/eol/provider.go`: document partial diagnostic lifecycle returns.
- `pkg/workflow/detection/activities.go`: preserve partial failures, avoid empty-version provider calls, annotate lifecycle copies, and aggregate breakdowns.
- `pkg/telemetry/metrics.go`: bounded cause/source gauges with stale-series reset.
- `deploy/endoflife-override/nginx.conf`: authoritative response-origin header.
- `deploy/endoflife-override/manifest.json`: override ownership, source, and review metadata.
- `deploy/endoflife-override/manifest.go`: deterministic parser and validator.
- `deploy/endoflife-override/*_test.go`: manifest and nginx contract tests.
- Existing package tests: lock client, provider, policy, detection, metrics, and snapshot behavior.

---

### Task 1: Define and propagate the lifecycle attribution contract

**Files:**
- Modify: `pkg/types/resource.go`
- Modify: `pkg/types/lifecycle_details.go`
- Modify: `pkg/types/resource_test.go`
- Modify: `pkg/policy/default.go`
- Modify: `pkg/policy/default_test.go`

**Interfaces:**
- Produces: `types.LifecycleUnknownCause`, `types.LifecycleDataSource`, `types.KnownLifecycleUnknownCauses()`, `types.KnownLifecycleDataSources()`, and `policy.UnknownCause(resource, lifecycle, status)`.
- Consumed by: endoflife.date client/provider, detection, telemetry, and snapshot tasks.

- [ ] **Step 1: Write failing lifecycle propagation tests**

Add tests that create a `VersionLifecycle` with provider source, data source, and
cause, then assert `LifecycleDetailsFromVersionLifecycle` preserves all three:

```go
func TestLifecycleDetailsFromVersionLifecycle_PropagatesAttribution(t *testing.T) {
	lifecycle := &VersionLifecycle{
		Source:       "endoflife-date-api",
		DataSource:   LifecycleDataSourceLocalOverride,
		UnknownCause: LifecycleUnknownCauseCycleNotFound,
	}

	details := LifecycleDetailsFromVersionLifecycle(lifecycle)

	assert.Equal(t, "endoflife-date-api", details.Source)
	assert.Equal(t, LifecycleDataSourceLocalOverride, details.DataSource)
	assert.Equal(t, LifecycleUnknownCauseCycleNotFound, details.UnknownCause)
}
```

Add table tests asserting the known-value functions return every enum exactly
once and in stable order.

- [ ] **Step 2: Run the type tests and verify red state**

Run: `go test ./pkg/types -run 'TestLifecycleDetailsFromVersionLifecycle_PropagatesAttribution|TestKnownLifecycle' -count=1`

Expected: compile failure because attribution types and fields do not exist.

- [ ] **Step 3: Implement the closed domain values and propagation**

Add these definitions to `pkg/types/resource.go`:

```go
type LifecycleUnknownCause string

const (
	LifecycleUnknownCauseProductNotFound       LifecycleUnknownCause = "product_not_found"
	LifecycleUnknownCauseCycleNotFound         LifecycleUnknownCause = "cycle_not_found"
	LifecycleUnknownCauseSourceError           LifecycleUnknownCause = "source_error"
	LifecycleUnknownCauseMalformedCycle        LifecycleUnknownCause = "malformed_cycle"
	LifecycleUnknownCauseEmptyInventoryVersion LifecycleUnknownCause = "empty_inventory_version"
	LifecycleUnknownCauseLifecycleMismatch     LifecycleUnknownCause = "lifecycle_mismatch"
	LifecycleUnknownCauseIndeterminate         LifecycleUnknownCause = "indeterminate_lifecycle"
	LifecycleUnknownCauseUnattributed          LifecycleUnknownCause = "unattributed"
)

type LifecycleDataSource string

const (
	LifecycleDataSourceEndOfLifeDate LifecycleDataSource = "endoflife_date"
	LifecycleDataSourceLocalOverride LifecycleDataSource = "local_override"
	LifecycleDataSourceUnknown       LifecycleDataSource = "unknown"
)

func KnownLifecycleUnknownCauses() []LifecycleUnknownCause {
	return []LifecycleUnknownCause{
		LifecycleUnknownCauseProductNotFound,
		LifecycleUnknownCauseCycleNotFound,
		LifecycleUnknownCauseSourceError,
		LifecycleUnknownCauseMalformedCycle,
		LifecycleUnknownCauseEmptyInventoryVersion,
		LifecycleUnknownCauseLifecycleMismatch,
		LifecycleUnknownCauseIndeterminate,
		LifecycleUnknownCauseUnattributed,
	}
}

func KnownLifecycleDataSources() []LifecycleDataSource {
	return []LifecycleDataSource{
		LifecycleDataSourceEndOfLifeDate,
		LifecycleDataSourceLocalOverride,
		LifecycleDataSourceUnknown,
	}
}
```

Add `DataSource` and `UnknownCause` to `VersionLifecycle`. Add these fields to
`LifecycleDetails` and copy them in `LifecycleDetailsFromVersionLifecycle`:

```go
DataSource   LifecycleDataSource   `json:"data_source,omitempty"`
UnknownCause LifecycleUnknownCause `json:"unknown_cause,omitempty"`
```

- [ ] **Step 4: Write failing policy attribution tests**

Add a table-driven test covering provider-cause precedence, blank inventory
version, empty lifecycle version, mismatch, indeterminate lifecycle,
non-UNKNOWN status, and nil lifecycle:

```go
func TestUnknownCause(t *testing.T) {
	tests := []struct {
		name      string
		resource  *types.Resource
		lifecycle *types.VersionLifecycle
		status    types.Status
		want      types.LifecycleUnknownCause
	}{
		{
			name: "provider cause wins",
			resource: &types.Resource{CurrentVersion: "8.0.35"},
			lifecycle: &types.VersionLifecycle{UnknownCause: types.LifecycleUnknownCauseProductNotFound},
			status: types.StatusUnknown,
			want: types.LifecycleUnknownCauseProductNotFound,
		},
		{
			name: "empty inventory version",
			resource: &types.Resource{CurrentVersion: " "},
			lifecycle: &types.VersionLifecycle{},
			status: types.StatusUnknown,
			want: types.LifecycleUnknownCauseEmptyInventoryVersion,
		},
		{
			name: "cycle absent",
			resource: &types.Resource{CurrentVersion: "8.0.35"},
			lifecycle: &types.VersionLifecycle{},
			status: types.StatusUnknown,
			want: types.LifecycleUnknownCauseCycleNotFound,
		},
		{
			name: "lifecycle mismatch",
			resource: &types.Resource{CurrentVersion: "8.0.35"},
			lifecycle: &types.VersionLifecycle{Version: "5.7"},
			status: types.StatusUnknown,
			want: types.LifecycleUnknownCauseLifecycleMismatch,
		},
		{
			name: "indeterminate lifecycle",
			resource: &types.Resource{CurrentVersion: "8.0.35"},
			lifecycle: &types.VersionLifecycle{Version: "8.0"},
			status: types.StatusUnknown,
			want: types.LifecycleUnknownCauseIndeterminate,
		},
		{
			name: "known status has no cause",
			resource: &types.Resource{CurrentVersion: "8.0.35"},
			lifecycle: &types.VersionLifecycle{Version: "8.0", IsSupported: true},
			status: types.StatusGreen,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, UnknownCause(tt.resource, tt.lifecycle, tt.status))
		})
	}
}
```

Nil lifecycle with UNKNOWN must return `unattributed`, not panic.

- [ ] **Step 5: Run the policy test and verify red state**

Run: `go test ./pkg/policy -run TestUnknownCause -count=1`

Expected: compile failure because `UnknownCause` does not exist.

- [ ] **Step 6: Implement the pure policy helper**

Add to `pkg/policy/default.go`, reusing the package-private `versionMatches`:

```go
func UnknownCause(
	resource *types.Resource,
	lifecycle *types.VersionLifecycle,
	status types.Status,
) types.LifecycleUnknownCause {
	if status != types.StatusUnknown {
		return ""
	}
	if lifecycle != nil && lifecycle.UnknownCause != "" {
		return lifecycle.UnknownCause
	}
	if resource == nil || lifecycle == nil {
		return types.LifecycleUnknownCauseUnattributed
	}
	if strings.TrimSpace(resource.CurrentVersion) == "" {
		return types.LifecycleUnknownCauseEmptyInventoryVersion
	}
	if strings.TrimSpace(lifecycle.Version) == "" {
		return types.LifecycleUnknownCauseCycleNotFound
	}
	if !versionMatches(lifecycle.Version, resource.CurrentVersion) {
		return types.LifecycleUnknownCauseLifecycleMismatch
	}
	return types.LifecycleUnknownCauseIndeterminate
}
```

- [ ] **Step 7: Run focused tests and commit**

Run: `go test ./pkg/types ./pkg/policy -count=1`

Expected: PASS.

```bash
git add pkg/types/resource.go pkg/types/lifecycle_details.go pkg/types/resource_test.go pkg/policy/default.go pkg/policy/default_test.go
git commit -m "feat: define lifecycle unknown attribution"
```

---

### Task 2: Enrich endoflife.date client responses with bounded source metadata

**Files:**
- Modify: `pkg/eol/endoflife/client.go`
- Modify: `pkg/eol/endoflife/client_test.go`
- Modify: `pkg/eol/endoflife/mock_client.go`
- Modify: provider tests and fixtures that implement `Client`

**Interfaces:**
- Consumes: `types.LifecycleDataSource` from Task 1.
- Produces: `ProductCyclesResult{Cycles, DataSource, FetchedAt}` and `EOLSourceHeader`.
- Consumed by: `Provider.ListAllVersions` and provider diagnostic attribution in Task 3.

- [ ] **Step 1: Write failing client source tests**

Extend client tests with these cases:

```go
func TestRealHTTPClient_ProductCyclesResultSource(t *testing.T) {
	tests := []struct {
		name       string
		baseURL    func(string) string
		header     string
		wantSource types.LifecycleDataSource
	}{
		{
			name: "custom endpoint with local override header",
			baseURL: func(serverURL string) string { return serverURL },
			header: "local_override",
			wantSource: types.LifecycleDataSourceLocalOverride,
		},
		{
			name: "custom endpoint without header",
			baseURL: func(serverURL string) string { return serverURL },
			wantSource: types.LifecycleDataSourceUnknown,
		},
		{
			name: "invalid source header",
			baseURL: func(serverURL string) string { return serverURL },
			header: "attacker-controlled-value",
			wantSource: types.LifecycleDataSourceUnknown,
		},
	}

	// Each server returns [] with the optional source header. Assert the
	// result source and that FetchedAt is non-zero.
}
```

Update the typed 404 test to assert that result metadata remains available with
the error. Add a direct-constructor unit test against a rewritten test transport
so `NewRealHTTPClient` defaults to `endoflife_date` without making a network
request.

- [ ] **Step 2: Run client tests and verify red state**

Run: `go test ./pkg/eol/endoflife -run 'TestRealHTTPClient_(ProductCyclesResultSource|404ReturnsTypedError)' -count=1`

Expected: compile failures because `GetProductCycles` still returns a slice.

- [ ] **Step 3: Implement the result envelope and trusted header parser**

Change the client contract:

```go
const EOLSourceHeader = "X-Version-Guard-EOL-Source"

type ProductCyclesResult struct {
	Cycles     []*ProductCycle
	FetchedAt  time.Time
	DataSource types.LifecycleDataSource
}

type Client interface {
	GetProductCycles(ctx context.Context, product string) (ProductCyclesResult, error)
}
```

Add a `defaultDataSource` field to `RealHTTPClient`. The default constructor uses
`endoflife_date`; `NewRealHTTPClientWithConfig` uses `unknown` unless `baseURL`
is empty and falls back to the direct upstream URL.

Normalize only known header values:

```go
func lifecycleDataSource(value string, fallback types.LifecycleDataSource) types.LifecycleDataSource {
	switch types.LifecycleDataSource(strings.ToLower(strings.TrimSpace(value))) {
	case types.LifecycleDataSourceEndOfLifeDate:
		return types.LifecycleDataSourceEndOfLifeDate
	case types.LifecycleDataSourceLocalOverride:
		return types.LifecycleDataSourceLocalOverride
	default:
		return fallback
	}
}
```

Create the result before performing the request. Once a response exists, update
its source from the header. Return the result alongside every error, including
404, body read, status, and decode errors.

- [ ] **Step 4: Update mocks and call sites to the new interface**

Change `MockClient.GetProductCyclesFunc` and all inline test clients from:

```go
func(context.Context, string) ([]*ProductCycle, error)
```

to:

```go
func(context.Context, string) (ProductCyclesResult, error)
```

Successful fixtures return:

```go
return ProductCyclesResult{
	Cycles: cycles,
	DataSource: types.LifecycleDataSourceEndOfLifeDate,
	FetchedAt: time.Now(),
}, nil
```

Do not alter provider semantics in this task beyond compiling against
`result.Cycles`; Task 3 adds attribution.

- [ ] **Step 5: Run focused tests and commit**

Run: `go test ./pkg/eol/endoflife -count=1`

Expected: PASS with existing provider behavior preserved.

```bash
git add pkg/eol/endoflife/client.go pkg/eol/endoflife/client_test.go pkg/eol/endoflife/mock_client.go pkg/eol/endoflife/*_test.go
git commit -m "feat: report lifecycle response source"
```

---

### Task 3: Attribute provider outcomes precisely

**Files:**
- Modify: `pkg/eol/provider.go`
- Modify: `pkg/eol/endoflife/provider.go`
- Modify: `pkg/eol/endoflife/provider_test.go`
- Modify: `pkg/eol/endoflife/provider_404_test.go`

**Interfaces:**
- Consumes: `ProductCyclesResult`, lifecycle cause/source types.
- Produces: provider lifecycles carrying `product_not_found`, `cycle_not_found`, `malformed_cycle`, or `source_error`, including partial diagnostic lifecycle plus error.
- Consumed by: detection activity in Task 4.

- [ ] **Step 1: Write failing provider attribution tests**

Add or extend tests for:

```go
func TestProvider_GetVersionLifecycle_Product404(t *testing.T) {
	// Mock returns a local_override result plus wrapped ErrProductNotFound.
	// Assert no final error, empty Version, product_not_found, local_override,
	// provider Source, and preserved FetchedAt.
}

func TestProvider_VersionNotFound(t *testing.T) {
	// Successful cycles do not match 99.99.
	// Assert cycle_not_found and endoflife_date.
}

func TestProvider_SourceErrorReturnsDiagnosticLifecycle(t *testing.T) {
	// Mock returns metadata plus a 500 error.
	// Assert lifecycle is non-nil, cause is source_error, source is preserved,
	// and error remains non-nil.
}

func TestProvider_MalformedMatchingCycle(t *testing.T) {
	// A cycle "8.0" has eol: "not-a-date" and requested version is 8.0.35.
	// Assert malformed_cycle. Add a second malformed unrelated cycle and
	// request 9.0; assert cycle_not_found.
}
```

Add a valid-cycle-wins case where malformed `8` and valid `8.0` can both prefix
match `8.0.35`; expect the valid lifecycle.

- [ ] **Step 2: Run provider tests and verify red state**

Run: `go test ./pkg/eol/endoflife -run 'TestProvider_(GetVersionLifecycle_Product404|VersionNotFound|SourceErrorReturnsDiagnosticLifecycle|MalformedMatchingCycle)' -count=1`

Expected: assertion failures because causes/source diagnostics are absent.

- [ ] **Step 3: Extend cached product metadata**

Change the cache entry to retain source/fetch/cause and malformed cycle IDs:

```go
type cachedVersions struct {
	versions       []*types.VersionLifecycle
	malformedCycles []string
	fetchedAt      time.Time
	dataSource     types.LifecycleDataSource
	productCause   types.LifecycleUnknownCause
}
```

Return a copy of the matching valid lifecycle with response metadata applied.
For an absent match, inspect `malformedCycles` with the same exact/prefix match
rules used for valid cycles and return a new diagnostic lifecycle.

- [ ] **Step 4: Add narrow ProductCycle validation**

Before adapter conversion, reject nil cycles, blank cycle IDs, and invalid
date-or-boolean strings for `support`, `eol`, `extendedSupport`, and `lts`:

```go
func validateProductCycle(cycle *ProductCycle) error {
	if cycle == nil {
		return errors.New("cycle is nil")
	}
	if strings.TrimSpace(cycle.Cycle) == "" {
		return errors.New("cycle identifier is empty")
	}
	for name, value := range map[string]any{
		"support": cycle.Support,
		"eol": cycle.EOL,
		"extendedSupport": cycle.ExtendedSupport,
		"lts": cycle.LTS,
	} {
		if err := validateDateOrBoolean(value); err != nil {
			return errors.Wrapf(err, "%s for cycle %q", name, cycle.Cycle)
		}
	}
	return nil
}
```

`validateDateOrBoolean` accepts nil, booleans, empty strings, `"true"`,
`"false"`, and strict `YYYY-MM-DD`; it rejects all other types and strings.
Record a rejected non-empty cycle ID instead of adding it to valid versions.

- [ ] **Step 5: Preserve partial source failures and product 404 metadata**

For 404, cache an empty entry with `product_not_found` and return it as graceful
UNKNOWN. For non-404 errors, return a lifecycle and the wrapped error:

```go
return &types.VersionLifecycle{
	Engine:       engine,
	Source:       p.Name(),
	DataSource:   result.DataSource,
	FetchedAt:    result.FetchedAt,
	UnknownCause: types.LifecycleUnknownCauseSourceError,
}, errors.Wrapf(err, "failed to fetch cycles for product %s", product)
```

Update the `eol.Provider` interface comment to state that implementations may
return a non-nil diagnostic lifecycle with a non-nil error and callers should
preserve it.

- [ ] **Step 6: Run focused tests and commit**

Run: `go test ./pkg/eol/... -count=1`

Expected: PASS.

```bash
git add pkg/eol/provider.go pkg/eol/endoflife/provider.go pkg/eol/endoflife/provider_test.go pkg/eol/endoflife/provider_404_test.go
git commit -m "feat: attribute lifecycle provider failures"
```

---

### Task 4: Preserve diagnostics in findings and expose bounded metrics

**Files:**
- Modify: `pkg/workflow/detection/activities.go`
- Modify: `pkg/workflow/detection/activities_test.go`
- Modify: `pkg/telemetry/metrics.go`
- Modify: `pkg/telemetry/metrics_test.go`
- Modify: `pkg/snapshot/builder_test.go`

**Interfaces:**
- Consumes: provider diagnostic lifecycle, `policy.UnknownCause`, known cause/source lists.
- Produces: finding `eol.unknown_cause`/`eol.data_source`, `RecordDetectionBreakdown(resourceType, unknownCounts, sourceCounts)`, and two bounded gauge families.

- [ ] **Step 1: Write failing detection tests**

Add a counting provider test double and cover:

```go
func TestFetchEOLData_EmptyVersionDoesNotCallProvider(t *testing.T) {
	// Resource CurrentVersion is whitespace.
	// Assert provider call count is zero and lifecycle cause is
	// empty_inventory_version with data_source unknown.
}

func TestFetchEOLData_PreservesDiagnosticLifecycleOnError(t *testing.T) {
	// Provider returns a source_error lifecycle and error.
	// Assert the lifecycle remains in VersionLifecycles.
}

func TestDetectDrift_AnnotatesLifecycleCopy(t *testing.T) {
	// Pass a mismatch lifecycle pointer.
	// Assert finding cause is lifecycle_mismatch and original pointer cause
	// remains empty.
}
```

Extend lifecycle detail propagation assertions to include source and cause.

- [ ] **Step 2: Run detection tests and verify red state**

Run: `go test ./pkg/workflow/detection -run 'Test(FetchEOLData|DetectDrift).*' -count=1`

Expected: new attribution assertions fail.

- [ ] **Step 3: Implement detection preservation and copy annotation**

In `FetchEOLData`, synthesize empty-version diagnostics before calling the
provider. On provider errors, retain a non-nil lifecycle; if nil, create:

```go
lifecycle = &types.VersionLifecycle{
	Engine:       resource.Engine,
	Source:       provider.Name(),
	DataSource:   types.LifecycleDataSourceUnknown,
	UnknownCause: types.LifecycleUnknownCauseSourceError,
}
```

In `DetectDrift`, copy before annotating:

```go
annotated := *lifecycle
status := a.Policy.Classify(resource, &annotated)
annotated.UnknownCause = policy.UnknownCause(resource, &annotated, status)
lifecycleDetails := types.LifecycleDetailsFromVersionLifecycle(&annotated)
```

For known statuses, the helper clears the cause.

- [ ] **Step 4: Write failing metric tests**

Add exact OpenMetrics expectations:

```go
func TestRecordDetectionBreakdown(t *testing.T) {
	ResetForTest()
	RecordDetectionBreakdown(
		"aurora-mysql",
		map[types.LifecycleUnknownCause]int{
			types.LifecycleUnknownCauseCycleNotFound: 2,
		},
		map[types.LifecycleDataSource]int{
			types.LifecycleDataSourceLocalOverride: 3,
		},
	)

	// CollectAndCompare asserts only resource_type,cause on the first gauge
	// and resource_type,source on the second. Every known value exists; all
	// unobserved values are zero.
}
```

Add a second-call test that records a nonzero cause, then records empty maps and
asserts the former series is zero. Add invalid/empty inputs and assert they roll
into `unattributed`/`unknown`.

- [ ] **Step 5: Run telemetry tests and verify red state**

Run: `go test ./pkg/telemetry -run TestRecordDetectionBreakdown -count=1`

Expected: compile failure because metrics and recorder do not exist.

- [ ] **Step 6: Implement bounded gauges and normalization**

Add and register:

```go
detectionUnknownResources = prometheus.NewGaugeVec(prometheus.GaugeOpts{
	Name: "version_guard_detection_unknown_resources",
	Help: "Latest Version Guard UNKNOWN resource counts by resource type and cause.",
}, []string{"resource_type", "cause"})

detectionLifecycleResources = prometheus.NewGaugeVec(prometheus.GaugeOpts{
	Name: "version_guard_detection_lifecycle_resources",
	Help: "Latest Version Guard detection resource counts by resource type and lifecycle data source.",
}, []string{"resource_type", "source"})
```

`RecordDetectionBreakdown` first sets every known cause/source to zero, then
records normalized observed values. Add both vectors to `Register` and
`ResetForTest`.

- [ ] **Step 7: Derive breakdowns from findings**

In `EmitMetrics`, build maps while calculating the existing summary:

```go
unknownCounts := make(map[types.LifecycleUnknownCause]int)
sourceCounts := make(map[types.LifecycleDataSource]int)
for _, finding := range findings {
	sourceCounts[finding.EOL.DataSource]++
	if finding.Status == types.StatusUnknown {
		unknownCounts[finding.EOL.UnknownCause]++
	}
}
telemetry.RecordDetectionSummary(input.ResourceType, summary)
telemetry.RecordDetectionBreakdown(input.ResourceType, unknownCounts, sourceCounts)
```

Normalization stays inside telemetry so all callers receive cardinality safety.

- [ ] **Step 8: Lock the additive snapshot contract**

Extend `TestBuilder_CurrentSchemaBreakWireShape` with an UNKNOWN finding and:

```go
assert.Equal(t, "cycle_not_found", eol["unknown_cause"])
assert.Equal(t, "local_override", eol["data_source"])
assert.Equal(t, "aurora-postgresql", eol["engine"])
```

Keep the expected snapshot version at `v4`.

- [ ] **Step 9: Run focused tests and commit**

Run: `go test ./pkg/workflow/detection ./pkg/telemetry ./pkg/snapshot -count=1`

Expected: PASS.

```bash
git add pkg/workflow/detection/activities.go pkg/workflow/detection/activities_test.go pkg/telemetry/metrics.go pkg/telemetry/metrics_test.go pkg/snapshot/builder_test.go
git commit -m "feat: expose lifecycle attribution metrics"
```

---

### Task 5: Add override source headers and manifest governance

**Files:**
- Modify: `deploy/endoflife-override/nginx.conf`
- Modify: `deploy/endoflife-override/README.md`
- Create: `deploy/endoflife-override/manifest.json`
- Create: `deploy/endoflife-override/manifest.go`
- Create: `deploy/endoflife-override/manifest_test.go`
- Create: `deploy/endoflife-override/nginx_test.go`

**Interfaces:**
- Consumes: `EOLSourceHeader` values `local_override` and `endoflife_date`.
- Produces: validated manifest schema version 1 and authoritative nginx source headers.

- [ ] **Step 1: Write failing manifest validation tests**

Implement tests around an internal function with injected filesystem root, UTC
date, and warning writer:

```go
func validateManifest(root string, now time.Time, warnings io.Writer) error
```

Table cases must cover:

```go
tests := []struct {
	name        string
	mutate      func(root string)
	wantErr     string
	wantWarning string
}{
	{name: "valid manifest"},
	{name: "duplicate product", mutate: duplicateProduct, wantErr: "duplicate product"},
	{name: "missing API file entry", mutate: addUnlistedAPIFile, wantErr: "has no manifest entry"},
	{name: "entry references missing file", mutate: removeReferencedFile, wantErr: "does not exist"},
	{name: "invalid source URL", mutate: useHTTPSourceURL, wantErr: "must use https"},
	{name: "invalid review date", mutate: useInvalidDate, wantErr: "YYYY-MM-DD"},
	{name: "review interval over 30 days", mutate: extendReviewInterval, wantErr: "exceeds 30 days"},
	{name: "overdue review warns", mutate: makeOverdue, wantWarning: "review overdue"},
}
```

The overdue case must assert `NoError` and warning output. Use `t.TempDir()` and
copy fixture files; never mutate repository fixtures during tests.

Add a repository-fixture test so normal `go test ./...` validates the checked-in
manifest on every CI run while preserving warn-only expiry behavior:

```go
func TestRepositoryManifest(t *testing.T) {
	var warnings bytes.Buffer
	require.NoError(t, validateManifest(".", time.Now().UTC(), &warnings))
	if warnings.Len() > 0 {
		t.Log(strings.TrimSpace(warnings.String()))
	}
}
```

- [ ] **Step 2: Run manifest tests and verify red state**

Run: `go test ./deploy/endoflife-override -run TestValidateManifest -count=1`

Expected: compile failure because validator does not exist.

- [ ] **Step 3: Add the schema-versioned manifest**

Create `manifest.json`:

```json
{
  "schema_version": 1,
  "overrides": [
    {
      "product": "amazon-aurora-mysql",
      "path": "api/amazon-aurora-mysql.json",
      "reason": "Product pending upstream inclusion",
      "owner": "@block/block-platform",
      "source_url": "https://github.com/endoflife-date/endoflife.date/pull/9534",
      "reviewed_on": "2026-08-05",
      "review_due_on": "2026-09-04"
    },
    {
      "product": "amazon-opensearch",
      "path": "api/amazon-opensearch.json",
      "reason": "Required cycles are missing upstream",
      "owner": "@block/block-platform",
      "source_url": "https://github.com/endoflife-date/endoflife.date/pull/9919",
      "reviewed_on": "2026-08-05",
      "review_due_on": "2026-09-04"
    }
  ]
}
```

- [ ] **Step 4: Implement deterministic validation**

Define private manifest structs and validate:

1. `schema_version == 1`.
2. Required strings are non-empty.
3. Products and paths are unique.
4. `source_url` parses and uses HTTPS.
5. Dates use strict `2006-01-02` and due date is not before review date or more
   than 30 days after it.
6. Every manifest path exists under root and stays under `api/`.
7. Every `api/*.json` has exactly one manifest entry and vice versa.
8. Each API file is a top-level `[]ProductCycle`; every cycle passes the same
   exported or package-shared lifecycle validation used by the provider.
9. `now.UTC()` after the due date writes a warning and does not return an error.

Avoid copying lifecycle validation logic: expose the smallest reusable
`endoflife.ValidateProductCycle` function from Task 3 and call it here.

- [ ] **Step 5: Write and pass nginx contract tests**

The test reads `nginx.conf` and asserts the local location and named upstream
location each own one trusted header statement, and the upstream location hides
incoming copies:

```go
assert.Contains(t, config, "add_header X-Version-Guard-EOL-Source local_override always;")
assert.Contains(t, config, "proxy_hide_header X-Version-Guard-EOL-Source;")
assert.Contains(t, config, "add_header X-Version-Guard-EOL-Source endoflife_date always;")
```

Then update nginx:

```nginx
location /api/ {
    root /data;
    try_files $uri @upstream;
    add_header X-Version-Guard-EOL-Source local_override always;
}

location @upstream {
    proxy_pass https://endoflife.date;
    proxy_set_header Host endoflife.date;
    proxy_set_header User-Agent "version-guard/1.0";
    proxy_ssl_server_name on;
    proxy_hide_header X-Version-Guard-EOL-Source;
    add_header X-Version-Guard-EOL-Source endoflife_date always;
}
```

- [ ] **Step 6: Update override operating documentation**

Replace the hand-maintained override table with instructions to update
`manifest.json` whenever adding, reviewing, or removing an override. State:

- review interval is at most 30 days;
- due-date expiration warns but does not fail CI;
- malformed/missing metadata still fails validation;
- source URL and owner are required;
- nginx source headers flow into snapshot findings and source metrics.

- [ ] **Step 7: Run focused tests and commit**

Run: `go test ./deploy/endoflife-override ./pkg/eol/endoflife -count=1`

Expected: PASS. The real manifest may print no warning because its due date is
in the future on 2026-08-05.

```bash
git add deploy/endoflife-override pkg/eol/endoflife/provider.go
git commit -m "feat: govern local lifecycle overrides"
```

---

### Task 6: Integrate documentation and run repository verification

**Files:**
- Modify: `README.md`
- Modify: `ARCHITECTURE.md`
- Modify: `USAGE.md`
- Modify: relevant files from Tasks 1–5 only if verification finds defects

**Interfaces:**
- Consumes: final metric names, lifecycle fields, cause/source values, and manifest workflow.
- Produces: user-facing operating contract aligned with implemented behavior.

- [ ] **Step 1: Update metric and UNKNOWN documentation**

Document both new metrics with their exact labels. Replace claims that UNKNOWN
means only “version not found” with the bounded cause list. Explain that
snapshots contain `eol.unknown_cause`, `eol.data_source`, engine, and version for
drill-down while Prometheus does not label engine/version.

- [ ] **Step 2: Update override and architecture documentation**

Document that direct upstream responses resolve to `endoflife_date`, nginx local
files to `local_override`, and untrusted/custom endpoints without the header to
`unknown`. Link to the override manifest and validation policy.

- [ ] **Step 3: Run formatting**

Run: `make fmt-all`

Expected: exit 0. Review `git diff` and ensure formatting did not alter unrelated
files.

- [ ] **Step 4: Run targeted race-sensitive packages**

Run: `go test -race ./pkg/eol/endoflife ./pkg/workflow/detection ./pkg/telemetry ./deploy/endoflife-override -count=1`

Expected: PASS with no race reports.

- [ ] **Step 5: Run repository test suite**

Run: `make test`

Expected: all packages PASS.

- [ ] **Step 6: Run repository lint/check target**

Run: `make check`

Expected: build, tests, formatting checks, and lint pass. If chart files are
unchanged, chart-testing is not required.

- [ ] **Step 7: Inspect the final diff for compatibility and scope**

Run:

```bash
git diff --check origin/main...HEAD
git diff --stat origin/main...HEAD
git status --short
```

Expected: no whitespace errors; changes are limited to the design/plan,
lifecycle attribution, metrics, override governance, tests, and aligned docs.

- [ ] **Step 8: Commit final documentation or verification fixes**

```bash
git add README.md ARCHITECTURE.md USAGE.md
git commit -m "docs: explain lifecycle unknown diagnostics"
```

If verification required code fixes, include only those related files and use a
message describing the corrected behavior.

- [ ] **Step 9: Perform one independent full-diff review before PR creation**

Review `git diff origin/main...HEAD` for cause precedence, stale metrics, cache
mutation, arbitrary label values, Temporal payload compatibility, manifest
warning semantics, and unrelated edits. Apply and verify any concrete fixes in
one final commit before pushing.
