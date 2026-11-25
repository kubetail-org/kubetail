# Commands and the approval request

[← Security model](../security-model.md)

The model runs shell commands through one tool, Bash. A command the sandbox confines runs unasked. A command outside the sandbox runs only after the user approves that exact command in the transcript, and only the user can switch a chat outside the sandbox. The approval request is the gate, so it shows every character of the command legibly, with the directory it runs in and the model's description marked as the model's claim. An approved command runs as the user and reaches what the user reaches: the user's eye is the bound, by decision. The command's output is redacted before the model reads it, which is hygiene and not the boundary. The gate is the user's eye on the request, so these rows assume the user reads the command before approving it.

## Protections

| ID | Protection | Status | Residual |
| --- | --- | --- | --- |
| CMD-1 | A command outside the sandbox runs only after the user approves that exact command, in the transcript. That is any command in a chat the user switched outside it, or any on a machine with no sandbox. No setting, no allowlist and no read is exempt. | **Enforced** | — |
| CMD-2 | A turn's session carries the switch it reserved under, and a subagent's carries its parent turn's. A switch flipped mid-turn changes the next turn. | **Enforced** | — |
| CMD-3 | The model has no way out of the sandbox: `dangerouslyDisableSandbox` is bad input on every machine. | **Enforced** | — |
| CMD-4 | Only the user switches a chat outside the sandbox, from the composer, after a dialog. | **Enforced** | — |
| CMD-5 | A send carries the switch its sender saw, and a send whose switch differs is refused. | **Enforced** | — |
| CMD-6 | Bash is offered to every turn on a model that takes tools, wherever a shell is found. | **Enforced** | — |
| CMD-7 | An input of any other shape than Bash's own shows no request and runs nothing. | **Enforced** | — |
| CMD-8 | A reply's requests are put to the user one at a time, in reply order. | **Enforced** | — |
| CMD-9 | The approval, the run and the shown action read the input through one `parse`. It accepts five keys, each spelled exactly and at most once, and refuses any other key and a `null`. What is shown is what runs. | **Enforced** | — |
| CMD-10 | The approval request spells every invisible, reordering or control character in the command as its code point, drawn apart from the command's own text. | **Enforced** | A look-alike letter from another script is not made visible. That is the eye's. |
| CMD-11 | The command in a request wraps anywhere, with nothing capping its height. | **Enforced** | — |
| CMD-12 | A command past 2,000 characters or 24 lines is folded behind a button that says how much, and Approve is disabled until the rest is shown. Deny stays enabled, and a description never changes the fold. | **Enforced** | — |
| CMD-13 | The model's description takes one line above the command, labelled as the model's, italic and quoted, with its invisible characters spelled. It can neither push the command out of view nor pass for the app's words. | **Enforced** | A user who reads the description and not the command approves what the command does. That is the eye's. |
| CMD-14 | The directory the command runs in is one line under the command, spelled the same way. It is bounded at parse to 4,096 bytes with no control character. | **Enforced** | — |
| CMD-15 | A request is keyed on its approval, never the call or the message. A decided request's pressed state never reaches the next. | **Enforced** | — |
| CMD-16 | A request's buttons are live only on a `Pending` approval while the message's `awaitingApproval` holds. A subagent's request under a settled answer meets that. | **Enforced** | — |
| CMD-17 | Every waiting request is drawn, in the order asked. Approve arms only once its place on screen has held for 500ms, and re-arms on any move. | **Enforced** | — |
| CMD-18 | A file read's request is *Read this file?* and the path spelled the same way, never folded, with no description line. | **Enforced** | — |
| CMD-19 | A file write's request is *Write this file?*, the path spelled the same way, then the whole content spelled and folded as a command is, with Approve waiting on it. | **Enforced** | — |
| CMD-20 | A file write's request spells each space or tab ending a line, and a line says whether the file ends with a newline. | **Enforced** | — |
| CMD-21 | A file edit's request is *Edit this file?*, the path spelled the same way, then the old string over the new. Each is spelled as a write's content is and folded on its own, with Approve waiting on both. | **Enforced** | — |
| CMD-22 | A file edit's request has a line under each string saying whether it starts or ends with a newline, and a line when the two differ only in whitespace. | **Enforced** | — |
| CMD-23 | A page fetch's request is *Fetch this page?*, the URL spelled the same way and never folded, then the host the sidecar dials on its own line, off the action. | **Enforced** | — |
| CMD-24 | A request to save a memory for every cluster is *Remember this for every cluster?*, the name spelled the same way and never folded, then the whole body spelled and folded as a write's content is, with Approve waiting on it, then a line saying every cluster's chats will read it. | **Enforced** | — |
| CMD-25 | A request to forget a memory for every cluster is *Forget this memory for every cluster?* and the name spelled the same way. | **Enforced** | — |
| CMD-26 | A memory call for one cluster is not drawn as a request, since none waits on the user. | **Enforced** | — |
| CMD-27 | A request the app cannot show offers no Approve. That covers a request with none of the known kinds and a gated call the sidecar cannot show. | **Enforced** | — |
| CMD-28 | The committed row and the approval come before anything runs. The request is the command byte for byte, with the description shown beside it. | **Enforced** | — |
| CMD-29 | A no or a cancel runs nothing. | **Enforced** | — |
| CMD-30 | An approval that commits under a cancel is kept and never starts the command. | **Enforced** | — |
| CMD-31 | A write that fails before the start refuses the command rather than runs it. | **Enforced** | — |
| CMD-32 | A call may run up to 600s at the model's asking. `timeout` is defaulted, capped and rounded. | **Enforced** | By decision, the request does not show the bound. Cancel stops the command at once. |
| CMD-33 | An approved command runs as the user with the environment Kubetail runs with, and reaches what it reaches. The user's eye is the bound. | **By decision** | — |
| CMD-34 | A command starts in the directory its approval request shows: the call's `workdir`, else the chat's workspace. | **By decision** | — |
| CMD-35 | Resolving the directory is string work, so no path is touched before the user decides. A missing directory starts nothing. | **By decision** | — |
| CMD-36 | The directory check runs off the turn's goroutine, so a dead network mount cannot hold a cancel there. | **By decision** | A mount that dies between the check and the start still holds the call until the filesystem answers. |
| CMD-37 | The process group is killed after every run. | **By decision** | A descendant that left the group is outside the guarantee. |
| CMD-38 | A cancel kills the process group at once, with no grace, so shutdown's kill lands inside the host's budget whatever the command traps. | **By decision** | — |
| CMD-39 | On Windows the shell is Git for Windows' bash, found through Git's own install record and never off `PATH`. The command reaches it as a file, never on a Windows command line. | **By decision** | — |
| CMD-40 | On Windows the job object is the group. It is terminated at the same two moments, and nothing under its flags can spawn outside it. | **By decision** | A process started through an out-of-job broker (a scheduled task, WMI, ShellExecute) is outside the guarantee. |
| CMD-41 | A command reaches the cluster as the user's own `kubectl` would. The prompts make the shell the last resort after a cluster tool and have every cluster command name the card's context. | **By decision** | That is guidance and not a bound: a command without `--context` runs against the kubeconfig's current one. |
| CMD-42 | An approved command can read a workspace file it does not name. A `.git/config` that a sandboxed command or an unasked `Write` planted runs its hook under an approved `git status`, outside the sandbox, on every machine. | **By decision** | The request shows the command and its directory, not what the directory holds. Closing it is in TODO. |
| CMD-43 | The command and the model's description are read again from `tool_calls.arguments` on every read, by the tool's own action, and served as `ToolCall.action`. The description is beside the command and never in it. | **Enforced** | — |
| CMD-44 | The directory a call shows is the one the gate resolved, kept on `tool_calls.cwd`. | **Enforced** | — |
| CMD-45 | The command is recomputed by the build that reads it, so how a tool reads its input never changes for rows it has written. Changing what an input means is a new tool name. | **By decision** | — |
| CMD-46 | A settled call's row is its kind in the app's words, then the model's description bare and italic, or with no description the command. The command opens the call's body, spelled through `VisibleText`. | **Enforced** | By decision, a lying description names the call on the row until it is opened. |
| CMD-47 | A settled call's row has no `title`, so an override in the command or the description cannot reorder the tag beside it. | **Enforced** | — |
| CMD-48 | The live list of tool calls and the stored read are one spelling on every status. | **Enforced** | — |
| CMD-49 | A command whose block never reached the content is still listed after a crash. | **Enforced** | — |
| CMD-50 | Over the environment go `KUBETAIL`, `KUBETAIL_SIDECAR_PID` and `KUBETAIL_DESKTOP_PID`, and the snapshot's `PATH`. The host's variable is left out when the sidecar was not told it. | **By decision** | — |
| CMD-51 | The user's variables for commands outside the sandbox are exactly those the request showed. They are read when the request is made, kept on the call's row, drawn in the request and handed to the run, never read again. One replaces the process's own of its name and none replaces Kubetail's. | **By decision** | — |
| CMD-52 | No provider key is in a command's environment. | **By decision** | — |
| CMD-53 | A sandboxed call's environment is built rather than inherited, so no credential variable rides in and `PATH` is the run's frozen list. | **By decision** | — |
| CMD-54 | A sandboxed call's kubeconfig dials a free loopback port the forwarder takes before the command starts. A port taken in between fails the run. | **By decision** | — |
| CMD-55 | Any local process can reach that port. The proxy answers it 401 without the run's token, sent as proxy credentials, and the kubeconfig's `Host`. | **By decision** | — |
| CMD-56 | What passes the proxy is a read the policy allows. A path that is not canonical is refused. | **By decision** | — |
| CMD-57 | A read of `secrets` through the proxy passes with its values, `last-applied-configuration`, and a helm release's values and manifest Secrets rewritten as it streams. It fails closed on anything the rewriter cannot read. | **By decision** | Still passing: a value copied out of a Secret (a ConfigMap, an env value), a release's chart defaults, rendered notes and other rendered objects, and a Secret typed `helm.sh/release.v1` by hand. |
| CMD-58 | The proxy's reads go with the user's own credentials, which the command never holds. | **By decision** | — |
| CMD-59 | The proxy's grant dies with the run. | **By decision** | — |
| CMD-60 | The proxy's server bounds headers and idle connections. | **By decision** | — |
| CMD-61 | A command outside the sandbox runs in the user's shell after a snapshot of their login profile: its functions, options, regular aliases and `PATH`. A command's meaning follows the user's own definitions, as in their terminal. | **By decision** | — |
| CMD-62 | The command is quoted whole into a fixed wrapper, so the approved text is still the command. | **By decision** | — |
| CMD-63 | The snapshot leaves out global and suffix aliases and the startup options, `monitor` among them. Nothing the snapshot replays takes a command's children out of its group. | **By decision** | — |
| CMD-64 | The profile's names cannot steer the snapshot's dump. | **By decision** | — |
| CMD-65 | The snapshot file is 0400 in a 0700 directory. | **By decision** | A command the user approves can rewrite the snapshot and so every later one until the next start. |
| CMD-66 | The snapshot is taken after READY on a machine with no sandbox. On one with a sandbox it is taken once, the first time a command runs outside it, and never once the sidecar is stopping. | **By decision** | — |
| CMD-67 | The snapshot's shell dies with the sidecar, and no test's app takes a snapshot. | **By decision** | — |
| CMD-68 | A sandboxed command neither sources the snapshot nor waits for it. | **By decision** | — |
| CMD-69 | On a machine with a sandbox the snapshot's login shell runs in it. It reads the whole home but Kubetail's directories, writes only its scratch, reaches no network, and holds no variable the sandbox refuses. | **By decision** | — |
| CMD-70 | The `kill` and `pkill` shims that refuse Kubetail's processes are a courtesy, on Unix alone. | **By decision** | — |
| CMD-71 | What the login shell outputs shapes what runs outside the sandbox. The snapshot's functions, aliases and `PATH` run in every command outside it. On macOS the imported variables and `PATH` shape the sidecar's own `exec` credential plugins, so a startup file can postpone its harm there. | **By decision** | — |
| CMD-72 | On macOS the import can carry out a secret the resolution's shell read from a file the denied-always list does not name, in `HTTPS_PROXY` or `OLLAMA_HOST`. The denied-always list is shut to the resolution's shell. | **By decision** | What the denied-always list leaves is the list's curated-list residual. |
| CMD-73 | A command outside the sandbox runs the files every `-c` shell reads, with full access, once per command: zsh's `/etc/zshenv` and `~/.zshenv`, and the file bash's `BASH_ENV` names. | **By decision** | — |
| CMD-74 | A command's output is redacted before the model reads it. Redaction comes first, then the cut, and it runs over the whole capture. | **Enforced** | — |
| CMD-75 | Output is whole up to 30,000 bytes, header included. | **Enforced** | — |
| CMD-76 | Output past the inline limit is saved to the chat's `results/` with a 2 KB preview. The saved file and the preview are redacted alike. | **Enforced** | A saved file keeps up to 8 MiB until its chat is deleted, so what redaction misses stays on disk longer. |
| CMD-77 | A failed save falls back to a cut, with no Go error. | **Enforced** | — |
| CMD-78 | The call's row keeps what the model read. | **Enforced** | — |
| CMD-79 | Redaction strips the credential shapes real tools print: the provider keys the config read, credential headers, a URL's userinfo and query, bearer tokens, JWTs, `sk-` keys, and what a cluster command prints. | **Enforced** | A value with no form under a name not in the list, a short flag (`-p secret`), and any printed form interrupted before its closing delimiter, which can leak past its first token. |
| CMD-80 | Redaction is hygiene, not the boundary. The model chooses the command, and a command can re-encode its output past any rule (`… \| base64`). The approval request is the bound, and the rules cover the shapes real tools print. | **By decision** | — |
| CMD-81 | A background command is approved like any other, and its request says it outlives the answer. | **By decision** | — |
| CMD-82 | A background command runs until it exits, the model's `TaskStop` or the user's *Stop* ends it, its chat is deleted, or the app quits. A Cancel leaves it running. | **By decision** | A crash on Linux or macOS can leave a command outside the sandbox running, read `lost`, where Windows' kill-on-close job ends it. |
| CMD-83 | The model's `TaskStop` reaches its own chat's commands alone. | **By decision** | — |
| CMD-84 | Only a background command's exit starts a turn, never at startup, with every check a send makes. | **By decision** | A notice turn sends the chat to the provider with no new gesture, bounded by the approvals behind each exit. |
| CMD-85 | A notice is one escaped line. | **By decision** | — |
| CMD-86 | At most 4 background commands run at once in a chat and 16 in the app. | **By decision** | — |
| CMD-87 | A background command's output file is raw on disk and redacted when `Read` opens it. | **By decision** | — |
| CMD-88 | A sandboxed command ends with the sidecar however it dies, through the lifeline its forwarder watches. | **By decision** | On macOS a process that left the run's group survives the lifeline too. |

