---
title: Each Go module builds without go.work
date: 2026-11-09
scope: repo
status: Accepted
---

# Each Go module builds without go.work

## Context

The Go code became several modules in #113 (`66fde8c2`, September 2024), which split the backend into `agent`, `common` and `server`. The same commit added both a `go.work` that `use`s every module and a `replace` in each `go.mod` pointing at its sibling (`replace .../backend/common => ../common`). The siblings are required at the zero pseudo-version `v0.0.0-00010101000000-000000000000`. No module has ever been tagged for the Go module proxy, so the `replace` is the only way a module finds its sibling outside the workspace.

At first the release Dockerfiles built in workspace mode. They copied `go.work`, `go.work.sum` and the `go.mod` and `go.sum` of every module, the CLI and agent included, into each image's build stage. #127 (`68234d89`, October 2024) rewrote them: each Dockerfile copies only the modules its component needs, drops `go.work`, and runs `go mod download all` and `go build` inside the component's module. The PR description lists the directory renames and the workflow changes; it does not give the reason for this change. A workspace build needs every module that `go.work` uses to be present, which is why the earlier Dockerfiles had to copy them all.

#743 (`654c56a8`, October 2025) moved the CLI release to vendored source. `release-cli.yml` moves the built UI into `modules/dashboard/website`, runs `go mod vendor` in `modules/cli` with `GOWORK=off`, publishes the tree as `kubetail-<version>-vendored.tar.gz`, and builds each binary with `go build -mod=vendor`, again with `GOWORK=off`. The PR gives its reason as builds that are "simpler, faster and easier to maintain". `go mod vendor` refuses to run in workspace mode, so `GOWORK=off` is required by the command. The AUR, OBS, Launchpad, MacPorts, Gentoo GURU and Alpine publish workflows all build from that tarball.

## Decision

Every `go.mod` under `modules/` carries a `replace` for each sibling it imports and a `go.sum` complete for a build of that module alone. `modules/go.work` exists for development, CI's tests and Tilt; no release build uses it. `Dockerfile.dashboard` copies `shared` and `dashboard`, `Dockerfile.cluster-api` copies `shared` and `cluster-api`, and `Dockerfile.cli` copies `shared`, `dashboard` and `cli`. The CLI release vendors and builds `modules/cli` with `GOWORK=off`.

## Alternatives considered

**Build in workspace mode everywhere.** What the Dockerfiles did from #113 to #127. Each image's build stage then needed every module's `go.mod`, including modules the component does not import, and `go mod vendor` cannot run in a workspace at all.

**Publish the shared modules and require tagged versions.** Not recorded as considered. No module has a module-path tag, and the zero pseudo-versions in the `require` lines show the siblings are never fetched from the proxy.

## Consequences

A Docker build's context per image is only the modules it compiles, and the CLI source tarball is self-contained: a packager builds it with Go alone, offline.

Two dependency views coexist. In the workspace, Go resolves one version of each dependency across all four modules (`go.work.sum` holds extra sums). Outside it, each module resolves on its own `go.mod`. Code that builds and passes tests in the workspace can fail to build alone: a missing `replace`, a `go.sum` line that only `go.work.sum` had, or a dependency version that the workspace raised. A pin such as the `fsnotify` `replace` has to be written into each module for the same reason.

Nothing in the ordinary CI path builds a module alone. The Go jobs run in the workspace (`go work vendor`). The Docker builds run only when a PR touches `modules/`, `crates/` and `dashboard-ui/` together, and the vendored build runs only at release. A new sibling import needs its `replace` and a `COPY` in the component's Dockerfile, and either can be missed until then.

A module whose `go.mod` has a `replace` cannot be installed with `go install <module>@<version>`, so `go install` is not a way to get the CLI.

## Revisit when

The modules are published with real versions, or merged into fewer modules, so that no `replace` is needed. Or CI gains a job that builds each module with `GOWORK=off` on every PR, which would remove the late failure without changing the decision.
