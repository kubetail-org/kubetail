---
title: Every tool the model can call is in one box, and what it is follows from what it implements
date: 2026-09-24
scope: sidecar
status: Accepted
amended_by: [The catalog is its own package, and lists every tool each provider is offered](2026-09-24-the-catalog-is-its-own-package.md)
---

# Every tool the model can call is in one box, and what it is follows from what it implements

## Context

The model can call three kinds of tool: the app's own (bash, Read, Write, TaskStop), a vendor's
tool the sidecar runs (none yet), and a vendor's tool the provider runs (web search). The app's
tools were each complete in one place, a `tools.Tool` in `tools.Box`. Web search was split across
four: `llm`'s table of contracts held how the Messages wire spells it and its word; a table in
`services/chat` held its budget and its prompt section; `tools/websearch` read a stored call; and
`app.chatActions()` linked that reader to the contract's name. A second server tool meant touching
all four. The agent carried two offers side by side (`Turn.Tools`, `Turn.ServerTools`), and the
prompt was assembled in two places.

The word (`web_search`) was the key usage and budgets were counted under, and nothing kept two
tools from sharing one. *A native tool is a contract* made "which list a name is in" the
statement of who runs a tool, and kept each contract's wire spelling in a table in `llm`.
[A tool call shows itself from its arguments](2026-09-23-a-tool-call-shows-itself-from-its-arguments.md)
read stored calls through a `tools.Actions` map beside the box. [`llm` is one
package](2026-09-21-llm-is-one-package.md) held that only a `dialect_*.go` file imports a model
SDK.

## Decision

**A tool's kind is what it implements** (`tools/tool.go`). Every tool is a `Reader` (`Name`,
`ActionKind`, `Action`) with a `Prompt`. A `Custom` tool is a `Runner` with a `Definition`; a
`Native` tool is an `llm.NativeTool` (`Name`, `Contract`); the sidecar runs a native tool when it
is a `Runner`, and the provider runs it when it is `Budgeted` (`MaxUses`). No field, enum or list
restates this. `NewBox` panics on a tool that is neither or both, or run by neither side or both.

**One `tools.Box` holds every tool**, in offer order, and the readers of tools this machine cannot
offer (`alsoReads`). `Box.For(target)` chooses a turn's offer: every custom tool and each native
tool `target.Takes`, one tool per `ActionKind`, the first in box order winning. It also returns the
contracts the model lists that it could not offer, which chat logs once. `Box.Action` reads every
stored call, `Box.Runner` finds the tool for a `tool_use`, `Box.Offer` builds the request's
definitions and native offers, and `Box.Prompts` is every section of "What you can do".

**A native tool spells itself on each wire it rides.** `llm` defines one interface per wire
(`MessagesServerTool` in `dialect_messages.go`); the tool implements it in a file named for the
dialect (`tools/anthropicwebsearch/messages.go`); the encoder asserts it. `llm` never names a tool.
`Target.Takes(tool, server)` says whether a target's model lists the tool's contract and its wire
has the interface.

**A tool has three names.** `Name()` is its identity, unique in the box: what every stored call,
row and usage count names. `Contract()` is the vendor shape it speaks, which the catalog lists and
several tools may share. The call name is the vendor's, per wire, unique only within a request:
the decoder matches a call by it and writes `Name()` into the block, and `Stream` refuses a request
that spells one twice. **The word is gone**: usage is keyed by tool name, and the category it stood
for is the `ActionKind`.

**The tool owns its budget and prompt.** `anthropicwebsearch.Tool` holds its cap and its section,
which now sits under "What you can do" with every other tool's.

**The SDK boundary widens.** Inside `llm` a model SDK is imported by a dialect file or the
helpers. Outside it, only a tool's wire file and its test may import one, for its types alone
(the root package and `shared/…`), and neither builds a client or a service.
`TestOnlyAWireFileImportsAModelSDK` walks the module, and
`TestNoDialectReadsTheSDKsEnvironmentDefaults` bans the constructors in a wire file.

This replaces, in the 2026-09-19 ADR, "which list a name is in is the whole statement of who
runs it", the contract table in `llm`, the two `services/chat` tables, and "the reader writes a client
call under the wire's own contract", which becomes "under the offered tool's name". It replaces
the 2026-09-23 ADR's `tools.Actions` map, and the 2026-09-21 ADR's "only a `dialect_*.go` file
imports a model SDK" with the boundary above.

## Alternatives considered

- **A kind field on the tool.** It restates what the interfaces already say, and the two can
  disagree: a tool marked server-run that is also a `Runner`.
- **Keep the codecs in `llm`, in its table.** Adding a tool then touches `llm` and the tool's
  package both, and `llm` names every tool. The interface keeps the SDK's types in `llm`'s
  signatures, but the tool's spelling in the tool.
- **Key usage by contract.** A contract is shared by design (a sandboxed runner for
  `anthropic_bash_20250124` beside the adapter of ours), so two offered tools would share a count.
  The tool's name is the one key unique in the box.
- **Allow two tools of one kind in a turn.** The Messages API reports one `web_search_requests`
  count for every search version offered, so two search tools would each count every call.

## Consequences

- A server tool is one package and a catalog entry, once its wire has an interface.
- `llm` is no longer the only package that imports a provider's SDK. The boundary is a test over
  the module rather than a package border, and a wire file that builds a client fails it.
- `llm_calls.server_uses` is keyed by tool name. Old rows keep the word; nothing reads the column.
- Provider-run rows are still named `web_search`, through chat's `legacyRowNames`, so `Box.Action`
  finds them by contract. A row takes the tool of its name only when that tool speaks the row's
  contract, so a later tool named `web_search` never reads them.
- Box order is policy. Which of two fetches a Claude turn gets is decided by where each sits in
  `chatTools`, and that is a security choice as much as a design one.

## Revisit when

A turn needs two tools of one kind at once, or a provider reports usage per tool rather than per
kind.
