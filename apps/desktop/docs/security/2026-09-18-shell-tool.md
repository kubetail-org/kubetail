# Security record — the shell tool, 18 September 2026

**Subject:** the OpenAI row's models take the Responses API's own `shell` tool in place of the
function named `bash`. A GPT model's command arrives as a `shell_call` item carrying a **list**
of commands and two caps. The loop fans it out into one `tool_calls` row, one approval and one
card per command, runs the approved ones in order through the turn's one shell session, and
answers the call with one outcome per command. The living model is
[security-model.md](../security-model.md); this record widens
[the bash tool](2026-09-18-bash-tool.md) and
[tools on the Responses API](2026-09-18-tools-on-the-responses-api.md).

## What now runs

**Several commands from one call, each under the same gate.** A `shell_call` is one `tool_use`
named `shell` in the record, its input the action as the API sent it. `callRows`
(`services/chat/tools.go`) expands it before anything is written: one pending row per command, its
`arguments` `{"command": …}` written by the sidecar (`shellCallBlock`) in the one shape
`readBashInput`, `commandLine` and `messageCommands` read, so the card and the transcript agree
with a `bash` row by construction. Every command's card is on the user at once. Each runs only
after the user approves that command, and the ticket chain runs the approved ones one at a time
in command order, whatever order they were decided in. Pinned by:

- `TestAShellCallIsOneCardPerCommand` — both cards are up before either is decided, each row's
  `arguments` is the command verbatim, and the string the shell received is the string the card
  carried.
- `TestADeniedShellCallCommandIsAnEntry` — a denied command does not run, and the one after it
  still waits on its own card.
- `TestACancelMidListRefusesTheRest` — a cancel runs nothing queued.
- `TestABadShellInputIsRefused` — a list past `maxShellCallCommands` (8) is refused whole,
  `bad-input`, since a wall of cards is not the user's to read.

## The caps

The model sends `timeout_ms` and `max_output_length`. Both are requests, never orders: each is
clamped to the sidecar's own (`commandTimeout`; `commandResultBudget` split across the call's
commands), zero takes the cap, negative is bad input, and the model's number is read as bytes so
it never gets more characters than it asked for. `TestAShellCallsCapsAreBounded` pins each arm.
`cutOutput` ends under any budget, including one under its own note's length
(`TestCutOutputEndsUnderAnyBudget`).

## What now leaves

Nothing new. Each entry's `stdout` is the same redacted, cut capture a `bash` result carries,
without the trailer the row's text keeps; the outcome — an exit code, a timeout — rides apart
from it, in the field the API's item has for it. A command that did not run is answered exit 126
with its refusal in `stderr`. The model's `max_output_length` is echoed back on the output item,
as the API asks. `TestAShellEntryCarriesTheOutcome` pins the entries.

## The record

Every reader that keyed a command row on `tool_name = 'bash'` keys on `'shell'` too:
`messageCommands`, `answering.addToolCall` through `isCommandTool`, and `knownTool`, where a
`shell` call is known only under a model offered the shell tool — one offered the function bash
that emits a `shell_call` anyway is refused `unknown-tool`
(`TestAShellCallIsUnknownToAModelOnTheFunction`). `TestAShellCallCommandSurvivesAReload` pins
that the stored read lists a shell command with its output and status;
`TestCommandOfMatchesTheStoredRead` runs every state under both names. `seq` counts across the
whole reply from one place, so a shell call's rows and a refused call's never collide on the
unique index (`TestShellRowsCountAcrossTheReply`).

## Replay

A `shell_call` item goes back verbatim to the provider that wrote it; a call without its form is
rebuilt from the action. A model not offered the shell — a `BashFunction` entry, or a platform
GPT row, whose `withoutHostedTools` clears `Bash` — takes no `shell_call` item, so a shell round
goes to it in the function shape, form or no form
(`TestOpenAIRendersAShellRoundAsAFunctionWithoutTheShell`). Nothing in a replay leaves that was
not already on the record.

## The card

A shell call's commands share one `toolUseID`, and `ApprovalCard` holds `decided` and `shown` in
local state — `shown` being the gate Approve waits on for a folded command. Keyed on the id, the
first card's pressed state would reach the second once the first left the list. So the waiting
list keys on `approvalID`, unique per row and present exactly while the card is drawn, and the
*Commands* list keys on position. `keys two cards of one call on their own approvals` pins that
the second card's buttons stay live and its fold shut after the first is decided.

## Consent

Per command, in the transcript, whatever the call's shape. The list is the wire's; the gate is
per command.

## Note, 19 September 2026

The names moved with *A native tool is a contract*:
`Model.WebSearch`/`WebFetch`/`Bash` are the `NativeServerTools` and `NativeClientTools` lists of contracts
(`anthropic_web_search_20260318`, `anthropic_web_fetch_20260318`, `openai_web_search`;
`anthropic_bash_20250124`, `openai_shell` — the function bash is no contract, and a build offers
none); `Request.WebSearches`/`WebFetches`/`WebFetchTokens`/`Bash` are
`Request.NativeServerTools` (name and caps) and `Request.NativeClientTools`; a `web_search` or `web_fetch` block is a
`server_use` block under that word; `web_searches`/`web_fetches` on `llm_calls` are one
`server_uses` object; `TestAShellCallIsUnknownToAModelOnTheFunction` is
`TestAShellCallIsUnknownToAModelOnTheBash`. The contract's shape — the action's read
(`OpenAIShell.Commands`, `commandtools.MaxCommands`) and the entries (`OpenAIShell.Result`) — is
`sidecar/internal/commandtools`, tested on its own (`TestOpenAIShellReadsTheAction`,
`TestOpenAIShellAnswersOneEntryPerCommand`); the fan-out, the clamp (`clampCaps`,
`TestClampCaps`) and the rows' `{"command"}` (`commandArguments`) are `command.go`'s one path,
shared with the bash call. A `function_call` that spells `openai_shell` — a plain call, not the
API's `shell_call` — fails the reply rather than being read as the contract
(`TestOpenAIRefusesAFunctionCallSpellingTheContract`). The bounds and the gate are unchanged.
