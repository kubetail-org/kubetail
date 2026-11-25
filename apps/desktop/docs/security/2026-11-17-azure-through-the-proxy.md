# Security record — Azure through the proxy, 17 November 2026

**Subject:** step 2D of the cloud proxies. With `sandbox.azure` on, `az` in a sandboxed run reads
Azure Resource Manager as the user, through the run's door, and asks before each change. The
sidecar holds the user's Resource Manager tokens; the run holds the grant's token alone. The door
answers TLS. The living model is [security-model.md](../security-model.md); the decision is
[the ADR](../adr/2026-11-17-a-sandboxed-command-reaches-azure-through-a-token-injecting-proxy.md).

## What changes

**Widened.**

- With the switch on, a sandboxed command reads everything the user's login can `GET` from
  Resource Manager, in every subscription the login holds, unasked. Resource Manager keeps secrets
  out of a `GET`; the secret-metadata list covers the documented exceptions, which ask as class 6
  (`TestSecretMetadataAsksAsADataRead`).
- The sidecar runs the user's `az` from `PATH`, outside the sandbox, with fixed arguments, and
  holds Resource Manager tokens in memory until five minutes before they expire, registered with
  `safe` while held (`TestAHeldTokenIsBlankedAndADroppedOneIsNot`).
- The run's door answers TLS. The handshake runs before any token is checked, on a port that on
  macOS any local process can dial (`TestAFailedHandshakeClosesTheConnection`).
- A run with Azure trusts a certificate Kubetail made, through `REQUESTS_CA_BUNDLE`, which every
  Python tool reads.

**Narrowed.** `neverEnv` refuses every `AZURE_` and `ARM_` variable but `AZURE_CONFIG_DIR`, and
kubelogin's `AAD_SERVICE_PRINCIPAL_CLIENT_SECRET` and the legacy `MSI_SECRET`, so no run is handed
a real Azure credential by name, a new one included (`TestNeverEnvHoldsTheRules`,
`TestARunsCheckRefusesAnUnpassableVariable`).

## What holds

- No Entra token is in the run's environment, its files or the door's answers
  (`TestNoAzureCredentialIsInTheRun`).
- The run's token is the grant's: it dies with the run and opens Azure's route alone.
- The certificate's key never leaves the sidecar's memory, and the certificate names `127.0.0.1`
  alone (`TestTheCertificateNamesTheLoopbackAlone`), so trusting it lets nothing pose as a public
  host.
- The upstream is one of three fixed hosts. Nothing `az` or the command says picks it.
- An action that answers a credential is refused, never asked (`TestACredentialActionIsRefusedUnasked`).
- A namespace, type or action outside ASCII is refused, so a letter Resource Manager folds to
  ASCII, as `ſ` to `s`, cannot carry an action past the classifier (`TestAWordOutsideASCIIIsRefused`).
- A change reaches Azure only once approved (`TestAChangeAsksAndOnlyAnApprovalForwards`), and is
  sent once. A change on a path naming no subscription, a management group's, is refused
  (`TestAChangeOutsideASubscriptionIsRefused`).
- An `az` before 2.73, which asks IMDS for a managed identity's token, counts as no login, so on
  an Azure VM with network on it cannot take the VM's identity past the proxy
  (`TestAnAzBeforeManagedIdentityOverMSALIsNoLogin`).
- `az`'s arguments are fixed but for a tenant id the service read itself, and only the account's
  tenants reach it (`TestOnlyTheAccountsTenantsReachAz`).
- A login change narrows access at once: each discovery drops every held token, so no request
  rides the identity the user signed out of (`TestARediscoveryDropsEveryToken`,
  `TestATokenFromBeforeARediscoveryIsNotKept`).

## Residuals

Each is a **By decision** row in the model.

- The certificate is trusted by whatever reads `REQUESTS_CA_BUNDLE`, and with network on those
  tools verify against the system's bundle in place of their own.
- On macOS that system bundle is `/etc/ssl/cert.pem`, which holds no root added to the Keychain
  alone. With network on, a tool behind a TLS-inspecting proxy whose root lives only there fails
  to verify public hosts, as `az` does with its own bundle.
- Every Azure SDK in the run believes it has a managed identity. With network on, one that calls
  the real Resource Manager sends it the grant's token, which opens nothing there.
- A `POST` that only reads (`Microsoft.ResourceGraph`, cost queries) asks as a change.
- A `POST` that answers a secret under a name the credential and list/get rules miss is class 4:
  it asks as a change and is not refused under `NoSecretData`.
- A change that runs code on a machine (a VM extension, a deployment script) is class 4 and rests
  on the user's eye.
- A secret in metadata the list does not name is read unasked, as step 1 records.
- `az` is found on `PATH`, so whoever controls `PATH` controls what the sidecar runs, as with a
  kubeconfig's `exec` plugin.
- A long `az … create` asks once per request.
