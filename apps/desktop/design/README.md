# Design

The app's design lives in Figma. The app's code lives in `src/`. This folder keeps the two in step.

Nothing syncs by itself. Sometimes the design is ahead, sometimes the code is. The system records where each side stood the last time they matched, so anyone can see which side has moved. A person then decides what to do about it, and an agent does the work.

This page is for people. The rules an agent follows are in [`CLAUDE.md`](CLAUDE.md).

## The three pieces

**1. The Figma files.** [Desktop](https://www.figma.com/design/UTfwwz8cyLq5RA6mNFxUsD/Desktop) holds the app's components and screens. [kubetail-ui](https://www.figma.com/design/KoHFVSSKfENrqPidIkJfpO/kubetail-ui) is the shared library it builds on.

**2. The manifest, [`components.json`](components.json).** It has one entry per Figma component. The entry says which code draws that component:

```json
{
  "id": "dashboard/status-pill",
  "figma": { "node": "9:148", "name": "StatusPill" },
  "code": [
    { "path": "src/components/dashboard/status-pill.tsx", "blob": "49e431f2…" }
  ]
}
```

Read it as: the Figma component `StatusPill` is drawn by `status-pill.tsx`, and they last matched when the file's fingerprint was `49e431f2…`. The full entry also holds a fingerprint of the design (`figma.hash`), taken the same day.

The manifest has a second list, `screens`, with one entry per screen on the Screens page. A screen's entry names the files that lay the screen out, such as `src/routes/dashboard.tsx` for `screens/dashboard-pods`.

The fingerprint (`blob`) is what git prints for a file's contents:

```sh
git hash-object src/components/dashboard/status-pill.tsx
```

If the output equals the `blob`, the code has not changed since the last sync. If it differs, the code has moved on.

**3. The stamp.** Each component's description in Figma is one small block:

```json
{ "kubetail": 1, "id": "dashboard/status-pill" }
```

A screen has no description, so its stamp is a text layer named `Stamp` at the top of the screen's section.

The `id` ties the Figma component or screen to its manifest entry. When the design has something the code lacks, the stamp also includes a `changes` field:

```json
{
  "kubetail": 1,
  "id": "chat/sandbox-popover/menu",
  "changes": ["The popup is 400px wide; the code's is 288px."]
}
```

## Reading the state

| The code's fingerprint | The stamp | What it means |
| --- | --- | --- |
| Matches | No `changes` | In sync. Nothing to do. |
| Differs | No `changes` | The code is ahead. Update the design. |
| Matches | Has `changes` | The design is ahead. Write the code. |
| Differs | Has `changes` | Both moved. A person decides which wins. |

An edit made by hand in Figma is caught even with no `changes` line: the design's fingerprint no longer matches, and the report says *edited in Figma*. Write the line anyway. The fingerprint says that something changed, and only you can say what.

To see what has moved, run:

```sh
make design-status
```

It prints each component and screen that is not in sync, then a count:

```
code ahead  dashboard/scope-trigger     src/components/dashboard/dashboard-resource-nav/scope-menu.tsx
not built   chat/sandbox-popover/badge
1 code ahead, 1 not built, 94 in sync
```

It reads the design side from Figma with a personal access token. Create one in Figma under **Settings → Security → Personal access tokens**, with **File content: Read-only** as its only scope. On a Mac, keep it in the Keychain, where `make` finds it:

```sh
security add-generic-password -a "$USER" -s kubetail-figma-token -w
```

Elsewhere, set `FIGMA_TOKEN`. Without a token, the report covers the code side alone and says so.

## Example 1: you change the code first

You make `StatusPill` fully round in `status-pill.tsx` and merge it.

1. The file's fingerprint no longer matches the manifest. `make design-status` now lists `dashboard/status-pill` as "code ahead".
2. In Figma, save a named version of Desktop (**File → Save to version history**). Agent edits land directly in the file, and this is your undo.
3. Ask an agent: *"Update StatusPill in Figma to match the code."*
4. The agent tells you what it will change and waits for your yes.
5. It edits the component in Figma, shows you a screenshot, and records both new fingerprints in the entry: the code's and the design's.
6. Look at the screenshot. Merge the manifest change.

`StatusPill` is in sync again.

## Example 2: you change the design first

You redraw the sandbox popup in Figma. It is wider and has new sections.

1. Add what you changed to the component's stamp, one plain sentence per change, as in the stamp above. If an agent made the edit for you, it writes these.
2. `SandboxPopover/Menu` now reads as "design ahead". The `changes` are the to-do list for whoever writes the code.
3. When you are ready, ask an agent: *"Implement the design changes to SandboxPopover/Menu."*
4. The agent reads the design, changes `sandbox-popover.tsx`, and records both new fingerprints in the entry, in the same pull request.
5. Review and merge the pull request.
6. Ask the agent to remove the finished `changes` from the stamp.

## Example 3: you design something new

You draw a new component, `SandboxPopover/Badge`. No code draws it yet.

1. Give it a stamp with a new `id` and one `changes` line saying what it is:

   ```json
   { "kubetail": 1, "id": "chat/sandbox-popover/badge", "changes": ["A badge for the sandbox popup's folders and domains."] }
   ```

2. Add its entry to the manifest with no `code`. That is how the manifest says "designed, not built".
3. When the code is written, the same pull request adds `code` to the entry. Then the `changes` line comes out of the stamp.

## Example 4: both sides changed

Someone edited `ChatNav/Row` in Figma while a pull request changed `chat-nav.tsx`.

The agent will not pick a side. It lists what changed on each and asks you. You choose: the code wins, the design wins, or you merge them by hand. Then it follows example 1 or 2.

## Example 5: someone edits Figma and writes nothing down

A designer nudges `StatusPill`'s padding by hand and adds no `changes` line.

1. Nothing looks different until someone asks an agent what is out of sync. The design's fingerprint no longer matches, so the report says:

   ```
   design ahead  dashboard/status-pill  edited in Figma, with no changes written on its stamp
   ```

2. The report says that the component changed, not what changed. Open it in Figma, or ask the agent to describe it. Figma's version history shows who edited it and when.
3. Decide what the edit was:
   - **It was meant.** Add a `changes` line saying what changed. This is now example 2.
   - **It changes nothing a user sees**, such as a renamed layer. Ask the agent to record the design's new fingerprint. The component is in sync again.
   - **It was a mistake.** Restore the component from Figma's version history.

## Example 6: you ask what is out of sync

You have not looked at the design in a while and want the whole picture.

1. Ask an agent: *"What is out of sync between Figma and the code?"*
2. The agent runs `make design-status`, which reads every component's and screen's stamp and fingerprint out of Figma, and compares them and the code with the manifest. It changes nothing. You can run it yourself.
3. It gives you the report:

   ```
   diverged      chat/chat-nav/row           src/components/chat/chat-nav.tsx, 1 design change
   code ahead    dashboard/scope-trigger     src/components/dashboard/dashboard-resource-nav/scope-menu.tsx
   design ahead  chat/sandbox-popover/menu   2 design changes
   design ahead  dashboard/status-pill       edited in Figma, with no changes written on its stamp
   not built     chat/sandbox-popover/badge
   unmatched     Chat/NewThing (812:40)      has no stamp
   1 diverged, 1 code ahead, 2 design ahead, 1 not built, 1 unmatched, 90 in sync
   ```

4. Read each line by its first word:

   | State | What to do |
   | --- | --- |
   | `code ahead` | Example 1. |
   | `design ahead`, with design changes | Example 2. |
   | `design ahead`, edited in Figma | Example 5. |
   | `not built` | Example 3, when someone writes the code. |
   | `diverged` | Example 4. |
   | `unmatched` | The stamps and the manifest disagree, as when a component has no stamp. Ask the agent to explain the line. |

The report covers components and the screens that are drawn. A clean report does not mean the colors match: see [What is not built yet](#what-is-not-built-yet).

## Example 7: you sync everything

The report from example 6 is long and you want it cleared.

1. In Figma, save a named version of Desktop.
2. Ask an agent: *"Sync everything that is out of sync between Figma and the code."*
3. The agent runs the report, then gives you one plan, grouped by state, and waits. For the report above:
   - **Code ahead:** it will update `ScopeTrigger` in Figma.
   - **Design ahead:** it will change the code for `SandboxPopover/Menu`, in a pull request.
   - **Needs your answer:** which side wins for `ChatNav/Row`, and whether the edit to `StatusPill` was meant.
   - **Left alone:** `SandboxPopover/Badge`, unless you ask it to build it.
4. Answer the questions. Say yes to the plan, or to part of it.
5. The agent works through the components one at a time. It updates Figma directly and shows you a screenshot of each component it changed. Code changes go in a pull request, and so do the manifest's new fingerprints.
6. Review the screenshots and merge the pull request.
7. Ask the agent for the report again. What is left is what you chose to leave.

A long list is easier to review in parts. *"Sync everything that is code ahead"* is the safe half: it changes only Figma, and the named version undoes it.

## Example 8: a screen changes

Screens work like components. The difference is what counts as the screen's own.

- **You move the dashboard's header in `src/routes/dashboard.tsx`.** `screens/dashboard-pods` reads as "code ahead". Ask an agent: *"Update the Dashboard / Pods screen in Figma to match the code."* This is example 1.
- **You rearrange the Dashboard / Pods frames in Figma.** Add a `changes` line to the `Stamp` text at the top of its section. The screen reads as "design ahead". This is example 2.
- **You restyle `StatusPill`.** Only `dashboard/status-pill` is reported, although the screen shows it. A screen answers for its layout, the components it uses and the text it sets in them, not for how those components look.

## Rules to remember

- **Save a named version in Figma before an agent edits it.**
- **The agent always asks which side wins.** If it does not, stop it.
- **One file can hold several components.** `cluster-status/popover.tsx` draws four. A change to it flags all four as "code ahead". For the ones the change did not touch, record the new fingerprint and change nothing else. The same goes for `app-layout.tsx`, which lays out several screens.
- **Some components are security-sensitive.** Their entry has `"security": true`, as `Chat/ApprovalRequest` does. A design change to one is reviewed as a security change, not a restyle.
- **Never put real cluster data in Figma.** Screens use made-up names and logs.
- **A change to the kubetail-ui library must be published in Figma** before Desktop sees it.

## What is not built yet

- No check tells a pull request that it left a design behind. `make design-status` says so when someone runs it.
- Colors and the kubetail-ui library are not tracked. They are synced by hand.
- A screen is tracked once it is drawn. A page or state with no screen in Figma is never reported.
