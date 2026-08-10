# Wiz Saved-Report Health Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make stale, missing, mismatched, incomplete, empty-because-broken, or schema-incompatible Wiz saved reports fail as explicit collector dependency-health errors.

**Architecture:** Enrich and validate saved-report metadata at the Wiz HTTP boundary, then cross-check the API's expected result count against the parsed CSV before caching it. Keep resource-specific schema validation in the generic parser, but run it before returning a valid zero-resource result and wrap failures with both resource and report identifiers.

**Tech Stack:** Go 1.24, Wiz GraphQL API, `encoding/json`, `encoding/csv`, `net/http`, `testify`, structured Temporal activity logging.

## Global Constraints

- Use `ReportRun.runAt`, whose verified Wiz schema description is “Date this report run (start time).”
- Derive freshness from `runIntervalHours + 6h`; all currently configured reports have a 24-hour cadence.
- Allow at most five minutes of future clock skew.
- Treat an API row count of zero plus a valid header-only CSV as healthy.
- Never include credentials or the presigned report URL in errors or logs.
- Add no configuration knobs, custom metrics, schedule changes, or deployment changes.
- Follow red-green-refactor and run focused Wiz package tests after each production change.

---

### Task 1: Validate Saved-Report Metadata and Freshness

**Files:**
- Modify: `pkg/inventory/wiz/http_client.go:16-154`
- Modify: `pkg/inventory/wiz/client.go:35-41`
- Test: `pkg/inventory/wiz/http_client_test.go:18-209`
- Test support: `pkg/inventory/wiz/fixtures_test.go:9-69`

**Interfaces:**
- Consumes: `HTTPClient.GetReport(ctx, accessToken, reportID)` and Wiz's existing GraphQL `report(id:)` query.
- Produces: `Report{ID string, Name string, DownloadURL string, LastRun time.Time, RunIntervalHours int, ExpectedRows int}` for Task 2.

- [ ] **Step 1: Write failing HTTP metadata tests**

Add a healthy response with all required metadata, and table-driven failures for null report, mismatched ID, blank name, null last run, non-completed status, blank URL, missing/invalid `runAt`, missing/non-positive `runIntervalHours`, missing/negative row count, stale run, and a run more than five minutes in the future. Build timestamps relative to one captured `now := time.Now().UTC()` so tests remain deterministic enough without adding a production clock abstraction.

Representative healthy body and assertions:

```go
runAt := time.Now().UTC().Add(-time.Hour)
body := fmt.Sprintf(`{
  "data": {"report": {
    "id": "rep-1",
    "name": "Aurora Inventory",
    "runIntervalHours": 24,
    "lastRun": {
      "status": "COMPLETED",
      "url": "https://files.example/abc.csv",
      "runAt": %q,
      "results": {"__typename": "ReportRunResultsCloudResourceV2", "rowCount": 12}
    }
  }}
}`, runAt.Format(time.RFC3339Nano))

rep, err := c.GetReport(context.Background(), "test-token", "rep-1")
require.NoError(t, err)
assert.Equal(t, runAt, rep.LastRun)
assert.Equal(t, 24, rep.RunIntervalHours)
assert.Equal(t, 12, rep.ExpectedRows)
```

Inspect the received GraphQL request in the healthy test and assert its query contains `runIntervalHours`, `runAt`, both supported result fragments, and `rowCount` aliases.

- [ ] **Step 2: Run focused tests and verify RED**

Run:

```bash
go test ./pkg/inventory/wiz -run 'TestGetReport_' -count=1
```

Expected: FAIL because the query and response model do not expose cadence, run time, or expected rows, and missing/mismatched metadata is currently accepted.

- [ ] **Step 3: Extend the report model and GraphQL query**

Change `Report` to:

```go
type Report struct {
	ID               string
	Name             string
	DownloadURL      string
	LastRun          time.Time
	RunIntervalHours int
	ExpectedRows     int
}
```

Request verified metadata and normalize supported result counts:

```graphql
report(id: $reportId) {
  id
  name
  runIntervalHours
  lastRun {
    status
    url
    runAt
    results {
      __typename
      ... on ReportRunResultsGraphQuery { rowCount: resultCount }
      ... on ReportRunResultsCloudResource { rowCount: count }
      ... on ReportRunResultsCloudResourceV2 { rowCount: count }
    }
  }
}
```

Use pointers in the wire response for nullable objects and `rowCount`, so a legitimate zero is distinguishable from missing metadata:

```go
type reportRunResponse struct {
	Status  string    `json:"status"`
	URL     string    `json:"url"`
	RunAt   time.Time `json:"runAt"`
	Results *struct {
		Type     string `json:"__typename"`
		RowCount *int   `json:"rowCount"`
	} `json:"results"`
}
```

- [ ] **Step 4: Implement minimal metadata validation**

Add constants:

