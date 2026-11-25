---
title: The cluster cache is ordinary application data, not credential-bearing storage
date: 2026-09-02
scope: sidecar
status: Accepted
---

# The cluster cache is ordinary application data, not credential-bearing storage

## Context

Each cluster gets one SQLite file holding full object bodies for every mirrored kind. Secret values
are blanked at write time (`kubestore/objects.go`), keyed off the body's own group and kind so the
redaction cannot be dodged by how the object was addressed — but that table is deliberately narrow.
ConfigMaps, inline container env values, annotations on non-Secret kinds and CRDs outside the listed
entries are stored in the clear.

The file is `0600`, sidecars included, in a `0700` directory. That protects against another account
on the same machine and nothing else; a lost disk is protected by full-disk encryption or not at
all. The question this ADR answers is whether that is the right level — whether the cache is
ordinary application data or credential-bearing storage that needs encryption at rest with a key in
the OS keyring.

## Decision

**The cache is ordinary application data.** File permissions plus the operating system's disk
encryption are the protection it gets. We do not encrypt stored bodies, and `rawcodec.go` stays a
compression chokepoint rather than becoming a cryptographic one.

An attacker who reads the file gets, offline and with no RBAC check, the contents of every object
the user's credentials could list: names, namespaces, labels, annotations, container images and
env, ConfigMap data, event messages. What they do **not** get is a credential — no kubeconfig, no
bearer token, no refresh token; the refresh token lives in the OS keyring
(`internal/services/auth/keyring.go`) and the cluster credentials live in the user's kubeconfig.

That is the crux. To read the cache an attacker must already be the local user or hold the disk,
and the local user can read the kubeconfig sitting beside it and simply ask the cluster — a live,
current, unredacted answer, strictly better than a stale mirror. Encrypting the bodies would not
change that, because the key would have to be available to the same process the same attacker is
already running as. It buys one case only: the stolen disk, which is what FileVault and BitLocker
are for and which are on by default on current Apple and Windows hardware.

Nor would encryption make the file opaque. The columns the store indexes and sorts on — names,
namespaces, labels, status summaries, event reasons and messages — must stay readable to be
queryable, and the events FTS index holds message text in the clear by construction. Encrypting
bodies would narrow what the file leaks while leaving its shape, its scale and its most legible
text intact, and would buy that at the cost of a key-loss path and a keyring dependency on the read
path of every object.

**One argument survives all of that: the cache outlives the credentials.** A token expires, a
certificate is revoked, an employee is offboarded — the file still answers. That is the single way
the cache is genuinely not "the same as the kubeconfig beside it", and the answer to it is
retention, not encryption: a cache whose cluster the user has stopped opening should stop existing.
That work is tracked in [`TODO.md`](../TODO.md#security).

## Alternatives considered

**Encrypt stored bodies, key in the OS keyring (answer B).** Rejected on the ratio above: the key
is reachable by anything running as the user, so the protection collapses to the stolen-disk case
that full-disk encryption already covers, while the indexes and the FTS table keep leaking the most
readable text in the file. The costs are real and permanent — a keyring round trip in the object
read path, a rebuild-from-cluster path for a lost key, and the loss of plaintext event search or a
carve-out that undoes half the benefit.

**Widen the redaction table until nothing sensitive is stored.** Rejected: it cannot terminate. A
CRD invented tomorrow can hold a credential in a field nobody has named, and a redaction list that
must be exhaustive to be correct is a list that is silently wrong. The narrow table stays what it
is — a correct blanking of the fields we can name, not a completeness claim.

**Do not cache bodies at all.** Rejected: mirroring the cluster is the product. Without it every
view is a live authenticated call, which is the latency and the API-server load the cache exists to
remove.

## Consequences

The cache is protected exactly as the kubeconfig is, and the security model says so rather than
listing a gap. `rawcodec.go` stays simple and the read path stays keyring-free.

The obligations this creates are the ones that must not be broken without noticing: the file and
its `-wal`/`-shm` sidecars stay `0600` in a `0700` directory (`TestCacheFileIsOwnerOnly`,
`TestOpenWriterFilesAreOwnerOnly`), Secret values stay redacted at write time
(`TestProjectRedactsSecretValues`, `TestProjectRedactsOnlyCoreSecrets`), and removing a cache
deletes its files rather than orphaning them (`TestRemoveDeletesTheFilesAndDropsTheOpenStore`). On
Windows the mode is the inherited profile ACL, [by decision](2026-09-02-windows-cache-files-rely-on-the-profile-acl.md).

Retention is now owed. Until it lands, a cache outlives the user's interest in the cluster it
mirrors, and this decision is only sound with that qualification stated.

## Revisit when

The app stops being single-user-desktop — a shared or multi-tenant install, or a cache written
somewhere the owning user is not the only reader — or when a cache begins holding something the
user's own credentials could not have fetched.
