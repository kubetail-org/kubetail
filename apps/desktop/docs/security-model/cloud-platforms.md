# Cloud platforms and GitHub

[← Security model](../security-model.md)

A sandboxed command reaches GitHub, Google Cloud, Azure and AWS as the user, through a proxy on the run's door, and holds none of the user's credentials. A request whose answer is a credential is refused, a change or a data read reaches the cloud platform only once a mode or rule the user set allows it or the user approves it, and any other read passes unasked. The rows defend against a model, or text it read, that tries to take a credential or change an account. They assume the user reads each request before approving it. Many rows are by decision: reads are as wide as the user's login, the lists of credential and secret routes are kept by hand, and every cloud platform whose login is on the machine is on until the user turns it off.

Each platform's proxy has its review: [a sandboxed command reaches AWS](../security/2026-11-17-sandboxed-aws.md), [Google Cloud through the proxy](../security/2026-11-17-google-cloud-through-the-proxy.md), [Azure through the proxy](../security/2026-11-17-azure-through-the-proxy.md) and [a sandboxed command reaches GitHub](../security/2026-11-17-a-sandboxed-command-reaches-github.md).

## Protections

| ID | Protection | Status | Residual |
| --- | --- | --- | --- |
| CLOUD-1 | Every cloud platform whose login is on the machine is reached by every chat's sandboxed commands unless the user turned its switch off. The switch is a `sandbox.<platform>.enabled` row of `app.db`'s `settings`. | **By decision** | — |
| CLOUD-2 | Cloud platform switches that cannot be read are off. | **By decision** | — |
| CLOUD-3 | A background command reads a cloud platform's account after its turn ends, until it ends or the user turns the switch off. | **By decision** | — |
| CLOUD-4 | A cloud platform's metadata is read unasked, so a secret a user put where no cloud platform's list names it reaches the model. | **By decision** | — |
| CLOUD-5 | A cloud platform's request is approved on its method, host, path, media type and body. The summary and details are a reading of it, and an `Unshown` body is approved on the cloud platform's sentence for it. | **By decision** | — |
| CLOUD-6 | A sandboxed command reaches GitHub holding no GitHub token. The run's environment and files hold the grant's token and a placeholder login alone. | **Enforced** | — |
| CLOUD-7 | The sidecar adds the GitHub token outside the sandbox. | **Enforced** | — |
| CLOUD-8 | GitHub's proxy sends each request to one of its two fixed origins. | **Enforced** | — |
| CLOUD-9 | GitHub's proxy sends each request with only its listed headers. | **Enforced** | — |
| CLOUD-10 | A sandboxed request whose answer is a GitHub credential is refused, never asked for, by name or by id. | **Enforced** | — |
| CLOUD-11 | A signed link GitHub answers with, in `Location` or `Link`, reaches a command only through a data read the user allowed or approved. | **Enforced** | — |
| CLOUD-12 | A link to GitHub's own origin, however it is spelled, is handed back as the route's. | **Enforced** | — |
| CLOUD-13 | A GitHub change reaches GitHub only once a mode or a rule allows it ([CLOUD-64](#cloud-64)) or the user approves it. | **Enforced** | — |
| CLOUD-14 | A GitHub API change shows the bytes sent. | **Enforced** | — |
| CLOUD-15 | A GraphQL document holding any mutation asks, whichever operation it selects. | **Enforced** | — |
| CLOUD-16 | A GitHub push asks and shows its repository, refs and options. | **Enforced** | — |
| CLOUD-17 | `gh` runs at most once a minute. | **Enforced** | — |
| CLOUD-18 | With GitHub on, a sandboxed command reads a repository's content unasked, private ones included. There is no repository fence: the token's reach is the proxy's. | **By decision** | — |
| CLOUD-19 | A GitHub push is approved on its repository, refs and options. Its pack is not shown. | **By decision** | — |
| CLOUD-20 | A GitHub answer's body can hold a signed link the lists do not name, such as a private file's `download_url`. | **By decision** | — |
| CLOUD-21 | GitHub's `credentialPaths` and `dataPaths` are kept by hand, and GraphQL is classified by Kubetail's parser. A route or field GitHub adds that answers a credential or a secret is a read until it joins one. | **By decision** | — |
| CLOUD-22 | `HTTP_PROXY` hands GitHub's grant token to every Go and Python program in a run with GitHub on. | **By decision** | — |
| CLOUD-23 | A sandboxed command's GitHub reads spend the user's rate limit. | **By decision** | — |
| CLOUD-24 | A redirect on GitHub's own origin comes back through the proxy as a new request, so an approved request GitHub redirects there asks again. | **By decision** | — |
| CLOUD-25 | `gh` on the sidecar's `PATH` runs with the user's authority to read their token, unless the GitHub connector is connected. | **By decision** | — |
| CLOUD-26 | No Google credential enters the sandbox. With Google Cloud's switch on (`sandbox.gcp.enabled`), a run holds the grant's token, and the user's token stays in the sidecar's memory. | **Enforced** | — |
| CLOUD-27 | A Google request whose answer is a credential is refused. | **Enforced** | — |
| CLOUD-28 | A Google read of an object's contents or of what a guest wrote is a data read: it asks unless a mode or a rule allows it. | **Enforced** | — |
| CLOUD-29 | Only the listed headers reach Google. | **Enforced** | — |
| CLOUD-30 | A method override in a Google request's query is refused, so nothing turns a read into a change. | **Enforced** | — |
| CLOUD-31 | An approved Google request is sent as the account it drew, or not at all. | **Enforced** | — |
| CLOUD-32 | The user's Google token goes only to a table host, on a path on the table. | **Enforced** | — |
| CLOUD-33 | Instance and node metadata values, a VPN tunnel's shared secret and a cluster's `masterAuth` secrets are blanked in every Google answer. | **Enforced** | — |
| CLOUD-34 | A Google answer that cannot be read is not passed on. | **Enforced** | — |
| CLOUD-35 | A sandboxed command reads what the user's Google account reads on six services, unasked, log entries included. | **By decision** | — |
| CLOUD-36 | A secret in a Google field no list names, such as a label or a description, is read unasked. A `POST` that reads and is on no list asks. | **By decision** | — |
| CLOUD-37 | `add-metadata` and `remove-metadata` do not work in the sandbox: a change carrying a blanked value is refused. | **By decision** | — |
| CLOUD-38 | An account or project switched in a terminal reaches the sandbox only when the held token expires, up to an hour. With no login, `gcloud` runs again every 30 s while commands run. | **By decision** | — |
| CLOUD-39 | `gcloud` keeps its logs and configuration under the chat's tool home. | **By decision** | — |
| CLOUD-40 | With Azure's switch on (`sandbox.azure.enabled`), a sandboxed command reaches Azure Resource Manager alone, as the user, holding no Azure credential. | **Enforced** | — |
| CLOUD-41 | A sandboxed command is refused every Azure action that answers a credential. | **Enforced** | — |
| CLOUD-42 | A change reaches Azure only once a mode or a rule allows it ([CLOUD-64](#cloud-64)) or the user approves it. | **Enforced** | — |
| CLOUD-43 | The run's door answers TLS for Azure's route alone. | **Enforced** | — |
| CLOUD-44 | The run's door's certificate names `127.0.0.1` alone, and its key stays in the sidecar's memory. | **Enforced** | — |
| CLOUD-45 | The sidecar runs the user's `az` from `PATH`, outside the sandbox, with fixed arguments, and holds the user's Resource Manager tokens in memory until five minutes before they expire. | **By decision** | Whoever controls `PATH` controls what runs, as with a kubeconfig's `exec` plugin. |
| CLOUD-46 | A run with Azure trusts a certificate Kubetail made through `REQUESTS_CA_BUNDLE`. With network on, every tool that reads it verifies public hosts against the system's bundle in place of its own. | **By decision** | — |
| CLOUD-47 | On macOS a run's `ca.pem` carries `/etc/ssl/cert.pem`, which holds no root added to the Keychain alone. With network on, a tool behind a TLS-inspecting proxy whose root lives only there fails to verify public hosts. | **By decision** | — |
| CLOUD-48 | Every Azure SDK in a run with Azure believes it has a managed identity. With network on, one that calls the real Resource Manager sends it the grant's token. | **By decision** | — |
| CLOUD-49 | An Azure `POST` that only reads asks as a change. One that answers a secret under a name the credential and list/get rules miss is a write off `ordinary`, asked each time and refused to the monitor. | **By decision** | — |
| CLOUD-50 | An Azure change that runs code on a machine (a VM extension, a deployment script) is a write off `ordinary`, which asks each time, and rests on the user's eye. | **By decision** | — |
| CLOUD-51 | A long `az … create` asks once per request. | **By decision** | — |
| CLOUD-52 | A sandboxed command reaches AWS only with AWS's switch on (`sandbox.aws.enabled`), only the thirteen listed services, as the user's identity and holding none of it. | **Enforced** | — |
| CLOUD-53 | The AWS proxy drops the run's signature and signs what it built for a host of its table. | **Enforced** | — |
| CLOUD-54 | An AWS answer that is a credential is refused unasked: STS but for `GetCallerIdentity`, key pairs, access keys, ECR logins, presigned layer URLs and S3 Express sessions. | **Enforced** | — |
| CLOUD-55 | An unasked AWS read names no other account's bucket, registry or template URL. | **Enforced** | — |
| CLOUD-56 | No AWS upload is approved unshown. | **Enforced** | — |
| CLOUD-57 | A chat's AWS identity is the profile its cluster's kube-context names in its `exec` block, else the sidecar's default chain. | **Enforced** | — |
| CLOUD-58 | The AWS profile's name passes `sandbox.AWSProfile` before the run's config file declares it. | **Enforced** | — |
| CLOUD-59 | Each AWS profile is its own login with its own bucket list. | **Enforced** | — |
| CLOUD-60 | A role or identity the `exec` block names otherwise is not read, and such a run uses the default chain. | **By decision** | — |
| CLOUD-61 | Resolving the user's AWS profile can run its `credential_process` outside the sandbox, with no stdin and no stderr. A role assumed from a source profile's process is no login. | **By decision** | — |
| CLOUD-62 | An AWS read is unasked and the service table is hand-written. Metadata no list names (a tag, a CloudFront origin's header, a log event) reaches the model, a parameter no row names can reach another account's log, and a credential-answering read AWS adds is unasked until a row names it. | **By decision** | — |
| CLOUD-63 | No file is uploaded to S3 from the sandbox, and a login that cannot list its buckets is asked for every bucket read. | **By decision** | — |
| CLOUD-64 | A cloud platform's request is decided as a cluster's, by its account's mode and the rules. A change a mode or a rule may allow is one its platform's `ordinary` table names; any other asks each time. | **Enforced** | — |
| CLOUD-65 | An access control change asks each time, but an Azure role assignment in a resource group. | **Enforced** | — |
| CLOUD-66 | A request whose account the classifier cannot trust (none on the credentials, a route by id, a project number) asks each time. One reaching past a known account (another named in its body or a header, a bucket it does not own, another project billed) keeps that account's scope, so its mode and rules still refuse it, and asks each time. A request billing more than one project is refused. | **Enforced** | — |
| CLOUD-67 | A data read in the account runs unasked under `auto`. | **Enforced** | — |
| CLOUD-70 | A GitHub change setting a repository's visibility or a pull request's maintainer edits, a repository's transfer, a GitHub GraphQL mutation that changes who may do what, and a Cloud Storage ACL or predefined ACL change are access control changes, read from decoded fields. A GraphQL document mixing one with a destructive mutation is refused. | **Enforced** | — |
| CLOUD-72 | A data read that gathers several places (an Azure data read in no resource group, a GitHub owner's alerts or archives) is decided by a deny or ask for any place below it, and allowed only by an allow reaching them all. | **Enforced** | — |
| CLOUD-71 | Opening a GitHub pull request keeps GitHub's default letting the base's maintainers push to its branch, and runs unasked under `auto` or an allow. Changing that afterward is an access control change. | **By decision** | The base's maintainers may push to a fork's branch the user opened the pull request from. |
| CLOUD-68 | A change to who may reach an S3 bucket or key (its policy, ACL, public access block or ownership controls, or an ACL or grant header) and a change to an Azure role assignment, role or lock, at any scope, are access control changes, which an access control rule decides. | **Enforced** | — |
| CLOUD-69 | An AWS request on a bucket the login owns is sent expecting that owner, so S3 refuses it if the bucket changed hands; a caller naming another owner asks. The list of owned buckets answers only for the login it was listed with, and a change is sent as the login that was decided on or not at all. | **Enforced** | A login with no account id sends no owner condition, so a bucket that changes hands within the list's minute is read unasked. |

## Evidence

### CLOUD-1

- **Where:** `sidecar/internal/services/settings/cloudplatform.go`, `sidecar/internal/tools/bash/cloud.go`
- **Records:** [every cloud platform is on until the user turns it off](../adr/2026-11-18-every-cloud-platform-is-on-until-the-user-turns-it-off.md), [record](../security/2026-11-18-cloud-platforms-are-on-by-default.md)
- `TestEachCloudPlatformStartsOn`, `TestSetCloudPlatformSwitchWritesOneSwitch` — the default and the off.
- Note: Settings draws no switch yet, so off is a `sandbox.<platform>.enabled` row with value 0, inserted into `app.db`'s `settings`.

### CLOUD-2

- **Where:** `sidecar/internal/services/settings/cloudplatform.go`, `sidecar/internal/tools/bash/cloud.go`
- **Records:** [every cloud platform is on until the user turns it off](../adr/2026-11-18-every-cloud-platform-is-on-until-the-user-turns-it-off.md), [record](../security/2026-11-18-cloud-platforms-are-on-by-default.md), [record](../security/2026-11-25-settings-in-app-db.md)
- `TestACloudPlatformSwitchReadThatFailsIsAnError` — a failed read is an error.
- `TestASwitchValueKubetailCannotUseIsOff` — a stored value Kubetail cannot use is off ([record](../security/2026-11-25-settings-in-app-db.md)).
- `TestARunThatCannotReadTheSwitchesHasNoPlatform` — a run's start reads a failed read as off.
- `TestASwitchThatCannotBeReadMidRunIsOff` — so does a grant.

### CLOUD-3

- **Where:** `sidecar/internal/tools/bash/cloud.go`
- **Records:** [the run's door](../adr/2026-11-17-a-sandboxed-command-reaches-a-cloud-platform-through-a-proxy-on-its-runs-one-relay.md)
- `TestABackgroundCommandsGrantHasNoAsker` — its grant has no asker, so every change is refused.
- `TestSwitchingACloudPlatformOffClosesItsRoute`, `TestASwitchedOffPlatformIsUnauthorized` — the switch stops its next request; the second is in each cloud platform's proxy.
- Note: it reads the account as it reads the cluster.
- Note: its grant reads the switch in `Enter`, after the token, and refuses `401`.

### CLOUD-4

- **Where:** `sidecar/internal/run/runproxy`
- **Records:** [the run's door](../adr/2026-11-17-a-sandboxed-command-reaches-a-cloud-platform-through-a-proxy-on-its-runs-one-relay.md)
- Note: each cloud platform's step lists the metadata its documentation says can hold a secret and asks for it or blanks it.

### CLOUD-5

- **Where:** `src/components/chat/chat-transcript/transcript.tsx`, `sidecar/internal/run/runproxy`
- **Records:** [the run's door](../adr/2026-11-17-a-sandboxed-command-reaches-a-cloud-platform-through-a-proxy-on-its-runs-one-relay.md)
- Note: a forwarded header is drawn only when it changes what the request does.

### CLOUD-6

- **Where:** `sidecar/internal/tools/bash/github.go`
- **Records:** [Record](../security/2026-11-17-a-sandboxed-command-reaches-github.md)
- `TestNoGitHubTokenIsInTheRun`, `TestGitHubsMountWritesItsFilesAndVariables` — the run's environment and files hold no GitHub token.

### CLOUD-7

- **Where:** `sidecar/internal/tools/bash/github.go`, `sidecar/internal/run/githubproxy`
- **Records:** [Record](../security/2026-11-17-a-sandboxed-command-reaches-github.md)
- `TestTheTokenIsAddedOutside` — the sidecar adds the token outside the sandbox.

### CLOUD-8

- **Where:** `sidecar/internal/run/githubproxy`
- `TestTheUpstreamIsFixed` — the upstream is fixed.

### CLOUD-9

- **Where:** `sidecar/internal/run/githubproxy`
- `TestOnlyTheListedHeadersReachGitHub` — only the listed headers reach GitHub.

### CLOUD-10

- **Where:** `sidecar/internal/run/githubproxy/classify.go`
- `TestACredentialPathIsRefused`, `TestACredentialPathIsRefusedUnasked` — a credential path is refused, unasked.
- `TestAPathByIDIsClassifiedAsItsRoute` — a path by id is classified as its route.

### CLOUD-11

- **Where:** `sidecar/internal/run/githubproxy/githubproxy.go`
- `TestARedirectToAnotherHostNeedsAnApprovedDataRead`, `TestADataPathAsks` — a signed link needs a data read the user allowed or approved.

### CLOUD-12

- **Where:** `sidecar/internal/run/githubproxy/githubproxy.go`
- `TestARedirectOnTheSameUpstreamNamesTheRoute`, `TestALinkHeaderNamesTheRoute`, `TestAnyLinkToTheUpstreamsOriginNamesTheRoute` — a link to GitHub's own origin names the route.

### CLOUD-13

- **Where:** `sidecar/internal/run/githubproxy`, `sidecar/internal/services/githubauth`
- `TestOnlyAnApprovedChangeReachesGitHub` — only an approved change reaches GitHub.
- `TestARuleAndAModeDecideAGitHubAction` — a mode or a rule allows one.

### CLOUD-14

- **Where:** `sidecar/internal/run/githubproxy`
- `TestAnAPIChangeShowsWhatIsSent` — an API change shows what is sent.

### CLOUD-15

- **Where:** `sidecar/internal/run/githubproxy`
- `TestAnyMutationIsAChange` — any mutation is a change.

### CLOUD-16

- **Where:** `sidecar/internal/run/githubproxy`
- `TestAPushAsksAndIsForwardedWhole`, `TestASandboxedPushAsksAndRuns` — a push asks.

### CLOUD-17

- **Where:** `sidecar/internal/services/githubauth`
- `TestRepeatedRefusalsRunGhOncePerInterval`, `TestAFailedReadIsHeldForTheInterval` — `gh` runs once per interval.

### CLOUD-18

- **Where:** `sidecar/internal/run/githubproxy/classify.go`
- **Records:** [GitHub over plain HTTP](../adr/2026-11-17-a-sandboxed-command-reaches-github-over-plain-http-at-ghs-own-host-name.md)
- Note: a user who wants a narrower reach logs `gh` in with a fine-grained token.

### CLOUD-19

- **Where:** `sidecar/internal/run/githubproxy/push.go`
- **Records:** [GitHub over plain HTTP](../adr/2026-11-17-a-sandboxed-command-reaches-github-over-plain-http-at-ghs-own-host-name.md)
- `TestAPushNamesItsRefs` — a push names its refs.
- Note: a commit id is a hash of its whole content.

### CLOUD-20

- **Where:** `sidecar/internal/run/githubproxy`
- **Records:** [GitHub over plain HTTP](../adr/2026-11-17-a-sandboxed-command-reaches-github-over-plain-http-at-ghs-own-host-name.md)
- Note: the link is short-lived and opens one download.

### CLOUD-21

- **Where:** `sidecar/internal/run/githubproxy/classify.go`, `sidecar/internal/run/githubproxy/graphql.go`
- **Records:** [GitHub over plain HTTP](../adr/2026-11-17-a-sandboxed-command-reaches-github-over-plain-http-at-ghs-own-host-name.md)
- `TestAGraphQLBodyThatDoesNotReadIsRefused` — the strict body checks keep the parsers from disagreeing on a change.

### CLOUD-22

- **Where:** `sidecar/internal/tools/bash/github.go`
- **Records:** [GitHub over plain HTTP](../adr/2026-11-17-a-sandboxed-command-reaches-github-over-plain-http-at-ghs-own-host-name.md)
- Note: the token opens GitHub's grant alone.

### CLOUD-23

- **Where:** `sidecar/internal/run/runproxy`
- **Records:** [GitHub over plain HTTP](../adr/2026-11-17-a-sandboxed-command-reaches-github-over-plain-http-at-ghs-own-host-name.md)
- Note: the grant bounds the rate, not the hour's total.

### CLOUD-24

- **Where:** `sidecar/internal/run/githubproxy/githubproxy.go`
- **Records:** [GitHub over plain HTTP](../adr/2026-11-17-a-sandboxed-command-reaches-github-over-plain-http-at-ghs-own-host-name.md)
- `TestARedirectOnTheSameUpstreamNamesTheRoute` — a change answered `307` or `308` is sent again only once the user approves it again.

### CLOUD-25

- **Where:** `sidecar/internal/services/githubauth`, `sidecar/internal/services/connectors/github/token.go`
- **Records:** [GitHub over plain HTTP](../adr/2026-11-17-a-sandboxed-command-reaches-github-over-plain-http-at-ghs-own-host-name.md)
- `TestTheTokenIsNeverLogged` — nothing `gh` prints is logged.
- Note: it runs as a kubeconfig's `exec` plugin runs.
- Note: it is killed past ten seconds.

### CLOUD-26

- **Where:** `sidecar/internal/services/gcpauth`, `sidecar/internal/tools/bash/gcp.go`
- `TestNoGCPCredentialEntersTheSandbox` — searches a real sandboxed run's environment, directory, tool home and workspace for the token and the sidecar's Google variables.

### CLOUD-27

- **Where:** `sidecar/internal/run/gcpproxy/classify.go`
- `TestEveryRequestIsClassified` — every request is classified.

### CLOUD-28

- **Where:** `sidecar/internal/run/gcpproxy/classify.go`
- `TestEveryRequestIsClassified` — every request is classified.
- `TestARuleAndAModeDecideAGCPAction` — a mode or a rule decides it.

### CLOUD-29

- **Where:** `sidecar/internal/run/gcpproxy/handler.go`, `sidecar/internal/run/gcpproxy/request.go`
- `TestOnlyTheListedHeadersAreForwarded` — only the listed headers are forwarded.

### CLOUD-30

- **Where:** `sidecar/internal/run/gcpproxy/handler.go`, `sidecar/internal/run/gcpproxy/request.go`
- `TestAMethodOverrideInTheQueryIsRefused` — a method override in the query is refused.

### CLOUD-31

- **Where:** `sidecar/internal/run/gcpproxy/handler.go`
- `TestAnApprovedChangeRunsAsTheApprovedAccount` — an approved change runs as the approved account.

### CLOUD-32

- **Where:** `sidecar/internal/run/gcpproxy`
- `TestAPathOffTheTableIsRefused` — a path off the table is refused.
- `TestTheUpstreamIsTheTablesHost` — the upstream is the table's host.

### CLOUD-33

- **Where:** `sidecar/internal/run/gcpproxy/redact.go`
- `TestInstanceMetadataIsBlanked`, `TestNodeMetadataIsBlanked`, `TestMasterAuthSecretsAreBlanked` — the values are blanked.

### CLOUD-34

- **Where:** `sidecar/internal/run/gcpproxy/redact.go`
- `TestAnAnswerThatCannotBeRewrittenFailsClosed` — an answer that cannot be rewritten fails closed.

### CLOUD-35

- **Where:** `sidecar/internal/run/gcpproxy`
- **Records:** [Google Cloud](../adr/2026-11-17-google-cloud-is-reached-with-the-active-gcloud-accounts-token.md)
- Note: the reads are as wide as the account.

### CLOUD-36

- **Where:** `sidecar/internal/run/gcpproxy/classify.go`, `sidecar/internal/run/gcpproxy/redact.go`
- **Records:** [Google Cloud](../adr/2026-11-17-google-cloud-is-reached-with-the-active-gcloud-accounts-token.md)
- Note: the lists grow as cases are met.

### CLOUD-37

- **Where:** `sidecar/internal/run/gcpproxy/handler.go`
- **Records:** [Google Cloud](../adr/2026-11-17-google-cloud-is-reached-with-the-active-gcloud-accounts-token.md)
- `TestAChangeCarryingABlankedValueIsRefused` — a change carrying a blanked value is refused.

### CLOUD-38

- **Where:** `sidecar/internal/services/gcpauth`
- **Records:** [Google Cloud](../adr/2026-11-17-google-cloud-is-reached-with-the-active-gcloud-accounts-token.md)
- Note: each request draws the account it uses.

### CLOUD-39

- **Where:** `sidecar/internal/tools/bash/toolhome.go`
- **Records:** [Google Cloud](../adr/2026-11-17-google-cloud-is-reached-with-the-active-gcloud-accounts-token.md)

### CLOUD-40

- **Where:** `sidecar/internal/run/azureproxy`, `sidecar/internal/services/azureauth`, `sidecar/internal/tools/bash/azure.go`
- **Records:** [record](../security/2026-11-17-azure-through-the-proxy.md)
- `TestNoAzureCredentialIsInTheRun` — no Entra token is in the run's environment or files.

### CLOUD-41

- **Where:** `sidecar/internal/run/azureproxy`
- **Records:** [record](../security/2026-11-17-azure-through-the-proxy.md)
- `TestACredentialActionIsRefusedUnasked` — a credential action is refused unasked.

### CLOUD-42

- **Where:** `sidecar/internal/run/azureproxy`
- **Records:** [record](../security/2026-11-17-azure-through-the-proxy.md)
- `TestAChangeAsksAndOnlyAnApprovalForwards` — only an approval forwards.
- `TestARuleAndAModeDecideAnAzureAction` — a mode or a rule allows one.

### CLOUD-43

- **Where:** `sidecar/internal/run/runproxy`, `sidecar/internal/tools/bash/proxy.go`
- `TestTheLoopbackAuthorityRoutesByTransport` — a route is keyed by transport and `Host`.
- `TestARunWithNoTLSRouteHasNoCertificate` — a run with no TLS route closes a handshake.

### CLOUD-44

- **Where:** `sidecar/internal/run/runproxy`, `sidecar/internal/tools/bash/proxy.go`
- `TestTheCertificateNamesTheLoopbackAlone` — the certificate names the loopback alone.

### CLOUD-45

- **Where:** `sidecar/internal/services/azureauth`
- **Records:** [Azure through the proxy](../adr/2026-11-17-a-sandboxed-command-reaches-azure-through-a-token-injecting-proxy.md), [record](../security/2026-11-17-azure-through-the-proxy.md)
- `TestOnlyTheAccountsTenantsReachAz` — only the account's tenants reach `az`.
- `TestAHeldTokenIsBlankedAndADroppedOneIsNot` — a held token is blanked in every log.
- `TestAFailedAzSaysNothingOfItsOutput` — `az`'s output is never kept.

### CLOUD-46

- **Where:** `sidecar/internal/tools/bash/azure.go`, `sidecar/internal/run/runproxy/cert.go`
- **Records:** [Azure through the proxy](../adr/2026-11-17-a-sandboxed-command-reaches-azure-through-a-token-injecting-proxy.md), [record](../security/2026-11-17-azure-through-the-proxy.md)
- Note: the certificate names `127.0.0.1` alone, so it vouches for no public host.

### CLOUD-47

- **Where:** `sidecar/internal/run/runproxy/cert.go`
- **Records:** [Azure through the proxy](../adr/2026-11-17-a-sandboxed-command-reaches-azure-through-a-token-injecting-proxy.md), [record](../security/2026-11-17-azure-through-the-proxy.md)
- Note: `az` fails the same way with its own bundle.

### CLOUD-48

- **Where:** `sidecar/internal/tools/bash/azure.go`
- **Records:** [Azure through the proxy](../adr/2026-11-17-a-sandboxed-command-reaches-azure-through-a-token-injecting-proxy.md), [record](../security/2026-11-17-azure-through-the-proxy.md)
- Note: the grant's token opens the run's door alone.

### CLOUD-49

- **Where:** `sidecar/internal/run/azureproxy/classify.go`
- **Records:** [Azure through the proxy](../adr/2026-11-17-a-sandboxed-command-reaches-azure-through-a-token-injecting-proxy.md), [record](../security/2026-11-17-azure-through-the-proxy.md)
- `TestEveryRequestIsClassified` — pins each row.

### CLOUD-50

- **Where:** `sidecar/internal/run/azureproxy/classify.go`
- **Records:** [Azure through the proxy](../adr/2026-11-17-a-sandboxed-command-reaches-azure-through-a-token-injecting-proxy.md), [record](../security/2026-11-17-azure-through-the-proxy.md)
- Note: a VM's run-command is destructive.
- Note: AKS run-command is refused.

### CLOUD-51

- **Where:** `sidecar/internal/run/azureproxy`
- **Records:** [Azure through the proxy](../adr/2026-11-17-a-sandboxed-command-reaches-azure-through-a-token-injecting-proxy.md), [record](../security/2026-11-17-azure-through-the-proxy.md)
- Note: each request is drawn whole.

### CLOUD-52

- **Where:** `sidecar/internal/run/awsproxy`, `sidecar/internal/tools/bash/aws.go`
- `TestNoRealCredentialIsInTheRun` — no real credential is in the run, through the real sandbox.
- `TestOnlyTheRunsKeyIdOpens` — only the run's key id opens.
- `TestASandboxedCommandReachesAWS` — a sandboxed command reaches AWS.

### CLOUD-53

- **Where:** `sidecar/internal/run/awsproxy`
- `TestAReadIsSignedAnew` — a read is signed anew.
- `TestTheHostComesFromTheTable` — the host comes from the table.

### CLOUD-54

- **Where:** `sidecar/internal/run/awsproxy/services.go`, `sidecar/internal/run/awsproxy/classify.go`
- `TestEveryRowsOperationsAreClassed` — every row's operations are classed.
- `TestARefusedOperationReachesNoUpstream`, `TestAnOperationNamedElsewhereReachesNoUpstream` — a refused operation reaches no upstream.

### CLOUD-55

- **Where:** `sidecar/internal/run/awsproxy`
- `TestAReadOfAnotherAccountsBucketAsks`, `TestAFailedBucketListAsksForEveryBucket`, `TestAnOwnedBucketExpiresWithItsList` — a read of a bucket not known to be the account's asks.
- `TestEveryRowsOperationsAreClassed` — every row's operations are classed.

### CLOUD-56

- **Where:** `sidecar/internal/run/awsproxy`
- `TestAStreamedUploadIsRefused`, `TestAnUploadIsShownOrRefused` — an upload is shown or refused.

### CLOUD-57

- **Where:** `sidecar/internal/services/kubeconfig/awsprofile.go`, `sidecar/internal/services/awsauth`, `sidecar/internal/tools/bash/aws.go`
- **Records:** [a run's AWS identity follows its chat's cluster](../adr/2026-11-18-a-runs-aws-identity-follows-its-chats-cluster.md), [record](../security/2026-11-18-a-runs-aws-identity-follows-its-cluster.md)
- `TestAWSProfileReadsTheExecProvider` — the profile is read from the `exec` block.
- `TestTheAWSMountFollowsTheClustersProfile`, `TestALoginReachesAWSAsItsProfile` — the run reaches AWS as its cluster's profile.

### CLOUD-58

- **Where:** `sidecar/internal/tools/bash/aws.go`
- **Records:** [a run's AWS identity follows its chat's cluster](../adr/2026-11-18-a-runs-aws-identity-follows-its-chats-cluster.md), [record](../security/2026-11-18-a-runs-aws-identity-follows-its-cluster.md)
- `TestAWSProfile` — the names `sandbox.AWSProfile` passes.

### CLOUD-59

- **Where:** `sidecar/internal/services/awsauth`, `sidecar/internal/tools/bash/aws.go`
- **Records:** [a run's AWS identity follows its chat's cluster](../adr/2026-11-18-a-runs-aws-identity-follows-its-chats-cluster.md), [record](../security/2026-11-18-a-runs-aws-identity-follows-its-cluster.md)
- `TestEachProfileHasItsOwnLoginAndBuckets` — each profile has its own login and buckets.

### CLOUD-60

- **Where:** `sidecar/internal/services/kubeconfig/awsprofile.go`
- **Records:** [a run's AWS identity follows its chat's cluster](../adr/2026-11-18-a-runs-aws-identity-follows-its-chats-cluster.md), [record](../security/2026-11-18-a-runs-aws-identity-follows-its-cluster.md)

### CLOUD-61

- **Where:** `sidecar/internal/services/awsauth`
- **Records:** [AWS through a re-signing proxy](../adr/2026-11-17-a-sandboxed-command-reaches-aws-through-a-re-signing-proxy.md)
- `TestAHelpersStderrNeverReachesTheSidecars` — a helper's stderr never reaches the sidecar's.
- `TestARoleChainedFromAProcessIsNoLogin` — a role chained from a process is no login.

### CLOUD-62

- **Where:** `sidecar/internal/run/awsproxy`
- **Records:** [AWS through a re-signing proxy](../adr/2026-11-17-a-sandboxed-command-reaches-aws-through-a-re-signing-proxy.md)

### CLOUD-63

- **Where:** `sidecar/internal/run/awsproxy`
- **Records:** [AWS through a re-signing proxy](../adr/2026-11-17-a-sandboxed-command-reaches-aws-through-a-re-signing-proxy.md)

### CLOUD-64

- **Where:** `sidecar/internal/run/runproxy/ask.go`, `sidecar/internal/run/awsproxy`, `sidecar/internal/run/azureproxy`, `sidecar/internal/run/gcpproxy`, `sidecar/internal/run/githubproxy`
- **Records:** [Target-neutral permissions](../security/2026-11-25-target-neutral-permissions.md)
- `TestARuleAndAModeDecideAnAWSAction`, `TestARuleAndAModeDecideAnAzureAction`, `TestARuleAndAModeDecideAGCPAction`, `TestARuleAndAModeDecideAGitHubAction` — each proxy's request is decided by its account's mode and the rules.
- `TestNoProxyDropsAFieldBeforeTheDecision`, `TestDecideFollowsThePolicy`, `TestDecideKeepsEveryField` — the decision sees the whole action.
- `TestOrdinaryIsExactlyTheTable`, `TestAChangeOffTheOrdinaryTableAsksOnce`, `TestAnS3PutWithACopySourceAsksOnce` — only an `ordinary` change may be allowed.
- `TestAReadIsClassifiedAsToday`, `TestEachCredentialRefusalIsReach` — reads and credential refusals keep their classes.
- `TestACommandAnswerAllowsTheNextRequest`, `TestAGrantableCloudActionTakesEveryDuration` — an answer can write a rule.

### CLOUD-65

- **Where:** `sidecar/internal/run/azureproxy/classify.go`
- **Records:** [Target-neutral permissions](../security/2026-11-25-target-neutral-permissions.md)
- `TestOnlyARoleAssignmentInAGroupIsConfined` — only a role assignment in a resource group is confined.

### CLOUD-66

- **Where:** `sidecar/internal/run/runproxy/ask.go`, `sidecar/internal/run/awsproxy`, `sidecar/internal/run/azureproxy`, `sidecar/internal/run/gcpproxy`, `sidecar/internal/run/githubproxy`
- **Records:** [Target-neutral permissions](../security/2026-11-25-target-neutral-permissions.md)
- `TestAnAliasedOrForeignScopeIsEmpty`, `TestAScopeIsLowerCase`, `TestAGlobalAWSServiceHasOneScope` — the account a request names.
- `TestARuleAndAModeDecideAGCPAction` — a write billed to another project asks under `auto`.
- `TestAForeignRequestKeepsItsScopeAndAsksOnce`, `TestARuleAndAModeDecideAnAWSAction`, `TestARuleAndAModeDecideAnAzureAction` — a request reaching past its account keeps the account's mode and rules.
- `TestAProjectBilledTwiceIsRefused` — one project billed at most.

### CLOUD-67

- **Where:** `sidecar/internal/run/runproxy/ask.go`
- **Records:** [Target-neutral permissions](../security/2026-11-25-target-neutral-permissions.md)
- `TestAnAllowedDataReadGivesUpTheLock` — an allowed data read runs.

### CLOUD-68

- **Where:** `sidecar/internal/run/awsproxy/classify.go`, `sidecar/internal/run/azureproxy/classify.go`
- **Records:** [Target-neutral permissions](../security/2026-11-25-target-neutral-permissions.md)
- `TestAnS3PolicyOrACLChangeIsAccessControl` — S3's access subresources and headers, a delete included; a read of one stays a read.
- `TestEveryRequestIsClassified`, `TestARuleAndAModeDecideAnAzureAction` — an Azure role assignment's delete at a subscription is access control, which an access control deny refuses.

### CLOUD-69

- **Where:** `sidecar/internal/run/awsproxy/awsproxy.go`, `sidecar/internal/run/awsproxy/buckets.go`, `sidecar/internal/run/awsproxy/forward.go`
- **Records:** [Target-neutral permissions](../security/2026-11-25-target-neutral-permissions.md)
- `TestABucketRequestIsSentExpectingItsOwner` — the expected owner header, and a caller's other owner asks.
- `TestALoginWithAMalformedAccountIsItsKey` — a login is its account, else its access key.
- Note: STS's `GetCallerIdentity` would give a static-key login its account; see [TODO](../TODO.md#security).
- `TestAListAnswersOnlyForItsLogin` — the list of owned buckets is the login's.
- `TestALoginThatChangedAccountSendsNothing`, `TestALoginWithNoAccountWhoseKeyChangedSendsNothing` — a change is sent as the login decided on.

### CLOUD-70

- **Where:** `sidecar/internal/run/githubproxy/classify.go`, `sidecar/internal/run/githubproxy/graphql.go`, `sidecar/internal/run/gcpproxy/classify.go`
- **Records:** [Target-neutral permissions](../security/2026-11-25-target-neutral-permissions.md)
- `TestABodyThatChangesAccessIsAccessControl`, `TestARuleAndAModeDecideAGitHubAction` — a repository's visibility and a pull request's maintainer edits, by name or id; under `auto` they ask.
- `TestAnAccessControlMutationIsAccessControl` — GraphQL's access control mutations.
- `TestAStorageACLChangeIsAccessControl` — Cloud Storage's ACLs, by their path position, a predefined ACL or decoded metadata, a multipart upload's included.
- `TestMaintainerEditsAreReadFromTheParsedInput` — GraphQL's maintainer edits, from the parsed input and its variables.

### CLOUD-71

- **Where:** `sidecar/internal/run/githubproxy/classify.go`, `sidecar/internal/run/githubproxy/graphql.go`
- **Records:** [Permissions are access levels, a boundary, modes and rules](../adr/2026-11-25-permissions-are-access-levels-a-boundary-modes-and-rules-over-one-action.md)
- `TestABodyThatChangesAccessIsAccessControl`, `TestMaintainerEditsAreReadFromTheParsedInput` — an update of the setting is access control.

### CLOUD-72

- **Where:** `sidecar/internal/run/azureproxy/classify.go`, `sidecar/internal/run/githubproxy/classify.go`, `sidecar/internal/run/permissions`
- **Records:** [Target-neutral permissions](../security/2026-11-25-target-neutral-permissions.md)
- `TestASubscriptionWideDataReadSpansItsGroups`, `TestARuleAndAModeDecideAnAzureAction` — an Azure data read in no group, refused by a group's deny.
- `TestAnOwnersAggregateDataReadSpansItsRepositories` — a GitHub owner's alerts and archives, refused by a repository's deny.
