# Define variables
CLI_DIR := ./modules/cli
OUTPUT_DIR := ./bin
CLI_BINARY := kubetail

DASHBOARD_UI_DIR := ./dashboard-ui
DASHBOARD_SERVER_DIR := ./modules/dashboard
CRATES_DIR := ./crates/rgkl
MODULES_DIR := ./modules

# Detect the operating system
OS := $(shell uname -s | tr '[:upper:]' '[:lower:]')
ARCH := $(shell uname -m)

# Translate the OS to Go's format
ifeq ($(findstring _nt,$(OS)),_nt)
  GOOS := windows
else
  GOOS := $(OS)
endif

# Translate the architecture to Go's format
ifeq ($(ARCH),x86_64)
  GOARCH := amd64
else ifeq ($(ARCH),aarch64)
  GOARCH := arm64
else
  GOARCH := $(ARCH)
endif

# Rust toolchain detection
RUST_VERSION := $(shell command -v rustc >/dev/null 2>&1 && rustc --version | cut -d' ' -f2)
RUST_ARCH := $(shell command -v rustc >/dev/null 2>&1 && rustc -vV | grep host | cut -d' ' -f2)

# Allow version override via CLI argument (default to "dev")
VERSION ?= dev

# Define ldflags
LDFLAGS := -ldflags="-s -w -X 'github.com/kubetail-org/kubetail/modules/cli/cmd.version=$(VERSION)'"

# Default target
all: build

# Create the bin directory if it doesn't exist
$(OUTPUT_DIR):
	mkdir -p $(OUTPUT_DIR)

# Build the dashboard UI
build-dashboard-ui:
	@echo "Building dashboard UI..."
	@cd $(DASHBOARD_UI_DIR) && pnpm install && pnpm build
	@echo "Copying dashboard-ui/dist to modules/dashboard/website..."
	@rm -rf $(DASHBOARD_SERVER_DIR)/website
	@cp -r $(DASHBOARD_UI_DIR)/dist $(DASHBOARD_SERVER_DIR)/website
	@touch $(DASHBOARD_SERVER_DIR)/website/.gitkeep
	@echo "Dashboard UI built and copied successfully."

# Build CLI binary for host platform
build-cli: build-dashboard-ui
	@echo "Building kubetail CLI binary..."
	@cd $(CLI_DIR) && GOOS=$(GOOS) GOARCH=$(GOARCH) CGO_ENABLED=0 go build $(LDFLAGS) -o ../../$(OUTPUT_DIR)/$(CLI_BINARY) ./main.go

# Build all the CLI binaries
build-cli-all: build-dashboard-ui
	@echo "Building kubetail CLI binaries..."
	@cd $(CLI_DIR) && GOOS=darwin GOARCH=amd64 CGO_ENABLED=0 go build $(LDFLAGS) -o ../../$(OUTPUT_DIR)/$(CLI_BINARY)-darwin-amd64 ./main.go
	@echo "Built kubetail for darwin-amd64."
	@cd $(CLI_DIR) && GOOS=darwin GOARCH=arm64 CGO_ENABLED=0 go build $(LDFLAGS) -o ../../$(OUTPUT_DIR)/$(CLI_BINARY)-darwin-arm64 ./main.go
	@echo "Built kubetail for darwin-arm64."
	@cd $(CLI_DIR) && GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build $(LDFLAGS) -o ../../$(OUTPUT_DIR)/$(CLI_BINARY)-linux-amd64 ./main.go
	@echo "Built kubetail for linux-amd64."
	@cd $(CLI_DIR) && GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build $(LDFLAGS) -o ../../$(OUTPUT_DIR)/$(CLI_BINARY)-linux-arm64 ./main.go
	@echo "Built kubetail for linux-arm64."
	@cd $(CLI_DIR) && GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build $(LDFLAGS) -o ../../$(OUTPUT_DIR)/$(CLI_BINARY)-windows-adm64 ./main.go
	@echo "Built kubetail for windows-amd64."
	@echo "Kubetail CLI binaries built successfully."

