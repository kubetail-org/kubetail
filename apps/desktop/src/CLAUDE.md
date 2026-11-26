# src — React webview

React 19 + TanStack Router + [urql] (GraphQL) + Vite + Tailwind v4. UI primitives from `@kubetail/ui`. Package manager is **pnpm**. Path alias: **`@/*` → `src/*`**.

## Security invariants

The whole picture is [`docs/security-model.md`](../docs/security-model.md); `apps/desktop/CLAUDE.md` states the three that hold everywhere. What an ordinary change here can break:

- **The CSP in `src-tauri/tauri.conf.json` is the webview's containment**: never widen `script-src`/`connect-src` to a remote origin, never load a script the bundle doesn't ship.
- **`ApprovalRequest` (`chat-transcript/transcript.tsx`) draws exactly what will run**: the command, path, content, edit strings, URL and host, memory name and body, or a cluster change's classified action, diff and request. Every invisible or reordering character is spelled out (`VisibleText`); long content folds behind a button Approve waits on. **Approve is a click on that drawn request and nothing else**: no keyboard shortcut, no default button, no approving from anywhere the request is not shown, no "always allow" for a raw command. Duration rules (*Allow for this command* / *this chat* / *Always allow*) are offered for a classified action alone, only where a grant can lift the verdict, armed as Approve is. A request whose action the UI cannot draw offers Deny alone. Adding any approval path is a security change → [the bash tool](../docs/security/2026-09-18-bash-tool.md), [the prompt names the action](../docs/security/2026-10-04-the-prompt-names-the-action.md).
  - **What runs unasked**: a command the sandbox confines; `Write`/`Edit` in the chat's workspace or a read-and-write granted folder; `Read` in a granted folder → [Bash runs in a sandbox](../docs/security/2026-09-28-bash-runs-in-a-sandbox.md), [path grants](../docs/security/2026-10-04-path-grants.md). A confined command wanting network the chat's switch and the turn's toggle don't give is its own request. The switch and toggle are the user's clicks alone, the switch behind a confirm; the composer draws no toggle yet, so a send carries it off → [network is the user's switch](../docs/security/2026-10-04-network-is-the-users-switch.md).
  - **A grant is the user's act alone** (Settings, or `folderGrant`); a way for the model to make one is a security change.
  - Each change a sandboxed command sends to a cluster or a cloud platform, and each read of Secret data or a cloud object's contents, runs, asks or is refused per its account's mode and the rules; Secret data passes `[redacted]` unless allowed. What no table names, a destructive change and an access control change outside a namespace ask each time → [one engine for every target](../docs/security/2026-11-25-target-neutral-permissions.md), [cluster writes ask](../docs/security/2026-09-29-cluster-writes-ask.md), [the permissions engine](../docs/security/2026-10-02-the-permissions-engine.md), [Secret data is a permissioned read](../docs/security/2026-10-05-secret-data-is-a-permissioned-read.md).
  - A subagent's request is the same request, opening with *An agent asks:* and its `Agent` call's description → [the Agent tool](../docs/security/2026-09-25-agent-tool.md).
  - **Requests are drawn whether or not an answer streams, several at once, in the order asked**. Buttons are live on a `Pending` approval while the message's `awaitingApproval` holds. **Approve arms only after its place on screen holds still for `APPROVE_ARM_MS` (500ms)**, re-arming on any move. The composer's *Show* and the chat list's dot point to a request and never approve one → [background agents](../docs/security/2026-09-25-background-agents.md).
