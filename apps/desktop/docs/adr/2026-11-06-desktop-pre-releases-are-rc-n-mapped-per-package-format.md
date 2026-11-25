---
title: Desktop pre-releases are rc.N, mapped to a version per package format
date: 2026-11-06
scope: repo
status: Accepted
---

# Desktop pre-releases are rc.N, mapped to a version per package format

## Context

`release-desktop.yml` accepted any SemVer pre-release and passed it to `tauri build` unchanged.
`desktop/v0.0.1-rc.1` passed the check and failed ten minutes later in two bundlers (#783):

- **RPM** forbids `-` in a version.
- **MSI** versions are `major.minor.build.revision`, all numeric, with major and minor at most 255
  and the others at most 65535. Tauri maps a numeric pre-release to the fourth field and rejects
  any other.

Tauri's `version` must be SemVer, so neither format's native pre-release form (`0.0.1~rc.1` for
RPM) can go through it. Tauri offers `bundle.windows.wix.version` to set the MSI version apart
from the app's, and `bundle.linux.rpm.release` for the RPM release. `tauri bundle` packages a
binary `tauri build` already compiled.

## Decision

A desktop version is `<x>.<y>.<z>` or `<x>.<y>.<z>-rc.<n>`. The `config` job runs
`scripts/check-release-version.sh`, which rejects anything else and derives:

| Format | Final `<x>.<y>.<z>` | Pre-release `<x>.<y>.<z>-rc.<n>` |
| ------ | ------------------- | -------------------------------- |
| RPM    | Version `<x>.<y>.<z>`, Release `1` | Version `<x>.<y>.<z>`, Release `0.rc.<n>` |
| MSI    | `<x>.<y>.<z>`       | `<x>.<y>.<z>.<n>`                |

The Linux job builds the deb and AppImage with the SemVer, then bundles the RPM from the same
binary with `tauri bundle -c` under the RPM version and release. The Windows job sets
`bundle.windows.wix.version`. The app, the sidecar, the tag, the release, the SBOM and every asset
name but the RPM's keep the SemVer. The RPM is named by its own version and release
(`Kubetail-0.0.1-0.rc.1.x86_64.rpm`).

## Alternatives considered

- **RPM Version `<x>.<y>.<z>~rc.<n>`**, RPM's own pre-release form: Tauri rejects a non-SemVer
  `version`, and there is no RPM-only version setting.
- **Any alphanumeric pre-release, with a mapping per label** (`alpha`, `beta`, `rc` in ranges of
  the MSI field): more forms to check and document for no current use. `release-version`
  recommends `rc.<n>`.
- **Checking MSI's field limits in the `config` job**: no release is near 255 minors or 65535
  candidates, and Tauri's bundler rejects one that is.
- **MSI pre-release as the app version `<x>.<y>.<z>-<n>`**: changes the version the app reports
  and the MSI's file name. `wix.version` keeps both on the SemVer.

## Consequences

- An unmappable tag fails in `Configure workflow`, naming the allowed forms.
- A final release builds the RPM as before: Release `1`, same file name.
- RPM sorts each rc before its final (`0.rc.10` < `1`) and rcs by number.
- Windows Installer ignores the fourth field when comparing versions for an upgrade, so it treats
  `<x>.<y>.<z>.<n>` and `<x>.<y>.<z>` as one version.
- The deb still carries the SemVer, which dpkg reads as upstream `<x>.<y>.<z>`, revision `rc.<n>`.
  It sorts an rc after its final.

## Revisit when

Tauri accepts a version per Linux package format, or Kubetail needs pre-release labels besides `rc`.
