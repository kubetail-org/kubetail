# Define variables
CLI_DIR := ./cli
OUTPUT_DIR := ./bin
CLI_BINARY := kubetail

DASHBOARD_UI_DIR := ./apps/web
DASHBOARD_SERVER_DIR := ./dashboard
CLUSTER_AGENT_DIR := ./cluster/agent
DESKTOP_DIR := ./apps/desktop

# Allow version override via CLI argument (default to "dev")
VERSION ?= dev

# Define ldflags
LDFLAGS := -ldflags="-s -w -X 'github.com/kubetail-org/kubetail/cli/cmd.version=$(VERSION)'"

.DEFAULT_GOAL := build

# No target names a file it makes, and build/ is a directory.
.PHONY: build build-dashboard-ui clean help generate check-generated \
	lint-cli vet-cli test-cli test-cli-changed cover-cli-changed \
	lint-libs vet-libs test-libs test-libs-changed cover-libs-changed \
	lint-cluster-api vet-cluster-api test-cluster-api test-cluster-api-changed cover-cluster-api-changed \
	lint-cluster-agent vet-cluster-agent test-cluster-agent test-cluster-agent-changed cover-cluster-agent-changed \
	lint-web vet-web test-web test-web-changed cover-web-changed \
	generate-desktop build-desktop lint-desktop vet-desktop test-desktop test-desktop-changed cover-desktop-changed \
	build-desktop-sidecar build-desktop-sidecar-dev build-desktop-bwrap build-desktop-pasta \
	build-desktop-embedded-sources generate-desktop-prompts generate-desktop-icons \
	test-desktop-livecache test-desktop-liveswitch status-desktop-design record-desktop-design \
	clean-desktop-data clean-desktop-window \
	lint-e2e test-e2e \
	lint-repo lint-repo-sh lint-repo-md lint-repo-yaml lint-repo-toolchains lint-repo-digests test-repo

build: build-dashboard-ui ## Build the kubetail CLI, with the UI embedded, for this platform
	mkdir -p $(OUTPUT_DIR)
	cd $(CLI_DIR) && CGO_ENABLED=0 go build $(LDFLAGS) -o ../$(OUTPUT_DIR)/$(CLI_BINARY) ./main.go

build-dashboard-ui: ## Build the dashboard UI and copy it to dashboard/website
	cd $(DASHBOARD_UI_DIR) && pnpm install && pnpm build
	rm -rf $(DASHBOARD_SERVER_DIR)/website
	cp -r $(DASHBOARD_UI_DIR)/dist $(DASHBOARD_SERVER_DIR)/website
	touch $(DASHBOARD_SERVER_DIR)/website/.gitkeep

# In the Linux sandbox each app's dist is a mount point and Cargo's target dir
# lives outside the repo (scripts/sandbox-dev-setup.sh), so dist is emptied
# rather than removed and cargo clean follows CARGO_TARGET_DIR.
clean: ## Remove every build output and Go's test cache
	rm -rf $(OUTPUT_DIR)
	find $(DASHBOARD_SERVER_DIR)/website -mindepth 1 ! -name .gitkeep -delete
	if [ -d $(DASHBOARD_UI_DIR)/dist ]; then find $(DASHBOARD_UI_DIR)/dist -mindepth 1 -delete; fi
	if [ -d $(DESKTOP_DIR)/dist ]; then find $(DESKTOP_DIR)/dist -mindepth 1 -delete; fi
	rm -rf $(DESKTOP_DIR)/src-tauri/binaries
	if command -v cargo >/dev/null; then cd $(CLUSTER_AGENT_DIR) && cargo clean; fi
	if command -v cargo >/dev/null; then cd $(DESKTOP_DIR)/src-tauri && cargo clean; fi
	go clean -testcache

# Each check is <verb>-<area>, over the areas CI checks and measures apart: cli
# (with dashboard/), libs, cluster-api, cluster-agent, web, desktop, e2e and repo. No
# target runs every area: run the areas a change touches, and CI runs them all.

# The Go areas share these recipes, each called with the area's modules.

define lint-go
	test -z "$$(gofmt -l $(1))"
	sh scripts/check-license-headers.sh $(1)
	sh scripts/check-coverage-markers.sh $(1)
endef

