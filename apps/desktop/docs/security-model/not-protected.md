# What is not protected

[← Security model](../security-model.md)

Every row of the model that no test enforces, and every residual an enforced row states.
`tools/securitymodel` writes this file from the subject files: edit a row there, then run
`go run ./securitymodel write` from `tools/`.

## Cloud platforms and GitHub

- [CLOUD-1](cloud-platforms.md#cloud-1), By decision. Every cloud platform whose login is on the machine is reached by every chat's sandboxed commands unless the user turned its switch off. The switch is a `sandbox.<platform>.enabled` row of `app.db`'s `settings`.
- [CLOUD-2](cloud-platforms.md#cloud-2), By decision. Cloud platform switches that cannot be read are off.
- [CLOUD-3](cloud-platforms.md#cloud-3), By decision. A background command reads a cloud platform's account after its turn ends, until it ends or the user turns the switch off.
- [CLOUD-4](cloud-platforms.md#cloud-4), By decision. A cloud platform's metadata is read unasked, so a secret a user put where no cloud platform's list names it reaches the model.
- [CLOUD-5](cloud-platforms.md#cloud-5), By decision. A cloud platform's request is approved on its method, host, path, media type and body. The summary and details are a reading of it, and an `Unshown` body is approved on the cloud platform's sentence for it.
- [CLOUD-18](cloud-platforms.md#cloud-18), By decision. With GitHub on, a sandboxed command reads a repository's content unasked, private ones included. There is no repository fence: the token's reach is the proxy's.
- [CLOUD-19](cloud-platforms.md#cloud-19), By decision. A GitHub push is approved on its repository, refs and options. Its pack is not shown.
- [CLOUD-20](cloud-platforms.md#cloud-20), By decision. A GitHub answer's body can hold a signed link the lists do not name, such as a private file's `download_url`.
- [CLOUD-21](cloud-platforms.md#cloud-21), By decision. GitHub's `credentialPaths` and `dataPaths` are kept by hand, and GraphQL is classified by Kubetail's parser. A route or field GitHub adds that answers a credential or a secret is a read until it joins one.
- [CLOUD-22](cloud-platforms.md#cloud-22), By decision. `HTTP_PROXY` hands GitHub's grant token to every Go and Python program in a run with GitHub on.
- [CLOUD-23](cloud-platforms.md#cloud-23), By decision. A sandboxed command's GitHub reads spend the user's rate limit.
- [CLOUD-24](cloud-platforms.md#cloud-24), By decision. A redirect on GitHub's own origin comes back through the proxy as a new request, so an approved request GitHub redirects there asks again.
- [CLOUD-25](cloud-platforms.md#cloud-25), By decision. `gh` on the sidecar's `PATH` runs with the user's authority to read their token, unless the GitHub connector is connected.
- [CLOUD-35](cloud-platforms.md#cloud-35), By decision. A sandboxed command reads what the user's Google account reads on six services, unasked, log entries included.
- [CLOUD-36](cloud-platforms.md#cloud-36), By decision. A secret in a Google field no list names, such as a label or a description, is read unasked. A `POST` that reads and is on no list asks.
- [CLOUD-37](cloud-platforms.md#cloud-37), By decision. `add-metadata` and `remove-metadata` do not work in the sandbox: a change carrying a blanked value is refused.
- [CLOUD-38](cloud-platforms.md#cloud-38), By decision. An account or project switched in a terminal reaches the sandbox only when the held token expires, up to an hour. With no login, `gcloud` runs again every 30 s while commands run.
- [CLOUD-39](cloud-platforms.md#cloud-39), By decision. `gcloud` keeps its logs and configuration under the chat's tool home.
- [CLOUD-45](cloud-platforms.md#cloud-45), By decision. The sidecar runs the user's `az` from `PATH`, outside the sandbox, with fixed arguments, and holds the user's Resource Manager tokens in memory until five minutes before they expire. Residual: Whoever controls `PATH` controls what runs, as with a kubeconfig's `exec` plugin.
- [CLOUD-46](cloud-platforms.md#cloud-46), By decision. A run with Azure trusts a certificate Kubetail made through `REQUESTS_CA_BUNDLE`. With network on, every tool that reads it verifies public hosts against the system's bundle in place of its own.
- [CLOUD-47](cloud-platforms.md#cloud-47), By decision. On macOS a run's `ca.pem` carries `/etc/ssl/cert.pem`, which holds no root added to the Keychain alone. With network on, a tool behind a TLS-inspecting proxy whose root lives only there fails to verify public hosts.
- [CLOUD-48](cloud-platforms.md#cloud-48), By decision. Every Azure SDK in a run with Azure believes it has a managed identity. With network on, one that calls the real Resource Manager sends it the grant's token.
- [CLOUD-49](cloud-platforms.md#cloud-49), By decision. An Azure `POST` that only reads asks as a change. One that answers a secret under a name the credential and list/get rules miss is a write off `ordinary`, asked each time and refused to the monitor.
- [CLOUD-50](cloud-platforms.md#cloud-50), By decision. An Azure change that runs code on a machine (a VM extension, a deployment script) is a write off `ordinary`, which asks each time, and rests on the user's eye.
- [CLOUD-51](cloud-platforms.md#cloud-51), By decision. A long `az … create` asks once per request.
- [CLOUD-60](cloud-platforms.md#cloud-60), By decision. A role or identity the `exec` block names otherwise is not read, and such a run uses the default chain.
- [CLOUD-61](cloud-platforms.md#cloud-61), By decision. Resolving the user's AWS profile can run its `credential_process` outside the sandbox, with no stdin and no stderr. A role assumed from a source profile's process is no login.
- [CLOUD-62](cloud-platforms.md#cloud-62), By decision. An AWS read is unasked and the service table is hand-written. Metadata no list names (a tag, a CloudFront origin's header, a log event) reaches the model, a parameter no row names can reach another account's log, and a credential-answering read AWS adds is unasked until a row names it.
- [CLOUD-63](cloud-platforms.md#cloud-63), By decision. No file is uploaded to S3 from the sandbox, and a login that cannot list its buckets is asked for every bucket read.
- [CLOUD-71](cloud-platforms.md#cloud-71), By decision. Opening a GitHub pull request keeps GitHub's default letting the base's maintainers push to its branch, and runs unasked under `auto` or an allow. Changing that afterward is an access control change. Residual: The base's maintainers may push to a fork's branch the user opened the pull request from.
- [CLOUD-69](cloud-platforms.md#cloud-69), residual. A login with no account id sends no owner condition, so a bucket that changes hands within the list's minute is read unasked.

## Cluster data and the caches

- [DATA-6](cluster-data.md#data-6), By decision. Cache contents are left unencrypted, protected by file mode/ACL. Disk encryption is an operator responsibility.
- [DATA-7](cluster-data.md#data-7), By decision. `app.db` holds chat text in the clear, protected by the same file mode. Sign-out does not clear it. Residual: A user can paste anything from a cluster into a chat, and nothing deletes old chats.
- [DATA-8](cluster-data.md#data-8), By decision. `app.db` holds the name of every kube-context the importer has seen, in the `clusters` table. A context that left the file keeps its row.
- [DATA-9](cluster-data.md#data-9), Built. A cache has a size ceiling: the janitor judges each sweep and publishes the edge. Residual: Soft by one sweep interval.
- [DATA-10](cluster-data.md#data-10), Built. The cache pass stops the sync of a cache over its size ceiling and says why. Residual: Soft by one sweep interval, and a stopped cache stays stopped until the user clears it.
- [DATA-11](cluster-data.md#data-11), By decision. Cached Kubernetes events have no retention beyond the relist's prune. The size ceiling (DATA-9, DATA-10) is what bounds them.
- [DATA-12](cluster-data.md#data-12), Not built. A retention policy, so a cache stops outliving the user's interest in its cluster.
- [DATA-13](cluster-data.md#data-13), Not built. A gesture before a kubeconfig `exec` plugin runs. Residual: Every imported context is dialled, so a kubeconfig write runs the plugin it names.
- [DATA-14](cluster-data.md#data-14), Not built. Query cost, concurrency, fan-out and total-disk budgets. Residual: The outer request-size and timeout bounds are not budgets on the work an operation causes.

## The cluster proxy and permissions

- [KUBE-20](cluster-proxy.md#kube-20), residual. `managedFields` is dropped from the diff.
- [KUBE-25](cluster-proxy.md#kube-25), residual. Its body reaches the cluster's admission webhooks.
- [KUBE-51](cluster-proxy.md#kube-51), By decision. Cluster reads leave the machine on the model's word. Everything the user's credentials can read, less Secret values the user has not allowed showing, can reach the provider with no per-call approval.
- [KUBE-52](cluster-proxy.md#kube-52), By decision. A helm release passes redacted inside. Its chart, default values, rendered notes and other objects' manifests pass, and so does a Secret typed `helm.sh/release.v1` by hand, all but its `config` and its manifests' Secrets, unless the user allowed showing them. A release write runs once its command read no Secret data redacted in its namespace. Residual: An audit of what else carries secrets is in TODO.

## Commands and the approval request

- [CMD-10](commands.md#cmd-10), residual. A look-alike letter from another script is not made visible. That is the eye's.
- [CMD-13](commands.md#cmd-13), residual. A user who reads the description and not the command approves what the command does. That is the eye's.
- [CMD-32](commands.md#cmd-32), residual. By decision, the request does not show the bound. Cancel stops the command at once.
- [CMD-33](commands.md#cmd-33), By decision. An approved command runs as the user with the environment Kubetail runs with, and reaches what it reaches. The user's eye is the bound.
- [CMD-34](commands.md#cmd-34), By decision. A command starts in the directory its approval request shows: the call's `workdir`, else the chat's workspace.
- [CMD-35](commands.md#cmd-35), By decision. Resolving the directory is string work, so no path is touched before the user decides. A missing directory starts nothing.
- [CMD-36](commands.md#cmd-36), By decision. The directory check runs off the turn's goroutine, so a dead network mount cannot hold a cancel there. Residual: A mount that dies between the check and the start still holds the call until the filesystem answers.
- [CMD-37](commands.md#cmd-37), By decision. The process group is killed after every run. Residual: A descendant that left the group is outside the guarantee.
- [CMD-38](commands.md#cmd-38), By decision. A cancel kills the process group at once, with no grace, so shutdown's kill lands inside the host's budget whatever the command traps.
- [CMD-39](commands.md#cmd-39), By decision. On Windows the shell is Git for Windows' bash, found through Git's own install record and never off `PATH`. The command reaches it as a file, never on a Windows command line.
- [CMD-40](commands.md#cmd-40), By decision. On Windows the job object is the group. It is terminated at the same two moments, and nothing under its flags can spawn outside it. Residual: A process started through an out-of-job broker (a scheduled task, WMI, ShellExecute) is outside the guarantee.
- [CMD-41](commands.md#cmd-41), By decision. A command reaches the cluster as the user's own `kubectl` would. The prompts make the shell the last resort after a cluster tool and have every cluster command name the card's context. Residual: That is guidance and not a bound: a command without `--context` runs against the kubeconfig's current one.
- [CMD-42](commands.md#cmd-42), By decision. An approved command can read a workspace file it does not name. A `.git/config` that a sandboxed command or an unasked `Write` planted runs its hook under an approved `git status`, outside the sandbox, on every machine. Residual: The request shows the command and its directory, not what the directory holds. Closing it is in TODO.
- [CMD-45](commands.md#cmd-45), By decision. The command is recomputed by the build that reads it, so how a tool reads its input never changes for rows it has written. Changing what an input means is a new tool name.
- [CMD-46](commands.md#cmd-46), residual. By decision, a lying description names the call on the row until it is opened.
- [CMD-50](commands.md#cmd-50), By decision. Over the environment go `KUBETAIL`, `KUBETAIL_SIDECAR_PID` and `KUBETAIL_DESKTOP_PID`, and the snapshot's `PATH`. The host's variable is left out when the sidecar was not told it.
- [CMD-51](commands.md#cmd-51), By decision. The user's variables for commands outside the sandbox are exactly those the request showed. They are read when the request is made, kept on the call's row, drawn in the request and handed to the run, never read again. One replaces the process's own of its name and none replaces Kubetail's.
- [CMD-52](commands.md#cmd-52), By decision. No provider key is in a command's environment.
- [CMD-53](commands.md#cmd-53), By decision. A sandboxed call's environment is built rather than inherited, so no credential variable rides in and `PATH` is the run's frozen list.
- [CMD-54](commands.md#cmd-54), By decision. A sandboxed call's kubeconfig dials a free loopback port the forwarder takes before the command starts. A port taken in between fails the run.
- [CMD-55](commands.md#cmd-55), By decision. Any local process can reach that port. The proxy answers it 401 without the run's token, sent as proxy credentials, and the kubeconfig's `Host`.
- [CMD-56](commands.md#cmd-56), By decision. What passes the proxy is a read the policy allows. A path that is not canonical is refused.
- [CMD-57](commands.md#cmd-57), By decision. A read of `secrets` through the proxy passes with its values, `last-applied-configuration`, and a helm release's values and manifest Secrets rewritten as it streams. It fails closed on anything the rewriter cannot read. Residual: Still passing: a value copied out of a Secret (a ConfigMap, an env value), a release's chart defaults, rendered notes and other rendered objects, and a Secret typed `helm.sh/release.v1` by hand.
- [CMD-58](commands.md#cmd-58), By decision. The proxy's reads go with the user's own credentials, which the command never holds.
- [CMD-59](commands.md#cmd-59), By decision. The proxy's grant dies with the run.
- [CMD-60](commands.md#cmd-60), By decision. The proxy's server bounds headers and idle connections.
- [CMD-61](commands.md#cmd-61), By decision. A command outside the sandbox runs in the user's shell after a snapshot of their login profile: its functions, options, regular aliases and `PATH`. A command's meaning follows the user's own definitions, as in their terminal.
- [CMD-62](commands.md#cmd-62), By decision. The command is quoted whole into a fixed wrapper, so the approved text is still the command.
- [CMD-63](commands.md#cmd-63), By decision. The snapshot leaves out global and suffix aliases and the startup options, `monitor` among them. Nothing the snapshot replays takes a command's children out of its group.
- [CMD-64](commands.md#cmd-64), By decision. The profile's names cannot steer the snapshot's dump.
- [CMD-65](commands.md#cmd-65), By decision. The snapshot file is 0400 in a 0700 directory. Residual: A command the user approves can rewrite the snapshot and so every later one until the next start.
- [CMD-66](commands.md#cmd-66), By decision. The snapshot is taken after READY on a machine with no sandbox. On one with a sandbox it is taken once, the first time a command runs outside it, and never once the sidecar is stopping.
- [CMD-67](commands.md#cmd-67), By decision. The snapshot's shell dies with the sidecar, and no test's app takes a snapshot.
- [CMD-68](commands.md#cmd-68), By decision. A sandboxed command neither sources the snapshot nor waits for it.
- [CMD-69](commands.md#cmd-69), By decision. On a machine with a sandbox the snapshot's login shell runs in it. It reads the whole home but Kubetail's directories, writes only its scratch, reaches no network, and holds no variable the sandbox refuses.
- [CMD-70](commands.md#cmd-70), By decision. The `kill` and `pkill` shims that refuse Kubetail's processes are a courtesy, on Unix alone.
- [CMD-71](commands.md#cmd-71), By decision. What the login shell outputs shapes what runs outside the sandbox. The snapshot's functions, aliases and `PATH` run in every command outside it. On macOS the imported variables and `PATH` shape the sidecar's own `exec` credential plugins, so a startup file can postpone its harm there.
- [CMD-72](commands.md#cmd-72), By decision. On macOS the import can carry out a secret the resolution's shell read from a file the denied-always list does not name, in `HTTPS_PROXY` or `OLLAMA_HOST`. The denied-always list is shut to the resolution's shell. Residual: What the denied-always list leaves is the list's curated-list residual.
- [CMD-73](commands.md#cmd-73), By decision. A command outside the sandbox runs the files every `-c` shell reads, with full access, once per command: zsh's `/etc/zshenv` and `~/.zshenv`, and the file bash's `BASH_ENV` names.
- [CMD-76](commands.md#cmd-76), residual. A saved file keeps up to 8 MiB until its chat is deleted, so what redaction misses stays on disk longer.
- [CMD-79](commands.md#cmd-79), residual. A value with no form under a name not in the list, a short flag (`-p secret`), and any printed form interrupted before its closing delimiter, which can leak past its first token.
- [CMD-80](commands.md#cmd-80), By decision. Redaction is hygiene, not the boundary. The model chooses the command, and a command can re-encode its output past any rule (`… \| base64`). The approval request is the bound, and the rules cover the shapes real tools print.
- [CMD-81](commands.md#cmd-81), By decision. A background command is approved like any other, and its request says it outlives the answer.
- [CMD-82](commands.md#cmd-82), By decision. A background command runs until it exits, the model's `TaskStop` or the user's *Stop* ends it, its chat is deleted, or the app quits. A Cancel leaves it running. Residual: A crash on Linux or macOS can leave a command outside the sandbox running, read `lost`, where Windows' kill-on-close job ends it.
- [CMD-83](commands.md#cmd-83), By decision. The model's `TaskStop` reaches its own chat's commands alone.
- [CMD-84](commands.md#cmd-84), By decision. Only a background command's exit starts a turn, never at startup, with every check a send makes. Residual: A notice turn sends the chat to the provider with no new gesture, bounded by the approvals behind each exit.
- [CMD-85](commands.md#cmd-85), By decision. A notice is one escaped line.
- [CMD-86](commands.md#cmd-86), By decision. At most 4 background commands run at once in a chat and 16 in the app.
- [CMD-87](commands.md#cmd-87), By decision. A background command's output file is raw on disk and redacted when `Read` opens it.
- [CMD-88](commands.md#cmd-88), By decision. A sandboxed command ends with the sidecar however it dies, through the lifeline its forwarder watches. Residual: On macOS a process that left the run's group survives the lifeline too.

## Sign-in, connectors and stored credentials

- [CRED-12](credentials.md#cred-12), Built, not yet checked end to end. The OpenRouter connector's key stays in the keyring and memory. Residual: The source's status is "Built, not yet checked end to end": Enforced once the OpenRouter live checks in `TODO.md` pass.
- [CRED-13](credentials.md#cred-13), Built, not yet checked end to end. The OpenRouter connector's key signs `openrouter.ai/api/v1` alone. Residual: Enforced once the OpenRouter live checks in `TODO.md` pass.
- [CRED-14](credentials.md#cred-14), Built, not yet checked end to end. The OpenRouter connector's key is dropped only once OpenRouter confirms it dead. Residual: Enforced once the OpenRouter live checks in `TODO.md` pass.
- [CRED-15](credentials.md#cred-15), residual. Code in the webview can still replace a key, as it can call every mutation.
- [CRED-17](credentials.md#cred-17), Built, not yet checked end to end. The AWS connector's session stays in the keyring and memory. Residual: The source's status is "Built, not yet checked end to end": Enforced once the AWS live checks in `TODO.md` pass.
- [CRED-18](credentials.md#cred-18), Built, not yet checked end to end. Every AWS token request carries a proof under the session's key. Residual: Enforced once the AWS live checks in `TODO.md` pass.
- [CRED-19](credentials.md#cred-19), Built, not yet checked end to end. A rotated AWS refresh token is saved before credentials are handed out. Residual: Enforced once the AWS live checks in `TODO.md` pass.
- [CRED-20](credentials.md#cred-20), Built, not yet checked end to end. AWS tokens that arrive after a Cancel or Disconnect are dropped. Residual: Enforced once the AWS live checks in `TODO.md` pass.
- [CRED-21](credentials.md#cred-21), By decision. A disconnected AWS session is not revoked. Residual: The session stays valid at AWS until it expires.
- [CRED-22](credentials.md#cred-22), By decision. The AWS ID token's signature is not checked.
- [CRED-23](credentials.md#cred-23), Built, not yet checked end to end. The GitHub connector's token stays in the keyring and memory. Residual: The source's status is "Built, not yet checked end to end": Enforced once the GitHub live checks in `TODO.md` pass.
- [CRED-24](credentials.md#cred-24), Built, not yet checked end to end. The GitHub connector's token reaches a sandboxed command only through GitHub's proxy, and is granted only after the browser opened on GitHub's own origin. Residual: Enforced once the GitHub live checks in `TODO.md` pass.
- [CRED-25](credentials.md#cred-25), Built, not yet checked end to end. The GitHub connector's token is dropped only once GitHub confirms it dead. Residual: Enforced once the GitHub live checks in `TODO.md` pass.
- [CRED-26](credentials.md#cred-26), By decision. A disconnected GitHub token stays authorized on GitHub until the user revokes Kubetail there.
- [CRED-27](credentials.md#cred-27), Built, not yet checked end to end. The Google connector's bearer reaches Vertex AI in the user's project alone, under endpoints written in code. The environment's project and location cannot move it off `aiplatform.googleapis.com`. Residual: The source's status is "Built, not yet checked end to end": Enforced once the Google live checks in `TODO.md` pass.
- [CRED-29](credentials.md#cred-29), Held by review for authorization use. A restored account is display-only. Residual: At launch the account is read from the `oauth_connections` row, unverified, and nothing keeps it out of an authorization decision but review.
- [CRED-31](credentials.md#cred-31), residual. The server-side revoke beside it is not a promise. A failed revoke, or a quit before it finishes, leaves the grant live while the app reports signed out.

## The file and web tools

- [FILE-5](file-tools.md#file-5), residual. A link swapped into a parent between the check and the open, or on Windows a link-type reparse point swapped in after the tag read. A link already in a parent directory, which the request names the path through.
- [FILE-8](file-tools.md#file-8), residual. A hard link outside the data directory to a file in it.
- [FILE-12](file-tools.md#file-12), residual. A goroutine left in a syscall on a dead mount.
- [FILE-18](file-tools.md#file-18), residual. After an approved command, a hard link in the results to any file, readable unasked.
- [FILE-29](file-tools.md#file-29), residual. A file changed, or a link swapped into a parent, between the check and the rename. The temporary file a crash leaves. Hard links, xattrs and ACLs the rename drops.
- [FILE-30](file-tools.md#file-30), residual. A non-atomic write of a new file on FAT or exFAT. Setgid dropped from a directory made under a setgid parent whose group the user is not in.
- [FILE-35](file-tools.md#file-35), residual. A rename under way when the call is cancelled, which lands unstamped.
- [FILE-38](file-tools.md#file-38), residual. A match against the raw file tells the model something about text `Read` redacted, one approved probe per answer.
- [FILE-39](file-tools.md#file-39), residual. A file holding `[redacted]` as its own text is not editable with it as context.
- [FILE-43](file-tools.md#file-43), residual. The request draws LF strings in a CRLF file.
- [FILE-46](file-tools.md#file-46), residual. `Write`'s residuals (FILE-29, FILE-30, FILE-35).
- [FILE-51](file-tools.md#file-51), residual. A URL built from cluster text that the user approves.
- [FILE-53](file-tools.md#file-53), residual. A public name inside a VPN.
- [FILE-54](file-tools.md#file-54), residual. With a proxy, the proxy decides where a name lands. A `NO_PROXY` entry naming the proxy.
- [FILE-55](file-tools.md#file-55), residual. A same-host or `www.` hop is followed without asking.
- [FILE-57](file-tools.md#file-57), residual. A conversion left running after a cancel.

## Kubetail's folders and links

- [FOLD-6](folders.md#fold-6), By decision. Kubetail assumes the path to its folders is the user's alone and checks only the runtime folder. Residual: A process of the user's own outside the sandbox, or another account that can write a folder on the path, can still plant or repoint a link at one of Kubetail's folders.
- [FOLD-9](folders.md#fold-9), residual. It rests on the assumption that only the user or root can write the folders on the path.
- [FOLD-10](folders.md#fold-10), residual. It rests on the assumption that only the user or root can write the folders on the path.
- [FOLD-11](folders.md#fold-11), residual. It rests on the assumption that only the user or root can write the folders on the path.
- [FOLD-15](folders.md#fold-15), residual. It does not check a folder above the runtime folder.
- [FOLD-16](folders.md#fold-16), residual. It does not check a folder above the runtime folder.
- [FOLD-21](folders.md#fold-21), By decision on Windows. On Windows these directories and files, the bash tool's shell directory included, rely on the inherited profile ACL.
- [FOLD-23](folders.md#fold-23), residual. The file holds cluster names and server hostnames, so the directory is the gate.
- [FOLD-26](folders.md#fold-26), residual. On Windows the file relies on the inherited profile ACL, as the cache files do.

## The socket and the processes

- [IPC-4](ipc.md#ipc-4), residual. The Windows arm compiles from the same code but is exercised only by a native run.
- [IPC-5](ipc.md#ipc-5), residual. The Windows arm compiles from the same code but is exercised only by a native run.

## Logs

- [LOG-12](logs.md#log-12), residual. Colour can hide text from the eye.
- [LOG-16](logs.md#log-16), residual. Colour can hide text from the eye.
- [LOG-17](logs.md#log-17), residual. Redaction is hygiene, never a boundary: the boundary is the user's selection.
- [LOG-19](logs.md#log-19), residual. Redaction is hygiene, never a boundary: the lines are the ones `kubectl logs` would show.
- [LOG-25](logs.md#log-25), residual. A pattern that matches at once costs one turn.
- [LOG-34](logs.md#log-34), By decision. A cluster cache's log file holds the log lines its pages read, in the clear, under `<cache>/logs/`, a secret a container wrote included, as `kubectl logs` would show it. Residual: A line reaches the model through `LogsRead` and the View section. The grep a page or a follow runs is the user's or the model's pattern over text the cluster wrote.
- [LOG-35](logs.md#log-35), By decision. A container's log can stop its own stream and slow the pages that read it, and can make a page read about one second, at most 1 MiB, past a window's reservation; no more. Residual: A reservation is charged, not enforced, and a window is not stopped in its last second.

## Memory

- [MEM-3](memory.md#mem-3), residual. An approved command is outside it.
- [MEM-5](memory.md#mem-5), Held by review. The model follows only the memories marked as the user's, in the newest `<context>` block. Residual: No test can pin how a model reads the words.
- [MEM-9](memory.md#mem-9), residual. A credential in no shape it knows is stored in the clear in `app.db`, like the chat that produced it.
- [MEM-11](memory.md#mem-11), Held by review. A memory changes no permission: no tool offer, approval or gate reads one.
- [MEM-12](memory.md#mem-12), By decision. A save to one cluster is not approved.
- [MEM-13](memory.md#mem-13), By decision. A memory the model saved for every cluster reaches every cluster's chats, whole, once approved. Residual: An injection that gets the model to propose a note and the user to approve it lands in every cluster's chats as information.
- [MEM-14](memory.md#mem-14), By decision. A memory the model saved reaches every later question on its cluster, whole. Residual: A note shaped by an injection lands in the context unasked, up to 500 bytes.

## Model providers and what leaves the machine

- [MODEL-1](model-providers.md#model-1), Pending rebuild, was By decision. A chat's transcript leaves the machine, to the provider the user picked for each send.
- [MODEL-2](model-providers.md#model-2), Pending rebuild, was By decision. An injected instruction buys a misleading answer, a list of names, or a command the user reads before it runs, and nothing more.
- [MODEL-3](model-providers.md#model-3), Pending rebuild, was By decision. A send may name any provider, so a chat that switches vendor sends the new one its whole transcript. No provider's payloads reach another.
- [MODEL-4](model-providers.md#model-4), Pending rebuild, was By decision. OpenAI requests carry `store: false`. Residual: Anthropic retention is the account's. The other seven direct Chat Completions vendors are account-side only.
- [MODEL-5](model-providers.md#model-5), Pending rebuild, was By decision. OpenRouter requests carry `provider.data_collection: deny`, which restricts routing to downstream providers OpenRouter classifies as not collecting user data. Residual: This is OpenRouter's classification, not zero retention.
- [MODEL-6](model-providers.md#model-6), Pending rebuild, was By decision. OpenCode Go is a gateway rather than a vendor: the transcript reaches OpenCode's gateway and then the model's own provider. Residual: The transcript is retained per that model's published terms and monitored for abuse.
- [MODEL-7](model-providers.md#model-7), Pending rebuild, was By decision. A request to xAI, Fireworks, OpenRouter, Mistral or OpenCode Go also carries the chat id, or a subagent's run id, as its affinity key. The key is a UUIDv7, which tells the vendor when the chat began and nothing else.
- [MODEL-8](model-providers.md#model-8), Pending rebuild, was By decision. A model whose entry states no output cap sends none. Residual: Its per-turn spend ceiling is the vendor's own.
- [MODEL-10](model-providers.md#model-10), By decision. The model reads the chat's cluster's cache, bodies included, unasked, through `KubeQuery`: up to 30,000 bytes inline and 8 MiB saved a call. Residual: An off switch lands before a release build.
- [MODEL-14](model-providers.md#model-14), By decision. A KubeQuery statement's temp disk is bounded by its deadline alone, a few hundred MB a call. Residual: The row and byte limits trim the answer, not the work. No pragma caps a materialized subquery's temp storage.
- [MODEL-15](model-providers.md#model-15), residual. An env value under an ordinary name, ConfigMap data and annotations are not redacted, as stated in the record.
- [MODEL-17](model-providers.md#model-17), residual. The sandbox switch is copied at spawn, so a subagent started outside keeps running outside after the user switches back, each command still asking.
- [MODEL-18](model-providers.md#model-18), By decision. The model hands a task it wrote after reading the cluster to a subagent through `Agent`. The task is prose in the subagent's one message, after the chat's newest card.
- [MODEL-19](model-providers.md#model-19), By decision. The subagent holds every tool the parent holds but `Agent`, `Memory`, `LogsView` and `LogsWatch`, so depth is one.
- [MODEL-20](model-providers.md#model-20), By decision. A subagent's run is a committed row before it runs, and a start that fails leaves nothing running and no row behind.
- [MODEL-21](model-providers.md#model-21), By decision. A write failing in the subagent ends it alone, and no file stamp crosses between the subagent and its parent.
- [MODEL-22](model-providers.md#model-22), Pending rebuild, was By decision. A question carries a cluster card to the provider the user picked: cluster data the user did not type. The card holds the cluster's display name and kube-context, the server version, the connection verdict and TLS posture, the freshness verdict with the timestamp the app last saw the cluster live, namespace names and the API groups served.
- [MODEL-23](model-providers.md#model-23), Pending rebuild, was By decision. The cluster card leaves out the endpoint, the principal and every count.
- [MODEL-24](model-providers.md#model-24), Pending rebuild, was By decision. The cluster card is bounded at 4 KiB.
- [MODEL-25](model-providers.md#model-25), Pending rebuild, was By decision. The cluster card carries names and the sidecar's own reasons, never a message from the cluster. Every value is an escaped JSON string, so none can close the block or end the fence.
- [MODEL-26](model-providers.md#model-26), Pending rebuild, was By decision. A transcript sent to a platform leaves the machine to the user's own cloud account, under that account's logging and retention.
- [MODEL-27](model-providers.md#model-27), Pending rebuild, was By decision. The sidecar can sign outbound model requests with the user's own cloud identity (AWS, Google, Entra ID) to a platform's own endpoints. It does so for no platform until a setting turns one on. Residual: The setting is not built.
- [MODEL-33](model-providers.md#model-33), residual. A startup file chooses the key, the trust a terminal launch gives it.
- [MODEL-37](model-providers.md#model-37), Pending rebuild, was Enforced. The sidecar dials loopback at startup: Ollama discovery runs in every build, with no key. It sends nothing but `GET /api/tags` and, per model listed without capabilities, `POST /api/show` naming it.
- [MODEL-38](model-providers.md#model-38), Pending rebuild, was Enforced. A cloud-backed Ollama model is never listed. A model with a `remote_host` runs on Ollama's servers, and a transcript sent to it would leave the machine under a heading that says local.
- [MODEL-39](model-providers.md#model-39), Pending rebuild, was By decision. `OLLAMA_HOST` is honoured wherever it points.
- [MODEL-43](model-providers.md#model-43), By decision. A model's query leaves the machine to its provider's search backend, on every turn of a Claude model on the Anthropic provider, capped at 5 per turn.
- [MODEL-44](model-providers.md#model-44), By decision. The transcript draws every search query the client was told of, and the sources beside it, so the user sees what went.
- [MODEL-45](model-providers.md#model-45), By decision. The search is the model's direct call, never a code-execution container's. Every Claude model is offered it, and no other provider's list but the fake's names a vendor's tool.
- [MODEL-47](model-providers.md#model-47), By decision. A subagent's web searches, like the parent's, do not wait on the user. A subagent's search is the provider's, approved by no one, on a turn's search budget of its own.
- [MODEL-48](model-providers.md#model-48), By decision. The model puts a subagent on any model of its provider that takes tools, one the user did not pick and which may cost more.
- [MODEL-49](model-providers.md#model-49), By decision. At most 4 agents of 16 calls each run at once in a chat, and 16 background agents and commands in the app.
- [MODEL-50](model-providers.md#model-50), By decision. An agent runs in the background: its requests wait on the user whatever the parent is doing, several at once and out of view.
- [MODEL-51](model-providers.md#model-51), By decision. A request out of view is pointed to, never approved from, and Approve arms once its place has held.
- [MODEL-52](model-providers.md#model-52), By decision. A background agent's report reaches the model as a notice in a user message, escaped in its notice.
- [MODEL-53](model-providers.md#model-53), By decision. A background agent's end starts a turn only under a user's send, and no chain runs past the turn an agent's end starts.
- [MODEL-54](model-providers.md#model-54), By decision. A Cancel leaves a background agent running. Residual: The model can speak again after a Cancel, when an agent the send launched ends after it.
- [MODEL-55](model-providers.md#model-55), By decision. A subagent's `TaskStop` reaches only its own tasks.

## The sandbox: files and the fence

- [SBX-1](sandbox-files.md#sbx-1), By decision. A command the sandbox confines runs without asking, in the foreground or the background, on macOS and Linux.
- [SBX-15](sandbox-files.md#sbx-15), residual. On Linux a Deny is a mount, and a mount needs a path. A denied-always path that does not exist when a run starts is not covered, and a file something outside the run makes there while it runs is readable to it. The run itself cannot make the path.
- [SBX-16](sandbox-files.md#sbx-16), residual. On Linux a Deny needs a path that exists. Kubetail's config directory is made at launch: removed mid-session, a settings save during a run makes it again uncovered.
- [SBX-19](sandbox-files.md#sbx-19), residual. The host reads each root once, at launch: a link in a root's path that is retargeted while the app runs leaves the host's open log files in the folder it led to before.
- [SBX-27](sandbox-files.md#sbx-27), By decision. The one run Kubetail starts without the denied-always list is the snapshot's login shell. Its output reaches only commands outside the sandbox, which read the home themselves. It still reads nothing of Kubetail's and writes only its scratch.
- [SBX-29](sandbox-files.md#sbx-29), By decision. A rule's path is named when the run's rules compile, and a later change can leave the rule on a name the disk has stopped using until the next run: a rename by a process outside the sandbox, a link on the path as written repointed, or a rename by another run holding a Write rule over a folder on the path.
- [SBX-30](sandbox-files.md#sbx-30), By decision. Two paths are compared by the files they name, through a link, a `..` after one, a second name of one file or, on macOS, another case. Settings and the file tools so compare a grant with the zones however each is spelled. Residual: Not compared as files: a path past its deepest folder that exists, and within one policy a file with two hard links or a folder mounted twice.
- [SBX-31](sandbox-files.md#sbx-31), By decision. The kernel names each rule path for the profile, on macOS as the volume stores it. A rule it cannot name refuses the run, and so does a policy that names one folder two ways.
- [SBX-32](sandbox-files.md#sbx-32), By decision. A policy's check refuses a rule beneath a Write rule of the same policy, so a run cannot rename a folder on its own rules' canonical paths.
- [SBX-33](sandbox-files.md#sbx-33), residual. Only the caller's wait is bounded: the filesystem call itself is not cancelled and holds its thread until the mount answers. Not bounded: the Linux probe's lookup of its bwrap and pasta candidates, the probes' scratch folder in `TMPDIR`, and a direct call to `System` or `Never`.
- [SBX-45](sandbox-files.md#sbx-45), residual. `NoWrite` is curated, so a file a startup file sources from elsewhere, a repository's `.git/config` hook or its `.envrc` is open to a read-write grant.
- [SBX-47](sandbox-files.md#sbx-47), residual. The kubectl discovery cache is shared, so one chat can plant a document a later chat's `kubectl` reads.

## The sandbox: network and the run's door

- [NET-14](sandbox-network.md#net-14), By decision. A sandboxed command with network can send what it read to any server: cluster data short of Secret values the user has not allowed showing, and its workspace.
- [NET-15](sandbox-network.md#net-15), By decision. With network, a sandboxed command reaches the local network: on macOS any other machine's IPv4 address; on Linux any host address but loopback and the one `pasta` copies, a Docker bridge's among them.
- [NET-16](sandbox-network.md#net-16), By decision. On macOS, with network, a command can have the trust daemon send a GET, to a host and path it chooses, the loopback and the local network included.
- [NET-17](sandbox-network.md#net-17), residual. A standalone sidecar keeps this only while its API stays out of every path and port a run can reach, and while nothing a run talks to outside the sandbox passes requests through to it.
- [NET-20](sandbox-network.md#net-20), By decision. On macOS a sandboxed run with a cluster or a cloud platform holds a loopback port for the run's life, which any local process can dial.
- [NET-21](sandbox-network.md#net-21), By decision. A browser page can send a run's loopback authority as `Host`.

## The sandbox: environment, PATH and processes

- [PROC-7](sandbox-process.md#proc-7), residual. A folder an administrators' group can write is searched unasked. A startup file chooses which `kubectl` runs among the open folders, which `logToolMoves` logs.
- [PROC-8](sandbox-process.md#proc-8), residual. A typed folder is readable to every run.
- [PROC-12](sandbox-process.md#proc-12), By decision. On Linux two bind mounts of one directory are one `PATH` folder. The second entry is dropped as a duplicate, whatever is mounted beneath only it.
- [PROC-13](sandbox-process.md#proc-13), By decision. The Sandbox console runs the command the user typed only where a sandbox confines it, as a chat's sandboxed command runs.
- [PROC-14](sandbox-process.md#proc-14), By decision. A console command runs in a workspace of its own, with the folders granted always.
- [PROC-15](sandbox-process.md#proc-15), By decision. A console command reads the cluster picked but never changes it, holds no cloud platform, and reaches the internet only when the user ticks it. Residual: A console command leaves no record of what its proxy decided.
- [PROC-16](sandbox-process.md#proc-16), By decision. Console commands run one at a time. The output is capped, redacted and drawn as text, and reaches no chat and no model.
- [PROC-20](sandbox-process.md#proc-20), By decision. A sandboxed command's CPU time is bounded in the foreground. Residual: On macOS a process that ignores `SIGXCPU` outlives its CPU limit.
- [PROC-21](sandbox-process.md#proc-21), By decision. A sandboxed command's open files are bounded, and it leaves no core dump.
- [PROC-22](sandbox-process.md#proc-22), By decision. A sandboxed command's process count is bounded in the foreground, over what the kernel already counts against the run. Residual: Where the kernel counts the user's whole machine, the base is read once per run, foreground runs started together share one margin, and other programs can fill it before the clock ends. A sidecar in a container's PID namespace scans short. Root has no process limit.
- [PROC-23](sandbox-process.md#proc-23), By decision. A sandboxed command's memory is bounded on Linux. Residual: Memory on macOS is bounded by the clock or by Stop alone.
- [PROC-24](sandbox-process.md#proc-24), By decision. The limits are set inside the run, per run and per process. Residual: A background run's CPU and process count are bounded by the clock or by Stop alone. Limits are per run and per process, so parallel runs and a fork loop multiply them.
- [PROC-26](sandbox-process.md#proc-26), residual. On a macOS that runs setuid programs under a profile, one off the list, the system's own such as `ARDAgent` or a third-party one, would run as root inside it.
- [PROC-27](sandbox-process.md#proc-27), residual. On Linux a process can read its own descendants' `/proc/<pid>/mem` and `environ`, which the kernel checks without the syscall.
- [PROC-30](sandbox-process.md#proc-30), residual. The guarantee rests on the session, so a caller that replaces `Command`'s `SysProcAttr` without `Setsid` loses it.
- [PROC-32](sandbox-process.md#proc-32), By decision. Native Windows has no sandbox, so every Bash call there asks. WSL2 runs the Linux build's. Residual: In WSL2 the Linux rows and the refusal of a Windows program started inside are checked by hand, since CI has no WSL2 runner.
- [PROC-33](sandbox-process.md#proc-33), By decision. On macOS a process a sandboxed command spawns out of its group through `posix_spawn` outlives the run, and the sidecar, confined, with the forwarder's port in reach. The lifeline's kill reaches the forwarder's group alone.
- [PROC-34](sandbox-process.md#proc-34), By decision. Installing the `.deb` on Ubuntu 24.04 lets any local program run `/usr/lib/kubetail-desktop/bwrap` and get a user namespace whose processes hold no capability.
- [PROC-35](sandbox-process.md#proc-35), By decision. Installing the `.deb` on Ubuntu 24.04 lets any local program run `/usr/lib/kubetail-desktop/pasta` and get a user and network namespace in which pasta holds capabilities and can start only a bwrap.
- [PROC-42](sandbox-process.md#proc-42), residual. A POSIX `sh` such as `dash` abandons the rest of that startup file, by decision, since the shell writes only its scratch.

## Supply chain, release and updates

- [SUPPLY-1](supply-chain.md#supply-1), residual. A `docker://` step's image is not checked.
- [SUPPLY-2](supply-chain.md#supply-2), residual. `release-desktop.yml`'s workflow-wide `contents: write` is ignored until R-08 lands.
- [SUPPLY-3](supply-chain.md#supply-3), residual. Releases do not depend on these jobs (R-08).
- [SUPPLY-5](supply-chain.md#supply-5), residual. A Go call newly reaching a vulnerable function, and a RustSec-only advisory, wait for `audit.yml`'s full audits.
- [SUPPLY-12](supply-chain.md#supply-12), By decision. Every dependency update merges by a maintainer's review. None merges automatically. Residual: CI's jobs are not required checks, so nothing short of a review stops a red update.
- [SUPPLY-13](supply-chain.md#supply-13), By decision. Toolchains are bumped by hand.
- [SUPPLY-14](supply-chain.md#supply-14), By decision. Tauri's minor and major versions are bumped by hand.
- [SUPPLY-15](supply-chain.md#supply-15), By decision. Coverage reports leave for Codecov as file paths and line hit counts, never source or a secret.
- [SUPPLY-16](supply-chain.md#supply-16), By decision. No Codecov token is stored.
- [SUPPLY-17](supply-chain.md#supply-17), Held by review. The GPG signing key and the token that creates the release are used by `gpg` and `gh` in our own steps, never handed to a third-party action.
- [SUPPLY-19](supply-chain.md#supply-19), Held by review. A release job restores no GitHub Actions cache, so nothing a `main` run of a `ci-*.yml`, a `coverage-*.yml` or `audit.yml` saves reaches a release build. Residual: zizmor's `cache-poisoning` audit does not see inside the composite action.
- [SUPPLY-20](supply-chain.md#supply-20), Held by review. The SignPath token never shares a runner with installed dependencies.
- [SUPPLY-21](supply-chain.md#supply-21), Held by review. The updater's signing key never shares a runner with installed dependencies. Residual: Who can use the key is who can run a job in the `production` environment.
- [SUPPLY-22](supply-chain.md#supply-22), By decision. The Apple signing certificate and App Store Connect key sit in the job that runs `pnpm install` and `pnpm tauri build`. Residual: A dependency's install or build script can read them.
- [SUPPLY-23](supply-chain.md#supply-23), Held by review. A dispatched release version cannot reach a shell as source. Residual: Nothing exercises `scripts/check-release-version.sh`.
- [SUPPLY-27](supply-chain.md#supply-27), Held by review for the agent. Pull request text reaches the agent writing a release's notes as data. Residual: The agent runs with the releaser's shell while it reads the text, so a steered agent can still run commands in that session or write a false claim the releaser must catch.
- [SUPPLY-28](supply-chain.md#supply-28), Held by review for the agent's own write. The agent writes to GitHub only a draft's notes, only when the user asks.
- [SUPPLY-29](supply-chain.md#supply-29), By decision. The twenty-one advisories Tauri's Linux stack, `urlpattern`, the Tauri plugins' zbus 5 and `tauri-utils`' `serde_with` pin are left unresolved.
- [SUPPLY-30](supply-chain.md#supply-30), By decision. picomatch's ReDoS advisory, unpatched upstream, is left unresolved in build and test tooling.
- [SUPPLY-31](supply-chain.md#supply-31), Not built. The app tells the user a newer release exists. Residual: Nothing checks, so a user can sit on a version with a known bug indefinitely.
- [SUPPLY-32](supply-chain.md#supply-32), Built, not yet checked end to end. Signed in-app updates on macOS and Windows: the app installs only an artifact signed by one of the two keys it was built with, current or standby. Residual: The source's status is "Built, not yet checked end to end". Nothing publishes the manifests to `updates.kubetail.com` yet, and the manual check waits on the real key. Linux applies no update.
- [SUPPLY-33](supply-chain.md#supply-33), Built, not yet checked end to end. An in-app update's signature is bound to the version the manifest names. Residual: As SUPPLY-32.
- [SUPPLY-34](supply-chain.md#supply-34), Built, not yet checked end to end. Each updater key has its own manifest on `updates.kubetail.com`, and each endpoint is held to its key's id. Residual: As SUPPLY-32.
- [SUPPLY-35](supply-chain.md#supply-35), Not built. A release is gated on the checks for its own commit. Residual: `release-desktop.yml` grants write permissions to every job, and audits run only per pull request.
- [SUPPLY-36](supply-chain.md#supply-36), Not built. The macOS bundle grants only the hardened-runtime exceptions it needs. Residual: Three exceptions ship whose necessity has never been tested against a signed build.

## The webview and the windows

- [WEB-2](webview.md#web-2), residual. CSP does not govern navigation. Per-engine firing is verified by hand, not by a test.
- [WEB-7](webview.md#web-7), residual. The sidecar draws no verdict, so the frontend tests are the fence.
- [WEB-14](webview.md#web-14), By decision. No GraphQL field is served ahead of a consumer. Residual: A mutation with no caller is what review is for.
- [WEB-15](webview.md#web-15), By decision, not built. The host forwards only operations the app ships. Residual: CSP reduces injection risk but does not restrict the authority of permitted bundled code.
