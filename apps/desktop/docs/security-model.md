# Security model

What the app protects, from whom, where the boundaries are, and which protections are enforced
by a test rather than by remembering. Present tense, like a `CLAUDE.md`: this file states what is
true now.

This page is the map: the threat model, the assets, the boundaries and an index. The protections
themselves are in one file per subject under [`security-model/`](security-model/), and
[what is not protected](security-model/not-protected.md) lists every accepted risk, gap and
residual in one place.

Dated findings live in [`docs/security/`](security/) and are never rewritten. Gaps live in
[`TODO.md`](TODO.md#security), each carrying the shape of the work; one settled enough to build
straight through gets a spec ([`docs/specs/`](../../../docs/specs/README.md)). A risk we decide to accept becomes an ADR
and a **By decision** row in its subject, so it can be told apart from one nobody has noticed.

Latest review and complete disposition register: [4 September 2026](security/2026-09-04-security-review.md).
Open findings are recorded work, not accepted risks unless an ADR explicitly says so. Tests pin
specific properties; they do not prove the absence of vulnerabilities or enforce repository branch
protection. Platform checks and advisory-scan limitations are listed in the review.

## Threat model

Kubetail runs as the user, holds the user's cluster credentials, and lets a model run commands. It
defends the user's machine, credentials and clusters against the text and code it handles. It does
not defend the user against their own account. The first review's threat model, with its findings, is the
[2 September 2026 record](security/2026-09-02-threat-model.md).

### Who attacks

| Attacker | Starts with | Stopped by |
| --- | --- | --- |
| **A cluster the user connects to** | Every object body, event and log line Kubetail reads, which reach the screen, the caches and the model. | Text is drawn as text and stripped of control sequences ([the webview](security-model/webview.md), [logs](security-model/logs.md)); Secret data is redacted before it is stored ([cluster data](security-model/cluster-data.md)). |
| **The model, once its input holds attacker text** | Every tool the chat offers. It reads all of its input as instruction. | A command runs in the sandbox or waits on the user ([commands](security-model/commands.md)); a cluster write, a file outside the chat's folders and a fetch each ask ([the cluster proxy](security-model/cluster-proxy.md), [the file and web tools](security-model/file-tools.md)); what it sends out is bounded and redacted ([model providers](security-model/model-providers.md)). |
| **A command in the sandbox** | Code running as the user, started unasked. | It reads the system, the toolchains and the folders the user granted, never the denied-always list or Kubetail's directories ([files](security-model/sandbox-files.md)); it has no network but its cluster unless the user turned it on ([network](security-model/sandbox-network.md)); it holds no credential and cannot reach the sidecar's API ([processes](security-model/sandbox-process.md), [cloud platforms](security-model/cloud-platforms.md)). |
| **A command outside the sandbox** | The user's whole account, once approved. | Nothing after the approval. The request shows exactly what will run, and the user's eye is the gate ([commands](security-model/commands.md)). |
| **A script in the webview** | Every GraphQL operation, `approvalDecide` and the permission settings included. | It must not run at all: the CSP, no HTML sink and restricted navigation ([the webview](security-model/webview.md)). |
| **Another account on the machine** | The shared filesystem and process table. | Owner-only folders, files and socket, and a peer check both ways ([the socket](security-model/ipc.md), [folders and links](security-model/folders.md)). |
| **A dependency, a CI action or a release step** | Code in the build. | Pinned actions, advisory audits, delayed installs and signing keys kept off runners that install dependencies ([supply chain](security-model/supply-chain.md)). |
| **A model provider, a connector's issuer or a network peer** | The requests Kubetail sends it. | Fixed endpoints, a credential sent to its own host alone, verified ID tokens and signed updates ([credentials](security-model/credentials.md), [model providers](security-model/model-providers.md), [supply chain](security-model/supply-chain.md)). |

### What Kubetail assumes

- **The user's account and root are trusted.** A process of the user's own outside the sandbox,
  or anyone who can run code in or debug the host process, holds everything Kubetail holds.
- **Every folder on the path to one of Kubetail's folders can be written only by the user or by
  root.** Kubetail checks one folder itself, the Unix runtime folder, because its default parent is
  shared. → [ADR](adr/2026-11-24-follow-a-link-to-a-kubetail-folder-once-at-start-and-refuse-one-below-it.md).
- **The kubeconfig is the user's.** Reading it can run its `exec` plugins, and its TLS policy is
  the one Kubetail applies.
- **The page in the webview is the app's own.** The host forwards whatever it sends.

### What it does not protect

The largest accepted risks, each stated in full in its subject:

- A chat's transcript, and the cluster data the model reads into it, leave the machine for the
  provider the user picked ([model providers](security-model/model-providers.md)).
- A sandboxed command with network on can send what it read to any server
  ([network](security-model/sandbox-network.md)).
- The caches, `app.db` and the cached log lines hold cluster data and chat text in the clear,
  behind file modes alone ([cluster data](security-model/cluster-data.md)).
- Native Windows has no sandbox, so there the approval request is the only gate
  ([processes](security-model/sandbox-process.md)).
- A link on the path to one of Kubetail's folders can be repointed by the user's own processes
  between launches ([folders and links](security-model/folders.md)).

The whole list is [what is not protected](security-model/not-protected.md).

## Assets

- **Kubeconfig credentials**, including `exec` plugin invocations that mint live tokens.
- **The cluster caches** — one SQLite file per cache holding full object bodies for every synced
  kind.
- **The IPC socket** — a credential-free path to those caches and to every mutation.
- **The Kubetail account's refresh token** in the OS keyring, and its access and ID tokens in the
  sidecar's memory. No request is signed with them
  ([the Kubetail sign-in on the engine](security/2026-11-19-the-kubetail-sign-in-on-the-engine.md)).
- **The ChatGPT plan's refresh token** in the OS keyring, which spends the user's plan on
  `api.openai.com`, and its hour-long access token in the sidecar's memory
  ([the ChatGPT connector](security/2026-11-18-the-chatgpt-connector.md)).
- **The OpenRouter connector's API key** in the OS keyring, which spends the credits on the
  user's OpenRouter account and does not expire
  ([the OpenRouter connector](security/2026-11-19-the-openrouter-connector.md)).
- **Saved provider API keys** in the OS keyring, one per keyed provider, each spending on its
  owner's account at that vendor ([saved provider keys](security/2026-11-20-saved-provider-keys.md)).
- **The AWS connector's session** in the OS keyring, a refresh token and the key it is bound to,
  which yield credentials with the signed-in principal's whole permission, and those fifteen-minute
  credentials in the sidecar's memory. Nothing uses them yet
  ([the AWS connector](security/2026-11-19-the-aws-connector.md)).
- **The Google connector's refresh token** in the OS keyring, whose `cloud-platform` scope acts
  as the user on every Google Cloud project they can reach
  ([the Google connector](security/2026-11-19-the-google-connector.md)).
- **The GitHub connector's OAuth token** in the OS keyring, which acts as the user on their
  repositories, organizations and workflow files and does not expire
  ([the GitHub connector](security/2026-11-19-the-github-connector.md)).
- **The user's own files**: the credentials, histories, browser profiles and documents a command
  running as the user could read. The denied-always list and the closed folders name them
  ([the sandbox's files](security-model/sandbox-files.md)).
- **Kubetail's own folders**, as much for what they decide as for what they hold: `app.db` keeps
  the approval modes, the always rules, the folder grants and the settings that say what runs
  unasked ([folders and links](security-model/folders.md)).
- **Cluster availability** — the app holds standing watches on every mirrored kind.
- **The user's AWS credentials**, per profile, resolved by `services/awsauth` once a sandboxed
  request needs them and held in memory, blanked in every log.
- **The user's GitHub token**, read from `gh` outside the sandbox once GitHub's switch is on, and
  held in the sidecar's memory until `gh`'s next answer replaces it or the sidecar exits.
- **The user's Azure and Google access tokens**, read from `az` and `gcloud` outside the sandbox
  once a run with that platform's switch on needs one, and held in the sidecar's memory.
- **The release's signing keys**: the GPG key, the updater's two keys, the Apple certificate and
  the SignPath token, which decide what an installed app accepts as Kubetail
  ([supply chain](security-model/supply-chain.md)).

## Boundaries

Ten boundaries. The first seven are in the order an attacker from outside meets them; the last
three are the ones a model's command crosses, and the app's own update.

1. **Webview → host.** The host exposes Tauri commands; no filesystem or shell plugin permission
   is granted to the page. Production CSP restricts script loads and connection APIs to local
   origins; it is not a general network sandbox. Navigation is restricted separately, by
   `build_window`'s default-deny callbacks. The host forwards GraphQL verbatim.
   Script execution can read the caches and invoke every mutation. The schema exposes no
   arbitrary Kubernetes write or pod-exec operation. It does expose the settings that decide what
   runs unasked (folder grants, always rules, approval modes, a chat's sandbox and network
   switches), `sandboxConsoleRun`, which runs a typed command in the sandbox, and
   `approvalDecide`: the approval id rides the messages watch, so a script in the page can approve
   any command the model asks for, and a prompt injection that makes the model ask reaches the
   user's shell through it — the user's eye is the bound only while the page is the user's
   ([the bash tool](security/2026-09-18-bash-tool.md)). Custom host commands
   also allow window creation, host preferences and app exit.
2. **Host → sidecar.** One Unix socket (named pipe on Windows) carries GraphQL over HTTP/1.1 and
   gRPC over h2c. File mode/DACL restricts the account; the sidecar additionally checks the host PID
   supplied by the host. Standalone `--host-pid=0` permits the same account. Authentication runs
   both ways: the host reads the peer's PID from the kernel on every dial and refuses anything that
   is not the child it spawned. On Unix the endpoint sits in an owner-only directory; the Windows
   pipe namespace is flat, so there the DACL and the peer check are the whole policy.
3. **Sidecar → clusters.** Credentials and TLS policy both come from the user's kubeconfig. Everything
   a watch returns is attacker-controlled if the cluster is.
4. **Sidecar → cloud.** OIDC sign-in through the system browser and a loopback redirect, run by
   the Kubetail Cloud connector (`services/connectors/kubetailcloud`) on the OAuth engine, the only path to the
   cloud. OAuth transmits protocol credentials and identifiers, and the issuer sees
   normal connection metadata. There is no implemented cluster-to-cloud data path. Cloud sign-out does not
   revoke Kubernetes credentials, stop cluster access, or erase caches, and revoking its own
   grant is best-effort: the local credential is cleared first and the revoke is tried twice while
   the app runs, so an unreachable issuer or a quit leaves the grant live at the issuer.

5. **Sidecar → model providers.** A chat turn sends that chat's whole transcript — everything the
   user typed, every answer — to the provider the send named, over HTTPS under that provider's credential: a key read from the
   environment or saved in the keyring, or a connector's sign-in (boundary 7). The keyless row,
   the user's own Ollama daemon, is pending rebuild. Nothing else goes: no kubeconfig, no identity. A
   transcript can hold anything the user pasted into it, cluster text included, and the model
   reads all of it as instruction. The model's `KubeQuery` tool sends what one query over the
   chat's cluster's local cache reads, object bodies and events included, redacted and bounded
   per call and per turn, unasked; its bash tool — `bash_20250124` on the Anthropic row, the API's own `shell` on the
   OpenAI row — sends a command's output, redacted and cut, after the user approved the
   command. Both ride the two rows alike, and a platform row on either wire takes the cluster
   tools and no bash. Of the cloud platforms, Vertex AI
   is signed under the Google connector's sign-in (boundary 7); Bedrock and Foundry are pending
   rebuild. →
   [model provider egress](security/2026-09-11-model-provider-egress.md), [cloud
   platforms](security/2026-09-13-cloud-platforms.md), [the first tool](security/2026-09-14-list-objects-tool.md).
6. **Sidecar → GitHub.** With GitHub's switch on, a sandboxed run's `git` and `gh` reach
   `github.com` and `api.github.com` through the run's door, over HTTPS with the user's token, which
   the sidecar adds; the sandbox holds none. A read passes, and a change, a push and a read of
   stored data wait on the user. → [a sandboxed command reaches GitHub](security/2026-11-17-a-sandboxed-command-reaches-github.md).
7. **Sidecar → a connector's sign-in.** With the ChatGPT connector connected, the sidecar reaches
   `auth.openai.com` for discovery, the JWKS, the token and revocation endpoints, and sends a
   chat's transcript to `api.openai.com` under the plan's bearer, as boundary 5 says of any
   provider. Sign-in is OAuth with PKCE through the system browser and a loopback redirect; the
   webview holds no token. → [the ChatGPT connector](security/2026-11-18-the-chatgpt-connector.md).
   With the OpenRouter connector, the sidecar posts a sign-in's code and PKCE verifier to
   `openrouter.ai/api/v1/auth/keys` and checks a refused key at `openrouter.ai/api/v1/key`.
   → [the OpenRouter connector](security/2026-11-19-the-openrouter-connector.md).
   With the AWS connector, the sidecar posts a sign-in's code and PKCE verifier, then its refresh
   token, to `us-east-1.signin.aws.amazon.com/v1/token`, each under a DPoP proof.
   → [the AWS connector](security/2026-11-19-the-aws-connector.md).
   With the GitHub connector, the sidecar posts the OAuth App's client id to
   `github.com/login/device/code`, polls `github.com/login/oauth/access_token` with the device
   code, and reads `api.github.com/user`. → [the GitHub connector](security/2026-11-19-the-github-connector.md).
   With the Google connector, the sidecar reaches `oauth2.googleapis.com` for the token and
   revocation, `www.googleapis.com` for the JWKS, and sends a chat's transcript to Vertex AI in the
   user's project under the bearer. → [the Google connector](security/2026-11-19-the-google-connector.md).
8. **Model → tools → the user's machine.** A tool call is the model's word, and the model reads
   cluster text as instruction. On macOS and Linux a Bash command runs in a sandbox, unasked: it
   reads the system, the toolchains and the folders the user granted, less the denied-always list
   and Kubetail's directories, writes its workspace, and has no network but the chat's cluster
   unless the user turned the network on. Outside the sandbox (native Windows, a machine with no
   usable sandbox, a chat the user switched outside it) every command waits on the user's
   approval of that exact command. `Read`, `Write` and `Edit` reach the chat's own directory and
   the granted folders; anything else asks. → [commands](security-model/commands.md),
   [the sandbox's files](security-model/sandbox-files.md), [network](security-model/sandbox-network.md),
   [processes](security-model/sandbox-process.md), [the file and web tools](security-model/file-tools.md).
9. **Sandbox → clusters and cloud platforms.** A sandboxed command reaches the chat's cluster
   through the cluster proxy, where a read passes and a change runs, waits on the user or is
   refused by the user's mode and rules. It reaches AWS, Azure and Google Cloud, like GitHub in
   boundary 6, through the run's door: each platform is on until the user turns its switch off,
   the sidecar adds the user's credential outside the sandbox, a read passes unasked and a change
   asks. → [the cluster proxy](security-model/cluster-proxy.md),
   [cloud platforms and GitHub](security-model/cloud-platforms.md).
10. **App → update server.** On macOS and Windows the app fetches a manifest from
    `updates.kubetail.com` and installs only an artifact signed by one of the two keys it was built
    with. → [supply chain](security-model/supply-chain.md).

## Protections

Each subject file opens with its rule in a few sentences, then a table of rows, one property
each, then the evidence for every row: where it lives, the tests that pin it, and the records and
ADRs behind it.

**Enforced** means a named automated check detects the specified regression when run. CI defines
these checks, but required-check and release-gating settings are not established by this file.
**Built** means the mechanism and its tests are in, and the row says what is still unproven, such
as a live sign-in or a signed build nobody has yet run end to end. **Held by review** means it is true today and nothing stops the next change undoing it. **By
decision** means the gap is a risk we accepted, and links the ADR or record that accepted it.
**Not built** links the work. **Pending rebuild** means the mechanism was removed for a rewrite
(the LLM rewrite) and is not back yet; the row keeps the status it had and states what the rebuild
must reproduce. A row's **Residual** is what its protection leaves open.

<!-- index:start, written by tools/securitymodel -->
| Subject | Enforced | Built | Held by review | By decision | Not built | Pending rebuild |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| [Cloud platforms and GitHub](security-model/cloud-platforms.md) | 42 | 0 | 0 | 30 | 0 | 0 |
| [Cluster data and the caches](security-model/cluster-data.md) | 5 | 2 | 0 | 4 | 3 | 0 |
| [The cluster proxy and permissions](security-model/cluster-proxy.md) | 54 | 0 | 0 | 2 | 0 | 0 |
| [Commands and the approval request](security-model/commands.md) | 44 | 0 | 0 | 44 | 0 | 0 |
| [Sign-in, connectors and stored credentials](security-model/credentials.md) | 17 | 11 | 1 | 3 | 0 | 0 |
| [The file and web tools](security-model/file-tools.md) | 57 | 0 | 0 | 0 | 0 | 0 |
| [Kubetail's folders and links](security-model/folders.md) | 26 | 0 | 0 | 2 | 0 | 0 |
| [The socket and the processes](security-model/ipc.md) | 6 | 0 | 0 | 0 | 0 | 0 |
| [Logs](security-model/logs.md) | 33 | 0 | 0 | 2 | 0 | 0 |
| [Memory](security-model/memory.md) | 9 | 0 | 2 | 3 | 0 | 0 |
| [Model providers and what leaves the machine](security-model/model-providers.md) | 20 | 0 | 0 | 18 | 0 | 17 |
| [The sandbox: files and the fence](security-model/sandbox-files.md) | 41 | 0 | 0 | 6 | 0 | 0 |
| [The sandbox: network and the run's door](security-model/sandbox-network.md) | 16 | 0 | 0 | 5 | 0 | 0 |
| [The sandbox: environment, PATH and processes](security-model/sandbox-process.md) | 28 | 0 | 0 | 14 | 0 | 0 |
| [Supply chain, release and updates](security-model/supply-chain.md) | 14 | 4 | 9 | 8 | 3 | 0 |
| [The webview and the windows](security-model/webview.md) | 16 | 0 | 0 | 2 | 0 | 0 |
| **All** | 428 | 17 | 12 | 143 | 6 | 17 |
<!-- index:stop -->

`make lint-desktop` checks every row against the code: each cited test and file exists,
each status has what it needs (a test for *Enforced*, an ADR or record for *By decision*, its
work in `TODO.md` for *Not built*), no cell is over 600 characters, and every record in
[`docs/security/`](security/) is linked. `go run ./securitymodel write`, from `tools/`, rewrites
the index above and [what is not protected](security-model/not-protected.md).

## Two facts that shape everything

**The webview is fully trusted by the host.** `graphql_query` forwards the operation string
unexamined, so a script running in the page can read every mirrored object and call every mutation
— `approvalDecide` included, so it can also run whatever command the model can be made to ask for.
CSP and text rendering reduce injection risk; bundled malicious JavaScript is already permitted
by `script-src 'self'`. Navigation off the app origin is refused, so the script stays in the app's
own page — it does not stay out of the app's authority.

**The sidecar authenticates a process, not a principal.** With a nonzero host PID, the endpoint serves that process only — the kernel supplies the peer's pid, so it cannot be claimed. But an attacker who can
run code *inside* the host process, or debug it, inherits the whole surface, and the file mode still
carries the rest of the policy.

These controls do not isolate the app from a compromised user account or host process. The
mutation surface is local app, cache, account and settings state, the permission settings
included, and a shell on the user's machine: confined by the sandbox where one runs
([Bash runs in a sandbox](security/2026-09-28-bash-runs-in-a-sandbox.md)), behind the user's
per-command approval elsewhere ([the bash tool](security/2026-09-18-bash-tool.md)). A command's
cluster writes pass the cluster proxy, which asks
([cluster writes ask](security/2026-09-29-cluster-writes-ask.md)). A new write path requires a
new boundary review, as assistant egress had in [web search](security/2026-09-17-web-search.md).

## Terms

- **Kubetail's directories** — the data, config, cache, state, log and runtime folders, and on
  macOS and Windows the app folder that holds several of them.
- **A layout folder** — one of those folders, as the layout both the host and the sidecar
  compute names it. It may be a link, followed once at start.
- **A subfolder** — a folder Kubetail makes inside a layout folder (`chats`, `kube`, `logs`,
  `runs`, …). It must be a real folder.
- **A link parent** — the folder that holds a link Kubetail followed. No read-write grant sits on
  or above one.
- **The fence** — the set of Kubetail's directories, at their resolved paths, that no sandboxed
  command and no file tool may open.
- **The zones** — Kubetail's lists of locations: `System` and `Toolchain`, which every sandboxed
  run reads; `Never`, the denied-always list; `Closed`; and `NoWrite`.
- **The denied-always list** — the credentials, histories, container sockets and browser
  profiles no rule or grant opens.
- **A closed folder** — a folder such as `~/Documents` that a grant of the home leaves shut and
  a grant of its own opens.
- **A grant** — a folder the user opened to sandboxed commands and the file tools, read or read
  and write.
- **A rule** — a stored decision about a cluster request: always allow, or always ask.
- **A mode** — how a cluster's writes are decided when no rule matches: ask, allow, or read-only.
- **A session** — what one run may do: its kind, its cluster, its folders and its limits.
- **The run's door** — the one loopback relay through which a sandboxed run reaches GitHub and
  the cloud platforms; the sidecar adds the user's credential behind it.

## How this is recorded

- **This file** — what is true now. A row moves to *Enforced* when the mechanism holding it lands,
  never because the code looks right.
- **[`docs/security/`](security/)** — dated review records, append-only, each naming the commit it
  reviewed. Like ADRs, they are exempt from the describe-the-present rule: a review says what was
  believed on the day. A later review is a new file, not an edit.
- **[`TODO.md`](TODO.md#security)** — the gaps. They stay out of the subject files except as *Not
  built* rows, so nothing here reads as a protection that exists.
- **`docs/adr/`** — a risk we decide to accept, carried into its subject as a *By decision*
  row. An accepted risk is a decision with a cost, and writing it down is what stops the next
  review re-litigating it.
