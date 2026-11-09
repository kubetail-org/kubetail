#!/bin/sh
# Fails if the npm tree in <head-dir> has an advisory, moderate or higher, that
# the tree in <base-dir> does not, so a pull request fails only for what it
# brings in. A finding is an advisory against one version, so a bump from one
# vulnerable version to another counts as new. Each tree's pnpm-workspace.yaml
# applies, its auditConfig.ignoreGhsas included.
#
#	scripts/audit-npm-diff.sh <base-dir> <head-dir>
set -eu

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

# Prints one "<GHSA> <package>@<version>" line per finding, sorted.
findings() {
	# pnpm audit exits 1 when it finds anything, so a report without
	# advisories is what marks the audit itself as failed.
	(cd "$1" && pnpm audit --json --audit-level moderate) >"$tmp/report.json" || true
	if ! jq -e 'has("advisories")' "$tmp/report.json" >/dev/null 2>&1; then
		echo "pnpm audit failed in $1:" >&2
		cat "$tmp/report.json" >&2
		exit 1
	fi
	jq -r '.advisories[] | .github_advisory_id as $id | .module_name as $name
		| .findings[] | "\($id) \($name)@\(.version)"' "$tmp/report.json" |
		LC_ALL=C sort -u
}

findings "$1" >"$tmp/base"
findings "$2" >"$tmp/head"

new=$(LC_ALL=C comm -13 "$tmp/base" "$tmp/head")
if [ -n "$new" ]; then
	echo "This change brings in npm advisories:"
	echo "$new" | while read -r id pkg; do
		echo "  $pkg  https://github.com/advisories/$id"
	done
	exit 1
fi
echo "No new npm advisories."
