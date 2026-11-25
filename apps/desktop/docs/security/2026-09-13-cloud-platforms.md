# Security record — every model on Bedrock, Vertex and Foundry, 13 September 2026

**Subject:** the sidecar can sign outbound model requests with the user's own cloud identity —
AWS, Google, Entra ID — for Bedrock, Vertex AI and Foundry, though no platform is on in any
build yet. The directory is append-only, so this record widens the [12 September
record](2026-09-12-cloud-providers-from-the-environment.md): what it says about the gesture,
prompt injection and redaction holds unchanged, and its *Bedrock and Vertex are not rows* item
is superseded here. The living model is [security-model.md](../security-model.md); the decision
was *a platform is a setting, and its SDK's conventions*, an ADR removed with the platform code.

## What is new

- **Eight rows, three platforms, on in no build.** `platforms.All()` holds them and the
  encoders sign for them, but nothing puts a platform row in the registry: consent is a
  setting the Settings spec has yet to design, and the environment is never read as consent.
  `TestEveryRowIsWhole` still pins the registry's table as `providers.All()` alone.
- **No `KUBETAIL_` variable, no platform key in the config.** A platform row states no value;
  the platform's own conventions supply its region, project, resource and credential — read by
  the SDKs themselves on Bedrock (`AWS_REGION`, the profile, `AWS_BEARER_TOKEN_BEDROCK`, the
  default chain), by `platforms/vertex.go` and `platforms/foundry.go` for Vertex (`CLOUD_ML_REGION`,
  `ANTHROPIC_VERTEX_PROJECT_ID`, `GOOGLE_CLOUD_PROJECT`, ADC's project) and Foundry
  (`ANTHROPIC_FOUNDRY_RESOURCE`, `ANTHROPIC_FOUNDRY_API_KEY`). The sidecar neither reads a
  platform key into its config nor clears one: a Bedrock or Foundry key in the environment
  is read by the SDK at the turn, as by any client of that platform, and no kubeconfig plugin
  reads either. `TestAFoundryRowIsFilledFromItsConventions` and `TestAVertexRowIsFilledFromItsConventions` pin the reading;
  `TestTakeProviderKeysClearsEveryCloudKey` still pins that the ten vendor keys are what is
  cleared.
- **The chain is walked once per row and endpoint, on the first turn, and a chain that finds
  nothing is a refused turn.** The AWS chain is the SDK's own, inside the client it builds,
  with the SDK's default metadata probe; ADC and Entra ID are reached through one seam each.
  `TestEachChainIsWalkedOncePerRow`, `TestAWSChainIsWalkedOncePerRow` and
  `TestAChainThatFindsNothingRefusesTheTurn` pin it.
- **The request goes to the platform's own endpoint, signed, and nowhere else.** The
  conformance suite runs every case under every auth (`cells()` in `conformance_test.go`);
  `TestEveryCellRoutesAndSignsAsThePlatformDoes` pins the path, the credential header and that
  the body is the vendor's; `TestEveryCellTakesNothingFromTheEnvironment` sets both vendors'
  variables — and the platforms' whole-URL variables — and asserts none reaches a request
  under any auth: a stated base URL wins. A platform's refusal is stored as its status alone
  (`TestEveryCellRendersAPlatformRefusalAsItsStatus`).

## What each platform retains

- **Bedrock** stores prompts and completions only when model invocation logging is switched on
  in the account; Mantle serves the vendor's own API, so an OpenAI request carries no `store`
  field there — Bedrock's Responses API keeps no state.
- **Vertex AI** keeps a partner model's prompts under the project's own terms; nothing in the
  request changes that.
- **Foundry**: a Claude deployment hosted on Azure keeps prompts and completions in Azure and
  sends Anthropic usage metadata and safety-flagged content alone; one hosted on Anthropic is
  under Anthropic's terms. The OpenAI surfaces are the account's Azure OpenAI retention.

Each is the platform's published position on the day of this record, cited from its page and
not something the sidecar verifies.

## Open

- The consent gesture: a setting, not yet specified. Until it lands the rows are unreachable.
- The Foundry key headers and the Bedrock Responses omissions are the platforms' documented
  shapes, not yet confirmed against a live resource; the conformance suite pins what the
  sidecar sends, and a deployment that refuses it is found by the first turn.
