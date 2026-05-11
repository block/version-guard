# Contributing to Version Guard

Thank you for your interest in contributing to Version Guard! This document provides guidelines and instructions for contributing.

## Code of Conduct

By participating in this project, you agree to maintain a respectful and inclusive environment for all contributors.

## How to Contribute

### Reporting Bugs

1. Check if the bug has already been reported in [GitHub Issues](https://github.com/block/Version-Guard/issues)
2. If not, create a new issue with:
   - Clear, descriptive title
   - Steps to reproduce
   - Expected vs actual behavior
   - Version Guard version, Go version, OS
   - Relevant logs or error messages

### Suggesting Features

1. Check [GitHub Discussions](https://github.com/block/Version-Guard/discussions) for existing feature requests
2. Create a new discussion with:
   - Clear description of the feature
   - Use case and benefits
   - Potential implementation approach (if applicable)

### Pull Requests

1. **Fork the repository** and create a feature branch:
   ```bash
   git checkout -b feature/my-new-feature
   ```

2. **Make your changes**:
   - Write clear, concise code
   - Follow existing code style and patterns
   - Add tests for new functionality
   - Update documentation as needed

3. **Test your changes**:
   ```bash
   make test          # Run all tests
   make lint          # Check code quality
   make build-all     # Verify build
   ```

4. **Commit your changes**:
   - Use clear, descriptive commit messages
   - Reference relevant issues (e.g., "Fix #123: Description")

5. **Push and create a pull request**:
   ```bash
   git push origin feature/my-new-feature
   ```
   Then create a PR on GitHub with:
   - Description of changes
   - Related issues
   - Testing performed

## Development Setup

### Prerequisites

- Go 1.24+
- Docker (for local Temporal)
- Make
- AWS CLI (for S3 snapshot testing)

### Local Development

```bash
# Clone the repository
git clone https://github.com/block/Version-Guard.git
cd Version-Guard

# Install development tools
make setup

# Build binaries
make build-all

# Run tests
make test

# Start local Temporal server (in separate terminal)
make temporal

# Run the server with auto-reload
make dev
```

## Code Style

- **Go**: Follow [Effective Go](https://golang.org/doc/effective_go) and run `gofmt`
- **Linting**: Code must pass `golangci-lint` (run `make lint`)
- **Imports**: Use `goimports` for import formatting (run `make fmt-imports`)
- **Tests**: Write unit tests for new functionality (aim for >80% coverage)

## Testing Guidelines

### Unit Tests

- Place test files next to the code they test (`foo.go` → `foo_test.go`)
- Use table-driven tests where appropriate
- Mock external dependencies

Example (table-driven, mirrors the style used across `pkg/policy`,
`pkg/config`, and `pkg/eol/endoflife`):

```go
func TestPolicy_Classify(t *testing.T) {
    tests := []struct {
        name      string
        resource  *types.Resource
        lifecycle *types.VersionLifecycle
        want      types.Status
    }{
        {
            name: "past EOL classifies RED",
            // ...
        },
    }
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            got := policy.NewDefaultPolicy().Classify(tt.resource, tt.lifecycle)
            if got != tt.want {
                t.Fatalf("Classify() = %s, want %s", got, tt.want)
            }
        })
    }
}
```

### Integration Tests

- Tag integration tests with the modern build constraint —
  `//go:build integration` on the first line, optionally followed by the legacy
  `// +build integration` for older toolchains. See
  `pkg/eol/endoflife/integration_test.go` for the reference pattern.
- Require actual external dependencies (Wiz, endoflife.date, AWS, etc.) and
  are excluded from `make test` by default; run them with
  `make test-integration`.
- Document setup requirements (env vars, credentials) in the test file's
  header comment.

## Using AI Skills to Add Resources

**Recommended approach**: Version Guard includes AI agent skills that automate adding new cloud resource types. No manual configuration editing required.

### Quick Start with AI Skills

```bash
# With any AI agent that supports Agent Skills (Claude Code, Goose, Amp)
claude "Use the add-version-guard-resource skill to add OpenSearch support"
```

The AI agent will autonomously:
1. ✅ Validate product has EOL data on [endoflife.date](https://endoflife.date)
2. 📝 Gather required inputs (resource ID, Wiz report ID, display name)
3. 🔍 Auto-detect Wiz CSV schema from existing test fixtures
4. ⚙️ Generate `pkg/config/defaults/resources.yaml` entry with proper field mappings
5. 🧪 Run tests to verify configuration works
6. 📦 Create properly formatted git commit

**Time saved**: ~30-60 minutes of manual work reduced to 2-3 minutes.

### Detailed Documentation

See [SKILLS.md](SKILLS.md) for:
- Installation instructions for different AI platforms
- Detailed usage examples (OpenSearch, Aurora PostgreSQL, EKS)
- Troubleshooting guide
- Creating your own skills

---

## Adding a New Resource Type (Manual Process)

If you prefer to add resources manually (or AI skills are not available),
Version Guard is YAML-driven — most resources require **zero Go code
changes**.

1. **Add a resource block** to `pkg/config/defaults/resources.yaml` with
   `inventory.required_mappings`, `inventory.field_mappings`, and an `eol`
   section pointing at an endoflife.date product. See [USAGE.md → Runbook 1](./USAGE.md#runbook-1-onboarding-new-resource-type)
   and [TRANSFORMS.md](./TRANSFORMS.md) for the field reference.

2. **Add the Wiz report ID** to the `WIZ_REPORT_IDS` JSON map (the key must
   match the resource's `id`).

3. **Add a fixture** under `pkg/inventory/wiz/testdata/` if the new resource
   has a Wiz CSV shape not already covered by an existing fixture, and a
   `pkg/config/loader_test.go` case if the YAML parsing is non-trivial.

4. **Add a new endoflife.date schema adapter** in
   `pkg/eol/endoflife/adapters.go` *only* if the product uses non-standard
   semantics (e.g. EKS-style standard support / extended support split).

5. **Code changes are only required when** you need a non-Wiz inventory
   source or a non-endoflife.date EOL provider. In that case implement the
   `InventorySource` / `EOLProvider` interfaces and wire them into the
   per-resource maps in `cmd/server/main.go`. There is no `Detector`
   interface — the generic detection activities in
   `pkg/workflow/detection/activities.go` dispatch through those maps.

6. **Add tests** for any new code (loader cases, adapter cases, custom
   sources) and **update documentation** (README.md, ARCHITECTURE.md) as
   needed.

## Project Structure

```
Version-Guard/
├── cmd/
│   ├── server/          # Main server binary (worker + HTTP admin)
│   └── cli/             # CLI tool
├── pkg/
│   ├── types/           # Core data structures (Resource, Finding, Snapshot, Status)
│   ├── config/          # YAML loader, transforms DSL, embedded defaults
│   │   └── defaults/    # Canonical resources.yaml shipped with the binary
│   ├── policy/          # Classification policy (RED/YELLOW/GREEN/UNKNOWN)
│   ├── inventory/       # Inventory sources (Wiz generic source + mock)
│   ├── eol/             # EOL data providers (endoflife.date + schema adapters)
│   ├── registry/        # Optional registry client for service lookups
│   ├── store/           # Finding storage interface (in-memory implementation)
│   ├── snapshot/        # Snapshot builder + store (S3 / in-memory backends)
│   ├── workflow/        # Temporal workflows (orchestrator + detection)
│   ├── scan/            # Scan trigger (shared by HTTP /scan and CLI)
│   ├── schedule/        # Temporal Schedule create-or-update wiring
│   └── emitters/        # Emitter interfaces + reference logging emitter
├── charts/version-guard/  # Helm chart
├── deploy/                # Dockerfile + endoflife.date override shim
└── .github/               # GitHub Actions workflows
```

## Release Process

Releases are managed by maintainers. The container image and Helm chart are
cut from a single `vX.Y.Z` git tag by the
[`Docker & Helm` workflow](./.github/workflows/docker.yml):

1. If the chart has changed, bump `version` (and `appVersion` if the image
   changed) in
   [charts/version-guard/Chart.yaml](./charts/version-guard/Chart.yaml).
   Land the bump on `main` as a `chore(release)` PR.
2. Tag and push: `git tag -a vX.Y.Z -m "Release vX.Y.Z" && git push origin vX.Y.Z`.
   The tag's version **must equal** `charts/version-guard/Chart.yaml`'s
   `version` whenever the chart changed since the previous tag — the workflow
   fails the release otherwise.
3. The workflow then:
   - builds and pushes a multi-arch container image to
     `ghcr.io/block/Version-Guard` (semver + `latest` tags), and
   - if the chart changed since the previous `v*` tag, packages and pushes
     the Helm chart to `oci://ghcr.io/block/charts`.

There is no `CHANGELOG.md` — release notes live on the GitHub Releases page.

## Questions?

- **General questions**: Use [GitHub Discussions](https://github.com/block/Version-Guard/discussions)
- **Bug reports**: Use [GitHub Issues](https://github.com/block/Version-Guard/issues)
- **Security issues**: Report privately via GitHub's [security advisory form](https://github.com/block/Version-Guard/security/advisories/new) (do not open public issues)

Thank you for contributing to Version Guard!
