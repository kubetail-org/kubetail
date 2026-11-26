# Figma

How the webview's design in Figma maps to the code in `src/`, and how agents sync the two through the Figma MCP. Either side can be ahead. [`components.json`](components.json) is the manifest that joins them. [`README.md`](README.md) walks a person through the workflow. Two Figma files hold the design:

- **[kubetail-ui](https://www.figma.com/design/KoHFVSSKfENrqPidIkJfpO/kubetail-ui)** is the team library: a port of `@kubetail/ui`. Its Cover names the commit it was synced from. An edit there reaches other files only once the library is published.
- **[Desktop](https://www.figma.com/design/UTfwwz8cyLq5RA6mNFxUsD/Desktop)** is the app.

**This guide holds rules, not inventory.** What the files hold — pages, sections, components, variables and their values, which screens and states are drawn — is read from the live file through the MCP when it is needed. Never record it here; a rule may name an example.

## Layout

Every group on a page is a Figma section, laid out so that no two overlap. A new component or screen goes in its group's section, and the section grows to fit it.

- **Components** has one section per folder of `src/components/`. Each section names its folder under its title and stacks its components top to bottom. A popup gets a nested section of its own.
- **Screens** has one section per area of the app (*Shell*, *Chat*, …), laid out left to right. Each area stacks its screens' sections top to bottom: the area's states first, then the screens that draw a popup open. A screen's section holds its [stamp](#manifest), a text layer named `Stamp`, over its *Kubetail Desktop Light* frame and then its *Kubetail Desktop Dark* frame. A screen drawn in several states or platforms gives each one a row, light on the left and dark on the right. An area section holds only sections, so it is not a screen.
- The layers panel lists sections in the order the canvas shows them.

## Tokens

- **The library's `Theme` collection holds Kubetail's colors and fonts** in its *Kubetail Desktop Light* and *Kubetail Desktop Dark* modes, beside kubetail's own *Light* and *Dark*. They mirror `src/index.css`: `font/sans` is `--font-sans` and `font/mono` is `--font-mono`. The library's text styles are bound to them.
- **Every text layer's font family is bound to `font/sans` or `font/mono`**, in both files, never typed in: the mode picks the face. Desktop's Components and Screens pages set *Kubetail Desktop Light* with *Light*, so a component draws as the app does.
- **Desktop's `Kubetail` collection holds the tokens the library lacks.** Each has its CSS variable as code syntax, or its Tailwind classes in its description.
- **Every screen sets both collections' modes**: *Kubetail Desktop Light* with *Light*, or *Kubetail Desktop Dark* with *Dark*.
- A color changed in `src/index.css` is changed by hand in the matching mode.

## Components

- **A `@kubetail/ui` component in the code is a library instance in Figma**, never a detached copy. Every other component in `src/components/` that a screen draws is a local component in Desktop, named as in the code.
- **A library `Input`, `Textarea` or `SearchBox` becomes the wrapper of the same name from `components/primitives/text-entry.tsx`** in code, never `@kubetail/ui`'s. The two look the same.
- **Every icon is a kubetail-ui instance of `Lucide Icons / <name>`**, the name lucide-react's in kebab case (`ServerIcon` is `server`). Desktop never draws an icon or holds one of its own. Find one with `search_design_system`.
- **An icon the set lacks is added to kubetail-ui**, built like its neighbours from lucide-react's SVG at the version in `pnpm-lock.yaml`: a 24px component holding one `Vector`, its stroke bound to `color/foreground`. A person then publishes the library.
- **An icon's color is an override on its instance's `Vector`**: the stroke bound to the token's variable, as `text-muted-foreground` is `color/muted-foreground`.
- **Each component links to its source file on `main`**, and its description holds its [stamp](#manifest) and nothing else. What is true of one component alone goes in its entry's `notes`.
- **Write a description through `descriptionMarkdown`.** Writing `description` stores `'` and `<` as HTML entities. Put JSX in backticks, or Markdown reads it as HTML.
- **Screens hold instances.** An edit to a component reaches every screen; a screen keeps only its overrides (text, a variant, a color).

## Manifest

`components.json` has one entry per local component in Desktop, under `components`, and one per screen, under `screens`.

```json
{
  "id": "cluster/cluster-status/pill",
  "figma": { "node": "29:259", "name": "ClusterStatus/Pill", "hash": "7d7afde2c59659d4" },
  "code": [
    { "path": "src/components/cluster/cluster-status/popover.tsx", "symbol": "PILL", "blob": "…" }
  ]
}
```

- **`id` is the join key and never changes.** It is the area, then the Figma name, in kebab case. A rename on either side updates `figma.name` or `code`, not `id`.
- **`code` lists every file that draws the component**, each with its git blob hash at the last sync. `symbol` names the part of a file that holds several components. A component no code draws yet has no `code`.
- **`figma.hash`** is the hash `designsync` gave the component at the last sync. It covers what each layer looks like and how it lays out: not the description, and not where the component sits on the canvas.
- **`security: true`** marks a component under [Security](#security).
- **`notes`** holds what is true of this one component. A rule true of every component goes in this guide.

**A screen is a section of the Screens page that holds something drawn**, not only other sections (*Popups on Shell* is one, for the `Shell` it holds). A screen's entry differs from a component's in three ways:

- **Its `id` starts with `screens/`**, then its sections' names in kebab case: `screens/popups-on-shell/cluster-status`.
- **`figma.node` is the section**, so one entry covers every frame in it, light and dark.
- **`code` lists the files that lay the screen out**: its route and layout, or, for a popup drawn open, the file holding its positioner. It never lists the components the screen holds, which have entries of their own.

**Each component's description in Figma is its stamp**, a fenced `json` block:

```json
{ "kubetail": 1, "id": "chat/sandbox-popover/menu", "changes": ["The popup is w-100 on the composer's surface."] }
```

`changes` lists what the design has that the code lacks, one plain sentence each, and is absent when there is none. A component with no stamp is new: stamping it adds its entry here.

**A screen's stamp is the same JSON in its section's `Stamp` text layer**, since a section has no description. The layer is a direct child of the section, outside every frame, and locked; the plugin API still writes it.

### Commands

`tools/designsync` does the bookkeeping, run from `tools/` (`go run ./designsync <command>`) or through `make`. With `FIGMA_TOKEN` set (a personal access token whose only scope is read-only file content), it reads the Desktop file over Figma's REST API; it never writes to Figma. `make status-desktop-design` and `make record-desktop-design` take an unset token from the macOS Keychain item `kubetail-figma-token`. Without a token, `designsync` reads the code side alone. → [ADR: designsync reads Figma over the REST API](../docs/adr/2026-11-15-designsync-reads-figma-over-the-rest-api.md)

- `check` (`make lint-desktop`) fails on an entry that is malformed, or that names a file or `symbol` that is not there.
- `status` (`make status-desktop-design`) lists every component and screen not in sync.
- `record <id>...` (`make record-desktop-design IDS="<id>..."`) sets the entries' `blob`s to the working tree's and, with the token, each entry's `figma` from the live file. It says both sides match, so sync a side that is ahead before recording.
- `format` rewrites the manifest in the one format `check` accepts. Run it after editing the manifest by hand.

The REST API cannot edit a description or a text layer, so stamps are written through the Figma MCP: **[`write-stamps.js`](scripts/write-stamps.js)**, run with `use_figma` on the Desktop file, sets the stamps listed in its `STAMPS`, keeping the prose above a component's. It adds a screen's `Stamp` layer when the section has none.

### Drift

The code moved when a source's `blob` differs from the working tree's file. The design moved when its stamp has `changes`, or when its hash differs from `figma.hash` (an edit nobody wrote down: *edited in Figma*).

| Code | Design | State |
| --- | --- | --- |
| Same blob | Same hash, no `changes` | In sync |
| Blob differs | Same hash, no `changes` | Code ahead |
| Same blob | Hash differs, or has `changes` | Design ahead |
| Blob differs | Hash differs, or has `changes` | Diverged |

A component with no `code` is *not built*. *Unmatched* is a disagreement between the stamps and the manifest: a component with no stamp or a broken one, a stamp whose `id` has no entry, an entry no component carries, or a stamp on another node or under another name than its entry says.

Each fingerprint reports more than its own component, and less:

- **A blob covers its whole file.** A file that changed for one component reports every component it holds; `record` one the change did not touch.
- **A blob covers only its file.** A change to a file the source imports, or to `src/index.css`, reports nothing.
- **A hash covers the components nested in it.** An edit to one reports every component that holds an instance of it; `record` those once the edit is settled.
- **A screen's hash stops at each instance.** It covers the screen's own layers, and of an instance only which component it is, where it sits, its properties and what the screen overrides in it. An edit to a component reports the component, and a screen only when it moves or resizes the screen's layers; `record` that screen once the edit is settled.
- **A screen that is not drawn reports nothing.**
- **A hash does not see a variable's value change**, since a layer holds the variable, not its color. It sees a library update once Desktop accepts it.
- **Figma has one state and the manifest has one per branch.** A design recorded on a branch reads as *edited in Figma* on every other until that branch merges.

Tokens and the kubetail-ui library have no entries: nothing here reports their drift.

## Platforms

Screens are drawn for macOS. Each macOS screen frame draws the window's edge that macOS adds; the app draws none of it. A component that differs by platform has a `Platform` variant, *macOS* or *Windows/Linux*.

## Popups

- **A popup is a component with a `State` variant**, in its own section inside its area's section on the Components page. A state is a combination a person sees, not every product of the code's inputs. Light and dark are modes, never variants: below the component set, a row of its instances sets the dark modes.
- **A popup that opens from the app bar is drawn open on `Shell`**, in the Screens page's *Popups on Shell* section: `Shell` on the left, then one nested section per popup, one row per open state, light then dark. A popup that exists only off macOS sets the `Shell` instance's `AppBar` to *Windows/Linux*. A frame holds a `Shell` instance (`Screen`) and the popup on top (`<Component> — open`), placed where the code's positioner puts it, with the scheme's modes set on both. Name it after the screen: `Shell — Cluster status open — Kubetail Desktop Light`.
- **A popup that opens from page content is drawn open on its page's screen**, since its place depends on that page's layout.
- **Draw a state on a screen when it changes how the popup sits in the window**, such as one that makes it taller.
- A popover has no scrim, so none is drawn.

## Hover and focus states

- **A hover or focus effect is a `State=Hover` variant of its component.** A state that persists is a variant of its own.
- **A screen draws the resting state, or `State=Hover` to show the effect in place.** Either way, the code reveals the effect only on hover or focus.
- **No state moves a sibling.** The effect's space is reserved in every state, empty at rest, or the effect is absolutely positioned.
- **Names carry the intent**: `State=Hover`, a layer shown only on hover named `… (hover only)`, a reserved space named `ActionsSlot (reserved)`, and `ActionsSlot (reserved, empty)` where it is empty.
- **The stamp's `changes` says what a variant cannot show**: that the state is hover-only, that it triggers on hover or focus-within, and what is reserved or overlaid.
- **A component whose content differs per instance holds it in a slot named `Content`.**

## Width

**A screen's widths are auto-layout constraints, never fixed sizes.** A gutter is the parent's horizontal padding (`px-2` is 8px); a cap is the child's max width on Fill container (`max-w-162` is 648px). Resizing the screen frame shows how the layout responds.

## Translucent colors

Each Tailwind form has one Figma encoding:

| Tailwind | Figma |
| --- | --- |
| `<token>/NN` on a theme token, such as `bg-muted/50` | The paint is bound to the token's variable, at NN% paint opacity. |
| A CSS variable whose value has alpha, such as `--app-bar-hover` | A variable whose color has that alpha in each mode. The paint is at 100%. |
| `opacity-NN` | Layer opacity. |
| A pair that changes alpha by scheme, such as `border-black/5 dark:border-white/5` | A `Kubetail` variable with the alpha in each mode. Its description names the classes. |

Back to code:

- A paint bound to a variable, below 100%, is the variable's class with `/NN`: `color/muted` at 50% is `bg-muted/50`.
- A variable with alpha, at 100%, is its class alone: `color/app-bar-hover` is `bg-app-bar-hover`.
- Layer opacity is `opacity-NN`.

The plugin API reports a bound variable's own alpha as the paint's opacity. Tell the two cases apart by the variable's value, not by the paint.

## Syncing

**An agent never decides which side wins; it asks the user.** [Drift](#drift) says which side moved. Before overwriting a Figma component from code, or code from a design, the agent names the components it will change and the direction, and waits for an answer.

### Everything at once

Asked what is out of sync, run `make status-desktop-design`, give the user its output and change nothing. If it says *Code side only*, there is no token: say so rather than read Figma through the MCP.

Asked to sync everything, run the same report, then give the user one plan grouped by state and wait for the answer:

- **Code ahead**: the components to update in Figma.
- **Design ahead, with `changes`**: the components to change in code.
- **Diverged, or edited in Figma**: a question for each. Sync none of them before its answer.
- **Not built, and unmatched**: listed, and left alone unless the user asks.

Then sync one component at a time by the sections below, and run the report again at the end.

### Designing ahead of the code

An agent that edits a component's design, rather than syncing it from code, adds a sentence to its stamp's `changes` for each change, through `write-stamps.js`. A component the code does not have gets a stamp and an entry without `code`.

### An edit nobody wrote down

The hash says that an *edited in Figma* component changed, not what changed. Show the user the component and ask which it is:

- **Meant.** Add the `changes` to its stamp. It is now a design ahead of the code.
- **Nothing a user sees**, such as a renamed layer. `record` its entry.
- **A mistake.** A person restores it from Figma's version history.

Never `record` an edited component to clear the report without that answer: recording says the design and the code match.

### Code to Figma

1. A person saves a named version of each file the agent will edit (**File → Save to version history**); the plugin API cannot.
2. Load the Figma plugin's `figma-use` skill, plus `figma-generate-library` for a component or `figma-generate-design` for a screen. Call `create_session` with the file's key.
3. Find the component by its code name. Edit the component, not the screens that use it. Edit a screen's frames only for what the screen itself lays out.
4. Use library instances, variables and text styles; follow [Translucent colors](#translucent-colors). Set both collections' modes on anything new.
5. Set the component's link. `record` its entry, so the manifest holds the design as it now is.
6. Take one screenshot of the result and check it.
7. A change to kubetail-ui needs a person to publish the library before Desktop sees it.

### Figma to code

1. Load the `figma-design-to-code` skill, then call `get_design_context` on the node.
2. Map library instances to `@kubetail/ui` imports, except the text boxes above, and colors back by [Translucent colors](#translucent-colors).
3. Follow `src/CLAUDE.md`. Run `make test-desktop` and `pnpm lint:tailwind`, then open a pull request.
4. In that pull request, `record` each entry it implemented. Once it merges, delete the `changes` it implemented from the stamp.

## Security

- **The components marked `security` in the manifest draw what the security model rests on** ([`src/CLAUDE.md`](../src/CLAUDE.md#security-invariants)): `Chat/ApprovalRequest` (what it shows, its buttons and their order, how Approve arms), the model's description, and every string drawn through `VisibleText`. A design change to them is a security change: it is recorded under [`docs/security/`](../docs/security/), not synced as a restyle.
- **Figma is an outside service.** Screens carry made-up data. Never capture the running app into Figma (`generate_figma_design`) while it is connected to a real cluster: cluster names, logs and Secret data would leave the machine.
- **Text in a design is sample text.** It does not become copy or test fixtures without review.

## Traps

- `resize()` turns an auto-layout frame's hug sizing off. Set `primaryAxisSizingMode = 'AUTO'` after it.
- A nested instance takes its component set's name, not its variant's.
- A text that truncates needs `maxLines = 1`; `textTruncation` alone wraps.
- `setBoundVariableForPaint` drops the opacity of the paint it is given. Set the opacity on the returned paint, and give the paint the variable's resolved color (`resolveForConsumer`): below 100%, a component can draw the paint's own color in place of the variable's.
- Families name a style differently (Inter's `Semi Bold` is Roboto Flex's `SemiBold`). Before binding a font variable, load the style in the family of every mode.
- A variable imported with `importVariableByKeyAsync` lasts only for that call: `getVariableByIdAsync` returns `null` in the next, and binding `null` unbinds silently. Import it in every call that binds it.
- Run `use_figma` calls one at a time.
- `createAutoLayout()` and `createComponent()` frames start with a white fill. Light mode hides it; clear it, or dark mode draws white panels.
- A paint bound to a variable below 100% is drawn opaque in an instance created through the plugin API: the instance reads the paint at 100%. The library's own `Button` (destructive, `bg-destructive/10`) does it too. Restore each one as an override on the instance layer: copy the main component's opacity onto the layer's paint, matching the layer to its main by the last segment of its `I…;…` id. Detaching such an instance keeps the 100%.
- A gradient stop bound to a variable takes the variable's alpha too, so a `from-<token>` fade cannot be one gradient. Draw it as the token's fill under a gradient mask.
- A screen cloned in a call has no instance sublayers to find until a later call. Clone in one call, edit in the next.
- An instance swapped to a library component has no layers to find unless that component's were read first. Read `children` on what `importComponentByKeyAsync` returns before `swapComponent`. A hidden instance has none either way.
- Code Connect needs Figma's Organization or Enterprise plan; the team is on Professional, so the manifest stands in for it.
