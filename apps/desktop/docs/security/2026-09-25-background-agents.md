# Security record — an agent runs in the background, 25 September 2026

**Subject:** an `Agent` call answers at once with the agent's id, and the subagent runs as a
background task of the chat, beside the parent's answer and after it. Its report reaches the parent
as a notice, and its gated calls wait on the user whatever the parent is doing. This record amends
[the Agent tool](2026-09-25-agent-tool.md); the living model is
[security-model.md](../security-model.md), and the decision is
[an agent runs in the background](../adr/2026-09-25-an-agent-runs-in-the-background.md).

## What bounds an agent now

Step 1 bounded agents by one turn's calls: the parent waited, so a turn could run 8 subagents one
after another. Now an agent outlives its call, and three bounds hold instead.

**Slots.** A running agent takes one of the chat's background-task slots, shared with commands:
at most 4 run at once in a chat and 16 in the app. A call past that is refused in words the model
can act on (`TestALimitIsARefusal`), and four agents leave a subagent no slot for a background
command of its own, which its prompt says.

**No chain past the turn an agent's end starts.** `Agent` is ungated, so a notice turn that could
launch an agent whose end starts another turn would run on with no one approving anything. An
agent's end starts a turn only when the turn that launched it answered a user's send — its trigger
carries a request key (`TestACompletedAgentStartsATurn`, `TestAFailedAgentStartsATurn`). An agent a
sidecar-started turn launched rides the next question (`TestAnAgentFromANoticeTurnStartsNoTurn`).
The background-commands record's "bounded by approvals" covers a command's chain, since each
command needs one; this is what covers an agent's.

**A request stands for 30 minutes at most.** A subagent's request waits until the user answers it,
the agent is stopped, or 30 minutes pass; then the agent is stopped `unanswered`, its slot freed, and
its approval left `pending`, which `approvalDecide` answers false for
(`TestAnUnansweredRequestStopsItsAgent`). Without the bound, a request in a chat the user is not
looking at would hold a slot for good. The parent's own request is not bounded: it holds the turn,
which the user sees and can cancel (`TestTheParentsOwnRequestWaits`).

## Requests arrive with no answer streaming, several at once, and out of view

**A subagent's request can wait under an answer that has settled.** Only the subagent's run flips to
`waiting_approval` (`TestASubagentsGatedCallWaitsOnTheUser`). The message's `awaitingApproval` is true
while any run of it waits (`TestAWaitingRequestMarksItsMessage`,
`TestASubagentWaitingMarksItsSettledAnswer`), and a request's buttons are live on a `Pending`
approval while it is — never off the message's status, which a settled answer's request never
meets (`draws a request under a settled answer, live while the answer awaits approval`). A stranded
run fails at start, so a request a restart left is down; the gate decides only what is drawn, and
`approvalDecide` answers false for an id nothing waits on.

**Every waiting request is drawn, in the order asked.** Each is keyed on its approval and Approve is a
click on the request it sits in (`draws every waiting request, each Approve reaching its own id`).
Approval ids are UUIDv7, so a new request lands below the ones already there
(`draws the requests in the order they were asked`).

**Approve arms only once its place on screen has held for 500ms.** The page moves around a waiting
request now: the answer above it grows, the transcript follows a growing answer, a request or a
block above it comes or goes. `useHeldStill` reads the buttons' rect every animation frame, and any
move starts the wait again (`useHeldStill`'s cases; the transcript's `arms Approve once its place has
held for the wait, and never holds Deny back`, `disarms Approve when the page moves it, and arms it
again once it holds`). A user's own scroll re-arms it too, which costs them half a second. Deny is
never held back: refusing the wrong request costs one more question, not a command.

**A request out of view is pointed to, never approved from.** While a message other than the last
awaits approval, the composer draws *An agent is waiting on you.* and a Show that scrolls to the
first such request (`points the composer to a request waiting in an earlier message, and scrolls to
it`, `says when a request waits out of view, and Show only shows it`). The chat list marks a chat
with a request waiting (`marks a chat a request is waiting in, on either mode`), and every write
that moves a run into or out of `waiting_approval` pings the list, so the mark clears when the agent
stops (`TestAWaitingAgentMarksItsChat`, `TestAStrandedWaitClearsTheChatsMark`). The mark shows only in
the list for that chat's mode and cluster; the 30-minute bound is what holds for every other.

## The report reaches the model in a user message

A report was a `tool_result` in step 1. Now it is a notice, which rides a question or is one, so it
reaches the model in a user message — cluster data one step removed, in the message a model weighs
most. It goes in a `<result>` element through `noticeEscaper`, like every field, so it cannot close
the tag or open another (`TestACompletedAgentsNoticeCarriesItsReport`, `TestAResultCannotCloseItsTag`),
and the prompt calls it data (`agent.md`). The transcript draws the report off the call's task,
never the notice, and never the launch text the model read (`draws the report off its task, never
the launch text the model read`).

## A Cancel leaves agents running

A Cancel stops the answer, never an agent it started (`TestACancelLeavesTheAgentRunning`). An agent
that ends after the Cancel starts a turn, since the user's send launched it, so the model can speak
again after the user pressed Cancel; one that ended before rides the next question
(`TestAnAgentsReportRidesTheNextQuestion`). Stop on the call stops an agent, as it does a command
(`TestStopBackgroundTaskStopsAnAgent`), and so does the app's stop
(`TestTheAppsStopRecordsItsStopOnAnAgent`); a chat's delete stops every task of the chat, agents
among them. An agent a crash left running is lost at the next start
(`TestAStrandedAgentIsLost`).

## A subagent's TaskStop reaches only what it started

Step 1's record noted that a subagent's `TaskStop` stopped any task of the chat. A stop by the model
is written notified, because its result told it, and only the model that called it read that
result: a subagent stopping a sibling, or its own agent, would leave the parent waiting for a notice
that never comes. A subagent's stop now reaches only the tasks its own run's calls started; the
parent's reaches every task of the chat (`TestASubagentStopsOnlyWhatItStarted`,
`TestTheParentsTaskStopReachesASubagentsCommand`).

## What a start that fails leaves

A start whose rows do not land is the call's refusal, and the turn goes on, since nothing ran
(`TestAFailedStartAnswersCouldNotStartAndTheTurnGoesOn`). A Cancel during the start starts nothing,
and the run, its link and the task row are taken back in one transaction
(`TestACancelDuringTheStartStartsNothing`, `TestAFailedStartTakesBackTheRunAndTheLink`). A write that
fails inside a subagent ends it alone, `failed` (`TestAWriteFailedInASubagentFailsItAlone`), and the
task's end write heals its rows (`TestTheAgentsEndHealsItsRows`, `TestAFailedFinalFinishWriteKeepsTheReport`).
