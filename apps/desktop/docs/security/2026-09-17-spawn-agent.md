# Security record — the general agent, 17 September 2026

**Subject:** the model can hand a task to a child agent. Chat gains one tool, `spawn_agent`
(`sidecar/internal/services/chat/spawn.go`), offered beside `list_objects` wherever tools are; the child
is a second run of the same loop, under the parent's run, offered the general agent's allowlist —
`list_objects` alone. The living model is [security-model.md](../security-model.md); the ADR is
*a child agent is a run under its parent's*, since removed.

## What now leaves the machine

**Nothing new in kind.** The child reaches the same tool under the same per-call bound — 8 KiB
— on a budget of its own, `maxChildToolCalls` (16) to the parent's 8, so one turn can now send up
to three times the names it could. The parent's `task` and `context` go to the provider in the child's
user turn, as the parent's own text already does in the parent's; both are the model's words
about the cluster, not the cluster's bytes.

**The task is prose the model wrote after reading the cluster.** That is the one new path: cluster
text can reach a model's instruction turn rephrased, rather than inside a `<context>` block or a
tool result the prompt names as data. `general.md` says the task and any name in it are data too,
and `spawn.md` tells the parent to put every name in `context`, which the sidecar renders into the
child's `<context>` block (`clustercard.WithSection`) so it reads as the card does.

## Prompt injection

An instruction that rides the task can steer the child, and what the child can do is call
`list_objects`. The bound is the allowlist, not the prompt: `TestTheChildIsOfferedItsAllowlistAlone`
pins that a tool the source defines does not reach the child unless its definition names it, and
`TestAChildAskingToSpawnIsRefused` that a child cannot spawn. So an injected instruction steers
**which names come back** and nothing else — the same line the first tool drew.

**A tool granted to the general agent is a security change.** Its `allowed` list is edited by
hand, and a body read, a log or a metric goes to a focused agent with a structured input, never
here.

## Input handling

`parseSpawnInput` reads one JSON value with no unknown key, a non-blank `task`, and a `context`
that is absent, `null`, or an object under `clustercard.Budget` (`TestABadContextIsRefusedBeforeAnythingRuns`,
`TestABadTaskInsertsNothing`, `TestTheSpawnInputIsOneValue`). A refusal names the field when the
field is the fault and nothing otherwise — never the value — and leaves no child row. The context
is re-marshalled through the card's escaping, so no value can end the fence or close the block
(`TestWithSectionKeepsEveryValueInsideTheShape`).

## What comes back

The child's text is cut to `spawnResultBudget` (8 KiB, the listing's bound) before it re-enters
the parent's prompt or goes on the record, marked `truncated` when it was
(`TestAChildsTextIsCutToTheBudget`). A child that rambles, or is steered into rambling, cannot
inflate the parent's next request past that.

## The record

The child's run is a committed row, linked from the spawning call, before the child's first model
call (`TestTheLinkIsOnDiskBeforeTheChildRuns`); its own calls are rows as they happen; a stranded
child is failed at the next start with its link intact (`TestStartFailsAStrandedChildAndKeepsItsLink`).
Every call the child made is under its own `run_id`, so the audit trail says which agent touched
what.

## Consent

Unchanged: the user's gesture is the send, and the child runs inside it. The transcript holds the
spawn's `tool_use` and its result; the child's own rounds are on its rows, and drawing them is the
webview's follow-on.
