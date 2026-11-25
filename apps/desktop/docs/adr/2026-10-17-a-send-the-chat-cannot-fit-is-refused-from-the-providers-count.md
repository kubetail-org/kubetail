---
title: Refuse a send the chat cannot fit, from the provider's own count
date: 2026-10-17
scope: sidecar
status: Accepted
---

# Refuse a send the chat cannot fit, from the provider's own count

## Context

This decision predates this file; it was recorded in `sidecar/CLAUDE.md` when `roomFor`
(`internal/agent/chat/context.go`) landed. A chat that has grown past a model's context window
fails every turn on that model, and each failure is a paid request the user did not need to make.
The sidecar has no tokenizer, so it cannot measure the next request itself. What it has is the
usage each provider reports on every model call.

## Decision

Every send into an existing chat, and every turn a finished background task starts
(`startNoticeTurn`), is checked by `roomFor` against the box the turn would get. It refuses with
`ErrChatContextFull`, writing nothing, on either of two rules:

- **The chat's newest turn failed with `contextFullText` on its first request, on the same provider
  and model.** A refused request reports no usage, so the count alone would admit the next send.
  The model stays refused until a turn on another model succeeds. A turn that overflowed on a later
  request was pushed over by its own tool results, which the next request drops, so it refuses
  nothing.
- **The tokens the newest reported call of a succeeded turn on the same provider read and wrote
  exceed `contextCeiling`**: the window less `MaxOutputTokens` less `turnInputAllowance`, a sum of
  named terms (the context block at its budgets, a long question, `MaxToolCalls` results at
  `typicalResultBytes`, each `Budgeted` tool's `Allowance()`), all at `bytesPerToken`.

The count is read per provider, because each provider replays the transcript in its own shape, so
only a turn on the same provider is a prefix of the request it gates. It is read through
`messages.run_id`, so a subagent's calls do not count against its parent's chat. A call that ran a
server tool is not counted: inside one request the provider samples again after each server call,
and whether its usage counts the chat once or once per sampling is unverified, so an older call
answers instead.

## Alternatives considered

- **Count tokens in the sidecar.** It would need a tokenizer per provider, kept in step with each
  vendor's. The provider's own reported count is exact for the prefix it read.
- **Rely on the provider's refusal alone.** Every send past the window would be a failed, paid
  request, repeated as often as the user pressed Send or Ask again.

## Consequences

A model with no stated window, one whose window is under the cap and the allowance, and one whose
provider has no counted call on the chat are never refused ahead. Results past the typical size,
notices and failed turns' text are the provider's refusal to catch. A budget change moves the
ceiling, since the allowance is built from the budgets themselves.

## Revisit when

The sidecar gains a tokenizer, or a provider documents how server-tool sampling is counted.