# The Windows pass is the one place _windows.go files are vetted.
# scripts/staticcheck.sh runs once per GOOS and merges; tools/go.mod pins staticcheck.
define vet-go
	go vet $(foreach m,$(1),./$(m)/...)
	GOOS=windows go vet $(foreach m,$(1),./$(m)/...)
	sh scripts/check-go-fix.sh $(1)
	sh scripts/staticcheck.sh $(1)
endef

define test-go
	go test -race $(foreach m,$(1),./$(m)/...)
endef

# cli

lint-cli: ## Check cli/'s and dashboard/'s Go formatting, license headers and coverage markers
	$(call lint-go,cli dashboard)

vet-cli: ## Run go vet for the host and Windows, the go fix check and staticcheck on cli/ and dashboard/
	$(call vet-go,cli dashboard)

test-cli: ## Test cli/ and dashboard/ with the race detector
	$(call test-go,cli dashboard)

# One test-<area>-changed per area, each a wrapper around scripts/test-changed.sh.
test-cli-changed: ## Run the tests in cli/ and dashboard/ the change reaches: narrows test-cli, never replaces it before a PR
	bash scripts/test-changed.sh cli

# One cover target per CI coverage area, each a wrapper around
# scripts/cover-changed.sh. Each coverage-*.yml's Gate in CI has the last word.
cover-cli-changed: ## List the changed lines in cli/ and dashboard/ no test covers (FULL=1 runs every test)
	bash scripts/cover-changed.sh cli

# libs

lint-libs: ## Check libs/go-common/'s Go formatting, license headers and coverage markers
	$(call lint-go,libs/go-common)

vet-libs: ## Run go vet for the host and Windows, the go fix check and staticcheck on libs/go-common/
	$(call vet-go,libs/go-common)

test-libs: ## Test libs/go-common/ with the race detector
	$(call test-go,libs/go-common)

test-libs-changed: ## Run the tests in libs/go-common/ the change reaches: narrows test-libs, never replaces it before a PR
	bash scripts/test-changed.sh libs

cover-libs-changed: ## List the changed lines in libs/go-common/ no test covers (FULL=1 runs every test)
	bash scripts/cover-changed.sh libs

# cluster-api

lint-cluster-api: ## Check cluster/api/'s Go formatting, license headers and coverage markers
	$(call lint-go,cluster/api)

vet-cluster-api: ## Run go vet for the host and Windows, the go fix check and staticcheck on cluster/api/
	$(call vet-go,cluster/api)

test-cluster-api: ## Test cluster/api/ with the race detector
	$(call test-go,cluster/api)

test-cluster-api-changed: ## Run the tests in cluster/api/ the change reaches: narrows test-cluster-api, never replaces it before a PR
	bash scripts/test-changed.sh cluster-api

cover-cluster-api-changed: ## List the changed lines in cluster/api/ no test covers (FULL=1 runs every test)
	bash scripts/cover-changed.sh cluster-api

# cluster-agent

lint-cluster-agent: ## Check cluster/agent/'s Rust formatting, license headers and coverage markers
	cd $(CLUSTER_AGENT_DIR) && rustup component add rustfmt
	cd $(CLUSTER_AGENT_DIR) && cargo fmt --all -- --check
	sh scripts/check-license-headers.sh cluster/agent
	sh scripts/check-coverage-markers.sh cluster/agent

vet-cluster-agent: ## Run clippy on cluster/agent/
	cd $(CLUSTER_AGENT_DIR) && rustup component add clippy
	cd $(CLUSTER_AGENT_DIR) && cargo clippy --all --all-targets -- -D warnings

test-cluster-agent: ## Test cluster/agent/
	cd $(CLUSTER_AGENT_DIR) && cargo test

test-cluster-agent-changed: ## Run the tests in cluster/agent/ the change reaches: narrows test-cluster-agent, never replaces it before a PR
	bash scripts/test-changed.sh cluster-agent

cover-cluster-agent-changed: ## List the changed lines in cluster/agent/ no test covers (needs cargo-llvm-cov)
	bash scripts/cover-changed.sh cluster-agent

# web

# lint:tailwind is a run apart from lint for its time (.oxlintrc.tailwind.json).
lint-web: ## Lint apps/web/ and its Tailwind classes, and check its license headers and coverage markers
	cd $(DASHBOARD_UI_DIR) && pnpm install && pnpm lint && pnpm lint:tailwind
	sh scripts/check-license-headers.sh apps/web
	sh scripts/check-coverage-markers.sh apps/web/src

# The type-check half of `pnpm build`. The tsconfigs set noEmit, so it writes no JS.
vet-web: ## Type-check apps/web/ and its tests
	cd $(DASHBOARD_UI_DIR) && pnpm install && pnpm exec tsc -b

