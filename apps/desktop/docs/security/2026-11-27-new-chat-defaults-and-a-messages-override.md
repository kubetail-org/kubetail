# Security record — new-chat defaults and a message's override, 27 November 2026

**Subject:** the sandbox switch is drawn. A new chat starts with a default the user sets in
Settings, for every cluster or one, and a message can run its own turn outside the sandbox. The
chat's switch is in the composer's sandbox popup. The decision is
[ADR: a new chat starts with its default, and a message can leave the sandbox for its turn](../adr/2026-11-27-a-new-chat-starts-with-its-default-and-a-message-can-leave-the-sandbox-for-its-turn.md),
which amends [leaving the sandbox is the user's switch for a chat](../adr/2026-09-30-leaving-the-sandbox-is-the-users-switch-for-a-chat.md).
The living model is [security-model.md](../security-model.md).

## What changed

Until now every chat started sandboxed, and no control in the app drew its switch
([outside the sandbox is the user's choice](2026-09-30-outside-the-sandbox-is-the-users-choice.md)).

- **A new chat starts with its default.** The cluster's `sandbox.disabled` row, else every
  cluster's, else sandboxed, read in the transaction that creates the chat
  (`TestANewChatStartsWithItsDefault`, `TestScanSandboxDefaultReadsAClustersValue`). A value
  Kubetail cannot read is sandboxed (`TestASandboxDefaultKubetailCannotReadIsSandboxed`), and a
  read that fails is an error, never a default (`TestASandboxDefaultReadThatFailsIsAnError`).
- **A default reaches new chats alone.** Changing one leaves every chat that exists as it was
  (`TestADefaultChangeLeavesExistingChatsAlone`).
- **A create runs where its sender saw it would.** The send carries the default its composer
  showed, and the transaction refuses one that differs from the default it resolves
  (`TestACreateThatSawAnotherDefaultIsRefused`). The composer holds Send until it knows the
  default and while any default's change is in flight (`composer.test.tsx`, `chat-pane.test.tsx`,
  `sandbox-default.test.tsx`).
- **A message's override is its turn's alone.** It runs that turn outside, never moves the chat's
  switch, and the next turn runs where the chat does
  (`TestAMessagesOverrideRunsThatTurnAloneOutside`). Its subagents run outside too
  (`TestASubagentOfAnOverriddenTurnRunsOutside`). The `## Sandbox` section says so for that turn
  (`TestTheContextSaysWhereCommandsRun`). It rides the outbox entry like the network toggle: the
  accepted send clears it, a held send keeps it, drawn with the sandbox it left with and fixed until
  Retry or Discard, and Ask again never sends it (`chat-outbox.test.tsx`, `composer.test.tsx`). Pressing the chat's switch clears it, so a toggle
  hidden while the chat ran outside never comes back armed (`composer.test.tsx`).
- **Network is the sandbox's.** A send that asks for both the override and this turn's network is
  refused, and so is the override on a machine with no sandbox
  (`TestAnOverrideIsRefusedWithoutASandboxOrWithNetwork`). Setting either toggle turns the other off
  (`chat-outbox.test.tsx`).
- **No sandbox, no defaults.** A stored default is ignored and a write is refused
  (`TestASandboxDefaultWithoutASandbox`, `TestANewChatIgnoresItsDefaultWithoutASandbox`,
  `TestSandboxDefaultSetRefusals`), and the composer draws no sandbox control.
- **Every move outside confirms.** The chat's switch (`sandbox-switch.test.tsx`,
  `sandbox-popover.test.tsx`), the message's toggle (`message-sandbox-switch.test.tsx`) and each
  default, every cluster's on the status card (`sandbox-card.test.tsx`, `sandbox.test.tsx`) and every
  reset that lands outside included (`sandbox-new-chats.test.tsx`), open a
  dialog, and only its confirm calls the mutation or sets the toggle. Moving back in is immediate.
  A reset names every cluster's default as the user saw it, and the sidecar refuses one that would
  land elsewhere, so a change made in another window cannot skip the confirm
  (`TestASandboxDefaultResetThatLandsElsewhereIsRefused`).

## The bound

The model still has no flag, and nothing it says sets a default, a chat's switch or a message's
toggle. Only the user's click and the dialog's confirm do. Every command outside the sandbox still
asks with its exact text, as before.

## Prompt injection

An instruction in cluster text can make the model tell the user to start new chats outside, or to
send a message outside. It cannot do either.

## Residuals

- **A default outside opens every new chat.** A user who sets every cluster's default outside and
  then approves commands without reading them runs them unconfined, in every chat they start. This
  is [the bash tool](2026-09-18-bash-tool.md)'s residual, once per chat rather than once.
- **A turn outside has the machine's network.** Nothing outside the sandbox can take it away, so
  there is no network switch there to set.
