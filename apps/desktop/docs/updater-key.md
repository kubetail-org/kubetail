# The updater keys

The desktop app on macOS and Windows installs an update only when it is signed by one of the two
keys the app was built with: the current key or the standby. Why it is shaped this way is the
[ADR](adr/2026-11-08-the-updater-trusts-a-current-and-a-standby-key.md); this page is how to
handle the keys.

## Where they live

| What | Where |
| --- | --- |
| Current public key and its manifest | `plugins.updater.pubkey` and `plugins.updater.endpoints` in `src-tauri/tauri.conf.json` |
| Standby public key and its manifest | `pubkey` and `endpoint` in `src-tauri/updater-standby.json` |
| Current private key and password | the `production` environment's secrets `TAURI_SIGNING_PRIVATE_KEY` and `TAURI_SIGNING_PRIVATE_KEY_PASSWORD` |
| A retired key, kept for apps two rotations behind | `TAURI_SIGNING_PRIVATE_KEY_PREVIOUS` and `TAURI_SIGNING_PRIVATE_KEY_PREVIOUS_PASSWORD` |
| Standby private key and password | offline only, never a secret, kept apart from the current key's offline copy |

A key is named by its id, the hex string in its `.pub` file's comment:

```sh
base64 -d kubetail-updater.key.pub | head -1   # untrusted comment: minisign public key: <id>
```

**Each key has its own manifest**, `https://updates.kubetail.com/desktop/stable/latest-<id>.json`.
The app checks the current key's first, then the standby's, and the standby's is absent until it
is promoted. `sign-updater` signs every updater artifact with each key in the secrets and attaches
each key's manifest to the release; publishing a desktop final copies them to `updates.kubetail.com`. It fails when the current key is not the committed one, when the standby is unset, or
when the standby is a key it signs with. Each endpoint naming its key's id is a test,
`the_updater_pins_its_keys_manifests_and_signed_version`, so a pull request that changes a key
without its endpoint fails CI.

## Set up the keys

Generate both, each with its own password:

```sh
pnpm tauri signer generate -w ~/.tauri/kubetail-updater-<date>-a.key
pnpm tauri signer generate -w ~/.tauri/kubetail-updater-<date>-b.key
gh secret set TAURI_SIGNING_PRIVATE_KEY --env production < ~/.tauri/kubetail-updater-<date>-a.key
gh secret set TAURI_SIGNING_PRIVATE_KEY_PASSWORD --env production
```

Commit key `a`'s public key and id as the current key (`latest-<id>.json` in the endpoint), and
key `b`'s in `updater-standby.json`. Move key `b`'s private half and password offline, to a
different place or maintainer than key `a`'s offline copy, and delete it from the machine. Until
then the keys are placeholders: empty public keys and all-zero ids, which the test accepts and
`sign-updater` refuses, so no release ships without both keys.

## Rotate

The same steps whether the rotation is planned or the current key is lost or compromised.

1. **Generate the new standby**, as above, and keep it offline.
2. **Promote the standby** in a pull request of its own: the standby's public key and endpoint
   become `plugins.updater`'s, and the new standby's go in `updater-standby.json`. A changed key
   changes who can install code on every app, so review it as a security change.
3. **Move the secrets**, just before merging: the promoted key and its password become
   `TAURI_SIGNING_PRIVATE_KEY` and its password. A release cut while the secrets and the commit
   disagree fails at `sign-updater`; nothing ships.
4. **Decide the old key's fate.**
   - **Planned**: set it as `TAURI_SIGNING_PRIVATE_KEY_PREVIOUS` (and its password) if apps built
     before the rotation before this one may still be installed: they trust the old key and not
     the promoted one. Otherwise destroy it and its offline copy.
   - **Lost**: nothing to do. Apps two rotations behind trust no key still in service and must be
     reinstalled by hand: say so in the release notes, pointing to the release page and
     `SHA256SUMS.asc`.
   - **Compromised**: never sign with it again. First check the manifests on `updates.kubetail.com`
     against the releases' copies, and every desktop release's files for an unknown artifact:
     installing a forged update also takes serving its manifest, which means writing to
     `updates.kubetail.com`.
5. **Release as usual.** Installed apps find no update on the old key's manifest, read the promoted
   key's, and update to a build that trusts it and the new standby.
6. **Retire the previous key** when you choose: delete the two `_PREVIOUS` secrets and destroy the
   key. Apps that still trust only it find no manifest and stay on their version until reinstalled.
7. **Record the rotation** in a dated note under `docs/security/`, naming the key ids and the first
   release built with them. After a compromise, update the [security model](security-model.md)'s
   updater row if the boundary changed.

## Replace a lost standby

Generate a new standby and commit it in `updater-standby.json`. Apps built from then on trust it;
installed apps keep the lost one as their standby until they update. Until most have, losing the
current key too would strand them, so release soon after.
