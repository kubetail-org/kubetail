# Sandbox, credentials and permissions

A note, not a spec: the design of the agent runtime's security, written 29 September 2026. The
agent-security sequence built it and landed on 6 October 2026; *Where this meets the code* says
where the build departs from the note's words. What is true of the code is described by the
`CLAUDE.md` files, `docs/security-model.md` and the ADRs.

Where this note says "must", it states an invariant that a test covers. Where it says "should",
it states the recommended default, and the implementer may deviate with a documented reason.
Claude Code and Codex are referenced where their design is worth copying; both are open source
and their sandbox profiles are usable starting points.

## Purpose and scope

This note specifies the security architecture for the agent runtime of a Kubernetes monitoring
and troubleshooting app: how agent shell commands are sandboxed, how the cluster's credentials
are isolated, and how user permissions are enforced. It describes mechanisms and invariants, not code.

The app runs on the user's laptop (macOS first, Linux second). Users drive it from a chat
interface. A chat agent runs bash commands to answer questions and act; a monitoring agent runs in
the background while the app is open. Both use the user's own CLI tools (`kubectl`, `helm`,
`kustomize`, `git`, `jq`). The cluster is the one upstream they reach with a credential.

## Design principles and threat model

Three principles drive every decision below. An implementer who internalizes them can resolve
most questions this note does not answer.

1. **Two orthogonal axes: sandbox and approval.** The sandbox defines what a process can
   physically do (files, network, syscalls). The approval policy defines what the user must
   consent to. The sandbox is a hard floor that never depends on parsing bash or trusting the
   model. The approval layer can be heuristic because the floor is underneath it. Codex exposes
   exactly these two axes to users (sandbox mode and approval policy); copy that.
2. **Enforce upstream permissions at the credential proxy, not on the command line.** Every
   Kubernetes call passes through a proxy that injects the cluster's credentials. The proxy
   therefore sees every call and can classify it as read or write mechanically (HTTP verb, path).
   This classification is immune to aliases, `sh -c`, `eval`, base64 tricks and every other
   reason bash pattern-matching fails. Bash allowlists remain useful as UX; they are never the
   security boundary. Every other host a command reaches, it reaches with no credential at all,
   so the only question the proxy asks there is whether the host is allowed.
3. **One session = one sandbox instance + one proxy token + one approval policy.** The chat
   agent, the monitoring agent and any subagents are simply sessions with different tokens. The
   monitoring agent's "reads need no permission" property is enforced by giving it a token the
   proxy treats as read-only, not by prompting or by trusting the model.

### Threat model

The realistic attacker is prompt injection: text in pod logs, events, annotations, ConfigMaps or
GitHub issues that hijacks the model. The monitoring agent reads such content continuously and
unattended, so it is the more exposed of the two agents, not the less.

Assume the attacker fully controls the model's outputs. Then the attacker can: read anything the
user's cluster credentials can read (subject to Secret redaction, see Permissions), write inside
the session workspace and granted paths, and ask the user for permission. The attacker must not
be able to: read any credential, reach any network host outside the allowlist, write to the
cluster without a human approving, execute anything outside the sandbox, or read files outside
the allowed zones. Every mechanism below exists to make that
sentence true.

Out of scope: a malicious binary already installed on the user's PATH (the sandbox runs the
user's own tools and cannot protect against them), and the app process itself being compromised.

### Things an implementer is likely to get wrong

- Treating bash pattern matching as security. It is UX only.
- Letting a real credential into the sandbox environment "just for convenience". Once done,
  credential isolation is gone and hard to recover.
- Making the monitoring sandbox weaker because it is "read-only anyway". Its exposure to injected
  content is higher.
- Forgetting that deny rules must win over user grants. Granting `~` must not expose `~/.ssh`.
- Resolving PATH or environment from the app's own launch environment. A GUI app on macOS does
  not inherit the user's shell PATH.

## System overview

The system has four components: sessions, the sandbox, the proxy, and the permissions layer.
Two sessions (chat and monitoring) share one proxy with two faces: the cluster proxy, which
fronts the Kubernetes API servers with the cluster's credentials, and the egress proxy, which
relays a listed host and injects nothing.

Each agent runs in its own sandboxed session with no credentials and no direct network egress;
every connection carries a session token to the proxy, which authenticates it, classifies a
cluster call or checks a host, and applies that session's approval policy before forwarding.

| Component | Responsibility | Lives |
| --- | --- | --- |
| Session | Binds a sandbox instance, a proxy token and an approval policy to one agent run | App process |
| Sandbox | Confines each agent command: filesystem zones, allow-listed environment, no egress, syscall limits | OS primitives (Seatbelt, bubblewrap) |
| Proxy | Holds the cluster's credentials, redirects `kubectl`/`helm` traffic, classifies read vs write, enforces the host allowlist for everything else | Host process, outside the sandbox |
| Permissions | Action classes, approval modes, grant rules, prompt UX; decisions consumed by the proxy and by the app's tool layer | App process |

## Sandbox

The sandbox wraps every command the agent runs, using OS primitives: Seatbelt (`sandbox-exec`
profiles) on macOS, bubblewrap plus seccomp (and optionally Landlock) on Linux. Sandboxing is per
command, not a long-lived container; all state lives in the workspace on disk, so this is simpler
and equally safe. If the sandbox cannot be established (for example unprivileged user namespaces
are disabled on a hardened Linux distro), every command runs outside it and waits for the user's
approval, as it did before the sandbox existed, and the app says so: the sandbox switch is not
offered and the first-launch screen names the reason. The app never runs a command unconfined
and unasked, and never falls back silently.

