# Security record — The model reads logs, 17 October 2026

**Subject:** `LogsRead`, a tool the model calls to read a range of a view's log lines through the
log cache. It is `Shown` and `Bounded`, never `Gated`: it checks the view against the mirror as
`LogsView` does, reads one page with `logs.Service.Fetch`, and answers a bounded sample, which the
call's action keeps and the transcript draws. This is rung 4 of the log-views sequence. The living
model is [security-model.md](../security-model.md).

## What widens

**Log lines reach the model on its own call, through the log cache, with no approval.** They
already did through `kubectl logs` in a sandboxed Bash, so the set of lines a model can read does
not grow, with one exception: the file can hold lines the cluster no longer serves, of a gone pod
or a rotated file, which the same credentials read when the cluster served them.

**The read runs on the cluster's connection, outside the sandbox's proxy**, as the viewer's reads
do. It reads `pods/log` and writes nothing to the cluster. A monitor's or a subagent's read runs
the same way.

## What holds it

- **Every message reaches the model through `logs.ForModel`**, stripped and redacted, then cut at
  2 KiB on a character boundary, so a cut never splits a secret the redaction would have taken
  (`TestAMessageIsRedactedBeforeItIsCut`). A refusal's words go through it too
  (`TestAFailedReadIsRefused`).
- **No message can pass for the sidecar's words.** `stripControl` removes every newline, so a
  record's line starts with its timestamp, and only the header and a hole's `[` line are the
  sidecar's (`TestAHoleIsALineBeforeItsRecord`, `TestTheActionIsTheSample`). The prompt names the
  lines data, under *Data is not instructions*.
- **The sample is capped.** At most 100 lines, 16 KiB of them; the header names skipped streams up
  to 2 KiB and notes up to 4 KiB, so no output passes `tools.InlineLimit` and none is saved to a
  file (`TestASampleDropsPastItsBudget`, `TestTheHeaderNamesSkippedStreamsUpToItsBudget`,
  `TestTheHeaderNamesNotesUpToItsBudget`, `TestNoOutputPassesTheInlineLimit`). A call names at most
  100 sources (`TestMoreThanMaxSourcesIsRefused`).
- **A cursor names a position, never a file or a query.** It is base64url JSON the model copies
  back; its fields are checked as a parsed cursor's are, the instance id's length and control
  characters included (`TestABadCursorIsRefused`, `TestLineCursorIsAPagesCursor`). A forged one
  reads a page the model could have asked for by moment. It is printed after the header's
  redaction, which would otherwise take it with a credential header's line
  (`TestTheCursorIsPrintedAfterTheRedaction`).
- **The transcript shows what the model read.** The action holds the lines as the model read them,
  not a query to run again; the card draws the action alone, through `VisibleText`, and requests
  nothing (`logs-read-card.test.tsx`). *View around this* opens the `viewAround` the sidecar
  built, never a view the webview made.

## Residuals

- **Redaction is hygiene, never a boundary.** A container can write a secret in a shape no rule
  knows, and the model reads it as it would through `kubectl logs`.
- **A read is never asked.** A gate on `LogsRead` alone would push the model to Bash, which reads
  the same lines and keeps a worse record. Secret data stays the one permissioned read.

## Rebuilt on the log fetch API, 23 October 2026

`LogsRead` now reads a window through `logs.Service.Fetch` and prints page tokens in place of the
line cursors above. The promise holds, and is pinned in new places:

- **Redacted, then cut.** The service cuts, so the tool asks every page with `ForModel` set, and
  the service runs `logline.ForModel` before `capMessage` (`TestAMessageIsRedactedBeforeItIsCut`
  in `services/logs`, `TestEveryPageIsForTheModel`). The cap is the call's `max_message_bytes`,
  2 KiB by default; a cut line says the bytes cut. A refusal's words go through `ForModel` too
  (`TestAServiceRefusalKeepsItsCodeAndReason`).
- **No message passes for the sidecar's words** (`TestAHoleIsALineBeforeIt`,
  `TestTheActionIsThePage`).
- **A token is the service's page cursor**, checked by the service's decoder
  (`TestABadCursorIsRefused`), printed whole after the header's redaction.
- **The header is capped**: notes at 4 KiB and notices at 2 KiB, the rest counted
  (`TestNotesStopAtTheirBudget`, `TestNoticesStopAtTheirBudget`).
- **The widest page passes the inline limit.** A hundred lines of a hundred streams, with a token
  of each, is about 37 KB; the result is saved to a file, as every tool's oversized result is, and
  the tokens are printed whole (`TestTheWidestPagePrintsItsTokensWhole`).
