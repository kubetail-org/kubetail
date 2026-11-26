---
title: A prompt names the action and offers a duration
date: 2026-10-04
scope: cross-cutting
status: Accepted
---

# A prompt names the action and offers a duration

## Context

A sandboxed command's cluster write asked as the request the API server would receive: a
heading by HTTP method, the path, the media type and the body byte for byte. The user read a JSON
merge patch and answered with one boolean, and the next identical write asked again. Step 3C made
the policy's verdict binary, so an answer that outlasts the request is a grant, and a grant lifts
only an `Unmatched` verdict. Step 4B of the agent-security sequence builds the prompt on that.

## Decision

**The request names the action.** Its heading is the classifier's `Summary`. For a `PUT` or
`PATCH` of a named object that asks, `kubeproxy/diff.go` reads the object and dry-runs the same
request, and the request draws the two as a YAML diff (`DiffBlock`), with the raw request under
*Show the request*. Approve waits on the diff; with no diff, or one cut at 2,000 lines, the raw
request is drawn open and Approve waits on its body.

**Five answers, and the record keeps which.** `approvalDecide` takes `Once`, `Command`, `Chat`,
`Always` or `Deny`; `approvals.duration` stores how long an approval holds. `Command` adds
`permissions.CommandRule` to the grant, in memory, for the rest of the command. `Chat` writes
`permissions.GrantRule` as a `chat_grants` row, listed and removed beside the composer; `Always`
writes it into `securityconfig`'s rules. A rule already held is not written twice. The allow
answers are offered only where `permissions.Grantable` holds: an `Unmatched` verdict, not a dry
run, with a context. `service.Approve` claims the waiter first, writes the rule outside the
lock, then delivers.

**The rule's words are the sidecar's.** `Rule.Line()` draws each pattern field one way — a name
bare, a literal unescaped in quotes, any other pattern after *matching* — and the request draws
the line it returns under each allow button, as Settings draws its rules.

## Alternatives considered

- **Draw the raw request alone.** It is exact, but a strategic merge patch or an apply says what
  the client sent, not what the cluster will do; the dry run answers that, and a webhook that
  refuses it would refuse the write.
- **Diff the body against the object in the webview.** It would need a patch engine per media
  type in the webview and still miss defaulting and webhooks.
- **Send a cut diff whole.** A large object's diff is tens of thousands of lines on the wire and
  in the row; the raw request is the exact bytes, always whole behind its fold.
- **Let a chat or always rule name the verb and resource.** Narrower, but it asks again for each
  verb in a namespace the user meant to allow; the command rule is the narrow one, and lasts as
  long as the command.
- **Spell the rule in the webview.** A second spelling could disagree with Settings'; the sidecar's
  `Rule.Line()` is the one.

## Consequences

A dry run's body reaches admission webhooks for a write the user may deny. A namespace's rule
an answer writes covers every write in it but the Namespace object, which carries its own name as
its namespace, so the rule says *inside* and leaves it out. *Allow for this command* approves
objects the user has not seen. The diff is a preview, not a lock. Each is a residual in [the
security record](../security/2026-10-04-the-prompt-names-the-action.md).

`approvalDecide`'s answers are a table in `services/chat`: a call's own request and an action no rule may
allow take `Once` and `Deny` alone. A step that adds a class adds its row there. A field added to
`permissions.Rule` is drawn by `Rule.Line()`'s three cases.

## Revisit when

A cluster's API server can preview a write without running its admission webhooks, or a user
needs a rule that names a dry run.
