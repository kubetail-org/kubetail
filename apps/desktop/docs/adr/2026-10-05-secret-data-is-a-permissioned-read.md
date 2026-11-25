---
title: Secret data is a permissioned read
date: 2026-10-05
scope: cross-cutting
status: Accepted
---

# Secret data is a permissioned read

## Context

Until this step every sandboxed read of core `secrets` was redacted, always, and a write of a helm
release was refused outright, since `helm upgrade` rebuilds a release from the Secrets it read
and would write `[redacted]` back. The model had no way to read a Secret's values but to ask the
user to run the command outside the sandbox, and helm's writes never ran in it. The
agent-security note's class 6 says a read of Secret data carries write-like risk: redacted by
default, shown as a permissioned action. Step 3C gave every decision a verdict (`Authorize`), and
step 4B a request with five answers and rules that last for the command, the chat or always. Step
6B's monitor needs a session that never asks and never reads Secret data, whatever the user's
rules say.

## Decision

**A read of core `secrets` that can carry data is a class 6 action, decided by `Authorize` at the
proxy** (`kubeproxy/secretread.go`). A get, a list, a watch, and a table that asks for the whole
object each ask; `Allowed` passes the answer unredacted, and `Denied`, a denial and an abandoned
wait pass it redacted with a 200 — "redacted unless permitted", never refused. A grant with no
asker reads redacted and records nothing. The read is decided under the grant's write lock, as a
write is, so the user sees one request at a time and the command's rules and `redactedReads` need
no lock of their own; the lock is released before the forward, since a watch streams for as long
as it is open.

**A read that prefers metadata alone asks no one** (`metadataOnly`): the first JSON type its
`Accept` names is a Table or `PartialObjectMetadata` and it does not ask for `includeObject=Object`.
It is redacted as before; asking would put a request before the user for every `kubectl get
secrets`, which shows nothing more once approved.

**A class 6 rule names the context and the namespace, and nothing else** (`GrantRule`). No Secret
is a Namespace object, so neither of step 4B's reasons to name the resource applies. A held rules
field adds `permissions.RefusedSecrets` beside `Refused`, since a class 4 `Deny` does not cover
class 6. `Auto` shows Secret data unasked, as the note's table has it.

**The helm release refusal is lifted for a command that read no Secret data redacted in the
release's namespace.** The grant keeps `redactedReads`, the namespaces a read passed redacted in,
and a release write in one of them (or after a redacted read across the cluster) is refused. A
release body is also decoded and refused when it carries the mark inside.

**A session can never ask, and never read Secret data** (`Session.NoPrompts`,
`Session.NoSecretData`). The grant copies both onto the policy it reads, and `Authorize` reads
them: `NoSecretData` refuses class 6 ahead of every rule, and `NoPrompts` turns a verdict that
would ask into a refusal.

## Alternatives considered

**Gate the helm write on the policy at the time of the write.** It would refuse an upgrade after
a read answered *Once* or *for this command*, two of the answers on helm's own request, and pass
one whose read happened to go redacted. helm writes the new release first and logs a failed
rewrite of the old one, so a mark check alone, reached on the rewrite, comes too late to stop an
upgrade. What the command read decides.

**Rename `clusterWrites` to `clusterActions`.** The rename reaches the schema, the generated code,
the record's JSON key and the webview's types for one `GET`. The doc strings say what the list
holds.

**Decide a read the policy allows or denies outside the write lock.** It would spare such a read
the wait behind a write the user is deciding, and the 429 a ninth in the queue answers. But the
lock already serializes every decision on a grant and guards the command's rules, and the journal
puts one ask to the user at a time anyway, so a read waiting behind a write loses little; the
split would cost a mutex over the command's rules and `redactedReads`, a second verdict under the
lock, and a race test.

**Let a `Policy` function set the two flags.** That would be a second place a monitor could get
them wrong. The session is their one source.

## Consequences

- A Secret's values reach the model, and so the provider, once the user allows it: for one read,
  the command, the chat, or always for a context and namespace. A read that ran unredacted is
  recorded on the call and drawn in its disclosure.
- In `Ask`, every helm command asks — `list` and `status` included, more than once per upgrade.
  The chat rule is the way out, and the prompt says so.
- A user who picked `Auto` for a context before this step has allowed every Secret read there.
  The Settings mode line is the one warning.
- An approved watch streams every later value under one answer.
- A helm upgrade whose old release's rewrite the mark check refuses leaves two releases marked
  deployed.
- `NoSecretData` and `NoPrompts` are the invariant step 6B's monitor relies on; a change to
  `Authorize`'s order must keep them first and last.

## Revisit when

A tool besides helm rebuilds what it read from Secrets in the sandbox, or users report the helm
requests as noise the chat rule does not quiet.
