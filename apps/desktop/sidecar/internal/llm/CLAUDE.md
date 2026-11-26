# internal/llm — the model wires

Every model call goes through `llm`, chat's or anyone's: three wires and one fake. Which providers
and tools a turn gets is `internal/agent/catalog`. *Decided, not built* lists designs whose ADRs are
written and whose code is not.
→ [ADR: the llm package wires itself](../../../docs/adr/2026-09-21-llm-is-one-package.md).

## Package shape

- **`Dialect`** is `messages`, `responses`, `chatcompletions` or `fake`, named for the API, never the
  vendor. The GraphQL `LLMDialect` enum binds member by member onto `Dialects`.
- **Dialects are code, providers are data.** `Target.Stream` is one `switch` on the dialect, and
  `spoken` beside it is what `Resolve` refuses by. No interface, and no code on a provider but
  `Provider.fake`. A vendor's quirk is a line on its row in `catalog/providers.go`, never a branch in
  a wire. **`llm` names no vendor and no tool.**
- **Inside a wire's file every helper is a method on the wire's type** (`messagesWire`,
  `responsesWire`, `chatWire`); every other top-level name starts with the wire's word.
- **The SDK boundary.** Inside `llm` only `dialect_*.go` and `helpers.go` import a model SDK.
  Outside it only a tool's wire file named for its dialect (`messages.go`, `responses.go`,
  `chatcompletions.go`) and its test may, for types alone (never `option`), building no client.
  `TestOnlyAWireFileImportsAModelSDK` and `TestNoDialectReadsTheSDKsEnvironmentDefaults` hold it.
  → [ADR: every tool is in the box](../../../docs/adr/2026-09-24-every-tool-is-in-the-box.md).
- **A client takes nothing from the environment**: base URL, key and retries off
  (`WithMaxRetries(0)`; a turn is never retried), nothing else. Anthropic uses
  `WithoutEnvironmentDefaults`; the OpenAI wires build their service from `openAIOptions`, never
  `NewClient`. An empty key sends no `Authorization`. **Bumping either SDK means re-reading its
  `DefaultClientOptions`.**
- **`Resolve(providerID, modelID, effort)`** refuses an unknown provider, model or unspoken dialect
  (`ErrUnknownModel`) and an unlisted effort (`ErrBadEffort`; a model with no efforts takes `""`).
  The agent never builds a `Request`; `Target.Stream` does. `Model.ContextWindow` 0 is unstated,
  and such a model is never refused ahead.
- **The provider list is fixed; a provider's catalog is not.** `SetCatalog` replaces one provider's
  models.

## Credentials: a key or a signer

- **`Provider.Key` or `Provider.Signer`, never both** (`New` panics). A `Signer` sets each request's
  authorization and hears a `401` (`Refused`). Every wire runs `send` (`helpers.go`) on
  `NoRedirectClient`; no SDK is given a key. `send` and `NoRedirectClient` put `version.UserAgent()`
  ahead of the SDK's own token. `authorize` puts a key where `AuthHeader` says: `KeyHeader`, else
  the dialect's own header; `services/providercredentials` takes each route's header from it too.
  A failed `Sign` sends nothing: `SignError`. → [ADR: a provider's credential is a key or a
  signer](../../../docs/adr/2026-11-18-a-providers-credential-is-a-key-or-a-signer.md).
- **A `SessionSigner` binds**: `Resolve` puts `Bind()`'s signer on the target, and
  `ResolveUnder(parent, …)` keeps the parent's for a subagent on its provider.
- **`Provider.Codes`** is a row's own error codes and their class, kept by the OpenAI wires;
  `ResponseError` sets `Error.Class` from it, and `Error.UsageLimit()` reads it.

## Providers and the catalog

- **Every catalog is written** but `chatgpt`'s, which is the account's, and `google`'s (Vertex AI),
  present when a project was found; their connectors alone sign them. Each provider file says
  whether its ids were checked; no test reaches a vendor.
- **A provider's `Dialect` and `BaseURL` are defaults; a model may override them** (`Model.Dialect`,
  `Model.BaseURL`). `Provider.ForModel(m)` applies the overrides, and **every `Target` holds the
  provider it returns**: `Resolve` alone builds one. So everything downstream reads one dialect off
  `Target.Provider`. The GraphQL `llmDialect` is the provider's default. → [ADR: a model may
  override its provider's default dialect](../../../docs/adr/2026-11-12-a-model-may-override-its-providers-default-dialect.md).
- **Effort is the vendor's own vocabulary**, never translated. A model with an output cap of zero
  on Chat Completions sends no `max_tokens`; Messages and Responses always state one.
