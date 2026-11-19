#!/bin/sh
# Deletes the BuildKit caches on a ref that no current build needs: every
# index but the newest of each scope, and every blob last accessed before a
# cutoff.
#
# A `type=gha,mode=max` export looks up every blob its result needs, which
# updates the blob's last access. So once every build that exports has run, a
# blob none of them accessed is in no newest index. Blobs are stored by digest
# and shared across scopes: the cutoff must precede every build whose index is
# kept, and the prune runs only after all of them.
#
#   sh scripts/prune-buildkit-caches.sh <ref> <cutoff>
#
# <cutoff> is an ISO 8601 time, such as 2026-11-19T23:16:53Z.
# Needs GH_TOKEN with actions: write, and GITHUB_REPOSITORY.
set -eu

ref=$1
cutoff=$2

gh api --paginate "repos/$GITHUB_REPOSITORY/actions/caches?ref=$ref&per_page=100" \
	--jq '.actions_caches[]' | jq -rs --arg cutoff "$cutoff" '
	# The API gives fractional seconds, which fromdateiso8601 rejects.
	def seconds: sub("\\.[0-9]+"; "") | fromdateiso8601;
	($cutoff | seconds) as $cutoff
	| (map(select(.key | startswith("index-")))
		| group_by(.key | sub("#[0-9]+$"; ""))[]
		| sort_by(.created_at) | reverse | .[1:][]),
	  (.[] | select((.key | startswith("buildkit-blob-"))
		and (.last_accessed_at | seconds) < $cutoff))
	| [.id, .key] | @tsv
' | while IFS="$(printf '\t')" read -r id key; do
	echo "deleting $key"
	gh api -X DELETE "repos/$GITHUB_REPOSITORY/actions/caches/$id" > /dev/null ||
		echo "::warning::could not delete $key"
done
