# src/components/chat — the chat

The chat panes, composer, transcript and list. Also owns `src/lib/`'s `chats.tsx`, `chat-outbox.tsx`, `models.tsx`, `chat-switch.tsx`, `chat-grants.tsx`, `sandbox.tsx`, `held-still.ts` and `visible-text.ts`. Memory is in the cluster guide ([`../cluster/CLAUDE.md`](../cluster/CLAUDE.md)); where a log card's view goes is in the logs guide ([`../logs/CLAUDE.md`](../logs/CLAUDE.md)).

## Panes

`ChatPane` (one chat) and `NewChatPane` (a chat not yet started) are mounted by chat mode's page and the dashboard's right sidebar. **Neither navigates**: they report `onCreated(id)` / `onGone()`, and the home decides — `chat.tsx` goes to `/chat/$chatId` with `replace: true`, `chat-id.tsx` back to `/chat`, `DashboardChat` writes or clears its `chat` param. → [ADR: the answer is a record](../../../docs/adr/2026-09-10-the-answer-is-a-record.md).

## Reads

**Reads are `src/lib/chats.tsx`.** `useChats` sorts by `Date.parse(updatedAt)` desc, then `id` desc on a millisecond tie. `useChatMessages(chatId)` orders by `seq`; ids order nothing. `useChats` and `useChatMessages` are `unshared: true`, fold an id-keyed map over `useFoldedSubscription` (no `useCacheCollectionWatch`: no cache provenance), and gate on `type === 'Bookmark'`. `useChats` holds its last synced list through a reconnect's replay, each record replaced as it lands.

**Each part of a message has one reader in that file** (`textOf`, `noticesOf`, `searchesOf`, `sourcesOf`, `summaryOf`, `toolCallTag`, `grantOffered`, `waitingRequestsOf`, `inFlight`, `queryResultOf`). Keep it that way: a second, laxer narrowing of `content: JSON!` is how a tool block reaches the screen. `textOf` reads text blocks only; tool rounds, thinking, `context` and notice blocks draw nothing. Web text is data, drawn as text. `chatModeOf` is the one `AppMode` → wire mapping, so the list filter and the send agree.

**A chat belongs to one mode and one cluster** (`Chat.mode`, `Chat.clusterID`, fixed at creation). The list watch is unscoped and both filters are the client's, since `OpenChat` must tell a deleted chat from another cluster's. **Any send may name any model in the catalog** → [ADR: a chat can switch to any model](../../../docs/adr/2026-09-25-a-chat-can-switch-to-any-model.md).

**`OpenChat` is keyed on the chat id** and owns both watches. After the list's `Bookmark`, an id the map lacks renders "This chat was deleted." A chat of another cluster renders its own notice, gated on the clusters snapshot, with `Switch to it` while that context exists. Switching cluster from the omnibox's picker clears the `chat` and `namespace` params, leaves `/chat/$chatId` for `/chat`, and clears the focused log view; `setContext` itself touches none of them.

## Writes

**Writes are `src/lib/chat-outbox.tsx`**, mounted at `AppLayout` above the routes so a send settles whatever became of the composer that started it. One entry per chat, keyed by chat id or `new:<mode>`: the draft, the `pick` (model + effort), the turn's toggle (`thisTurn`), and one `Send` (`idle | sending | held | awaiting`). `useChatOutbox(mode, chatId, clusterId?, seed?)` is the whole API. → [ADR: the chat outbox](../../../docs/adr/2026-10-17-the-chat-outbox-holds-each-chats-send-above-the-routes.md).

