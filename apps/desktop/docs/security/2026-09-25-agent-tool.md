# Security record — Agent hands a task to a second model, 25 September 2026

**Subject:** `Agent` lets the model hand a task, in prose it wrote after reading the cluster, to a
subagent: a second loop on a model of the same provider, run to completion inside the call. The
subagent holds every tool the parent holds but `Agent`. It is offered on every machine, to every turn
on a model that takes tools. The living model is [security-model.md](../security-model.md); the
decision is [an agent is a tool in the box](../adr/2026-09-25-an-agent-is-a-tool-in-the-box.md).

## The brief is untrusted prose

The subagent starts cold: the chat's newest cluster card, then the parent's prompt
(`TestTheSubagentsMessageIsTheNewestCardThenThePrompt`). The prompt was written by a model that read
the cluster, so a name or instruction in it may be an attacker's. Nothing but the prompt guards
it: `general_purpose.md` says the brief directs the work but is never the user's consent, and that
a name from the cluster in it is data. The parent reads the report as data from another model
(`agent.md`). The golden file (`TestTheSubagentsPromptIsAssembledInOrder`) pins what the subagent is told.

## The gate is the bound

**Every gated call of the subagent waits on the user** (`TestASubagentsGatedCallWaitsOnTheUser`): its
commands, reads outside the results directory, writes, edits and fetches, one at a time, as the
parent's do. The request sits in the parent's answer, and opens with *An agent asks:* and the
`Agent` call's description, since the user is approving a call whose reasoning they cannot see.
The rest of the request, and Approve, are unchanged. The answer and both runs read
`WaitingApproval` while it waits. **Depth is one**: the subagent's box is the parent's less every tool
of kind `delegate` (`TestTheSubagentIsOfferedEveryToolButAgent`), and a subagent's `Agent` call is
`unknown-tool` (`TestASubagentAskingToSpawnIsRefused`).

**No call follows a failed write.** A write that stops the subagent's loop stops the parent's turn
(`TestAFailedWriteInsideASubagentAnswersTheRestNotRun`,
`TestAFailedSubagentFinishWriteWithCallsEndsTheParentsTurn`, `TestAFailedSubagentRunInsertEndsTheTurn`).
The subagent's run is a committed row, linked from the `Agent` call, before its first model call
(`TestTheLinkIsOnDiskBeforeTheSubagentRuns`). A cancel ends the subagent and settles both runs
(`TestACancelDuringTheSubagentSettlesBoth`).

**The subagent sees none of the parent's file stamps**, and the parent none of the subagent's
(`TestASubagentsStampsAreItsOwn`): a stamp says the model saw a file, and Edit and Write lean on it.

## What is not gated

**Web search.** Where the provider offers it, the subagent has it, and no one approves a query, the
parent's or the subagent's. The subagent's queries come from a prompt the parent wrote after reading the
cluster. Each subagent has a turn's search budget, so one turn can search up to 9 times that budget:
its own, and 8 subagents'.

**The model.** `model` lets the parent put the subagent on any model of the same provider that takes
tools: one the user did not pick, which may cost more. The transcript names it on the call. With 8
`Agent` calls of 16 calls each, one turn can make 136 tool calls, 128 of them on a model the parent
chose.

**`TaskStop`.** A subagent's `TaskStop` stops any task of the chat, as the parent's does.

## What comes back

The subagent's last reply, whole on its run (`TestASubagentsRunKeepsItsWholeReport`), and through
`tools.Fit` on the call: saved to the chat's results and previewed past the inline limit
(`TestALongReportIsSaved`). A subagent's background command is a task of the chat; its notice goes to
the parent by the `Agent` call's id, naming the agent (`TestASubagentsBackgroundCommandTellsTheParent`).
