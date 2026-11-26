# internal/services/connectors/internal/oauthconn — the OAuth engine

**A `Service` is one provider's OAuth sign-in, and signs the requests to its API
base** (`llm.SessionSigner`). The engine names no provider: what one has of its own is its
`Provider` (`provider.go`); `chatgpt`, `google` and `kubetailcloud` run it.
**It runs every OAuth session**; a granted key is a connector of its own (`openrouter`, `github`).
→ [ADR: one package](../../../../../../docs/adr/2026-11-19-a-connector-is-one-package-and-its-machinery-is-internal.md),
[ADR: the engine](../../../../../../docs/adr/2026-11-18-an-oauth-connection-is-a-row-matched-to-its-keyring-entry.md),
[ADR: ChatGPT](../../../../../../docs/adr/2026-11-18-the-chatgpt-connector-signs-in-with-chatgpts-dynamic-registration.md),
[security record](../../../../../../docs/security/2026-11-18-the-chatgpt-connector.md).

- **A new OAuth connector is a package beside `chatgpt`**: a `Provider` and a `Service` over the
  engine; a model provider adds a keyless `catalog` row and its connector to `app`'s
  `providercredentials`. Widen `Provider` only by what its flow needs. `Provider.ID` keys the row and
  the keyring accounts. A fixed `Provider.ClientID` registers no client.
- **A session exists only when the row's `session_id` and the keyring entry's `session` match.**
  A sign-in mints the id and writes the entry, then the row, on every save. The restore matches
  them and reaches no network; any other entry is deleted, before the row's write. The access
  token is memory's alone.
- **`New` is `Restoring` and makes no keyring call**; the restore reads on a goroutine, and a row
  that cannot be read waits for it too. Until it publishes, `Sign` waits (after its resource
  check), `Bind` answers a `signer.Pending` of the restored session, and every change answers
  `ErrRestoring`. `Close` leaves a keyring call that never answers behind.
- **Every change runs under one lock against one generation.** `Connect`, `Cancel`, `Disconnect`
  and a session's end bump it; whatever finishes applies only to the generation it began under.
  Late tokens are revoked; a failed revocation flags the state that ended them.
- **`Connect` writes a NULL `session_id` and checks the keyring before any browser opens**, and is
  refused while `Restoring`, during an attempt or while connected. The client id the callback
  issues is saved before the code exchange.
- **`Sign` signs the API base alone**, none if it is empty, one refresh at a time under the
  service's own bound. A rotated refresh token is in the keyring before any access token is
  handed out. A refused refresh ends the session; an unreachable issuer clears nothing.
- **`Bind` is a signer for the current session**, ended outside `Connected` and `Restoring`. `Disconnect` clears,
  then revokes on a goroutine.
- Every token is registered with `safe.AddSecret`. Test seams are unexported options; tests run
  test providers against a fake issuer (`testutil_test.go`).
