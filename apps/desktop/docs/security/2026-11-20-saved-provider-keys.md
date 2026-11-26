# Security record — saved provider API keys, 20 November 2026

**Subject:** a keyed provider's API key can be typed into the Providers pane, saved in the OS
keyring and used at once, beside the environment's key and a connector. The design is
[a provider's credential follows one order](../adr/2026-11-20-a-providers-credential-follows-one-order.md).
The living model is [security-model.md](../security-model.md).

## What changes

- A new credential: one saved API key per keyed provider, in the OS keyring, service
  `Config.KeychainService`, account `<id>-api-key` (`openrouter-saved-api-key` for OpenRouter),
  held by `services/providerkeys`.
- **This is the first secret typed into the webview.** Its path is the key box's state, one urql
  mutation's variables, the `graphql_query` body, the sidecar's resolver, the service.
- New GraphQL: `providerCredentials`, `providerCredentialsWatch`, `providerKeySave`,
  `providerKeyDelete`. The OpenRouter connector loses `FromEnvironment`.

## What holds

- **The host forwards the body unexamined and logs none of it**, answered, refused by status,
  failed to connect or timed out (`a_query_logs_no_body`, `a_timed_out_query_logs_no_body`).
- **The sidecar's error presenter logs the operation type, Go error types and `code` alone**, and
  every refusal is a sentinel that wraps no key (`TestProviderKeySaveReturnsAndLogsNoKey` reads the
  raw log and every response after a save and a refused save).
- **The webview's error exchange hands the bus the whole `CombinedError`**, which holds the
  response, not the operation: a refused save puts the key nowhere in what the bus receives, nor in
  what the error log sends the host (`puts no saved key on the error bus or in the error log when a
  save fails`, which walks every field, getters included). The hook calls
  `client.mutation(...).toPromise()`, never `useMutation`, whose state would keep the operation.
- **No log line, file, `app.db` row or GraphQL field holds a saved key.** Each is registered with
  `safe.AddSecret` on receipt and on restore, and forgotten when replaced, refused or removed
  (`TestNoSavedKeyLeavesTheKeyring`). `ProviderCredentialState` binds to `providerkeys.State`, which
  holds no key.
- **A key signs its provider's own base URLs alone**, in each base's header, and follows no
  redirect: a request under none of them is refused unsigned (`TestTheKeyGoesNowhereElse`), and
  every wire's client refuses redirects.
- **The keyring entry is as reachable as the connectors' entries**: any process of the user can
  ask for it; a sandboxed command cannot reach the keyring.

## What does not hold

- **Code running in the webview can replace or remove any provider's key**, as it can call every
  other mutation. It cannot read one back. A replaced key sends later chats under an account the
  attacker chose, at the provider's real endpoint. The CSP is the containment, as for every
  mutation.
- **A key is used unchecked**: the vendors share no check call. A `401` marks it rejected, and it
  keeps signing until the user replaces or removes it.
- **A removed key stays live at its vendor.** A key whose delete the keyring refused returns at the
  next launch, and the provider says so until a retry succeeds.
