# internal/services/connectors/openrouter — the OpenRouter connector

**A sign-in that grants a key, not an OAuth session.** OpenRouter's flow is PKCE alone; its
exchange answers an API key that does not expire. `Service` keeps it in the OS keyring and signs
the `openrouter` provider with it while connected, through `providercredentials`. It shares `lib/oskeyring`
and `lib/oauth/browser` with the OAuth engine, and imports neither it nor `lib/oauth`.
→ [ADR: one package](../../../../../docs/adr/2026-11-19-a-connector-is-one-package-and-its-machinery-is-internal.md),
[ADR: a connector is an OAuth session or a granted key](../../../../../docs/adr/2026-11-19-a-connector-is-an-oauth-session-or-a-granted-key.md),
[security record](../../../../../docs/security/2026-11-19-the-openrouter-connector.md).

- **Its keyring entry** is account `openrouter-api-key`, `{"v":1,"key"}`; the saved key's entry is
  `providercredentials`'.
- **While `Restoring`**, `Sign` waits and `Bind` answers a `signer.Pending` of the restored key.
  Outside it, `Bind` answers
  `signer.Ended{}` with no session, and `providercredentials` binds a key.
- **Every change runs under one lock against one generation.** `Connect`, `Cancel`, `Disconnect`
  and a rejected key bump it. A key that arrives after its attempt ended is dropped, never saved.
- **In `Connect`, our `state` rides the callback's query; only
  exactly one `code` is exchanged. The exchange follows no redirect, and
  only a `200` with a non-empty `key` connects (a `4xx` is `Refused`, the rest `Unavailable`).
- **`Sign` signs the API base alone**, while `Connected`. A binding ends when the generation moves.
- **A `401` is confirmed before the key is dropped**: one `GET <base>/key` at a time, and only its
  own `401` deletes the entry (`KeyRejected`).
- **`Disconnect` with no key held deletes nothing.** After a refused delete, a restart
  reconnects, and a retry deletes.
- Every key, code verifier and challenge is registered with `safe.AddSecret` and forgotten when
  cleared, a sign-in's before its outcome is published. Tests run against a fake OpenRouter
  (`testutil_test.go`).
- **Not yet checked against OpenRouter**: the assumptions `docs/TODO.md` lists.
