# Schema Adapters — and why EKS still needs its own

`endoflife.date` is the single upstream source for every EOL provider in
Version Guard, but it is **not** a uniform schema. Most products use
the "standard" cycle shape; a handful use product-specific semantics
where the same field name means a different thing. The `SchemaAdapter`
interface in [adapters.go](./adapters.go) is the seam where those
deviations are absorbed so the rest of Version Guard sees a single,
canonical `types.VersionLifecycle`.

This doc exists because EKS is the kind of deviation that will silently
mis-classify clusters in production if you wire it up the "obvious"
way.

---

## The standard schema (what most products look like)

Three real-world cycle shapes are all handled by the single
`StandardSchemaAdapter`:

### 1. Plain OSS (PostgreSQL, etc.)

```json
{
  "cycle": "15",
  "support": "2027-11-09",
  "eol": "2027-11-09"
}
```

`support` = end of standard support, `eol` = true end of life. There
is no extended-support concept; past `eol` is RED, past `support` but
before `eol` (if they differ) is YELLOW (deprecated).

### 2. Aurora pattern (support + eol + extendedSupport date)

```json
{
  "cycle": "17",
  "support": "2030-02-28",
  "eol": "2030-02-28",
  "extendedSupport": "2033-02-28"
}
```

`support` = end of standard support, `extendedSupport` = end of paid
extended support and **the true terminal date**. Past `support` but
before `extendedSupport` is in extended support (YELLOW); past
`extendedSupport` is RED.

### 3. AWS ElastiCache / Aurora MySQL pattern (no `support` field)

```json
{
  "cycle": "5",
  "eol": "2026-01-31",
  "extendedSupport": "2029-01-31"
}
```

There is no `support` field. Upstream uses `eol` as shorthand for
"end of standard support" *because* there's a real terminal date in
`extendedSupport`. The adapter recognizes the AWS pattern by
`extendedSupport` being a date, and treats `eol` as the
standard-support boundary, with `extendedSupport` as both the
extended-support end **and** the true EOL date.

The same `StandardSchemaAdapter` handles all three shapes — see
`deriveBoundaries` in [adapters.go](./adapters.go) for the three-way
switch.

| `VersionLifecycle` field | Sourced from                                                                    |
| ------------------------ | ------------------------------------------------------------------------------- |
| `DeprecationDate`        | `support` if present, else `eol` when `extendedSupport` is also present (AWS pattern). |
| `ExtendedSupportEnd`     | `extendedSupport` (date), or legacy boolean `true` falling back to `eol`.        |
| `EOLDate`                | `extendedSupport` when set (true terminal); else `eol`; else nil.               |

---

## EKS — still its own adapter, but for narrower reasons now

EKS still doesn't fit the standard schema because EKS clusters
**never truly stop working** — once you're past extended support, AWS
stops issuing patches but the control plane keeps running. There's no
"true EOL" for an EKS version, only "out of AWS support".

`EKSSchemaAdapter` therefore:

1. Maps `cycle.eol` → `DeprecationDate` (end of standard support;
   start of paid extended support).
2. Maps `cycle.extendedSupport` (date) → `ExtendedSupportEnd`.
3. Hard-sets `lifecycle.EOLDate = nil` regardless of input.
4. Classifies past-extended-support as RED via
   `IsDeprecated && !IsExtendedSupport` (same effect as the standard
   adapter's terminal RED branch, but without claiming the cluster is
   dead).

The adapter also tolerates the pre-2026 shape where
`cycle.extendedSupport` was a boolean — a `true` value falls back to
`cycle.eol` as the extended-support boundary.

### Live example

For amazon-eks cycle 1.30 (`eol: 2025-07-23`,
`extendedSupport: 2026-07-23`), evaluated on 2026-04-29:

| Field                          | Value          | Source / note                                            |
| ------------------------------ | -------------- | -------------------------------------------------------- |
| `EOLDate`                      | `nil`          | always nil for EKS                                       |
| `DeprecationDate`              | `2025-07-23`   | `cycle.eol` (end of standard support)                    |
| `ExtendedSupportEnd`           | `2026-07-23`   | `cycle.extendedSupport`                                  |
| `IsExtendedSupport`            | `true`         | now is between standard-end and extended-end             |
| `IsSupported`                  | `true`         | still in paid extended support                           |
| `IsDeprecated`                 | `true`         | past standard support                                    |

→ Policy classifies as **YELLOW**.

For the same cycle past `extendedSupport`, status flips to
`IsDeprecated=true, IsExtendedSupport=false, IsSupported=false` →
**RED**, with `EOLDate` still nil.

---

## Picking the right adapter

The adapter is selected per-resource via YAML — `eol.schema` on the
resource entry, validated by the config loader at startup:

```yaml
- id: eks
  eol:
    provider: endoflife-date
    product: amazon-eks
    schema: eks_adapter        # ← EKS-only — no true EOL
```

```yaml
- id: aurora-postgresql
  eol:
    provider: endoflife-date
    product: amazon-aurora-postgresql
    schema: standard           # ← the default for almost everything,
                               #   including AWS ElastiCache/RDS/Aurora
                               #   that ship eol+extendedSupport
```

Empty `schema` defaults to `standard`. Adding a new schema means
implementing `SchemaAdapter`, registering it in `SchemaAdapters` in
[adapters.go](./adapters.go), and naming it from YAML — no Go change
in the activities or the policy layer.

---

## Adding a new adapter — the rule of thumb

If a new product cycle's fields have different semantics from the
standard ones (in any of the three shapes above), write an adapter.
Symptoms that indicate you need one:

- A field's name suggests one thing but the dates encode another (the
  EKS `eol`-isn't-EOL case).
- The product is missing a concept the standard schema relies on
  (EKS having no true EOL).
- A field is a boolean where the standard schema expects a date in a
  way that the existing `extendedSupport: true` fallback doesn't cover.

If a new product matches one of the three standard shapes, do not
write an adapter — use `standard` and move on. The point of this seam
is to keep deviations explicit and small, not to encode every product
separately.

---

## When in doubt, fetch the live cycle

```sh
curl -s https://endoflife.date/api/amazon-eks.json | jq '.[0]'
curl -s https://endoflife.date/api/amazon-elasticache-redis.json | jq '.[0]'
curl -s https://endoflife.date/api/amazon-aurora-postgresql.json | jq '.[0]'
```

Three cycles side-by-side will show you in seconds which shape you're
looking at. Match against the table at the top — if the field shapes
are one of the three standard patterns, ship it as `schema: standard`.
If not, write an adapter and add a section here.
