# Security record — bash offered to every turn, 22 September 2026

**Subject:** the webview draws the approval card, and `app.go` puts bash in the box every turn is
offered (`chatTools`, in `sidecar/internal/app/app.go`). A turn on any model whose catalog entry
sets `Tools` can now ask for a command: every Anthropic and OpenAI model, and the flagged models of
seven Chat Completions vendors in `llm/provider.go` — Together's take none. This widens
[the bash tool](2026-09-18-bash-tool.md), which covered the Anthropic row alone. The living model is
[security-model.md](../security-model.md); the gate's shape is
[the gate is the tool's card](../adr/2026-09-22-the-gate-is-the-tools-card.md).

## What now runs

**A command any tool-taking model asks for, under the same gate.** The offer, the gate, the rows
and the timeout are spec 22's and unchanged; the loop asks before any gated call runs, whichever
wire the call came in on, and the card shows the sidecar's own decode of the input. What changes
is who can ask. `TestBashIsOfferedWhenItIsFound` pins the wiring: a bash on `PATH` is offered, and
a machine with none is offered no tool. On Windows `bash.New` trusts Git for Windows' install
record alone, never `PATH` (the bash record's Windows addendum).

**A Chat Completions model's call is read the same way.** Tool calling is the vendor's word per
entry, and a model that writes a malformed input is refused `bad-input` with no card
(`TestABadInputIsRefusedWithoutACard`); the transcript lists that call by its tool's name, tagged
*not run*. A model that writes a well-formed command gets the same card as a Claude model's.

## The card, as built

The card is the user's eye, and Approve is a click on it and nothing else — no keyboard shortcut,
no `autoFocus`, no default button, no form. Its tests are in `chat-transcript.test.tsx`
(`describe('commands')`) and `visible-text.test.ts`:

- **The command is text, exactly.** `draws a card with the command as text and two buttons while
  it waits`; the decision sends the approval id and one boolean (`sends deny for the other
  button`).
- **Every character reaches the eye.** `visibleSegments` spells every `Cc`, `Cf`, `Cs`, `Zl`,
  `Zp` and non-ASCII `Zs` character, every `Default_Ignorable_Code_Point` and the Braille blank
  as its code point, in a `<mark>` apart from the command's own text (`spells every invisible or
  reordering character out`, `spells an invisible character in the command out`). The command
  wraps anywhere and nothing caps its height (`wraps a command with no spaces`).
- **A long command is folded, and Approve waits on it.** Past 2,000 characters or 24 lines the
  rest is behind a button that says how much (`folds a long command and holds Approve until the
  rest is shown`); Deny is never held (`keeps Deny enabled behind the fold`).
- **A choice that no longer exists is not offered.** The buttons are live only on a `Pending`
  approval while the message is `WaitingApproval` (`holds the buttons down on a decided
  approval`); a `false` answer leaves them down (`leaves the buttons down when nothing was
  waiting`); only a failed mutation hands them back (`re-enables the buttons and says so when the
  decision could not be sent`). The card is keyed on its approval, so one card's pressed state
  never reaches the next (`keys the card on its approval`).

A look-alike letter from another script is not made visible; that is the eye's.

## What does not change

The webview's authority is the one [the bash tool](2026-09-18-bash-tool.md) recorded: a script
running in it can call `approvalDecide` itself, so the CSP and the no-remote-script rule are what
keep the eye the user's. Output redaction, the capture limit and the process-group kill are the
tool's, and hold on every wire.
