#!/bin/sh
# Every hand-written Go, Rust and TypeScript file opens with the Apache 2.0
# header below. Prints every file that does not, and fails. Generated code is
# left out, schema.resolvers.go too: gqlgen rewrites it without the header.
#
#   sh scripts/check-license-headers.sh [dir...]   # default: every language's tree
set -eu
cd "$(dirname "$0")/.."

[ "$#" -gt 0 ] || set -- modules crates dashboard-ui tools scripts

header='// Copyright 2024 The Kubetail Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.'
lines=$(printf '%s\n' "$header" | wc -l)

missing=$(
  for dir in "$@"; do
    git ls-files --cached --others --exclude-standard -- \
      "$dir/*.go" "$dir/*.rs" "$dir/*.ts" "$dir/*.tsx"
  done |
    grep -vE '(^|/)(generated\.go|models_gen\.go|schema\.resolvers\.go)$|/__generated__/' |
    while IFS= read -r f; do
      # A Windows checkout may end each line in CRLF.
      [ "$(head -n "$lines" "$f" | tr -d '\r')" = "$header" ] || echo "$f"
    done
)
if [ -n "$missing" ]; then
  echo "$missing"
  echo "files above need the Apache 2.0 header; copy it from a neighbouring file" >&2
  exit 1
fi
