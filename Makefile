# Define variables
CLI_DIR := ./modules/cli
OUTPUT_DIR := ./bin
CLI_BINARY := kubetail

DASHBOARD_UI_DIR := ./dashboard-ui
DASHBOARD_SERVER_DIR := ./modules/server

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
build-cli:
	@echo "Building kubetail CLI binary..."
	@cd $(CLI_DIR) && GOOS=$(GOOS) GOARCH=$(GOARCH) go build -o ../../$(OUTPUT_DIR)/$(CLI_BINARY) ./main.go

# Build all the CLI binaries
build-cli-all:
	@echo "Building kubetail CLI binaries..."
	@cd $(CLI_DIR) && GOOS=darwin GOARCH=amd64 go build -o ../../$(OUTPUT_DIR)/$(CLI_BINARY)-darwin-amd64 ./main.go
	@echo "Built kubetail for darwin-amd64."
	@cd $(CLI_DIR) && GOOS=darwin GOARCH=arm64 go build -o ../../$(OUTPUT_DIR)/$(CLI_BINARY)-darwin-arm64 ./main.go
	@echo "Built kubetail for darwin-arm64."
	@cd $(CLI_DIR) && GOOS=linux GOARCH=amd64 go build -o ../../$(OUTPUT_DIR)/$(CLI_BINARY)-linux-amd64 ./main.go
	@echo "Built kubetail for linux-amd64."
	@cd $(CLI_DIR) && GOOS=linux GOARCH=arm64 go build -o ../../$(OUTPUT_DIR)/$(CLI_BINARY)-linux-arm64 ./main.go
	@echo "Built kubetail for linux-arm64."
	@cd $(CLI_DIR) && GOOS=windows GOARCH=amd64 go build -o ../../$(OUTPUT_DIR)/$(CLI_BINARY)-windows-adm64 ./main.go
	@echo "Built kubetail for windows-amd64."
	@echo "Kubetail CLI binaries built successfully."

# Build the CLI
build: build-dashboard-ui build-cli

# Build the CLI
build-all: build-dashboard-ui build-cli-all

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
	@echo "  clean                 - Remove the built binaries"
	@echo "  help                  - Show this help message"