# Build the CLI
build: build-dashboard-ui build-cli

# Build the CLI
build-all: build-dashboard-ui build-cli-all

# Rust (crates) targets
crates-build:
	@echo "Building Rust crates..."
	@cd $(CRATES_DIR) && cargo build --release

crates-lint:
	@echo "Linting Rust crates with architecture $(RUST_ARCH)..."
	@cd $(CRATES_DIR) && rustup component add rustfmt
	@cd $(CRATES_DIR) && cargo fmt --all -- --check
	@sh scripts/check-coverage-markers.sh crates

crates-vet:
	@echo "Vetting Rust crates with architecture $(RUST_ARCH)..."
	@cd $(CRATES_DIR) && rustup component add clippy
	@cd $(CRATES_DIR) && cargo clippy --all -- -D warnings

crates-test:
	@echo "Testing Rust crates..."
	@cd $(CRATES_DIR) && cargo test

crates-all: crates-build crates-lint crates-vet crates-test
	@echo "All Rust crate operations completed successfully."

# Go (modules) targets
modules-lint:
	@echo "Linting Go modules..."
	@cd $(MODULES_DIR) && test -z $$(gofmt -l .)
	@sh scripts/check-coverage-markers.sh modules

modules-test:
	@echo "Testing Go modules..."
	@cd $(MODULES_DIR) && go test -race github.com/kubetail-org/kubetail/modules/...

# The Windows pass is the one place _windows.go files are vetted.
modules-vet: modules-check-go-fix
	@echo "Vetting Go modules..."
	@cd $(MODULES_DIR) && go vet github.com/kubetail-org/kubetail/modules/...
	@cd $(MODULES_DIR) && GOOS=windows go vet github.com/kubetail-org/kubetail/modules/...

# Fails if go fix's modernizers would rewrite a module (scripts/check-go-fix.sh).
modules-check-go-fix:
	@sh scripts/check-go-fix.sh

# One run per GOOS, merged (scripts/staticcheck.sh). tools/go.mod pins staticcheck.
modules-staticcheck:
	@echo "Running staticcheck on Go modules..."
	@sh scripts/staticcheck.sh

modules-all: modules-lint modules-test modules-vet modules-staticcheck
	@echo "All Go module operations completed successfully."

# Dashboard UI targets
dashboard-ui-lint:
	@echo "Linting dashboard UI..."
	@cd $(DASHBOARD_UI_DIR) && pnpm install && pnpm lint
	@sh scripts/check-coverage-markers.sh dashboard-ui/src

dashboard-ui-test:
	@echo "Testing dashboard UI..."
	@cd $(DASHBOARD_UI_DIR) && pnpm install && pnpm test run

dashboard-ui-all: dashboard-ui-lint dashboard-ui-test
	@echo "All dashboard UI operations completed successfully."

# Every shell script in the repo.
lint-sh:
	@git ls-files '*.sh' | xargs shellcheck

# Local paths and anchors in every Markdown file (scripts/check-markdown-links.sh).
lint-md:
	@sh scripts/check-markdown-links.sh

lint-python:
	@cd e2e && uv sync --group dev && uv run ruff check . && uv run ruff format --check .

# Every linter CI runs.
lint: lint-all lint-sh lint-md lint-python
	@echo "All CI linters passed."

# Combined targets
lint-all: crates-lint modules-lint dashboard-ui-lint lint-yaml
	@echo "All linting completed successfully."

test-all: crates-test modules-test dashboard-ui-test
	@echo "All tests completed successfully."

vet-all: crates-vet modules-vet modules-staticcheck
	@echo "All vetting completed successfully."

ci-checks: lint-all test-all vet-all
	@echo "All CI checks completed successfully."

