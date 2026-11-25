# Security record — a sandboxed command reaches AWS, 17 November 2026

**Subject:** step 2A of the cloud proxies. With `sandbox.aws` on, a sandboxed command calls the AWS
API as the user through `run/awsproxy`, which re-signs each request with the user's credentials.
[ADR](../adr/2026-11-17-a-sandboxed-command-reaches-aws-through-a-re-signing-proxy.md). Built in the
commit that adds this record, on `wip/cloud-proxies`.

## What it widens

A sandboxed command reads, on the model's word and unasked, the metadata and state the user's AWS
identity can read in the thirteen listed services. The answer reaches the model's provider, and
with network on a command can send it anywhere. Every change asks, once, as step 1 decides.

## What holds

- **A `credential_process`'s diagnostics never reach the host's log.** It runs with no stdin and
  no stderr (`TestAHelpersStderrNeverReachesTheSidecars`, `TestARoleChainedFromAProcessIsNoLogin`).
- **Nothing hung holds the credential service.** The SDK's timeout kills a `credential_process`'s
  whole process group, a descendant outside it is waited for five seconds at most, and every
  retrieval is bounded at two minutes (`TestACancelledHelperIsKilledWithItsGroup`,
  `TestAHelpersChildOutsideItsGroupIsWaitedForNoLonger`, `TestAHungRefreshGivesUpItsTurn`).
- **No AWS credential is in the sandbox.** The key id is the grant's token and the secret signs
  nothing (`TestNoRealCredentialIsInTheRun`, through the real sandbox).
- **The sandbox's signature decides nothing.** It is dropped, and the proxy signs the request it
  built (`TestAReadIsSignedAnew`, `TestAPresignedKeyIdOpens`).
- **The upstream is a host of the table.** Service and region come from the scope, the region by
  pattern (`TestTheHostComesFromTheTable`).
- **No answer is a credential.** STS but for `GetCallerIdentity`, key pairs, access keys, ECR
  logins, presigned layer URLs and S3 Express sessions are refused unasked
  (`TestEveryRowsOperationsAreClassed`).
- **A read whose answer can hold a secret asks as a secret read**, though its name reads like
  any other. A log read that unmasks asks, and so does every Logs Insights result, whose query
  may have unmasked, and a VPN connection's sample configuration, which holds its tunnels'
  pre-shared keys. Under `NoSecretData` each is refused (`TestEveryRowsOperationsAreClassed`,
  `TestASecretReadAsksAndIsRefusedUnderNoSecretData`).
- **The operation classified is the one AWS runs.** It is read only where its row's protocol
  names it, and a request naming it elsewhere or twice is refused
  (`TestAnOperationNamedElsewhereReachesNoUpstream`).
- **No header changes a request unseen.** Each header that changes what a request does is drawn,
  any other `x-amz` header refuses it, and so does a forwarded header sent twice
  (`TestAnUnlistedAmzHeaderIsRefused`, `TestARefusedShapeReachesNoUpstream`).
- **No upload is approved unshown** (`TestAStreamedUploadIsRefused`, `TestAnUploadIsShownOrRefused`).
- **A read of another account's bucket, registry or template URL asks**
  (`TestAReadOfAnotherAccountsBucketAsks`, `TestEveryRowsOperationsAreClassed`). A bucket is the
  login's only while the list that holds it is fresh, and a dead login forgets the list
  (`TestAnOwnedBucketExpiresWithItsList`, `TestADroppedLoginForgetsItsBuckets`).
- **A run holds an `AWS_` name only with a mount's value** (`TestOnlyAPlaceholderPassesTheCheck`);
  the sandboxed login shell holds none (`TestTheSandboxedShellDropsAWSCredentials`).
- **The real key id and session token do not come back in an error** (`TestAnErrorAnswerIsChecked`);
  a dead credential is dropped even when its error is withheld
  (`TestADeadCredentialEchoingItsKeyIsDropped`), and when the proxy's own bucket list meets it
  (`TestABucketListWithADeadCredentialDropsIt`).

## Residuals

Each is a **By decision** row in the model.

- Resolving the user's profile can run its `credential_process` outside the sandbox.
- A role assumed from a source profile's `credential_process` is no login: the SDK runs that
  process with the sidecar's stderr, which Kubetail cannot replace.
- Metadata no list names can hold a secret: a tag, a description, a CloudFront origin's custom
  header, a log event.
- No file is uploaded to S3 from the sandbox.
- An unasked read can carry data in a parameter no row names, to wherever AWS records it.
- The proxy lists the login's buckets itself; a login that cannot list them is asked for every
  bucket read.
- The table and the class lists are hand-written: a credential-answering read AWS adds under
  `Describe`, `Get` or `List` is read unasked until a row names it.
- The chat's AWS identity is the sidecar's default chain, which need not be the identity its
  kubeconfig uses for the cluster.

## Measured

boto3 1.44.4 against a recording listener at a loopback `AWS_ENDPOINT_URL`: a 70-character key id
is accepted; a global service is signed for the client's region, so the proxy signs `us-east-1`;
CloudWatch speaks JSON; a small `PutObject` is sent whole; S3 presigns with SigV2, which the proxy
refuses. AWS CLI v2 and macOS were not measured.
