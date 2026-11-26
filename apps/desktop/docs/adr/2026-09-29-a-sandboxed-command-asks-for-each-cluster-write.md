---
title: A sandboxed command asks for each cluster write
date: 2026-09-29
scope: cross-cutting
status: Accepted
amended_by: [Decide every target's request by access level, a boundary, a mode and rules over one action](2026-11-25-permissions-are-access-levels-a-boundary-modes-and-rules-over-one-action.md)
---

# A sandboxed command asks for each cluster write

## Context

A sandboxed command runs without asking, and reaches the chat's cluster through a proxy
([the sandbox is the gate](2026-09-28-the-sandbox-is-the-gate-for-a-sandboxed-command.md)). Until
now the proxy refused every change, so `kubectl delete pod x` ran only outside the sandbox, with
the user's whole kubeconfig and credentials. The design was settled on 2026-09-27, with
[the sandbox is the gate](2026-09-28-the-sandbox-is-the-gate-for-a-sandboxed-command.md).

## Decision

**The proxy holds each write a foreground call's command sends and puts it to the user.** A
`POST`, `PUT`, `PATCH` or `DELETE` of a resource waits on `kubeproxy.Asker`, and the grant
forwards the bytes it showed once the user approves. What the user sees is the request itself,
never a reading of it: the method, the path and query as sent, and the body.

- **One write at a time.** A write takes the grant's write lock before its body is read, and
  holds it until the forward returns. At most eight more wait for it.
- **The wait belongs to the call.** It ends with the command's connection, with the call's
  timeout, with the run, and at the grant's `End`. A wait that ends forwards nothing and is
  recorded `abandoned`. `End` then `Wait` join every handler before Bash's `Run` returns.
- **A write is an approval of the call it runs under.** `approvals` gains a `kind`
  (`cluster`) and the request as JSON. A call keeps at most one approval of its own.
- **Nobody to ask is a refusal.** A background task's grant and a run with no
  `Runtime.ClusterWriteAsker` refuse every write unasked.
- **What cannot be shown is refused.** A body that is not JSON or YAML text, is encoded, or is
  not UTF-8. A body past 1 MiB, what a user can be asked to read. A body carrying `[redacted]`
  or its base64. Any write of a helm release Secret. A service account token.
- **A dry run is read strictly.** The heading says *(dry run)* only for a `POST`, `PUT` or
  `PATCH` whose every `dryRun` is `All`, and never for a `DELETE`.

**An approved write can bring a Secret back to the model.** A Pod that mounts a Secret and prints
it reads back through `pods/log`, which passes as it is. The request shows the manifest, and the
user's eye on it is the gate. This extends the gate's decision that cluster reads leave the machine
on the model's word, logs included.

## Alternatives considered

**Keep every change outside the sandbox.** A change then runs with the user's whole kubeconfig,
every context and credential in reach, on one approval of a command whose effect the user must
infer. Asking for the request itself shows exactly what the API server receives.

**Approve a run's writes at once.** One request would cover writes the user has not seen. It remains
possible later work, and would be a security change of its own.

**Read what a write means and draw a diff.** Nothing reading a write can be trusted to read it as
the API server does. Drawing the bytes sent keeps what is approved and what is received the same.

**Refuse a pod whose spec mounts a Secret.** A Secret reaches a pod many ways — env, volume,
projected volume, an image pull — and a rule that knows some of them reads as a guarantee it is
not. The manifest is on the request.

## Consequences

- A sandboxed `kubectl delete`, `apply`, `scale` or `rollout restart` works, one request per
  object, and a `helm` change still runs outside.
- `kubectl diff` and `--dry-run=server` ask too: they reach the API server and its webhooks.
- The call's timeout covers the wait. A model that sets no timeout leaves the user two minutes.
- A service account token is refused until one can reach a command without reaching the model
  ([TODO](../TODO.md#security)).

## Revisit when

A second asker needs the proxy's writes (an approval that covers several), or a user needs a
change past 1 MiB in the sandbox.
