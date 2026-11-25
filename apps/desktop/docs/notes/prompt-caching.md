# Prompt caching

How each model provider caches a prompt, and what Kubetail sends to get the cache. The vendor
facts are as their docs stated them on 2026-09-25. A cell marked UNVERIFIED is one the docs
leave open; `make test-desktop-livecache` is what confirms a row.

## Why it matters

The model APIs are stateless. Every turn resends the system prompt and the whole transcript, so
a chat's input grows with every turn. A provider that has seen a prefix before can serve it from
a cache and bill it at a fraction of the input price. On a long chat most of each request is
prefix, so caching decides most of the input bill.

A cache only helps when the prefix is byte-for-byte the same as last time. Kubetail keeps it that
way:

- The system prompt is a file with nothing per-turn in it.
- History is append-only: a turn's request is the previous request plus what happened since.
  Each wire's tests pin a turn's rendering as a leading slice of the next's.
- Anything per-turn (the cluster card, memory notes) rides in the messages, at the end.

Switching model or provider starts a new cache. The first turn after a switch pays full price.

## Three ways a provider caches

- **Automatic.** The provider caches every prefix past a minimum length on its own. Nothing to
  send. Most vendors work this way.
- **Marks.** The provider caches only what the request marks with `cache_control`. Anthropic
  works this way, and so does Claude through OpenRouter.
- **Per machine, found by a key.** Some vendors cache on the server that answered. A later
  request only hits if it lands on the same server, so the vendor takes a key naming the
  conversation and routes by it. Kubetail calls this the **affinity key**.

Each catalog entry states which of the first two applies (`Model.Cache`: `CacheAuto`,
`CacheMarks` or `CacheNone`). A provider row whose vendor routes by a key states where the key
goes (`Provider.AffinityRoute`: a header or a body field).

## What Kubetail sends, per wire

| Wire | Mark | Affinity key |
| --- | --- | --- |
| Messages (`anthropic`) | Always: the system block and the request-level `cache_control`, both `ttl: 1h` | none |
| Responses (`openai`) | none | none |
| Chat Completions (eight vendors) | Request-level `cache_control` `{"type": "ephemeral", "ttl": "1h"}`, only for a `CacheMarks` model | Where the row's `AffinityRoute` says, when it names a place |

The affinity key is the chat id for a chat's turns, and the run id for a subagent's, since a
subagent's prefix is its own brief and not the chat's. Both are UUIDv7s: they tell a vendor when
the conversation began and nothing else.

**Why an hour.** A chat is often picked up again within the hour, and a long think can outlast
the five-minute default. A one-hour write costs more than a five-minute one (2× input against
1.25×). A read costs the same either way.

## Per provider

### Anthropic

- **How:** marks only. Nothing is cached unless the request marks it.
- **What Kubetail marks:** two places. The system block is a fixed read point every chat shares.
  The request-level `cache_control` is placed by the API after the last block and moved forward
  each turn. So a turn reads the transcript so far and writes only what the last turn added.
- **Minimum prefix:** 512 tokens on Fable 5.1, Opus 5.5, Sonnet 5.5 and Haiku 5.5, as Anthropic
  documents it. A shorter prefix is not cached, and no error says so. The system prompt alone
  may be under it, so the system mark only pays once the prompt grows.
- **Reported as:** `cache_read_input_tokens` and `cache_creation_input_tokens`. Here
  `input_tokens` is the uncached remainder, so the wire adds all three up.
- **Kept:** one hour past last use, the lifetime the mark sets.

### OpenAI

- **How:** automatic.
- **Affinity key:** none. From GPT-5.6 on, OpenAI routes to the cache itself.
  `prompt_cache_key` only separates cache accounting between groups of requests. Keyed by chat,
  it would stop chats sharing the system prompt's cached prefix, so Kubetail sends none.
- **Minimum prefix:** 1,024 tokens.
- **Reported as:** `input_tokens_details.cached_tokens`, included in `input_tokens`.
- **Kept:** in memory, 30 minutes past last use. The API takes no retention setting from
  GPT-5.6 on.

### Gemini

- **How:** automatic ("implicit caching"), on every Gemini 3 and 2.5 model.
- **Affinity key:** none.
- **Minimum prefix:** Flash: 1,024 tokens; Pro: 4,096.
- **Reported as:** UNVERIFIED through the OpenAI-compatible endpoint Kubetail uses. The endpoint
  may cache without reporting it. If the check reads zero, comparing the bill is the only way to
  tell a miss from a missing report.
- **Kept:** in memory, lifetime undocumented. Google counts implicit caching as retention.

### Groq

- **How:** automatic on `openai/gpt-oss-120b` and `openai/gpt-oss-20b`. Groq documents no cache
  for its Qwen models, so those are `CacheNone`.
- **Affinity key:** none.
- **Minimum prefix:** UNVERIFIED.
- **Reported as:** `prompt_tokens_details.cached_tokens`.
- **Kept:** volatile memory only, two hours past last use.

### Mistral

- **How:** automatic. A key raises the chance of a hit.
- **Affinity key:** body field `prompt_cache_key`. Mistral's docs name a conversation id as its
  value, which is what the key is. The name is OpenAI's too, but OpenAI's key only separates
  accounting.
- **Minimum prefix:** UNVERIFIED.
- **Reported as:** `prompt_tokens_details.cached_tokens`.
- **Kept:** undocumented.

### DeepSeek

- **How:** automatic, on disk. A hit needs a whole cache unit to have been written, and the write
  can land after the reply. So a request sent right after the first can miss. The live check
  allows for this.
