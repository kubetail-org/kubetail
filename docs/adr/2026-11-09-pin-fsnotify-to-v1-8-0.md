---
title: Pin fsnotify to v1.8.0
date: 2026-11-09
scope: repo
status: Accepted
---

# Pin fsnotify to v1.8.0

## Context

fsnotify v1.9.0 (April 2025) changed how its kqueue backend handles symlinks, the backend used on macOS. Upstream issue fsnotify/fsnotify#689 reports the effect: when a watched symlink is removed and recreated in place (`ln -sfn`, an unlink then a symlink), v1.8.0 sends a `Create` event and v1.9.0 sends none. A bisect in that thread points at upstream commit `3ec05173`, "Fix relative links on kqueue". Kubelet's `/var/log/containers` is a directory of symlinks, which is what the Go Cluster Agent watched at the time.

The history in this repo runs in three steps. In June 2025 a dependency upgrade (#408) brought in v1.9.0, and the Cluster Agent's tests failed consistently on macOS and passed on Linux. #486 (`e14b4b47`) pinned v1.8.0 with a `replace` in every module, and #487 was opened to upgrade properly. #580 (`561f131a`, August 2025) removed the pin and upgraded to v1.9.0. The failures went unnoticed again because the affected code was tested only on Linux: when macOS joined the Go test matrix in #596, `cluster-api` and `cluster-agent` were excluded on it. #610 (`620cd85b`, August 2025) found the cause, linked fsnotify#689, restored the `replace` in every module and removed the macOS exclusions from CI.

#610 records the choice that was weighed. The regression was in Cluster Agent code only, which runs on Linux in production, so macOS could have been dropped for it instead. That would have degraded local development unless the agent's tests were also excluded on macOS, and #610 chose the pin, with a note to "revisit the decision later".

## Decision

Each Go module's `go.mod` (`modules/shared`, `modules/dashboard`, `modules/cluster-api`, `modules/cli`) carries `replace github.com/fsnotify/fsnotify => github.com/fsnotify/fsnotify v1.8.0`. The `require` lines may name a newer version; the `replace` wins. The four pins move together.

## Alternatives considered

**Upgrade to v1.9.0.** Tried in #580, which closed #487. It shipped the regression, because the affected tests did not run on macOS in CI.

**Drop macOS support for the affected code.** Named in #610. It would have meant excluding the Cluster Agent's tests on macOS locally as well as in CI, which #610 judged a worse developer experience than the pin.

## Consequences

The pin is a `replace`, not a `require`, so it also holds back fsnotify for dependencies that pull it in transitively (viper among them). `go get -u` does not move it, and a module that drops its `replace` silently picks up the newer version. Since each module builds without `go.work`, each needs its own `replace`.

The code that hit the regression no longer exists. The Go Cluster Agent was replaced by the Rust one ten days after #610 (`2d005c27`), and the Rust agent uses the `notify` crate, not fsnotify. The only direct user left is `modules/shared/k8shelpers/kube-config-watcher.go`, which watches the kubeconfig files in `desktop` and does run on macOS. Whether it is affected by fsnotify#689 has not been tested; a kubeconfig replaced by a symlink swap would be the case to try.

Upstream fsnotify#689 is still open. v1.10.0 and v1.10.1 (2026) list kqueue fixes for dangling symlinks and a descriptor leak, but none for this regression.

## Revisit when

fsnotify#689 is fixed upstream, or a test shows that `KubeConfigWatcher` on macOS behaves the same under v1.8.0 and the latest release. Either way the four `replace` lines go together.
