# Lifecycle Schemas

`endoflife.date` is the single upstream source for every EOL provider in
Version Guard, but it is not a uniform schema. Most products use the
built-in `standard` schema. Products with different field semantics
should use `schema: declarative` plus an `eol.lifecycle` block in YAML.

The goal is the same pattern used by inventory transforms: product
quirks live next to the resource config, while Go provides a small set
of reusable operations.

---

## Standard Schema

Three real-world cycle shapes are handled by `StandardSchemaAdapter`.

### Plain OSS

```json
{
  "cycle": "15",
  "support": "2027-11-09",
  "eol": "2027-11-09"
}
```

`support` = end of standard support, `eol` = true end of life. There is
no extended-support concept.

### Support + Extended Support

```json
{
  "cycle": "17",
  "support": "2030-02-28",
  "eol": "2030-02-28",
  "extendedSupport": "2033-02-28"
}
```

`support` = end of standard support. `extendedSupport` = end of paid
extended support and the true terminal date. Past `support` but before
`extendedSupport` is YELLOW; past `extendedSupport` is RED.

### AWS Pattern Without `support`

```json
{
  "cycle": "5",
  "eol": "2026-01-31",
  "extendedSupport": "2029-01-31"
}
```

When `extendedSupport` is a date and `support` is absent, the standard
adapter treats `eol` as the standard-support boundary and
`extendedSupport` as both the extended-support end and true EOL.

| `VersionLifecycle` field | Standard source |
| --- | --- |
| `DeprecationDate` | `support`, else `eol` when `extendedSupport` is also present |
| `ExtendedSupportEnd` | `extendedSupport` date, or legacy boolean `true` falling back to `eol` |
| `EOLDate` | `extendedSupport` when set, else `eol`, else nil |

---

## Declarative Schema

Use `schema: declarative` when a product's field names do not match the
standard semantics. The lifecycle block maps upstream fields into
Version Guard boundaries and names the status applied in each window.

Supported field names:

- `support`
- `eol`
- `extendedSupport`

Supported actions:

- `extended_support` - supported, deprecated, `IsExtendedSupport=true`;
  policy reports this as YELLOW.
- `unsupported` - unsupported and deprecated, but not true EOL; policy
  reports this as RED.
- `eol` - true end of life; policy reports this as RED.
- `supported` - currently supported.

### EKS

EKS cycle `eol` means end of standard support, not true EOL.
`extendedSupport` is the end of paid extended support and the terminal
EOL date used in findings.

```yaml
eol:
  provider: endoflife-date
  product: amazon-eks
  schema: declarative
  lifecycle:
    deprecation_date:
      field: eol
    extended_support_end:
      field: extendedSupport
      bool_true_fallback: eol
    eol_date:
      field: extendedSupport
    deprecated_window: extended_support
    past_extended_support: unsupported
```

For amazon-eks cycle 1.30 (`eol: 2025-07-23`,
`extendedSupport: 2026-07-23`), evaluated on 2026-04-29:

| Field | Value | Source / note |
| --- | --- | --- |
| `EOLDate` | `2026-07-23` | `cycle.extendedSupport` |
| `DeprecationDate` | `2025-07-23` | `cycle.eol` |
| `ExtendedSupportEnd` | `2026-07-23` | `cycle.extendedSupport` |
| `IsExtendedSupport` | `true` | `deprecated_window: extended_support` |

Policy classifies that as YELLOW. Past `extendedSupport`, the
`EOLDate` check makes it RED. The `past_extended_support: unsupported`
fallback only matters for archived data where `extendedSupport` was a
boolean and no terminal EOL date is available.

### Lambda

Lambda uses Standard Support and Deprecated Support columns instead of
`extendedSupport`. For Version Guard's user-facing `EOLDate`, Lambda uses
the first/actionable date (`support`) so dashboards do not show AWS's later
terminal deprecated-support date as the runtime EOL.

```yaml
eol:
  provider: endoflife-date
  product: aws-lambda
  schema: declarative
  lifecycle:
    deprecation_date:
      field: support
    deprecated_support_end:
      field: eol
    deprecated_window: deprecated_support
    eol_date:
      field: support
```

For `python3.8` (`support: 2024-10-14`, `eol: 2027-03-03`), the emitted
`EOLDate` is `2024-10-14`, while `DeprecatedSupportEnd` preserves the
later AWS terminal date for downstream consumers that need it. Dates after
`support` become true EOL / RED.

---

## Adding Products

Use `schema: standard` when the product matches one of the standard
shapes. Use `schema: declarative` when the same field name means
something product-specific.

Examples that call for declarative YAML:

- A field's name suggests one thing but the dates encode another.
- The product has no true EOL, but still has an unsupported-after date.
- The product has a supported post-standard window without using the
  upstream `extendedSupport` field.

If the available lifecycle actions are not expressive enough, add a new
generic action. Avoid adding product-named adapters unless the product
needs behavior that cannot be described as boundary mapping plus window
actions.

---

## When In Doubt

Fetch the live cycle and compare the field meanings:

```sh
curl -s https://endoflife.date/api/amazon-eks.json | jq '.[0]'
curl -s https://endoflife.date/api/aws-lambda.json | jq '.[0]'
curl -s https://endoflife.date/api/amazon-elasticache-redis.json | jq '.[0]'
```