## Evidence

### CMD-1

- **Where:** `sidecar/internal/tools/bash`, `sidecar/internal/agent/loop/run.go`, `sidecar/internal/agent/chat/approval.go`
- **Records:** [the permissions engine](../security/2026-10-02-the-permissions-engine.md), [the bash tool](../security/2026-09-18-bash-tool.md)
- `TestASandboxedCallAsksNoOne`, `TestASandboxedBackgroundCallAsksNoOne` — a call in a chat switched outside, one through a sandbox that does not confine, and one with no sandbox ask.
- `TestASwitchedChatAsks` — the same, end to end.
- Note: a command the sandbox confines runs unasked. Its cluster writes are decided by the user's mode and rules (the sandbox's rows and the permissions engine).
- Note: Bash is one custom function on every dialect, with one command and one approval request per call.

### CMD-2

- **Where:** `sidecar/internal/tools/bash`, `sidecar/internal/agent/loop/run.go`, `sidecar/internal/agent/chat/approval.go`
- `TestATurnsRuntimeCarriesItsChatsSwitch` — a turn's session carries the switch it reserved under.
- `TestASubagentsRuntimeCarriesItsParentsSwitch` — a subagent's session carries its parent turn's switch.

### CMD-3

- **Where:** `sidecar/internal/tools/bash`, `sidecar/internal/agent/loop/run.go`, `sidecar/internal/agent/chat/approval.go`
- `TestTheFlagIsBadInput`, `TestNoPromptNamesTheFlag` — `dangerouslyDisableSandbox` is bad input on every machine.

