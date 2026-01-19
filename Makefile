# qBittorrent WebUI Makefile

# Load .env file if it exists (silently)
ifneq (,$(wildcard .env))
    include .env
    export
endif

# Variables
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
GIT_COMMIT := $(shell git rev-parse HEAD 2> /dev/null)
GIT_TAG := $(shell git describe --abbrev=0 --tags)
BINARY_NAME = qui
BUILD_DIR = build
WEB_DIR = web
INTERNAL_WEB_DIR = internal/web

# Go build flags with Polar credentials
LDFLAGS = -ldflags "-X github.com/autobrr/qui/internal/buildinfo.Version=$(VERSION) -X main.PolarOrgID=$(POLAR_ORG_ID)"

.PHONY: all build frontend backend dev dev-backend dev-frontend dev-expose clean test test-coverage test-integration test-ssh-integration test-ftp-integration test-e2e test-e2e-keep test-e2e-clean test-e2e-coverage test-all test-coverage-report test-coverage-html help themes-fetch themes-clean lint lint-full lint-json lint-fix fmt modern deps docs-dev docs-build

# Default target
all: build

# Build both frontend and backend
build: frontend backend

build/docker:
	@echo "Building docker image..."
	docker build -t ghcr.io/autobrr/qui:dev -f distrib/docker/Dockerfile . --build-arg  GIT_TAG=$(GIT_TAG) --build-arg GIT_COMMIT=$(GIT_COMMIT) --build-arg POLAR_ORG_ID=$(POLAR_ORG_ID) --build-arg VERSION=$(VERSION)

build/dockerx:
	docker buildx build -t ghcr.io/autobrr/qui:dev -f distrib/docker/Dockerfile . --build-arg GIT_TAG=$(GIT_TAG) --build-arg GIT_COMMIT=$(GIT_COMMIT) --build-arg VERSION=$(VERSION) --platform=linux/amd64,linux/arm64 --pull --load