```go
const (
	reportFreshnessGrace = 6 * time.Hour
	maxFutureClockSkew   = 5 * time.Minute
)
```

Validate in this order so errors are actionable and do not dereference null data:

```go
if result.Report == nil {
	return nil, errors.Errorf("report %s not found", reportID)
}
if result.Report.ID != reportID {
	return nil, errors.Errorf("report identity mismatch: requested %s, received %s", reportID, result.Report.ID)
}
if strings.TrimSpace(result.Report.Name) == "" {
	return nil, errors.Errorf("report %s has no name", reportID)
}
if result.Report.LastRun == nil {
	return nil, errors.Errorf("report %s has no last run", reportID)
}
```

Then enforce completed status, non-empty URL, non-zero run time, positive interval, supported result type, present non-negative row count, five-minute future skew, and age no greater than:

```go
maxAge := time.Duration(result.Report.RunIntervalHours)*time.Hour + reportFreshnessGrace
```

Return errors containing the report ID, observed metadata, and threshold, but never `LastRun.URL`.

- [ ] **Step 5: Update shared fixtures and verify GREEN**

Set fixture expected rows to their CSV data-row counts:

```go
AuroraReport:      &Report{ExpectedRows: 5, RunIntervalHours: 24, ...}
ElastiCacheReport: &Report{ExpectedRows: 5, RunIntervalHours: 24, ...}
LambdaReport:      &Report{ExpectedRows: 5, RunIntervalHours: 24, ...}
```

Run:

```bash
go test ./pkg/inventory/wiz -run 'TestGetReport_' -count=1
go test ./pkg/inventory/wiz -count=1
```

Expected: PASS.

- [ ] **Step 6: Commit metadata health validation**

```bash
git add pkg/inventory/wiz/http_client.go pkg/inventory/wiz/http_client_test.go \
  pkg/inventory/wiz/client.go pkg/inventory/wiz/fixtures_test.go
git commit -m "feat(wiz): validate saved report freshness"
```

### Task 2: Cross-Check CSV Completeness and Preserve Valid Zero Results

**Files:**
- Modify: `pkg/inventory/wiz/client.go:127-164`
- Test: `pkg/inventory/wiz/client_test.go:59-78`

**Interfaces:**
- Consumes: Task 1's `Report.ExpectedRows` and downloaded CSV rows.
- Produces: `Client.GetReportData(ctx, reportID) ([][]string, error)` that returns only complete, API-consistent CSV data and caches only validated rows.

- [ ] **Step 1: Replace the old empty-report test with failing completeness cases**

Add table-driven tests with per-case `Report.ExpectedRows` and CSV bodies:

```go
tests := []struct {
	name         string
	expectedRows int
	csv          string
	wantRows     int
	wantErr      string
}{
	{name: "valid zero result", expectedRows: 0, csv: WizAPIFixtures.EmptyCSVData, wantRows: 1},
	{name: "missing header", expectedRows: 0, csv: "", wantErr: "has no header"},
	{name: "broken header only", expectedRows: 5, csv: WizAPIFixtures.EmptyCSVData, wantErr: "expected 5 data rows, downloaded 0"},
	{name: "truncated data", expectedRows: 2, csv: "id,name\n1,one\n", wantErr: "expected 2 data rows, downloaded 1"},
}
```

For each case, copy the fixture report by value before changing `ExpectedRows`, so global fixtures are not mutated across tests.

- [ ] **Step 2: Run completeness tests and verify RED**

Run:

```bash
go test ./pkg/inventory/wiz -run 'TestClient_GetReportData_(CSVCompleteness|EmptyReport)' -count=1
```

Expected: FAIL because empty and mismatched downloads are currently cached and returned without validation.

- [ ] **Step 3: Implement CSV completeness validation before caching**

Immediately after `csvReader.ReadAll()`:

```go
if len(rows) == 0 {
	return nil, errors.Errorf("Wiz report %s CSV has no header", reportID)
}

actualRows := len(rows) - 1
if actualRows != report.ExpectedRows {
	return nil, errors.Errorf(
		"Wiz report %s expected %d data rows, downloaded %d",
		reportID,
		report.ExpectedRows,
		actualRows,
	)
}
```

Remove the comment claiming all header-only reports are valid. Keep validation before the cache write so unhealthy output is never cached.

- [ ] **Step 4: Run focused and package tests and verify GREEN**

Run:

```bash
go test ./pkg/inventory/wiz -run 'TestClient_GetReportData_' -count=1
go test ./pkg/inventory/wiz -count=1
```

Expected: PASS.

- [ ] **Step 5: Commit CSV completeness validation**

```bash
git add pkg/inventory/wiz/client.go pkg/inventory/wiz/client_test.go
git commit -m "feat(wiz): reject incomplete report CSVs"
```

### Task 3: Enforce Schema Health for Zero Results and Add Resource Context

