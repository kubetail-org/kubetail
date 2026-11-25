# Security record — KubeQuery reads the mirror, 27 September 2026

**Subject:** the `KubeQuery` tool runs one SQLite query the model wrote over views of the chat's
cluster's mirror, unasked, and sends what it reads to the turn's model provider. It is offered on
every machine, to every turn on a model that takes tools, and to subagents. This is the body-read
record [the first tool's record](2026-09-14-list-objects-tool.md) said would come before one
landed. The living model is [security-model.md](../security-model.md). → [ADR: KubeQuery reads
views on a connection of its own](../adr/2026-09-27-kubequery-reads-views-on-a-connection-of-its-own.md),
[ADR: KubeQuery reads without asking](../adr/2026-09-27-kubequery-reads-without-asking.md).

## What goes

Any column of any view: object bodies, events, labels, annotations, status history. Up to 30,000
bytes a call inline, and up to 8 MiB saved to the chat's results, which `Read` opens unasked.
Calls are bounded as every tool's are: 8 tool calls a turn and 16 for each subagent, of any tool,
and a chat runs at most 4 background tasks, agents among them, at once.

Bodies are the store's: Secret values and the fields in `redactions` redacted, `stringData`,
`managedFields` and the last-applied annotation dropped. Then every text cell is redacted as a
command's output is (`TestEveryTextCellIsRedacted`). A JSON cell is redacted by its structure
(`safe.RedactJSON`): each string decoded and read line by line as `kubectl -o yaml` would print
it (`TestRedactJSONReadsEscapedLines`); everything under a key naming a credential, in any case
or spelling (`TestRedactJSONRedactsACredentialMember`, `TestRedactJSONMatchesAKeyBySuffixAndCase`,
`TestRedactJSONRedactsUnderACredentialKey`); and the value of an object named for one, as a Pod's
env var is (`TestRedactJSONRedactsANamedValue`).

## The residual

An env var's value under an ordinary name (`DB_URL` with a password in it) goes as it is, unless a
rule's shape catches it. ConfigMap data, annotations, and CRD fields the table does not list go
as they are, less what the rules catch. A body can carry the API server's URL, which the card
keeps out (`cluster-info` in `kube-public`). The cache file's path, which `pragma_database_list`
shows, is already in every saved result's path. Redaction over-reaches the other way too: an event
message `token: expired` loses its value.

## The guard is the connection and the wrap

Enforced twice, by tests of their own:

- **The connection.** Read-only at open (`TestOpenQueryPoolIsReadOnly`,
  `TestAQueryConnectionCannotWrite`), no attached database, so neither `ATTACH` nor `VACUUM INTO`
  opens another file (`TestAQueryConnectionCannotAttach`), no value past 16 MiB
  (`TestAValuePastTheLengthLimitFails`), and a fresh connection per query
  (`TestAQueryConnectionLeavesNothingForTheNext`).
- **The wrap.** The statement is prepared as a subquery before anything else, so only a query
  compiles: no `PRAGMA`, which can change settings every connection in the process shares even
  when only prepared, no `ATTACH`, no DDL (`TestOnlyAQueryRuns`). It is then prepared alone, so a
  trailing `/*` cannot hide the wrap's tail (`TestACommentCannotEscapeTheWrap`). A `;` may only end
  it (`TestASemicolonBeforeTheEndIsRefused`).

A chat's KubeQuery reads its own cluster alone: the binding fixes the cluster from the chat's
stored record, and the tool names none (`TestATurnsMirrorIsItsChatsCluster`,
`TestASubagentReadsTheChatsMirror`). Rows are withheld while the mirror is `syncing` or `unknown`
(`TestSyncingAndUnknownWithholdTheRows`).

## Injection steers only what is read

A model that read attacker-controlled cluster text can choose which of the mirror's data reaches
the provider, which already has the chat. KubeQuery reaches no other host, file or cluster.

## What a statement can cost

A sort over a large join, or rows of values built to the length limit, fill temp disk until the
10 s deadline stops it: a few hundred MB a call, measured at about 28 MB/s. The row and byte limits
trim the answer, not the work, since the wrap builds its rows before the first is read. No pragma
bounds it: SQLite already spills temp data to disk, `cache_size` does not reach a materialized
subquery, and `temp_store=MEMORY` moves the same bytes into RAM. Memory stays near 60 MB. The
deadline is the bound (**By decision**). `body()` inflates a blob the statement hands it at most 16 MiB and
fails the statement past that (`TestBodyStopsAtTheLengthLimit`), since SQLite checks only a value
already built and cannot interrupt the Go callback. An answer whose column names alone pass what
it may hold is refused within the inline limit (`TestColumnNamesPastTheBoundAreRefused`). The wrap holds up to `limit + 1` rows before the first is answered,
and runs the whole statement in the one step modernc interrupts, so the deadline and a cache's
clear end it (`TestADeadlinePastTheFirstRowInterruptsTheQuery`,
`TestAClearInterruptsARunningQuery`). Local, and bounded per call and per turn.

## Consent

The user's send is the gesture, as it is for the card (**By decision**). The transcript shows every
query, its SQL and what the model read, so what went can be seen.

## Not in this change

There is no off switch yet. It lands before a release build ships, beside memory's
([TODO](../TODO.md)).
