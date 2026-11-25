# internal/services/providercredentials — every provider's credential

**One service holds every provider's keys, knows its connector, signs it and syncs its catalog.**
It is not a connector: nothing signs in.
→ [ADR: a provider's credential follows one order](../../../../docs/adr/2026-11-20-a-providers-credential-follows-one-order.md),
[ADR: every provider is one row, signed by one service](../../../../docs/adr/2026-11-20-every-provider-is-one-row-signed-by-one-service.md),
[security record](../../../../docs/security/2026-11-20-saved-provider-keys.md).

Every data access here is named for its contract: [`docs/data-access.md`](../../../../docs/data-access.md).

- **The order, lowest to highest: environment key < saved key < connector.** `source` is its one
  spelling; `failure` puts `NotDeleted` over `KeyRejected`.
- **A provider with no `KeyVar` takes no key**: no keyring read, no environment key, and `Save`
  and `Delete` are `ErrNoKey`. Its connector alone signs it.
- **One entry per provider in the OS keyring**: account `<id>-api-key` (`openrouter-saved-api-key`
  for OpenRouter, whose connector owns `openrouter-api-key`), `{"v":1,"key"}`. `account` is the one
  spelling; `TestTheAccountNames` pins it. The restore reads as the connectors do: a newer `v` is
  left and marked, so `Delete` leaves it too; an entry that does not parse, or holds a key `Save` would refuse, is
  deleted; an unreadable keyring writes nothing, and `Delete` still reaches its entry.
- **A provider is `SourceRestoring` until its own read has answered and its connector has
  restored**; a keyless provider waits on its connector alone. A follower subscribes to
  `WatchSessionGenerationState` only after the connector's `Restored()`, and lands on its `RestoredSession()`,
  `landedGen`, which moves no `keyGen`. Until then `Save` and `Delete` answer `ErrRestoring`,
  `Sign` waits, and `Bind` is a `signer.Pending` of the session at the bind: the connector's
  binding says, or `landedGen` while the connector restores. `HasCredential` holds for the
  environment key alone meanwhile.
- **Two locks, in this order.** `writes` serializes `Save` and `Delete`, the only lock held across
  a keyring call. `mu` guards every provider and publishes each change, so the state watch never ends on
  an older list. Neither is held across a connector call.
- **`keyGen` moves only when the saved key in memory changes or the connector starts a session.**
  A key's binding (`keyBinding`) signs until it moves.
  `Delete` drops the key from memory and publishes first, so its bindings end whatever the keyring
  answers.
- **`Bind` returns the connector's binding unless it is `signer.Ended{}`**, its answer with no
  session.
- **A key goes on every route whose base covers the request, in that route's header**
  (`catalog.Providers`), and nowhere else: a request under no base is `errOffBase`, unsigned.
- **One `401` to the key that signs marks it rejected**; it keeps signing until a key is saved.
- **`WatchCredentialGenerationState` is `0` for no credential, `1` for a keyed provider, else the session's
  generation**, so a keyless provider's catalog is read per session and a keyed one's never
  moves. `SyncCatalogs` runs `catalogsync` over it.
- Every saved key is registered with `safe.AddSecret` while held.