# Every YAML file, then the workflows and composite actions: actionlint
# type-checks them and shellchecks their `run:` blocks, zizmor audits them
# against .github/ci-config/zizmor.yml. zizmor's online audits run only when
# GH_TOKEN is set, as in CI. tools/go.mod pins actionlint.
lint-yaml:
	@echo "Linting YAML and workflows..."
	@uv tool run yamllint@1.38.0 --strict .
	@sc=$$(command -v shellcheck) || { echo "shellcheck not found" >&2; exit 1; }; \
		cd tools && SHELLCHECK_OPTS="--severity=warning --exclude=SC2155" \
		go tool actionlint -shellcheck "$$sc" -config-file ../.github/ci-config/actionlint.yaml
	@uv tool run zizmor@1.31.1 --config .github/ci-config/zizmor.yml .

# The tests for what this branch changed (scripts/test-changed.sh).
test-changed:
	@bash scripts/test-changed.sh

## List the changed lines this OS's tests leave uncovered (scripts/cover-changed.sh).
## Run it before pushing; CI's Coverage · Gate has the last word. FULL=1 runs
## every Go test, RUST=1 measures Rust too.
cover-changed:
	@bash scripts/cover-changed.sh

## Build e2e images and CLI, then run the full e2e suite.
## Pass ONLY_BUILD=1 to skip pytest and just rebuild artifacts (useful when
## iterating on cluster-api / cluster-agent code while a kind cluster is up).
## The kind cluster persists across runs; run e2e/scripts/down.sh to tear it down.
test-e2e:
	@bash e2e/scripts/build.sh
ifndef ONLY_BUILD
	@bash e2e/scripts/up.sh
	@cd e2e && uv run pytest -v
endif

## Clean the build output
clean:
	@echo "Cleaning up..."
	@rm -rf $(OUTPUT_DIR)
	@echo "Cleanup done"

# Help message
help:
	@echo "Makefile targets:"
	@echo "  all                   - Build the kubetail CLI"
	@echo "  build                 - Compile the kubetail CLI for the current OS"
	@echo "  build-all             - Compile the kubetail CLI for all platforms"
	@echo "  clean                 - Remove the built binaries"
	@echo "  crates-build          - Build Rust crates"
	@echo "  crates-lint           - Lint Rust crates"
	@echo "  crates-vet            - Vet Rust crates"
	@echo "  crates-test           - Test Rust crates"
	@echo "  crates-all            - Run all Rust crate operations"
	@echo "  modules-lint          - Lint Go modules"
	@echo "  modules-test          - Test Go modules"
	@echo "  modules-vet           - Vet Go modules, for the host and Windows"
	@echo "  modules-check-go-fix  - Fail if go fix would change a Go module"
	@echo "  modules-staticcheck   - Run staticcheck on Go modules"
	@echo "  modules-all           - Run all Go module operations"
	@echo "  dashboard-ui-lint     - Lint dashboard UI"
	@echo "  dashboard-ui-test     - Test dashboard UI"
	@echo "  dashboard-ui-all      - Run all dashboard UI operations"
	@echo "  lint                  - Run every linter CI runs"
	@echo "  lint-all              - Lint Rust, Go, the dashboard UI and YAML"
	@echo "  lint-yaml             - Lint YAML files and workflows"
	@echo "  lint-sh               - Lint every shell script"
	@echo "  lint-md               - Check local links in every Markdown file"
	@echo "  lint-python           - Lint the e2e suite"
	@echo "  test-all              - Run all tests"
	@echo "  test-changed          - Run the tests for this branch's changes"
	@echo "  vet-all               - Run all vetting"
	@echo "  ci-checks             - Run all CI checks (lint, test, vet)"
	@echo "  cover-changed         - List the changed lines no test covers"
	@echo "                          (FULL=1 runs every Go test, RUST=1 measures Rust)"
	@echo "  test-e2e              - Build images + CLI and run the e2e test suite"
	@echo "                          (pass ONLY_BUILD=1 to skip pytest)"
	@echo "  help                  - Show this help message"