test-web: ## Test apps/web/
	cd $(DASHBOARD_UI_DIR) && pnpm install && pnpm test run

test-web-changed: ## Run the tests in apps/web/ the change reaches: narrows test-web, never replaces it before a PR
	bash scripts/test-changed.sh web

cover-web-changed: ## List the changed lines in apps/web/ no test covers
	bash scripts/cover-changed.sh web

# desktop

# The sidecar is a module outside go.work, so its Go commands run in its
# directory. Tauri's externalBin bundles src-tauri/binaries/kubetail-sidecar-
# <rust-host-triple>, and tauri-build fails without it, so every cargo command
# on the host builds the sidecar first.

build-desktop: build-desktop-sidecar ## Build the desktop app's sidecar, webview and host for this platform, unbundled
	cd $(DESKTOP_DIR) && pnpm install && pnpm build
	cd $(DESKTOP_DIR)/src-tauri && cargo build

build-desktop-sidecar: ## Build the desktop app's sidecar into src-tauri/binaries/, untagged, as every release does
	cd $(DESKTOP_DIR) && go run scripts/build-sidecar.go

# `tauri dev` runs the same build.
build-desktop-sidecar-dev: ## Build the desktop app's sidecar under -tags debug, which lets KUBETAIL_OAUTH_ISSUER and friends redirect it
	cd $(DESKTOP_DIR) && go run scripts/build-sidecar.go -tags debug

build-desktop-bwrap: ## Build the pinned bwrap the desktop's Linux packages carry into src-tauri/linux/ (Linux only)
	cd $(DESKTOP_DIR) && bash scripts/build-bwrap.sh

build-desktop-pasta: ## Build the pinned pasta the desktop's Linux packages carry into src-tauri/linux/ (Linux only)
	cd $(DESKTOP_DIR) && bash scripts/build-pasta.sh

build-desktop-embedded-sources: ## Bundle bwrap's and pasta's source into the desktop's src-tauri/linux/embedded-sources.tar.gz
	cd $(DESKTOP_DIR) && bash scripts/build-embedded-sources.sh

# scripts/generate.sh. src-tauri/build.rs emits the Rust gRPC bindings on every
# build, so build-desktop surfaces their errors.
generate-desktop: ## Regenerate the desktop app's committed protoc, gqlgen and codegen output
	bash scripts/generate.sh desktop

# The goldens are the system prompt and every tool's offer, per kind of run and
# machine (sidecar/internal/app/testdata/prompt/), a question's context block and
# the subagent's assembled prompt (sidecar/internal/agent/chat/testdata/). Review
# the diff: it is the model's view of the change, byte for byte where the cache
# depends on it.
generate-desktop-prompts: ## Rewrite the goldens that pin what the desktop's model reads, after changing a prompt or an offer
	cd $(DESKTOP_DIR)/sidecar && go test ./internal/app ./internal/agent/chat -run 'MatchesItsGolden|TestTheSubagentsPromptIsAssembledInOrder' -update

# Kubetail Desktop ships no mobile app, so the Android and iOS sets are dropped.
# icon.icns comes from app-macos.svg, which follows Apple's icon grid. tray.png
# comes from tray-macos.svg at 18pt @2x, the height tray-icon draws a macOS tray
# image at.
generate-desktop-icons: ## Regenerate the desktop's src-tauri/icons/, public/favicon.ico and src/assets/icons/ from src-icons/
	cd $(DESKTOP_DIR) && rm -rf src-tauri/icons
	cd $(DESKTOP_DIR) && pnpm tauri icon src-icons/app-light.svg
	cd $(DESKTOP_DIR) && rm -rf src-tauri/icons/android src-tauri/icons/ios
	cd $(DESKTOP_DIR) && pnpm tauri icon src-icons/app-macos.svg -o src-tauri/icons/macos
	cd $(DESKTOP_DIR) && mv src-tauri/icons/macos/icon.icns src-tauri/icons/icon.icns && rm -rf src-tauri/icons/macos
	cd $(DESKTOP_DIR) && pnpm tauri icon src-icons/tray-macos.svg --png 36 -o src-tauri/icons/tray
	cd $(DESKTOP_DIR) && mv src-tauri/icons/tray/36x36.png src-tauri/icons/tray.png && rmdir src-tauri/icons/tray
	cd $(DESKTOP_DIR) && cp src-tauri/icons/icon.ico public/favicon.ico
	cd $(DESKTOP_DIR) && mkdir -p src/assets/icons && cp src-icons/app-light.svg src-icons/app-dark.svg src/assets/icons/

