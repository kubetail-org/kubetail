---
title: Group the sidecar's packages into services, lib and the rest
date: 2026-10-06
scope: sidecar
status: Accepted
amended_by: [Group the sidecar's packages into layers](2026-10-07-sidecar-folders-are-layers.md)
---

# Group the sidecar's packages into services, lib and the rest

## Context

`sidecar/internal/` held 37 packages side by side. Services, generic building blocks and domain
packages sat together, so the directory said nothing about what each one was. `internal/tools/`
already grouped the tools under one folder, and that grouping worked: the folder says what a
package is, and each package keeps its own name.

## Decision

Group the packages into folders by one rule. Packages are moved, never merged or split.

- **`internal/services/`** holds a package that owns state outliving a request (rows, a watch, a
  connection, goroutines) and is reached from `graph/` or `grpc/`: `auth`, `chat`, `cloud`,
  `cluster`, `kubeconfig`, `memory`, `poke`, `securityconfig`.
- **`internal/lib/`** holds a building block that knows nothing of Kubetail's domain: `apimeta`,
  `atomicjson`, `deltafold`, `drain`, `ipc`, `lifecycle`, `logging`, `rawjson`, `rootdir`, `safe`,
  `sqlitemigrate`, `sqlitepool`, `sqlstmt`, `supervisor`, `testutil`, `version`, `workqueue`.
  `lib` is a folder, never a package name.
- **The rest** stay at the top of `internal/`: `app`, `tools`, and the domain packages that are
  neither (`agent`, `appdb`, `catalog`, `clustercard`, `kubeproxy`, `llm`, `loginshell`,
  `permissions`, `sandbox`, `session`).

Under `services/` the `svc` suffix is dropped: `chatsvc` is `chat`, `clustersvc` is `cluster`,
`memorysvc` is `memory`. Where a file imports both memory packages, the tool is imported as
`memorytool`, as `tools/agent` is imported as `agenttool`.

`appdb` and `llm` are reached from `graph/`, but neither is a service by this rule: `appdb` is the
handle services are built from, and `llm` is a provider map with no lifecycle of its own.
`supervisor` is in `lib/` rather than under `services/cluster/internal/`, although the cluster
service is its only user today: it knows nothing of clusters, and other services are meant to
run on it.

## Alternatives considered

- **Merge packages into one `services` and one `lib` package.** Fewer import lines, but Go names a
  package for what it provides, so `lib.Redact` says less than `safe.Redact`. It also erases
  boundaries that are kept on purpose — `sqlstmt` imports nothing of ours, and `deltafold` imports
  `apimeta` alone — and one large services package would make its tests slow.
- **Both `lib/` and `util/`.** The words mean nearly the same thing, so every new package would
  reopen which one it belongs in.
- **Group by domain** (`cluster/`, `chat/`, `sandbox/`). Closer to how the code is used, but the
  domain packages' boundaries are not settled: `permissions` is used by the sandbox and the cluster
  proxy alike, so a `sandbox/` folder would draw a line the code does not.
- **A central `prompts` package.** `go:embed` cannot reach a parent directory, so every prompt
  would be exported from one place, away from the tool schema and tests it belongs with. Prompts
  stay beside the package that embeds them.

## Consequences

`internal/` has 14 entries instead of 37, and where a package sits says what kind of package it
is. A new package needs a decision on where it goes; the rule above makes that decision for a
service or a building block, and anything else goes at the top.

Paths are baked into more than imports, and a missed one fails quietly. `scripts/build-sidecar.go`
stamps the version through `-X …/internal/lib/version.Version`, and a wrong path ships `dev`
without failing the build. gqlgen's generated code spells import paths in its function names, so
it is regenerated from `gqlgen.yml`, never rewritten by hand.

## Revisit when

Several domain packages at the top settle into a group of their own, such as the sandbox and its
proxy. Then give them a folder, by the same rule of moving rather than merging.
