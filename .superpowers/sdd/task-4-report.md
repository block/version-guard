# Task 4: Final Review and Live Contract Check

## Verdict

**DONE_WITH_CONCERNS.** The implementation behavior is review-ready: the full
`origin/main...HEAD` diff has no acceptance defect, credential or presigned-URL
leak, cache-before-validation path, missing-metadata acceptance, shared-fixture
mutation, or unexplained scope expansion. No code change or commit was created.

The one concern is lint: `make lint` exits 2 via Make (underlying
`golangci-lint` findings) with 10 findings. Five are existing mainline debt and
five are attributable to this branch. These are static quality findings, not a
behavioral defect, and the task explicitly says not to change code merely for
polish or suppress lint, so they were documented rather than changed.

## Branch review

- Reviewed all 11 changed files and all six commits through
  `12234c17b79d6d3b2f25fb31867f1dde8249f564`.
- `git diff --check origin/main...HEAD`: PASS, no output.
- Download request-construction and transport errors are replaced with fixed
  messages; status errors contain only the numeric HTTP status. The presigned
  URL is not propagated through these errors.
- Report metadata validation rejects null/mismatched reports, blank names,
  missing/incomplete runs, unsupported result metadata, absent/negative row
  counts, invalid cadence, stale runs, and materially future runs before a
  download is accepted.
- CSV header and API row-count checks execute before the cache write.
- The completeness test copies `AuroraReport` by value before changing
  `ExpectedRows`; shared fixtures are not mutated.
- Required-column validation runs before the healthy empty result return.
- The two prior Minor notes remain Minor and are not acceptance defects:
  `GetReport` lacks a dedicated healthy wire-response `rowCount: 0` test, while
  zero is covered by pointer-based parsing plus the valid-zero client/generic
  tests; the two new generic mock tests omit `AssertExpectations`, though their
  required calls are exercised to produce the asserted result.
- Self-review found no behavioral fix to make. The docs and implementation are
  aligned with the specified 30-hour freshness contract and live schema.

## Live contract evidence

The check read AWS Secrets Manager secret `/services/version-guard/wiz-api`
in-process using profile `cash-utility-staging--admin` in `us-west-2`, obtained
an OAuth token in memory, and sent the branch's exact `reportDownloadQuery`
shape to `https://api.us13.app.wiz.io/graphql`. No secret, token, report ID,
report name, row-count value, or download URL was printed, saved, or logged.

Schema-safe results:

| Candidate | GraphQL errors | Result type | id | runIntervalHours | status | runAt | aliased rowCount |
|---|---:|---|---:|---:|---:|---:|---:|
| Configured CloudResourceV2 report | false | `ReportRunResultsCloudResourceV2` | present | present | present | present | present |
| OpenSearch report | false | `ReportRunResultsGraphQuery` | present | present | present | present | present |

## Verification commands and results

1. `git diff --stat origin/main...HEAD` — PASS; 11 files, 827 insertions, 69 deletions.
2. `git diff --check origin/main...HEAD` — PASS, no output.
3. `git diff origin/main...HEAD -- pkg/inventory/wiz README.md docs/superpowers` — reviewed in full.
4. `go clean -testcache && make test` — PASS; all packages passed from a clean Go test cache, including `pkg/inventory/wiz`.
5. `make lint` — **FAIL**, 10 findings, no suppression added:
   - Existing mainline debt (5):
     - `pkg/eol/endoflife/adapters.go:421:1` — `gocyclo` 17 > 15.
     - `pkg/eol/endoflife/client.go:42:1` — unused `nolint:govet` (`nolintlint`).
     - `pkg/schedule/schedule.go:65:1` — `gocyclo` 16 > 15.
     - `pkg/types/resource.go:138:14` — `fieldalignment` 480 -> 472 pointer bytes.
     - `pkg/workflow/orchestrator/workflow.go:126:1` — `gocyclo` 16 > 15.
   - Branch-attributable findings (5):
     - `pkg/inventory/wiz/client_test.go:61:13` — `fieldalignment` 56 -> 40 pointer bytes (new test table).
     - `pkg/inventory/wiz/http_client.go:88:24` — `fieldalignment` 64 -> 56 pointer bytes.
     - `pkg/inventory/wiz/http_client.go:92:11` — `fieldalignment` 24 -> 16 pointer bytes.
     - `pkg/inventory/wiz/http_client.go:99:10` — `fieldalignment` 48 -> 32 pointer bytes.
     - `pkg/inventory/wiz/http_client.go:146:1` — `GetReport` complexity 17 > 15; the function existed on main, but the added validation branches caused this finding.
6. Final pre-report `git status --short --branch`:

   ```text
   ## youssef/ccix-214-wiz-report-health...origin/main [ahead 6]
   ```

   The checkout was clean. This requested report is the only subsequently
   created untracked artifact.

## Commit and remaining concerns

- Commit created by Task 4: **none**.
- Existing HEAD remains `12234c17b79d6d3b2f25fb31867f1dde8249f564`.
- Concern: lint is not green, and half of its findings are branch-attributable.
  They do not indicate an acceptance or security defect, but should be resolved
  if a green lint gate is required before merge.

## Task 4 fix: remove branch lint regressions

### Files changed

- `pkg/inventory/wiz/client_test.go`: reordered the CSV completeness table fields for Go alignment without changing test cases or values.
- `pkg/inventory/wiz/http_client.go`: reordered the report wire fields for Go alignment and extracted ordered metadata validation into `validateReportMetadata`.

### RED lint evidence

Before the fix, `make lint` exited 2 with 10 findings. Five were branch-attributable: `client_test.go:61:13` field alignment 56 -> 40 pointer bytes; `http_client.go:88:24` field alignment 64 -> 56; `http_client.go:92:11` field alignment 24 -> 16; `http_client.go:99:10` field alignment 48 -> 32; and `http_client.go:146:1` `GetReport` complexity 17 > 15.

### Verification

- `gofmt -w pkg/inventory/wiz/http_client.go pkg/inventory/wiz/client_test.go`: PASS.
- `go test ./pkg/inventory/wiz -count=1`: PASS (`ok`, 3.655s).
- `make test`: PASS; all packages passed, including `pkg/inventory/wiz` in 4.691s.
- `make lint`: expected nonzero exit 2 with only the five documented mainline findings (`pkg/eol/endoflife/adapters.go`, `pkg/eol/endoflife/client.go`, `pkg/schedule/schedule.go`, `pkg/types/resource.go`, and `pkg/workflow/orchestrator/workflow.go`); no finding remains in branch-changed code.
- `git diff --check`: PASS, no output.

### Self-review

Validation remains in its original order and preserves every error message, the 6-hour freshness grace, 5-minute future skew, and all returned `Report` fields. The extraction receives the current UTC time once and never places the download URL in an error; the blank-URL test also asserts the presigned host is absent. `TestGetReport_InvalidMetadata` still covers every validation branch (including nulls, identity/name/status/URL/time/interval/results/type/row-count/freshness/skew), while the happy path covers the accepted metadata and returned fields. Field names, JSON tags, fixture values, and wire semantics are unchanged.