# lint:tailwind is a run apart from lint for its time (.oxlintrc.tailwind.json).
# designsync checks the design manifest against the code it names, and
# securitymodel the security model against the tests and files it cites.
lint-desktop: ## Lint the desktop app's webview, Tailwind classes, Go and Rust formatting, license headers, coverage markers, design manifest and security model
	cd $(DESKTOP_DIR) && pnpm install && pnpm lint && pnpm lint:tailwind
	test -z "$$(gofmt -l $(DESKTOP_DIR)/sidecar)"
	cd $(DESKTOP_DIR)/src-tauri && cargo fmt --check
	sh scripts/check-license-headers.sh apps/desktop
	sh scripts/check-coverage-markers.sh apps/desktop
	cd tools && go run ./designsync check
	cd tools && go run ./securitymodel check

# Each build the sidecar ships or tags, as scripts/go-builds.sh lists them: the
# live checks sit behind tags no other target builds, and vetting them keeps
# them compiling. check-sidecar-deps.sh keeps the typed Kubernetes clients out
# of the binary.
vet-desktop: build-desktop-sidecar ## Type-check the desktop's webview, vet its sidecar for every build, check its deps, run go fix and staticcheck, and run clippy on its host
	cd $(DESKTOP_DIR) && pnpm install && pnpm exec tsc -b
	cd $(DESKTOP_DIR)/sidecar && GOOS=linux GOARCH=amd64 go vet ./... && GOOS=linux GOARCH=arm64 go vet ./... \
		&& GOOS=darwin GOARCH=amd64 go vet ./... && GOOS=windows GOARCH=amd64 go vet ./...
	cd $(DESKTOP_DIR)/sidecar && go vet -tags debug ./... && go vet -tags livecache ./internal/agent/catalog \
		&& go vet -tags liveswitch ./internal/agent/catalog
	sh $(DESKTOP_DIR)/scripts/check-sidecar-deps.sh
	sh scripts/check-go-fix.sh apps/desktop/sidecar
	sh scripts/staticcheck.sh apps/desktop/sidecar
	cd $(DESKTOP_DIR)/src-tauri && cargo clippy --all-targets -- -D warnings

# As CI tests the sidecar. checkptr, which -race turns on, is off for modernc's
# SQLite alone, where it spends a quarter of agent/chat's CPU. The login
# shell's tests read SSH_AUTH_SOCK. GORACE drops the race detector's one-second
# sleep at each clean exit.
test-desktop: build-desktop-sidecar ## Test the desktop app's sidecar with the race detector, its host and its webview
	cd $(DESKTOP_DIR)/sidecar && GORACE=atexit_sleep_ms=0 SSH_AUTH_SOCK= \
		go test -race -short -gcflags='modernc.org/...=-d=checkptr=0' ./...
	cd $(DESKTOP_DIR)/src-tauri && cargo test
	cd $(DESKTOP_DIR) && pnpm install && pnpm test --run

test-desktop-changed: ## Run the tests in apps/desktop/ the change reaches: narrows test-desktop, never replaces it before a PR
	bash scripts/test-changed.sh desktop

# Each reaches the vendors and spends real tokens, so nothing runs it
# automatically: run it before a catalog change, or a change to how a wire
# replays another's rows, lands, and paste its lines into the PR.
test-desktop-livecache: ## Check every model whose key is set reads its second turn from the cache
	cd $(DESKTOP_DIR)/sidecar && go test -tags livecache -count=1 -v -run TestEveryModelReadsItsSecondTurnFromTheCache ./internal/agent/catalog

test-desktop-liveswitch: ## Walk one desktop chat across every dialect with the keys in the environment
	cd $(DESKTOP_DIR)/sidecar && go test -tags liveswitch -count=1 -v -run TestAChatSwitchesAcrossEveryDialect ./internal/agent/catalog

cover-desktop-changed: ## List the changed lines in apps/desktop/ no test covers (FULL=1 runs every Go test; the host's needs cargo-llvm-cov)
	bash scripts/cover-changed.sh desktop

# designsync reads Figma when FIGMA_TOKEN is set. On macOS an unset one comes
# from the Keychain item kubetail-figma-token; without either, the code side alone.
status-desktop-design record-desktop-design: export FIGMA_TOKEN ?= $(shell security find-generic-password -s kubetail-figma-token -w 2>/dev/null)

