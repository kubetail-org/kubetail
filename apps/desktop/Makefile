# Polyglot task entry point. The Go sidecar lives outside the JS world,
# so it doesn't belong in package.json scripts; this Makefile is the
# place where Go, Rust, and JS commands meet.
#
# Each target is `<verb>` for every area or `<verb>-<area>` for one. `make help`
# lists them.

.DEFAULT_GOAL := help

# No target names a file it makes.
.PHONY: build-sidecar build-sidecar-dev build-bwrap build-pasta build-embedded-sources \
	generate-proto generate-prompts generate-icons \
	lint lint-go lint-rust lint-ts lint-tailwind lint-yaml lint-sh lint-md lint-claude-md lint-markers lint-design lint-security-model \
	design-status design-record \
	vet vet-go vet-go-changed vet-rust \
	test test-go test-rust test-ts test-changed test-livecache test-liveswitch \
	cover-go cover-ts cover-changed \
	check check-changed clean clean-data help

# Tauri's externalBin picks up src-tauri/binaries/kubetail-sidecar-<rust-host-triple>.
build-sidecar: ## Build the Go sidecar into src-tauri/binaries/, untagged, as every release does
	go run scripts/build-sidecar.go

# `tauri dev` runs the same build.
build-sidecar-dev: ## Build the sidecar under -tags debug, which lets KUBETAIL_OAUTH_ISSUER and friends redirect it
	go run scripts/build-sidecar.go -tags debug

build-bwrap: ## Build the pinned bwrap the Linux packages carry into src-tauri/linux/ (Linux only)
	bash scripts/build-bwrap.sh

build-pasta: ## Build the pinned pasta the Linux packages carry into src-tauri/linux/ (Linux only)
	bash scripts/build-pasta.sh

build-embedded-sources: ## Bundle bwrap's and pasta's source into src-tauri/linux/embedded-sources.tar.gz
	bash scripts/build-embedded-sources.sh

# The //go:generate directives in sidecar/grpc/ run scripts/protoc.sh, which
# fetches the pinned protoc; the Go bindings are committed. src-tauri/build.rs
# emits the Rust ones on every build, so the cargo build surfaces their errors.
# tauri-build fails without the bundled sidecar binary, built after the Go
# bindings it compiles.
generate-proto: ## Regenerate the gRPC bindings from proto/, for Go and Rust
	cd sidecar && go generate ./grpc/...
	go run scripts/build-sidecar.go
	cd src-tauri && cargo build

# The goldens are the system prompt and every tool's offer, per kind of run and
# machine (sidecar/internal/app/testdata/prompt/), a question's context block and
# the subagent's assembled prompt (sidecar/internal/agent/chat/testdata/). Review
# the diff: it is the model's view of the change, byte for byte where the cache
# depends on it.
generate-prompts: ## Rewrite the goldens that pin what the model reads, after changing a prompt or an offer
	cd sidecar && go test ./internal/app ./internal/agent/chat -run 'MatchesItsGolden|TestTheSubagentsPromptIsAssembledInOrder' -update

# Kubetail ships no mobile app, so the Android and iOS sets are dropped. icon.icns
# comes from app-macos.svg, which follows Apple's icon grid. tray.png comes from
# tray-macos.svg at 18pt @2x, the height tray-icon draws a macOS tray image at.
generate-icons: ## Regenerate src-tauri/icons/, public/favicon.ico and src/assets/icons/ from src-icons/
	rm -rf src-tauri/icons
	pnpm tauri icon src-icons/app-light.svg
	rm -rf src-tauri/icons/android src-tauri/icons/ios
	pnpm tauri icon src-icons/app-macos.svg -o src-tauri/icons/macos
	mv src-tauri/icons/macos/icon.icns src-tauri/icons/icon.icns
	rm -rf src-tauri/icons/macos
	pnpm tauri icon src-icons/tray-macos.svg --png 36 -o src-tauri/icons/tray
	mv src-tauri/icons/tray/36x36.png src-tauri/icons/tray.png
	rmdir src-tauri/icons/tray
	cp src-tauri/icons/icon.ico public/favicon.ico
	mkdir -p src/assets/icons
	cp src-icons/app-light.svg src-icons/app-dark.svg src/assets/icons/

