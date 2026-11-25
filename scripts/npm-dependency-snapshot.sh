#!/bin/sh
# Prints a dependency-graph snapshot of the npm packages in each app's
# pnpm-lock.yaml, apps/web's by default, for GitHub's dependency submission
# API. The graph reads only the first YAML document of pnpm's lockfile, which
# pins pnpm itself, and Dependabot alerts read the graph. Every app goes in one
# snapshot, since a snapshot replaces the last one with its correlator. Run
# from the repo root:
#
#	scripts/npm-dependency-snapshot.sh <commit-sha> <git-ref> [app...] >snapshot.json
#	gh api repos/{owner}/{repo}/dependency-graph/snapshots --input snapshot.json
set -eu

sha=$1
ref=$2
shift 2
[ $# -gt 0 ] || set -- apps/web

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

# A package the production tree holds is runtime, the rest development; one
# the root depends on is direct, the rest indirect.
for app in "$@"; do
	(cd "$app" && pnpm sbom --sbom-format spdx --lockfile-only) >"$tmp/all.json"
	(cd "$app" && pnpm sbom --sbom-format spdx --lockfile-only --prod) >"$tmp/prod.json"
	jq -n \
		--slurpfile all "$tmp/all.json" \
		--slurpfile prod "$tmp/prod.json" \
		--arg lockfile "$app/pnpm-lock.yaml" '
		def purls: .packages
			| map(select(.SPDXID != "SPDXRef-RootPackage")
				| {key: .SPDXID, value: (.externalRefs[] | select(.referenceType == "purl") | .referenceLocator)})
			| from_entries;
		($all[0] | purls) as $ids
		| [$prod[0] | purls | .[]] as $runtime
		| [$all[0].relationships[]
			| select(.spdxElementId == "SPDXRef-RootPackage" and .relationshipType == "DEPENDS_ON")
			| $ids[.relatedSpdxElement]] as $direct
		| {($lockfile): {
			name: $lockfile,
			file: {source_location: $lockfile},
			resolved: ([$ids[] | {
				key: .,
				value: {
					package_url: .,
					relationship: (if IN($direct[]) then "direct" else "indirect" end),
					scope: (if IN($runtime[]) then "runtime" else "development" end)
				}
			}] | from_entries)
		}}'
done >"$tmp/manifests.json"

jq -s \
	--arg sha "$sha" \
	--arg ref "$ref" \
	--arg pnpm "$(cd "$1" && pnpm --version)" \
	--arg job "${GITHUB_RUN_ID:-local}" '{
		version: 0,
		sha: $sha,
		ref: $ref,
		job: {correlator: "pnpm-lock", id: $job},
		detector: {name: "pnpm sbom", version: $pnpm, url: "https://pnpm.io/cli/sbom"},
		scanned: (now | todate),
		manifests: add
	}' "$tmp/manifests.json"
