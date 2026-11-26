# Security review — 4 September 2026

**Baseline:** `44596a6f0e5d7c75867d7dc0253df08b05f51d07`. Findings describe that commit;
items marked **mitigated in this change** describe the accompanying working-tree changes.
The 2 September review and accepted ADRs remain historical records. This review supplies the
current disposition of every previous finding and records additional findings. The living model
is [security-model.md](../security-model.md); pending work is in [TODO](../TODO.md#security).

## Scope and confidence

Source review covered the React rendering and GraphQL bridge, Tauri commands/capabilities/CSP,
sidecar spawning and all three peer-credential implementations, GraphQL/gRPC transport, kubeconfig
loading and automatic probes, cache projection/storage/retirement/size limits, OAuth/keyring/cloud
sync, and build/release workflows. Dependency review covered the three committed lockfiles.
Adversaries include hostile cluster responses, modified kubeconfigs, malicious bundled JavaScript,
local processes (same and different account), compromised cloud endpoints, and build dependencies.

Severity describes impact **with the stated prerequisite**. A local-account compromise is not
silently treated as an unauthenticated remote exploit. Dependency presence is distinguished from
reachable vulnerable code. An open item is not accepted merely by recording it here.

This is a source and automated-test assessment, not proof that all vulnerabilities have been found.
No real cluster, user kubeconfig, keychain, production cloud account, signing secret, or installed
release was exercised. No macOS/Windows runtime exploit, binary-signature verification, native
webview penetration test, fuzz campaign, or Git-history secret scan was performed. Server-side
OAuth configuration, GitHub required checks/environment protections, and OS security updates are
outside this checkout. These are explicit verification gaps, not passing results.

## Previous findings: complete disposition

| ID | Current disposition | Evidence and remaining exposure |
| --- | --- | --- |
| S-1 | Mitigated for host-launched sidecars | `main.go` wraps the listener in `ipc.Authenticated`; UID/PID checks, owner-only Unix socket and Windows DACL. Standalone host PID zero allows the account. Reverse authentication is missing: R-01. |
| S-2 | **Open — high** | `kubeconfig/restconfig.go` passes exec config to client-go; enabled imported contexts are probed automatically. No consent/trust gate. Kubeconfig trust also covers token/certificate paths and destination/proxy selection. |
| S-3 | Accepted plaintext design; **retention open — high confidentiality impact** | [Cache ADR](../adr/2026-09-02-the-cache-is-ordinary-application-data.md). ConfigMaps, pod env, annotations, arbitrary CRDs and event messages can hold credentials. Cloud logout/RBAC revocation does not erase copies. |
| H-1 | Accepted broad GraphQL authority; exposure remains | [Allowlist ADR](../adr/2026-09-03-no-graphql-operation-allowlist.md). Current mutations affect app/account/cache lifecycle, not arbitrary live Kubernetes resources. Bundled malicious JS can read the mirror and invoke all custom commands. CSP does not reject compromised bundled code. R-02 covers navigation. |
| H-2 | Mitigated for release endpoint configuration | Host constants become command arguments; untagged sidecar ignores endpoint environment overrides. Ordinary environment effects still exist: kubeconfig selection, proxy/CA settings, executable lookup and log level. |
| H-3 | **Open — medium** | `entitlements.plist` enables library-validation bypass, JIT and unsigned executable memory. Library validation concerns in-process libraries, not permission to exec a separately signed sidecar. Test the minimum entitlement set in signed macOS bundles before removing exceptions. |
| H-4 | **Open — medium operational risk** | No in-app updater or update notification. Release workflow configures macOS notarization, SignPath MSI signing and GPG checksums; Linux bundles remain unsigned. A manual test-signing policy exists. Actual signing and trusted key distribution are unverified. |
| S-4 | Mitigated on Unix; Windows accepted dependency | Owner-only umask, directory and SQLite file checks. [Windows profile ACL ADR](../adr/2026-09-02-windows-cache-files-rely-on-the-profile-acl.md). Modes are not encryption or a same-user barrier. |
| S-5 | Partly mitigated; accepted per-event TTL decision | Default cache ceiling is 2 GiB including WAL/SHM; the janitor stops sync after a sweep, not before allocation. Status history has a seven-day TTL. [Size ADR](../adr/2026-09-03-bound-the-cache-by-total-size.md). Aggregate disk/memory risk remains R-05. |
| S-6 | Removed from current surface | `chatStream` is absent from the current schema/resolvers. No implemented assistant egress. Require prompt-injection, tool authorization and data-consent review before adding one. |
| X-1 | Partly mitigated | Three PR audit jobs exist; moderate npm findings do not fail, Rust informational warnings require review, releases are independent and there is no scheduled scan. R-08/R-09 below. |
| H-5 | **Open**, incorporated into R-01 | Predictable temp socket path and unlink-before-bind remain. Same-user replacement and other-user startup obstruction are separate from client authentication. |
| H-6 | Mitigated | Opener/shell have no frontend capability grant. Rust plugins remain registered for native use; custom application commands remain exposed. |
| H-7 | Mitigated for structured-field collision | `services/sidecar/logs.rs` nests forwarded fields. This is not credential redaction or terminal-control sanitization; R-06. |
| S-7 | Visibility mitigated, insecure transport remains opt-in risk | `tlsUnverifiedReason` marks skip-verify and plain HTTP. No enforcement upgrade: credentials/data can still traverse insecure configured connections. |
| S-8 | Partly mitigated, held by review | Separate unverified type makes conversion explicit; public `DisplayOnly` returns `Identity`, so compiler enforcement does not extend to authorization. Startup auth state is presence of a refresh token; cloud authorization belongs to the server. |
| S-9 | Allowlist accepted; **resource budgets open — medium** | 64 MiB outer request cap, 5s header and 60s idle timeouts; host queries have 30s/64 MiB response bounds. No application query complexity, depth, concurrency or subscription quota. Declining an allowlist did not accept unbounded work. R-05. |
| X-2 | Partly mitigated; future surfaces deferred | App source renders text and lint rejects HTML sinks/template evaluation. This does not audit dependency internals or future terminal escape/URL handling. R-02 and R-06 remain. |

## Additional findings and mitigations

### R-01 — host does not authenticate the sidecar (medium, open)

**Evidence:** `src-tauri/src/services/sidecar/ipc.rs::connect_with_budget` accepts any successful
connection. `service.rs::spawn` uses `Endpoint::pick(std::env::temp_dir())`; the name contains PID
and a counter. Unix `ipc.Listen` removes an existing path before binding. Windows names share the
named-pipe namespace. The host retains the child PID but never compares it with the server peer.

**Prerequisite/impact:** a same-account process able to replace the socket can impersonate sidecar
responses and observe IPC requests. A different account may obstruct a predictable shared-temp
name; this is not evidence of cross-account credential theft. Windows spoofing feasibility depends
on pipe creation/access semantics and requires a native test. Server-side peer checks do not
protect the host from a fake server. No claim of arbitrary code execution from spoofed JSON.

**Mitigation owed:** verify the expected child PID/account on *every* query, SSE and gRPC connection,
fail closed, and use a private runtime directory with safe creation/cleanup on Unix. Test malicious
listeners, replacement/reconnect, child exit/PID reuse and Windows pipe precreation. Owner-only
placement alone does not stop same-user impersonation.

### R-02 — CSP is overstated as network containment (medium, open)

**Evidence:** production CSP restricts fetch/connect, scripts, images, frames and forms, but
`window_manager.rs::build_window` installs no native navigation or new-window URL policy.
`connect-src` is not a top-level navigation allowlist. A bundled compromised script is permitted
by `script-src 'self'` and could attempt navigation with cached data in a URL. This is a missing
application control; actual behavior must be verified on WKWebView, WebView2 and WebKitGTK.
No HTML injection or remote-origin IPC access was demonstrated.

**Mitigation owed:** a native policy admitting only the app's exact origin, blocking remote and
unexpected-scheme navigation/popups, with deliberate external links owned by native code. Test
normal routing/reload plus attacker URLs and confirm remote documents cannot invoke commands.
The existing H-1 ADR remains unchanged; it does not establish that every egress path is blocked.
See the [CSP specification](https://www.w3.org/TR/CSP/).

### R-03 — GraphQL diagnostic disclosure (medium, mitigated in this change)

**Evidence:** `graph/server.go` logged `RawQuery`, error message, operation name and response path.
Inline arguments are part of the document; scalar validation errors can echo variable values;
names and aliases are caller-controlled. The old sentinel test deliberately used a valid ID and
therefore missed validation disclosure.

**Change:** log only operation type and Go error type. Detailed errors still return over IPC.
`TestErrorLogOmitsRequestData` exercises inline/variable values, invalid scalars, names, aliases and
unknown fields, and requires both an error response and a log entry. This reduces diagnostic detail
in logs intentionally; other log producers remain R-06. Existing logs are not scrubbed.

### R-04 — cloud SSE aggregate-frame memory exhaustion (medium, mitigated in this change)

**Evidence:** `cloud/api/api.go::parseSSE` capped each scanner line at 1 MiB but appended every
`data:` line to one builder until a blank line. A compromised/misconfigured cloud endpoint can
send indefinitely many small lines and exhaust sidecar memory while staying under the line cap.

**Change:** reject aggregate data above 1 MiB before appending. The parser returns an error and
its owner closes the response/retries with backoff. `TestParseSSEBoundsAggregateFrame` constructs
many individually small lines without a frame terminator. This does not bound native IPC SSE or
Kubernetes object decoding; those remain R-05.

### R-05 — incomplete resource budgets (medium, open)

**Evidence:** `graph/server.go` has no complexity or concurrency extension. The schema includes
nested record relationships and unbounded lists. `graphql/subscribe.rs` uses `eventsource-stream`
without an app-level frame or active-subscription quota. `kubestore/rawcodec.go` decompresses via
`io.ReadAll`. Kubernetes list/watch decoding and discovered-kind fan-out are not covered by the
8 MiB discovery-document limit. Cache ceilings are per file, checked after writes; no aggregate
cache or app database quota exists. A 2 GiB compressed cache does not bound decompressed memory.

**Prerequisites/impact:** a compromised webview can allocate concurrent expensive work; a hostile
API server can return excessive data/kinds; modified cache data can force decompression expansion.
A normal large fleet can also exhaust disk or memory. Client-go idle-read limits constrain stalls,
not total bytes or the size of a response that continues arriving. Exact peak usage was not measured.

**Mitigation owed:** specify query cost/depth, list/frame/object/decompression limits, subscription
and worker budgets, and a global disk budget including `app.db`. Verify boundary rejection, recovery
and legitimate large clusters. Existing per-request/per-file controls are partial mitigations.

### R-06 — diagnostic and malformed-object confidentiality gaps (medium, open)

**Evidence:** `auth/auth.go` logs upstream OAuth errors, which can include response text;
`oauth.Revoke` and `cloud/api` retain bounded remote error bodies. Probe/sync errors reach records
and diagnostics. Namespacing forwarded log fields does not remove secrets or terminal controls.
`kubestore/objects.go::redact` leaves an unexpected scalar/map shape untouched; it also chooses
redaction from the body's self-declared group/kind. A hostile endpoint need not obey Kubernetes
schemas. Legitimate credential-bearing fields outside the small table remain the accepted S-3 risk.

**Impact:** sensitive remote text or malformed credential fields may persist in logs, records or
cache files. The GraphQL logging fix does not make all diagnostics safe. A hostile server already
controls its response; this finding concerns local persistence, not gaining previously unknown
cluster credentials.

**Mitigation owed:** structured error categories with bounded/redacted diagnostics; sentinel tests
through auth/probe/sync and native forwarding; fail-closed treatment for named credential paths
with malformed shapes, and tests for GVK/address mismatches. No exhaustive CRD redaction promise.

### R-07 — release input shell injection (medium, mitigated in this change)

**Evidence:** `release.yml` interpolated manual `version` into shell source, then passed it to
version/path/build-config expressions in jobs with release permissions and signing environments.
Exploit requires dispatch/tag privileges; this is not an unauthenticated pull-request path.

**Change:** dispatch inputs cross into the config script as environment values; both manual and
tag versions must match numeric `major.minor.patch` plus an optional dot-separated prerelease.
Signing policy and boolean outputs are also validated. Quotes, whitespace, shell substitutions,
newlines and build-metadata suffixes are rejected. The Apple API key is staged via an environment
variable rather than interpolated shell source. Downstream version expressions receive only the
validated alphabet. Future workflow interpolation must preserve this rule.
See [GitHub's script-injection guidance](https://docs.github.com/en/actions/concepts/security/script-injections).

### R-08 — release and supply-chain assurance gaps (medium, open)

**Evidence:** most actions are pinned to mutable tags (`rust-toolchain@master` included);
`release.yml` grants write permissions at workflow scope and does not require CI/audit jobs.
PR-only audits do not detect new advisories on an idle branch. npm fails only for high/critical;
Rust unsound/unmaintained informational advisories need explicit triage. Repository settings may
add protections, but they were not inspected. A source-tree SBOM is configured; its coverage of
native runtime libraries and embedded sidecar artifacts was not verified.

**Mitigation owed:** pin actions to reviewed full SHAs; narrow per-job permissions; gate the exact
release commit on required checks and triaged advisories; schedule scans; verify production signing
policy/artifacts, publish trusted verification instructions/key fingerprint, and validate SBOM
coverage. OS-provided WebKitGTK/WebView2/WebKit, Go/Rust toolchains and native libraries require
separate patch tracking. No direct claim that a signing key is compromised.

### R-09 — Rust dependency advisories (open)

The current lock contains `glib 0.18.5`, affected by
[RUSTSEC-2024-0429](https://rustsec.org/advisories/RUSTSEC-2024-0429.html), an **unsoundness** advisory:
`VariantStrIter` can invoke undefined behavior/null-pointer dereference. The patched line is
`>=0.20.0`. It is in the Linux GTK dependency stack. No application call to the affected iterator
was identified, and transitive runtime reachability was not established; do not label this proven
remote code execution or silently dismiss it. Track an upstream-compatible fix/backport and native
validation; blindly forcing glib 0.20 into the GTK 0.18 graph is not a compatible remediation.

The fallback lockfile review also matched these **unmaintained** notices, not demonstrated exploits:

| Locked packages | Advisory IDs |
| --- | --- |
| `gtk`, `gtk-sys`, `gtk3-macros`, `gdk`, `gdk-sys`, `gdkx11`, `gdkx11-sys`, `gdkwayland-sys`, `atk`, `atk-sys` (all 0.18.2) | RUSTSEC-2024-0411 through RUSTSEC-2024-0420 |
| `proc-macro-error 1.0.4` | RUSTSEC-2024-0370 |
| `unic-char-range 0.9.0` | RUSTSEC-2025-0075 |
| `unic-common 0.9.0` | RUSTSEC-2025-0080 |
| `unic-char-property 0.9.0` | RUSTSEC-2025-0081 |
| `unic-ucd-version 0.9.0` | RUSTSEC-2025-0098 |
| `unic-ucd-ident 0.9.0` | RUSTSEC-2025-0100 |

Advisory records are available in the [RustSec database](https://github.com/RustSec/advisory-db).
Owners: desktop/dependency maintainers. Resolution requires an upgrade/replacement/backport or an
explicit, scoped ADR with a revisit trigger; these notices have no blanket acceptance here.

### R-10 — OAuth callback availability (low, open)

`auth/login.go` creates its loopback HTTP server without read-header/idle limits, and sends a valid
state-bearing error to `errCh` with a blocking send. Repeated callbacks with known state can leave
handlers blocked after the buffer fills; closing sockets does not unblock channel sends. The
listener is ephemeral and the login lifetime is five minutes, reducing exposure. Missing/invalid
state cannot reach this path. Add bounded HTTP handling, nonblocking one-shot error delivery, and
repeated-callback/cancellation tests. Revocation after logout is separately best-effort: failure or
process exit can leave the server grant valid even though the local keyring entry is cleared.

### R-11 — pending login can restore a logged-out session (medium, open)

`auth/auth.go::StartLogin` launches a detached five-minute completion task and unconditionally
calls `grant.set` after exchange/verification. `Logout` clears only the current grant; it neither
cancels pending logins nor invalidates their completion. Reproduction sequence from source: begin
login, call logout before its callback, then complete the browser flow; the old task can persist a
fresh token and publish signed-in again. Concurrent logins can similarly complete out of order.
This requires an existing login attempt, not guessing OAuth state or bypassing PKCE.

**Mitigation owed:** serialize session transitions with a login generation/epoch, cancel pending
flows on logout or replacement, and check the generation atomically with credential persistence.
Cancellation alone is insufficient when completion is already racing. Revoke/discard superseded
grants and test the sequence with channel-controlled fake login completions. Owner: auth.

## Validation and remaining verification

- `go test ./...` passed on Linux/arm64 before and after the code changes. Targeted `./graph`
  and `./internal/services/cloud/api` tests also passed, covering the two code mitigations above.
- Frontend: `pnpm test run` passed all 40 files / 377 tests; `pnpm lint` passed. Commands used
  `npx --yes pnpm@11.0.8` after a frozen-lockfile install.
- `go test -race ./...` could not run: this environment disables CGO and has no C compiler.
- `npx --yes pnpm@11.0.8 audit --json` completed: zero reported advisories at all severities,
  774 dependencies reported. This is a database result, not a malicious-package audit.
- `go run golang.org/x/vuln/cmd/govulncheck@v1.7.0 ./...` could not fetch the database:
  `https://vuln.go.dev/index/modules.json.gz` returned HTTP 403. **Go advisory status is unknown.**
- `cargo-audit 0.22.2` installation was attempted after installing Rust, but compilation failed
  because no C linker is available. **Canonical Cargo audit and native Rust tests remain unrun.**
  Fallback: matched every locked package/version against nonwithdrawn RustSec records from
  `5a0ebedfe8bdd2e295b171f4162f8c977bcad9a5`, using patched/unaffected version ranges. This identified
  one unsoundness and sixteen unmaintained notices above; it is not a cargo-audit success report.
- Tracked text files were scanned for private-key headers, AWS access-key IDs and GitHub token
  patterns: no matches. This limited pattern scan does not cover entropy, every provider or history.
- Release input checks passed 18 local cases: valid versions and hostile strings through both
  manual-dispatch and tag branches. `git diff --check` passed. No release,
  signing operation or external publication was performed.

Before calling a release security-verified, complete the blocked Go/Cargo checks, native platform
checks (including actual signatures/entitlements/navigation/IPC peers), dependency reachability
triage, and release-settings review. Keep failures and unreviewed advisories visible; a green
scanner cannot close the open design findings above.
