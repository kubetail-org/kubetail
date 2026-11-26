# Security record — the prompt names the action, 4 October 2026

**Subject:** a sandboxed command's cluster write is put to the user as the classified action and
a diff the API server computed, with five answers: once, this command, this chat, always, deny.
The three that outlast the request allow a change the user did not see. This is step 4B of the
agent-security sequence. The living model is [security-model.md](../security-model.md); the
decision is [a prompt names the action and offers a
duration](../adr/2026-10-04-a-prompt-names-the-action-and-offers-a-duration.md).

This supersedes the request paragraph of [cluster writes ask](2026-09-29-cluster-writes-ask.md),
and narrows the *Consent* section of [the bash tool](2026-09-18-bash-tool.md): a request may now
write a rule, for a classified action and never for a raw command, which still asks every time
with Approve and Deny alone.

## What changed

- **The heading is the action.** The request names the classifier's summary, *Delete
  pods/api-7f9c in team-a on dev-eks*, written from the parsed path and never from the model's
  text, with *(dry run)* after a dry run's (`chat-transcript.test.tsx`).
- **A diff is a dry run of the exact bytes.** For a `PUT` or `PATCH` of a named object that asks,
  the proxy reads the object and dry-runs the same request, body and media type, with
  `dryRun=All` beside the query, and diffs the two as YAML (`TestADiffForAPutShowsTheChange`,
  `TestADiffForAPatch`). Only a write that asks is previewed (`TestOnlyAPromptedWriteIsPreviewed`),
  only on a group version the API server serves itself (`TestAnAPIThatMayIgnoreADryRunIsNotPreviewed`),
  and never a `POST` or a `DELETE` (`TestAPostOrADeleteHasNoDiff`). Both requests go through the
  endpoint, a slot and the limiter, are bounded in time and size, and fail to a reason the
  request draws (`TestADiffUsesTheEndpoint`, `TestTheDiffsRequestsAreBounded`,
  `TestAFailingDryRunIsReportedAndTheWriteStillAsks`).
- **The diff hides what must not be shown.** A Secret's values read `[redacted]`, and
  `[redacted: changed]` on the side that changes them (`TestADiffOfASecretMarksTheChangedKeys`);
  every kind's last-applied annotation is redacted the same way, and a change to it alone still
  reads as one (`TestADiffHidesTheLastApplied`). What the server assigns is dropped, `status` is
  kept (`TestADiffDropsWhatTheServerAssigns`, `TestADiffShowsAStatusWrite`).
- **The raw request is one fold away.** With a whole diff the request sits under *Show the
  request*, and Approve waits on the diff; with none, or one cut at 2,000 lines, the request is
  drawn open and Approve waits on its body too (`TestADiffIsCutAtTwoThousandLines`,
  `chat-transcript.test.tsx`).
- **A rule is offered only where a grant can lift the verdict.** `permissions.Grantable` is an
  `Unmatched` verdict, not a dry run, with a context (`TestGrantableIsUnmatched`,
  `TestADryRunIsNotGrantable`, `TestADryRunAsksWithNoGrant`). Class 5, an `AskFor` rule and a
  dry run offer Approve once and Deny alone, and the sidecar refuses any other answer to them, and
  to a call's own request, with the request still waiting
  (`TestAnUngrantableActionTakesOnceOrDenyAlone`, `TestACallsOwnApprovalTakesOnceOrDenyAlone`).
- **A rule is scoped as the button says.** A chat or always rule names the context and the
  namespace as literals, and for a cluster-scoped action or a Namespace object the group and the
  resource too (`TestAGrantRuleKeepsTheLiteralScope`, `TestAClusterScopedGrantNamesTheResource`).
  Any other chat or always rule is `Inside`: it covers what is in the namespace and never the
  Namespace object, and its line reads *inside* (`TestAGrantRuleKeepsTheLiteralScope`).
  A command rule also names the verb and the resource, lives on the command's grant and ends with
  it (`TestACommandRuleNamesTheChange`, `TestACommandAnswerAllowsTheRestOfTheCommand`,
  `TestACommandRuleEndsWithTheGrant`). Each rule's line reads one way whatever a value holds
  (`TestALineReadsOneWay`, `TestALiteralFieldReadsQuoted`), and is drawn through `VisibleText`.
- **A rule is written before the decision lands, and seen where it can be removed.** A chat rule
  is a `chat_grants` row listed beside the composer; an always rule is in Settings
  (`TestChatWritesAGrantBeforeTheDecisionLands`, `TestAlwaysWritesTheRuleIntoTheSettings`,
  `TestAChatsGrantsAreListedAndRemoved`, `chat-grants.test.tsx`). An answer claims its request
  before any write, so two answers write one rule (`TestApproveClaimsTheWaiterBeforeWriting`), and
  `Always` is refused while the settings hold rules Kubetail cannot read
  (`TestAlwaysWaitsWhileTheRulesAreHeld`).
- **The record keeps the duration.** An approval stores `once`, `command`, `chat` or `always`, a
  denial none (`TestOnceWritesTheDurationAndNoRule`, `TestADenialRecordsNoDuration`,
  `TestTheApprovalChecksHold`), and the call's disclosure tags it.

## The bound

What the user decides on is still the request: the action, and the diff the API server computed
from the bytes it will receive. An allow answer is a click on that request, armed only once its
place holds, as Approve is. A rule never reaches past a forbid, a dry run or a missing context,
and every rule it writes is on screen and removable.

## Prompt injection

An instruction in cluster text can make a command repeat a change the user allowed for the
command, or send one in a namespace they allowed for the chat or always, with nobody asked. It
cannot widen what a rule allows, and every write it makes is recorded and drawn.

## Residuals

- **A dry run's body reaches admission webhooks for a write the user may deny.**
- **An `Allow` for a namespace covers every write there**, a Pod that mounts a Secret or a
  privileged Pod included; only the cluster's admission stops those. A rule written in Settings
  covers the Namespace object too, its `pod-security.kubernetes.io/enforce` label included; a rule
  an answer writes does not.
- **`managedFields` are dropped from both sides**, so a client that sets them changes them with no
  line in the diff; the raw request shows them.
- **A cluster-scoped chat rule covers every object of its resource in the context**, as its line
  says.
- **Allow for this command approves objects the user has not seen**, how many unknown until the
  command ends; the rule's line, not the object, is what it allows.
- **The diff is a preview, not a lock.** An object that changes between the dry run and the
  forward takes the patch as it is then.
- **A rule is written before the decision reaches the turn**, so a cancel that wins the turn
  leaves it in place; the user removes it in the chat's list or in Settings.
