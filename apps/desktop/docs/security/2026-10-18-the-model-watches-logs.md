# Security record — The model watches logs, 18 October 2026

**Subject:** `LogsWatch`, a tool the model calls to follow a view's logs in the background and be
told once, when the watch ends. It is `Shown` and `Bounded`, never `Gated`: it checks the view
against the mirror as `LogsView` does, opens `logs.Service.Follow` on a context of the watch's own,
and hands the follow to the chat as a background task. The watch ends holding its limit of lines or
bytes, at its time, when the follow ends, or on a stop; its end reaches the model as a notice. This
is rung 7 of the log-views sequence. The living model is [security-model.md](../security-model.md).

## What widens

**A second ungated tool whose end starts a turn.** The first is `Agent`. A watch's end starts a
turn only when the turn that started it answered a user's send, the agents' rule: a watch a notice
turn started rides the next question. So each turn a watch starts is one the user asked for, when
they asked to be told, and no chain of turns runs on with no one asking anything.

**A follow held open on the model's word, with no approval.** It reads `pods/log` on the cluster's
connection, outside the sandbox's proxy, as `LogsRead` and the viewer do, and writes nothing to the
cluster. It is bounded by the chat's 4 task slots, the app's 16, and a day: an `until` past the
cap is cut to it.

## What holds it

- **Every line reaches the model through `logs.ForModel`**, stripped and redacted, then cut at 2 KiB
  on a character boundary (`TestTheByteLimitEndsItOnTheLineThatReachesIt`). The follow's own end
  reason goes through it too (`TestTheFollowsEndEndsIt`); any other error reaches the model with
  none of its text (`TestAFollowErrorEndsItWithNoText`).
- **No line can pass for the sidecar's words.** A line starts with its timestamp, a hole's with
  `[`; the header is the sidecar's. The prompt names the lines data, under *Data is not
  instructions*.
- **The sample is capped.** At most 100 lines and 16 KiB of them, the line that reaches the byte
  limit included, so no notice passes `tools.InlineLimit` (`TestNoSamplePassesTheInlineLimit`).
- **The notice escapes every field**, the sample included, so nothing a container wrote closes its
  tag (`TestAWatchsNoticeCarriesItsResult`).
- **A cursor is a position**, checked as `LogsRead`'s is, and printed after the header's redaction
  (`TestTheCursorsArePrintedAfterTheRedaction`, `TestBadArgumentsAreRefusedByField`).
- **The chain is cut at the first turn the user did not send** (`TestAWatchFromANoticeTurnStartsNoTurn`,
  `TestAWatchStartsATurnOnlyUnderASend`). A stop starts no turn (`TestAWatchTheModelStopsRidesTheNextSend`).
- **The slots and the cap bound it** (`TestAFifthWatchIsRefused`, `TestAnUntilPastTheCapIsCut`). A
  start the chat refuses lets the follow go (`TestARefusedStartLetsTheFollowGo`), and every stop —
  the model's, the user's, the chat's delete, the app's — reaches the follow
  (`TestTheFollowOutlivesTheCall`, `TestAStoppedWatchKeepsItsLines`).
- **Neither a subagent nor the monitor can start one** (`TestASubagentStartsNoWatch`,
  `TestTheMonitorStartsNoWatch`): neither has a user's turn to start.
- **The card draws the task alone**, through `VisibleText`, and fetches nothing
  (`logs-watch-card.test.tsx`). *View around this* opens the `viewAround` the sidecar built.

## Residuals

- **A pattern that matches at once fills one turn**, which costs a model call.
- **Lines the model reads in a turn it did not send** are cluster text in the message a model
  weighs most, as an agent's report is; the prompt calls them data.
- **Redaction is hygiene, never a boundary**, as for `LogsRead`.

## Rebuilt on the log fetch API, 23 October 2026

`LogsWatch` now follows through the service's new `Follow`, from now, a moment, or a read's
`next` or `follow` token. The promise holds, and is pinned in new places:

- **Redacted, then cut.** The tool opens every follow with `ForModel` set, and the service runs
  `logline.ForModel` before it cuts (`TestAFollowRedactsAMessageBeforeItIsCut` in
  `services/logs`, `TestTheReceiptNamesTheWatch`). The held line keeps the service's cut and says
  the bytes cut (`TestTheByteLimitEndsItOnTheLineThatReachesIt`, `TestAWatchHoldsALineCut`).
- **The sample is capped**: its lines by `max_bytes`, its notices at 2 KiB with the rest counted
  (`TestNoSamplePassesTheInlineLimit`, `TestNoticesPrintInTheHeader`).
- **The header prints no cursor.** The model reads around the lines by their timestamps.
- **A token is checked by the service's decoder**, and a refusal keeps its code with its words
  through `ForModel` (`TestARefusedWatchOpensNothing`).
