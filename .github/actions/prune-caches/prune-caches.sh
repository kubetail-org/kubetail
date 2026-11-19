#!/bin/sh
# Deletes every Actions cache on a ref but the newest of each family, so a
# ref holds one copy of each cache. A family is a key without the part a save
# changes: the Go caches' `-run-<id>`, rust-cache's two trailing hashes, and
# the pnpm store's and setup-uv's lockfile hash. A key in no family, such as a
# BuildKit blob, is never deleted.
#
# Given names, it prunes only the caches whose key holds one of them between
# dashes (`cli-test` in `go-1.26.11-cli-test-linux-amd64-run-1`), so each
# workflow prunes its own.
#
# Keeping the newest copy, rather than this run's, means a failed save leaves
# the last good cache in place.
#
#   sh .github/actions/prune-caches/prune-caches.sh <ref> [<name>...]
#
# Needs GH_TOKEN with actions: write, and GITHUB_REPOSITORY.
set -eu

ref=$1
shift
names=$(jq -cn '$ARGS.positional' --args "$@")

gh api --paginate "repos/$GITHUB_REPOSITORY/actions/caches?ref=$ref&per_page=100" \
	--jq '.actions_caches[]' | jq -rs --argjson names "$names" '
	def family:
		if test("-run-[0-9]+$") then sub("-run-[0-9]+$"; "-run-")
		elif startswith("v0-rust-") then sub("-[0-9a-f]+-[0-9a-f]+$"; "")
		elif startswith("node-cache-") or startswith("setup-uv-") then sub("-[0-9a-f]+$"; "")
		else . end;
	map(select(.key as $key | $names == [] or any($names[] as $n | $key | test("(^|-)" + $n + "-"); .)))
	| group_by(.key | family)[]
	| sort_by(.created_at) | reverse | .[1:][]
	| [.id, .key] | @tsv
' | while IFS="$(printf '\t')" read -r id key; do
	echo "deleting $key"
	# A concurrent run may have deleted it first.
	gh api -X DELETE "repos/$GITHUB_REPOSITORY/actions/caches/$id" > /dev/null ||
		echo "::warning::could not delete $key"
done
