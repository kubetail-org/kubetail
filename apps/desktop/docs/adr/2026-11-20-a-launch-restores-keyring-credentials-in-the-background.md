---
title: A launch restores keyring credentials in the background
date: 2026-11-20
scope: cross-cutting
status: Accepted
---

# A launch restores keyring credentials in the background

## Context

Every connector and `providerkeys` read its keyring entry inside `New`, and `build` called each
`New` before the sidecar printed `READY`. On Linux a locked login keyring raises an unlock prompt
on that read, and the read waits for the user. The launch therefore waited behind a password
prompt, past the host's 30-second readiness budget, and the user saw an error screen for typing a
password too slowly ([#1094](https://github.com/kstackhq/kstack/issues/1094)).

Step 1 of the work put every read behind one gate (`oskeyring.Gate`), so the launch raises one
prompt, not one per service, and a dismissed prompt fails every read with no second prompt.

## Decision

**No `New` calls the keyring.** Each service publishes a new phase, `Restoring` (`connectors.Restoring`,
`providerkeys.SourceRestoring`, `AuthState.restoring` on GraphQL and gRPC), reads on a goroutine of
its own through `oskeyring.Read`, and publishes what it read. `Restored()` closes once it has.

**While `Restoring`, a request that needs the credential waits; a request that would change it is
refused.** `Sign`, `Token` and `Retrieve` wait for the restore, ended by the caller's context and by
`Close`; `Bind` answers a `signer.Pending` that resolves to the session the restore published.
`Connect`, `Disconnect`, `Save` and `Delete` answer `ErrRestoring`.

**A turn's idle clock stops while its request is signed**, so a turn sent at launch can wait out the
prompt.

**`Close` leaves a keyring call that never answers behind.** The restore returns as soon as its
context ends; the call ends with the process.

This amends the Decision's *ignored and deleted at launch* in [an OAuth connection is a row matched
to its keyring entry](2026-11-18-an-oauth-connection-is-a-row-matched-to-its-keyring-entry.md): the
match is made by the restore, after the launch, and the entry's delete now comes before the row's
write.

## Alternatives considered

- **A deadline on the read.** A short one fails the user who is typing a password, and nothing
  reads again, so the session is lost until the next launch. A long one stalls the launch as long.
- **Waiting on a change, as a signature does.** It would hold a mutation open behind a password
  prompt, with no view able to say why. Refusing is one rule for every change; after step 3 no view
  offers a change while it draws *checking*.
- **Letting each service read at once.** Each read could raise its own prompt. The gate's probe runs
  alone first, so the launch raises one.
- **A `Close` that waits for the read.** A read that never answers would hang the sidecar's stop
  until its watchdog killed it.

## Consequences

- The launch no longer waits on the keyring. The windows draw a `Restoring` row until step 3 gives
  it its own look.
- Every consumer of a connection gauge, `providerCredentialsWatch` and `authStateWatch` sees
  `Restoring` first. A consumer that read the first frame as the answer must wait for the next.
- A provider whose only key is saved lists no model until its read answers, so a turn on it at
  launch is refused as an unknown model; one with an environment key lists at once and waits in
  `Sign`.
- A turn sent at launch shows no first token until the user answers the prompt, and nothing but
  *Stop* times it.