### Filesystem zones

The filesystem is split into three zones with different defaults. The rule is: system directories
are readable by default, the home directory is closed by default except for tool directories, and
only the workspace is writable.

Rationale: system directories hold binaries and libraries and almost never hold secrets, so
allow-by-default keeps the user's tools working without a fragile allowlist. The home directory
holds every secret and private file, and the tool directories inside it are few and well known.
Claude Code and Codex allow reading nearly everything with a deny list; that suits a coding tool
where the repo is the user's data. This app's subject is the cluster, not the laptop, so a closed
home is cheap.

**Read: allowed by default (system)**

| Path | Why |
| --- | --- |
| `/usr`, `/bin`, `/sbin`, `/lib*`, `/System` (macOS), `/Library/Frameworks` | Binaries and shared libraries |
| `/opt/homebrew`, `/usr/local`, `/opt/local`, `/nix/store`, `/snap` | Package managers; Homebrew symlinks resolve into `Cellar` here |
| `/etc` narrowed to `resolv.conf`, `hosts`, `localtime`, `ssl`, `ca-certificates`, `passwd`, `group`; never `/etc/ssh`, `/etc/sudoers*`, `/etc/shadow`, `/etc/krb5*` | Resolver, locale, timezone, CA bundles |
| `/private/etc`, `/private/var/db/timezone` (macOS) | Resolved forms of the above |
| `/System/Library/dyld`, `/System/Volumes/Preboot/Cryptexes`, `/private/var/db/dyld`, `/System/Cryptexes` (macOS) | The dyld shared cache; without these every binary fails at once |
| `/dev/null`, `/dev/urandom`, `/dev/zero`, `/dev/tty` | Required by most tools; deny the rest of `/dev` |
| `/proc/self`, `/proc/sys/kernel/random` (Linux) | Runtimes read these; deny the rest of `/proc` |

**Read: allowed by default (home toolchain)**

| Path | Why |
| --- | --- |
| `~/.local/bin`, `~/bin`, `~/go/bin`, `~/.cargo/bin` | User-installed binaries |
| `~/.asdf`, `~/.local/share/mise`, `~/.nvm`, `~/.pyenv`, `~/.rbenv`, `~/.volta`, `~/.rustup` | Version-manager shims and the interpreters, standard libraries and site-packages behind them |
| `~/.krew` | kubectl plugins |
| `~/.local/share/helm/plugins` | helm plugins |
| Every other directory on the user's resolved PATH | Detected once, filtered, shown in settings (see PATH resolution) |

**Read: denied always, even when a rule above or a user grant would match**

| Path | Why |
| --- | --- |
| `~/.kube`, `~/.aws`, `~/.config/gh`, `~/.config/gcloud`, `~/.azure`, `~/.docker`, `~/.helm`, `~/.terraform.d` | Cluster credentials, which the proxy owns, and cloud credentials, which no command in the sandbox may hold |
| `~/.ssh`, `~/.gnupg`, `~/.netrc`, `~/.npmrc`, `~/.pypirc`, `~/.gem/credentials`, `~/.git-credentials`, `~/.config/git/credentials` | Tokens for everything else |
| `~/Library/Keychains`, `~/Library/Cookies`, browser profile directories, the app's own data directory | OS, browser and app secrets |
| `~/.bash_history`, `~/.zsh_history`, `~/.*_history` | Frequently contain pasted secrets |
| `~/Documents`, `~/Desktop`, `~/Downloads`, and by default everything else in `~` | Listed explicitly so a future allow rule cannot cover them by accident |
| `/var/run/docker.sock`, `~/.docker/run/docker.sock`, `/run/containerd` | Socket access is root on the host |
| Other users' home directories, `/root` | Explicit, not implied |

User grants ("let the agent see `~/code/my-service`") are appended to the allow rules for a
session or persistently, as read or read-write. The denied-always list must still win, so
granting `~` wholesale cannot expose `~/.ssh`. This ordering is the single most important
invariant in the sandbox and must have a test.

One sandboxed run Kubetail starts reads the list: the login shell that builds the shell snapshot
(step 4A). Its output reaches only commands run outside the sandbox, which read the home
themselves, and hiding `~/.ssh/config` or `~/.kube/config` would change what it builds. It still
writes nothing but its scratch folder and reaches no network. The `PATH` resolution's shell,
whose output leaves the sandbox, has the list shut like any other run.

### Write policy

Writable: the session workspace, a private per-session temp directory (`TMPDIR` pointed at it),
and paths the user granted as read-write. Nothing else, including nothing in the toolchain
directories, so a compromised session cannot plant a modified `kubectl` for the next one.

Tools that insist on writing under `$HOME` are redirected with their own variables rather than by
opening the real home: `HELM_CACHE_HOME`, `HELM_CONFIG_HOME`, `HELM_DATA_HOME`, `KUBECACHEDIR`,
`XDG_CACHE_HOME`, `XDG_CONFIG_HOME`, `XDG_DATA_HOME`, `NPM_CONFIG_CACHE`, `PIP_CACHE_DIR`,
`GOCACHE`, `GOMODCACHE`, `CARGO_HOME`. Setting `HOME` itself to the workspace catches the rest. Prefer a
persistent per-cluster cache directory over a per-session one for helm repository indexes and
similar, so sessions do not re-download.

