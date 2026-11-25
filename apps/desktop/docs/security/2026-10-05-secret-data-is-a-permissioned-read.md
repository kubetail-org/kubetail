# Security record — Secret data is a permissioned read, 5 October 2026

**Subject:** a sandboxed read of a Secret's data asks, and passes unredacted once the user allows
it; a helm release write runs once the command read its namespace's Secret data unredacted; a
session can be one that never asks and never reads Secret data. This is step 5A of the
agent-security sequence. The living model is [security-model.md](../security-model.md); the
decision is [Secret data is a permissioned read](../adr/2026-10-05-secret-data-is-a-permissioned-read.md).

## What widens

Before this step every sandboxed read of core `secrets` was redacted, and no helm change ran in
the sandbox. After it, a Secret's values reach the model, and so the provider, once the user
allows it — for one read, for the rest of the command, for the chat, or always for a context and
namespace — and under `Auto` without asking. A helm upgrade runs in the sandbox once its reads were
allowed.

## What holds it

- **The default is redacted.** Every mode but `Auto` asks before a read shows Secret data, and a
  denial, an abandoned wait, a refused read and an asker that fails all pass the answer redacted
  with a 200, so a command gets nothing by asking twice (`TestAPromptedSecretReadWaitsForTheDecision`,
  `TestADeniedSecretReadIsRedactedAndTwoHundred`, `TestAReadOnlyContextRefusesEveryWrite`).
- **A read nobody can see is redacted.** A grant with no asker — a background command's, or a run
  with no one to ask — reads redacted whatever its rules allow, and a read whose record fails is
  redacted (`TestASecretReadWithNoAskerIsRedacted`, `TestASandboxedRunReadsASecretRedacted`).
- **A read that ran unredacted is on screen.** It is recorded on the call with its method and path
  before it is forwarded, and drawn as a line of the call's disclosure, `allowed` with the rule, or
  `approved` with its duration (`TestAnAllowedSecretReadPassesUnredacted`, the Secret read cases in
  `chat-transcript.test.tsx`).
- **The request names what it shows and the scope every rule covers.** *Show Secret db-creds in
  team-a on dev-eks*, *Show Secret data on dev-eks* for a read across the cluster; the chat and
  always rules name the context and the namespace alone, the command rule the verb and the
  resource too (`TestAClassSixVerbIsTheServers`, `TestASecretReadGrantNamesItsScope`,
  `TestAClusterWideListAsksForTheContext`, `TestACommandAnswerCoversTheReadsThatFollow`).
- **A session marked `NoSecretData` reads none**, ahead of every rule and under every mode, and a
  session marked `NoPrompts` asks nothing; both come from the session alone, which the grant copies
  onto the policy (`TestNoSecretDataRefusesClassSixAheadOfEveryRule`,
  `TestNoPromptsRefusesWhatWouldAsk`, `TestSecretDataIsRedactedWithoutTheGrant`). A chat's session
  carries neither (`TestAChatsSessionAsksAndReadsSecretData`).
- **A settings file Kubetail cannot read keeps Secret data redacted**, under `Auto` too
  (`TestHeldRulesKeepSecretDataRedacted`).
- **The helm gate follows what the command read.** A release write after a read that passed
  redacted in its namespace, or across the cluster, is refused unasked
  (`TestAHelmReleaseWriteFollowsWhatTheCommandRead`); a release body carrying the mark inside, one
  that does not decode, one under `stringData` and a JSON patch of one are refused
  (`TestARedactedReleaseIsRefusedUnderTheGrant`); and any body carrying `[redacted]` is refused as
  before (`TestAWriteCarryingRedactedIsStillRefused`).
- **The Settings mode lines say what each mode does with Secret data**, `Auto` showing it unasked.

## What narrows

Nothing. A metadata-only read — `kubectl get secrets`' table, a `PartialObjectMetadata` list — is
redacted as before and asks no one (`TestAMetadataOnlyReadDoesNotAsk`).

## Residuals

- `Auto` allows class 6, as the note's table has it: a context the user put in `Auto` before this
  step now shows every Secret there unasked. The mode line is the one warning.
- In `Ask` every helm command asks, `list` and `status` included; a request answered by reflex
  protects little. The chat rule is the way out, and the prompt says so.
- A rule written from a read across the cluster covers every namespace of the context.
- An approved watch streams every later value under one answer.
- A helm upgrade whose old release's rewrite the mark check refuses leaves two releases marked
  deployed: a history to tidy, never a corrupted value.
- A value copied out of a Secret — a ConfigMap, an env value, a Pod that mounts one and logs it —
  passes as before ([TODO](../TODO.md#security)).
- The output's `safe.Redact` is hygiene: a value with no known shape reaches the model as it would
  from any read.
