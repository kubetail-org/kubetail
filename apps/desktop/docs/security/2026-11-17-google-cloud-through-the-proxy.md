# Security record — Google Cloud through the proxy, 17 November 2026

**Subject:** step 2C of the cloud proxies, the commit that adds `services/gcpauth`,
`run/gcpproxy` and `tools/bash/gcp.go`. With `sandbox.gcp` on, `gcloud` in a sandboxed run works
as the user's active account on six services, through the run's door, with no Google credential
in the sandbox. The living model is [security-model.md](../security-model.md); the decision is
[the ADR](../adr/2026-11-17-google-cloud-is-reached-with-the-active-gcloud-accounts-token.md).

## What changes

**A sandboxed command reads the user's Google account, unasked.** On `compute`, `container`,
`iam`, `logging`, `storage` and `cloudresourcemanager`: cluster definitions, IAM policies, log
entries, bucket and object listings. The answers reach the model's provider. With network on, they
can be sent anywhere.

**The sidecar runs `gcloud`.** `gcloud config config-helper` runs outside the sandbox at a run's
mount with the switch on, where before it ran only as a kubeconfig's plugin. Its arguments are
constants.

**Approving a request changes the user's project.**

## What holds

- No Google credential is in the sandbox. The environment holds the grant's token, which opens
  Google's routes alone and ends with the run; the user's token lives in the sidecar's memory and
  is registered with `safe` while held (`TestNoGCPCredentialEntersTheSandbox`,
  `TestAHeldTokenIsARegisteredSecret`).
- The user's token goes only to the table's host for a path on the table, over TLS the sidecar
  verifies (`TestTheUpstreamIsTheTablesHost`, `TestAPathOffTheTableIsRefused`).
- Every change asks; an object's contents and what a guest wrote ask as class 6; a request whose
  answer is a credential is refused (`TestEveryRequestIsClassified`).
- An approved request is sent as the account it drew, or not at all: the active account
  switched while the user read it is refused
  (`TestAnApprovedChangeRunsAsTheApprovedAccount`).
- Instance and node metadata values, a VPN tunnel's shared secret and a cluster's `masterAuth`
  secrets are blanked in every answer, and an answer the rewriter cannot read is not passed on
  (`TestInstanceMetadataIsBlanked`, `TestNodeMetadataIsBlanked`,
  `TestAnAnswerThatCannotBeRewrittenFailsClosed`).
- No header off the list reaches Google, and a method override in the query is refused, so
  neither `X-HTTP-Method-Override` nor `$httpMethod` can turn a read into a change
  (`TestOnlyTheListedHeadersAreForwarded`, `TestAMethodOverrideInTheQueryIsRefused`).

## Residuals

Each is a **By decision** row in the model.

- The reads are as wide as the account.
- Log entries are read unasked, as a pod's logs are, and hold whatever a workload printed.
- The model never reads an instance's or a node's metadata values, and `add-metadata` does not
  work in the sandbox.
- A secret in a field no list names (a label, a description) is read unasked.
- An account or project the user switches to in a terminal reaches the sandbox only when the held
  token expires, up to an hour. Each request draws the account it uses.
- With no login, the helper runs again every 30 s while commands run.
- A `POST` that reads and is not on the list asks; a `GET` that answers something sensitive and is
  on no list passes.
- `gcloud` keeps its logs and configuration under the chat's tool home.

## Open

The spike against a real `gcloud` (the spec's task 1) has not run: the behaviours the run's
variables rest on are documented, not observed.
