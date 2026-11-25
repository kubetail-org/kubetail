# Model providers and what leaves the machine

[← Security model](../security-model.md)

A chat sends its transcript to the model provider the user picked for that send. The user's own send is the gesture that lets data leave, and the transcript shows what the model read, searched and ran. This subject lists what rides along with a send: the cluster card, KubeQuery results, a subagent's brief and a web search's query. It also covers how a provider key is read, kept out of the environment and the launch strings, and redacted wherever it appears. The defence is against text an attacker plants in cluster data or model output, and against another process of this user reading a key. It assumes the user trusts the provider they picked, under that provider's or their own account's retention terms.

The first subagent's review is [the general agent](../security/2026-09-17-spawn-agent.md).

## Protections

| ID | Protection | Status | Residual |
| --- | --- | --- | --- |
| MODEL-1 | A chat's transcript leaves the machine, to the provider the user picked for each send. | **Pending rebuild**, was By decision | — |
| MODEL-2 | An injected instruction buys a misleading answer, a list of names, or a command the user reads before it runs, and nothing more. | **Pending rebuild**, was By decision | — |
| MODEL-3 | A send may name any provider, so a chat that switches vendor sends the new one its whole transcript. No provider's payloads reach another. | **Pending rebuild**, was By decision | — |
| MODEL-4 | OpenAI requests carry `store: false`. | **Pending rebuild**, was By decision | Anthropic retention is the account's. The other seven direct Chat Completions vendors are account-side only. |
| MODEL-5 | OpenRouter requests carry `provider.data_collection: deny`, which restricts routing to downstream providers OpenRouter classifies as not collecting user data. | **Pending rebuild**, was By decision | This is OpenRouter's classification, not zero retention. |
| MODEL-6 | OpenCode Go is a gateway rather than a vendor: the transcript reaches OpenCode's gateway and then the model's own provider. | **Pending rebuild**, was By decision | The transcript is retained per that model's published terms and monitored for abuse. |
| MODEL-7 | A request to xAI, Fireworks, OpenRouter, Mistral or OpenCode Go also carries the chat id, or a subagent's run id, as its affinity key. The key is a UUIDv7, which tells the vendor when the chat began and nothing else. | **Pending rebuild**, was By decision | — |
| MODEL-8 | A model whose entry states no output cap sends none. | **Pending rebuild**, was By decision | Its per-turn spend ceiling is the vendor's own. |
| MODEL-9 | A provider's key and a turn's body reach the provider's endpoint alone: no request to a model provider follows a redirect. | **Enforced** | — |
| MODEL-10 | The model reads the chat's cluster's cache, bodies included, unasked, through `KubeQuery`: up to 30,000 bytes inline and 8 MiB saved a call. | **By decision** | An off switch lands before a release build. |
| MODEL-11 | A KubeQuery statement writes nothing, opens no other file and leaves nothing for the next. | **Enforced** twice | — |
| MODEL-12 | A KubeQuery statement ends at its deadline and at its cache's clear. | **Enforced** | — |
| MODEL-13 | KubeQuery's functions bound their own work, since the deadline cannot interrupt them. | **Enforced** | — |
| MODEL-14 | A KubeQuery statement's temp disk is bounded by its deadline alone, a few hundred MB a call. | **By decision** | The row and byte limits trim the answer, not the work. No pragma caps a materialized subquery's temp storage. |
| MODEL-15 | A KubeQuery result is redacted as a command's output is, a JSON cell by its structure. | **Enforced** | An env value under an ordinary name, ConfigMap data and annotations are not redacted, as stated in the record. |
| MODEL-16 | A chat's KubeQuery reads its own cluster alone. | **Enforced** | — |
| MODEL-17 | A subagent's session is never looser than its parent's. | **Enforced** | The sandbox switch is copied at spawn, so a subagent started outside keeps running outside after the user switches back, each command still asking. |
| MODEL-18 | The model hands a task it wrote after reading the cluster to a subagent through `Agent`. The task is prose in the subagent's one message, after the chat's newest card. | **By decision** | — |
| MODEL-19 | The subagent holds every tool the parent holds but `Agent`, `Memory`, `LogsView` and `LogsWatch`, so depth is one. | **By decision** | — |
| MODEL-20 | A subagent's run is a committed row before it runs, and a start that fails leaves nothing running and no row behind. | **By decision** | — |
| MODEL-21 | A write failing in the subagent ends it alone, and no file stamp crosses between the subagent and its parent. | **By decision** | — |
| MODEL-22 | A question carries a cluster card to the provider the user picked: cluster data the user did not type. The card holds the cluster's display name and kube-context, the server version, the connection verdict and TLS posture, the freshness verdict with the timestamp the app last saw the cluster live, namespace names and the API groups served. | **Pending rebuild**, was By decision | — |
| MODEL-23 | The cluster card leaves out the endpoint, the principal and every count. | **Pending rebuild**, was By decision | — |
| MODEL-24 | The cluster card is bounded at 4 KiB. | **Pending rebuild**, was By decision | — |
| MODEL-25 | The cluster card carries names and the sidecar's own reasons, never a message from the cluster. Every value is an escaped JSON string, so none can close the block or end the fence. | **Pending rebuild**, was By decision | — |
| MODEL-26 | A transcript sent to a platform leaves the machine to the user's own cloud account, under that account's logging and retention. | **Pending rebuild**, was By decision | — |
| MODEL-27 | The sidecar can sign outbound model requests with the user's own cloud identity (AWS, Google, Entra ID) to a platform's own endpoints. It does so for no platform until a setting turns one on. | **Pending rebuild**, was By decision | The setting is not built. |
| MODEL-28 | A provider key is read from the environment and taken out of it before anything can exec. | **Enforced** | — |
| MODEL-29 | A provider key is taken out of the launch strings the kernel keeps, which another process of this user can read. | **Enforced** on macOS and Linux | — |
| MODEL-30 | A `--keys-fd` flag naming anything but a readable pipe stops the start, rather than run without the keys. | **Enforced** on macOS and Linux | — |
| MODEL-31 | Every SDK client reads nothing from the environment or the on-disk profile, under every variable its credential chain consults. | **Enforced** | — |
| MODEL-32 | The OpenAI SDK's one reader of its environment defaults is never built. No package but `llm` builds a client, and no other file of the module imports a model SDK. | **Enforced** | — |
| MODEL-33 | The launch's login shell is asked for every provider key too, apart from the allowlist, and a key the launch set wins. | **Enforced** | A startup file chooses the key, the trust a terminal launch gives it. |
| MODEL-34 | No key the login shell answers is set on the process or inherited by a child. | **Enforced** | — |
| MODEL-35 | Redaction knows every provider key by value, whatever its shape. | **Enforced** | — |
| MODEL-36 | No discovery runs: every provider's catalog is written, and every base URL is a constant. | **Enforced** | — |
| MODEL-37 | The sidecar dials loopback at startup: Ollama discovery runs in every build, with no key. It sends nothing but `GET /api/tags` and, per model listed without capabilities, `POST /api/show` naming it. | **Pending rebuild**, was Enforced | — |
| MODEL-38 | A cloud-backed Ollama model is never listed. A model with a `remote_host` runs on Ollama's servers, and a transcript sent to it would leave the machine under a heading that says local. | **Pending rebuild**, was Enforced | — |
| MODEL-39 | `OLLAMA_HOST` is honoured wherever it points. | **Pending rebuild**, was By decision | — |
| MODEL-40 | `OLLAMA_HOST` is on the macOS shell-environment allowlist. | **Enforced** | — |
| MODEL-41 | `GOOGLE_CLOUD_PROJECT`, `GOOGLE_CLOUD_LOCATION` and `CLOUDSDK_ACTIVE_CONFIG_NAME` are on the macOS shell-environment allowlist. | **Enforced** | — |
| MODEL-42 | A provider's error is stored as its status, type and code, never its response body. | **Enforced** | — |
| MODEL-43 | A model's query leaves the machine to its provider's search backend, on every turn of a Claude model on the Anthropic provider, capped at 5 per turn. | **By decision** | — |
| MODEL-44 | The transcript draws every search query the client was told of, and the sources beside it, so the user sees what went. | **By decision** | — |
| MODEL-45 | The search is the model's direct call, never a code-execution container's. Every Claude model is offered it, and no other provider's list but the fake's names a vendor's tool. | **By decision** | — |
| MODEL-46 | A subagent's gated calls wait on the user. | **Enforced** | — |
| MODEL-47 | A subagent's web searches, like the parent's, do not wait on the user. A subagent's search is the provider's, approved by no one, on a turn's search budget of its own. | **By decision** | — |
| MODEL-48 | The model puts a subagent on any model of its provider that takes tools, one the user did not pick and which may cost more. | **By decision** | — |
| MODEL-49 | At most 4 agents of 16 calls each run at once in a chat, and 16 background agents and commands in the app. | **By decision** | — |
| MODEL-50 | An agent runs in the background: its requests wait on the user whatever the parent is doing, several at once and out of view. | **By decision** | — |
| MODEL-51 | A request out of view is pointed to, never approved from, and Approve arms once its place has held. | **By decision** | — |
| MODEL-52 | A background agent's report reaches the model as a notice in a user message, escaped in its notice. | **By decision** | — |
| MODEL-53 | A background agent's end starts a turn only under a user's send, and no chain runs past the turn an agent's end starts. | **By decision** | — |
| MODEL-54 | A Cancel leaves a background agent running. | **By decision** | The model can speak again after a Cancel, when an agent the send launched ends after it. |
| MODEL-55 | A subagent's `TaskStop` reaches only its own tasks. | **By decision** | — |