status-desktop-design: ## List the desktop components and screens whose code or design moved since their last sync, reading Figma with FIGMA_TOKEN
	cd tools && go run ./designsync status

record-desktop-design: ## Record the desktop design entries in IDS as in sync: their code's blobs and, with FIGMA_TOKEN, Figma's hash
	cd tools && go run ./designsync record $(IDS)

# Apart from clean, so clearing build outputs never deletes a developer's chats.
# It refuses while a dev sidecar runs.
clean-desktop-data: ## Delete the desktop dev app's data, cache, config and state directories (database, chats, settings, mirror, logs)
	go -C $(DESKTOP_DIR)/sidecar run ./scripts/clean-dev-data.go

clean-desktop-window: ## Delete the desktop dev app's saved window frame
	go -C $(DESKTOP_DIR)/sidecar run ./scripts/clean-dev-data.go -window-state

# e2e

lint-e2e: ## Lint the e2e suite
	cd e2e && uv sync --group dev && uv run ruff check . && uv run ruff format --check .

# The kind cluster persists across runs; e2e/scripts/down.sh deletes it.
# ONLY_BUILD=1 suits iterating on the Cluster API or Cluster Agent while a
# cluster is up.
test-e2e: ## Build the images and the CLI, then run the e2e suite (ONLY_BUILD=1 skips the run)
	bash e2e/scripts/build.sh
ifndef ONLY_BUILD
	bash e2e/scripts/up.sh
	cd e2e && uv run pytest -v
endif

# repo: what belongs to no component. CI runs each lint-repo-* in a job of its own.

lint-repo: lint-repo-sh lint-repo-md lint-repo-yaml lint-repo-toolchains lint-repo-digests ## Run every lint-repo-* and check tools/'s and scripts/' license headers
	sh scripts/check-license-headers.sh tools scripts

lint-repo-sh: ## Lint every shell script
	git ls-files '*.sh' | xargs shellcheck -x

# scripts/check-markdown-links.sh and scripts/check-claude-md.sh
lint-repo-md: ## Check local paths and anchors in every Markdown file, and every tracked CLAUDE.md against its byte budget
	sh scripts/check-markdown-links.sh
	sh scripts/check-claude-md.sh

# actionlint type-checks the workflows and composite actions and shellchecks
# their `run:` blocks; zizmor audits them against .github/ci-config/zizmor.yml.
# zizmor's online audits run only when GH_TOKEN is set, as in CI. tools/go.mod
# pins actionlint. tools/cicallers checks that ci.yml, coverage.yml and
# stress.yml call every area workflow beside them.
lint-repo-yaml: ## Lint every YAML file, then the workflows with actionlint, zizmor and cicallers
	uv tool run yamllint@1.38.0 --strict .
	sc=$$(command -v shellcheck) || { echo "shellcheck not found" >&2; exit 1; }; \
		cd tools && SHELLCHECK_OPTS="--severity=warning --exclude=SC2155" \
		go tool actionlint -shellcheck "$$sc" -config-file ../.github/ci-config/actionlint.yaml
	uv tool run zizmor@1.31.1 --config .github/ci-config/zizmor.yml .
	cd tools && go run ./cicallers ../.github/workflows

# scripts/check-toolchain-versions.sh
lint-repo-toolchains: ## Check the Dockerfiles' golang, node and rust images, and the desktop's Rust and pnpm pins, match CI's versions
	sh scripts/check-toolchain-versions.sh

# scripts/check-image-digests.sh
lint-repo-digests: ## Check every image a Dockerfile pulls is pinned by digest
	sh scripts/check-image-digests.sh

test-repo: ## Test the scripts with bats, and scripts/stress.go
	bats scripts/
	go test scripts/stress.go scripts/stress_test.go

# scripts/generate.sh
generate: ## Regenerate the committed protoc, gqlgen, codegen and CLI config output, the desktop's included
	bash scripts/generate.sh

# scripts/check-generated.sh
check-generated: ## Regenerate the committed output with generate, and fail on a change
	bash scripts/check-generated.sh

# Prints each target's `##` description.
help: ## Show this help message
	@awk 'BEGIN {FS = ":.*## "} /^[a-z0-9-]+:.*## / {printf "  %-32s %s\n", $$1, $$2}' $(MAKEFILE_LIST)
