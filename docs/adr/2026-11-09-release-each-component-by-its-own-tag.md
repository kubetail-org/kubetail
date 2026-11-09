---
title: Release each component by its own tag
date: 2026-11-09
scope: repo
status: Accepted
---

# Release each component by its own tag

## Context

From February to October 2024 the repo had one version. Tags were plain semver (`0.1.1` to `0.8.0-rc2`), and `.github/workflows/release.yml` ran on any tag and built both images, `kubetail-server` and `kubetail-agent`, under that version.

In October 2024 #127 added a release workflow for the CLI, and split the image workflow in two. Tags gained a component prefix: `cli/v0.0.1` (October 2024), then `server/v0.9.0` and `agent/v0.9.0`. In January 2025 #149 split the server into the Dashboard and the Cluster API and renamed the agent to the Cluster Agent. The tags became `dashboard/v…`, `cluster-api/v…` and `cluster-agent/v…`, each starting again at v0.1.0. #761 (November 2025) folded the three image workflows into one `release-docker.yml` that picks the component from the tag.

The reasoning for leaving one repo-wide version is not recorded in #127, #149 or their commits. What the record does show is that the components already shipped on different schedules soon after: by October 2026 the CLI had reached v0.18, the Dashboard v0.17, the Cluster API v0.8 and the Cluster Agent v0.7.

## Decision

Each component is released by a tag of its own: `cli/v<semver>`, `dashboard/v<semver>`, `cluster-api/v<semver>`, `cluster-agent/v<semver>`.

- `release-cli.yml` runs on `cli/v*`, builds and signs the CLI and opens a draft GitHub release.
- `release-docker.yml` runs on all four prefixes and builds the component's image.
- The `publish-*` workflows run when a release is published and strip `cli/v` from the tag to get the version for each package manager.
- `.claude/skills/release-cli` and `.claude/skills/release-cluster` find each component's latest tag, list the commits under its paths since then, and propose the next version. A person invokes them.
- `modules/shared/versioncheck` finds the latest CLI with `^cli/v(\d+\.\d+\.\d+)$`.

The version a cluster user sees is the Helm chart's, released from `kubetail-org/helm-charts` as `kubetail-<semver>`; `versioncheck` reads that tag for cluster upgrade notices.

## Alternatives considered

**One version for the whole repo.** The scheme in use until October 2024: a plain tag released every component. No PR or commit says why it was dropped.

No other scheme is mentioned in the record.

## Consequences

A component releases only when its own code changes, and its version says something about that component alone. A CLI fix does not re-release the cluster images, and a Cluster Agent fix does not ship a CLI.

The cost is four version lines to track. Which component a commit belongs to is decided by path, and `modules/shared` and `crates/types` count toward every component that uses them; the release skills encode that mapping, and nothing else does. The components' versions say nothing about compatibility with each other. A user of the cluster pieces installs a Helm chart version, not a component version.

The tag scheme is a contract. `versioncheck`'s regex, the `GITHUB_REF#refs/tags/cli/v` strip in every `publish-*` workflow and the `on.push.tags` filters all parse it, so renaming a prefix breaks the CLI's update check for every installed CLI and silently stops the publish and image workflows.

## Revisit when

The components need to be released and installed in lockstep, for instance if a CLI version can only work with one Cluster API version.