## Evidence

### MODEL-1

- **Where:** `sidecar/internal/llm`, `sidecar/internal/agent/chat`
- **Records:** [model provider egress](../security/2026-09-11-model-provider-egress.md), widened by [cloud providers from the environment](../security/2026-09-12-cloud-providers-from-the-environment.md) and [cloud platforms](../security/2026-09-13-cloud-platforms.md)
- Note: the user's own send is the gesture.
- Note: this row stays **Pending rebuild** for the tools it describes. MODEL-2 through MODEL-8 come from the same source row.

### MODEL-2

- **Where:** `sidecar/internal/llm`, `sidecar/internal/agent/chat`
- **Records:** [tools on the Responses API](../security/2026-09-18-tools-on-the-responses-api.md), [the shell tool](../security/2026-09-18-shell-tool.md)
- Note: the model's cluster tool lists names from the local mirror (the row below).
- Note: the model's bash tool runs only what the user approved, on the Anthropic and OpenAI providers alike.
- Note: on the OpenAI provider the command is a list of them, each read alone.

### MODEL-3

- **Where:** `sidecar/internal/llm`, `sidecar/internal/agent/chat`
- **Records:** [a chat switches vendor](../security/2026-09-25-a-chat-switches-vendor.md)
- `TestAChatMovesAcrossDialects`, `TestEveryDialectReplaysEveryOtherDialectsTurn` — a chat that switches vendor sends the new one its whole transcript, and no provider's payloads reach another.

