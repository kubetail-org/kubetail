# Security record — outside the sandbox is the user's choice, 30 September 2026

**Subject:** the model can no longer ask for a command outside the sandbox. The user switches a
chat to run its commands outside it, and each such command still asks. This is step 1B of
the build order for [the sandbox, credentials and permissions note](../notes/sandbox-credentials-and-permissions.md).
The living model is [security-model.md](../security-model.md); the decision is
[leaving the sandbox is the user's switch for a chat](../adr/2026-09-30-leaving-the-sandbox-is-the-users-switch-for-a-chat.md).

## What changed

Until now a Bash call with `dangerouslyDisableSandbox` ran outside the sandbox once the user
approved it ([Bash runs in a sandbox](2026-09-28-bash-runs-in-a-sandbox.md)).

- **The flag is bad input.** A call carrying it is refused on every machine, through `Approval`,
  `Run` and `ActionOf` (`TestTheFlagIsBadInput`). The schema, the prompt and the description never
  name it (`TestNoPromptNamesTheFlag`, `TestTheDefinitionIsTheReferences`,
  `TestBashIsOfferedWithTheSandbox`).
- **The runtime decides.** A chat switched outside runs every call outside the sandbox and asks. A
  chat that is not runs a confined call unasked (`TestASandboxedCallAsksNoOne`,
  `TestASandboxedBackgroundCallAsksNoOne`, `TestASandboxedCallRunsThroughTheSandbox`).
- **The turn reads the switch once**, with the context block that tells the model. A switch flipped
  during a turn changes the next turn
  (`TestATurnsRuntimeCarriesItsChatsSwitch`), and a subagent runs under its parent's
  (`TestASubagentsRuntimeCarriesItsParentsSwitch`).
- **The switch is the chat's row.** A chat starts sandboxed (`TestAChatStartsSandboxed`). The
  switch reaches every window through the list watch and leaves `updatedAt` alone
  (`TestTheSwitchIsWrittenAndWatched`). It is refused on a machine with no sandbox
  (`TestTheSwitchIsRefusedWithoutASandbox`, `TestChatSandboxDisabledSetServesTheSwitchedChat`).
- **A send runs where its sender saw it would.** `chatSend` carries the switch the composer
  showed, and the transaction that pins the switch to the turn refuses one that differs
  (`TestASendThatSawTheOtherSwitchIsRefused`, `TestACreateThatSaysOutsideIsRefused`), so a window
  that has not seen another's switch cannot start a turn outside it. A replay is answered by its
  key alone (`TestAReplayIgnoresTheSwitch`). The composer sends what it shows and holds Send until
  the list delivers it (`chat-composer.test.tsx`, `chat-outbox.test.tsx`).
- **Only the user flips it, after a dialog.** Turning it on opens a dialog that says every command
  will run as the user, with their files, credentials and network, after they approve it. Only the
  dialog's confirm calls the mutation. Turning it off needs no dialog (`sandbox-switch.test.tsx`).
- **The model is told.** The context's `## Sandbox` section says which way the chat runs
  (`TestTheContextSaysWhereCommandsRun`). The prompt asks the model to name what the sandbox lacks
  and not to work around it.
- **The heading follows the machine.** Every command's request on a machine with a sandbox reads
  *Run this command outside the sandbox?*, never keyed on the chat's switch
  (`chat-transcript.test.tsx`).

End to end, with the real sandbox and `kubectl`: a chat switched through the mutation asks for a
delete that a sandboxed chat runs unasked, and a denial runs nothing (`TestASwitchedChatAsks`).

## The bound

No chain of approvals leaves the sandbox. The model can ask the user to switch a chat, in words.
Only the user's click on the composer's button, then on the dialog's confirm, switches it. Every
command outside the sandbox then asks with its exact text, as before.

## Prompt injection

An instruction in cluster text can make the model tell the user that a command needs to run
outside the sandbox. It cannot make that command run outside.

## Residuals

- **A switched chat is as open as the bash tool was.** The user switches a chat on and then
  approves a command they did not read closely. This is
  [the bash tool](2026-09-18-bash-tool.md)'s residual.
- **What started under the switch keeps it.** A background command or a subagent started while the
  chat ran outside keeps running outside after the user switches back. Each command still asks,
  and the dialog says so.
- **The model's belief can lag the switch.** A send's turn reads the switch in the transaction
  that writes its context block, so the two agree. A turn a background notice starts writes no
  context, so the model reads the last question's. It errs toward a command that asks or one that
  is refused.
