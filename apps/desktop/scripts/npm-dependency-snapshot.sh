#!/bin/sh
# Prints a dependency-graph snapshot of the npm packages in pnpm-lock.yaml,
# for GitHub's dependency submission API. The graph reads only the first YAML
# document of pnpm's lockfile, which pins pnpm itself, and Dependabot alerts
# read the graph. Run from the repo root:
#
#	scripts/npm-dependency-snapshot.sh <commit-sha> <git-ref> >snapshot.json
#	gh api repos/{owner}/{repo}/dependency-graph/snapshots --input snapshot.json
set -eu

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

pnpm sbom --sbom-format spdx --lockfile-only >"$tmp/all.json"
pnpm sbom --sbom-format spdx --lockfile-only --prod >"$tmp/prod.json"

# A package the production tree holds is runtime, the rest development; one
# the root depends on is direct, the rest indirect.
jq -n \
	--slurpfile all "$tmp/all.json" \
	--slurpfile prod "$tmp/prod.json" \
	--arg sha "$1" \
	--arg ref "$2" \
	--arg pnpm "$(pnpm --version)" \
	--arg job "${GITHUB_RUN_ID:-local}" '
	def purls: .packages
		| map(select(.SPDXID != "SPDXRef-RootPackage")
			| {key: .SPDXID, value: (.externalRefs[] | select(.referenceType == "purl") | .referenceLocator)})
		| from_entries;
	($all[0] | purls) as $ids
	| [$prod[0] | purls | .[]] as $runtime
	| [$all[0].relationships[]
		| select(.spdxElementId == "SPDXRef-RootPackage" and .relationshipType == "DEPENDS_ON")
		| $ids[.relatedSpdxElement]] as $direct
	| {
		version: 0,
		sha: $sha,
		ref: $ref,
		job: {correlator: "pnpm-lock", id: $job},
		detector: {name: "pnpm sbom", version: $pnpm, url: "https://pnpm.io/cli/sbom"},
		scanned: (now | todate),
		manifests: {
			"pnpm-lock.yaml": {
				name: "pnpm-lock.yaml",
				file: {source_location: "pnpm-lock.yaml"},
				resolved: ([$ids[] | {
					key: .,
					value: {
						package_url: .,
						relationship: (if IN($direct[]) then "direct" else "indirect" end),
						scope: (if IN($runtime[]) then "runtime" else "development" end)
					}
				}] | from_entries)
			}
		}
	}'