- **Three option lines on a provider, applied by every wire** through `providerOptions`: `Extra`
  (JSON set), `Query` and `Omit` (JSON delete). `Omit` goes last, after the wire's own options, so
  it deletes what the wire wrote too (all but `stream`). `Extra` and `Query` go sorted.
- **OpenRouter's `Extra` is `provider.data_collection: deny`**: OpenRouter's classification, not
  ours, and not zero-data-retention.
- **A key saved from the Providers pane needs no restart** (`services/providercredentials`); an
  environment key is read once per launch (`sidecar/CLAUDE.md`).
- **Which models take tools is `Model.Tools`**: every entry sent on Messages or Responses, per entry
  on Chat Completions.

## Blocks and the record

- **The record is the app's schema** (`block.go`). Fields past `Type` and `Text` are `omitempty`.
  `ToolUseBlock` panics on invalid JSON, so a `[]Block` always marshals. → [ADR: the record is the
  app's](../../../docs/adr/2026-09-11-the-record-is-the-apps-not-the-providers.md).
- **Readers**: `Text` puts a blank line across a tool round or a server call (the webview's `textOf`
  follows it); `Prompt` is a message as one string, without thinking. **The `<context>` tags are the
  wire's** (`wireText`), never stored.
- **A wire assembles the record in the reply's order**, one thinking block per provider item.
  `sectionJoiner` makes streamed thinking read as stored thinking does.
- **A `Payload` is the provider's own item, verbatim**, for replay to the provider that wrote it. It
  is always a JSON object (`Block.replayable`): a reply carrying anything else fails, and a stored
  message holding one goes as foreign to everyone (`disownUnreplayable`).
- **`Message.ProviderID` and `Message.Dialect` are the writer.** **A payload is keyed on provider and
  dialect**: `stripForeign` drops every payload for any other provider, or the same provider on
  another dialect, and keeps every app field. A message naming no writer is foreign to everyone.
- **When a wire's reading of an API changes so old payloads cannot replay**, add a `wire_version`
  column on `agent_runs`, defaulting to zero for existing rows, carry it on `Message` and compare it
  in `foreign`. The column does not exist until then.
- **Every wire replays a call under an id it hands out** (`replayIDs`, deterministic over the
  conversation): foreign calls and reused own ids are minted `call%05d`, and a minted call keeping a
  payload has the id written in (`withReplayID`). The record keeps the writer's ids.
  `make test-desktop-liveswitch` walks one chat across the real APIs. → [ADR: a chat can switch to
  any model](../../../docs/adr/2026-09-25-a-chat-can-switch-to-any-model.md), [security
  record](../../../docs/security/2026-09-25-a-chat-switches-vendor.md).
- **A payload never leaves the sidecar.** `agent/chat` applies `WithoutPayloads` in `Progress` and
  `forReader`; only `history` (via `listMessages`) keeps them.
- **`toolInput`** keeps a call's arguments as a trimmed object. A call cut off inside them is stored
  `{}` and answered not-run; a non-object that ended normally fails the reply; **a repaired call
  carries no payload**.
- **A request offering nothing replays no round** (`WithoutRounds`). **A payload goes with its
  round** wherever rounds are dropped, including `agent/chat`'s history for a row not settled
  Complete: the Responses API refuses a reasoning item whose call is gone.
- **`StopReason` is the provider's word**, except that every wire reports a reply that asked for a
  tool as `StopToolUse`, the one word the loop keys on. **`Usage.InputTokens` is everything read** on
  every wire, cache counts included.
- **`llm.Error` never holds a body's text, on any path** (a body can echo the conversation).
  `TransportError` logs its cause; `ReadError` keeps nothing; `IncompleteError` is a clean end with
  no stop reason. `ContextFull()` classifies an overflow.

## Tools on the wire

- **The offer** is `ToolDefinition`s (the app's tools, sent as functions) and `NativeOffer`s (a
  vendor's tool, `Server` when the provider runs it, `MaxUses` ≥1 for a server tool).
- **`Target.Stream` refuses before sending** (`ErrToolsUnsupported`): an offer on a model that takes
  none, a native offer the target does not `Take`, a name spelled twice.
- **A native tool spells itself on each wire**: one interface per wire in its dialect file
  (`MessagesServerTool`), implemented in the tool's package, never a branch keyed on a tool.
- **A server call is recorded, never run.** A `server_use` block is the call; a `native` block is a
  provider item kept for replay alone; a citing text block's payload is the one copy of its
  citations. A request offering no server tool sends them as plain text (`withoutServerCalls`).
  `Citations(dialect, blocks)` reads by the dialect of the run that wrote them.

## The wires

- **Thinking is asked for exactly when `Request.Effort` is set.** Responses also includes
  `reasoning.encrypted_content`: `store` is false, so the item is the only copy. Chat Completions
  reads `reasoning_content` / `reasoning` off `delta.JSON.ExtraFields`.
- **Every stream runs under an idle guard**, reset by every event and every body read, ending with
  `ErrStreamIdle` as the cause. The bound is per dialect, the clock a parameter (`idleClock`). **The
  clock stops while a request is signed** (`authorize`): a signer may wait for the keyring. The
  guard rides the context; a request with none signs as it is. → [ADR: bound a model stream by wire
  silence](../../../docs/adr/2026-10-17-a-model-stream-is-bounded-by-wire-silence.md).
- **Caching.** Messages marks the system block and the request-level `cache_control` at one hour.
  Every entry states `Model.Cache`; Chat Completions marks only `CacheMarks`. A request's
  `AffinityKey` goes where `Provider.AffinityRoute` says. **Append, never edit**: a turn's rendering
  is a prefix of the next's. The cache token counts on the model call's row are the only evidence
  caching works; `make test-desktop-livecache` confirms a row. → [ADR: the prompt
  cache](../../../docs/adr/2026-10-17-the-messages-wire-caches-the-prompt-for-an-hour.md),
  [`docs/notes/prompt-caching.md`](../../../docs/notes/prompt-caching.md).
- **Messages.** **A call's arguments are read off the deltas** (`messagesToolInputs`): the SDK
  replaces unparsable input with `{}`, which is also a legitimate input. The offer is the schema
  whole (`ExtraFields`, so a `$ref` resolves). **With thinking on, the row holding the last round
  drops its tool blocks when it has no thinking block to send**, decided by the writer and
  `Effort`, never by its blocks. A request that does not think sends no thinking block.
- **Responses.** Function tools go with `strict: false` explicitly (Chat Completions leaves `strict`
  unset). An organization OpenAI has not verified may be refused the reasoning summary as a 400
  before the stream exists: a `Failed` row on every send until it is. **An incomplete response
  keeps its own stop word** over any call it carries, so the call is answered not-run. The replay is
  the payload verbatim, else rebuilt from the fields.
- **Chat Completions.** `chatCalls` folds `tool_calls` fragments; **every fragment counts** (name
  pieces, a late id, a vendor key), and a fragment with a new id opens the next call. A call
  carrying a vendor key (Gemini's thought signature) keeps its entry as payload. `stop` with calls
  present is `tool_use`. **No `finish_reason` is `io.ErrUnexpectedEOF`**, since `[DONE]` and a
  closed socket look alike. **It sends `max_tokens`, not `max_completion_tokens`**: local runtimes
  read only the older name.
- **The conformance table is `dialect_test.go`**: one `wire` fixture per real dialect, every case
  run per wire. A new wire adds a fixture there.

## The fake (`dialect_fake.go`)

The one scripted dialect: what a consumer's test needs from the wire is a knob here, never a second
fake. A debug build adds one (`Config.AddFake`). Its catalog is `fake` (tools, efforts) and
`fake-no-tools` (neither). `SetReply()` with `FailAfter(0, err)` is a reply refused before any
chunk. **Every reply thinks**, and `FailAfter(n)` and the gate count chunks of either kind, so
"after the first chunk" is inside the thought. It has no idle guard: stage a quiet turn as
`FailAfter(n, ErrStreamIdle)`. **`SetGate` holds the next reply alone and releases on close alone**
(a send panics); to hold a second reply, arm it from a tool's `Run`. `Route(prompt)` is a fake of
its own for a subagent's requests.

## Decided, not built

None of this is in the code. Each design is fixed by its ADR or record; build to it.

- **Client-run native tools** (Anthropic's bash, OpenAI's `shell`). A reader writes a client call
  under the wire's own tool, offered or not, so the loop refuses an unsolicited one; **a plain
  function call spelling the tool's name fails the reply**; an unoffered tool's call replays as a
  plain function round. → [shell tool](../../../docs/security/2026-09-18-shell-tool.md),
  [tools on the Responses API](../../../docs/security/2026-09-18-tools-on-the-responses-api.md).
- **Discovery** of the keyed vendors: once, off the path to READY, each entry under its own
  deadline. A model is listed only when its catalog says it can chat. → [ADR: a discovered model is
  listed on its catalog's word](../../../docs/adr/2026-10-17-a-discovered-model-is-listed-on-its-catalogs-word.md).
- **Cloud platforms** (Bedrock via Mantle, Vertex, Foundry): a signer per entry, never a dialect;
  every value from the platform's own conventions, no `KUBETAIL_` variables. → [security
  record](../../../docs/security/2026-09-13-cloud-platforms.md).
- **Ollama**: the one keyless entry, always last, dialed where `OLLAMA_HOST` says. A cloud-backed
  model is never listed. → [ADR: Ollama is local](../../../docs/adr/2026-10-17-ollama-is-local-and-ollama-host-says-where.md).