- **Nothing moves between keys** except `moveDraft` (the deleted-chat notice's "Start a new one"). An existing chat's send goes `awaiting(seq)` until the composer sees that row and calls `settle`; a created chat's leaves the `new:` entry idle and empty.
- **A refusal is settled, a dropped response is not.** A GraphQL error returns to `idle` with the draft kept; the next submit mints a new `requestID`. A `networkError` holds the send — id, content, cluster, pick, switches, turn's toggle, and whether it was the draft — behind Retry/Discard. Retry resends it exactly, never under the window's current cluster or pick.
- `submit`/`retry` resolve to the created chat (id + cluster) or null. The composer follows it only while mounted *and* still on that cluster — the `new:` composer outlives a cluster switch.
- **Ask again** (on the last row when it is a `Failed` answer to a question) is `askAgain(content, pick)`: a fresh send under a new `requestID` with the failed row's question and pick. It leaves the draft alone and rides the entry's one send. Retry is the same send under the same id.
- The one refusals remembered are `KUBETAIL_CHAT_CONTEXT_FULL` (`{ kind: 'context-full', model }`, the model that send named), `KUBETAIL_CHAT_SANDBOX_CHANGED` and `KUBETAIL_CHAT_NETWORK_CHANGED`; the next send clears them. Send stays enabled.
- An unpicked entry takes the composer's `seed` (the last answer's model, else the catalog's first) only once both the messages `Bookmark` and the catalog have answered. **An effort belongs to its model**: falling back to another model takes that model's default.

`useModels` (`src/lib/models.tsx`) reads the one `modelsStateWatch`, held by the root's `ModelsProvider`; the file owns `seedPick`/`modelOf`. `snapshotComplete` is its first frame: after it an empty catalog is a sidecar with no provider. The ChatGPT plan's models come and go with its connection, and every picker follows.

## The composer

- **`ModelPicker`** (`primitives/model-picker.tsx`, shared with the incident triage): a tab only browses; a model picked there moves the effort to its default.
- **The pick.** A stored pick the catalog lacks is written back to the seed by an effect, and `canSend` reads the entry's actual pick, never a render-time fallback. **A pick or last answer whose provider lists nothing stays** (ChatGPT, disconnected), drawn *unavailable*, with Send shut.
- **Send is disabled** while the watch it waits on is `connecting`, while the send is unsettled, while a new chat has no cluster, while a switch is in flight, and while the last message is `Streaming`. The last is in the guard as well as behind the button, since Enter takes the same path. IME composition is respected (`nativeEvent.isComposing`).
- **Placeholders that blame the user wait for their watch to answer.** An empty catalog's reads *Connect ChatGPT, or set an API key and restart Kubetail*, since the sidecar reads keys once per launch.
- **A snapshot of the view rides a send** in chat mode, on a chat: the focused view while it is this chat's and the right sidebar is open, else `shownWindowOf(chatID)`'s log window. The composer reads the picked `LogViewportHandle` at the press. `snapshotOf` (`src/lib/logs-snapshot.ts`) keeps the first 50 selected lines and sends `selectedCount`. A send never waits on a log window, so it carries that window's last push. It carries the grep the viewer runs, which the sidecar's View section puts in place of the call's. A chip above the box names the snapshot's place, and says when fewer lines ride than were selected.
- **A selection rides every send** until it changes or the user presses the chip's ×, which takes the lines out of the send and leaves them selected in the viewer. The composer holds the removed selection array and compares by identity, since the viewer writes a new array only when the selection changes. A held send keeps its snapshot; Ask again sends none.

**A send names the switches its sender saw.** `chatSend` carries the chat's `sandboxDisabled` and `networkEnabled` as the list watch has them, and the sidecar refuses a mismatch. A new chat carries its cluster's default from `useSandboxDefault` (`src/lib/sandbox-default.tsx`) and no network, and `NewChatPane` holds Send until the default arrives and while a change of it is in flight (`usePendingSwitch`); a create that saw a stale default is refused. So Send and Ask again wait for the list to deliver the chat, and from a switch's press until the watch shows its committed value (`useChatSwitch`, `src/lib/chat-switch.tsx`, held in `ChatSwitchProvider` at `AppLayout`). → [ADR: leaving the sandbox is the user's switch](../../../docs/adr/2026-09-30-leaving-the-sandbox-is-the-users-switch-for-a-chat.md), [ADR: a new chat starts with its default](../../../docs/adr/2026-11-27-a-new-chat-starts-with-its-default-and-a-message-can-leave-the-sandbox-for-its-turn.md).

- `useSandbox` (`src/lib/sandbox.tsx`) is the one reader of the `sandboxStateGet` query.
- **Every control that moves toward outside confirms in a `Dialog`; moving back in is immediate.** `SandboxPopover`'s popup holds an open chat's `SandboxSwitch`, or names the default a new chat starts with, and `MessageSandboxSwitch` while the next message would run in the sandbox. A chat starts with its default (Settings › Sandbox). → [outside the sandbox is the user's choice](../../../docs/security/2026-09-30-outside-the-sandbox-is-the-users-choice.md), [new-chat defaults and a message's override](../../../docs/security/2026-11-27-new-chat-defaults-and-a-messages-override.md).
- **The outbox entry's `thisTurn`** is the per-message toggle, `network` or `outside`, one at most since network is the sandbox's: `submit` sends it, a held send keeps it (the popup drawn as it left and fixed, and Retry naming another cluster, until Retry or Discard), the accepted draft send and a press of the chat's switch clear it, Ask again ignores it.
- **`NetworkSwitch`** (open chat, in the sandbox, drawn only while its network is on): off is immediate; on confirms. Nothing in the app turns network on yet. Nothing in the composer sets the entry's `network` toggle yet. → [network is the user's switch](../../../docs/security/2026-10-04-network-is-the-users-switch.md).
- **`ChatGrants`** lists the chat's own rules, then its folder grants; a folder's Remove is `folderRevoke` as the chat's. `useChatGrants` (`src/lib/chat-grants.tsx`) re-asks after a removal, on focus, and after a `Chat`/`Always` decision. **Every query or mutation that can change the rule list carries `chatGrantsContext`** (`additionalTypenames: ['PermissionRule']`) — an empty list gives urql's cache nothing to key on.