- **The model's description of a command is its claim, never the app's.** One line, through `VisibleText`, italic, labelled *The model says:* and quoted on the request, never in place of the command there and never brighter than it → [the command description](../docs/security/2026-09-23-the-command-description.md). A settled call's row draws it bare after the call's kind, with the command in the call's body → [a settled call reads by its kind](../docs/security/2026-11-15-a-settled-call-reads-by-its-kind.md).
- **`src/` has no HTML sink** — no `innerHTML`, `dangerouslySetInnerHTML` or `eval` — and `kubetail/no-restricted-syntax`, `react/no-danger` and `no-restricted-imports` in `.oxlintrc.json` fail the lint on one, and on a template engine by import or call. Printer columns go through the restricted reader in `src/lib/jsonpath.ts`. A tooltip can't mark a spelled character, so a `title` holding cluster data takes `visibleTitle` (`src/lib/visible-text.ts`).
- **A chat answer inherits this.** `markdown/markdown.tsx` renders to React elements with raw HTML left as text; no rehype plugin may put HTML back.
- **The log viewer and the exec window inherit this.** Output is untrusted bytes: no HTML interpretation, terminal control sequences stripped. The sidecar strips each log line of every control but its SGR sequences (`services/logs`). The viewer draws every cell through `VisibleText` and the message's colours through `AnsiText` (`components/logs/ansi-text.tsx`: fancy-ansi's escaped output, parsed in an inert `DOMParser` document and read back as styled runs, never inserted). Copy writes a message without its sequences. The exec window, when it lands, brings both. → [log lines keep their colours](../docs/security/2026-10-19-log-lines-keep-their-colours.md).

## Guides

Each larger subsystem has a guide beside its components. A nested guide loads only when a file in its folder is read, and `src/lib/` is flat, so **open a subsystem's guide before changing its `src/lib/` modules**. Each guide's first line lists the modules it owns. → [ADR: agent guides sit beside the components they describe](../../../docs/adr/2026-11-15-agent-guides-sit-beside-the-components-they-describe.md)

| Guide | Covers |
| --- | --- |
| [`components/chat/CLAUDE.md`](components/chat/CLAUDE.md) | the chat: reads, outbox, composer, tool calls, approval requests, transcript, list |
| [`components/dashboard/CLAUDE.md`](components/dashboard/CLAUDE.md) | the resource nav, the cached-data watches, pages, tables, the object panel |
| [`components/logs/CLAUDE.md`](components/logs/CLAUDE.md) | the focused log view, log windows, `LogViewer`, `src/lib/log-viewer/` |
| [`components/shell/CLAUDE.md`](components/shell/CLAUDE.md) | the startup gate, the app bar, both sidebars, the window frame |
| [`components/cluster/CLAUDE.md`](components/cluster/CLAUDE.md) | the context omnibox, the cluster picker and status popover, the kubeconfig mirror, memory |
| [`components/settings/CLAUDE.md`](components/settings/CLAUDE.md) | the settings dialog |

## Routing & layout

- Routes are code-based, in `src/routes/`, wired in `src/routeTree.ts`. `_app.tsx` is the pathless layout of the app's routes; `_logs.tsx` is the log windows' (`/logs`), with no sidebars and no dialogs host. **The chat id is a path param, not a search param** — `retainSearchParams` on `_app` would drag a search param into the dashboard.
- **Layouts** (`src/layouts/`) are presentational shells rendering an `Outlet`.
- **A file in `src/lib/` holds one concept, not one export.** A single export joins the module that owns its concept (`approvalAnchor` in `chats.tsx`; the looks areas share, in `control-style.ts`). It keeps a file of its own only when several areas name it and none owns it (`app-mode.ts`, `storage-key.ts`).
- **A length of time is drawn by `humanDuration`** (`src/lib/human-duration.ts`), as kubectl prints one: `83s`, `5m7s`, `3h20m`, `3d`. A caller adds wording around it (*in 90s*, *45s ago*), never a formatter of its own. `humanMinutes` is the same by the whole minute, for a span drawn beside one that ticks each second. One that ticks is drawn through `Elapsed` (`primitives/elapsed.tsx`), which `RelativeTime` and `Countdown` word.
- **A component under `src/components/` is a `.tsx` file or a folder exporting one React component.** A component in one file is a file in its area folder (`dashboard/status-pill.tsx`). A component with parts is a folder whose `index.ts` exports that component and its prop types alone; nothing outside imports a part, except a test's `vi.mock` of the part's file and a CSS side-effect import. A part's file does not repeat its folder's name (`left-sidebar/panel.tsx`). Code that is not a component (hooks, pure functions, shared types) lives in `src/lib/`. `log-cards/` is the one family: it exports the transcript's three cards.
- **Each component sits in one of seven area folders.** `primitives/` holds components that know no feature concept: it would make sense in an app with no clusters or chats. The others are `cluster/`, `logs/`, `settings/`, `chat/` (the transcript's log cards and the dashboard's chat included), `dashboard/` and `shell/` (the app bar, the omnibox, both sidebars, the dialogs host, the startup gate, the error boundary and banner).
- **Imports point one way**: `primitives` ← `cluster` ← `logs` ← `settings` ← `chat`, `dashboard` ← `shell`. An area imports only from the areas to its left (and `src/lib/`); `chat` and `dashboard` import neither each other. Across areas an import is `@/components/<area>/<name>`; a sibling in the same area may use `./`.

