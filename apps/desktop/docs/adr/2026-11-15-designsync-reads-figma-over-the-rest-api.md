---
title: designsync reads Figma over the REST API
date: 2026-11-15
scope: repo
status: Accepted
---

# designsync reads Figma over the REST API

## Context

`tools/designsync` compares `design/components.json` with the code and the Desktop Figma file
(`design/CLAUDE.md`). It never talked to Figma. The design side reached it as a *stamps file*: an
agent pasted `design/scripts/read-stamps.js` (about 6 KB) into the Figma MCP's `use_figma`, then
copied the result (about 14 KB) into a file by hand. Every run cost that many tokens twice. The
model retyped the result character by character, so the file carried a checksum to catch typos.
And since the design side needed an agent, nothing could run the check unattended (#899).

Figma's REST API reads the same file with a personal access token. `GET /v1/files/:key/nodes`
returns a page's whole layer tree. With `geometry=paths` it adds each layer's `size`,
`relativeTransform` and path data. Each page's response also maps every component it uses to its
`key` and `description`.

## Decision

`designsync status` and `record` read the Desktop file over the REST API when `FIGMA_TOKEN` is set
(`tools/designsync/figma.go`). The token is a personal access token whose only scope is read-only
file content. Without one, both read the code side alone, as before. `make design-status` and
`make design-record` take an unset `FIGMA_TOKEN` from the macOS Keychain item `kubetail-figma-token`.

`read-stamps.js` is ported to Go: `tools/designsync/design.go` finds the components and screens
and reads their stamps, and `snapshot.go` hashes them. The port keeps what the script decided:

- a screen is a section holding something drawn, not only other sections;
- a screen's snapshot skips each nested section and each `Stamp` layer, and stops at each instance;
- a component's snapshot holds every layer, its instances' included, and leaves out its own place
  on the canvas;
- numbers are rounded to two decimals, and the hash is 64-bit FNV-1a.

The properties are the REST API's counterparts of the plugin's:

| Plugin API | REST API |
| --- | --- |
| `width`, `height` | `size` |
| `x`, `y` | `relativeTransform`, which also carries rotation |
| `strokeTopWeight` and the other sides | `individualStrokeWeights` |
| `dashPattern` | `strokeDashes` |
| `topLeftRadius` and the other corners | `rectangleCornerRadii` |
| `fillStyleId`, `strokeStyleId`, `effectStyleId`, `textStyleId` | `styles` |
| `fontName`, `fontSize`, `lineHeight`, `letterSpacing`, `text*`, `maxLines` | `style` |
| `vectorPaths` | `fillGeometry` and `strokeGeometry`, on layers drawn by a path |
| `getMainComponentAsync().key` | `componentId`, looked up in the page's `components` |
| `descriptionMarkdown` | the `description` in the page's `components` or `componentSets` |

Every other hashed property has the same name in both. The hash adds what the plugin left out:
`characterStyleOverrides` and `styleOverrideTable`, which the plugin reported as `mixed`; `arcData`;
`counterAxisAlignContent`; and the min and max widths and heights, which carry a screen's caps.

An instance's `overrides` name each overridden field but give no value. The value is the
overridden layer's own, found by id in the instance's layers, which the API returns expanded. A
field the API keeps under another name is read there: `width` and `height` from `size`, `text`
from `characters`, `inheritTextStyleId` from `styles`, and font fields from `style`.

`-stamps` and `read-stamps.js` are removed, and with them the checksum. Every entry's `figma.hash`
was recorded again in the same change, with the code and the design otherwise in sync.
`write-stamps.js` stays: the REST API cannot edit a description or a text layer.

## Alternatives considered

- **Keep `-stamps` as an offline path.** `read-stamps.js` would have to hash what the Go port
  hashes. The two APIs name and shape properties differently, so that means keeping two
  implementations in step, tested against each other through an agent. The offline case it would
  serve, reading Figma without a token, still needs the network and the MCP.
- **Fetch the whole file in one request.** `GET /v1/files/:key?geometry=paths` returns every page.
  The two requests cost one extra round trip and skip pages nothing reads.
- **Hash every layer's path data.** A text layer's `fillGeometry` is its glyph outlines, which move
  with font rendering. The hash keeps path data only on layers no other property outlines, as the
  plugin's `vectorPaths` did.
- **Read the token from the Keychain in `designsync`.** It would tie the Go tool to macOS. CI, when
  the check runs there, sets `FIGMA_TOKEN` from a secret.

## Consequences

`make design-status` reports both sides with one command and no model in the loop. The check can
run in CI once it has a token secret and a rule for whether drift fails a build.

A description reaches `designsync` as plain text: Markdown is stripped, so a backtick in a stamp's
`changes` does not reach the report. `status` only counts the changes, and the stamp in Figma keeps
them.

A run downloads both pages with their path data, about 25 MB for today's file.

The hash now depends on what the REST API returns. A property the API adds, renames or starts
returning at a different precision reads as an edit on every entry it touches. Record those
entries again once the cause is known.

## Revisit when

Figma's REST API can write a component's description or a text layer, so `write-stamps.js` can
move into `designsync` too.