## Tool calls

`ChatMessage.toolCalls` is every call of a turn, each with the `action` its tool in the sidecar read from its arguments (or, for `LogsView`, `LogsRead` and `LogsWatch`, what its run resolved), its `approval`, and `agentCallID` for a subagent's call. **The webview never parses a tool's arguments, a path, a URL or a host** — it draws the sidecar's fields. → [the request reads the arguments](../../../docs/security/2026-09-23-the-request-reads-the-arguments.md).

- Each of the answer's own calls not awaiting approval is a closed disclosure in the answer's work disclosure above its text. A log card, a running background task, an unasked write or edit and a grant offer stay under the text instead (`standsOut`). The row's tag (`toolCallTag`) shows only when something went wrong or still runs. Rows go through `VisibleText` with no `title`.
- **A `Write`/`Edit` that ran unasked** (no `approval`, `Succeeded`) draws its content open under the answer, since a later command may run the file.
- A memory call for every cluster carries *for every cluster* whatever its status.
- A sandboxed call's *Ran in* line says `sandboxed` (and `with network`); the request never does.
- **A `KubeQuery` call's body** (`chat-transcript/kube-query.tsx`) reads its result through `queryResultOf` alone; an answer that is not rows draws as its text. Column names and cells go through `VisibleText`.
- **A `LogsView` card**: the chat's newest pinned `Tail` view runs live; every other card reads one page and stops. A frozen tail card ends at its *until*: the next view's `resolvedAt` when pinned, its own when not, so a reload shows the same lines. `viewEnds` gives each card its *until*, null for the newest.
- **A `LogsRead` card** draws the lines the model read from the action alone, requesting nothing. A line stamped before `since` is tagged *before*, one past the range's end *after* (`until`, else the nanosecond before `resolvedAt` when padded after), compared with `compareTimestamps`.
- **A `LogsWatch` card** draws what it waits for off the action and its task's state off `ToolCall.background` alone, and fetches nothing. Why it ended can carry the cluster's words, so it goes through `VisibleText`.
- *View around this* on a read or watch card sets the focused view to its `viewAround`, in chat mode alone.

## Approval requests

