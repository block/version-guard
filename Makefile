# Makefile for Version Guard
.DEFAULT_GOAL := help

# Go settings
PACKAGES = $(shell go list ./... 2>/dev/null | grep -v "/vendor/")
TIMEOUT  := 300s
BINARY_NAME := version-guard

# Tool versions
TPARSE_VERSION := latest
TPARSE := $(shell go env GOPATH)/bin/tparse

# Colors for output
CYAN  := \033[36m
RESET := \033[0m

# ── Help ──────────────────────────────────────────────────────────────────────

.PHONY: help
help: ## Show available targets
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) \
	  | awk 'BEGIN {FS = ":.*?## "}; {printf "  $(CYAN)%-20s$(RESET) %s\n", $$1, $$2}'

# ── Setup ─────────────────────────────────────────────────────────────────────

.PHONY: setup
setup: ## Initial setup (install tools, setup hooks)
	@echo "🔧 Setting up development environment..."
	@chmod +x scripts/setup-hooks.sh
	@./scripts/setup-hooks.sh
	@echo "📦 Installing development tools..."
	@command -v golangci-lint > /dev/null || (echo "Installing golangci-lint..." && curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | sh -s -- -b $$(go env GOPATH)/bin)
	@command -v goimports > /dev/null || (echo "Installing goimports..." && go install golang.org/x/tools/cmd/goimports@latest)
	@command -v temporal > /dev/null || echo "⚠️  Temporal CLI not found. Install with: brew install temporal"
	@command -v grpcurl > /dev/null || echo "⚠️  grpcurl not found. Install with: brew install grpcurl"
	@echo "✅ Setup complete!"

# ── Build ─────────────────────────────────────────────────────────────────────

.PHONY: build
build: ## Build the server binary
	@echo "🔨 Building $(BINARY_NAME) server..."
	@mkdir -p bin
	@go build -o bin/$(BINARY_NAME) cmd/server/main.go
	@echo "✅ Build complete: bin/$(BINARY_NAME)"

.PHONY: build-cli
build-cli: ## Build CLI tool
	@echo "🔨 Building $(BINARY_NAME)-cli..."
	@mkdir -p bin
	@go build -o bin/$(BINARY_NAME)-cli cmd/cli/main.go
	@echo "✅ Build complete: bin/$(BINARY_NAME)-cli"

.PHONY: build-all
build-all: build build-cli ## Build both server and CLI binaries

.PHONY: build-with-protos
build-with-protos: build build-cli protos ## Build everything (binaries + protos)

# ── Protocol Buffers ──────────────────────────────────────────────────────────

.PHONY: protos
protos: ## Generate Go code from .proto files
	@echo "🔧 Generating protobuf code..."
	@if [ -d "protos" ]; then $(MAKE) -C protos; else echo "⚠️  protos/ directory not found, skipping"; fi

# ── Tests ─────────────────────────────────────────────────────────────────────

.PHONY: test
test: ## Run unit tests with race detection
	@echo "🧪 Running tests..."
	@go test $(PACKAGES) -race -timeout $(TIMEOUT)

.PHONY: test-verbose
test-verbose: ## Run unit tests with verbose output
	@echo "🧪 Running tests (verbose)..."
	@go test $(PACKAGES) -v -race -timeout $(TIMEOUT)

.PHONY: test-coverage
test-coverage: ## Run tests with coverage report
	@echo "🧪 Running tests with coverage..."
	@go test $(PACKAGES) -race -timeout $(TIMEOUT) -coverprofile=coverage.out
	@go tool cover -html=coverage.out -o coverage.html
	@echo "✅ Coverage report: coverage.html"

.PHONY: test-integration
test-integration: ## Run integration tests (requires credentials)
	@echo "🧪 Running integration tests..."
	@go test $(PACKAGES) -tags=integration -v -timeout $(TIMEOUT)

.PHONY: test-acceptance
test-acceptance: ## Run acceptance tests (requires full env)
	@echo "🧪 Running acceptance tests..."
	@go test $(PACKAGES) -tags=acceptance -v -timeout $(TIMEOUT)

.PHONY: _install-tparse
_install-tparse:
	@test -x $(TPARSE) \
	  || { echo "Installing tparse..."; \
	       cd /tmp && go install github.com/mfridman/tparse@$(TPARSE_VERSION); }

.PHONY: test-ci
test-ci: _install-tparse ## Run tests as CI does (parallel 4, race) & Tabular summary — per-package pass/fail/coverage table
	@echo "🧪 Running tests (CI mode with tparse)..."
	@go test -json $(PACKAGES) -parallel 4 -race -timeout $(TIMEOUT) | $(TPARSE) -all -progress

.PHONY: test-all
test-all: test-ci ## Full test cycle with CI-style testing

# ── Code Quality ──────────────────────────────────────────────────────────────

.PHONY: lint
lint: ## Run golangci-lint
	@echo "🔎 Running linter..."
	@golangci-lint run --timeout=5m

.PHONY: lint-fix
lint-fix: ## Run golangci-lint with auto-fix
	@echo "🔧 Running linter with auto-fix..."
	@golangci-lint run --timeout=5m --fix

.PHONY: vet
vet: ## Run go vet
	@echo "🔍 Running go vet..."
	@go vet $(PACKAGES)

.PHONY: fmt
fmt: ## Format all Go files
	@echo "📝 Formatting Go files..."
	@find . -name '*.go' -not -path './vendor/*' -not -path './protos/block/*' | xargs gofmt -l -w
	@echo "✅ Formatting complete"

.PHONY: fmt-imports
fmt-imports: ## Format imports with goimports
	@echo "📦 Formatting imports..."
	@command -v goimports >/dev/null 2>&1 || (echo "Installing goimports..." && go install golang.org/x/tools/cmd/goimports@latest)
	@find . -name '*.go' -not -path './vendor/*' -not -path './protos/block/*' | xargs goimports -local github.com/block/Version-Guard -w
	@echo "✅ Import formatting complete"

.PHONY: fmt-all
fmt-all: fmt fmt-imports ## Format code and imports

.PHONY: check
check: fmt-all lint test ## Run all checks (format, lint, test)

# ── Local Development ─────────────────────────────────────────────────────────

TEMPORAL_NAMESPACE := version-guard-dev

.PHONY: temporal
temporal: ## Start local Temporal dev server and open Web UI
	@echo "🕰️  Starting Temporal dev server (namespace: $(TEMPORAL_NAMESPACE))..."
	@echo "   Web UI: http://localhost:8233"
	@open http://localhost:8233 &
	@temporal server start-dev --namespace $(TEMPORAL_NAMESPACE) \
		--dynamic-config-value limit.blobSize.error=20000000 \
		--dynamic-config-value limit.blobSize.warn=15000000

.PHONY: dev
dev: ## Run the service locally with auto-reload on code changes
	@if [ -f .env ]; then set -a; . ./.env; set +a; fi; \
	echo "🚀 Starting Version Guard with auto-reload (Ctrl+C to stop)..."; \
	find . -name '*.go' -not -path './vendor/*' | entr -r go run ./cmd/server

.PHONY: run-locally
run-locally: build ## Run the service locally (connects to local Temporal)
	@echo "🚀 Starting Version Guard locally..."
	@CONFIG_ENV=development bin/$(BINARY_NAME)

.PHONY: run-worker
run-worker: build ## Run Temporal worker locally
	@echo "🚀 Starting Temporal worker locally..."
	@CONFIG_ENV=development bin/$(BINARY_NAME) --mode=worker

.PHONY: run-server
run-server: build ## Run gRPC server locally
	@echo "🚀 Starting gRPC server locally..."
	@CONFIG_ENV=development bin/$(BINARY_NAME) --mode=server

# ── Docker ────────────────────────────────────────────────────────────────────

.PHONY: docker-build
docker-build: ## Build Docker image
	@echo "🐳 Building Docker image..."
	@docker build -t block/version-guard:latest -f deploy/Dockerfile .
	@echo "✅ Docker image built: block/version-guard:latest"

.PHONY: docker-run
docker-run: docker-build ## Run Docker container locally
	@echo "🐳 Running Docker container..."
	@docker run -p 8080:8080 --env-file .env block/version-guard:latest

# ── Deployment ────────────────────────────────────────────────────────────────

.PHONY: deploy-dev
deploy-dev: ## Deploy to development environment
	@echo "🚀 Deploying to development..."
	@kubectl apply -f deployments/dev/

.PHONY: deploy-staging
deploy-staging: ## Deploy to staging environment
	@echo "🚀 Deploying to staging..."
	@kubectl apply -f deployments/staging/

.PHONY: deploy-prod
deploy-prod: ## Deploy to production environment
	@echo "⚠️  Deploying to PRODUCTION..."
	@read -p "Are you sure? [y/N] " -n 1 -r; \
	echo; \
	if [[ $$REPLY =~ ^[Yy]$$ ]]; then \
		kubectl apply -f deployments/prod/; \
	else \
		echo "Deployment cancelled."; \
	fi

# ── Dependencies ──────────────────────────────────────────────────────────────

.PHONY: tidy
tidy: ## Tidy go.mod
	@echo "📦 Tidying go.mod..."
	@go mod tidy
	@echo "✅ go.mod tidied"

.PHONY: deps
deps: ## Download dependencies
	@echo "📦 Downloading dependencies..."
	@go mod download

.PHONY: vendor
vendor: ## Vendor dependencies
	@echo "📦 Vendoring dependencies..."
	@go mod vendor

# ── Cleanup ───────────────────────────────────────────────────────────────────

.PHONY: clean
clean: ## Clean build artifacts
	@echo "🧹 Cleaning build artifacts..."
	@rm -rf bin/
	@rm -rf coverage.out coverage.html
	@go clean ./...
	@echo "✅ Clean complete"

.PHONY: clean-all
clean-all: clean ## Clean everything including vendor
	@echo "🧹 Cleaning everything..."
	@rm -rf vendor/

# ── Git Hooks ─────────────────────────────────────────────────────────────────

.PHONY: install-hooks
install-hooks: ## Install Git hooks
	@./scripts/setup-hooks.sh

.PHONY: test-hooks
test-hooks: ## Test pre-push hook without pushing
	@echo "🧪 Testing pre-push hook..."
	@./.git-hooks/pre-push

# ── Development Helpers ───────────────────────────────────────────────────────

.PHONY: watch
watch: ## Watch for changes and rebuild (requires entr)
	@echo "👀 Watching for changes..."
	@find . -name '*.go' | entr -r make build

.PHONY: todo
todo: ## List all TODO/FIXME/HACK comments
	@echo "📋 Listing TODOs..."
	@grep -rn "TODO\|FIXME\|HACK" --include="*.go" --exclude-dir=vendor --exclude-dir=protos . || echo "No TODOs found!"

.PHONY: lines
lines: ## Count lines of code
	@echo "📏 Counting lines of code..."
	@find . -name '*.go' -not -path './vendor/*' -not -path './protos/block/*' | xargs wc -l | tail -1

# ── Database (Future) ─────────────────────────────────────────────────────────

.PHONY: db-migrate
db-migrate: ## Run database migrations (future)
	@echo "⚠️  Database migrations not yet implemented"

.PHONY: db-rollback
db-rollback: ## Rollback database migration (future)
	@echo "⚠️  Database rollback not yet implemented"

# ── CI Helpers ────────────────────────────────────────────────────────────────

.PHONY: ci
ci: check ## Run all CI checks (same as check)

.PHONY: ci-coverage
ci-coverage: test-coverage ## Generate coverage for CI
	@echo "📊 Coverage for CI generated"
