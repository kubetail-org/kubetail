---
title: Test each component on the platforms it ships for, or record why not
date: 2026-11-10
scope: repo
status: Accepted
amended_by: 2026-11-10-run-the-riscv64-jobs-weekly.md
---

# Test each component on the platforms it ships for, or record why not

## Context

Kubetail ships these artifacts:

- **The CLI binary**, with the UI embedded. `release-cli.yml` builds it for darwin, linux and windows on amd64 and arm64. Most `publish-*.yml` workflows repackage those binaries. Alpine, Launchpad, COPR, OBS, Gentoo GURU and AUR's `kubetail-cli` build it from the vendored source tarball instead. Alpine (`arch="all"`) and Launchpad's newer series (`any`) also build it for x86, armhf, armv7, loongarch64, ppc64le, riscv64 and s390x.
- **Four images**: cli, dashboard, cluster-api and cluster-agent. `release-docker.yml` builds each for linux/amd64 and linux/arm64, in a `final` and a `final-alpine` target.
- **The UI**, a JavaScript bundle embedded in the CLI and the dashboard image.

Until #1291, no list said which of these CI checks, on which platform. `ci.yml`, `coverage.yml` and `stress.yml` each covered part, and some platforms had no check at all. #1278 settled which checks run on a PR and which on `main` only ([its ADR](2026-11-10-run-each-os-once-on-a-pr-and-the-other-platforms-on-main.md)). This ADR settles whether every shipped platform is checked somewhere.

The hand-written Go code has no `_windows.go`, `_darwin.go` or per-arch files. It has two `runtime.GOOS` branches, neither a code path of its own, and no `unsafe` or `encoding/binary`. So a bug specific to a platform comes from the toolchain, a dependency, or a property of the platform the code assumes without saying: word size, or the OS around the process.

## Decision

The matrix below is what CI checks. **PR** is every pull request that touches the component's paths. **main** is every push to `main`. **nightly** is `stress.yml`. **weekly** is `riscv64.yml`. **—** means the check does not exist for that platform. **none** is a gap accepted for the reason listed after the table.

**CLI binary**

| Platform | Build | Unit tests | Race tests | E2E |
| --- | --- | --- | --- | --- |
| linux/amd64 | PR | PR | PR | PR |
| linux/arm64 | main | main | main | main |
| darwin/amd64 | main | main | main | none |
| darwin/arm64 | main | PR | main | none |
| windows/amd64 | main | PR | main | none |
| windows/arm64 | main | main | — | none |
| linux/386, arm v6, arm v7 | main | main, as 386 | — | none |
| linux/riscv64 | main | weekly | — | none |
| linux/loong64, ppc64le, s390x | main | none | none | none |

`Go · Build and Test` runs the first six rows' build, unit and race checks on `main`. On a PR it runs linux/amd64, and `coverage.yml` runs the unit tests on macOS 15 arm64 and Windows 2025. `Go · Vet` type-checks for windows on a PR too. `stress.yml` repeats the unit and race tests nightly on the first six rows. `Alpine · Build` cross-compiles the last three rows and the 32-bit row, then runs `kubetail serve --test` under QEMU. `Go · Test (linux, 386)` runs the unit tests as 386. E2E means `e2e/test_cli.py`: `kubetail serve` and `kubetail logs` against a kind cluster.

**Images**

| Image | Build and start, amd64 | Build and start, arm64 | Run in a cluster |
| --- | --- | --- | --- |
| dashboard, cluster-api, cluster-agent: `final` | PR | main | amd64 on PR, arm64 on main |
| dashboard, cluster-api, cluster-agent: `final-alpine` | PR if a Dockerfile changed, else main | main | — |
| cli: `final` | PR | main | — |
| cli: `final-alpine` | PR if a Dockerfile changed, else main | main | — |

`Docker · Build` starts each image it builds once, with `--version` for the cli and `--help` for the others, which exit after start-up. That checks the binary and, for the agent, the glibc libraries each final stage copies onto its base. The cli image is not a server, and a `final-alpine` image runs the same binary as its `final` image, so neither runs in a cluster. `riscv64.yml` also builds and starts the cluster-api image for riscv64 weekly, a platform no release ships. The Go servers' unit and race tests run as their linux rows above. `Rust · Build and Test` builds and tests the cluster agent on linux/amd64 and linux/arm64 on every PR. E2E runs on Kubernetes 1.21 and the latest on amd64, and on the latest on arm64.

**UI**: `TypeScript · Test` and `TypeScript · Build` run on `ubuntu-24.04` on every PR. `Docker · Build` builds the UI again inside the cli and dashboard images, on amd64 on a PR and on arm64 on `main`.

**Accepted gaps, and why:**

- **Race tests on windows/arm64, 386, arm and riscv64.** Go's race detector does not support those platforms.
- **The UI on other OSes.** The bundle is the same file on every OS, and the build that makes it runs in Node, which hides the OS. The axis that varies for users is the browser, and the unit tests run in jsdom. Browser coverage is a separate question from platform coverage.
- **E2E of `kubetail serve` and `kubetail logs` on macOS and Windows.** E2E needs a cluster the runner can reach. GitHub's macOS arm64 runners have no nested virtualization to run Docker, and its Windows runners cannot run Linux containers. The desktop path has no code of its own per OS, and its Go tests run on every OS.
- **Unit tests on loong64, ppc64le and s390x.** No GitHub runner runs them natively, and the full suite under QEMU would run for a long time on every push. All three are 64-bit, like amd64. s390x is big-endian, but the hand-written code never reads bytes as integers. The binary builds and starts on each, on `main`.
- **Unit tests on 32-bit arm.** `Go · Test (linux, 386)` stands in for it. The property 32-bit code can break is word size: an `int` that overflows, a 64-bit atomic that is not aligned. 386 shares that property and runs natively on an amd64 runner.
- **Release-only build settings**: `GOWORK=off`, `-mod=vendor` and `CGO_ENABLED=0` as `release-cli.yml` and the source packages use them. The release workflows are #1282's audit.

`package-healthcheck.yml` installs the published package from each channel nightly and runs `kubetail --version`. It checks the channel after a release, not the code before one, so it is no cell of the matrix.

## Alternatives considered

**E2E on arm64 on every PR.** It runs the same tests on images built from the same Dockerfiles. Under #1278's rule a leg that rarely fails alone runs on `main`.

**E2E on arm64 against Kubernetes 1.21 too.** The oldest-version leg checks the API the components call, which is the same on every arch. One arm64 leg on the latest checks the images.

**The Go tests under QEMU for every Alpine architecture.** The riscv64 leg runs natively and takes most of its timeout. Under QEMU, six legs would each run several times longer, on every push to `main`, to check two properties that 386 and the build already check.

**Run the desktop E2E on macOS against a cluster on a Linux job.** It would need a network path between two runners and credentials for the cluster on the second. That is a new kind of job for one `runtime.GOOS` branch.

## Consequences

Every shipped platform now has a build that runs on `main`, and every 64-bit platform that GitHub runs natively runs the unit tests. The arm64 images run in a cluster before a release, on `main`, instead of first in a user's cluster.

A change that adds platform-specific code (a `_windows.go` file, a `runtime.GOARCH` branch, `unsafe`, `encoding/binary`) moves a gap above out of "accepted". So does a new shipped platform or image target. Either updates this matrix.

`e2e/docker-bake.hcl` reads the `Docker · Build` cache of the arch in `CACHE_ARCH`, so `Docker · Build` on that arch must run before E2E for the cache to help.

## Revisit when

A bug specific to a platform reaches a release that a cell marked "none" would have caught, or the code gains platform-specific files or branches.
