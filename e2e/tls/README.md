# Test-only TLS material

Static TLS material the e2e cluster mounts via secrets. Pinned to the
`kubetail-e2e` namespace; if the e2e namespace ever changes, the leaf SANs
must be regenerated to match (see "Regenerate" below). Long validity
(100 years) so the certs don't need rotation in CI.

| File | Purpose |
| --- | --- |
| `ca.{crt,key}` | Self-signed CA. Trust anchor for the cluster-api / cluster-agent leaf certs. Mounted into both pods as `kubetail-ca` and used by the cluster-agent's trust-chain interceptor. |
| `cluster-api.{crt,key}` | Serving cert for cluster-api at `kubetail-cluster-api.kubetail-e2e.svc`. Also presented as a client cert when cluster-api dials cluster-agent — the only identity the agent allows past its interceptor. |
| `cluster-agent.{crt,key}` | Serving cert for cluster-agent at `kubetail-cluster-agent.kubetail-e2e.svc`. Re-used in `test_zero_trust.py` to exercise the "valid CA but disallowed CN" branch. |
| `untrusted-client.{crt,key}` | Self-signed cert that chains to neither of the cluster-api's CA pools. Drives the `aggregationAuthMiddleware` "no valid certificate found" branch in `test_zero_trust.py::test_untrusted_client_cert_with_spoofed_headers_rejected`. The middleware's `Verify()` fails on chain mismatch (not expiry), so a permanent cert keeps the test robust against clock drift. |
| `*.cnf` | OpenSSL configs used to generate the certs. Kept in-tree for reproducibility. |

The serving leaves (`cluster-api`, `cluster-agent`) are checked into the
repo so e2e is hermetic — no openssl invocation at setup time. They have
no privileges outside the throwaway kind cluster.

**Do not** add any cert here to a real trust store.

## Regenerate

```sh
cd e2e/tls
openssl genpkey -algorithm RSA -out ca.key -pkeyopt rsa_keygen_bits:4096
openssl req -x509 -new -nodes -key ca.key -config ca.cnf -days 36500 -out ca.crt

for name in cluster-api cluster-agent; do
  openssl genpkey -algorithm RSA -out "${name}.key" -pkeyopt rsa_keygen_bits:4096
  openssl req -new -key "${name}.key" -out "${name}.csr" -config "${name}.cnf"
  openssl x509 -req -in "${name}.csr" \
    -CA ca.crt -CAkey ca.key -CAcreateserial \
    -out "${name}.crt" -days 36500 \
    -extensions v3_req -extfile "${name}.cnf"
done
rm -f ca.srl *.csr
```