The rules are in [Security invariants](../../CLAUDE.md#security-invariants). Traps for anyone changing `ApprovalRequest`:

- Requests are drawn after the answer's text and calls, in `approval.id` (UUIDv7) order, via `waitingRequestsOf`, which also returns waiting sandbox actions and which the composer's Show and the chat list's dot use. Each carries the DOM id `approvalAnchor(approval.id)`.
- **Keyed on `approval.id`**, never call or message, so pressed state never leaks to the next request.
- **A `false` answer leaves the buttons down**; only a mutation error hands them back.
- **Approve and every allow answer arm through `useHeldStill`** (`src/lib/held-still.ts`); Deny never waits. Tests pass `approveArmMs`; production passes nothing.
- `cutText` (`src/lib/visible-text.ts`) folds long text, and Approve waits on every fold that carries what is approved. Paths, URLs, hosts and names are never folded. Whitespace that changes meaning — `\r`, trailing spaces, a final newline, newlines at an edit's ends — is spelled or stated.
- The command heading comes from `commandHeading` off `sandboxAvailable` and the call's own `command.sandboxed`/`network` — **never from the chat's current switch**, which can flip while a request waits.
- A write's fields are read through `writeOf`: the HTML-sink lint refuses any `.write`.
- **What a sandboxed command sends** is `ToolCall.sandboxActions`, one list in the order asked, each a `ClusterWrite` or a `CloudRequest` (`clusterRequestOf`, `cloudRequestOf`). Cluster writes and Secret reads draw from the proxy's `action.summary`, the `diff` and the raw request; a cloud platform's draws its summary, host, details and raw request open, every string through `VisibleText`. The allow answers appear on any action whose `grantable` is set. `approved`/`allowed` (`sandboxActionTag`) is the decision, not proof the cluster received it. → [cluster writes ask](../../../docs/security/2026-09-29-cluster-writes-ask.md), [the permissions engine](../../../docs/security/2026-10-02-the-permissions-engine.md), [Secret data is a permissioned read](../../../docs/security/2026-10-05-secret-data-is-a-permissioned-read.md).
- **The model's description** is drawn only by `ModelDescription` over `descriptionLine`, with no `title`: a native tooltip draws it unspelled.
- `WaitingApproval` is in flight like `Streaming` (`inFlight`): spinner, and the composer shows Cancel. An agent waiting does not put its message in flight.

The security records of the tools whose requests this draws: [bash offered to every turn](../../../docs/security/2026-09-22-bash-offered-to-every-turn.md), [the command names its directory](../../../docs/security/2026-09-23-the-command-names-its-directory.md), [saved output and Read](../../../docs/security/2026-09-23-saved-output-and-read.md), [read any approved file](../../../docs/security/2026-09-23-read-any-approved-file.md), [the write tool](../../../docs/security/2026-09-24-write-tool.md), [the edit tool](../../../docs/security/2026-09-24-edit-tool.md), [the web fetch tool](../../../docs/security/2026-09-24-web-fetch-tool.md), [memory](../../../docs/security/2026-09-24-memory.md), [KubeQuery reads the mirror](../../../docs/security/2026-09-27-kubequery-reads-the-mirror.md).

## Background tasks, agents and grants

**Background commands, agents and watches outlive their answer.** Their calls tag off `ToolCall.background` and draw a **Stop** (`backgroundTaskStop`) while running — down from the press, handed back only on an error. A Cancel stops the answer, not its tasks. A task's end reaches the model as a notice on a user message; the transcript draws a notice as one muted line, never a bubble, and draws neither an agent's report nor its error, nor a watch's sample, in it. A failed answer to notices alone has no Ask again. → [background commands](../../../docs/security/2026-09-23-background-commands.md).

**An `Agent` call holds its subagent's calls** (those with its id as `agentCallID`), drawn inside its disclosure with the brief and, once complete, the report through `Markdown`. The call's `output` is not drawn. A subagent's request is drawn where requests go. → [background agents](../../../docs/security/2026-09-25-background-agents.md).

**A failed sandboxed command offers a folder grant** (`grantOffered`; `GrantOffer` mounts `FolderGrantForm` under the call). The field starts empty — nothing reads the output to fill it — and a grant never re-runs the command. → [path grants](../../../docs/security/2026-10-04-path-grants.md).

## The transcript

**The transcript draws an answer when it has something to say**: `Streaming`/`Failed`/`Cancelled` always; `Complete` with text, else as its stop reason; `Stopped: tool_use` and `Stopped: pause_turn` always, since the earlier rounds' text would read as the answer.

- **Every question and answer reserves a 24px row of actions under it** (`chat-transcript/turn-actions.tsx`), so showing it moves nothing. An answer in flight keeps its row empty until its turn finishes. Copy copies `textOf`; Reload and Edit are disabled, since the sidecar can neither answer a turn again nor change one. A message of notices alone has no row.
- **The work disclosure's `open` is React's alone** (the click handler prevents the native toggle).
- **A markdown link gets a `title`, never an `href`.**
- Fences with a language are highlighted lazily by `src/lib/highlight.ts` (one chunk per grammar, `ALIASES` maps fence words). Text that is not markdown (an object's YAML, a query's SQL) takes the same colours through `HighlightedText`, which draws every run through `VisibleText`.
- *Sources* and *Searched the web · N* draw under an answer as soon as seen — the queries left the machine.
- A question is plain text, never markdown. The context it carried (cluster card, memory notes, workspace, view, sandbox) is not drawn.
- The transcript follows a growing answer only within 32px of the end.
- **While an answer streams, only its row renders.** `Message` is memoized by hand, so every prop `ChatTranscript` passes it must keep its identity across frames: the fold keeps an unchanged message's object, and `useViewEnds` keeps `ends` while its entries hold. `Markdown` is memoized on `text`, and a fence's highlight on its source.

## The chat list

`ChatNav` takes `mode`, deciding which chats it lists and where a row points (`/chat/$chatId`, or the dashboard's `chat` param). Rename (`ChatRenameBox`) and Delete (`ChatDeleteDialog`) are shared with `ChatHeader`. `Chat.awaitingApproval` draws a *Waiting on you* dot.
