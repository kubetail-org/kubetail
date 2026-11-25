# Security record — the permissions engine, 2 October 2026

**Subject:** a sandboxed command's cluster write runs, asks or is refused by its class, the
context's mode and the user's rules. For a classified write, a rule or the `Auto` mode can now
let a change run with nobody asked. This is step 3B of the agent-security sequence. The living
model is [security-model.md](../security-model.md); the decision is
a sandboxed cluster write runs, asks or is refused by its class, the context's mode and the user's rules.

This supersedes the *Consent* paragraph of [the bash tool](2026-09-18-bash-tool.md) for
classified actions: a cluster write a sandboxed command sends is decided by the user's mode and
rules, which are settings. A command outside the sandbox still asks every time.

## What changed

Until now every write asked, or was refused when nobody could be asked
([cluster writes ask](2026-09-29-cluster-writes-ask.md)).

- **The engine.** `permissions.Policy.Decide` answers `Allowed`, `Prompted` or `Denied` in a fixed
  order (`TestDecideFollowsTheModeTable`, `TestDenyWinsOverAskWinsOverAllow`). Class 5 asks in
  every mode and is refused in a read-only one (`TestClassFiveAsksInEveryMode`); a class 4 rule
  covers it, and an `Allow` never reaches it (`TestAClassFourRuleCoversClassFive`). A rule names
  a group exactly, and a resource covers its `scale` and no other subresource, so an `Allow` of
  creating pods is not one of evicting them, even when its pattern is a glob such as `pods*`; a
  bare `*` is every kind, so a `Deny` of `*` reaches an eviction
  (`TestARuleMatchesByGroupAndSubresource`). A namespace pattern, `*` included, never matches a
  cluster-scoped write (`TestASetNamespaceSkipsAClusterScopedWrite`); the word `[cluster]`
  matches a cluster-scoped write alone, never a namespace of that name
  (`TestAClusterScopeMatchesOnlyAClusterScopedWrite`). A rule's line names a class 5 rule *destructive*, so
  two rules that differ only in class read differently (`TestARuleReadsAsALine`).
  `*` crosses `/` and `:` (`TestMatchCrossesSlashes`), and a value Kubetail writes is escaped
  (`TestALiteralMatchesItselfAlone`).
- **The classifier.** Each write is classed off its path. A write that can set replicas — of a
  `scale` subresource or of a deployment, stateful set, replica set or replication controller —
  is class 4 only in a form
  the proxy reads whole and that leaves at least one: an object with no `$` directive on it or its
  spec and a literal positive `spec.replicas`, or on a workload none, or a JSON Patch, its keys read
  exactly, that only sets `/spec/replicas` to a positive count or, on a workload, touches paths
  beside it. Every other form is class 5, so a shape the classifier does not know asks rather than
  passes (`TestAScaleIsClassFourOnlyInAPlainForm`). A write of a mutating admission policy or its
  binding is class 5, as a validating one's is, and so is a namespace's `finalize`, which clears
  what holds a terminating namespace and so completes a delete (`TestEveryRequestIsClassified`). A body that
  repeats a key in one object is refused before it is classified, since the API server's typed
  decoder merges what the proxy keeps only the last of: a Namespace's name or a zero replicas
  could otherwise hide in the repeat (`TestAWriteThatCannotBeShownIsRefused`). A write to a core
  Namespace is in that namespace's scope, a create's read off the body's `metadata.name`; a create
  naming it only by `generateName` is in no namespace's scope. A dry run is class 1 on a stable group version the API server serves itself, and
  classed as its write on any other, since an aggregated API, which the aggregator routes by group and version, may ignore `dryRun`; a `DELETE` is
  never one.
- **The proxy.** The write path classifies and decides under the write lock, after the refusals
  that stand (`TestDecideRunsUnderTheWriteLock`). An allowed write is recorded, then forwarded; one
  whose record fails forwards nothing (`TestAnAllowedWriteForwardsUnasked`). A denied write is a
  403 naming the mode or the rule, recorded `refused` (`TestADeniedWriteIsAForbiddenStatus`). A
  read-only context refuses every write and still reads Secrets redacted
  (`TestAReadOnlyContextRefusesEveryWrite`). The mode and the rules are read for the grant's own
  context (`TestTheModeIsReadForTheGrantsContext`, `TestTheGrantDecidesInTheRecordsContext`).
- **The modes.** A context nothing names takes the default mode, `Ask` when unset
  (`TestAContextNothingNamesTakesTheDefaultMode`); a mode set in Settings names one context, ahead
  of every pattern (`TestAModeSetInSettingsWinsOverAPattern`).
- **A value Kubetail cannot read refuses.** A refused `modes` entry makes every context read-only,
  a refused rule refuses every cluster write, and a chat grant that does not decode refuses that
  chat's. A key a rule's type does not name refuses it, so a misspelled scope field never widens
  it (`TestABadModeIsReadOnly`, `TestABadRuleRefusesEveryClusterWrite`,
  `TestAChatsGrantsGoWithTheChat`, `TestAGrantWithAnUnknownKeyRefuses`). A held field's edits wait for the fix or a discard the user
  confirms (`TestAHeldListRefusesItsEdits`, `TestAHeldFieldRefusesAnUnnamedChange`).
- **A background command's write is refused**, whatever the mode or the rules
  (`TestABackgroundWriteIsRefused`).
- **The record.** A write decided with nobody asked is an `approvals` row, `allowed` or
  `refused`, with its reason, against the call that sent it, and the call's disclosure draws it
  with its tag and reason (`TestARecordedWriteNeedsNoWait`, `chat-transcript.test.tsx`).

## The bound

The sandbox is still the floor: a command reaches the cluster only through the proxy, which
classifies mechanically. A rule is scoped, written by the user, and on screen in Settings. Every
write that ran unasked is recorded before it is forwarded and drawn with its reason, and a write
that cannot be recorded does not run.

## Prompt injection

An instruction in cluster text can now make a command change the cluster with nobody asked,
inside a scope the user allowed or a context in `Auto`. It cannot reach a destructive write
unasked, a read-only context, or a `Deny` rule's refusal, and every write it makes is on screen.

## Residuals

- **A write in an allowed scope can reach past it.** A Pod that mounts a Secret and prints it reads
  back through `pods/log`.
- **A dry run's body reaches admission webhooks unasked** (decision 2 of the spec).
- **A bulk delete is decided object by object.** kubectl deletes one object per request, so
  `kubectl delete pods --all` under `Auto` or an `Allow` rule deletes every pod with nobody asked,
  each recorded `allowed`. Only a raw `deletecollection` is class 5.
- **The class 5 list is a list.** A destructive write it does not name, a privileged or `hostPath`
  Pod included, is class 4, which `Auto` or an `Allow` rule runs unasked.
- **No context is production by default.** A production context asks for each write like any
  other until the user sets its mode or adds a `Deny` rule, and a default of `Auto` reaches it too.
- **A mode binds sandboxed commands alone.** A chat switched outside the sandbox can change a
  read-only context once the user approves the command.
