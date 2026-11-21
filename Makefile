# Define variables
CLI_DIR := ./cli
OUTPUT_DIR := ./bin
CLI_BINARY := kubetail

DASHBOARD_UI_DIR := ./apps/web
DASHBOARD_SERVER_DIR := ./dashboard
CLUSTER_AGENT_DIR := ./cluster/agent

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

# Allow version override via CLI argument (default to "dev")
VERSION ?= dev

# Define ldflags
LDFLAGS := -ldflags="-s -w -X 'github.com/kubetail-org/kubetail/cli/cmd.version=$(VERSION)'"

.DEFAULT_GOAL := build

# No target names a file it makes, and build/ is a directory.
.PHONY: build build-dashboard-ui clean help generate check-generated \
	lint-cli vet-cli test-cli cover-cli-changed \
	lint-libs vet-libs test-libs cover-libs-changed \
	lint-cluster-api vet-cluster-api test-cluster-api cover-cluster-api-changed \
	lint-cluster-agent vet-cluster-agent test-cluster-agent cover-cluster-agent-changed \
	lint-web vet-web test-web cover-web-changed \
	lint-e2e test-e2e \
	lint-repo lint-repo-sh lint-repo-md lint-repo-yaml lint-repo-toolchains lint-repo-digests lint-repo-claude-md test-repo

build: build-dashboard-ui ## Build the kubetail CLI, with the UI embedded, for this platform
	@echo "Building kubetail CLI binary..."
	@mkdir -p $(OUTPUT_DIR)
	@cd $(CLI_DIR) && GOOS=$(GOOS) GOARCH=$(GOARCH) CGO_ENABLED=0 go build $(LDFLAGS) -o ../$(OUTPUT_DIR)/$(CLI_BINARY) ./main.go

build-dashboard-ui: ## Build the dashboard UI and copy it to dashboard/website
	@echo "Building dashboard UI..."
	@cd $(DASHBOARD_UI_DIR) && pnpm install && pnpm build
	@echo "Copying apps/web/dist to dashboard/website..."
	@rm -rf $(DASHBOARD_SERVER_DIR)/website
	@cp -r $(DASHBOARD_UI_DIR)/dist $(DASHBOARD_SERVER_DIR)/website
	@touch $(DASHBOARD_SERVER_DIR)/website/.gitkeep
	@echo "Dashboard UI built and copied successfully."

# In the Linux sandbox dashboard-ui/dist is a mount point and Cargo's target dir
# lives outside the repo (scripts/sandbox-dev-setup.sh), so dist is emptied
# rather than removed and cargo clean follows CARGO_TARGET_DIR.
clean: ## Remove every build output and Go's test cache
	@echo "Cleaning up..."
	@rm -rf $(OUTPUT_DIR)
	@find $(DASHBOARD_SERVER_DIR)/website -mindepth 1 ! -name .gitkeep -delete
	@if [ -d $(DASHBOARD_UI_DIR)/dist ]; then find $(DASHBOARD_UI_DIR)/dist -mindepth 1 -delete; fi
	@if command -v cargo >/dev/null; then cd $(CRATES_DIR) && cargo clean; fi
	@go clean -testcache
	@echo "Cleanup done"

# Each check is <verb>-<area>, over the areas CI checks and measures apart: cli
# (with dashboard/), libs, cluster-api, cluster-agent, web, e2e and repo. No
# target runs every area: run the areas a change touches, and CI runs them all.

# The Go areas share these recipes, each called with the area's modules.

define lint-go
	@test -z "$$(gofmt -l $(1))"
	@sh scripts/check-license-headers.sh $(1)
	@sh scripts/check-coverage-markers.sh $(1)
endef

# The Windows pass is the one place _windows.go files are vetted.
# scripts/staticcheck.sh runs once per GOOS and merges; tools/go.mod pins staticcheck.
define vet-go
	@go vet $(foreach m,$(1),./$(m)/...)
	@GOOS=windows go vet $(foreach m,$(1),./$(m)/...)
	@sh scripts/check-go-fix.sh $(1)
	@sh scripts/staticcheck.sh $(1)
endef

define test-go
	@go test -race $(foreach m,$(1),./$(m)/...)
endef

# cli

lint-cli: ## Check cli/'s and dashboard/'s Go formatting, license headers and coverage markers
	$(call lint-go,cli dashboard)

vet-cli: ## Run go vet for the host and Windows, the go fix check and staticcheck on cli/ and dashboard/
	$(call vet-go,cli dashboard)

test-cli: ## Test cli/ and dashboard/ with the race detector
	$(call test-go,cli dashboard)

# One cover target per CI coverage area, each a wrapper around
# scripts/cover-changed.sh. Each coverage-*.yml's Gate in CI has the last word.
cover-cli-changed: ## List the changed lines in cli/ and dashboard/ no test covers (FULL=1 runs every test)
	@bash scripts/cover-changed.sh cli

# libs

lint-libs: ## Check libs/go-common/'s Go formatting, license headers and coverage markers
	$(call lint-go,libs/go-common)

vet-libs: ## Run go vet for the host and Windows, the go fix check and staticcheck on libs/go-common/
	$(call vet-go,libs/go-common)

test-libs: ## Test libs/go-common/ with the race detector
	$(call test-go,libs/go-common)

cover-libs-changed: ## List the changed lines in libs/go-common/ no test covers (FULL=1 runs every test)
	@bash scripts/cover-changed.sh libs

# cluster-api

lint-cluster-api: ## Check cluster/api/'s Go formatting, license headers and coverage markers
	$(call lint-go,cluster/api)

