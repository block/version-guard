# Agent Instructions

`AGENTS.md` is the canonical repo guidance for agents. Keep these instructions
here and keep agent-specific entrypoints as symlinks to this file; do not edit
the symlinks separately. `CLAUDE.md` points here for Claude Code. `AGENT.md`
is a compatibility alias for singular agent-file readers; current Amp reads
`AGENTS.md` directly and also recognizes `AGENT.md` only as a fallback.

## Repo Shape

- Version Guard is a Go service for infrastructure version drift detection.
- Temporal workflows and activities are under `pkg/workflow`; keep workflow
  code deterministic and put network, clock, and storage effects in activities.
- The Helm chart lives in `charts/version-guard`; keep chart values, templates,
  and chart metadata in sync when deployment behavior changes.
- Resource definitions are config-driven in `pkg/config/defaults/resources.yaml`.
  Prefer the transforms DSL in `TRANSFORMS.md` before adding custom code for
  inventory reshaping.

## Development Workflow

- Use the existing Makefile targets instead of one-off command variants:
  `make build-all`, `make test`, `make test-ci`, `make lint`, `make fmt-all`,
  and `make check`.
- For chart changes, mirror the GitHub Actions workflow with
  `ct lint --target-branch main --charts charts/version-guard` when chart-testing
  is available.
- Keep Go tests next to the package they cover. Prefer table-driven tests for
  policy, parser, config, and transform behavior.
- Run focused package tests while iterating, then the relevant Makefile target
  before handing off.

## Metrics And Docs

- Do not claim custom application or business metrics unless the code implements
  them. The supported metrics surface is the Temporal Go SDK Prometheus/OpenMetrics
  endpoint exposed by the worker.
- Keep documentation aligned with the Makefile, docker-compose setup, Temporal
  workflow behavior, and Helm chart defaults.