### Environment

The environment is constructed from scratch, never inherited from the user's shell or from the
app's launch environment. Pass through only: `PATH` (the resolved and filtered list), `HOME` and
`TMPDIR` (workspace), `KUBECONFIG` (the generated proxy kubeconfig),
`HTTPS_PROXY`/`HTTP_PROXY`/`NO_PROXY`, `TERM`, `LANG`, `TZ`, and the `*_HOME` redirects above.

Must never pass through: `SSH_AUTH_SOCK`, `GITHUB_TOKEN`, `GH_TOKEN`, `GITHUB_ENTERPRISE_TOKEN`,
`GH_ENTERPRISE_TOKEN`, `AWS_PROFILE`, any `AWS_*`
credential variable, `DOCKER_HOST`, `DYLD_LIBRARY_PATH`, `DYLD_INSERT_LIBRARIES`,
`LD_LIBRARY_PATH`, `LD_PRELOAD`. The last four let the dynamic loader pull code from any readable
path; cover them with a test so a later passthrough cannot reintroduce them.

### Process and kernel restrictions

- No outbound network sockets except loopback. Seatbelt: deny `network-outbound` except to
  localhost. Linux: a network namespace with only `lo`, plus a Unix socket to the proxy.
- No `ptrace` or `task_for_pid`; no reading other processes' environment.
- No privilege escalation: `no_new_privs` on Linux; deny execution of `sudo` and other setuid
  binaries explicitly on both platforms.
- Minimal Mach services on macOS: Seatbelt denies `mach-lookup` by default; allow only what CLI
  tools need (user lookup via `opendirectoryd`). Nothing should need the keychain.
- Resource limits per command: CPU time, memory, open files, process count, and a wall-clock
  timeout, so a runaway `kubectl get --watch` cannot consume the machine.

### Network