## Errors reach the host's log

`reportError` (`src/lib/error-bus.ts`) is the one producer; `src/lib/error-log.ts` is the sink. `startErrorLogForwarder` and `installGlobalErrorHandlers` (`error`/`unhandledrejection` as `render`) are called in `main.tsx` **before `createRoot`**; a module that throws while loading is not covered. **`errorReportExchange` forwards every GraphQL operation error**, so a component never calls `reportError` for one.

- An `AppError`'s `context` holds **explicitly chosen keys, never a serialized `cause`** — a `CombinedError` holds GraphQL payloads full of cluster text.
- The forwarder reads each field under its own guard (a getter can throw), caps strings, swallows a rejected invoke, and **never calls `reportError` on failure** (that would loop).
- Sends past `MAX_IN_FLIGHT` are dropped, `RESERVED_IN_FLIGHT` slots kept for `render`/`auth`; the drop count rides as `context.dropped` on the next record.
- The host is the authority on caps and rate limits (`src-tauri/CLAUDE.md`, Logging).

## Window state

`kubeContext`, `resource`, `namespace` and `chat` are search params on `_app`, kept across mode switches via `retainSearchParams`: per-window, deep-linkable, history-pushing. → [ADR: URL params as window state](../docs/adr/2026-08-09-url-params-as-window-state.md). `useActiveKubeContext` (`src/lib/active-kube-context.tsx`) falls back to `KubeConfig.defaultContext`; `setContext` writes the param, never the kubeconfig, and `useFixWindowContext` fixes it there. `chat` is the dashboard panel's open chat, read by `DashboardChat` alone. `namespace` is the dashboard's namespace scope, absent for every namespace, read and written only through `useNamespaceScope` (`src/lib/namespace-scope.ts`); the omnibox's cluster switch clears it with `chat`. **The cluster is the window's one high-level scope**; everything else follows from it. → [ADR: the cluster is the app's scope](../docs/adr/2026-09-11-the-cluster-is-the-apps-scope.md), [ADR: the namespace scope is a search param](../docs/adr/2026-11-12-the-namespace-scope-is-a-search-param.md). **Links writing one param must spread the previous search** (`(prev) => ({ ...prev, resource })`).

**Chrome stays out of the URL**, so Back and Forward leave it alone: sidebar widths and open flags, the focused log view (`useLogsView`), the focused object (`useObjectView`).

**The layout bounds the scroll, and the window never scrolls.** `html` is `overflow: clip` (`index.css`), so a wheel outside every scroller moves nothing. The chain is `h-(--app-min-h) min-h-0` on the `SidebarProvider` column, `min-h-0 flex-1 overflow-clip` on the content row, the inset box, `min-h-0 flex-1 overflow-hidden` on `<main>` (all in `app-layout.tsx`), `flex min-h-0 flex-1 flex-col` on the route's `<section>`, then the table's `flex-1 overflow-auto`. Traps:

- `min-h-0` goes on every *plain* flex item between frame and scroller, never on the scroller itself.
- It must stay beside `h-` on the provider: only `min-h-*` displaces the library's hardcoded `min-h-svh` via tailwind-merge, and on Linux `--app-min-h` (100% of the `WindowFrame` inset) is shorter than `100svh`.
- That percentage is definite only while every DOM ancestor is — one wrapper `<div>` between `WindowFrame` and the provider breaks it.
- Any other scrolling route under `_app` needs `min-h-0` on its own chain (`chat.tsx` via `CenteredColumn`).
- Tests that render rows call `stubLayout()` from `@/test-utils`; jsdom has no layout, so a virtualized table renders none.

## Theming & storage

**Browser-storage keys are `kubetail:`-prefixed kebab-case, formed only by `storageKey`** (`src/lib/storage-key.ts`); call sites pass the bare name. Browser storage holds chrome shape (pane widths, open flags) and the history ceiling; a user *setting* goes to `host.json`.

**Every window shares one `localStorage`; each has its own `sessionStorage`.** Chrome shape (`usePersistedWidth`, `usePersistedFlag`) is an app-wide default with no live sync. The history ceiling is per-webview, in `sessionStorage`. → [ADR: chrome shape is an app-wide default](../docs/adr/2026-10-17-chrome-shape-is-an-app-wide-default-in-localstorage.md). Every `kubetail:*` key is *disposable* in [`docs/persisted-data.md`](../docs/persisted-data.md): a bad value reads as the default, and a new or changed key updates its register row in the same commit.

`src/lib/theme.tsx` has two axes: **color scheme** (`system`/`light`/`dark` → `.dark` on `<html>`) and **skin** (reserved). The preference lives in `host.json` (`src/lib/host-file.ts`), never localStorage. Boot is synchronous via `window.__KUBETAIL_HOST__` and the inline script in `index.html`, which **hand-mirrors `resolveColorScheme`**; its boot mark shows the light or dark app icon by the `.dark` class, as `AppMark` does, pinned by the root `index.test.ts`. Keep both in sync. → [ADR: host.json settings](../docs/adr/2026-08-09-host-json-settings.md), [ADR: first-paint theming](../docs/adr/2026-08-09-first-paint-theming.md).

## Text boxes

**Every text box turns off the webview's autocomplete, autocorrect, autocapitalize and inline predictions** (`writingsuggestions`). Draw one with `Input`, `Textarea` or `SearchBox` from `components/primitives/text-entry.tsx`, never `@kubetail/ui`'s (the lint refuses that import). Prose (the chat composer, a chat title, memory bodies and instructions) passes `prose` and keeps spellcheck; code-like text (a grep, a path, a name, a pattern) has it off. A raw `<input>`/`<textarea>` spreads `CODE_TEXT` or `PROSE_TEXT` from the same file. Each box's test pins its set with `expectCodeText`/`expectProseText` from `@/test-utils`. **A text box marks focus by its border alone, never a ring**: browsers match `:focus-visible` on a click into a text box. The primitives turn the library's ring off; a raw box sets `outline-none focus-visible:border-ring`. Other controls keep the library's keyboard-only ring.

## Cursors

**Controls and in-app links use the default arrow cursor, as native desktop apps do** (`index.css`'s base layer). Never add `cursor-pointer` to a button, menu item, tab, link, `<summary>` or popover trigger; only a link that leaves the app may take it.

## Figma

The webview's design lives in Figma; [`design/CLAUDE.md`](../design/CLAUDE.md) says how it maps to this code and how to sync it. **An agent asks the user which side wins before syncing**, and a design change to the approval request or the model's description is a security change. A change to `index.css`'s colors or to a drawn component leaves its Figma counterpart behind until someone re-syncs it.

## GraphQL — never talks HTTP directly

The webview has **no network access**. urql routes everything through Tauri IPC (`src/lib/graphql/`): queries and mutations via `invoke('graphql_query')`, subscriptions via `graphql_subscribe`/`graphql_unsubscribe` over a Tauri `Channel` (`subscribe-exchange.ts`, with its own capped-backoff reconnect). Read `src/lib/graphql/client.ts` and its tests before touching transport or retry logic. → [ADR: GraphQL over Tauri IPC](../docs/adr/2026-08-09-graphql-over-tauri-ipc.md).

**A `next` frame marked `extensions.subscriptionFailed`** is the sidecar naming why a watch died. `subscribe-exchange` sends it down the drop path (report, hold last-known data, reconnect), never to the urql sink, which would re-deliver the last frame's data. **Key on the marker, never on shape**: a live frame with a field error looks the same. A reconnect dials with the operation's variables, or with what its context's **`reconnectVariables`** answers (`logsTail` resumes past the newest line it handed on of each container). → [ADR: watch-failure reporting](../docs/adr/2026-08-14-watch-failure-reporting.md).

**Connection lifecycle is the transport-status registry** (`transport-status.ts`): `connected` and a process-wide monotonic `generation`, bumped on the host's **`open` frame**, never on the `graphql_subscribe` ack. Consumers use **`useFoldedSubscription`**, never raw `useSubscription`; it generation-gates its accumulator. **urql shares one connection per query + variables, so a consumer mounting into a running stream never sees its snapshot.** Where a watch has several consumers and one can mount late, hold the one watch in a provider above them all (`ClusterCachedDataKindsProvider`) or set **`unshared: true`**, which costs a connection and a cold list per instance, so it is not the default. `subscriptionPhase(snapshotComplete, connected)` gives `connecting` (no snapshot yet), `live`, `reconnecting`. A reconnect masks the data until its new snapshot lands. A view that must stay mounted through another watch's reconnect holds what it reads with `useLastSettled`. → [ADR: transport-status generation](../docs/adr/2026-08-09-transport-status-generation.md).

**Frames are reduced as a batch**, once per `requestAnimationFrame` or after 250ms, whichever is first. The reducer gets `frames: Data[]` (never empty). A map reducer copies once per batch and applies frames in place; a simple per-frame fold is lifted with `perFrame(step)`. A frame from a newer connection drops the pending batch. **Each subscription contract folds through its own reducer** in `src/lib/graphql/` (`collectionWatchReducer`, `stateWatchReducer`, …); a provider calls the one for its contract rather than writing its own. `ClustersProvider`'s fold treats a tombstoned cluster as a removal, for every view; change that there, never in views. An operation is named for its field, Pascal-cased; its document constant adds `Document`; a hook serving the operation alone is `use` plus its name. → [`docs/read-contracts/`](../docs/read-contracts/README.md). A test that pushes a frame and asserts on the next line calls `flushSubscriptionsSynchronously()` from `@/test-utils` at module scope. → [ADR: batch per animation frame](../docs/adr/2026-08-30-batch-watch-frames-per-animation-frame.md).

## Codegen

`pnpm codegen` reads the **sidecar's** schema (`sidecar/graph/schema.graphqls`) and emits `src/gql/`. Write operations with the **`graphql()` tagged template from `@/gql`**. After changing a query or the sidecar schema, run `pnpm codegen`.

## Tests

Vitest + `@testing-library/react` (jsdom), co-located (`*.test.ts[x]`). Mock the Tauri bridge with **`mockTauriCore()` from `@/test-utils`** (+ `vi.mock('@tauri-apps/api/core', …)` and dynamic import of the module under test). For GraphQL-driven components, push frames via `liveChannel().onmessage!(...)`, as `src/lib/graphql/use-folded-subscription.test.tsx` does.

- Workers run with `--no-experimental-webstorage` (`vite.config.ts`): Node's own `localStorage` would shadow jsdom's.
- `pnpm test --run <file>` while working; `pnpm test --run` for the whole suite.
- **Coverage is measured in `coverage-desktop.yml`'s `Measure · TypeScript`** as `vitest run --mode coverage`, which loads no React compiler. A test of re-renders fails there if it passes only through the compiler: memoize by hand where the re-render is the code's own promise (the log row's cells, the transcript's rows), and test no re-renders elsewhere. → [ADR: TypeScript coverage is measured without the React compiler](../docs/adr/2026-10-30-typescript-coverage-is-measured-without-the-react-compiler.md). Exclusions are in `vite.config.ts`, `scripts/cover-changed.sh`'s `ignored` and Codecov's `ignore`; keep the three in step. `pnpm vitest run --mode coverage` prints the per-file numbers.

## Lint / build

- `pnpm lint` (oxlint, then `oxfmt --check`), part of `make lint-desktop`. `pnpm format` writes, sorting imports too. → [ADR: lint the webview with oxlint's categories](../docs/adr/2026-11-05-lint-the-webview-with-oxlints-categories-and-a-chosen-rule-list.md), [ADR: oxlint's native React compiler rules](../docs/adr/2026-11-05-lint-the-react-compilers-rules-with-oxlints-native-ports.md)
- **`.oxlintrc.json` enables the `correctness`, `suspicious` and `perf` categories and names other rules one by one.** A rule an enabled category already turns on is listed only to give it options or turn it off, with the reason beside it. **Every rule is an error and a warning fails the run.** Kubetail's own rules are `lint/oxlint-plugin.js`, tested in `lint/oxlint-plugin.test.ts`.
- **`pnpm lint:tailwind`** (`.oxlintrc.tailwind.json`, part of `make lint-desktop`) runs apart, since loading Tailwind takes most of its ~6s. It checks `className` and `cn(...)` against `src/index.css`'s design system: the canonical spelling (`size-4`, not `h-4 w-4`), no unknown class, no two classes setting one property. `--fix` rewrites a spelling. A class string built anywhere else is not checked. **Run it after changing classes.** → [ADR: lint Tailwind classes in a run of their own](../../../docs/adr/2026-11-20-lint-tailwind-classes-in-a-run-of-their-own.md)
- **`||` stays where an empty string or `false` should fall through**; `??` is for a nullish default. **A switch over a union lists every member** and needs no `default`; any other switch has one. `tsc`, not the lint, checks fallthrough, implicit returns and `import type`.
- **A disable comment is `// oxlint-disable-next-line <rule> -- <why the rule is wrong here>`.** One without a reason or unused fails; `eslint-disable` comments are not read.
- **oxfmt** (`.oxfmtrc.json`) sorts imports: packages, `@/`, then relative paths. The `lint`/`format` scripts pass **globs only**: a directory argument mixed with a glob silently narrows to the glob's extensions.
- The **React compiler** is on in two places that must stay in step: the babel transform (`vite.config.ts`, off in `--mode coverage`) and oxlint's native ports of its rules (`recommended-latest`'s set), which a disable comment names `react/…`. A bail-out on a broken rule is a lint error; one on syntax the compiler does not support yet (`react/todo`, such as `??=`) is silent.
- **Write as though the compiler weren't there** — it bails per-function, silently. So `react/exhaustive-deps` is an **error**, and manual `useMemo`/`useCallback` are ordinary tools.
- In the Linux sandbox a package resolves only the dependencies it declares, so an error seen only there is most likely an undeclared dependency.
- **An `*.svg?react` import is a React component** drawing the SVG inline (`vite-plugin-svgr`); a plain `*.svg` import is a URL.
- `pnpm build` = `tsc -b && vite build`. Dev: `pnpm dev` (webview) or `pnpm tauri dev` (full app).

[urql]: https://commerce.nearform.com/open-source/urql/
