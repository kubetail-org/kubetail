# src-tauri/src/updater — the in-app updater

macOS and Windows update in the app; Linux does not. Also owns `scripts/sign-updater.sh` and `release-desktop.yml`'s `sign-updater` job. → [ADR: the updater trusts a current and a standby key](../../../docs/adr/2026-11-08-the-updater-trusts-a-current-and-a-standby-key.md).

`mod.rs` compiles and is tested on every platform. `platform.rs` wires `tauri-plugin-updater`, a macOS/Windows-only dependency, and is in `.github/codecov.yml`'s `ignore`.

## The check

- **A release build checks each trusted key's manifest**, `https://updates.kubetail.com/desktop/stable/latest-<key id>.json`, at launch and every 12 hours by the wall clock, read on an hourly tick, since Tokio's timers stop while the machine sleeps. The current key's (`plugins.updater` in `tauri.conf.json`) goes first, then the standby's (`updater-standby.json`; absent until promoted, which counts as no update). The first update found wins (`check_each`).
- **Each check's update is verified against that check's key alone** and, with `requireSignedVersion`, only a signature made for the manifest's version. `the_updater_pins_its_keys_manifests_and_signed_version` holds each endpoint to its key's id.
- A check that takes over `CHECK_TIMEOUT` fails like any other.
- **The check and the download send the sidecar's User-Agent**, `kubetail-desktop/<version> (<GOOS>/<GOARCH>)`.

## The install

- **An update found becomes the tray's first item and follows each later check** (`Offer::checked`): a newer release replaces it, no update withdraws it, a failed check leaves it. Its click downloads, verifies, installs and restarts. A failed download, or one that receives nothing for `DOWNLOAD_STALL` (the HTTP client's read timeout), puts the offer back.
- **The install stops the sidecar before it requests the restart.** Tauri lets nothing prevent a restart's `ExitRequested`, so `install` runs `lib.rs::stop_for_install`, the quit's stop on its own thread, and only then `request_restart`. A quit already under way is left to finish, and the new version runs on the next launch.
- **On Windows the plugin ends the process itself** after starting `msiexec`, with no `ExitRequested` or `RunEvent::Exit`. Its `on_before_exit` runs `stop_for_install`, then `cleanup_before_exit` and `exited`, whose kill ends a sidecar that outlived the grace period before `msiexec` starts.

## The keys

- **The updater key is read only by `release-desktop.yml`'s `sign-updater` job**, which installs nothing else. It signs the macOS `.app.tar.gz` and the MSIs SignPath signed, and attaches one `latest-<key id>.json` per key in the secrets: the current key, which must be the committed `pubkey` and the endpoint's id, and a retired one kept for older apps.
- **The standby's private key is offline** and signs nothing until a rotation promotes it; the script refuses an unset standby or one it signs with ([`docs/updater-key.md`](../../../docs/updater-key.md)).

## Publishing

- **Publishing a final copies its manifests to `updates.kubetail.com`** (`publish-desktop-updates.yml`). It refuses a manifest `release-desktop.yml` did not attest at the tag, and replaces a newer live one only when dispatched with `force`.
- **The bucket, the distribution and the AWS role are made by hand**; the role trusts only the `updates` environment, which holds no secrets.