**Files:**
- Modify: `pkg/inventory/wiz/helpers.go:141-165`
- Modify: `pkg/inventory/wiz/generic.go:69-105`
- Test: `pkg/inventory/wiz/generic_test.go`
- Modify: `README.md:416-438`

**Interfaces:**
- Consumes: Task 2's guarantee that every returned CSV has a header and matches the API row count.
- Produces: `GenericInventorySource.ListResources` errors containing both `resource <config ID>` and `report <report ID>` while preserving healthy empty inventories.

- [ ] **Step 1: Write failing zero-result schema and context tests**

Configure a generic source with a mock `Report{ExpectedRows: 0}` and a header-only CSV missing one required mapped column. Assert:

```go
_, err := source.ListResources(context.Background(), cfg.Type)
require.Error(t, err)
assert.Contains(t, err.Error(), `required column "versionDetails.version" not found`)
assert.Contains(t, err.Error(), "resource test-resource")
assert.Contains(t, err.Error(), "report test-report-id")
```

Add a healthy header-only case with all required columns and API expected count zero; assert an empty resource slice and no error.

- [ ] **Step 2: Run generic source tests and verify RED**

Run:

```bash
go test ./pkg/inventory/wiz -run 'TestGenericInventorySource_.*Empty' -count=1
```

Expected: FAIL because `parseWizReport` returns before header validation and errors do not include the resource config ID.

- [ ] **Step 3: Validate headers before returning an empty inventory**

In `parseWizReport`, rely on Task 2's non-empty-row guarantee, build the column index, validate every required column, then return an empty slice only after schema validation:

```go
cols := buildColumnIndex(rows[0])
for _, name := range requiredColumns {
	if !cols.hasColumn(name) {
		return nil, fmt.Errorf("required column %q not found in CSV header (have: %v)", name, rows[0])
	}
}

if len(rows) == 1 {
	return []*types.Resource{}, nil
}
```

- [ ] **Step 4: Wrap dependency-health failures with resource and report IDs**

Replace the direct `parseWizReport` return in `ListResources`:

```go
resources, err := parseWizReport(ctx, s.client, reportID, requiredColumns, filterRow, parseRow, s.logger)
if err != nil {
	return nil, errors.Wrapf(err, "Wiz dependency unhealthy for resource %s report %s", s.config.ID, reportID)
}
return resources, nil
```

This error reaches the existing Temporal workflow's structured `Failed to fetch inventory` log without exposing credentials or the download URL.

- [ ] **Step 5: Document the runtime report-health contract**

After the `WIZ_REPORT_IDS` benefits list in `README.md`, add:

```markdown
At scan time, Version Guard verifies each configured report's identity, completed
run status, schedule-based freshness, expected row count, and required CSV
columns. A header-only CSV is accepted only when Wiz reports zero results; stale,
truncated, or schema-incompatible output fails that resource's inventory fetch.
```

- [ ] **Step 6: Run focused and complete verification**

Run:

```bash
go test ./pkg/inventory/wiz -count=1
make fmt-all
make test
make lint
git diff --check
```

Expected: all tests and lint pass; formatting produces no unexplained changes; `git diff --check` emits no output.

- [ ] **Step 7: Commit schema/context behavior and documentation**

```bash
git add pkg/inventory/wiz/helpers.go pkg/inventory/wiz/generic.go \
  pkg/inventory/wiz/generic_test.go README.md
git commit -m "feat(wiz): surface report dependency health"
```

### Task 4: Final Review and Live Contract Check

**Files:**
- Review: all changes since `origin/main`

**Interfaces:**
- Consumes: Tasks 1-3.
- Produces: a review-ready branch whose query shape is proven against the live Wiz schema and whose behavior is covered by local tests.

- [ ] **Step 1: Review the complete branch diff**

Run:

```bash
git diff --stat origin/main...HEAD
git diff --check origin/main...HEAD
git diff origin/main...HEAD -- pkg/inventory/wiz README.md docs/superpowers
```

Check specifically for presigned URLs in errors, accidental credential material, acceptance of missing metadata, cache writes before validation, and tests that mutate shared fixtures.

- [ ] **Step 2: Re-run the exact GraphQL query shape read-only**

Using the existing staging Version Guard service account without printing its credentials or download URL, query one CloudResourceV2 report and the OpenSearch GraphQuery report. Confirm both return `id`, `runIntervalHours`, `lastRun.status`, `lastRun.runAt`, and aliased `lastRun.results.rowCount` without GraphQL errors.

- [ ] **Step 3: Run final verification from a clean test cache**

Run:

```bash
go clean -testcache
make test
make lint
git status --short --branch
```

Expected: tests and lint pass, and status shows only intentional committed branch changes.

- [ ] **Step 4: Commit any review fixes separately**

If review identifies a defect, write or adjust a failing regression test first, implement the smallest fix, rerun focused and full verification, then commit with a message describing the behavior fixed. If no defect is found, do not create an empty commit.