vet-cluster-api: ## Run go vet for the host and Windows, the go fix check and staticcheck on cluster/api/
	$(call vet-go,cluster/api)

test-cluster-api: ## Test cluster/api/ with the race detector
	$(call test-go,cluster/api)

cover-cluster-api-changed: ## List the changed lines in cluster/api/ no test covers (FULL=1 runs every test)
	@bash scripts/cover-changed.sh cluster-api

# cluster-agent

lint-cluster-agent: ## Check cluster/agent/'s Rust formatting, license headers and coverage markers
	@cd $(CLUSTER_AGENT_DIR) && rustup component add rustfmt
	@cd $(CLUSTER_AGENT_DIR) && cargo fmt --all -- --check
	@sh scripts/check-license-headers.sh cluster/agent
	@sh scripts/check-coverage-markers.sh cluster/agent

vet-cluster-agent: ## Run clippy on cluster/agent/
	@cd $(CLUSTER_AGENT_DIR) && rustup component add clippy
	@cd $(CLUSTER_AGENT_DIR) && cargo clippy --all --all-targets -- -D warnings

test-cluster-agent: ## Test cluster/agent/
	@cd $(CLUSTER_AGENT_DIR) && cargo test

cover-cluster-agent-changed: ## List the changed lines in cluster/agent/ no test covers (needs cargo-llvm-cov)
	@bash scripts/cover-changed.sh cluster-agent

# web

# lint:tailwind is a run apart from lint for its time (.oxlintrc.tailwind.json).
lint-web: ## Lint apps/web/ and its Tailwind classes, and check its license headers and coverage markers
	@cd $(DASHBOARD_UI_DIR) && pnpm install && pnpm lint && pnpm lint:tailwind
	@sh scripts/check-license-headers.sh apps/web
	@sh scripts/check-coverage-markers.sh apps/web/src

# The type-check half of `pnpm build`. The tsconfigs set noEmit, so it writes no JS.
vet-web: ## Type-check apps/web/ and its tests
	@cd $(DASHBOARD_UI_DIR) && pnpm install && pnpm exec tsc -b

test-web: ## Test apps/web/
	@cd $(DASHBOARD_UI_DIR) && pnpm install && pnpm test run

cover-web-changed: ## List the changed lines in apps/web/ no test covers
	@bash scripts/cover-changed.sh web

# e2e

lint-e2e: ## Lint the e2e suite
	@cd e2e && uv sync --group dev && uv run ruff check . && uv run ruff format --check .

# The kind cluster persists across runs; e2e/scripts/down.sh deletes it.
# ONLY_BUILD=1 suits iterating on the Cluster API or Cluster Agent while a
# cluster is up.
test-e2e: ## Build the images and the CLI, then run the e2e suite (ONLY_BUILD=1 skips the run)
	@bash e2e/scripts/build.sh
ifndef ONLY_BUILD
	@bash e2e/scripts/up.sh
	@cd e2e && uv run pytest -v
endif

# repo: what belongs to no component. CI runs each lint-repo-* in a job of its own.

lint-repo: lint-repo-sh lint-repo-md lint-repo-yaml lint-repo-toolchains lint-repo-digests lint-repo-claude-md ## Run every lint-repo-* and check tools/'s and scripts/' license headers
	@sh scripts/check-license-headers.sh tools scripts

lint-repo-sh: ## Lint every shell script
	@git ls-files '*.sh' | xargs shellcheck -x

# scripts/check-markdown-links.sh
lint-repo-md: ## Check local paths and anchors in every Markdown file
	@sh scripts/check-markdown-links.sh

# actionlint type-checks the workflows and composite actions and shellchecks
# their `run:` blocks; zizmor audits them against .github/ci-config/zizmor.yml.
# zizmor's online audits run only when GH_TOKEN is set, as in CI. tools/go.mod
# pins actionlint. tools/cicallers checks that ci.yml and coverage.yml call
# every area workflow beside them.
lint-repo-yaml: ## Lint every YAML file, then the workflows with actionlint, zizmor and cicallers
	@uv tool run yamllint@1.38.0 --strict .
	@sc=$$(command -v shellcheck) || { echo "shellcheck not found" >&2; exit 1; }; \
		cd tools && SHELLCHECK_OPTS="--severity=warning --exclude=SC2155" \
		go tool actionlint -shellcheck "$$sc" -config-file ../.github/ci-config/actionlint.yaml
	@uv tool run zizmor@1.31.1 --config .github/ci-config/zizmor.yml .
	@cd tools && go run ./cicallers ../.github/workflows

# scripts/check-toolchain-versions.sh
lint-repo-toolchains: ## Check the Dockerfiles' golang, node and rust images match CI's versions
	@sh scripts/check-toolchain-versions.sh

# scripts/check-image-digests.sh
lint-repo-digests: ## Check every image a Dockerfile pulls is pinned by digest
	@sh scripts/check-image-digests.sh

# scripts/check-claude-md.sh
lint-repo-claude-md: ## Check every tracked CLAUDE.md against its byte budget
	@sh scripts/check-claude-md.sh

test-repo: ## Test the scripts with bats
	@bats scripts/

# scripts/generate.sh
generate: ## Regenerate the committed protoc, gqlgen, codegen and CLI config output
	@bash scripts/generate.sh

# scripts/check-generated.sh
check-generated: ## Regenerate the committed output with generate, and fail on a change
	@bash scripts/check-generated.sh

# Prints each target's `##` description.
help: ## Show this help message
	@awk 'BEGIN {FS = ":.*## "} /^[a-z0-9-]+:.*## / {printf "  %-28s %s\n", $$1, $$2}' $(MAKEFILE_LIST)
