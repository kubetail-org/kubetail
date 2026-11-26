# Security record — the OpenRouter connector, 19 November 2026

**Subject:** the sidecar signs in to OpenRouter and keeps the API key OpenRouter issues, which
spends the credits on the user's OpenRouter account, to run the agent on OpenRouter models with no
`OPENROUTER_API_KEY`. The design is
[a connector is an OAuth session or a granted key](../adr/2026-11-19-a-connector-is-an-oauth-session-or-a-granted-key.md).
The living model is [security-model.md](../security-model.md).

## What changes

- A new credential: an OpenRouter API key (`sk-or-…`) labelled `Kubetail`, which does not expire,
  held by the sidecar (`services/connectors/openrouterkey`).
- New egress: the sidecar posts the sign-in's code and PKCE verifier to
  `openrouter.ai/api/v1/auth/keys`, and asks `openrouter.ai/api/v1/key` whether a key a turn saw
  refused is still live.
- The `openrouter` row, already offered `ours`, can be signed by the connector as well as keyed by
  `OPENROUTER_API_KEY`.
- New GraphQL: `openRouterConnection`, `openRouterConnectionWatch`, `openRouterConnect`,
  `openRouterConnectCancel`, `openRouterDisconnect`, `openRouterKeysOpen`.

## What holds

- **The key is in the OS keyring**, service `Config.KeychainService`, account
  `openrouter-api-key`, as reachable as the ChatGPT refresh token: any process of the user can ask
  the keyring for it, and a sandboxed command cannot reach the keyring.
- **No key, code or verifier is logged, projected, stored in `app.db` or written to a file.** Each
  is registered with `safe.AddSecret` on receipt and forgotten when cleared.
  `OpenRouterConnection` binds to `openrouterkey.State`, which holds no key.
  `TestNoKeyLeavesTheKeyring` reads the raw log after a sign-in, a turn's signing and a
  disconnect.
- **The key goes to the API base alone, and follows no redirect.** `Sign` refuses a URL whose
  scheme, host, effective port or path prefix is not the base's (`TestTheKeyGoesNowhereElse`);
  every wire's client refuses redirects, and so do the exchange's and the key check's
  (`TestTheExchangeTable`).
- **The callback is checked**: our `state` before anything else, exactly one `code`, on the
  listener this sign-in started, bound to `127.0.0.1` (`TestTheCallbackTable`).
- **The webview holds no key and opens no URL.** The sidecar opens the auth page it built and the
  key settings page, a constant (`TestOpenRouterMutations`).
- **The environment key wins**, and a stored key is then never read (`TestTheEnvironmentKeyWins`).
- **The keyring is checked before the browser opens**, so a machine that cannot keep a key is not
  issued one (`TestNoBrowserOpensWithoutAKeyring`).
- **A key is dropped only once OpenRouter confirms it dead.** A `401` on a turn starts one
  `GET /key`; only its own `401` deletes the entry (`TestARejectedKeyIsConfirmedFirst`).
- **A turn never signs with a key it did not start with.** A target holds a signer bound to the
  generation it began under (`TestABindingEndsWithItsSession`).
- **A failed delete is not a sign-out.** The process stops using the key at once, `Disconnect`
  errors, and the row says the key returns on restart until the delete succeeds
  (`TestAFailedDeleteIsNotASignOut`).

## Not yet checked against OpenRouter

The connector is built on the spec's assumptions about OpenRouter's flow: a `callback_url` at a
random `127.0.0.1` port is accepted, OpenRouter keeps the callback's query when it appends `code`,
a declined sign-in is not told apart, and a user key cannot delete itself. Each is checked once by
hand before the work reaches `main` (`docs/TODO.md`).

## Residual risk

- A key OpenRouter issued after a Cancel, or one whose keyring write failed after the check passed,
  is live on the account and in no store. The user deletes it by its label.
- A disconnected key stays live on OpenRouter until the user deletes it there. Kubetail says so and
  opens the key settings.
- A key whose delete failed is used again after a restart. The row says so.
