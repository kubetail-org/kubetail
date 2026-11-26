---
title: Group the sidecar's packages into layers
date: 2026-10-07
scope: sidecar
status: Accepted
---

# Group the sidecar's packages into layers

## Context

[Services, lib and the rest](2026-10-06-sidecar-packages-group-into-services-lib-and-the-rest.md)
grouped `sidecar/internal/` into `services/` and `lib/` by *kind* — a service owns state, a
building block knows nothing of the domain — and left eleven packages at the top. It said to
revisit once they settled into groups of their own.

The per-package import graph is already layered, and the compiler enforces it. Reading it from the
bottom: `llm`, `permissions` and `sandbox` import nothing of ours; `session`, `kubeproxy` and
`loginshell` import only each other; the `tools` contract imports `llm`, `permissions` and
`session`; the services import `appdb` and those leaves; `clustercard` imports `services/cluster`;
the tool implementations import the services and `clustercard`; the `agent` loop imports the
`tools` contract; `catalog` imports every tool; `chat` imports everything. The directory listing
showed none of it, and `chat` sat in `services/` as a peer of `poke` while depending on the whole
stack above it.

## Decision

A folder under `internal/` is a layer: every import between two folders points the same way.
Packages are moved, never merged or split, with one rename.

```
internal/
  lib/            building blocks that know nothing of Kubetail's domain
  appdb/          the handle the services are built from
  llm/            a call to a model; imports nothing of ours
  run/            what confines one run: permissions, sandbox, session, kubeproxy, loginshell
  services/       kubeconfig, poke, cluster, auth, securityconfig, memory
    cluster/clustercard   what a run is told about the cluster, a projection of the service
  tools/          the contract at the root, the box tools under it
  agent/          the agent itself, everything above tools: loop, catalog, chat
  app/            the composition root
```

The folder order is `lib → appdb → llm → run → services → tools → agent → app`. `tools/` spans two levels in the way `net` and `net/http` do: the root is the
contract, the children are implementations that depend on more.

`run/` holds the leaves the tools stand on. None of them runs an agent, and `agent` would have
said the opposite of what they are. The loop package is `loop`, since `agent/agent` stutters and
`tools/agent` is the `Agent` tool.

`chat` leaves `services/`. It owns rows and is reached from `graph/`, so by the earlier rule it is
a service; by dependency it is the application, the one importer of `loop`. The rule stands for
every other package: `services/` holds the services the tools stand on — `bash` and `kubequery`
read `cluster`, `memory` reads `memory`, `bash` reads `securityconfig` — and `agent/` the one
service that stands on the tools. The folder does not hold every service, as `net/http` is not
all of HTTP; it holds the ones a tool may import. `catalog` goes beside it: it names the
providers and the tools each is offered, which is the agent's configuration, and it stays its own
package as [the catalog ADR](2026-09-24-the-catalog-is-its-own-package.md) decides.

`clustercard` is the one package placed by where it is used rather than what it is. `tools/bash`
and `tools/kubequery` read it for the context name and the freshness verdict, so it sits below
the tools, and it imports `services/cluster` alone, so it sits above the services. It is the
cluster service's view for a model, and `services/auth/oauth` is the precedent for a non-service
child under a service's folder.

One edge is reversed in code. `services/memory` read `clustercard.SectionSize` and
`clustercard.UnavailableSection` to bound a scope's notes by what they cost in the card, which put
a service above the card. The card's escape — a backtick as ```, so a value cannot close the
fence — is now `lib/fencejson`, which both import; the sentinel is `memory.UnavailableSection`,
and `chat` reads it there.

## Alternatives considered

- **Group by topic: `llm/` and `agent/`, with `agent/` holding the loop and the confinement
  leaves.** The issue's proposal. It reads well, but the loop sits above `tools` and the leaves
  below it, so `tools ↔ agent` reference each other at folder level and the folder says nothing
  about which way an import may point. A folder that is not a layer is a label.
- **Move `tools/` under `agent/`.** Collapses one back-edge by putting both ends in one folder
  and leaves the others; `agent/` becomes a ten-package bag holding the leaves and what is built
  on them.
- **Keep `chat` in `services/`.** Then `services/` spans from `poke` to the top of the stack and
  no arrangement of the other folders can be one-way. The kind rule and the layer rule are
  orthogonal, and one folder level can carry only one of them.
- **Keep `clustercard` in `agent/`.** Two tools import it, so `tools → agent` would stand.
- **Rename `clustercard` to `card`.** `cluster/clustercard` stutters, but the rename touches
  every call site, and the issue's rule is to move, never rename, beyond `agent` → `loop`.
- **Rename `llm` to `core`.** Go names a package for what it provides: `core.Request` says
  nothing where `llm.Request` says everything.

## Consequences

Where a package sits now says what it may import: anything in a folder to its left. A new
package is placed by its imports, and a `go list` over `internal/...` grouped by first path
segment is the check, and `TestInternalFoldersAreLayers` (`internal/app/app_test.go`) walks
every import under `internal/` against the order.

Paths are baked into more than imports. gqlgen spells import paths in its generated names, so
`graph/generated.go` is regenerated from `gqlgen.yml`, whose `chat`, `permissions` and `sandbox`
bindings name the new paths; the `livecache` and `liveswitch` targets in the `Makefile` name
`./internal/agent/catalog`.

## Revisit when

A package needs to import across the order — a service that reads a tool, a leaf in `run/` that
reads a service. Then either the import is wrong or the layer is, and the ADR is reopened rather
than the edge quietly added.