lint: lint-go lint-rust lint-ts lint-tailwind lint-yaml lint-sh lint-md lint-claude-md lint-markers lint-design lint-security-model ## Run every linter CI runs

lint-go: ## Check the sidecar's Go formatting
	@unformatted=$$(gofmt -l sidecar); \
		if [ -n "$$unformatted" ]; then \
			echo "gofmt: files need formatting:"; echo "$$unformatted"; exit 1; \
		fi

lint-rust: ## Check the desktop host's Rust formatting
	cd src-tauri && cargo fmt --check

lint-ts: ## Lint and format-check the webview (pnpm lint)
	pnpm lint

# Apart from lint-ts for its time (.oxlintrc.tailwind.json).
lint-tailwind: ## Lint the webview's Tailwind classes
	pnpm lint:tailwind

# actionlint type-checks the workflows and composite actions and shellchecks
# their `run:` blocks; zizmor audits them against .github/ci-config/zizmor.yml.
# zizmor's online audits run only when GH_TOKEN is set, as in CI. The repo's tools/go.mod
# pins actionlint, where Dependabot moves it.
lint-yaml: ## Lint every YAML file, then the workflows with actionlint and zizmor
	uv tool run yamllint@1.38.0 --strict .
	sc=$$(sh scripts/shellcheck-path.sh) && cd ../../tools && \
		go tool actionlint -shellcheck "$$sc" -config-file ../apps/desktop/.github/ci-config/actionlint.yaml
	uv tool run zizmor@1.31.1 --config .github/ci-config/zizmor.yml .