# Fetch premium themes from private repository
themes-fetch:
	@echo "Fetching premium themes..."
	@if [ -n "$$THEMES_REPO_TOKEN" ]; then \
		rm -rf .themes-temp && \
		git clone --depth=1 --filter=blob:none --sparse \
			https://$$THEMES_REPO_TOKEN@github.com/autobrr/qui-premium-themes.git .themes-temp && \
		cd .themes-temp && git sparse-checkout set --cone themes && cd .. && \
		mkdir -p $(WEB_DIR)/src/themes/premium && \
		cp .themes-temp/themes/*.css $(WEB_DIR)/src/themes/premium/ && \
		rm -rf .themes-temp && \
		echo "Premium themes fetched successfully"; \
	else \
		echo "THEMES_REPO_TOKEN not set, skipping premium themes"; \
	fi

# Clean premium themes
themes-clean:
	@echo "Cleaning premium themes..."
	rm -rf $(WEB_DIR)/src/themes/premium

# Build frontend
frontend: themes-fetch
	@echo "Building frontend..."
	cd $(WEB_DIR) && pnpm install && pnpm build
	@echo "Copying frontend assets..."
	rm -rf $(INTERNAL_WEB_DIR)/dist
	cp -r $(WEB_DIR)/dist $(INTERNAL_WEB_DIR)/

# Build backend
backend:
	@echo "Building backend..."
	go build $(LDFLAGS) -o $(BINARY_NAME) ./cmd/qui

# Development mode - run both frontend and backend
dev:
	@echo "Starting development mode..."
	@make -j 2 dev-backend dev-frontend

# Run backend with hot reload (requires air)
dev-backend:
	@echo "Starting backend development server..."
	air -c .air.toml

# Run frontend development server
dev-frontend:
	@echo "Starting frontend development server..."
	cd $(WEB_DIR) && pnpm dev

# Development mode with frontend exposed on 0.0.0.0
dev-expose:
	@echo "Starting development mode with frontend exposed on 0.0.0.0..."
	@make -j 2 dev-backend dev-frontend-expose

# Run frontend development server exposed on 0.0.0.0
dev-frontend-expose:
	@echo "Starting frontend development server (exposed on 0.0.0.0)..."
	cd $(WEB_DIR) && pnpm dev --host

# Clean build artifacts
clean: themes-clean
	@echo "Cleaning..."
	rm -rf $(WEB_DIR)/dist $(INTERNAL_WEB_DIR)/dist $(BINARY_NAME) $(BUILD_DIR) $(COVERAGE_DIR)

# Coverage output directory
COVERAGE_DIR = .coverage

# Run unit tests
test:
	@echo "Running unit tests..."
	go test -race -count=1 -v ./...

# Run unit tests with coverage
test-coverage:
	@echo "Running unit tests with coverage..."
	@mkdir -p $(COVERAGE_DIR)
	go test -race -cover -coverprofile=$(COVERAGE_DIR)/unit.out ./...
	@echo ""
	@echo "Coverage summary:"
	@go tool cover -func=$(COVERAGE_DIR)/unit.out | tail -1

# Run integration tests (requires filesystem operations)
test-integration:
	@echo "Running integration tests..."
	go test -tags=integration -v ./internal/services/transfer/...

# Run SSH integration tests (requires SSH server)
test-ssh-integration:
	@echo "Running SSH integration tests..."
	@mkdir -p $(COVERAGE_DIR)
	go test -tags=ssh_integration -v -cover -coverprofile=$(COVERAGE_DIR)/ssh.out ./pkg/sshclient/...
	@echo ""
	@go tool cover -func=$(COVERAGE_DIR)/ssh.out | tail -1

# Run FTP integration tests (requires FTP server)
test-ftp-integration:
	@echo "Running FTP integration tests..."
	@mkdir -p $(COVERAGE_DIR)
	go test -tags=ftp_integration -v -cover -coverprofile=$(COVERAGE_DIR)/ftp.out ./pkg/ftpclient/...
	@echo ""
	@go tool cover -func=$(COVERAGE_DIR)/ftp.out | tail -1

# Run E2E tests (requires Docker)
test-e2e:
	@echo "Running E2E transfer tests..."
	go test -tags=e2e -v -timeout=15m ./tests/e2e/...

# Run E2E tests with coverage
test-e2e-coverage:
	@echo "Running E2E tests with coverage..."
	@mkdir -p $(COVERAGE_DIR)
	go test -tags=e2e -v -timeout=15m -cover -coverprofile=$(COVERAGE_DIR)/e2e.out ./tests/e2e/...
	@echo ""
	@go tool cover -func=$(COVERAGE_DIR)/e2e.out | tail -1

# Run E2E tests and keep containers running for debugging
test-e2e-keep:
	@echo "Running E2E tests (keeping containers running)..."
	E2E_KEEP_RUNNING=true go test -tags=e2e -v -timeout=15m ./tests/e2e/...

# Clean up E2E test containers
test-e2e-clean:
	@echo "Cleaning up E2E test environment..."
	docker compose -f tests/e2e/docker-compose.yml down -v
	rm -rf tests/e2e/.testdata

# Run all tests with combined coverage
test-all:
	@echo "Running all tests with coverage..."
	@mkdir -p $(COVERAGE_DIR)
	@echo "Step 1/3: Unit tests..."
	go test -cover -coverprofile=$(COVERAGE_DIR)/unit.out ./... || true
	@echo ""
	@echo "Step 2/3: Integration tests..."
	go test -tags=integration -cover -coverprofile=$(COVERAGE_DIR)/integration.out ./internal/services/transfer/... || true
	@echo ""
	@echo "Step 3/3: E2E tests (requires Docker)..."
	go test -tags=e2e -timeout=15m -cover -coverprofile=$(COVERAGE_DIR)/e2e.out ./tests/e2e/... || true
	@echo ""
	@echo "=== Coverage Summary ==="
	@if [ -f $(COVERAGE_DIR)/unit.out ]; then echo "Unit:        $$(go tool cover -func=$(COVERAGE_DIR)/unit.out | tail -1 | awk '{print $$3}')"; fi
	@if [ -f $(COVERAGE_DIR)/integration.out ]; then echo "Integration: $$(go tool cover -func=$(COVERAGE_DIR)/integration.out | tail -1 | awk '{print $$3}')"; fi
	@if [ -f $(COVERAGE_DIR)/e2e.out ]; then echo "E2E:         $$(go tool cover -func=$(COVERAGE_DIR)/e2e.out | tail -1 | awk '{print $$3}')"; fi

# Generate detailed coverage report
test-coverage-report:
	@echo "Generating detailed coverage report..."
	@mkdir -p $(COVERAGE_DIR)
	go test -cover -coverprofile=$(COVERAGE_DIR)/coverage.out ./...
	@echo ""
	@echo "=== Package Coverage ==="
	@go tool cover -func=$(COVERAGE_DIR)/coverage.out | grep -E "^github.com.*total:" | sort -t'	' -k3 -rn | head -20
	@echo ""
	@echo "=== Total Coverage ==="
	@go tool cover -func=$(COVERAGE_DIR)/coverage.out | tail -1

# Generate HTML coverage report
test-coverage-html:
	@echo "Generating HTML coverage report..."
	@mkdir -p $(COVERAGE_DIR)
	go test -cover -coverprofile=$(COVERAGE_DIR)/coverage.out ./...
	go tool cover -html=$(COVERAGE_DIR)/coverage.out -o $(COVERAGE_DIR)/coverage.html
	@echo "Coverage report generated: $(COVERAGE_DIR)/coverage.html"
	@open $(COVERAGE_DIR)/coverage.html 2>/dev/null || xdg-open $(COVERAGE_DIR)/coverage.html 2>/dev/null || echo "Open $(COVERAGE_DIR)/coverage.html in your browser"

# Validate OpenAPI specification
test-openapi:
	@echo "Validating OpenAPI specification..."
	go test -v ./internal/web/swagger

# Format changed code only (fast, for iteration)
fmt:
	@echo "Formatting changed Go code..."
	@gofiles=$$({ git diff --name-only --diff-filter=d; git diff --name-only --cached --diff-filter=d; } | sort -u | grep '\.go$$' || true); \
		if [ -n "$$gofiles" ]; then echo "$$gofiles" | xargs gofmt -w; fi
	@echo "Formatting changed frontend code..."
	@webfiles=$$({ git diff --name-only --diff-filter=d -- '$(WEB_DIR)/'; git diff --name-only --cached --diff-filter=d -- '$(WEB_DIR)/'; } | sort -u | sed 's|^$(WEB_DIR)/||' | grep -E '\.(ts|tsx|js|jsx)$$' || true); \
		if [ -n "$$webfiles" ]; then cd $(WEB_DIR) && echo "$$webfiles" | xargs pnpm eslint --fix; fi

# Lint code (changed files only - fast feedback for AI iteration)
lint:
	@echo "Linting changed Go code..."
	golangci-lint run --new-from-merge-base=develop --timeout=5m
	@echo "Linting frontend..."
	cd $(WEB_DIR) && pnpm lint

# Full lint (entire codebase - use before commits/PRs)
lint-full:
	@echo "Linting entire Go codebase..."
	golangci-lint run --timeout=10m
	@echo "Linting frontend..."
	cd $(WEB_DIR) && pnpm lint

# Lint with JSON output (for AI agent consumption)
lint-json:
	@echo "Generating lint report..."
	golangci-lint run --new-from-merge-base=main --output.json.path=./lint-report.json --timeout=5m || true
	@echo "Lint report saved to lint-report.json"

# Lint with auto-fix where possible
lint-fix:
	@echo "Running linters with auto-fix..."
	golangci-lint run --fix --timeout=10m
	cd $(WEB_DIR) && pnpm lint --fix

# Modernize Go code (interface{} -> any, etc)
modern:
	@echo "Modernizing Go code..."
	go run golang.org/x/tools/gopls/internal/analysis/modernize/cmd/modernize@latest -fix -test ./...

# Install development dependencies
deps:
	@echo "Installing development dependencies..."
	go mod download
	cd $(WEB_DIR) && pnpm install

# Documentation development server
docs-dev:
	@echo "Starting documentation development server..."
	cd documentation && pnpm start

# Build documentation
docs-build:
	@echo "Building documentation..."
	cd documentation && pnpm build

# Help
help:
	@echo "Available targets:"
	@echo ""
	@echo "Build:"
	@echo "  make build          - Build both frontend and backend"
	@echo "  make frontend       - Build frontend only"
	@echo "  make backend        - Build backend only"
	@echo "  make build/docker   - Build Docker image"
	@echo ""
	@echo "Development:"
	@echo "  make dev            - Run development servers (air + pnpm dev)"
	@echo "  make dev-backend    - Run backend with hot reload"
	@echo "  make dev-frontend   - Run frontend development server"
	@echo "  make dev-expose     - Run frontend dev server exposed on 0.0.0.0"
	@echo ""
	@echo "Testing:"
	@echo "  make test                  - Run unit tests with race detection"
	@echo "  make test-coverage         - Run unit tests with coverage summary"
	@echo "  make test-coverage-report  - Generate detailed per-package coverage report"
	@echo "  make test-coverage-html    - Generate HTML coverage report (opens browser)"
	@echo "  make test-integration      - Run integration tests (filesystem operations)"
	@echo "  make test-ssh-integration  - Run SSH client integration tests"
	@echo "  make test-ftp-integration  - Run FTP client integration tests"
	@echo "  make test-e2e              - Run E2E tests (requires Docker)"
	@echo "  make test-e2e-coverage     - Run E2E tests with coverage"
	@echo "  make test-e2e-keep         - Run E2E tests, keep containers running"
	@echo "  make test-e2e-clean        - Clean up E2E test containers"
	@echo "  make test-all              - Run all tests with combined coverage summary"
	@echo "  make test-openapi          - Validate OpenAPI specification"
	@echo ""
	@echo "Linting:"
	@echo "  make lint           - Lint changed files only (fast, for iteration)"
	@echo "  make lint-full      - Lint entire codebase"
	@echo "  make lint-json      - Generate JSON lint report for AI agents"
	@echo "  make lint-fix       - Auto-fix linting issues where possible"
	@echo ""
	@echo "Formatting:"
	@echo "  make fmt            - Format changed files only (fast, for iteration)"
	@echo "  make modern         - Modernize Go code (interface{} -> any)"
	@echo ""
	@echo "Documentation:"
	@echo "  make docs-dev       - Run documentation development server"
	@echo "  make docs-build     - Build documentation for production"
	@echo ""
	@echo "Other:"
	@echo "  make themes-fetch   - Fetch premium themes from private repository"
	@echo "  make themes-clean   - Clean premium themes"
	@echo "  make clean          - Clean build artifacts"
	@echo "  make deps           - Install dependencies"
	@echo "  make help           - Show this help message"