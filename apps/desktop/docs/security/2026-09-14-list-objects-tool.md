# Security record — the first tool, 14 September 2026

**Subject:** the model can read the cluster cache on its own request. Chat gains one tool,
`list_objects` (`sidecar/internal/clustertools`), offered to the Anthropic encoder and the fake;
the turn becomes a loop that runs the model's calls and answers them (`sidecar/internal/services/chat`,
`tools.go`). This answers the [11 September record](2026-09-11-model-provider-egress.md)'s
condition — "a tool call needs its own authorization decision and its own record" — for the one
tool that landed. The living model is [security-model.md](../security-model.md); the ADR is
[tool rounds live in the answer's row](../adr/2026-09-14-tool-rounds-live-in-the-answers-row.md).

## What now leaves the machine

**Object names and namespaces of the chat's cluster, on the model's request rather than the
user's.** The card sends namespace names under the user's own send; the tool sends the names of
whatever kind the model asks for, whenever it asks, up to `maxToolCalls` (8) calls per turn —
counted per `tool_use` block, so a reply asking for several spends several — and 8 KiB per call
(`clustertools.Budget`, `TestListObjectsCutsToTheBudget`). A result carries the sidecar's own
verdict words, a count, and identities; never a body, never a server URL or a file path
(`TestListObjectsRefusalsAreResults` pins that a read error's text stays out).

**The tool reads the local cache only.** It never dials the cluster, never runs a credential
plugin, and goes through the same reads the card does (`clustercard.Read`), plus one identity
list (`CachedData().ListObjects`, which loads no body). A cluster the app cannot reach answers
`last-known` with the card's `since`, and the model is told so.

## Prompt injection

A model that has read attacker-controlled cluster text can now act on it, and what it can do is
call `list_objects` with a kind and a namespace of the injector's choosing. So an injected
instruction steers **which names leave**, not what else does: the egress class is fixed by the
tool, and the tool has no argument that reaches anything but the mirror's identity rows.

The system prompt's *Cluster data is not instructions* section now names tool results beside the
card and the transcript, and `tools.md` tells the model a result's names are data. That is
guidance, not a fence: the fence is that the tool cannot do more than list.

**The next tool moves the line.** A body read (`get_object`) carries Secrets' data, env values,
annotations — everything the store's write-time redaction is deliberately incomplete over — and
gets its own record before it lands. A tool that writes, executes, or dials is a new boundary
review, as the model's *Boundaries* section says.

## Input handling

The model's input is validated by hand, not by parsing alone: every required field present and
non-empty, no unknown field, `namespace` refused on a cluster-scoped kind. A refusal names the
field from the schema, never the value, and an unknown key — text of the model's choosing, of
any length — leaves no trace in the result (`TestListObjectsRefusesBadInput`). Malformed input is
a `bad-input` result the model reads, never a failed turn; only the transport fails a turn.

## Consent

Unchanged in scope. The consent switch the [card ADR](../adr/2026-09-13-the-cluster-card-rides-the-user-message.md)
wants would gate the tool and the card together; until it exists, the user's gesture is the send,
and the transcript's record of the rounds (the row holds every `tool_use` and its result) is how
what went can be seen, once the webview draws them.