# scripts/shellcheck-path.sh pins shellcheck; -x follows scripts/changed.sh
# into the scripts that source it.
lint-sh: ## Lint the shell scripts with shellcheck
	sc=$$(sh scripts/shellcheck-path.sh) && "$$sc" -x scripts/*.sh src-tauri/linux/*.sh

# Offline, so CI never depends on the network.
lint-md: ## Check local paths and anchors in tracked Markdown
	sh scripts/check-markdown-links.sh

lint-claude-md: ## Check the size budgets of tracked CLAUDE.md guides
	sh scripts/check-claude-md.sh

lint-markers: ## Check every coverage marker gives its reason
	sh scripts/check-coverage-markers.sh

lint-design: ## Check the design manifest against the code it names
	cd ../../tools && go run ./designsync check

lint-security-model: ## Check the security model against the tests and files it cites
	cd ../../tools && go run ./securitymodel check

# designsync reads Figma when FIGMA_TOKEN is set. On macOS an unset one comes
# from the Keychain item kubetail-figma-token; without either, the code side alone.
design-status design-record: export FIGMA_TOKEN ?= $(shell security find-generic-password -s kubetail-figma-token -w 2>/dev/null)

design-status: ## List the components and screens whose code or design moved since their last sync, reading Figma with FIGMA_TOKEN
	cd ../../tools && go run ./designsync status

design-record: ## Record the entries in IDS as in sync: their code's blobs and, with FIGMA_TOKEN, Figma's hash
	cd ../../tools && go run ./designsync record $(IDS)

vet: vet-go vet-rust ## Run go vet, staticcheck and clippy

# Every GOOS, both Linux architectures and each build tag, as CI's Vet · Go
# does. The live checks are behind tags no other target builds; vetting them
# here keeps them compiling. scripts/staticcheck.sh runs once per GOOS and tag,
# merged.
vet-go: ## Vet every Go module per GOOS and tag, then the sidecar's deps, go fix and staticcheck
	cd sidecar && GOOS=linux GOARCH=amd64 go vet ./... && GOOS=linux GOARCH=arm64 go vet ./... && GOOS=darwin go vet ./... && GOOS=windows go vet ./...
	cd sidecar && go vet -tags debug ./... && go vet -tags livecache ./internal/agent/catalog && go vet -tags liveswitch ./internal/agent/catalog
	sh scripts/check-sidecar-deps.sh
	sh scripts/check-go-fix.sh
	sh scripts/staticcheck.sh

# scripts/vet-go-changed.sh. CI vets every package.
vet-go-changed: ## Run go vet and go fix on the Go packages this branch changed, on this platform (ALL_PLATFORMS=1 adds staticcheck and every GOOS and tag)
	bash scripts/vet-go-changed.sh

# tauri-build fails without the bundled sidecar binary, so build it first.
vet-rust: build-sidecar ## Run clippy on the desktop host, tests included
	cd src-tauri && cargo clippy --all-targets -- -D warnings

test: test-go test-rust test-ts ## Run every Go, Rust and TypeScript test

test-go: ## Test the sidecar
	cd sidecar && go test ./...

# tauri-build fails without the bundled sidecar binary, so build it first.
test-rust: build-sidecar ## Test the desktop host
	cd src-tauri && cargo test

test-ts: ## Test the webview
	pnpm test --run

# scripts/test-changed.sh. CI runs every suite.
test-changed: ## Run the tests for what this branch changed
	bash scripts/test-changed.sh

# Reaches the vendors and spends real tokens, so nothing runs it automatically:
# run it before a catalog change lands and paste its lines into the PR.
test-livecache: ## Check every model whose key is set reads its second turn from the cache
	cd sidecar && go test -tags livecache -count=1 -v -run TestEveryModelReadsItsSecondTurnFromTheCache ./internal/agent/catalog

# Anthropic, OpenAI, a Chat Completions vendor, Anthropic again, each step a
# real tool round. It spends real tokens, so nothing runs it automatically: run
# it before a change to how a wire replays another's rows lands.
test-liveswitch: ## Walk one chat across every dialect with the keys in the environment
	cd sidecar && go test -tags liveswitch -count=1 -v -run TestAChatSwitchesAcrossEveryDialect ./internal/agent/catalog

# Not a gate: CI's coverage gate is. `go tool cover -html=sidecar/coverage.out`
# opens it line by line.
cover-go: ## Print this OS's Go coverage per function
	cd sidecar && go test -coverpkg=./... -coverprofile=coverage.out ./... && go tool cover -func=coverage.out

# Not a gate: CI's coverage gate is.
cover-ts: ## Print the webview's coverage per file
	pnpm vitest run --mode coverage

# scripts/cover-changed.sh. CI's coverage gate has the last word.
cover-changed: ## List the changed lines this OS's tests leave uncovered (FULL=1 runs every Go test)
	bash scripts/cover-changed.sh

check: lint vet test ## Run every lint, vet and test

# scripts/check-changed.sh
check-changed: ## Run the checks for what this branch changed, on this platform, before opening a pull request (ALL_PLATFORMS=1 adds staticcheck and every GOOS and tag)
	bash scripts/check-changed.sh

clean: ## Remove the sidecar binaries, Cargo's target/, Go's test cache and the dev app's saved window frame
	rm -rf src-tauri/binaries
	rm -rf src-tauri/target
	cd sidecar && go clean -testcache
	go -C sidecar run ./scripts/clean-dev-data.go -window-state

# Separate from `clean`, so clearing build outputs never deletes a developer's chats.
clean-data: ## Delete the dev app's data, cache, config and state directories (database, chats, settings, mirror, logs)
	go -C sidecar run ./scripts/clean-dev-data.go

# Prints each target's `##` description.
help: ## Show this help message
	@awk 'BEGIN {FS = ":.*## "} /^[a-z0-9-]+:.*## / {printf "  %-24s %s\n", $$1, $$2}' $(MAKEFILE_LIST)