Deny by default; all egress goes through the proxy. The sandboxed process has no outbound
sockets other than loopback. The proxy enforces a host allowlist: the API servers of every kube
context, and the hosts the user has allowed, such as a chart or package registry. A listed host
is a tunnel the proxy cannot see into, and the command holds no credential to send through it.
An attempt to reach an unlisted host is an action requiring permission ("the agent wants to
reach `example.com`; allow once or always?"). The machine's own services are closed too: an IP
literal, and a name that resolves to the machine or the local network, are refused, except a
kube API server the kubeconfig puts there. `kubectl port-forward` stays refused (decision 7).

This is the mechanism that makes prompt injection mostly harmless: a hijacked agent that holds no
credentials and can reach no arbitrary host cannot exfiltrate. The residual is a listed host
that takes an upload with no credential, which is why a host the user lists is the user's to
judge and Settings says so.

### Per-OS implementation notes

**macOS (Seatbelt).** Later rules override earlier ones, so the profile is: `(deny default)`, the
system allows, `(deny file-read* (subpath "/Users/<name>"))`, the toolchain allows, the user
grants, and the denied-always list last. Use `literal` for single files and `subpath` for trees.
Resolve symlinks before writing the profile; Seatbelt matches on the real path and Homebrew is
symlinks all the way down. Enable denial reporting so every blocked path is logged. Codex ships a
working profile to start from.

**Linux (bubblewrap).** `--ro-bind` the system directories, `--tmpfs $HOME` to close the home,
`--ro-bind` each toolchain and granted directory back in, `--bind` the workspace,
`--unshare-net --unshare-pid --new-session --die-with-parent`, and `--proc /proc` for a fresh
procfs limited to the sandbox's pid namespace. Layer seccomp for syscall filtering; Landlock
optionally as a second guard on paths. Detect missing unprivileged user namespaces at startup and
refuse rather than run unsandboxed.

### Shared libraries and runtime files

Do not attempt to compute per-binary dependency lists. The zone model covers nearly all shared
libraries (`/usr/lib`, `/System/Library`, `/opt/homebrew/lib`, `/nix/store`) and the tools that
matter most (`kubectl`, `helm`, `kustomize`) are static Go binaries. The cases that break are
interpreter-based tools reading files at runtime: a `kubectl` plugin in Python, and tools
installed through `pyenv`/`nvm` reading their standard library from inside the version-manager
tree. The toolchain allowlist exists for these. The remaining long tail is handled by denial logging at runtime (see
User experience), which catches libraries, data files and plugin directories in one mechanism and
keeps working when a tool updates.

If a diagnostic panel wants static dependency information: `otool -L` and `otool -l` (for
`LC_RPATH`) on macOS, `readelf -d` or `lddtree` on Linux. Never `ldd`; it can execute code from
the binary's loader. Resolve symlinks before comparing against the profile.

## PATH resolution and tool discovery

The sandbox inherits the user's PATH, but resolves it from their login shell, filters it, freezes
it per session, and re-syncs it only at app launch with review.

### Resolution

A GUI app launched from Finder or the dock receives a bare PATH (`/usr/bin:/bin:/usr/sbin:/sbin`),
not the user's shell PATH, so `kubectl` from Homebrew or an `asdf` shim is absent. The user's real
PATH exists only after their shell rc files run. Resolve it the way VS Code does: spawn the user's
login shell once, outside the agent's sandbox, and read the result.

- Use the user's actual login shell from the account record (`dscl . -read /Users/<name>
  UserShell` on macOS, `/etc/passwd` on Linux), not whatever `$SHELL` the app process inherited.
- Run it as an interactive login shell (`-ilc`), because many users set PATH in `.zshrc`
  (interactive) rather than `.zprofile` (login).
- Non-POSIX shells: `fish -lc 'string join : $PATH'`; nushell `nu -l -c '$env.PATH | str join ":"'`.
- Wrap the output in sentinels; rc files print things.
- Run with a scrubbed environment of its own and a timeout of a few seconds; on timeout or
  failure fall back to the stored list rather than failing the app.
- Take only PATH from the output. Discard the rest of that environment; it contains
  `AWS_PROFILE`, `GITHUB_TOKEN`, `SSH_AUTH_SOCK` and whatever else lives in the rc files. Shell
  functions and aliases are never inherited; the agent's `kubectl` is the binary, not the user's
  `alias k='kubectl --context=prod'`.

### Filtering

Drop before writing entries into the sandbox profile:

- Empty entries, `.`, and relative paths (they resolve to the workspace, which is writable).
- Directories that do not exist.
- Directories writable by other users, sticky bit or not (a persistence vector for anything else
  on the machine). A directory writable by a group that is not an administrators' one waits for
  the user rather than being dropped: only the user knows who else is in the group. An
  administrators' group (`admin`, `wheel`, gid 0) adds no writer, since its members can write
  anywhere through `sudo`, so Homebrew's prefix is adopted on a default Mac.
- Anything under the denied-always list, such as `~/.docker/bin`, `~/.kube/bin`, `~/.aws/bin`.
- Project-local entries such as `node_modules/.bin`: a project folder is not user binaries
  (step 3A).

Keep the shell's order; shim-based version managers depend on their entry preceding the system one.

### Freezing and syncing

Store the resolved list in settings and display it; this is the one sandbox component users will
recognize. Use the same list for the read-allow rules and for the `PATH` variable inside the
sandbox; if the two drift, binaries are on the path but unreadable.

A run reads the list once, when it starts, and keeps it for its life. Only the user's Include,
Remove and Refresh PATH change the list, through the sidecar's socket, which no sandboxed command
reaches, so a session cannot trigger its own expansion. A change applies from the next run.

At each app launch, re-resolve and diff against the stored list:

- Entries that disappeared: drop.
- New entries under already-open system prefixes (`/opt/homebrew/bin`, `/usr/local/bin`,
  `/nix/...`): adopt automatically; they add no readable surface.
- New entries under `~` or another closed zone: adopt only after a one-click confirmation using
  the same grant UI as denied paths ("your shell added `~/.local/share/mise/shims` to PATH;
  include it?").
- Order changes: adopt silently, but if the resolved location of `kubectl` or `helm`
  changed as a result, say so once.

Provide a "Refresh PATH" action in settings for users who install a tool mid-session.

### Dependency probing

Probe only a curated list of known tools, plus tools the user registers by hand. Do not walk the
PATH: a typical PATH resolves to thousands of executables, nearly all in open zones, and running
arbitrary binaries found on a user's machine is unacceptable. Static analysis of all of them
produces a long, mostly false-positive report nobody reads and still misses runtime file reads.

The curated list is the set the agent will actually call: `kubectl`, `helm`, `kustomize`,
`git`, `jq`, `yq`. For each, the app knows a safe invocation (`kubectl version --client`, `helm
version`, `git --version`). A kubeconfig's `exec` credential plugins (`aws`, `gke-gcloud-auth-plugin`,
`kubelogin`) run in the sidecar, outside the sandbox, and are not probed.

At onboarding and after each PATH refresh, run each probe inside the real sandbox with denial
reporting on, collect denied paths, and present them with a one-click grant ("`kustomize` needs
to read `/opt/kustomize`; allow?"). Resolve shims to real binaries first;
`~/.asdf/shims/kubectl` is a shell script and the thing to check is what it execs. Report which
binary each tool resolved to, so a user with several `kubectl` installs knows which one the agent
uses. If `kubectl` is absent, say so at onboarding rather than letting the first chat fail.

Let users register additional tools in settings with an invocation (default `--version`); teams
with in-house CLIs will want it.

## The proxy

The proxy is one host process, outside every sandbox. It has two faces on one socket: the
**cluster proxy**, which holds the cluster's credentials, injects them into requests from
sandboxed tools, and applies the permission decision for each call; and the **egress proxy**,
which relays a listed host and injects nothing. The sandbox never contains a real credential,
and the cluster is the one upstream a command reaches with one.

### Session tokens

Each session receives a random token at creation. The proxy maps the token to `{agent kind,
identity, approval policy, host allowlist, secret redaction flag}`. Tokens are in-memory only,
invalidated when the run ends (decision 4), and never written to disk outside the run's
generated kubeconfig, which only that run can read. A request without a valid token is rejected. Subagents spawned by a session receive
a token whose policy is at most as permissive as the parent's.

### Tool redirection

`kubectl`, `helm` and `kustomize` are pointed at the cluster proxy by a generated kubeconfig in
the workspace whose `server` for every context is the proxy and whose auth is the session token;
`KUBECONFIG` points at it. The proxy maps the context name to the real cluster and credentials.
Users with an existing `~/.kube/config` need zero setup. Everything else (`curl`, `git` over
HTTPS, `helm repo update`) reaches the egress proxy through `HTTPS_PROXY`, and a tool that
ignores the variable reaches nothing, since the sandbox has no other network.

Because every cluster call passes through the proxy, the proxy is also where the read/write
classifier lives, and every connection passes it, so the host allowlist lives there too (see
Permissions). An implementer must not add any tool a path around the proxy "for performance";
the moment a tool talks to a cluster directly, that tool is outside both credential isolation and
permission enforcement.

### The credential is the kubeconfig's

The proxy reads the real kubeconfig files (`~/.kube/config` and `KUBECONFIG` entries) and runs
any `exec` credential plugin itself, on the host, the way `kubectl` would. That delegates every
edge case (an SSO session, an enterprise identity provider, a cloud CLI's token cache) to the
plugin that already handles it. The proxy never parses a cloud credentials file and never stores
a long-lived secret.

The proxy must not copy anything from a kubeconfig into the app's own store, even encrypted. That
would make the app a second place secrets live and a second thing to revoke.

### What the user sees

At onboarding, the folders the sandbox finds programs in and which `kubectl` it runs. Nothing to
sign into: the cluster works because the kubeconfig does. From then on their tools work in the
sandbox immediately and the proxy is invisible unless they look.

## Permissions

Permissions are enforced in two places: the proxy for cluster writes and new hosts, and the
app's tool layer for bash and filesystem. The model is never a policy enforcement point.

### Action classes

Every agent action falls into one class. Classes are ordered by risk; approval modes are defined
against them.

| Class | Examples | Where enforced |
| --- | --- | --- |
| 1. Read inside the sandbox | `ls`, `cat`, `grep`, `kubectl get` | Sandbox (nothing to enforce) |
| 2. Write inside workspace or granted paths | Writing a manifest to the workspace, editing a granted repo | Sandbox |
| 3. Reach a new network host | `curl https://example.com`, a chart from an unlisted registry | Proxy host allowlist |
| 4. Upstream write | Kubernetes `POST`/`PUT`/`PATCH`/`DELETE` | Proxy classifier |
| 5. Destructive or high blast-radius write | Delete namespace, delete PV/PVC, scale to zero, RBAC changes | Proxy classifier with a curated list |
| 6. Read Kubernetes Secret data | `kubectl get secret -o yaml`, the `secrets` resource in any read | Proxy: `data` fields redacted unless permitted |

Class 6 deserves its own entry because it is a read that carries write-like risk. The proxy
redacts `data` and `stringData` in Secret responses by default and treats reading them as a
permissioned action. The monitoring session must never receive Secret data.

### Classifiers

**Kubernetes.** Classify by HTTP verb: `GET`, `LIST` and `WATCH` are reads; `POST`, `PUT`,
`PATCH`, `DELETE` and `DELETECOLLECTION` are writes. Subresources are the exception an implementer
will miss: `exec`, `attach`, `portforward`, `proxy` and `ephemeralcontainers` are writes
regardless of verb, since they run code in or against the cluster. Dry-run requests
(`?dryRun=All`) are reads. Class 5 is a curated list keyed on resource kind and verb, and on
context name patterns.

**The network.** A host is listed or it is not; there is nothing to classify in a tunnel, and no
credential rides through one. A listed host is class 1, an unlisted one class 3.

**Bash.** Bash inside the sandbox is far less dangerous than it sounds: it can only affect the
workspace, granted paths and upstream reads. Everything else is caught by the sandbox or the
proxy. So in the default mode bash runs without a prompt and the prompt fires when the proxy sees
a write. Keep a per-command prompt as an option for cautious users, and keep a command-pattern
allowlist as UX only. Do not attempt to make a bash parser a security boundary; Claude Code's
experience is that it is heuristic at best.

### Approval modes

Two orthogonal user controls, as in Codex:

- **Sandbox profile**: what is physically possible. Rarely changed; changed when the user grants
  directories.
- **Approval mode**: which classes prompt.

| Mode | Class 1-2 | Class 3 | Class 4 | Class 5 | Class 6 |
| --- | --- | --- | --- | --- | --- |
| Read-only | Allow | Deny | Deny silently | Deny silently | Prompt |
| Ask (default) | Allow | Prompt | Prompt | Prompt | Prompt |
| Trusted scopes | Allow | Prompt | Allow in listed contexts and namespaces, prompt elsewhere | Prompt | Prompt |
| Auto | Allow | Allow | Allow | Prompt | Allow |

Kubetail does not guess which contexts are production: the user sets a production context's mode
to Read-only (decision 25). Class 5
always prompts, even in Auto; there is no mode that removes it, and Read-only refuses it. The
engine has three modes: *Trusted scopes* is Ask with Allow rules scoped to the trusted contexts
and namespaces, which the prompt's *Allow for this chat* and *Always allow* answers write, so
the table's row describes an effect rather than a fourth setting (decision 11). Auto is the one
mode that shows Secret values without asking, and the Settings picker says so.

### Grant rules

Grants are rules of the form `class : resource scope : duration`, in the style of Claude Code's
allow and deny lists. Examples: `k8s:write context=dev-eks namespace=team-a` for this session;
`net:host registry.example.com` always; `k8s:secret-read context=dev-*` for this session. Scopes
for Kubernetes are context and namespace; for the network, a host; for a path, a folder.
Durations are once, this session, or always.

Deny rules always win over allow rules, and users can add them. A cluster-scoped RBAC change is
on the class 5 list, so it asks in every mode. What a deny rule refuses is done from a chat
switched outside the sandbox, where the user approves the command itself.

### Prompt UX

Prompt with the classified action, not the shell string: "Delete pod `api-7f9c` in `team-a` on
`dev-eks`", and for `apply` or `patch` a diff of the change. The proxy has the request body, so
the diff is available: it reads the object, dry-runs the request against the API server, and
diffs the two, so the user sees what the cluster will do rather than what the body says. Each prompt offers: approve once, allow for this chat (scoped to the
resource scope shown), always allow this scope, deny. The "always" option writes a grant rule the
user can see and remove in settings. The cluster surface stays `kubectl` in Bash and KubeQuery
over the mirror; the proxy's classifier is what makes a raw command's prompt legible.

## Sessions and multiple agents

A session is the unit of isolation: `{sandbox profile instance, workspace, proxy token, approval
policy, host allowlist, granted paths}`. The chat agent and the monitoring agent share the same
sandbox design and the same proxy process, and run in separate session instances.

### Why the same design

One Seatbelt or bubblewrap template, one allow-listed environment, one host view, one proxy, one
classifier. A second kind of sandbox for the monitoring agent would be a second security boundary
to audit and keep in sync, and the less-tested one. Do not give monitoring a weaker sandbox on
the theory that it is read-only; it reads untrusted content continuously and unattended and is
the more likely injection target.

### Why separate instances

- Different privilege: sharing a workspace lets a chat session write a file the monitoring
  session reads and acts on, or the reverse, so an injection in one reaches the other. Separate
  workspaces make each session's blast radius exactly what its token says.
- Different lifetimes: monitoring runs while the app is open and accumulates state (baselines,
  last-seen events); a chat session lives with a conversation.
- Different grants: a user granting `~/code/my-service` means the conversation they are in, not
  the background process. Path grants attach to the chat session by default; extending them to
  monitoring is a separate, explicit switch.
- Different network shape: monitoring needs only the cluster API servers. Its allowlist is
  tighter, and it must never raise a "reach new host?" prompt because nobody is there to answer.

### Session definitions

| Session | Workspace | Token policy | Approval | Path grants | Network |
| --- | --- | --- | --- | --- | --- |
| Chat | the chat's `workspace/` | Reads free; writes classified and gated | User's chosen mode | Per chat or always | Full allowlist plus prompts for new hosts |
| Monitoring | `monitor/<cluster>/workspace/`, one per watched cluster | Read-only at the proxy; Secret data redacted; writes rejected | Never prompts | None by default | Known API servers only, no prompts |
| Subagent of chat | the chat's `workspace/`, shared with its parent | At most as permissive as the parent | Inherits parent | Inherits parent | Inherits parent |

### Remediation from monitoring

The monitoring agent may run bash freely; its bash can only read. When it decides an action is
warranted, it does not act. It emits a proposal card into chat ("restart deployment `api` in
`team-a`; pods are crash-looping on OOM") and clicking "do it" runs the action in a chat session
under the normal permission flow. This keeps the read-only guarantee absolute and keeps a human
on every write.

### Handoffs

Handoffs between sessions go through the app, not through shared files. Monitoring writes findings
into its own workspace; the app reads them and surfaces them. A chat session that wants a
monitoring finding receives it as a message, not as a path into another workspace.

## User experience

The security model is only acceptable if the user rarely notices it. Three mechanisms carry most
of that: a zero-click onboarding, denials surfaced in context with a one-click grant, and settings
that show the sandbox in the user's own terms.

### Onboarding

1. Resolve PATH from the login shell; filter; show the list.
2. Probe the curated tools inside the sandbox; report which binary each resolved to and any
   denied paths, each with a grant button. Say plainly if `kubectl` is missing.
3. Set the approval mode: Ask by default, and Read-only for any context the user names as
   production.

No step requires typing a secret or signing in: the cluster works because the kubeconfig does.

### Denials in context

The biggest usability failure of sandboxes is a command that dies with a cryptic "permission
denied" from three layers down. Every denied path, host and upstream write is found per command
(on macOS from Seatbelt's reports in the system log, each deny rule tagged with the run's id so
the sidecar can read its own from a `log stream`; on Linux, where no unprivileged process can
read the kernel's log, from the command's error output checked against the policy; the proxy's
own records for hosts, writes and Secret reads). The chat shows the denial in plain terms with
the action that resolves it: "the command tried to read `~/code/foo` and was blocked; grant
access?", "the agent wants to reach `charts.example.com`; allow?". This turns the closed home
directory from an annoyance into the way users discover the permission system.

Grants offered from a denial carry the same once / session / always choices as prompts, and
"always" grants are visible and removable in settings.

### Settings

- Sandbox: the resolved PATH list with a refresh action; registered extra tools; path grants with
  read or read-write; the denied-always list shown read-only so users understand why `~/.ssh` is
  unreachable.
- Permissions: approval mode per context; grant rules and deny rules as a list the user can edit.
- Network: the host allowlist with sources (from kubeconfig, added by user), and per-session vs
  always entries.
- Monitoring: which contexts it watches, and whether it shares any path grants.

Everything in settings speaks in the user's terms (contexts, namespaces, profiles, folders), never
in Seatbelt rules or proxy internals.

## Implementation order, invariants and open questions

Build the cluster proxy first, because it delivers both credential isolation and enforceable
permissions; the sandbox second; the egress proxy and the permissions UI after.

### Order

The order the design was written with. The agent-security sequence built it in steps of its own,
given what had already landed when the design was adopted.

1. Credential proxy with kubeconfig rewriting, the Kubernetes read/write classifier, session
   tokens, and borrowing of existing kubeconfig credentials including `exec` plugins. This alone
   gives working `kubectl` and `helm` with gated writes.
2. Sandbox on macOS (Seatbelt), starting from Codex's or Claude Code's published profile: zones,
   environment construction, network deny, denial reporting. Then PATH resolution and the curated
   probe.
3. Permissions UI: approval modes, grant and deny rules, prompt cards with diffs,
   denial-in-context grants.
4. Monitoring session with a read-only token, Secret redaction, and the proposal-card handoff.
5. Linux sandbox (bubblewrap, seccomp).
6. The egress proxy: the host allowlist and its prompts.

### Invariants that must have tests

- [ ] No file under the denied-always list is readable from the sandbox, including when the user
      has granted `~` or a parent directory.
- [ ] No process in the sandbox can open an outbound socket to a non-loopback address.
- [ ] The sandbox environment contains none of: `SSH_AUTH_SOCK`, `GITHUB_TOKEN`, `GH_TOKEN`,
      `GITHUB_ENTERPRISE_TOKEN`, `GH_ENTERPRISE_TOKEN`, `AWS_PROFILE`, real `AWS_*` credentials, `DOCKER_HOST`, `DYLD_*`, `LD_LIBRARY_PATH`,
      `LD_PRELOAD`.
- [ ] The generated kubeconfig contains no certificate data, token or exec plugin from the real
      kubeconfig.
- [ ] A Kubernetes `PATCH`, `POST`, `PUT` or `DELETE`, and any `exec`, `attach`, `portforward` or
      `proxy` subresource request, from a monitoring token is rejected by the proxy.
- [ ] Secret `data` and `stringData` are redacted for any token without the secret-read grant.
- [ ] A request with an unknown or expired session token is rejected.
- [ ] Class 5 prompts in Auto mode.
- [ ] Path grants attached to a chat session do not appear in the monitoring session's profile.
- [ ] If the sandbox cannot be established, every command asks the user before it runs and the
      user is told why; no command runs unconfined and unasked.
- [ ] Nothing from a kubeconfig or an `exec` plugin's answer is written into the app's data
      directory.

### Open questions, and where they were answered

- Whether project-local PATH entries (`node_modules/.bin`) count as user binaries. **Answered by
  step 3A**: they do not, and are dropped.
- Persistent per-cluster caches versus per-session workspaces. **Answered by step 2A**: one tool
  cache per cluster under Kubetail's cache directory, which every run on that cluster writes.
- Per-command sandboxing versus a persistent sandboxed shell for latency. **Per command stays**;
  measure if latency becomes a complaint.
- Kubernetes `WATCH` and `kubectl logs -f` through the proxy. **Landed**: the proxy streams as
  bytes arrive and never buffers a response.

## Where this meets the code

Adopted on 2026-09-30, over a sandbox already built, and landed in full on 2026-10-06. What is
true of the code is in the `CLAUDE.md` files; this section says how the design maps onto it and
where the build departs from the note's words, each departure a decision of its own. The step
numbers (`3B`) name the sequence's steps, and the ADRs and security records cite them.

**Already landed when the design was adopted**, by the sandboxed Bash work: the Seatbelt and
bubblewrap sandboxes with a closed home, a built environment and no network; the cluster proxy
(`kubeproxy`) with the Kubernetes read/write classifier, a per-run token carried as proxy
credentials, a generated kubeconfig, Secret redaction and the `exec` plugin run in the sidecar;
each cluster write put to the user as the request it is; and the chat's workspace. Of the order
above, that is most of 1, 2 and 6.

**Decisions taken when the design was adopted:**

1. **A command outside the sandbox is the user's choice, never the model's.** The model's
   `dangerouslyDisableSandbox` flag goes. A chat the user switches to run outside the sandbox runs
   every command there, each one asking, as the bash tool record has it. The default is the
   sandbox, and no chain of approvals leaves it.
2. **Session grants and "always" rules supersede the bash tool record's rule** ("no setting, no
   allowlist, no read exempt") for actions the proxy classifies. A raw command outside the
   sandbox still asks every time.
3. **The monitoring session's plumbing is built ahead of a monitoring agent** (step 6B): a run of
   the chat service under its cluster and no chat (`chat.RunMonitor`), its session, its record,
   its folder and its teardown, called by its tests alone until an agent exists. The proposal card
   goes to the agent's step.
4. **One token per run, mapped to its session.** A run's token dies with the run, as it does
   today, which is stricter than one per session and costs nothing; the session is what the token
   maps to. The proxy reads the session's policy through the run's grant.
5. **A path grant lasts for a chat or always.** Kubetail has no project; the chat is the session.
6. **The login shell runs in a read-only sandbox**, not merely outside the agent's, so a startup
   file cannot reach Kubetail's data or the network while it runs.
7. **`kubectl port-forward` stays refused.** The proxy refuses every upgrade, so loopback need not
   stay open for it. A later step may classify it as the class-4 write the note names.
8. **Nothing reports a denied path** (steps 5B and 6A). A chat's failed command offers a grant
   the user types, and its model reads the tool's own error; the executable probe shows each
   executable's own error, and a folder is granted in Settings. A report per denied path stays
   possible on both platforms (`docs/TODO.md`).
9. **A subagent shares its parent's workspace.** Its privilege is its parent's, so a folder of
   its own would separate nothing, and its `Write` and `Edit` are drawn open under the chat as
   the parent's are. The chat and the monitor keep separate workspaces, as the note says.
10. **Native Windows has no sandbox** ([ADR](../adr/2026-09-28-native-windows-has-no-sandbox.md)),
   so every command there runs outside it and asks, and none of the proxy's credential isolation
   holds. The design is for macOS and Linux, WSL2 included.

**Decisions the specs added**, each argued in the spec named:

11. **Three approval modes, not four** (step 3B). *Trusted scopes* is Ask with Allow rules scoped
    to the trusted contexts and namespaces, which is what the prompt's "for this chat" and
    "always" answers write. The read-only mode's refusal comes before every rule that asks, so a
    rule that says "always ask" cannot turn a refusal into a prompt.
12. **`/etc` is readable whole, with its secret files on the denied-always list** (step 2A),
    since the loader and libc read files the note's short list cannot name.
13. **Network is the user's switch, not a host allowlist** (step 4C,
    [ADR](../adr/2026-10-04-network-is-the-users-switch.md)). A sandboxed command has no network
    unless the user turned it on for the chat, for one message, or for one command they approved;
    on, it reaches the internet with nothing filtered by host but the machine's loopback. There is
    no egress proxy.
14. **A folder grant is read-only over the home, and a read-write grant may not hold a
    denied-always path** (step 4D), because a command that can write a folder can rename what
    is under it out from under a Seatbelt rule.
15. **The prompt's diff is a server dry run of the exact bytes the request holds, and Approve
    waits on the diff** (step 4B), with the raw request one fold away.
16. **Memory on macOS is bounded by the wall clock alone, and the process limit is a margin
    over what the kernel already counts against the run** (step 2B): nothing in a Linux user
    namespace of the run's own, the user's whole count elsewhere. macOS checks `RLIMIT_AS`, but
    every process already maps past any limit that would stop a leak, so none can be set; and
    a cgroup is not delegated to a desktop app. The CPU limit is the longest timeout and its
    kill grace, times the CPU count, so it binds only a process that outlives the clock, and on
    macOS only one that does not ignore `SIGXCPU`.
17. **The monitor is refused by its mode and its no-prompts policy, never by its kind** (step
    6B), so a proxy that forgot about monitors would still refuse it. It has an asker
    (`monitorAsker`), which records each refusal on its run and never asks, so a hijacked monitor
    leaves a trail; its proposal is text a chat runs under the normal flow.
18. **macOS refuses the setuid programs that exist to escalate, not every setuid program**
    (step 2B): `sudo`, `su`, `login` and `security_authtrampoline`. Seatbelt's `file-mode` rule
    also refuses setgid files, and no later rule wins `ps` and `top` back, which macOS installs
    setuid root and commands need. Every setuid program still runs inside the profile, and on
    Linux `no_new_privs` refuses them all.
19. **`~/Documents`, `~/Desktop` and `~/Downloads` are `Closed`, not denied always** (step 2A).
    A Files Deny keeps them shut under a grant of `~`, and lets a grant of a folder inside one,
    such as a project under `~/Documents`, open it.
20. **The tool home is per chat, and only the kubectl cache is per cluster** (step 2A). A shared
    config or build cache lets one chat plant what a later chat runs.
21. **`LANG` is the sidecar's or a platform default, and `LC_*` does not pass** (step 2A).
22. **A `PATH` entry no list covers finds nothing until the user includes it** (step 3A). An
    entry a list already opens is adopted unasked; any other waits in Settings.
23. **asdf's global versions ride as `ASDF_<TOOL>_VERSION`** (step 2A), read from the user's
    `~/.tool-versions` on the host, since `HOME` is the workspace.
24. **The environment holds more than the pass-through list** (step 2A): `PWD`, `ZDOTDIR`,
    `KUBECACHEDIR`, the `KUBETAIL` variables, the tool home's variables, each toolchain location's
    and `ASDF_<TOOL>_VERSION`, each built by Kubetail and none copied from the sidecar's.
25. **No production default, and a pattern's `*` crosses `/`** (step 3B). A guess from the
    context's name (`*prod*`) catches `nonprod` and `dev-products` and misses a production
    cluster named otherwise, so every context starts at the default mode and the user sets a
    production one's. A way to flag a context as production waits until users ask for one. Cloud
    tools name a context after the account first (`arn:aws:eks:…:cluster/prod-eu`,
    `gke_project_zone_prod`), which a `*` that stops at `/` cannot reach.
26. **A cluster-write rule covers destructive writes** (step 3B): a `Deny` of class 4 refuses a
    class 5 write in its scope, and no `Allow` reaches class 5. Cluster RBAC is on the class 5
    list, so it asks with no rule.
27. **A security setting Kubetail cannot read refuses rather than allows** (step 3B): a bad mode
    is read-only, and a bad rule refuses every cluster write until the file is fixed.
