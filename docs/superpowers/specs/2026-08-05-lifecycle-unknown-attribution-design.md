# Lifecycle UNKNOWN Attribution and Override Provenance

## Goal

Make lifecycle `UNKNOWN` findings actionable without introducing unbounded
Prometheus labels. Operators must be able to distinguish unsupported products,
missing or malformed cycles, source failures, empty inventory versions, and
classification gaps. Local endoflife.date overrides must also identify their
origin and carry machine-readable ownership and review metadata.

## Scope

This change covers the endoflife.date client and provider, detection findings,
snapshot drill-down, application metrics, the local nginx override, and local
override metadata validation. It preserves existing graceful scan behavior and
does not add engine, version, product, URL, owner, or error text to metric
labels.

Overdue override reviews produce warnings only. Invalid manifests, missing
files, malformed lifecycle data, or inconsistent metadata remain validation
errors.

## Lifecycle attribution model

Add closed string types to the lifecycle domain:

- Unknown causes: `product_not_found`, `cycle_not_found`, `source_error`,
  `malformed_cycle`, `empty_inventory_version`, `lifecycle_mismatch`,
  `indeterminate_lifecycle`, and compatibility fallback `unattributed`.
- Data sources: `endoflife_date`, `local_override`, and `unknown`.

`VersionLifecycle` gains `UnknownCause` and `DataSource`. The existing `Source`
field remains the provider identity (`endoflife-date-api`) for compatibility.
`LifecycleDetails` gains optional `unknown_cause` and `data_source` fields so
each snapshot finding retains cause, source, engine, and version together.

Provider attribution takes precedence. For an `UNKNOWN` classification without
a provider cause, detection assigns the cause as follows:

1. Blank inventory version: `empty_inventory_version`.
2. Blank lifecycle version: `cycle_not_found`.
3. Lifecycle version does not match inventory version: `lifecycle_mismatch`.
4. Matching lifecycle has no RED, YELLOW, or GREEN signal:
   `indeterminate_lifecycle`.
5. An old or invalid payload that cannot be classified: `unattributed`.

Detection annotates a copy of the lifecycle value. Cached provider lifecycle
pointers are never mutated.

## Client and provider flow

The endoflife.date client returns product cycles plus bounded response metadata:
data source and fetch timestamp. The direct upstream client defaults to
`endoflife_date`; a custom endpoint defaults to `unknown`. A trusted
`X-Version-Guard-EOL-Source` response header may select `endoflife_date` or
`local_override`; arbitrary values normalize to `unknown`.

Response metadata is retained on errors. The provider maps outcomes as follows:

| Outcome | Cause |
| --- | --- |
| Product HTTP 404 | `product_not_found` |
| Successful response without a matching cycle | `cycle_not_found` |
| Transport, non-404 HTTP, or response decode failure | `source_error` |
| Matching cycle fails lifecycle validation/adaptation | `malformed_cycle` |

Malformed-cycle attribution is precise: the provider tracks rejected cycle
identifiers and emits `malformed_cycle` only when a rejected cycle matches the
requested inventory version. An unrelated malformed cycle does not change a
missing version from `cycle_not_found`.

Providers may return partial lifecycle diagnostics with a non-nil error.
`FetchEOLData` retains that lifecycle while logging the error. If another
provider returns only an error, the activity creates a bounded `source_error`
lifecycle instead of dropping the lookup entirely.

## Override source and provenance

The nginx override adds `X-Version-Guard-EOL-Source: local_override` when a
static override file is served and `endoflife_date` when a request is proxied.
It hides any upstream copy of that header before setting its own value.

`deploy/endoflife-override/manifest.json` contains one entry per override:

- `product`
- `path`
- `reason`
- `owner`
- `source_url`
- `reviewed_on`
- `review_due_on`

The manifest has `schema_version: 1`. Validation requires unique products and
paths, HTTPS source URLs, strict `YYYY-MM-DD` dates, a review due date no more
than 30 calendar days after review, a one-to-one relationship between manifest
entries and `api/*.json`, and valid lifecycle arrays with non-empty cycle IDs.
The UTC due date itself is valid. A date after `review_due_on` emits a warning
but does not fail tests or CI.

Validation is local and deterministic apart from the injected current date. It
does not call upstream URLs. Review means confirming whether the upstream source
has landed or changed and whether the local JSON remains necessary and accurate.

## Metrics

Keep the existing `version_guard_detection_resources` metric unchanged and add:

- `version_guard_detection_unknown_resources{resource_type,cause}`: latest
  count of UNKNOWN findings by closed cause.
- `version_guard_detection_lifecycle_resources{resource_type,source}`: latest
  count of findings by closed lifecycle data source.

All known cause and source series are reset to zero on every resource-type scan
before observed values are recorded, preventing stale gauge values. Empty or
invalid UNKNOWN causes normalize to `unattributed`; empty or invalid sources
normalize to `unknown`.

The detailed drill-down remains in snapshot findings. No aggregate
engine/version report or metric is added.

## Compatibility

Activity names, workflow ordering, and activity input/output types remain
unchanged. New fields travel through the existing lifecycle map and finding EOL
block. They are additive and zero-value compatible with old Temporal payloads,
so no workflow version patch is required.

The snapshot remains schema `v4`: the optional fields extend the existing `eol`
object and do not alter the top-level contract. Existing `Source` values remain
unchanged.

## Verification

Tests cover:

- Product 404, missing cycle, source error, malformed matching cycle, and blank
  inventory version.
- Lifecycle mismatch, indeterminate lifecycle, provider-cause precedence, and
  compatibility fallback.
- Upstream, local-override, custom/unknown, and invalid-header source handling.
- Cause/source propagation into findings and snapshot JSON.
- Exact metric labels and counts, including zero-reset behavior.
- Manifest parsing, duplicate or missing entries/files, invalid URLs or dates,
  review intervals over 30 days, and overdue warning behavior.
- Nginx configuration contract for local and proxied source headers.

Focused package tests run during implementation, followed by `make test` and the
repository's relevant format/lint checks before handoff.
