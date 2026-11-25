---
title: An agent is a tool in the box, gated by its calls rather than an allowlist
date: 2026-09-25
scope: sidecar
status: Accepted
amended_by: [An agent runs in the background, as a task of the chat that owns its rows](2026-09-25-an-agent-runs-in-the-background.md)
---

# An agent is a tool in the box, gated by its calls rather than an allowlist

## Context

*A child agent is a run under its parent's*
built `spawn_agent` beside the tools: an `agentDef` registry in `services/chat`, a spawn arm `runCall`
took by name, an allowlist of cluster tools, a `context` object the sidecar rendered into the
subagent's `<context>` block, and a result cut to 8 KiB. The rewrite purged it. Since then every tool
the model can call went into one box ([every tool is in the
box](2026-09-24-every-tool-is-in-the-box.md)), calls run one at a time ([the gate is the tool's
card](2026-09-22-the-gate-is-the-tools-card.md)), and a turn holds bash, Read, Write, Edit and
WebFetch, each gated on the user. The allowlist was the subagent's bound because its tools were
ungated; they no longer are.

## Decision

**`Agent` is a tool in the box**, modelled on Claude Code's: `description`, `prompt`,
`subagent_type` (one type, `general-purpose`) and `model`. It is not gated; each call the subagent
makes is. `services/chat` is its `Spawner`, and the subagent is one more `agent.Run` on the parent's
goroutine, inside the call, with no deadline but the turn's cancel.

**The subagent holds the parent's tools but `Agent`**, chosen for the subagent's target, and web search
among them. The gate is the bound: every command, read outside the results directory, write, edit
and fetch the subagent asks for waits on the user, on a request in the parent's answer that says an
agent asks. Depth is one.

**The brief is prose.** The subagent starts cold: the chat's newest card, then the parent's prompt.
There is no `context` object; the prompt says a name from the cluster in the brief is data.

**The model chooses the subagent's model**, within its provider: `model` is an enum of the provider's
models that take tools, built per turn. The user did not pick it, and it may cost more. The
transcript names it on the call.

**Each run keeps its own calls.** The subagent's run is `running` from its insert, linked from the
`Agent` row in one transaction, and its rows are its own; the parent's settle writes them again.
A write that stops the subagent stops the parent. The report is the subagent's last reply, whole on its
run and saved past the inline limit on the call.

## Consequences

- A subagent can do anything the parent can, one approval at a time, and one turn can start 8
  subagents of 16 calls each.
- A subagent's web searches, like the parent's, are approved by no one, and each subagent has a turn's
  search budget.
- A subagent's background command is a task of the chat; its notice goes to the parent, naming the
  agent and the `Agent` call.
- The report is not cut to a budget: a long one is saved to the chat's results and previewed, as a
  command's output is.