### CMD-4

- **Where:** `sidecar/internal/tools/bash`, `sidecar/internal/agent/loop/run.go`, `sidecar/internal/agent/chat/approval.go`
- **Records:** [outside the sandbox is the user's choice](../security/2026-09-30-outside-the-sandbox-is-the-users-choice.md)
- `TestTheSwitchIsWrittenAndWatched`, `sandbox-switch.test.tsx` — only the user switches a chat, from the composer, after a dialog.

### CMD-5

- **Where:** `sidecar/internal/tools/bash`, `sidecar/internal/agent/loop/run.go`, `sidecar/internal/agent/chat/approval.go`
- **Records:** [outside the sandbox is the user's choice](../security/2026-09-30-outside-the-sandbox-is-the-users-choice.md)
- `TestASendThatSawTheOtherSwitchIsRefused` — a send whose switch differs from the chat's is refused.

### CMD-6

- **Where:** `sidecar/internal/tools/bash`, `sidecar/internal/app/app.go`, `sidecar/internal/agent/catalog`
- **Records:** [bash offered to every turn](../security/2026-09-22-bash-offered-to-every-turn.md)
- `TestEachListIsOneToolOfEachKind`, `TestBashIsOfferedWhenItIsFound` — every provider's list names bash, and it is offered where a shell is found.
- Note: `app.go`'s `chatTools` puts bash in the box wherever `bash.New` finds a shell, and every provider's list in `catalog` names it.

### CMD-7

- **Where:** `sidecar/internal/tools/bash`, `sidecar/internal/agent/loop/run.go`, `sidecar/internal/agent/chat/approval.go`
- `TestBashReadsItsInput`, `TestAnEmptyCommandIsBadInput`, `TestABadInputIsRefusedWithoutARequest` — an input of any other shape shows no request and runs nothing.

### CMD-8

- **Where:** `sidecar/internal/tools/bash`, `sidecar/internal/agent/loop/run.go`, `sidecar/internal/agent/chat/approval.go`
- **Records:** [ADR](../adr/2026-09-22-the-gate-is-the-tools-card.md)
- `TestGatedCallsAreAskedOneAtATimeInReplyOrder` — one request at a time.

### CMD-9

- **Where:** `sidecar/internal/tools/bash/bash.go`, `sidecar/internal/tools/box.go`
- **Records:** [the bash tool](../security/2026-09-18-bash-tool.md), [the request reads the arguments](../security/2026-09-23-the-request-reads-the-arguments.md)
- `TestBashReadsItsInput` — the case variant, the duplicate and the `null` are refused.
- `TestTheApprovalAndRunReadTheInputTheSameWay`, `TestARefusedWorkdirIsNotShown` — what is shown is what runs.
- Note: `Approval`, `Run` and `ActionOf` read the input through one `parse` that walks the tokens. It accepts `command`, `description`, `timeout`, `workdir` and `run_in_background`, each spelled exactly and at most once.
- Note: `ActionOf` applies `Run`'s `checkWorkdir` too, so every input `Run` refuses as bad input, it refuses.

### CMD-10

- **Where:** `src/components/chat/chat-transcript/transcript.tsx`, `src/lib/visible-text.ts`
- `spells every invisible or reordering character out`, `spells an invisible character in the command out` — each such character is spelled as its code point.
- Note: the request makes visible a bidirectional override, a zero-width or other invisible character, and a control character.
- Note: `visibleSegments` spells every `Cc`, `Cf`, `Cs`, `Zl`, `Zp` and non-ASCII `Zs` character, every `Default_Ignorable_Code_Point` and the Braille blank as its code point.
- Note: the request draws each code point in a `<mark>` apart from the command's own text.

### CMD-11

- **Where:** `src/components/chat/chat-transcript/transcript.tsx`, `src/lib/visible-text.ts`
- `wraps a command with no spaces` — the command wraps anywhere, with nothing capping its height.

### CMD-12

- **Where:** `src/components/chat/chat-transcript/transcript.tsx`, `src/lib/visible-text.ts`
- `folds a long command and holds Approve until the rest is shown`, `keeps Deny enabled behind the fold` — the fold, with Approve disabled until the rest is shown.
- `folds a long command under a description as it does without one` — a description never changes the fold.
- Note: `cutText` folds what is past 2,000 characters or 24 lines. A command too long to read is bounded this way.

### CMD-13

- **Where:** `src/components/chat/chat-transcript/transcript.tsx`, `src/lib/visible-text.ts`
- **Records:** [the command description](../security/2026-09-23-the-command-description.md)
- `draws the model's description above the command, labelled and quoted` — the label, the italics and the quotes.
- `spells an invisible character in the description out` — the description's invisible characters are spelled.
- `visible-text.test.ts` — the `descriptionLine` cases.
- Note: the description is `descriptionLine`'s one line, drawn above the command after *The model says:*. It sits in a `truncate` span with the quotes outside it and no `title`.
- Note: the description reads as the model's claim.

### CMD-14

- **Where:** `src/components/chat/chat-transcript/transcript.tsx`, `src/lib/visible-text.ts`, `sidecar/internal/tools/bash`
- **Records:** [the command names its directory](../security/2026-09-23-the-command-names-its-directory.md)
- `draws the directory under the command, above the buttons` — the directory's place.
- `draws the directory after Show the rest on a folded command` — its place on a folded command.
- `spells an invisible character in the directory out` — the directory is spelled the same way.
- `TestWorkdirIsParsed` — the bound at parse.
- Note: the line is a muted `in`, then the directory. On a folded command it comes after *Show the rest*.

### CMD-15

- **Where:** `src/components/chat/chat-transcript/transcript.tsx`, `src/lib/visible-text.ts`
- `keys the approval request on its approval` — a decided request's pressed state never reaches the next.
- Note: there is one request per call.

### CMD-16

- **Where:** `src/components/chat/chat-transcript/transcript.tsx`, `src/lib/visible-text.ts`
- **Records:** [bash offered to every turn](../security/2026-09-22-bash-offered-to-every-turn.md)
- `holds the buttons down on a decided approval` — a decided approval's buttons are not live.
- `draws a request under a settled answer, live while the answer awaits approval` — a subagent's request under a settled answer.
- Note: a request is live only while its approval is pending on a waiting turn.

### CMD-17

- **Where:** `src/components/chat/chat-transcript/transcript.tsx`, `src/lib/visible-text.ts`
- **Records:** [background agents](../security/2026-09-25-background-agents.md)
- `arms Approve once its place has held for the wait, and never holds Deny back` — Approve arms after the wait, and Deny is never held back.
- `disarms Approve when the page moves it, and arms it again once it holds` — any move re-arms.

### CMD-18

- **Where:** `src/components/chat/chat-transcript/transcript.tsx`, `src/lib/visible-text.ts`
- **Records:** [read any approved file](../security/2026-09-23-read-any-approved-file.md)
- `draws a read request with the path as text and two buttons` — the read request.

### CMD-19

- **Where:** `src/components/chat/chat-transcript/transcript.tsx`, `src/lib/visible-text.ts`
- **Records:** [the write tool](../security/2026-09-24-write-tool.md)
- `draws the path and the content as text, with two buttons` — the write request.
- `folds a long content and holds Approve until the rest is shown` — the fold, with Approve waiting on it.
- `spells each \\r of a CRLF content out` — each `\r` of a CRLF content is spelled.

### CMD-20

- **Where:** `src/components/chat/chat-transcript/transcript.tsx`, `src/lib/visible-text.ts`
- **Records:** [the write tool](../security/2026-09-24-write-tool.md)
- `spells the whitespace that ends a line and says how the file ends` — trailing whitespace and the file's ending.
- `spells content of whitespace alone` — a content of whitespace alone.
- `spells the whitespace ending the last line before the fold` — the last line before the fold.

### CMD-21

- **Where:** `src/components/chat/chat-transcript/transcript.tsx`, `src/lib/visible-text.ts`
- **Records:** [the edit tool](../security/2026-09-24-edit-tool.md)
- `draws the path and both strings as text, with two buttons` — the edit request.
- `spells each \\r and the whitespace ending a line` — each string is spelled as a write's content is.
- `folds each long string and holds Approve until both are shown` — each string folds on its own, with Approve waiting on both.

### CMD-22

- **Where:** `src/components/chat/chat-transcript/transcript.tsx`, `src/lib/visible-text.ts`
- **Records:** [the edit tool](../security/2026-09-24-edit-tool.md)
- `names the newlines at the edges of each string` — the line under each string.
- `names a change in whitespace alone` — the line when the two differ only in whitespace.

### CMD-23

- **Where:** `src/components/chat/chat-transcript/transcript.tsx`, `src/lib/visible-text.ts`
- **Records:** [the web fetch tool](../security/2026-09-24-web-fetch-tool.md)
- `draws a fetch request with the URL and the host as text` — the fetch request.

### CMD-24

- **Where:** `src/components/chat/chat-transcript/transcript.tsx`, `src/lib/visible-text.ts`
- **Records:** [memory](../security/2026-09-24-memory.md)
- `draws the name and the body as text, with two buttons` — the request to save a memory.
- `spells the whitespace that ends a line` — the body is spelled as a write's content is.
- `folds a long body and holds Approve until the rest is shown` — the fold, with Approve waiting on it.

### CMD-25

- **Where:** `src/components/chat/chat-transcript/transcript.tsx`, `src/lib/visible-text.ts`
- **Records:** [memory](../security/2026-09-24-memory.md)
- `draws a forget request with the name as text` — the request to forget a memory.

### CMD-26

- **Where:** `src/components/chat/chat-transcript/transcript.tsx`, `src/lib/visible-text.ts`
- **Records:** [memory](../security/2026-09-24-memory.md)
- `offers no Approve for a memory call for one cluster` — a memory call for one cluster is not drawn.

### CMD-27

- **Where:** `src/components/chat/chat-transcript/transcript.tsx`, `src/lib/visible-text.ts`, `sidecar/internal/tools/box.go`
- **Records:** [read any approved file](../security/2026-09-23-read-any-approved-file.md)
- `TestEveryGatedToolCanBeShown`, `draws no Approve for a request it cannot show` — the fail-closed request.

### CMD-28

- **Where:** `sidecar/internal/tools/bash`, `sidecar/internal/agent/loop/run.go`, `sidecar/internal/agent/chat/approval.go`
- **Records:** [the bash tool](../security/2026-09-18-bash-tool.md)
- `TestACommandWaitsOnTheUser` — the committed row and the approval before anything runs, and the request is the command byte for byte with the description shown beside it.
- Note: the decision is one mutation taking the approval id alone, so the gate is the user's eye on the page, not the page's code.

### CMD-29

- **Where:** `sidecar/internal/tools/bash`, `sidecar/internal/agent/loop/run.go`, `sidecar/internal/agent/chat/approval.go`
- `TestADeniedCommandDoesNotRun`, `TestACancelledWaitRunsNothing` — a no or a cancel runs nothing.

### CMD-30

- **Where:** `sidecar/internal/tools/bash`, `sidecar/internal/agent/loop/run.go`, `sidecar/internal/agent/chat/approval.go`
- `TestACancelAfterTheDecisionCommitsKeepsItAndRunsNothing` — an approval under a cancel never starts.

### CMD-31

- **Where:** `sidecar/internal/tools/bash`, `sidecar/internal/agent/loop/run.go`, `sidecar/internal/agent/chat/approval.go`
- `TestAFailedApprovalWriteRefusesTheCommand`, `TestAFailedStartWriteLeavesTheCommandNotRun` — a write failing before the start refuses rather than runs.

### CMD-32

- **Where:** `sidecar/internal/tools/bash`, `sidecar/internal/agent/loop/run.go`, `sidecar/internal/agent/chat/approval.go`
- **Records:** [ADR](../adr/2026-09-22-the-gate-is-the-tools-card.md)
- `TestATimeoutIsDefaultedCappedAndRounded` — `timeout` is capped.

### CMD-33

- **Where:** `sidecar/internal/tools/bash`, `sidecar/config.go`
- **Records:** [the bash tool](../security/2026-09-18-bash-tool.md)
- `TestACommandsEnvironmentIsTheProcessOwn` — pass-through outside the sandbox.
- Note: a sandboxed command runs under bubblewrap on Linux and Seatbelt on macOS (the sandbox's rows).

### CMD-34

- **Where:** `sidecar/internal/tools/bash`
- **Records:** [the bash tool](../security/2026-09-18-bash-tool.md), [the command names its directory](../security/2026-09-23-the-command-names-its-directory.md)
- `TestApprovalKeepsTheCwd`, `TestRunMakesTheWorkspace`, `TestACommandRunsInItsWorkdir`, `TestACdDoesNotCarry`, `TestTheApprovalShowsTheDirectoryItRunsIn` — a command starts in the directory its approval request shows.

### CMD-35

- **Where:** `sidecar/internal/tools/bash`
- **Records:** [the bash tool](../security/2026-09-18-bash-tool.md), [the command names its directory](../security/2026-09-23-the-command-names-its-directory.md)
- `TestAMissingWorkdirIsNotStarted` — a missing directory starts nothing.
- Note: `resolveWorkdir` is string work.

### CMD-36

- **Where:** `sidecar/internal/tools/bash`
- **Records:** [the bash tool](../security/2026-09-18-bash-tool.md), [the command names its directory](../security/2026-09-23-the-command-names-its-directory.md)
- `TestAStuckDirectoryCheckHonoursTheCancel` — a dead network mount cannot hold a cancel on the turn's goroutine.

### CMD-37

- **Where:** `sidecar/internal/tools/bash`
- **Records:** [the bash tool](../security/2026-09-18-bash-tool.md)
- `TestRunKillsTheGroupOnCancel`, `TestRunKillsWhatACommandLeftBehind` — the process group is killed after every run.

### CMD-38

- **Where:** `sidecar/internal/tools/bash`
- **Records:** [the bash tool](../security/2026-09-18-bash-tool.md)
- `TestACancelSkipsTheGrace`, `TestACancelDuringTheGraceKillsAtOnce` — a cancel kills at once, with no grace.

### CMD-39

- **Where:** `sidecar/internal/tools/bash`
- **Records:** [the bash tool](../security/2026-09-18-bash-tool.md)
- `TestRunDeliversTheCommandByteForByte` — the bytes of the command as the shell receives them.
- Note: Git Bash translates `PATH` and `HOME` (`%USERPROFILE%`) on the way in, so a command sees the Git Bash terminal's view of the same environment.

### CMD-40

- **Where:** `sidecar/internal/tools/bash`
- **Records:** [the bash tool](../security/2026-09-18-bash-tool.md)
- `TestRunKillsTheJobOnCancel`, `TestRunKillsTheJobOnTimeout`, `TestRunKillsWhatACommandLeftBehind` — the job is terminated at the same two moments, and nothing spawns outside it.

### CMD-41

- **Where:** `sidecar/internal/tools/bash`
- **Records:** [the bash tool](../security/2026-09-18-bash-tool.md), [the cluster through the shell](../security/2026-09-24-the-cluster-through-the-shell.md), [TODO](../TODO.md#security)

### CMD-42

- **Where:** `sidecar/internal/tools/bash`
- **Records:** [Bash runs in a sandbox](../security/2026-09-28-bash-runs-in-a-sandbox.md), [TODO](../TODO.md#security)

### CMD-43

- **Where:** `sidecar/internal/tools/bash/bash.go`, `sidecar/internal/tools/read/read.go`, `sidecar/internal/tools/box.go`, `sidecar/internal/agent/chat/record.go`, `sidecar/internal/agent/chat/store.go`
- **Records:** [the request reads the arguments](../security/2026-09-23-the-request-reads-the-arguments.md), [the command description](../security/2026-09-23-the-command-description.md)
- `TestActionOfReadsTheCommand` — neither the description nor the timeout reaches the command's `text`.
- Note: the read is by the tool's `Action`, over its `ActionOf`, through `Box.Action`, which takes an action only of its tool's kind.
- Note: a call's kind, `ToolCall.actionKind`, is its tool's through `Box.ActionKind`, never read from its arguments.
- Note: the approval row holds the decision alone, and nothing outside the tool's package parses its arguments.

### CMD-44

- **Where:** `sidecar/internal/tools/bash/bash.go`, `sidecar/internal/agent/chat/record.go`, `sidecar/internal/agent/chat/store.go`
- **Records:** [the command names its directory](../security/2026-09-23-the-command-names-its-directory.md)
- `TestTheToolCallRowHoldsTheCwd` — the directory is on the row, the live list and the stored read.

### CMD-45

- **Where:** `sidecar/internal/tools/bash/bash.go`, `sidecar/internal/tools/read/read.go`, `sidecar/internal/tools/box.go`
- **Records:** [the bash tool](../security/2026-09-18-bash-tool.md), [the command description](../security/2026-09-23-the-command-description.md), [the command names its directory](../security/2026-09-23-the-command-names-its-directory.md), [the request reads the arguments](../security/2026-09-23-the-request-reads-the-arguments.md), [a settled call reads by its kind](../security/2026-11-15-a-settled-call-reads-by-its-kind.md)
- `TestActionOfReadsOldRowsTheSame` — a fixed set of stored arguments against their actions.

### CMD-46

- **Where:** `src/components/chat/chat-transcript/transcript.tsx`
- **Records:** [a settled call reads by its kind](../security/2026-11-15-a-settled-call-reads-by-its-kind.md)
- `draws a settled command by its kind and description, the command in its body` — the summary names the command.
- Note: the command opens the call's body after `$ `.
- Note: a Bash call that never reached the gate reads as its command with no directory and no approval.

### CMD-47

- **Where:** `src/components/chat/chat-transcript/transcript.tsx`
- **Records:** [a settled call reads by its kind](../security/2026-11-15-a-settled-call-reads-by-its-kind.md)
- `gives the summary no title` — the row has no `title`.
- `spells an override in the command, keeping the tag in place`, `spells an override in the description, keeping the tag in place` — an override cannot reorder the tag.

### CMD-48

- **Where:** `sidecar/internal/agent/chat/record.go`, `sidecar/internal/agent/chat/store.go`
- `TestToolCallsMatchTheStoredRead` — the live list and the stored read are one spelling on every status.

### CMD-49

- **Where:** `sidecar/internal/agent/chat/record.go`, `sidecar/internal/agent/chat/store.go`
- `TestACommandSurvivesACrashBeforeItsCheckpoint` — a command whose block never reached the content is still listed.

### CMD-50

- **Where:** `sidecar/internal/tools/bash`, `sidecar/config.go`
- **Records:** [the bash tool](../security/2026-09-18-bash-tool.md), [bash runs the login shell](../security/2026-09-23-bash-runs-the-login-shell.md)
- `TestACommandSeesKubetailsOwnVariables` — the added variables, the host's left out when the sidecar was not told it.
- Note: the snapshot exports `PATH` alone.

### CMD-51

- **Where:** `sidecar/internal/tools/bash`, `sidecar/config.go`
- **Records:** [the bash tool](../security/2026-09-18-bash-tool.md), [the user's variables](../security/2026-11-20-the-users-variables.md)
- `TestApprovalOutsideCarriesTheUsersVariables` — the variables are read when the request is made.
- `TestTheToolCallRowHoldsTheVariablesItWasShownWith` — they are kept on the call's row.
- `TestAnApprovedCommandGetsTheVariablesItWasShownWith` — they are handed to the run.

### CMD-52

- **Where:** `sidecar/internal/tools/bash`, `sidecar/config.go`
- **Records:** [the bash tool](../security/2026-09-18-bash-tool.md)
- `sidecar/config_test.go` — the test of `takeProviderKeys`, in `config_test.go`.
- Note: absence of a key is `takeProviderKeys`, which runs before the shell import.

### CMD-53

- **Where:** `sidecar/internal/tools/bash`
- **Records:** [the bash tool](../security/2026-09-18-bash-tool.md)
- `TestTheSandboxedEnvironmentIsFixed` — the built environment.
- Note: the environment is `PATH`, the locale, `TERM`, `HOME` the workspace, and a `TMPDIR`, `ZDOTDIR` and kubeconfig of the run's own.

### CMD-54

- **Where:** `sidecar/internal/tools/bash`
- **Records:** [the bash tool](../security/2026-09-18-bash-tool.md)
- `TestInitExitsWhenThePortIsTaken` — a port taken in between fails the run.

### CMD-55

- **Where:** `sidecar/internal/tools/bash`
- **Records:** [the bash tool](../security/2026-09-18-bash-tool.md)
- `TestAWrongOrDeadTokenIsUnauthorized`, `TestAnotherHostIsUnauthorized` — 401 without the run's token and the kubeconfig's `Host`.

### CMD-56

- **Where:** `sidecar/internal/tools/bash`
- **Records:** [the bash tool](../security/2026-09-18-bash-tool.md)
- `TestEachRefusalIsAForbiddenStatus`, `TestAPathThatIsNotCanonicalIsRefused` — what passes is a read the policy allows.

### CMD-57

- **Where:** `sidecar/internal/tools/bash`
- **Records:** [the bash tool](../security/2026-09-18-bash-tool.md), [TODO](../TODO.md#security)
- `TestASandboxedRunReadsASecretRedacted`, `TestTheWalkRedactsASecretsData`, `TestATableIsRedacted`, `TestEachWatchEventArrivesAlone`, `TestAHelmReleasesValuesAreRedacted`, `TestAHelmReleasesSecretsAreRedacted` — the rewrite as the read streams.
- `TestAnUnreadableValueFailsClosed`, `TestAReleaseThatDoesNotDecodeFailsClosed` — it fails closed on anything the rewriter cannot read.

### CMD-58

- **Where:** `sidecar/internal/tools/bash`
- **Records:** [the bash tool](../security/2026-09-18-bash-tool.md)
- `TestAGetPassesWithTheConnectionsCredentials`, `TestASandboxedRunReachesTheClusterThroughTheProxy` — the reads go with the user's own credentials.

### CMD-59

- **Where:** `sidecar/internal/tools/bash`
- **Records:** [the bash tool](../security/2026-09-18-bash-tool.md)
- `TestTheGrantDiesWithTheRun` — the grant dies with the run.

### CMD-60

- **Where:** `sidecar/internal/tools/bash`
- **Records:** [the bash tool](../security/2026-09-18-bash-tool.md)
- `TestTheServerBoundsItsConnections` — the server bounds headers and idle connections.

### CMD-61

- **Where:** `sidecar/internal/tools/bash`, `sidecar/internal/run/loginshell`, `sidecar/internal/app/app.go`
- **Records:** [bash runs the login shell](../security/2026-09-23-bash-runs-the-login-shell.md)
- `TestTheShellIsTheLoginShellWhenItIsZshOrBash` — the shell is `$SHELL` when it is zsh or bash, else bash on `PATH`.

### CMD-62

- **Where:** `sidecar/internal/tools/bash`, `sidecar/internal/run/loginshell`
- **Records:** [bash runs the login shell](../security/2026-09-23-bash-runs-the-login-shell.md)
- `TestQuoteRoundTrips` — the command is quoted whole into a fixed wrapper.

### CMD-63

- **Where:** `sidecar/internal/tools/bash`, `sidecar/internal/run/loginshell`
- **Records:** [bash runs the login shell](../security/2026-09-23-bash-runs-the-login-shell.md)
- `TestTheSnapshotLeavesOutStartupOptionsAndGlobalAliases`, `TestRunKillsWhatACommandLeftBehindUnderZsh` — what the snapshot leaves out.

### CMD-64

- **Where:** `sidecar/internal/tools/bash`, `sidecar/internal/run/loginshell`
- **Records:** [bash runs the login shell](../security/2026-09-23-bash-runs-the-login-shell.md)
- `TestTheDumpIgnoresTheProfilesNames` — the profile's names cannot steer the dump.

### CMD-65

- **Where:** `sidecar/internal/tools/bash`, `sidecar/internal/run/loginshell`
- **Records:** [bash runs the login shell](../security/2026-09-23-bash-runs-the-login-shell.md)
- `TestTheSnapshotIsReadOnly` — the file is 0400 in a 0700 directory.

### CMD-66

- **Where:** `sidecar/internal/tools/bash`, `sidecar/internal/run/loginshell`, `sidecar/internal/app/app.go`
- **Records:** [bash runs the login shell](../security/2026-09-23-bash-runs-the-login-shell.md), [the sandbox's own environment](../security/2026-10-02-the-sandboxs-own-environment.md)
- `TestTheSnapshotIsTakenOnTheFirstRunOutside`, `TestTheStopReapsASnapshotStartedLate`, `TestNoSnapshotStartsAfterTheStop` — one is taken once, on first need, and never after the stop.

### CMD-67

- **Where:** `sidecar/internal/tools/bash`, `sidecar/internal/run/loginshell`, `sidecar/internal/app/app.go`
- **Records:** [bash runs the login shell](../security/2026-09-23-bash-runs-the-login-shell.md)
- `TestStoppingTheSnapshotReapsTheShell` — the shell dies with the sidecar.
- `TestAppTakesTheSnapshotOnlyWhenAsked` — no test's app takes one.

### CMD-68

- **Where:** `sidecar/internal/tools/bash`, `sidecar/internal/run/loginshell`
- **Records:** [bash runs the login shell](../security/2026-09-23-bash-runs-the-login-shell.md), [the sandbox's own environment](../security/2026-10-02-the-sandboxs-own-environment.md)
- `TestASandboxedRunSourcesNoSnapshot`, `TestASandboxedRunDoesNotWaitForTheSnapshot` — a sandboxed run sources none and waits for none.

### CMD-69

- **Where:** `sidecar/internal/tools/bash`, `sidecar/internal/run/loginshell`, `sidecar/internal/app/app.go`
- **Records:** [bash runs the login shell](../security/2026-09-23-bash-runs-the-login-shell.md), [the login shell in the sandbox](../security/2026-10-04-the-login-shell-in-the-sandbox.md)
- `TestTheSnapshotIsTakenInTheSandbox`, `TestTheSnapshotsShellReadsTheHome`, `TestANeverListVariableIsDropped`, `TestLaunchSetsTheSession` — the snapshot's login shell runs in the sandbox.

### CMD-70

- **Where:** `sidecar/internal/tools/bash`, `sidecar/internal/run/loginshell`
- **Records:** [bash runs the login shell](../security/2026-09-23-bash-runs-the-login-shell.md)
- `TestTheKillShimsRefuseKubetail` — the `kill`/`pkill` shims refuse Kubetail's processes.

### CMD-71

- **Where:** `sidecar/internal/run/loginshell`, `sidecar/internal/tools/bash`, `sidecar/internal/app/launchshell_darwin.go`
- **Records:** [the login shell runs in the sandbox](../adr/2026-10-04-the-login-shell-runs-in-the-sandbox.md)
- Note: the snapshot exists to carry the user's shell into commands outside the sandbox, which ask, every one.

### CMD-72

- **Where:** `sidecar/internal/run/loginshell`, `sidecar/internal/tools/bash`, `sidecar/internal/app/launchshell_darwin.go`
- **Records:** [the login shell runs in the sandbox](../adr/2026-10-04-the-login-shell-runs-in-the-sandbox.md)
- `TestTheResolutionCannotReadTheNeverList` — the denied-always list is shut to the resolution's shell.

### CMD-73

- **Where:** `sidecar/internal/tools/bash/bash_unix.go`
- **Records:** [the login shell runs in the sandbox](../adr/2026-10-04-the-login-shell-runs-in-the-sandbox.md), [outside the sandbox is the user's choice](../security/2026-09-30-outside-the-sandbox-is-the-users-choice.md)
- Note: such a command asks, every one. Running the user's shell as they set it up is what the chat's switch is for.

### CMD-74

- **Where:** `sidecar/internal/tools/bash/bash.go`, `sidecar/internal/tools/save.go`, `sidecar/internal/lib/safe`
- `TestACommandResultIsRedactedThenCut` — the order.
- `TestRedactKeepsTheLength` — the redaction runs over the whole capture.

### CMD-75

- **Where:** `sidecar/internal/tools/bash/bash.go`, `sidecar/internal/tools/save.go`
- `TestOutputUpToTheInlineLimitIsWhole` — the inline limit, with the header inside it.

### CMD-76

- **Where:** `sidecar/internal/tools/bash/bash.go`, `sidecar/internal/tools/save.go`, `sidecar/internal/lib/safe`
- **Records:** [saved output and Read](../security/2026-09-23-saved-output-and-read.md)
- `TestTheSavedFileIsRedactedWhole` — the saved file and the preview are redacted alike.
- Note: a saved file keeps up to 8 MiB until its chat is deleted, where 16 KiB was kept before.

### CMD-77

- **Where:** `sidecar/internal/tools/bash/bash.go`, `sidecar/internal/tools/save.go`
- `TestAFailedSaveFallsBackToACut` — a failed save cuts, with no Go error.

### CMD-78

- **Where:** `sidecar/internal/tools/bash/bash.go`
- `TestAnApprovedCommandIsShownRunningThenDone` — the row's `result` is what the model read.

### CMD-79

- **Where:** `sidecar/internal/lib/safe`
- `TestSafeStripsCredentialShapesAndKeepsTheDiagnostic` — each rule, from both sides.
- `TestRedactComposesTheRules` — the whole pipeline over one mixed text.
- Note: the shapes are `safe`'s. They are the provider keys the config read, an `authorization`/`cookie`/`api-key` header line, a URL's userinfo and query, a bearer token, a JWT, an `sk-` key, and what a cluster command prints.
- Note: of what a cluster command prints, a PEM private key is redacted whole (`pemRE`, a certificate kept).
- Note: a credential field is redacted by the name its format gives it, with the lines its value continues onto (`redactFields`, `fieldRE`: `client-key-data`, `token`, `password`, `private_key`, `aws_secret_access_key` and the rest of a closed list).
- Note: also redacted are a long flag's value (`flagEqRE`, `flagSpRE`), a `.netrc` password in any token order (`netrcRE`), an AWS access key id (`awsKeyRE`) and the tokens with an issuer prefix (`prefixedRE`).
- Note: the residual is stated as its class. A value with no form under a name not in the list is a Kubernetes Secret's `data:` map or an arbitrary `*_PASSWORD` in an env dump.
- Note: a printed form is interrupted before its closing delimiter by the capture limit, a kill, or a value's own blanks or newlines.
- Note: a new instance of the interrupted-form class is this residual restated, not a defect.
- Note: no rule matches on a string looking random.

### CMD-80

- **Where:** `sidecar/internal/tools/bash/bash.go`, `sidecar/internal/lib/safe`
- **Records:** [the bash tool](../security/2026-09-18-bash-tool.md)

### CMD-81

- **Where:** `sidecar/internal/tools/bash/task*.go`, `src/components/chat/chat-transcript/transcript.tsx`
- **Records:** [background commands](../security/2026-09-23-background-commands.md)
- `TestABackgroundApprovalSaysSo`, `says on the approval request that the command keeps running` — the request.

### CMD-82

- **Where:** `sidecar/internal/tools/bash/task*.go`, `sidecar/internal/tools/taskstop`, `sidecar/internal/agent/chat/tasks.go`
- **Records:** [background commands](../security/2026-09-23-background-commands.md)
- `TestDeletingAChatStopsAndJoinsItsTasks`, `TestShutdownStopsEveryTask`, `TestATaskStartingDuringShutdownIsStopped`, `TestAStartBehindTheStopIsRefused` — the lifetime.

### CMD-83

- **Where:** `sidecar/internal/tools/taskstop`, `sidecar/internal/agent/chat/tasks.go`
- **Records:** [background commands](../security/2026-09-23-background-commands.md)
- `TestTheModelStopsTheChatsOwnTask`, `TestTaskStopStopsTheChatsOwnTask` — the model reaches its own chat's commands alone.

### CMD-84

- **Where:** `sidecar/internal/agent/chat/tasks.go`, `sidecar/internal/agent/chat/notices.go`
- **Records:** [background commands](../security/2026-09-23-background-commands.md)
- `TestAnExitStartsATurn`, `TestACancelledTurnKicksNothing`, `TestAKickTheSendRefusesStartsNothing`, `TestTheStartSweepMarksRunningTasksLost` — only an exit starts a turn, as a send would.

### CMD-85

- **Where:** `sidecar/internal/agent/chat/notices.go`
- **Records:** [background commands](../security/2026-09-23-background-commands.md)
- `TestANoticeHoldsOneLine`, `TestANoticeEscapesEveryField` — a notice is one escaped line.

### CMD-86

- **Where:** `sidecar/internal/tools/bash/task*.go`, `sidecar/internal/agent/chat/tasks.go`
- **Records:** [background commands](../security/2026-09-23-background-commands.md)
- `TestAFifthTaskIsRefused`, `TestASeventeenthTaskIsRefused`, `TestASlotFreesWhenTheRowIsWritten` — the limits.

### CMD-87

- **Where:** `sidecar/internal/tools/bash/task*.go`
- **Records:** [background commands](../security/2026-09-23-background-commands.md)

### CMD-88

- **Where:** `sidecar/internal/tools/bash/task*.go`
- **Records:** [background commands](../security/2026-09-23-background-commands.md)
- `TestARunEndsWithTheLifeline` — a run ends with the lifeline, on macOS and Linux.
- `TestARunWithTheInternetEndsWithTheLifeline` — the same for a run with the internet.
