---
title: CI checks every platform split the code has on a pull request
date: 2026-11-10
scope: repo
status: Accepted
amended_by:
  - [Main builds and tests the desktop host's release profile on the release runners](2026-11-10-main-builds-and-tests-the-release-profile.md)
---

# CI checks every platform split the code has on a pull request

## Context

`ci.yml` grew job by job. #834 audited it, with `coverage.yml`, `stress.yml` and the two release
workflows, against what the code targets, right after [a pull request runs one leg per
OS](2026-11-10-a-pull-request-runs-one-leg-per-os.md) cut a PR to three test legs. This is the
record of that audit, as of `3cc8b6bb`.

### Jobs

A PR runs a language's jobs when its filter in `.github/ci-config/path-filters.yml` matches; a
push to `main` or a dispatch runs them all. *PR legs* are `ubuntu-24.04`, `xcode-27` (macOS 27,
arm64) and `windows-2025-vs2026`; *every leg* is the nine runners in `runners.json`.

| Workflow · job | Runs | Runners | Events | Filter |
| --- | --- | --- | --- | --- |
| ci · Go · Lint | gofmt, coverage markers | ubuntu-24.04 | PR, main, dispatch | go |
| ci · Go · Vet | `go vet` per GOOS on sidecar, CLI, tools; sidecar deps; `go fix` | ubuntu-24.04 | PR, main, dispatch | go |
| ci · Go · Staticcheck | `scripts/staticcheck.sh` | ubuntu-24.04 | PR, main, dispatch | go |
| ci · Go · Build and Test | sidecar build, `-race -short` tests; CLI `-race`; tools (not Windows) | PR legs; every leg on main | PR, main, dispatch | go |
| ci · Sandbox · Check AppArmor profiles | bwrap, pasta, `.deb` install, `check-sandbox-chain.sh` | ubuntu-24.04, ubuntu-24.04-arm | PR, main, dispatch | go |
| ci · Rust · Lint | `cargo fmt`, coverage markers | ubuntu-24.04 | PR, main, dispatch | rust |
| ci · Rust · Vet | `cargo clippy` | PR legs, every event | PR, main, dispatch | rust |
| ci · Rust · Build and Test | `cargo build`, `cargo test` | PR legs; every leg on main | PR, main, dispatch | rust |
| ci · TypeScript · Lint, Test, Build | oxlint, oxfmt, Tailwind; vitest (jsdom); `pnpm build` | ubuntu-24.04 | PR, main, dispatch | typescript |
| ci · Shell, YAML, Markdown · Lint | shellcheck; yamllint, actionlint, zizmor; lychee | ubuntu-24.04 | PR (Markdown always), main, dispatch | shell, yaml, — |
| coverage · Go | sidecar untagged and `-tags debug`, `CGO_ENABLED=0`; CLI | PR legs | PR, main, `wip/**`, dispatch | go |
| coverage · Rust | `cargo llvm-cov` | PR legs | PR, main, `wip/**`, dispatch | rust |
| coverage · TypeScript | vitest with coverage | ubuntu-24.04 | PR, main, `wip/**`, dispatch | typescript |
| stress · Go · Stress | sidecar, `-shuffle=on -count=3`, `-cpu` 1 and 4, race and not | every leg | nightly, dispatch | — |

### Suites by platform

| Suite | Ships on | A PR tests | `main` adds |
| --- | --- | --- | --- |
| Sidecar | darwin, linux, windows × amd64, arm64; `CGO_ENABLED=0` | one leg per OS, with `-race` (cgo) and without cgo | the other six legs; nightly stress on all nine |
| CLI | the same six, cross-compiled with `CGO_ENABLED=0` | one leg per OS, with `-race` (cgo) only | the other six legs |
| `tools/` | CI's Linux runners, maintainers' Macs | Linux, macOS; vetted for Windows | Linux arm64, four macOS legs |
| Rust host | Linux amd64/arm64 (built on Ubuntu 22.04), macOS universal, Windows amd64/arm64; release profile | one leg per OS, dev profile | the other six legs, dev profile |
| TypeScript | one bundle in WebKit, WebKitGTK and WebView2 | jsdom on Linux; `isMacOS`/`isLinux` branches by a stubbed user agent | — |
| AppArmor chains | Linux `.deb` on Ubuntu 24.04, amd64 and arm64 | both architectures | — |

### Platform-specific code

- **Go, by OS.** The sidecar has 21 `windows` and 83 `!windows` build constraints and its
  `_darwin`, `_linux`, `_unix` and `_windows` files; the CLI has two `_unix_test.go` files; `tools`
  has `releasenotes/terminal_{unix,windows}.go`. Go · Vet and Staticcheck compile each GOOS, and
  each OS's PR leg tests its own files.
- **Go, by architecture: one split.** `run/sandbox/seccomp_linux_{amd64,arm64}.go` set the
  filter's audit architecture and x32 rule. The arm64 file was never vetted, staticchecked or
  checked by `go fix`, and its tests ran only on `main`. The earlier ADR says the sidecar has no
  file split by architecture; this is the one it missed. Neither file holds a statement, so
  `coverage.yml`'s three legs still measure every line.
- **Go, by tag.** `debug` was tested by coverage on every PR leg but never vetted or
  staticchecked. `livecache` and `liveswitch`, the catalog's checks against the real providers,
  were vetted by `make vet-go` but not by CI's Go · Vet.
- **Rust.** `cfg` on `target_os`, `target_vendor`, `unix` and `windows`; no `target_arch`. Clippy ran
  on each OS's PR leg but without `--all-targets`, which `make vet-rust` passes, so no `cfg(test)`
  code was linted; with it, the Windows build fails on a test helper only Unix tests call.
  `cfg!(debug_assertions)` changes `is_app_origin`, the data and runtime directories' names, the
  keychain service, where the sidecar's log goes and the Windows subsystem, and CI never builds
  the release profile. The `debug-prod` feature is never built.

### Path filters

`typescript` missed three inputs: `src-icons/**`, which `index.test.ts` compares with
`src/assets/icons/`; `.oxlintrc.tailwind.json`, which `pnpm lint` formats and `pnpm lint:tailwind`
reads; and `graphql.config.ts`, which `pnpm lint` formats. The rest hold: `proto/**` is in `go`
and `rust`; each `go.mod` sits under its module's path; `scripts/**` is in `go` and its `*.sh` in
`shell`; `setup-environment` is shared and `stub-sidecar` is in `rust`; `rust-toolchain.toml` is
under `src-tauri/`; `.cargo/audit.toml` is `audit.yml`'s; `licenses/` is read only at bundling.

### Release builds

`release-cli.yml` cross-compiles the CLI with `CGO_ENABLED=0`, but CI tests it only with cgo
(`-race`), so the pure-Go resolver its downloads use was never tested. `release-desktop.yml`
builds each sidecar natively with `CGO_ENABLED=0`, which coverage tests on every PR leg and
`main` builds on every runner. It builds the macOS app's x86_64 half on arm64, which `main` tests
natively on the Intel legs, and the Linux app on Ubuntu 22.04, which CI never builds on.

## Decision

Every way the code splits, by OS, architecture or build tag, is compiled and vetted on a pull
request, and tested there unless it is listed under *Accepted as they are*. Tests of the other
architectures and macOS releases stay on `main`. This replaces the earlier ADR's finding that
every split is by OS; the rest of it stands.

- **Go · Vet** vets the sidecar on `linux/amd64`, `linux/arm64`, darwin and windows, and under the
  `debug`, `livecache` and `liveswitch` tags. `scripts/staticcheck.sh` and
  `scripts/check-go-fix.sh` run the same set; `make vet-go` matches CI.
- **Sandbox · Check AppArmor profiles**, already on both Linux architectures on every PR, runs
  `go test -run '^TestThe(Filter|Shell)' ./internal/run/sandbox` first: the tests that read the
  filter's verdicts and the ones that install it and ask the kernel.
- **Rust · Vet** runs `cargo clippy --all --all-targets`. `SidecarService::running_as` becomes
  `cfg(all(test, unix))`, as its one caller is.
- **coverage · Go** measures the CLI with `CGO_ENABLED=0`, as it ships, and so does
  `make cover-changed`.
- **The `typescript` filter** adds `src-icons/**`, `.oxlintrc*.json` and every root `*.ts`.

Filed: #844, building and testing the release profile in CI, and the Linux app on Ubuntu 22.04.

Accepted as they are:

- **TypeScript runs in jsdom on Linux alone.** Its platform branches read the user agent, which
  tests stub per OS. No test drives a real WebView; that is end-to-end testing, not a CI leg.
- **`tools/` is not tested on Windows** and has no coverage report. It runs in CI and on
  maintainers' Macs and ships to no user; Go · Vet still compiles it for Windows.
- **The CLI is not in the nightly stress run.** It holds little concurrency.
- **windows/arm64 runs without `-race`.** Go has no race detector there.
- **`debug-prod` is never built.** It is a local inspection build and never ships.
- **The `livecache` and `liveswitch` checks are vetted, never run.** They call the model
  providers with real keys, which CI does not hold.
- **Both Intel macOS runners stay on `main`.** Users install on both releases, and `stress.yml`
  restores their caches; nothing in the code needs them on a PR.
- **coverage.yml repeats ci.yml's Go and Rust tests on the PR legs.** The Go runs differ (`-race`
  with cgo; without cgo, with `-tags debug`), and ci.yml's Rust build is the uninstrumented
  binary. Merging them would save one job per OS and language and tie the race run to Codecov.

