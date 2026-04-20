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

Example:
```go
func TestDetector_Detect(t *testing.T) {
    tests := []struct {
        name    string
        input   *types.Resource
        want    *types.Finding
        wantErr bool
    }{
        {
            name: "detects red status",
            // ...
        },
    }
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            // ...
        })
    }
}
```

### Integration Tests

- Tag integration tests with `// +build integration`
- Require actual external dependencies (Wiz, AWS, etc.)
- Document setup requirements

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
4. ⚙️ Generate `config/resources.yaml` entry with proper field mappings
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

If you prefer to add resources manually (or AI skills are not available), follow these steps:

1. **Define the resource type** in `pkg/types/resource.go`:
   ```go
   const ResourceTypeYourResource ResourceType = "your-resource"
   ```

2. **Create an inventory source** in `pkg/inventory/`:
   ```go
   // pkg/inventory/wiz/your_resource.go
   type YourResourceInventorySource struct { /* ... */ }
   ```

3. **Create an EOL provider** (or use existing):
   ```go
   // pkg/eol/aws/your_resource.go or use endoflife.date
   ```

4. **Create a detector** in `pkg/detector/your_resource/`:
   ```go
   // pkg/detector/your_resource/detector.go
   type Detector struct { /* ... */ }
   ```

5. **Add tests** for all components

6. **Update documentation** (README.md, ARCHITECTURE.md)

## Project Structure

```
Version-Guard/
├── cmd/
│   ├── server/          # Main server binary
│   └── cli/             # CLI tool
├── pkg/
│   ├── types/           # Core data structures
│   ├── policy/          # Classification policies
│   ├── inventory/       # Inventory sources (Wiz, mock)
│   ├── eol/             # EOL data providers
│   ├── detector/        # Resource detectors
│   ├── store/           # Finding storage
│   ├── snapshot/        # S3 snapshot management
│   ├── workflow/        # Temporal workflows
│   ├── scan/            # Scan trigger (HTTP + CLI)
│   └── emitters/        # Emitter interfaces + examples
├── docs/                # Documentation
└── .github/             # GitHub workflows
```

## Release Process

Releases are managed by maintainers:

1. Update version in relevant files
2. Update CHANGELOG.md
3. Create a git tag: `git tag -a v1.0.0 -m "Release v1.0.0"`
4. Push tag: `git push origin v1.0.0`
5. GitHub Actions will build and publish release artifacts

## Questions?

- **General questions**: Use [GitHub Discussions](https://github.com/block/Version-Guard/discussions)
- **Bug reports**: Use [GitHub Issues](https://github.com/block/Version-Guard/issues)
- **Security issues**: Email security@block.xyz

Thank you for contributing to Version Guard!