- **Affinity key:** none.
- **Minimum prefix:** one cache unit; its size in tokens is UNVERIFIED.
- **Reported as:** `prompt_cache_hit_tokens` and `prompt_cache_miss_tokens`, beside
  `prompt_tokens`. The wire reads the hit count when `cached_tokens` is absent.
- **Kept:** on disk, cleared hours to days after last use. No setting. The only vendor here that
  documents keeping a prefix on disk.

### xAI

- **How:** automatic, per server.
- **Affinity key:** header `x-grok-conv-id`, which keeps a conversation on one server.
- **Minimum prefix:** UNVERIFIED.
- **Reported as:** `prompt_tokens_details.cached_tokens`.
- **Kept:** no stated lifetime. Entries are evicted under memory pressure or on restart.

### OpenRouter

- **How:** whatever the vendor it routes to does. OpenAI's models cache automatically. Claude
  caches on marks, so `anthropic/claude-haiku-4.5` is `CacheMarks` and gets the request-level
  mark. OpenRouter places that mark on the last cacheable block and moves it forward each turn.
- **Affinity key:** body field `session_id`, for sticky routing: a session's requests go to the
  downstream provider holding its cache.
- **Minimum prefix:** the routed vendor's. Haiku 4.5 needs 4,096 tokens.
- **Reported as:** `prompt_tokens_details.cached_tokens` for a read and
  `prompt_tokens_details.cache_write_tokens` for a write.
- **Kept:** the routed vendor's.
- **Open:** whether OpenRouter passes the `ttl` on (without it, Claude caches for five minutes);
  whether the top-level mark holds when it serves Claude from Bedrock or Vertex rather than
  Anthropic. `openrouter/auto` picks a model per request, so its cache is that model's. A request
  routed to Claude gets no mark, and the live check skips it.

### Together

- **How:** automatic, on models that list a cached-input price. Which of Kubetail's entries have
  one is UNVERIFIED.
- **Affinity key:** none.
- **Minimum prefix:** UNVERIFIED.
- **Reported as:** `prompt_tokens_details.cached_tokens`.
- **Kept:** shared across the fleet and evicted as traffic shifts. No stated lifetime.

### Fireworks

- **How:** automatic, per replica.
- **Affinity key:** header `x-session-affinity`, which keeps a session on one replica. (`user` or
  `prompt_cache_key` would too.)
- **Minimum prefix:** UNVERIFIED.
- **Reported as:** `prompt_tokens_details.cached_tokens`.
- **Kept:** volatile memory, minutes to hours, oldest evicted first.

### The cloud platforms

The platform rows (spec 28) are off in every build. Their Claude rows ride the Messages wire, so
they carry its marks. Bedrock and Vertex document the one-hour lifetime for current Claude
models, with exceptions for old ones. Foundry's one-hour lifetime is UNVERIFIED. A deployment
that refuses `ttl: "1h"` refuses the whole request, so the live check must pass on a platform
before the UI turns it on. The platforms' GPT and chat rows are UNVERIFIED throughout.

## Reading the counts

Every wire reports `llm.Usage`: `InputTokens` is everything the model read, and
`CacheReadTokens` and `CacheWriteTokens` are the parts of it served from and written to the
cache. They reach the model call's row (`llm_calls.cache_read_tokens`, `cache_write_tokens`,
beside the uncached remainder in `input_tokens`).

These counts are the only evidence caching works. A regression is no error, just a higher bill.
After a change to prompt assembly, check them by hand. The goldens under
`sidecar/internal/app/testdata/prompt/` and `sidecar/internal/agent/chat/testdata/context.md`
show the prefix byte for byte, so `make generate-desktop-prompts` and the diff say what moved before any call is
made.

## Checking a row

`make test-desktop-livecache` sends each keyed model two turns with one affinity key and reports whether
the second read from the cache. It reads keys from the environment, skips providers without
one, and prints one line per model: provider, model, kind, the first and last read counts.

- Each run opens with a fresh line, so the first request is always a cold write.
- The shared prefix is about 6,000 tokens, over every documented minimum.
- The second request is retried up to three times, for vendors that write behind the reply.
- A model that reports no usage fails: no check can see its cache.

It spends real tokens, so nothing runs it automatically. Run it before a catalog change lands
and paste its lines into the PR.

## Sources

- [Claude prompt caching](https://platform.claude.com/docs/en/build-with-claude/prompt-caching)
- [OpenRouter prompt caching](https://openrouter.ai/docs/guides/best-practices/prompt-caching)
  and [sticky routing](https://openrouter.ai/blog/tutorials/prompt-caching-sticky-routing/)
- [Gemini context caching](https://ai.google.dev/gemini-api/docs/caching)
- [Groq prompt caching](https://console.groq.com/docs/prompt-caching)
- [Mistral prompt caching](https://docs.mistral.ai/studio-api/conversations/advanced/prompt-caching)
- [DeepSeek context caching](https://api-docs.deepseek.com/guides/kv_cache/)
- [xAI prompt caching](https://docs.x.ai/developers/advanced-api-usage/prompt-caching)
- [Together serverless overview](https://docs.together.ai/docs/serverless/overview)
- [Fireworks prompt caching](https://docs.fireworks.ai/guides/prompt-caching)
- [Bedrock prompt caching](https://docs.aws.amazon.com/bedrock/latest/userguide/prompt-caching.html)
- [Vertex AI Claude prompt caching](https://docs.cloud.google.com/vertex-ai/generative-ai/docs/partner-models/claude/prompt-caching)