## Alternatives considered

- **Make linux/arm64 a PR leg of Go · Build and Test**, as the earlier ADR's *Revisit when*
  asks for a `GOARCH` split. The split is one file and its filter tests; the AppArmor job already
  runs on arm64 on every PR, builds the sidecar there and installs the filter through the
  sandbox's chain, so the filter tests cost seconds there against a whole suite on a new leg.
- **Vet linux/arm64 by cross-compiling only, without running its tests.** Vet sees types, not
  the filter's verdicts on arm64's syscall numbers.
- **Drop `macos-15-intel`.** It differs from `macos-15` only in architecture and from
  `macos-26-intel` only in release, and nothing branches on either; but it is the one place a
  macOS 15 Intel user's platform is tested before a release.

## Consequences

- A new `GOARCH` file needs its pass in Go · Vet, `make vet-go`, `staticcheck.sh` and
  `check-go-fix.sh`, and its tests on a PR leg of that architecture. One that holds a statement
  needs a coverage leg too.
- A new build tag needs its pass in the same four places.
- The AppArmor job picks the filter's tests by name: one not named `TestTheFilter…` or
  `TestTheShell…` runs on arm64 only on `main`.
- Rust test code is linted per OS: a helper only one OS's tests call carries that OS's `cfg`.
- A new root-level input to a language's jobs needs its filter line.

## Revisit when

- The Rust host or the CLI gains a `target_arch` or `GOARCH` split.
- GitHub retires an Intel macOS image.
- #844 lands: the release profile joins the table above.
