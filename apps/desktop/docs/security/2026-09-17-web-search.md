# Security record — web search, 17 September 2026

**Subject:** the model can search the web where its provider searches for it. A model whose
catalog entry names a search tool (`llm.Model.WebSearch`) is offered it on every turn: the
Messages API's `web_search_20260318` and the Responses API's `web_search`, each run on the
provider's side, capped per reply (`chat.maxWebSearches`, 5). The sidecar runs nothing itself.
The living model is [security-model.md](../security-model.md); the previous egress records are
[model provider egress](2026-09-11-model-provider-egress.md) and
[the general agent](2026-09-17-spawn-agent.md).

## What now leaves the machine

The model's own words, to the provider's search backend, and from there to whatever the search
hits. Until now everything that left went to the provider the user picked and stopped there. A
query is text the model composed after reading the transcript and the card, so cluster text can
reach a query. The prompt (`services/chat/prompts/web_search.md`) says not to put a cluster's names in
one, and the prompt is not a bound. The bounds are the count and the user's eye: the transcript
draws every query the client was told of (*Searched the web*), the sources beside it.

## Prompt injection

An instruction that rides cluster text can steer a query, and a query is outbound. That is the
new thing: before this, an injected instruction bought a misleading answer or a list of names,
and neither left the machine. Now it can put a name in a search. The parent searches under the
user's question and the user's eye, and the transcript shows what was searched.

**The child is not granted a search** for exactly this reason: its task is prose the model wrote
after reading the cluster, and a search on a task nobody typed is an egress nobody watched.
`agentDef.webSearch` makes the grant one field, false on `generalAgent`;
`TestTheChildIsOfferedItsAllowlistAlone` pins `WebSearches == 0` on the child's request. Flipping
it for any agent is a security change under this record's rule.

## Input handling

Nothing new is parsed from the user. What is parsed from the provider — the query, the citations'
`url` and `title` — is stored in the app's own fields and drawn as text (`searchesOf`,
`sourcesOf` in `src/lib/chats.tsx`; the transcript renders both into React elements, never
markup). A citation whose fields are not strings is dropped by the reader.

## What comes back

Search results are text from the web inside the provider's reply. The prompt names them data.
They are stored native (`web_search_tool_result`, with the provider's `encrypted_content`) for the
replay and never drawn; what the user sees of them is the sources list. The search is called by
the model directly (`allowed_callers: ["direct"]`), never from a code-execution container, so no
execution environment runs on the provider's side for it and no container block enters the
reply. Turning that on is a new record.

## The record

Every query the client was told of is on the message's row as a `web_search` block: from the
reply's content when it ended cleanly, with the provider's `server_tool_use` beside it, and from
the search chunks seen on the stream when it did not (`TestAnInterruptedReplyKeepsItsQueries`).
Every clean call's row holds `llm_calls.web_searches`, the count the provider reported
(`TestTheModelCallRowKeepsTheSearchCount`). A search the provider ran whose event never arrived
is outside what this side can record, and the row's count is NULL for such a call: the audit
trail is complete for what reached the machine, and nothing more can be promised.

## Consent

The user's gesture is the send. Where it is on: the Anthropic and OpenAI rows
(`TestAnthropicCatalogNamesASearchOnEveryModel`, `TestAProviderSearchesOnEveryModelOrNone`);
every platform row names none (`TestPlatformRowsNameNoSearch`), and every Chat Completions row
names none and its encoder refuses a request that asks (`TestChatCompletionsRefusesASearch`).
Retention of a query is the search backend's, under the vendor's own terms. Domain filtering
(`allowed_domains`, `blocked_domains`) is not set: the queries the model needs are open-ended,
and a list would be a policy with no owner.

## Verified against the real API

Search, citations and the replay on a follow-up turn: Haiku 4.5, Sonnet 5, Opus 5 and Fable 5.1,
17 September 2026. A search beside a cluster call in one reply, and the follow-up that replays
the mixed round: passed. A question needing several searches ran two and never paused: the
provider pauses its own loop around ten iterations, so under `maxWebSearches` (5) `pause_turn`
is not reached, and the resume path is insurance for a higher cap, covered by its unit tests
alone. Not yet run: the cap, a cancel mid-search, and the OpenAI row. Each is a check of the
loop's handling, not of what leaves; the bounds above do not depend on them.

## Note, 19 September 2026

The names moved with *A native tool is a contract*:
`Model.WebSearch`/`WebFetch`/`Bash` are the `NativeServerTools` and `NativeClientTools` lists of contracts
(`anthropic_web_search_20260318`, `anthropic_web_fetch_20260318`, `openai_web_search`;
`anthropic_bash_20250124`, `openai_shell` — the function bash is no contract, and a build offers
none); `Request.WebSearches`/`WebFetches`/`WebFetchTokens`/`Bash` are
`Request.NativeServerTools` (name and caps) and `Request.NativeClientTools`; a `web_search` or `web_fetch` block is a
`server_use` block under that word; `web_searches`/`web_fetches` on `llm_calls` are one
`server_uses` object. The bounds and the gate are unchanged.

## Note, 24 September 2026

The search is rebuilt on the Messages wire alone. The Responses row is not: nothing offers
OpenAI's `web_search`, and `Target.Stream` refuses a contract its provider's wire does not speak
(`TestStreamRefusesAContractItsWireDoesNotSpeak`). What the rebuild pins:

- **The cap is 5 searches per turn**, however many requests the turn makes: each request is
  offered what the earlier ones left, a spent search is not offered, and a paused reply is
  resumed only while searches remain (`TestEachRoundIsOfferedWhatIsLeftOfTheSearchBudget`,
  `TestASpentSearchBudgetIsNotOffered`, `TestAPauseIsNotResumedOnceAServerToolIsSpent`).
- **A search is a `tool_calls` row the provider ran** (`runs_on = 'provider'`), its arguments
  the query, kept the moment the stream shows the call, so a reply that broke off keeps its
  queries (`TestABrokenReplyKeepsItsServerCalls`, `TestASearchIsAProviderRow`). The count is
  `llm_calls.server_uses` (`TestTheModelCallRowKeepsTheSearchCount`).
- **A call with no result is kept in the record but replayed only to resume the reply it
  paused** (`TestAnUnansweredSearchIsDropped`, `TestAPausedSearchIsResumedWhole`).
- The search is still the model's direct call (`TestMessagesOffersTheSearch`), and no other
  keyed provider names a server tool (`TestNoOtherKeyedProviderNamesAServerTool`). The child
  agent is not rebuilt, so nothing grants one a search.

**Not verified against the real API since the rebuild**: the search, citations and replay on
each model, a cancel mid-search, and the resume. The bounds above are pinned by tests and do not
depend on it; the check is an open item in [`docs/TODO.md`](../TODO.md#security).
