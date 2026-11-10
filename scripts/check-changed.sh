#!/usr/bin/env bash
# Runs the checks for the areas the working tree's changes touch
# (scripts/changed.sh), so a change to one area needs only that area's
# toolchain. `make check` runs every area; CI runs what
# .github/ci-config/path-filters.yml selects.
#
#   always           lint-md, lint-toolchains and lint-digests, which need no
#                    toolchain, then test-changed and cover-changed
#   modules/         lint-go, vet-go
#   crates/, proto/  lint-rust, vet-rust
#   dashboard-ui/    lint-ts, vet-ts
#   e2e/             lint-python
#   *.sh             lint-sh
#   YAML, .github/   lint-yaml
set -euo pipefail
cd "$(dirname "$0")/.."

# shellcheck source=scripts/changed.sh
. scripts/changed.sh

targets=(lint-md lint-toolchains lint-digests)
if has '^modules/'; then targets+=(lint-go vet-go); fi
if has '^(crates|proto)/'; then targets+=(lint-rust vet-rust); fi
if has '^dashboard-ui/'; then targets+=(lint-ts vet-ts); fi
if has '^e2e/'; then targets+=(lint-python); fi
if has '\.sh$'; then targets+=(lint-sh); fi
if has '\.ya?ml$|^\.github/|^\.yamllint'; then targets+=(lint-yaml); fi
targets+=(test-changed cover-changed)

make "${targets[@]}"
