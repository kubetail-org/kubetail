# Security record — cluster writes ask, 29 September 2026

**Subject:** a sandboxed command can change the chat's cluster, one approved request at a time.
The proxy holds each write a foreground call's command sends and puts it to the user as an
approval request; it forwards the bytes it showed once the user approves. This is step 12 of
sandboxed Bash. The living model is
[security-model.md](../security-model.md); the decision is
[a sandboxed command asks for each cluster write](../adr/2026-09-29-a-sandboxed-command-asks-for-each-cluster-write.md).

## What changed

Until now the proxy refused every change ([Bash runs in a sandbox](2026-09-28-bash-runs-in-a-sandbox.md)).

- **The policy.** A `POST`, `PUT`, `PATCH` or `DELETE` of a resource passes `decide` and the
  handler asks (`TestThePolicy`). A self review still passes unasked
  (`TestASelfReviewPassesUnasked`). The `token` subresource of `serviceaccounts` is refused
  whatever the method, since its answer is a credential the model would read
  (`TestEachRefusalIsAForbiddenStatus`).
- **The grant.** A write waits on `Asker`; the upstream sees nothing until it answers, then the
  bytes shown with their length (`TestAWriteWaitsOnTheAsker`). A denial is a 403
  (`TestADeniedWriteIsForbidden`). One write waits at a time, the cluster sees writes in the
  order approved, and a queue past eight answers 429 (`TestWritesAskOneAtATime`,
  `TestQueuedWritesAreBounded`). A waiting write holds no slot (`TestAWriteWaitingHoldsNoSlot`).
- **The refusals.** Unasked, each a 403: a body that is not JSON or YAML, one with a
  `Content-Encoding`, one that is not UTF-8, one past 1 MiB
  (`TestAWriteThatCannotBeShownIsRefused`), or one that does not decode as its media type says
  (`TestAWriteThatDoesNotDecodeIsRefused`); a body carrying `[redacted]` or its base64 in any
  string as the API server decodes it, so a JSON or YAML escape of the mark does not hide it
  (`TestAWriteCarryingRedactedIsRefused`); any write of a helm release Secret, a `PATCH` by name
  included (`TestAHelmReleaseWriteIsRefused`); a query that does not parse, whose broken pair
  would be dropped on the way to the API server, so a `DELETE` shown with a selector could run
  without one (`TestAWriteWithAQueryThatDoesNotParseIsRefused`); every write from a grant with no
  asker, before the queue (`TestAGrantWithNoAskerRefusesWrites`).
- **The wait.** It ends with the client, with the grant, with the run, and at the subagent's
  bound; a wait that ends forwards nothing and is recorded `abandoned`
  (`TestAWriteWhoseWaitEndsForwardsNothing`, `TestAWriteWaitEndsWithTheRequest`,
  `TestAWriteWaitEndsWithTheRun`, `TestASubagentsWriteIsBoundedByItsLimit`). `End` then `Wait`
  join every handler, and the run's proxy closes its connections first
  (`TestWaitJoinsTheHandlers`, `TestTheProxyClosesBeforeItWaits`), so the journal is written by
  one goroutine at a time.
- **Who asks.** A foreground call asks through its runtime; a background task and a run with no
  asker refuse (`TestAForegroundGrantAsksThroughTheRuntime`, `TestABackgroundGrantRefusesWrites`).
- **The record.** A write is a `cluster` approval on its call, with the request as sent; a
  call keeps one approval of its own (`TestACallKeepsOneOwnApproval`,
  `TestACallWithWritesReadsOnce`).
- **The request.** Its heading names what the method does, or *Change in the cluster?* for a
  subresource, which the proxy parsed; *(dry run)* only when the request asks for one; then the
  path through `VisibleText`; the method and the media type, since a subresource's heading names
  no method and the same body patches differently as a merge patch and a strategic one; the body
  folded with Approve waiting on it; and the command under *Sent by* (`cluster writes` in
  `chat-transcript.test.tsx`).

End to end, against a fake API server, with the real sandbox and `kubectl`: a delete waits on one
request showing the `DELETE` and reaches the server once approved
(`TestASandboxedDeleteAsksAndRuns`); a denial comes back `Forbidden`
(`TestASandboxedDeleteDeniedIsForbidden`); a call whose time runs out abandons its write and
leaves the run running (`TestACallTimingOutWhileAWriteWaits`).

## The bound

What the user approves is what the API server receives: the method, the path and query, the media
type, and the body, byte for byte, since a body the request cannot draw as text is refused. Nothing reads what a
write means. The one claim the app adds, *(dry run)*, is read strictly: only a `POST`, `PUT` or
`PATCH` whose every `dryRun` is `All` carries it, never a `DELETE`, whose query the API server
ignores beside a body (`TestADryRunAsks`). It says what the request asks for: an aggregated API
server can ignore `dryRun`.

The command under *Sent by* is the one fold Approve does not wait on. It is context; what is
approved is the request above it.

## Prompt injection

An instruction in cluster text can now make a command ask to change the cluster. Each change
still waits on the user reading the request itself, one at a time, drawn with every invisible
character spelled out.

## Residuals

- **An approved write can bring a Secret to the model.** A Pod that mounts a Secret and prints it
  reads back through `pods/log`, which passes as it is. The manifest is on the request, and the
  user's eye is the gate. Accepted as part of decision 5 (a **By decision** row).
- **A wait that ends answers the command 403** *kubetail: the user did not answer this change.*, so
  a client still listening reads a refusal, never an empty success.
- **`approved` is the decision, not the delivery.** A write approved as its call ended may never
  have reached the server; the command's output says what it read back.
- **The wait counts against the call's timeout.** A model that sets none leaves the user two
  minutes for all of a command's writes. The prompt asks for more.
- **A sandboxed client-side `kubectl apply` of a Secret fails** before it sends anything, since it
  reads the redacted `last-applied-configuration`. The prompt names `--server-side`.
- A write's body stays in `app.db` with its approval, as a `Write`'s content does.