### MODEL-4

- **Where:** `sidecar/internal/llm`, `sidecar/internal/agent/chat`
- `TestResponsesStreamsAReplyAndRecordsIt` — OpenAI requests carry `store: false`.

### MODEL-5

- **Where:** `sidecar/internal/llm`, `sidecar/internal/agent/chat`
- `TestOpenRouterProviderDeniesDataCollection` — OpenRouter requests carry `provider.data_collection: deny`.

### MODEL-6

- **Where:** `sidecar/internal/llm`, `sidecar/internal/agent/chat`
- Note: OpenCode calls its gateway Console.
- Note: the model's published terms are 0–30 days of retention, with training off by default.

### MODEL-7

- **Where:** `sidecar/internal/llm`, `sidecar/internal/agent/chat`
- `TestChatCompletionsSendsTheAffinityKeyWhereTheRowSaysIt` — the affinity key is sent to the vendors whose row says it.

### MODEL-8

- **Where:** `sidecar/internal/llm`, `sidecar/internal/agent/chat`

### MODEL-9

- **Where:** `sidecar/internal/llm/helpers.go`
- `TestEveryDialectFollowsNoRedirect` — no request to a model provider follows a redirect.

### MODEL-10

- **Where:** `sidecar/internal/tools/kubequery`, `sidecar/internal/services/cluster/internal/kubestore/query.go`
- **Records:** [KubeQuery reads without asking](../adr/2026-09-27-kubequery-reads-without-asking.md), recorded in [KubeQuery reads the mirror](../security/2026-09-27-kubequery-reads-the-mirror.md), [TODO](../TODO.md#security)
- Note: the send is the gesture.
- Note: the transcript shows every query and what it read.

### MODEL-11

- **Where:** `sidecar/internal/services/cluster/internal/kubestore/query.go`, `sidecar/internal/lib/sqlitepool`
- `TestAQueryConnectionCannotWrite`, `TestAQueryConnectionCannotAttach`, `TestAQueryConnectionLeavesNothingForTheNext` — enforced by the connection.
- `TestOnlyAQueryRuns`, `TestACommentCannotEscapeTheWrap`, `TestASemicolonBeforeTheEndIsRefused` — enforced by the wrap.
- Note: the wrap prepares the statement as a subquery before anything else, so no pragma compiles.

### MODEL-12

- **Where:** `sidecar/internal/services/cluster/internal/kubestore/query.go`
- `TestADeadlinePastTheFirstRowInterruptsTheQuery`, `TestAClearInterruptsARunningQuery` — the deadline and the clear each interrupt a running query.
- Note: the wrap runs the whole statement in the one step modernc interrupts.

### MODEL-13

- **Where:** `sidecar/internal/services/cluster/internal/kubestore/query.go`, `sidecar/internal/services/cluster/internal/kubestore/quantity.go`
- **Records:** [ADR](../adr/2026-09-27-kubequery-reads-tables-written-with-the-object.md)
- `TestBodyStopsAtTheLengthLimit`, `TestQuantityOnAQueryConnection`, `TestParseQuantityRefusesWhatItCannotAfford` — the limits of `body()` and `quantity()`.
- Note: `body()` inflates at most 16 MiB.
- Note: `quantity()` refuses text past 64 bytes or an exponent past ±1,000 before it parses.
- Note: no function a statement can call parses a body, per the ADR.

### MODEL-14

- **Where:** `sidecar/internal/services/cluster/internal/kubestore/query.go`
- **Records:** [KubeQuery reads views on a connection of its own](../adr/2026-09-27-kubequery-reads-views-on-a-connection-of-its-own.md), stated in [KubeQuery reads the mirror](../security/2026-09-27-kubequery-reads-the-mirror.md)

### MODEL-15

- **Where:** `sidecar/internal/tools/kubequery`, `sidecar/internal/lib/safe/json.go`
- `TestEveryTextCellIsRedacted` — every text cell of a result is redacted.
- `TestRedactJSONReadsEscapedLines`, `TestRedactJSONRedactsACredentialMember`, `TestRedactJSONRedactsANamedValue` — a JSON cell is redacted by its structure.

### MODEL-16

- **Where:** `sidecar/internal/agent/chat/turn.go`, `sidecar/internal/tools/kubequery/cluster.go`
- `TestATurnsRuntimeIsItsChatsCluster`, `TestASubagentsRuntimeIsItsChatsCluster` — a turn's runtime and a subagent's each carry the chat's stored cluster.
- Note: the tool reads `Runtime.ClusterID`, which `agent/chat` sets from the chat's stored cluster (`chatOf`), for a turn and its subagents alike.

### MODEL-17

- **Where:** `sidecar/internal/run/session`, `sidecar/internal/agent/chat/subagent.go`
- **Records:** [outside the sandbox is the user's choice](../security/2026-09-30-outside-the-sandbox-is-the-users-choice.md)
- `TestNarrowKeepsTheParentsIdentity`, `TestASubagentsRuntimeCarriesItsParentsSwitch` — identity is copied at spawn, and policy is read live through the parent and never widened.
- Note: a subagent's session is `session.Narrow` of its parent turn's.
- Note: policy the user can change is read live through the parent.
- Note: the residual is the one in the linked record.

### MODEL-18

- **Where:** `sidecar/internal/tools/agent`, `sidecar/internal/agent/chat/subagent.go`
- **Records:** [an agent is a tool in the box](../adr/2026-09-25-an-agent-is-a-tool-in-the-box.md), recorded in [the Agent tool](../security/2026-09-25-agent-tool.md), [an agent runs in the background](../adr/2026-09-25-an-agent-runs-in-the-background.md), [background agents](../security/2026-09-25-background-agents.md)
- `TestTheSubagentsMessageIsTheNewestCardThenThePrompt` — the brief.
- `TestTheSubagentsPromptIsAssembledInOrder` — what the subagent is told.
- Note: the brief is guarded by the prompt, and the gate is the bound.
- Note: every gated call of the subagent waits on the user, on a request that says an agent asks.
- Note: the subagent runs in the background.

### MODEL-19

- **Where:** `sidecar/internal/tools/agent`, `sidecar/internal/agent/chat/subagent.go`
- **Records:** [an agent is a tool in the box](../adr/2026-09-25-an-agent-is-a-tool-in-the-box.md), recorded in [the Agent tool](../security/2026-09-25-agent-tool.md)
- `TestTheSubagentIsOfferedEveryToolButAgentAndMemory`, `TestASubagentAskingToSpawnIsRefused` — depth is one.

### MODEL-20

- **Where:** `sidecar/internal/tools/agent`, `sidecar/internal/agent/chat/subagent.go`
- **Records:** [an agent is a tool in the box](../adr/2026-09-25-an-agent-is-a-tool-in-the-box.md), recorded in [the Agent tool](../security/2026-09-25-agent-tool.md)
- `TestTheLinkIsOnDiskBeforeTheSubagentRuns` — the subagent's run is a committed row before it runs.
- `TestAFailedStartAnswersCouldNotStartAndTheTurnGoesOn`, `TestACancelDuringTheStartStartsNothing`, `TestAFailedStartTakesBackTheRunAndTheLink` — a start that fails leaves nothing running and no row behind.

### MODEL-21

- **Where:** `sidecar/internal/tools/agent`, `sidecar/internal/agent/chat/subagent.go`
- **Records:** [an agent is a tool in the box](../adr/2026-09-25-an-agent-is-a-tool-in-the-box.md), recorded in [the Agent tool](../security/2026-09-25-agent-tool.md)
- `TestAWriteFailedInASubagentFailsItAlone` — a write failing in the subagent ends it alone.
- `TestASubagentsStampsAreItsOwn` — no file stamp crosses between the two.

### MODEL-22

- **Where:** `sidecar/internal/services/cluster/clustercard`, `sidecar/internal/agent/chat`
- **Records:** [the cluster card rides the user message](../adr/2026-09-13-the-cluster-card-rides-the-user-message.md), recorded in [model provider egress](../security/2026-09-11-model-provider-egress.md)
- Note: the card is a fenced JSON object in one `context` block, wrapped in `<context>` tags by `llm` on the wire.
- Note: the transcript does not draw it.

### MODEL-23

- **Where:** `sidecar/internal/services/cluster/clustercard`, `sidecar/internal/agent/chat`
- **Records:** [the cluster card carries no counts](../adr/2026-09-14-the-cluster-card-carries-no-counts.md)

### MODEL-24

- **Where:** `sidecar/internal/services/cluster/clustercard`, `sidecar/internal/agent/chat`
- `TestRenderAdversarialLengthsStayUnderBudget` — the card stays under 4 KiB.

### MODEL-25

- **Where:** `sidecar/internal/services/cluster/clustercard`, `sidecar/internal/agent/chat`
- `TestRenderKeepsEveryValueInsideTheShape` — no value can close the block or end the fence.

### MODEL-26

- **Records:** [cloud platforms](../security/2026-09-13-cloud-platforms.md)
- Note: was in `sidecar/internal/llm/platforms/{bedrock,vertex,foundry}.go`.
- Note: the record states each platform's published position.
- Note: Bedrock stores prompts only with model invocation logging on, and its Responses API keeps no state (the row omits `store`).
- Note: Vertex's partner-model terms are the project's.
- Note: a Foundry Claude deployment hosted on Azure keeps prompts in Azure and sends Anthropic usage metadata and safety-flagged content alone.
- Note: a Foundry Claude deployment hosted on Anthropic is under Anthropic's terms.

### MODEL-27

- `TestEveryRowIsWhole` — the registry's table is the vendor rows alone.
- `TestEveryCellRoutesAndSignsAsThePlatformDoes` — the path and the credential under every auth.
- `TestEveryCellTakesNothingFromTheEnvironment` — the vendors' variables reach no request under any auth.
- Note: was in `sidecar/internal/llm/platforms/`.
- Note: the decision is *a platform is a setting, and its SDK's conventions*.
- Note: a cluster credential and a model credential are the same identity on these platforms, so neither a chain's presence nor a platform's own variables are consent. The setting is.

### MODEL-28

- **Where:** `sidecar/config.go`, `sidecar/main.go`
- `TestConfigFromArgsReadsTheProviderKeys` — the read of every key variable in `catalog.KeyVars()`.
- `TestTakeProviderKeysClearsOnlyTheTable` — the clear of exactly the table's variables, with the config still holding the key and another variable untouched.
- Note: `catalog.KeyVars()` names eleven variables: `ANTHROPIC_API_KEY`, `OPENAI_API_KEY`, `GEMINI_API_KEY`, `GROQ_API_KEY`, `MISTRAL_API_KEY`, `DEEPSEEK_API_KEY`, `XAI_API_KEY`, `OPENROUTER_API_KEY`, `TOGETHER_API_KEY`, `FIREWORKS_API_KEY`, `OPENCODE_GO_API_KEY`.
- Note: the table grows with the rows.

### MODEL-29

- **Where:** `sidecar/scrubexec_unix.go`, `sidecar/main.go`
- **Records:** [provider keys leave the launch strings](../security/2026-10-28-provider-keys-leave-the-launch-strings.md)
- `TestTheReexecKeepsTheKeyOutOfTheLaunchStrings` — reads the re-exec'd process's launch strings through each reader and finds neither the name nor the value, with the key still held as the provider's environment key.
- `TestAKeyInTheEnvironmentIsMovedToThePipe` — argv carries the pipe's number and never a key.
- Note: on macOS and Linux a sidecar started with a key variable set writes the keys to a pipe and execs itself once, first thing in `main`.
- Note: the re-exec leaves the key variables out and passes `--keys-fd` naming the pipe, so `kern.procargs2` and `/proc/<pid>/environ` answer without them.
- Note: Windows has no exec and no reader of another process's launch environment.

### MODEL-30

- **Where:** `sidecar/scrubexec_unix.go`, `sidecar/main.go`
- **Records:** [provider keys leave the launch strings](../security/2026-10-28-provider-keys-leave-the-launch-strings.md)
- `TestABadKeysFDStopsTheSidecar`, `TestAKeysFDThatIsNotAPipeIsAnError` — a flag naming anything but a readable pipe stops the start.

### MODEL-31

- **Where:** `sidecar/internal/llm`
- `TestMessagesClientTakesNothingFromTheEnvironment`, `TestResponsesClientTakesNothingFromTheEnvironment`, `TestChatCompletionsClientTakesNothingFromTheEnvironment` — every SDK client reads nothing from the environment or the on-disk profile, under every variable its credential chain consults.
- Note: the platforms' credential chains return with their rows.

### MODEL-32

- **Where:** `sidecar/internal/llm`
- `TestNoDialectReadsTheSDKsEnvironmentDefaults` — walks the dialect files for a call to `openai.NewClient` or `openai.DefaultClientOptions`, and every tool's wire file for any client or service constructor of either SDK.
- `TestOnlyAWireFileImportsAModelSDK` — keeps every other file of the module from importing an SDK at all.
- Note: the OpenAI SDK has no public switch that skips its defaults, so the guarantee is that its one reader of them is never built.

### MODEL-33

- **Where:** `sidecar/internal/app/launchshell.go`, `sidecar/internal/app/launchshell_unix.go`
- **Records:** [provider keys from the login shell](../security/2026-10-07-provider-keys-from-the-login-shell.md)
- `TestResolveReadsTheKeysItIsAskedFor`, `TestNewTakesTheProviderKeysTheShellSets`, `TestWithShellKeysPrefersTheLaunch` — the login shell answers the keys apart from the allowlist, and one the launch set wins.

### MODEL-34

- **Where:** `sidecar/internal/app/launchshell.go`, `sidecar/internal/app/launchshell_unix.go`
- `TestAKeyFromTheShellIsRedactedAndNeverInherited` — no key the shell answers is set on the process or inherited by a child.

### MODEL-35

- **Where:** `sidecar/internal/lib/safe`, `sidecar/config.go`, `sidecar/internal/app/launchshell_unix.go`
- `TestConfigFromArgsRegistersEveryKeyWithTheRedactor` — every key read is registered before the logger exists.
- `TestAKeyFromTheShellIsRedactedAndNeverInherited` — every key the login shell answered is registered before `runShell` logs, and no line it logs carries one.
- `TestSafeBlanksARegisteredValueWhereverItAppears`, `TestSafeBlanksALongKeyWholeWhenAShortOneIsInsideIt`, `TestSafeRegistersNoValueUnderSixteenBytes` — the blanking in prose, a URL and JSON, the span merge, and the floor.

### MODEL-36

- **Where:** `sidecar/internal/agent/catalog/providers.go`
- `TestEveryKeyedProviderIsWhole` — every provider's dialect, base URL, key variable and catalog.
- `TestEachKeyedProviderIsWrittenAsItsRow` — each row's values.
- Note: both tests are in `catalog/providers_test.go`.
- Note: the providers are data, so nothing is fetched before a send and no `GET /models` leaves the machine.
- Note: discovery returns with the Ollama entry, which cannot exist without it, and the debug-build base-URL override with it.

### MODEL-37

- `TestOllamaDiscoveryReadsTheCurrentTags`, `TestOllamaDiscoveryAsksShowWhenTagsLacksCapabilities` — each request's method, path and body, and that no `Authorization` header is sent.
- Note: was in `sidecar/internal/llm/providers/ollama.go`, `sidecar/internal/llm/discover.go`.

### MODEL-38

- Note: was in `sidecar/internal/llm/providers/ollama.go`.
- Note: both discovery tests serve a cloud-backed model and assert it is absent.

### MODEL-39

- **Where:** `sidecar/config.go`
- `TestParseOllamaHost` — the reading, wildcard-to-loopback included.
- `TestResolveProviderEndpointsWarnsOnAnUnusableOllamaHost` — a bad value warns without echoing it.
- Note: was in `sidecar/internal/llm/providers/ollama.go`.
- Note: the decision is *Ollama is local, and `OLLAMA_HOST` says where*.
- Note: it names the user's own daemon, which every Ollama client on the machine dials, so a transcript reaching it is the same decision as a transcript reaching a vendor.

### MODEL-40

- **Where:** `sidecar/internal/run/loginshell`
- `TestImportedIsExactlyTheAllowlist` — every row of the allowlist by name and kind.
- Note: the row is `plain`, an endpoint and not a credential.

### MODEL-41

- **Where:** `sidecar/internal/run/loginshell`
- `TestImportedIsExactlyTheAllowlist` — every row of the allowlist by name and kind.
- `TestBaseURLRefusesWhatIsNotAProjectOrLocation` — the Google connector builds no endpoint from a value that is not well formed.
- Note: each row is `plain`, an id and not a credential.

### MODEL-42

- **Where:** `sidecar/internal/llm/error.go`
- `TestMessagesKeepsAMalformedReplyOutOfTheRecordAndTheLog`, `TestResponsesKeepsAMalformedReplyOutOfTheRecordAndTheLog`, `TestChatCompletionsKeepsAMalformedReplyOutOfTheRecordAndTheLog` — serve a body echoing a marker and assert none of it reaches the error's text or any log record.
- `TestEveryDialectRendersAFailureWithoutTheBody` — the same over a body echoing the key, once per wire.
- Note: `llm.Error` has no message field.
- Note: every dialect renders a failure through one mapping: the API's status, and its type only when the API defines that word, never its message.
- Note: the marker bodies are an undefined error type, a body that is not SSE, a status line that is not HTTP, and an error inside the stream.

### MODEL-43

- **Where:** `sidecar/internal/agent/catalog`, `sidecar/internal/agent`, `sidecar/internal/agent/chat`
- **Records:** [web search](../security/2026-09-17-web-search.md)
- `TestEachRoundIsOfferedWhatIsLeftOfTheSearchBudget`, `TestAPauseIsNotResumedOnceAServerToolIsSpent` — the cap is the turn's.

### MODEL-44

- **Where:** `sidecar/internal/agent/catalog`, `sidecar/internal/agent`, `sidecar/internal/agent/chat`
- **Records:** [web search](../security/2026-09-17-web-search.md)
- `TestABrokenReplyKeepsItsServerCalls` — the queries stay on a reply that broke off.
- `TestASearchIsDrawnAsItsQueryAndItsSources` — the settled row.
- Note: the transcript draws the queries as *Searched the web*.
- Note: `searchesOf`/`sourcesOf` are the readers.

### MODEL-45

- **Where:** `sidecar/internal/agent/catalog`, `sidecar/internal/agent`, `sidecar/internal/agent/chat`
- **Records:** [web search](../security/2026-09-17-web-search.md)
- `TestMessagesOffersTheSearch` — the search is the model's direct call, never a code-execution container's.
- `TestEachTargetIsOfferedItsList` — the `anthropic` list names the search and every Claude model is offered it.
- `TestOnlyAnthropicAndTheFakeListAVendorTool` — no other provider's list but the fake's names a vendor's tool.

### MODEL-46

- **Where:** `sidecar/internal/agent/chat/subagent.go`, `sidecar/internal/agent/chat/approval.go`
- `TestASubagentsGatedCallWaitsOnTheUser` — a subagent's gated call is a request in the parent's answer, and nothing runs before the user decides.
- `chat-transcript/transcript.test.tsx` — the request opens with *An agent asks:*.
- Note: the subagent's run is `waiting_approval` until the user decides and the parent's is left alone, whether or not the parent is still answering.

### MODEL-47

- **Where:** `sidecar/internal/agent/chat/subagent.go`, `sidecar/internal/agent/chat/approval.go`
- **Records:** [the Agent tool](../security/2026-09-25-agent-tool.md)

### MODEL-48

- **Where:** `sidecar/internal/tools/agent`, `sidecar/internal/agent/chat/subagent.go`
- **Records:** [an agent is a tool in the box](../adr/2026-09-25-an-agent-is-a-tool-in-the-box.md), recorded in [the Agent tool](../security/2026-09-25-agent-tool.md)
- `TestTheModelEnumIsTheProvidersToolModels` — `model` is an enum of the provider's tool-taking models, built per turn.
- `TestAnUnknownModelWritesNoRun` — another provider's model is refused.
- Note: the transcript names the model on the call.

### MODEL-49

- **Where:** `sidecar/internal/tools/agent`, `sidecar/internal/agent/chat/subagent.go`, `sidecar/internal/agent/chat/tasks.go`
- **Records:** [an agent is a tool in the box](../adr/2026-09-25-an-agent-is-a-tool-in-the-box.md), recorded in [the Agent tool](../security/2026-09-25-agent-tool.md), [an agent runs in the background](../adr/2026-09-25-an-agent-runs-in-the-background.md), recorded in [background agents](../security/2026-09-25-background-agents.md)
- `TestALimitIsARefusal` — the shared slots.

### MODEL-50

- **Where:** `sidecar/internal/agent/chat/subagent.go`, `sidecar/internal/agent/chat/approval.go`
- **Records:** [an agent runs in the background](../adr/2026-09-25-an-agent-runs-in-the-background.md), recorded in [background agents](../security/2026-09-25-background-agents.md)
- `TestAnUnansweredRequestStopsItsAgent` — a request stands 30 minutes at most.
- `TestTheParentsOwnRequestWaits` — the parent's request is unbounded.

### MODEL-51

- **Where:** `sidecar/internal/agent/chat/approval.go`, `src/components/chat/chat-transcript/transcript.tsx`, `src/components/chat/chat-pane.tsx`, `src/components/chat/chat-nav.tsx`, `src/lib/held-still.ts`
- **Records:** [an agent runs in the background](../adr/2026-09-25-an-agent-runs-in-the-background.md), recorded in [background agents](../security/2026-09-25-background-agents.md)
- `TestAWaitingAgentMarksItsChat` — a request out of view is pointed to, never approved from.
- Note: the composer's and the list's cases pin the same with it.
- Note: the transcript's arming cases pin that Approve arms once its place has held.

### MODEL-52

- **Where:** `sidecar/internal/agent/chat/subagent.go`, `sidecar/internal/agent/chat/notices.go`
- **Records:** [an agent runs in the background](../adr/2026-09-25-an-agent-runs-in-the-background.md), recorded in [background agents](../security/2026-09-25-background-agents.md)
- `TestAResultCannotCloseItsTag` — a report is escaped in its notice.

### MODEL-53

- **Where:** `sidecar/internal/agent/chat/subagent.go`, `sidecar/internal/agent/chat/notices.go`
- **Records:** [an agent runs in the background](../adr/2026-09-25-an-agent-runs-in-the-background.md), recorded in [background agents](../security/2026-09-25-background-agents.md)
- `TestAnAgentFromANoticeTurnStartsNoTurn` — no chain runs past the turn an agent's end starts, since `Agent` is ungated.

### MODEL-54

- **Where:** `sidecar/internal/agent/chat/subagent.go`
- **Records:** [an agent runs in the background](../adr/2026-09-25-an-agent-runs-in-the-background.md), recorded in [background agents](../security/2026-09-25-background-agents.md)

### MODEL-55

- **Where:** `sidecar/internal/agent/chat/tasks.go`
- **Records:** [an agent runs in the background](../adr/2026-09-25-an-agent-runs-in-the-background.md), recorded in [background agents](../security/2026-09-25-background-agents.md)
- `TestASubagentStopsOnlyWhatItStarted` — a subagent's `TaskStop` reaches only its own tasks.
