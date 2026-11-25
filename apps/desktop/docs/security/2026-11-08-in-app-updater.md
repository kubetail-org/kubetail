# Security record — The in-app updater, 8 November 2026

**Subject:** the desktop app updates itself on macOS and Windows through `tauri-plugin-updater`
(`src-tauri/src/updater/`), from the `latest-<key id>.json` each desktop final publishes to
`updates.kubetail.com` for each key in service, verified against the current key or the offline standby the app was built with,
signed in `release-desktop.yml`'s `sign-updater` job (`scripts/sign-updater.sh`). The decision is
[the updater trusts a current and a standby key](../adr/2026-11-08-the-updater-trusts-a-current-and-a-standby-key.md);
the living model is [security-model.md](../security-model.md). This is H-4b in
[TODO](../TODO.md#security).

## What widens

**The app installs code it downloads.** Whoever holds the updater key and can serve the manifest
runs code on every installed copy, as the user on macOS and, through the MSI's elevation, as an
administrator on Windows.

**New secrets.** `TAURI_SIGNING_PRIVATE_KEY` and its password, and a retired key's when one is kept,
join the `production` environment. **A second key can sign updates**: the standby, held offline.

**The host makes a request on its own**, at launch and once a day: a GET of each key's manifest from
`updates.kubetail.com` carrying the plugin's user agent and no identity.

**A new host serves what the app trusts to offer.** Whoever can write to `updates.kubetail.com` can
withhold an update or leave a stale one.

## What narrows

**A user is no longer stuck on a version with a known flaw**, on macOS and Windows.

## What holds it

- **The key never shares a runner with installed dependencies.** Only `sign-updater` reads it; that
  job installs nothing but `cargo-tauri`, fetched by version and checked against its SHA-256. Held
  by review.
- **The app trusts two compiled keys, one per check.** The current key, `plugins.updater.pubkey`,
  and the standby, `updater-standby.json`, each with its own manifest; an update is verified
  against the key whose manifest named it. The standby's private key is offline, so a rotation
  promotes it and a lost or stolen current key strands no app.
  `the_updater_pins_its_keys_manifests_and_signed_version` holds each endpoint to its key's id, and
  `scripts/sign-updater.sh` fails the release when the current key is not the committed one or the
  standby is unset or a key it signs with ([`updater-key.md`](../updater-key.md)).
- **A signature names its version.** Each is made with `--app-version`, and `requireSignedVersion`
  refuses an artifact whose signed version is not the manifest's, so an older release cannot be
  replayed under a newer number. The plugin offers only a version greater than the running one.
- **The bytes are verified before anything is written** (the plugin's `download`), and the archive
  signed is the one installed: the notarized `.app`, and the MSI as SignPath signed it.
- **The webview is granted nothing from the updater.** No capability names an `updater:` permission;
  `default_capability_grants_what_the_app_window_calls` and
  `logs_capability_grants_the_viewer_and_nothing_else` pin both lists.
- **The offer's text is a parsed SemVer**, so the tray item carries no control characters.
- **Only one sidecar migrates the stores.** `install` runs the quit's stop, `stop_for_install`,
  before it requests the restart, and on Windows the plugin's `on_before_exit` runs it before
  `msiexec` (`an_install_stops_the_sidecar_before_it_returns`,
  `an_install_during_a_quit_leaves_the_stop_to_the_quit`).
- **Debug builds and Linux check nothing.**

## What it does not hold

- **The manifest is unsigned.** It can withhold an update, name a different signed release of the
  same version, or send the app to download any URL, whose bytes the plugin holds in memory until
  it has verified them; it cannot install unsigned bytes or a downgrade.
- **The `production` environment's protection rules are the key's real boundary**, and they live in
  the repository's settings.
- **Nothing publishes to `updates.kubetail.com` yet.** The host and the workflow that copies a
  published final's manifests are [TODO](../TODO.md#security); until then no app sees an update.
- **The standby is only as safe as its offline copy.** Nothing in the repository checks where it
  is kept.
- **No test exercises a real update.** The check below is manual.

## Manual check

**Not yet run: it needs the real key.** To be recorded here, dated, when it is:

1. Build and install an older version (`desktop/v<old>`) whose `plugins.updater.endpoints` names a
   test release's manifest, for example
   `https://github.com/kubetail-org/kubetail/releases/download/desktop/v<new>-rc.<n>/latest-<key id>.json`.
2. Publish `desktop/v<new>-rc.<n>` through `release-desktop.yml`. On macOS and on Windows, confirm
   the tray offers the update, that it installs, that the app restarts on `<new>`, and that
   `sidecar.log` shows the old sidecar's stop before the new one's start.
3. Replace that release's manifest with one whose signatures come from a different key
   (`cargo-tauri signer generate`, then `scripts/sign-updater.sh` with the
   committed public key swapped for the new one in a scratch checkout). Confirm the download is
   refused, `main.log` says so, the app keeps running on `<old>`, and the tray offers the update
   again.
