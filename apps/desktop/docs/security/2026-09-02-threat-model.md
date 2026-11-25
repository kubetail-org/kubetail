# Threat model — 2 September 2026

**Reviewed at** `862d1d1` (main) · **Method** source review; no runtime or network testing, and no
attempt to exploit any finding.

A review record: it says what was believed on the day, and is not edited afterwards. The living
picture is [`docs/security-model.md`](../security-model.md); the next review is a new file here.

Severities weigh impact on cluster credentials and cluster data against how much local privilege an
attacker must already hold. A finding that needs code execution as the user is rated on how much
work the app saves that attacker, not on whether the attacker exists.

## Adversaries considered

Another local user on a shared machine; malware running as the same UID; a hostile or compromised
cluster, which controls every byte its watches return; an attacker who can write a kubeconfig or set
the app's launch environment; and the supply chain across three ecosystems plus a bundled binary.

## Findings

### High

**S-1 · No peer authentication on the IPC socket.** The listener serves whoever connects — GraphQL
and gRPC alike, with no token and no peer check. A process running as the user can read every
mirrored object, call `StartLogin`/`Logout`, and issue destructive mutations without touching
`~/.kube/config`. File mode 0600 and the owner-only pipe DACL are the whole policy; the pid in the
socket name is obscurity, not access control. *Fix:* `SO_PEERCRED`/`getpeereid` on Unix,
`GetNamedPipeClientProcessId` on Windows, asserting the peer is the host.

**S-2 · Kubeconfig `exec` plugins run unprompted, for every context.** `RESTConfig` hands the config
to `clientcmd`, which honours `exec` credential plugins, and the connection probe runs against every
declared context on startup and on every file change. A kubeconfig an attacker can write is code
execution triggered by dropping a file. `kubectl` has the same property but runs one context on
demand; here the fan-out is automatic and covers contexts the user never opened.

**S-3 · The cache is a durable plaintext aggregation of everything the user's RBAC can read.**
Discovery mirrors every kind but three, storing full bodies. Redaction is correct where it applies
and the code says plainly the table is not exhaustive: ConfigMaps, inline container env values,
annotations on non-Secret kinds, and CRDs outside the four entries stay in the clear. The result
answers offline, without RBAC, what previously needed a live authenticated call.

**H-1 · Script execution in the webview equals full cluster access.** `graphql_query` and
`graphql_subscribe` forward the operation unexamined. The CSP is tight and `src/` has no HTML sink,
so the exposure is a dependency compromise rather than a rendering bug — but nothing at the boundary
narrows what an operation may be. `src/gql/` already enumerates every operation the app ships.

### Medium

**H-2 · Endpoints come from inherited environment.** `KUBETAIL_CLOUD_API_URL`, `KUBETAIL_OAUTH_ISSUER`,
`KUBETAIL_OAUTH_CLIENT_ID` and `KUBETAIL_DATA_DIR` are read from the environment the host passes down.
Whoever can set the app's launch environment redirects sign-in to an issuer they control.
`KUBETAIL_KEYCHAIN_SERVICE` already shows the right shape — set by the host, debug-only.

**H-3 · macOS hardened-runtime exceptions.** `disable-library-validation` is there so the separately
built sidecar can be exec'd; it is also what would otherwise stop a substituted binary or an injected
dylib in a signed bundle. Signing the sidecar under the same Team ID removes the need for it.

**H-4 · No update channel is implemented.** `src-tauri/CLAUDE.md` describes signed updates via
`tauri-plugin-updater`, but neither `Cargo.toml` nor `tauri.conf.json` declares the plugin or a
public key. Distribution is direct download, so there is no store-side check to fall back on.

**S-4 · Cache files rely on directory mode alone.** Data directories are 0700 and `atomicjson`
chmods its files 0600, but the SQLite files and their `-wal`/`-shm` siblings take the umask. One
regression in a directory mode exposes everything in S-3.

**S-5 · Cached events are never aged out.** The janitor vacuums and trims delete marks; nothing
bounds the events table. A busy cluster grows the cache without limit.

**S-6 · `chatStream` is declared but unimplemented.** The resolver is `panic("not implemented")` in
the shipped schema — recovered per request, so the cost today is a failed subscription. The design
risk is larger: an assistant is the first egress path for cluster data, and cluster data is
attacker-controlled text.

**X-1 · Three dependency ecosystems, no advisory scanning in CI.** Given H-1 and S-2, dependency
compromise is the most likely route to both high-severity outcomes.

### Low

**H-5 · Socket path in the shared temp directory.** `/tmp/kubetail-sidecar-<pid>-<n>.sock` on Linux;
the sidecar removes whatever it finds there before binding. The bind itself is sound.

**H-6 · The `opener` capability is granted but unused.** No code in `src/` calls it; the tray's
`open_url` is Rust-side and needs no webview permission.

**H-7 · Sidecar log fields are merged into host tracing events verbatim,** so a cluster-controlled
string can shadow or forge a structured field. Nesting them under `sidecar.*` closes it.

**S-7 · Insecure cluster TLS is honoured silently.** Matching `kubectl` is right; not showing it is
the gap. The fingerprint already covers `proxy-url` and the static auth config.

**S-8 · Auth invariants held only by comment.** `ParseIdentityUnverified` decodes claims without
checking the signature and must never become an authorization input.

**S-9 · No query cost limits or operation allowlist.** Local and same-user, so self-inflicted — but
the same control would blunt H-1 and S-1.

**X-2 · Cluster strings reach every UI surface.** No HTML sink today. The planned log-tail and exec
windows turn that data into a code path: no HTML interpretation, and strip terminal control
sequences.

## What held up

The umask-before-bind sequence in `ipc.Listen`, with the test that pins it. The OAuth flow end to
end: PKCE S256, an ephemeral loopback bound before the authorize URL is built, `state` compared
before a code or an `error` is consumed, full JWKS verification, keyring storage, revocation on
sign-out. Redaction keyed off the body's own group and kind, so it cannot be bypassed by how an
object was addressed. The error presenter that logs the operation but never `variables`. The
webview capability set, which grants no shell or filesystem access. Per-request idle-read bounds, so
a hung API server cannot park goroutines.

## Rendered copy

Published as an artifact for reading outside the repo:
<https://claude.ai/code/artifact/9f0a4aec-117b-4d93-9eb5-9e54ba99b551>. This file is the source of
truth.
