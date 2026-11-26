# Security record — a macOS run with the internet has IPv4 only, 4 October 2026

**Subject:** on macOS a sandboxed command with network reached the host's loopback and its own
addresses through IPv4-mapped IPv6 addresses. The fix denies IPv6 to such a run. This corrects
[network is the user's switch](2026-10-04-network-is-the-users-switch.md), whose macOS section said
the `localhost` deny under `ip6` shut the IPv4-mapped forms of loopback. The living model is
[security-model.md](../security-model.md).

## What was found

The profile allowed outbound to `ip4 "*:*"` and `ip6 "*:*"`, then denied `localhost` under both.
On GitHub's macOS 15 and macOS 26 runners, arm64 and Intel, a run with network connected to a
server on `127.0.0.1` through `[::ffff:127.0.0.1]` and read its response
(`TestTheInternetReachesNoHostLoopback`). On macOS 27.0.1 that form was refused, but two others
were not.

Measured on macOS 27.0.1 (arm64) with `sandbox-exec` and `curl -v` against servers on `127.0.0.1`,
`[::1]` and the host's LAN address. *Refused* is EPERM from the connect; *left* is a connect the
sandbox let out, which failed or timed out on the network.

| Target | Previous rules | Current rules |
|---|---|---|
| `127.0.0.1`, `0.0.0.0` | refused | refused |
| `[::ffff:127.0.0.1]` | refused (reached on macOS 15 and 26) | refused |
| **`[::ffff:0.0.0.0]`** | **reached `127.0.0.1`** | refused |
| `[::1]`, `[::]`, `[fe80::1%lo0]`, `[::127.0.0.1]` | refused | refused |
| the host's LAN address | refused | refused |
| **`[::ffff:<LAN address>]`** | **reached the host** | refused |
| the host's link-local IPv6 | refused | refused |
| the relay's port on `127.0.0.1` | reached | reached |
| `192.0.2.1` (TEST-NET-1) | left | left |
| `[2001:db8::1]` (documentation prefix) | left | refused |

The servers on loopback answer whatever reaches them: a user's Ollama, a dev server, a database
bound to `127.0.0.1`.

## Why IPv6 is denied whole

Seatbelt's network filters name `localhost` or `*` and no other host; a rule naming
`::ffff:127.0.0.1` does not compile (*host must be \* or localhost*). Its `localhost` under `ip6`
does not match every IPv4-mapped form of a host address, and no other filter tried shut them:

- `localhost` under `ip`, or under `tcp6` and `tcp`: no change.
- `(deny system-socket (socket-domain AF_INET6))`: no change.
- a `local` filter on `localhost`: refuses every connect, the internet included.
- the deny before the allow: the allow wins, and everything is reached.

Denying `ip6 "*:*"` shuts every form, and the relay's port and the resolver's socket stay open.
The rules are now:

```
(allow network-outbound (remote ip4 "*:*"))
(deny network-outbound (remote ip4 "localhost:*"))
(deny network-outbound (remote ip6 "*:*"))
```

**Their order is load-bearing, and Seatbelt documents none of it.** On macOS 27.0.1 the
`localhost` deny under `ip4` shuts nothing when it is the last network rule: with the IPv6 deny
absent, or before it, `127.0.0.1`, `0.0.0.0` and the LAN address are all reached. The same deny
holds with an IPv6 rule after it. `TestTheInternetReachesNoHostLoopback` and
`TestTheInternetReachesNoHostAddress` pin the result, not the mechanism.

## What the user gets

A sandboxed command with network on macOS reaches the internet over IPv4 alone. A destination
reachable only over IPv6 fails with EPERM (`TestTheInternetIsIPv4Only`). Without network nothing
changes, and Linux is unaffected: its run has a network namespace of its own.

## Tests

- `TestTheInternetReachesNoHostLoopback` adds `[::ffff:0.0.0.0]`; it still checks the relay's port.
- `TestTheInternetReachesNoHostAddress` adds `[::ffff:<host address>]`.
- `TestTheInternetIsIPv4Only` (new): `[2001:db8::1]` and `[::1]` are refused with EPERM.
- `TestTheInternetResolves` still reaches mDNSResponder's socket with network.
- The `internet` golden profile pins the three lines.

Every one passes on macOS 27.0.1, and against the previous rules the three loopback and address
tests fail there.

## Residuals

- **Confirmed on one version.** The rules were measured on macOS 27.0.1 alone; macOS 15 and 26
  are checked by CI. Seatbelt's matching differs between versions, as `[::ffff:127.0.0.1]` shows.
