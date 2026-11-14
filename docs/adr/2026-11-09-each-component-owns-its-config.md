---
title: Each component owns its config
date: 2026-11-09
scope: repo
status: Accepted
---

# Each component owns its config

## Context

Until January 2026 the Dashboard, the Cluster API and the Cluster Agent shared one config struct in `modules/shared/config/config.go`. Each component's options sat under a top-level parent (`.Dashboard`, `.ClusterAPI`, `.ClusterAgent`), and `hack/config.yaml` was a single 517-line seed file with `dashboard:`, `cluster-api:` and `cluster-agent:` sections.

Issue #873 (January 2026) gives the reasons to split it. The `ClusterAgent` part of the Go struct was already obsolete, because the agent had been rewritten in Rust (`2d005c27`, August 2025) and read its own `crates/cluster_agent/src/config.rs`. The CLI had just gained its own standalone config file (#867), which lived in `modules/shared/config/cli.go` but had no top-level parent. In the issue's words, the repo was "already moving towards component-specific configs instead of one monolith", and the task was to finish the job. The Helm chart already generated one config per component (`kubetail.dashboard.config`, `kubetail.clusterAPI.config`, `kubetail.clusterAgent.config`).

#876 did the split and was merged in January 2026 (`74c2b0d3`). The suggested `config/default/` layout came from a maintainer comment on #873.

## Decision

Each component owns its config struct, its defaults and its loader:

- `modules/dashboard/pkg/config`: `Config`, `DefaultConfig`, `NewConfig(configPath, v)`.
- `modules/cluster-api/pkg/config`: `Config`, `DefaultConfig`, `NewConfig(configPath, v)`.
- `modules/cli/pkg/config`: `Config`, `DefaultCLIConfig`, `NewConfig(configPath, v)`.
- `crates/cluster_agent/src/config.rs`: `Config::parse`, with defaults in `builder_with_defaults`.

No component's keys sit under a parent named for the component. `modules/shared/config` holds only the `Environment` type and its two values, which the shared packages need to pick an implementation per environment.

The defaults are set in each component's code. `config/default/` holds one documented default file per component (`dashboard.yaml`, `cluster-api.yaml`, `cluster-agent.yaml`, and `cli.{yaml,toml,json}`), and `modules/cli/assets/` embeds copies of the CLI's.

## Alternatives considered

**Keep one shared struct with a parent key per component.** This was the existing design. #873 rejects it because it no longer matched the components: one of its sections described a component written in another language, and the newest config (the CLI's) had already been written outside it.

**A Go module at `modules/cluster-agent` to host the agent's config.** Raised on #873 when the issue's checklist named `modules/cluster-agent/pkg/config`. Rejected by the maintainer: the agent is Rust, so its config stays in `crates/cluster_agent/src/config.rs` and only loses its `cluster-agent` prefix.

**Move the shared types to `modules/shared/configtypes`.** The description of #876 says it does this. The merged commit instead keeps `modules/shared/config` with `Environment` alone. Why the package kept its name is not recorded.

## Consequences

A component reads and validates only its own keys, and a config file for one component can be read without knowing the others. The Helm chart's per-component config generators map one-to-one onto these files.

The cost is that each default lives in two places: the component's code and its file in `config/default/`. Nothing checks that they match, so a changed default needs both edits. The CLI's file lives in a third place, `modules/cli/assets/`, also unchecked.

The CLI imports `modules/dashboard/pkg/config` to run `kubetail serve`, so a change to the Dashboard's `DefaultConfig` changes the CLI's behaviour too. The split was a breaking change to every deployed config file, which is why the maintainer held #876 until after a release and asked for the Helm chart to be updated in step.
