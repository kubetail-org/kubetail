# Security record — the user turns tools off, 20 November 2026

**Subject:** Settings › Tools, a switch per tool the model can be offered, stored as the
`tool_settings` table in `app.db`. The living model is [security-model.md](../security-model.md).

## What it changes

A turn is offered its provider's list (`catalog`'s `lists`) less every tool the user turned off.
The chat service reads the disabled tools once, where it reserves a turn, and takes them out of
the turn's box with `Box.WithoutNames` (`boxFor`). The turn's subagents take the same read; a monitor run reads them at its
start. A tool turned off leaves the offer and its section of the system prompt.

## What holds

- **A setting only removes.** The lists stay policy: the disabled tools are names subtracted from
  a turn's box, so no value of it can add a tool, or a vendor's tool, to any turn
  (`TestATurnIsNotOfferedAToolTheUserTurnedOff`, `TestASubagentFollowsTheToolSettings`,
  `TestAMonitorRunFollowsTheToolSettings`). Changing that is a security change.
- **The wire takes only listed names.** `toolSettingSet` refuses a name no listed tool has
  (`TestToolSettingSetRefusesANameNotListed`); a stored row naming a tool this build or machine
  lacks is ignored, and `enabled` only ever turns a listed tool off (`TestRowsOutsideTheListAreIgnored`).
- **A running turn keeps its tools** (`TestARunningTurnKeepsItsTools`); a change applies from the
  next turn.
- **Off leaves the turn's runners, not the box's readers.** A stored call of a tool turned off
  still reads and draws; a call of it the model makes anyway, having seen it earlier in the chat,
  is refused `unknown-tool` before anything runs (`TestWithoutNamesDropsTheNamedTools`,
  `TestACallOfAToolTurnedOffIsRefusedAndStillDraws`).

## Residual risk

A setting narrows what the model is offered, not what an approved command can do: with Bash on,
a command can still fetch a page WebFetch would have. A change costs each open chat one prompt
cache miss on its next turn, since the offer and the system prompt are its prefix.
