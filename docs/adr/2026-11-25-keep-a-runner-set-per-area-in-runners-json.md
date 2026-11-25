---
title: Keep a runner set per area in runners.json
date: 2026-11-25
scope: repo
status: Accepted
---

# Keep a runner set per area in runners.json

## Context

[Define the CI runner matrix in one runners.json file](2026-11-17-define-the-ci-runner-matrix-in-one-runners-json-file.md) made `.github/ci-config/runners.json` a flat list, one entry per OS and arch, which every `ci-*.yml` reads through the Plan action.

Kubetail Desktop brings its own runners from Kstack: macOS 15, 26 and 27 on each arch they have, `windows-2025-vs2026`, and a macOS 27 PR leg on `xcode-27`. Its tests split by macOS release, and its host needs a Visual Studio the default Windows image lacks. Appended to the flat list, these entries would run in every area's matrix, and two entries for one OS and arch would collide in the `runners` lookup.

## Decision

`runners.json` is an object of named sets, each a list of `{os, arch, runner, pull_request}` entries. `default` is the list the other areas ran on; `desktop` is Kstack's. The Plan action takes a `runners` input, `default` unless a workflow names another, and fails on a set the file lacks. It reads `legs`, `linux-arches` and `runners` from that set alone, and adds `pull-request-legs`, the set's PR legs on every event, for a job that runs one runner per OS on `main` too (`Vet · Rust` in `ci-desktop.yml`).

This replaces the flat list of the earlier ADR. The rest of it holds.

## Alternatives considered

**A second file, `runners-desktop.json`.** It leaves the format alone, but splits the one place a runner changes in two, which is what the earlier ADR set out to end.

**Move the desktop to the default runners.** It drops the macOS releases and the Windows image the desktop's platform ADRs test on, a change to what the desktop is tested on, made only to fit a file.

**One flat list, each entry naming its areas.** Every reader then filters, and an OS and arch shared by two areas with different runners still needs two entries.

## Consequences

A runner change for one area is one line in its set. A new area with its own runners is a new set and a `runners:` input in its workflows.

A `<os>-<arch>` key is unique within a set, not across the file. The desktop's macOS entries carry their release in `os` (`macos-26`), so its matrix legs and job names do too.

## Revisit when

The sets converge, or a third area needs runners of its own.
