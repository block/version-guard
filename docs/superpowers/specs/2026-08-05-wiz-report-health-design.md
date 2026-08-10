# Wiz Saved-Report Health Validation

## Goal

Fail a configured Wiz inventory dependency explicitly when its saved report is
missing, mismatched, stale, incomplete, schema-incompatible, or inconsistent
with its downloaded CSV. Preserve a legitimate zero-resource report when Wiz
metadata confirms that the completed run returned zero rows.

## Verified Wiz contract

Read-only introspection of Block's Wiz GraphQL schema on 2026-08-05 established:

- `Report.lastRun` is the latest run and has type `ReportRun`.
- `ReportRun.runAt: DateTime!` is the run start time.
- `Report.runIntervalHours: Int` is the configured scheduled cadence.
- `ReportRun.results` is a union. Version Guard's configured reports currently
  use `ReportRunResultsCloudResourceV2.count` or
  `ReportRunResultsGraphQuery.resultCount` as their CSV row count.
- All eight configured reports have a 24-hour interval.

The API result count matched parsed CSV data rows for representative reports:
Aurora MySQL (12,216), OpenSearch (279), and Lambda (21,853).

## Design

Extend the saved-report query to request the report identity, name, schedule
interval, run status, download URL, run timestamp, result type, and normalized
row count. Normalize the two supported result variants with a GraphQL
`rowCount` alias.

Validate report metadata before downloading:

1. The report exists and its returned ID exactly matches the configured ID.
2. Its name and completed-run download URL are non-empty.
3. The latest run status is `COMPLETED`.
4. `runAt`, `runIntervalHours`, and row-count metadata are present and valid.
5. The run timestamp is not materially in the future and is no older than the
   configured interval plus six hours. The current 24-hour reports therefore
   have a 30-hour freshness window, enough for schedule and collection delay
   while still detecting a missed daily run.

Carry the expected row count into CSV parsing and enforce:

- a completely empty download is invalid because it has no schema;
- API count zero plus a header-only CSV is a valid empty inventory;
- API count greater than zero plus a header-only CSV is invalid;
- any API/CSV data-row mismatch is invalid.

Required-column validation must run against the header before returning a valid
zero-resource result. This catches schema drift even when the report contains
no resources.

Failures continue through the existing inventory/activity error path. Error
messages identify the configured report without exposing credentials or the
presigned download URL. Existing structured activity logs provide the resource
context; no custom application metric is added because Version Guard's
supported metric surface is the Temporal SDK endpoint.

## Testing

Use table-driven HTTP and client tests covering:

- missing and mismatched reports;
- non-completed, stale, and future runs;
- missing or invalid required metadata;
- healthy completed reports;
- valid zero-result CSVs;
- broken header-only and completely empty downloads;
- API/CSV row-count mismatch; and
- required-column drift on an empty report.

Implementation follows red-green-refactor: each behavior test must fail for the
expected reason before production code changes are added.

## Non-goals

- Creating, editing, or rescheduling Wiz reports.
- Adding report-health configuration knobs or new custom metrics.
- Changing Version Guard's Temporal schedule or deployment configuration.
