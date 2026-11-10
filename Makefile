# Define variables
CLI_DIR := ./modules/cli
OUTPUT_DIR := ./bin
CLI_BINARY := kubetail

DASHBOARD_UI_DIR := ./dashboard-ui
DASHBOARD_SERVER_DIR := ./modules/dashboard
CRATES_DIR := ./crates
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

.DEFAULT_GOAL := build

# No target names a file it makes, and build/ is a directory.
.PHONY: build build-dashboard-ui clean help \
	lint lint-go lint-rust lint-ts lint-python lint-sh lint-md lint-yaml lint-toolchains lint-digests \
	vet vet-go vet-rust \
	test test-go test-rust test-ts test-changed test-e2e \
	cover-changed check check-changed

build: build-dashboard-ui ## Build the kubetail CLI, with the UI embedded, for this platform
	@echo "Building kubetail CLI binary..."
	@mkdir -p $(OUTPUT_DIR)
	@cd $(CLI_DIR) && GOOS=$(GOOS) GOARCH=$(GOARCH) CGO_ENABLED=0 go build $(LDFLAGS) -o ../../$(OUTPUT_DIR)/$(CLI_BINARY) ./main.go

build-dashboard-ui: ## Build the dashboard UI and copy it to modules/dashboard/website
	@echo "Building dashboard UI..."
	@cd $(DASHBOARD_UI_DIR) && pnpm install && pnpm build
	@echo "Copying dashboard-ui/dist to modules/dashboard/website..."
	@rm -rf $(DASHBOARD_SERVER_DIR)/website
	@cp -r $(DASHBOARD_UI_DIR)/dist $(DASHBOARD_SERVER_DIR)/website
	@touch $(DASHBOARD_SERVER_DIR)/website/.gitkeep
	@echo "Dashboard UI built and copied successfully."

clean: ## Remove the built binaries
	@echo "Cleaning up..."
	@rm -rf $(OUTPUT_DIR)
	@echo "Cleanup done"

lint: lint-go lint-rust lint-ts lint-python lint-sh lint-md lint-yaml lint-toolchains lint-digests ## Run every linter CI runs
	@echo "All CI linters passed."

lint-go: ## Check Go formatting and coverage markers
	@echo "Linting Go modules..."
	@cd $(MODULES_DIR) && test -z $$(gofmt -l .)
	@sh scripts/check-coverage-markers.sh modules

lint-rust: ## Check Rust formatting and coverage markers
	@echo "Linting Rust crates with architecture $(RUST_ARCH)..."
	@cd $(CRATES_DIR) && rustup component add rustfmt
	@cd $(CRATES_DIR) && cargo fmt --all -- --check
	@sh scripts/check-coverage-markers.sh crates

lint-ts: ## Lint the dashboard UI and check its coverage markers
	@echo "Linting dashboard UI..."
	@cd $(DASHBOARD_UI_DIR) && pnpm install && pnpm lint
	@sh scripts/check-coverage-markers.sh dashboard-ui/src

lint-python: ## Lint the e2e suite
	@cd e2e && uv sync --group dev && uv run ruff check . && uv run ruff format --check .

lint-sh: ## Lint every shell script
	@git ls-files '*.sh' | xargs shellcheck -x

# scripts/check-markdown-links.sh
lint-md: ## Check local paths and anchors in every Markdown file
	@sh scripts/check-markdown-links.sh

# actionlint type-checks the workflows and composite actions and shellchecks
# their `run:` blocks; zizmor audits them against .github/ci-config/zizmor.yml.
# zizmor's online audits run only when GH_TOKEN is set, as in CI. tools/go.mod
# pins actionlint.
lint-yaml: ## Lint every YAML file, then the workflows with actionlint and zizmor
	@echo "Linting YAML and workflows..."
	@uv tool run yamllint@1.38.0 --strict .
	@sc=$$(command -v shellcheck) || { echo "shellcheck not found" >&2; exit 1; }; \
		cd tools && SHELLCHECK_OPTS="--severity=warning --exclude=SC2155" \
		go tool actionlint -shellcheck "$$sc" -config-file ../.github/ci-config/actionlint.yaml
	@uv tool run zizmor@1.31.1 --config .github/ci-config/zizmor.yml .

# scripts/check-toolchain-versions.sh
lint-toolchains: ## Check the Dockerfiles' golang, node and rust images match CI's versions
	@sh scripts/check-toolchain-versions.sh

# scripts/check-image-digests.sh
lint-digests: ## Check every image a Dockerfile pulls is pinned by digest
	@sh scripts/check-image-digests.sh

vet: vet-go vet-rust ## Run go vet, the go fix check, staticcheck and clippy
	@echo "All vetting completed successfully."

# The Windows pass is the one place _windows.go files are vetted.
# scripts/staticcheck.sh runs once per GOOS and merges; tools/go.mod pins staticcheck.
vet-go: ## Run go vet for the host and Windows, the go fix check and staticcheck
	@echo "Vetting Go modules..."
	@cd $(MODULES_DIR) && go vet github.com/kubetail-org/kubetail/modules/...
	@cd $(MODULES_DIR) && GOOS=windows go vet github.com/kubetail-org/kubetail/modules/...
	@sh scripts/check-go-fix.sh
	@sh scripts/staticcheck.sh

vet-rust: ## Run clippy on the Rust workspace
	@echo "Vetting Rust crates with architecture $(RUST_ARCH)..."
	@cd $(CRATES_DIR) && rustup component add clippy
	@cd $(CRATES_DIR) && cargo clippy --all -- -D warnings

test: test-go test-rust test-ts ## Run every Go, Rust and dashboard UI test
	@echo "All tests completed successfully."

test-go: ## Test the Go workspace with the race detector
	@echo "Testing Go modules..."
	@cd $(MODULES_DIR) && go test -race github.com/kubetail-org/kubetail/modules/...

test-rust: ## Test the Rust workspace
	@echo "Testing Rust crates..."
	@cd $(CRATES_DIR) && cargo test

test-ts: ## Test the dashboard UI
	@echo "Testing dashboard UI..."
	@cd $(DASHBOARD_UI_DIR) && pnpm install && pnpm test run

# scripts/test-changed.sh
test-changed: ## Run the tests for what this branch changed
	@bash scripts/test-changed.sh

# The kind cluster persists across runs; e2e/scripts/down.sh deletes it.
# ONLY_BUILD=1 suits iterating on the Cluster API or Cluster Agent while a
# cluster is up.
test-e2e: ## Build the images and the CLI, then run the e2e suite (ONLY_BUILD=1 skips the run)
	@bash e2e/scripts/build.sh
ifndef ONLY_BUILD
	@bash e2e/scripts/up.sh
	@cd e2e && uv run pytest -v
endif

# scripts/cover-changed.sh. CI's coverage / Gate has the last word.
cover-changed: ## List the changed lines no test covers (FULL=1 runs every Go test, RUST=1 measures Rust)
	@bash scripts/cover-changed.sh

check: lint vet test ## Run every lint, vet and test
	@echo "All checks passed."

# scripts/check-changed.sh
check-changed: ## Run the checks for what this branch changed, before opening a pull request
	@bash scripts/check-changed.sh

# Prints each target's `##` description.
help: ## Show this help message
	@awk 'BEGIN {FS = ":.*## "} /^[a-z0-9-]+:.*## / {printf "  %-20s %s\n", $$1, $$2}' $(MAKEFILE_LIST)
