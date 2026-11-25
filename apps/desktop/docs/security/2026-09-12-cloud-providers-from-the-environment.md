# Security record — cloud providers from the environment, 12 September 2026

**Subject:** eight more model providers a transcript can be sent to, each keyed by the vendor's
own environment variable. The directory is append-only, so this record widens the
[11 September egress record](2026-09-11-model-provider-egress.md) rather than editing it: what
that record says about the gesture, prompt injection and the stored block schema holds unchanged.
The living model is [security-model.md](../security-model.md).

## What is new

- **Ten key variables, one endpoint variable.** `GEMINI_API_KEY`, `GROQ_API_KEY`,
  `MISTRAL_API_KEY`, `DEEPSEEK_API_KEY`, `XAI_API_KEY`, `OPENROUTER_API_KEY`, `TOGETHER_API_KEY`
  and `FIREWORKS_API_KEY` join the two, read by `configFromArgs` in every build and cleared by
  `takeProviderKeys` before anything can exec. A key selects an account, never an endpoint: every
  base URL is a constant in `sidecar/internal/llm/providers.go`, redirectable only under
  `-tags debug`. `OLLAMA_HOST` stays the one endpoint variable. `TestConfigFromArgsReadsEveryCloudKey`
  and `TestTakeProviderKeysClearsEveryCloudKey` pin the list.
- **Bedrock and Vertex are not rows.** Their credentials (`AWS_*`,
  `GOOGLE_APPLICATION_CREDENTIALS`) are the ones the kubeconfig's credential plugins need, so
  they cannot be taken out of the environment and "read, then clear" does not describe them.
  They wait for a provider that carries a credential of its own.
- **Discovery sends the key.** Each cloud row's catalog is `GET {BaseURL}/models` at startup
  with the key as a bearer and nothing else: no body, no other header. A row's request extras
  go on the chat call alone. `TestEachDiscoveryKindSendsTheKeyAsABearer` and
  `TestChatCompletionsSendsARowsExtrasOnTheChatBodyAlone` pin both.
- **Redaction knows every key by value.** The vendors' keys have prefixes of their own (`gsk_`,
  `AIza`, `xai-`) and some have none, so a prefix rule cannot keep up. `readProviders` registers
  each key it read with `safe.AddSecret` before the logger exists, and `safe.String` blanks every
  registered value wherever it appears — matched against the original text and merged into
  spans, so a short key inside a long one leaves no tail in the clear. A value under sixteen
  bytes registers nothing: no vendor's key is that short, and a short value would blank its
  letters out of every line. The prefix rule stays, for a key that arrived some other way.
  `TestConfigFromArgsRegistersEveryKeyWithTheRedactor` and the `safe` tests pin it.

## What each provider retains

Chat Completions has no field that controls retention, so for seven of the eight what is kept is
the account's setting and the vendor's published policy. What the request controls is stated as
such.

| Provider | Retention as configured |
| --- | --- |
| OpenRouter | **Routes each request to a downstream provider of its choosing**, so the transcript reaches a party the user never named. The row sends `provider: {"data_collection": "deny"}` on every chat request (`Provider.Extra`): OpenRouter then routes only to providers it classifies, from their own policies, as not collecting user data for training or retention. That is OpenRouter's classification, not a guarantee we verify, and it is not zero-data-retention — OpenRouter's separate `zdr` constraint is stricter and is not sent, since it narrows the model list further than this row asks; a user who wants it sets it on the account, where it applies to every request including ours. A model no eligible provider serves fails with OpenRouter's "no endpoints" error, stored on the row like any refusal. `TestConfigFromArgsReadsEveryCloudKey` pins the row's extras |
| OpenAI | The [11 September record](2026-09-11-model-provider-egress.md) states `prompt_cache_retention: in_memory` per catalog entry. The current lineup (GPT-6 Astra, GPT-5.6 Sol/Terra/Luna) takes no such field: from GPT-5.6 on the cached prefix stays reusable for thirty minutes past its last use, on the machine that wrote it, and the request has nothing that shortens it. So no current entry states a `CacheRetention` and none is sent; the encoder still sends one for an entry that states it (`TestOpenAISendsTheRetentionAModelStates`). `store: false` and the encrypted reasoning in the record are unchanged |
| Gemini | Retention and training use follow the API's terms per tier, an account matter; the compatibility endpoint takes nothing that changes it |
| Groq, Mistral, DeepSeek, xAI, Together, Fireworks | Account-side only: the vendor's default as its policy page gives it, and nothing in the request changes it |

**A discovered model sends no output cap.** `Model.MaxOutputTokens` of zero is none stated, and
the Chat Completions encoder then omits `max_tokens`, so the vendor's own default for the model
applies — for OpenRouter and Gemini, the model's maximum. A model with no cap therefore has no
per-turn spend ceiling but the vendor's, a decision taken so a vendor that refuses an over-limit
cap is not refused every turn. The written Anthropic and OpenAI catalogs always state a cap, and
`NewRegistry` refuses zero on the `anthropic` protocol. Ollama changes the same way on purpose: a
local model runs to the daemon's own `num_predict` default.

## Where a key can be seen

- **In the sidecar's memory and on the wire to its vendor**, as a bearer — the same as before.
- **In no test.** Every discovery and encoder test serves a synthetic body from an `httptest`
  server; nothing in the suite dials a vendor or reads a key from the environment. The one thing
  that does is `make llm-catalogs` (`sidecar/scripts/catalogs.go`), run by hand: it reads the
  same variables, sends each key to its own vendor as a bearer on `GET /models`, and prints
  model ids.
- **On macOS a GUI launch sees none of them**, by the shell-environment allowlist's rule that a
  secret is never imported. A dev run from a terminal is where a key arrives.

## What this record does not cover

Keys entered in Settings and stored in the keychain, a catalog that refreshes, a second provider
on one vendor, and Bedrock and Vertex as auth transports — each a boundary change with a record
of its own when it lands.
