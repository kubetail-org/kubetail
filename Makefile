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
RUST_VERSION := $(shell rustc --version | cut -d' ' -f2)
RUST_ARCH := $(shell rustc -vV | grep host | cut -d' ' -f2)

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
	@rustup component add rustfmt
	@cd $(CRATES_DIR) && cargo fmt --all -- --check

crates-vet:
	@echo "Vetting Rust crates with architecture $(RUST_ARCH)..."
	@rustup component add clippy
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

modules-test:
	@echo "Testing Go modules..."
	@cd $(MODULES_DIR) && go test -race github.com/kubetail-org/kubetail/modules/...

modules-vet:
	@echo "Vetting Go modules..."
	@cd $(MODULES_DIR) && go vet github.com/kubetail-org/kubetail/modules/...

modules-all: modules-lint modules-test modules-vet
	@echo "All Go module operations completed successfully."

# Dashboard UI targets
dashboard-ui-lint:
	@echo "Linting dashboard UI..."
	@cd $(DASHBOARD_UI_DIR) && pnpm install && pnpm lint

dashboard-ui-test:
	@echo "Testing dashboard UI..."
	@cd $(DASHBOARD_UI_DIR) && pnpm install && pnpm test run

dashboard-ui-all: dashboard-ui-lint dashboard-ui-test
	@echo "All dashboard UI operations completed successfully."

# Combined targets
lint-all: crates-lint modules-lint dashboard-ui-lint
	@echo "All linting completed successfully."

test-all: crates-test modules-test dashboard-ui-test
	@echo "All tests completed successfully."

vet-all: crates-vet modules-vet
	@echo "All vetting completed successfully."

ci-checks: lint-all test-all vet-all
	@echo "All CI checks completed successfully."

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
	@echo "  modules-vet           - Vet Go modules"
	@echo "  modules-all           - Run all Go module operations"
	@echo "  dashboard-ui-lint     - Lint dashboard UI"
	@echo "  dashboard-ui-test     - Test dashboard UI"
	@echo "  dashboard-ui-all      - Run all dashboard UI operations"
	@echo "  lint-all              - Run all lint checks"
	@echo "  test-all              - Run all tests"
	@echo "  vet-all               - Run all vetting"
	@echo "  ci-checks             - Run all CI checks (lint, test, vet)"
	@echo "  help                  - Show this help message"
