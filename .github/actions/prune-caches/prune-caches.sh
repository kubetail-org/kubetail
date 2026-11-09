#!/bin/sh
# Deletes every Actions cache on a ref but the newest of each family, so a
# ref holds one copy of each cache. A family is a key without the part a save
# changes: the Go caches' `-run-<id>`, rust-cache's two trailing hashes, and
# setup-node's lockfile hash. A key in no family, such as a BuildKit blob, is
# never deleted.
#
# Keeping the newest copy, rather than this run's, means a failed save leaves
# the last good cache in place.
#
#   sh .github/actions/prune-caches/prune-caches.sh <ref>
#
# Needs GH_TOKEN with actions: write, and GITHUB_REPOSITORY.
set -eu

gh api --paginate "repos/$GITHUB_REPOSITORY/actions/caches?ref=$1&per_page=100" \
	--jq '.actions_caches[]' | jq -rs '
	def family:
		if test("-run-[0-9]+$") then sub("-run-[0-9]+$"; "-run-")
		elif startswith("v0-rust-") then sub("-[0-9a-f]+-[0-9a-f]+$"; "")
		elif startswith("node-cache-") then sub("-[0-9a-f]+$"; "")
		else . end;
	group_by(.key | family)[]
	| sort_by(.created_at) | reverse | .[1:][]
	| [.id, .key] | @tsv
' | while IFS="$(printf '\t')" read -r id key; do
	echo "deleting $key"
	# A concurrent run may have deleted it first.
	gh api -X DELETE "repos/$GITHUB_REPOSITORY/actions/caches/$id" > /dev/null ||
		echo "::warning::could not delete $key"
done
