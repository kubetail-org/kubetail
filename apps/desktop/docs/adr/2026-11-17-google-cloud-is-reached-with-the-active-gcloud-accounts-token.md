---
title: Google Cloud is reached with the active gcloud account's token
date: 2026-11-17
scope: sidecar
status: Accepted
---

# Google Cloud is reached with the active gcloud account's token

## Context

[Step 1](2026-11-17-a-sandboxed-command-reaches-a-cloud-platform-through-a-proxy-on-its-runs-one-relay.md)
built the run's door, the grant and the gate, and decided that a cloud platform's credential is lent
through a proxy in the sidecar, never handed to the sandbox, as
[#25](https://github.com/kstackhq/kstack/pull/25) decided for the cluster. Issue #933 asks for
`gcloud` in the sandbox, as the user, on Compute Engine, GKE, IAM, Cloud Logging and Cloud
Storage.

## Decision

- **The token is the active `gcloud` account's.** `services/gcpauth` runs
  `gcloud config config-helper --format=json --min-expiry=10m` off the sidecar's `PATH`, as
  `gke-gcloud-auth-plugin` does, so the account, configuration and refresh are a GKE
  connection's. It keeps the token in memory until a minute before it expires, and a failure for
  30 s. It runs no earlier than a run's mount with the switch on.
- **`gcloud` alone is pointed at the door.** `CLOUDSDK_API_ENDPOINT_OVERRIDES_<SERVICE>` names a
  route per service, `CLOUDSDK_PROXY_*` the run's port, and `CLOUDSDK_AUTH_ACCESS_TOKEN` the
  grant's token. No `HTTP_PROXY` is set.
- **Six services, by path.** `compute` (`/compute/`), `container` (`/v1/`, `/v1beta1/`), `iam`
  (`/v1/`, `/v2/`, `/v3/`), `logging` (`/v2/`), `storage` (the JSON API's `/storage/v1/`,
  `/download/storage/v1/`, `/upload/storage/v1/`) and `cloudresourcemanager` (`/v1/`, `/v2/`,
  `/v3/`). A path off the list, a batch path, an empty, `.` or `..` segment, and an encoded `/`
  or `.` outside a storage object's name are refused.
- **Refused, as the answer is a credential:** a service account key's create, `signBlob`,
  `signJwt`, `generateAccessToken`, `generateIdToken`, an HMAC key's create, an OAuth client's
  credentials and a workforce provider's SCIM tokens, and a resumable upload, whose session
  uploads with no token.
- **Class 6:** an object's contents (`alt=media`, `/download/`), an instance's serial port, guest
  attributes and screenshot.
- **Class 5:** a `DELETE` of a project, a GKE cluster, a bucket or a service account, any
  `setIamPolicy`, and a bucket's IAM `PUT`.
- **Blanked in every answer:** instance metadata values in each of Compute's shapes (a
  `Metadata`'s items, instance settings' items map, a preserved state's or an instance group's
  map, partner metadata), a VPN tunnel's `sharedSecret`, a
  cluster's `masterAuth` client key and password, and node metadata values. A change carrying a
  blanked value is refused.
- **A body is shown whole or refused.** An upload of a text object under 1 MiB asks; a binary or
  larger upload is refused.
- **Every refusal is Google's `403`**, since `gcloud` retries a `429` and a `5xx`.

The audit of the six discovery documents found two secrets beyond the spec's list: a SCIM token
(`iam`, set on create, now refused) and a VPN tunnel's `sharedSecret`, which `compute`'s document
does not mark input-only (now blanked). It confirmed as input-only a router's
`md5AuthenticationKeys`, an SSL certificate's `privateKey`, a customer-supplied encryption key and
a workforce provider's client secret.

## Alternatives considered

- **Application Default Credentials.** It needs `golang.org/x/oauth2/google` and a key file the
  sidecar does not open, and may be another identity than the one `gcloud` shows.
- **A live token in the sandbox** (`CLOUDSDK_AUTH_ACCESS_TOKEN_FILE`, a metadata server). The
  sandbox would hold a credential.
- **Asking for each instance read.** Every listing carries metadata, so `instances list` would ask.

## Consequences

- `gcloud` must be installed for the sidecar; a missing login is a `403` naming `gcloud auth login`.
- `gsutil`, `bq`, client libraries and every other Google service reach nothing.
- `add-metadata` and `remove-metadata` do not work in the sandbox.
- An account or project switched in a terminal reaches the sandbox when the held token expires.
- The lists grow as cases are met.
