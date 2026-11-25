# Security record — The view rides the question, 17 October 2026

**Subject:** a send carries a snapshot of the focused log view, and the sidecar writes it into the
question's context block as a `## View` section: the `LogsView` call that opened the view, what
was on screen, and the lines the user selected. This is rung 3 of the log-views sequence. The
living model is [security-model.md](../security-model.md).

## What widens

**The lines the user selects reach the provider on their send.** Until now no log line reached the
model except through its own commands. The send is the user's act: the line above the composer
says a view will ride, and how many selected lines with it, before they send, and the question's
*Context* disclosure shows what rode after.

## What holds it

- **The sidecar reads the view, the webview names it.** A snapshot carries a `ToolCallID`; the
  section restates that call's stored `shown_action`, never a selector from the client. A call of
  another chat, of another tool, or with no action refuses the send with nothing written, as does
  a snapshot on a send that creates a chat (`TestASendRefusesASnapshotItCannotVouchFor`).
- **Bounds.** At most 50 selected lines, none past the wire's cap on a line, counts never negative
  and both stamps or neither (`TestSnapshotCheckRefusesWhatTheWebviewNeverBuilds`). The section is
  at most 8 KiB as it spells, the view 2 KiB of it, a message 1 KiB
  (`TestTheViewSectionCutsAMessageAtOneKiBMarshalled`, `TestTheViewSectionCountsTheLinesPastItsBudget`,
  `TestTheViewSectionCountsTheSourcesPastItsShare`).
- **No message spells the fence or the block's end.** Every value goes through `fencejson`
  (`TestTheViewSectionEscapesAndCleansMessages`), and the prompt names the selected lines data.
- **A message reaches the model through `logs.ForModel` alone**, which strips control sequences
  again, since this copy came from the webview, then runs `safe.Redact`, before the cut
  (`TestForModelRedacts`, `TestForModelStripsControlSequences`).
- **The section rides that send's turn alone.** A subagent's brief and a notice turn drop it
  (`TestTheSubagentsBriefCarriesNoView`, `TestANoticeTurnDropsTheView`).
- **The disclosure spells what it draws.** `ClusterContext` draws the block through `VisibleText`,
  since JSON leaves a reordering character as it is (`spells a reordering character in a selected
  line`).

## Residuals

- **Redaction is hygiene, never a boundary.** A container can write a secret in a shape no rule
  knows. The boundary is the user's selection.
- **A webview that could forge a snapshot already holds the whole cluster surface.** The check that
  a call is this chat's keeps an honest client honest; it is no defence against code running in
  the webview.
