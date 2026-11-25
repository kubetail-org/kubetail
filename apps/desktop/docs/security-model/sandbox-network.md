# The sandbox: network and the run's door

[← Security model](../security-model.md)

A sandboxed command reaches no network but the chat's cluster through its forwarder, unless the user turned network on. Network is the user's switch: the chat's switch behind a confirm, a toggle for one message, or a request that shows the one command. With network, a command reaches the internet and the local network but not the host's loopback, and it can send what it read to any server; it holds no credential. With or without network, a command cannot reach the sidecar's API. The forwarder's one relay leads to the run's door, which serves the cluster proxy and each cloud platform the user turned on, each behind its own token.

## Protections

| ID | Protection | Status | Residual |
| --- | --- | --- | --- |
| NET-1 | On Linux a sandboxed command reaches no network but the chat's cluster through its forwarder, unless the user turned network on. Without it the command has a network namespace of its own and a seccomp filter admitting only IP sockets. | **Enforced** on Linux | — |
| NET-2 | On macOS a sandboxed command reaches no network but the chat's cluster through its forwarder, unless the user turned network on. Without it the command reaches the profile's one TCP endpoint and socket. | **Enforced** on macOS | — |
| NET-3 | The user turns network on by the chat's switch behind a confirm, a toggle for one message, or a request that shows the one command. The switch is the chat's row, on only while the chat is in the sandbox, and a send that saw it the other way is refused. | **Enforced** | — |
| NET-4 | The toggle is the turn's. | **Enforced** | — |
| NET-5 | A call that asks for network alone waits on the user. | **Enforced** | — |
| NET-6 | A monitor never has network. | **Enforced** | — |
| NET-7 | The run takes the network its gate decided, and its row says why. | **Enforced** | — |
| NET-8 | Where network is unavailable nothing turns it on. | **Enforced** | — |
| NET-9 | A sandboxed command with network reaches the internet but not the host's loopback. | **Enforced** | — |
| NET-10 | On macOS a sandboxed command with network reaches none of the host's addresses, under any IPv4-mapped form, and reaches the internet over IPv4 alone. | **Enforced** on macOS | — |
| NET-11 | On Linux a sandboxed command with network runs as the user holding no capability under `pasta`. | **Enforced** on Linux | — |
| NET-12 | No run mounts or unmounts anything. | **Enforced** on Linux | — |
| NET-13 | A sandboxed run on macOS looks up one Mach service, and the resolver and the trust daemon only with the internet. | **Enforced** | — |
| NET-14 | A sandboxed command with network can send what it read to any server: cluster data short of Secret values the user has not allowed showing, and its workspace. | **By decision** | — |
| NET-15 | With network, a sandboxed command reaches the local network: on macOS any other machine's IPv4 address; on Linux any host address but loopback and the one `pasta` copies, a Docker bridge's among them. | **By decision** | — |
| NET-16 | On macOS, with network, a command can have the trust daemon send a GET, to a host and path it chooses, the loopback and the local network included. | **By decision** | — |
| NET-17 | A sandboxed command cannot reach the sidecar's API, its GraphQL and gRPC surface, `approvalDecide` included, by the host's socket, a loopback port or any other socket. | **Enforced** | A standalone sidecar keeps this only while its API stays out of every path and port a run can reach, and while nothing a run talks to outside the sandbox passes requests through to it. |
| NET-18 | The forwarder's one relay leads to the run's door, which serves the cluster proxy and each cloud platform the user turned on, each behind its own token. The door routes by `Host` and answers anything else `401`. | **Enforced** | — |
| NET-19 | A sidecar started inside the sandbox is confined like the command that started it. A child keeps its run's confinement. | **Enforced** | — |
| NET-20 | On macOS a sandboxed run with a cluster or a cloud platform holds a loopback port for the run's life, which any local process can dial. | **By decision** | — |
| NET-21 | A browser page can send a run's loopback authority as `Host`. | **By decision** | — |

## Evidence

### NET-1

- **Where:** `sidecar/internal/run/sandbox`, `sidecar/internal/tools/bash/bash.go`, `sidecar/internal/agent/chat`, `src/components/chat/chat-composer/network-switch.tsx`
- **Records:** [Network is the user's switch](../security/2026-10-04-network-is-the-users-switch.md)
- `TestNoListenerOutsideIsReachable`, `TestWithoutTheInternetNothingIsReached`, `TestAPublicNameDoesNotResolve`, `TestAUnixOrVsockSocketCannotBeOpened`, `TestNoDatagramReachesAHostSocket`, `TestAUserNamespaceCannotBeMade` — without network, a network namespace of its own and a seccomp filter admitting only IP sockets.

### NET-2

- **Where:** `sidecar/internal/run/sandbox`, `sidecar/internal/tools/bash/bash.go`, `sidecar/internal/agent/chat`, `src/components/chat/chat-composer/network-switch.tsx`
- **Records:** [Network is the user's switch](../security/2026-10-04-network-is-the-users-switch.md)
- `TestOnlyTheRunsPortAndSocketAreReached`, `TestARunWithoutAClusterHasNoNetwork`, `TestWithoutTheInternetNothingIsReached`, `TestAPublicNameDoesNotResolve`, `TestTheKeychainIsDenied`, `TestNoServiceReadsForTheRun` — without network, the profile's one TCP endpoint and socket.

### NET-3

- **Where:** `sidecar/internal/run/sandbox`, `sidecar/internal/tools/bash/bash.go`, `sidecar/internal/agent/chat`, `src/components/chat/chat-composer/network-switch.tsx`
- **Records:** [Network is the user's switch](../security/2026-10-04-network-is-the-users-switch.md)
- `TestTheNetworkSwitchIsTheChats`, `TestLeavingTheSandboxTurnsNetworkOff`, `TestASendWithAStaleNetworkSwitchIsRefused` — the switch is the chat's row, on only while the chat is in the sandbox, and a send that saw it the other way is refused.
- Note: the scopes are the user's.

### NET-4

- **Where:** `sidecar/internal/tools/bash/bash.go`, `sidecar/internal/agent/chat`
- **Records:** [Network is the user's switch](../security/2026-10-04-network-is-the-users-switch.md)
- `TestTheTurnsToggleReachesItsCommandsAndItsSubagents` — the toggle is the turn's.

### NET-5

- **Where:** `sidecar/internal/tools/bash/bash.go`, `sidecar/internal/agent/chat`
- **Records:** [Network is the user's switch](../security/2026-10-04-network-is-the-users-switch.md)
- `TestTheApprovalNamesTheNetwork` — a call that asks alone waits on the user.

### NET-6

- **Where:** `sidecar/internal/tools/bash/bash.go`, `sidecar/internal/agent/chat`
- **Records:** [Network is the user's switch](../security/2026-10-04-network-is-the-users-switch.md)
- `TestAMonitorNeverHasNetwork`, `TestAMonitorHasNoNetwork` — a monitor never has network.

### NET-7

- **Where:** `sidecar/internal/run/sandbox`, `sidecar/internal/tools/bash/bash.go`, `sidecar/internal/agent/chat`
- **Records:** [Network is the user's switch](../security/2026-10-04-network-is-the-users-switch.md)
- `TestTheRunTakesTheGatesApproval`, `TestTheRunTakesTheApprovalsNetwork` — the run takes the network its gate decided.
- `TestTheRecordSaysTheNetworkOnceTheCallRuns` — the run's row says why.

### NET-8

- **Where:** `sidecar/internal/run/sandbox`, `sidecar/internal/tools/bash/bash.go`, `sidecar/internal/agent/chat`
- **Records:** [Network is the user's switch](../security/2026-10-04-network-is-the-users-switch.md)
- `TestNothingTurnsNetworkOnWhereItIsUnavailable`, `TestNoPastaNoNetwork` — where network is unavailable nothing turns it on.

### NET-9

- **Where:** `sidecar/internal/run/sandbox/network_linux.go`, `sidecar/internal/run/sandbox/seccomp_linux.go`, `sidecar/internal/run/sandbox/sandbox_darwin.go`
- `TestTheInternetReachesOut`, `TestTheInternetReachesNoHostLoopback`, `TestTheInternetResolves` — both platforms.

### NET-10

- **Where:** `sidecar/internal/run/sandbox/sandbox_darwin.go`
- **Records:** [a macOS run with the internet has IPv4 only](../security/2026-10-04-a-macos-run-with-the-internet-has-ipv4-only.md)
- `TestTheInternetReachesNoHostAddress`, `TestTheInternetIsIPv4Only` — on macOS, none of the host's addresses and IPv4 alone.
- Note: Seatbelt cannot name the IPv4-mapped forms, so IPv6 is denied whole.

### NET-11

- **Where:** `sidecar/internal/run/sandbox/network_linux.go`, `sidecar/internal/run/sandbox/seccomp_linux.go`
- `TestANetworkRunHoldsNoCapability`, `TestANetworkCallRunsUnderPasta`, `TestAPastaFailureFailsTheRun` — a network run holds no capability and runs under `pasta`.
- `TestTheLimitsHoldWithTheInternet`, `TestANetworkRunKeepsTheExitCode`, `TestStopEndsANetworkRun`, `TestATimeoutEndsANetworkRun`, `TestKillingTheLauncherEndsTheRun` — a network run on Linux.
- `TestTheProbeNeedsNoRoute`, `TestTheProbeIsBounded` — the probe reads no route.

### NET-12

- **Where:** `sidecar/internal/run/sandbox/network_linux.go`, `sidecar/internal/run/sandbox/seccomp_linux.go`
- `TestANetworkRunCannotUnmountANeverPath`, `TestTheFilterRefusesMounts`, `TestTheMountSyscallsAreRefused` — on Linux, no run mounts or unmounts anything.

### NET-13

- **Where:** `sidecar/internal/run/sandbox/profile_darwin.sb`, `sidecar/internal/run/sandbox/sandbox_darwin.go`
- **Records:** [network is the user's switch](../security/2026-10-04-network-is-the-users-switch.md)
- `TestTheProfileNamesNoRefusedService` — its one exception is a run with the internet.
- `TestTrustFetchesNothingWithoutTheInternet` — without the internet the trust daemon fetches no issuer or OCSP URL for the run.

### NET-14

- **Where:** `sidecar/internal/run/sandbox`, `src/components/chat/chat-composer/network-switch.tsx`
- **Records:** [network is the user's switch](../adr/2026-10-04-network-is-the-users-switch.md)
- `network-switch.test.tsx` — the dialog that turns the chat's switch on says the command holds no credential.
- Note: the command holds no credential.
- Note: a background command keeps the network it started with.
- Note: a subagent started under the toggle keeps it for its whole life.

### NET-15

- **Where:** `sidecar/internal/run/sandbox/sandbox_darwin.go`, `sidecar/internal/run/sandbox/network_linux.go`
- **Records:** [network is the user's switch](../adr/2026-10-04-network-is-the-users-switch.md)
- Note: Seatbelt names `localhost` or `*` and no other host.
- Note: a port Docker publishes listens on every address of the host, so it is reachable on Linux and shut on macOS.

### NET-16

- **Where:** `sidecar/internal/run/sandbox/sandbox_darwin.go`
- **Records:** [network is the user's switch](../adr/2026-10-04-network-is-the-users-switch.md)
- `TestTrustFetchesNothingWithoutTheInternet` — without network the daemon is refused.
- Note: the daemon fetches a certificate's issuer, OCSP and CRL URLs from outside the sandbox.
- Note: the command reads only whether the certificate verified.
- Note: refusing the daemon would break TLS for every Go tool.

### NET-17

- **Where:** `sidecar/internal/run/sandbox`, `sidecar/internal/tools/bash/proxy.go`, `sidecar/internal/run/runproxy`
- `TestKubetailsDirectoriesAreUnreadable` — the host's socket lies in Kubetail's runtime directory, which no run reads, on both platforms.
- `TestNoListenerOutsideIsReachable`, `TestAUnixOrVsockSocketCannotBeOpened` — on Linux no listener outside the run's namespace answers and no Unix socket opens.
- `TestOnlyTheRunsPortAndSocketAreReached` — on macOS only the run's own port and socket are reached, never another loopback listener or socket.

### NET-18

- **Where:** `sidecar/internal/run/sandbox`, `sidecar/internal/tools/bash/proxy.go`, `sidecar/internal/run/runproxy`
- `TestTheDoorRoutesByHost` — the door routes by `Host` and answers anything else `401`.
- `TestAWrongRouteIsUnauthorized` — each grant checks its own token, so a request on the wrong route is refused.

### NET-19

- **Where:** `sidecar/internal/run/sandbox`, `sidecar/internal/tools/bash/proxy.go`, `sidecar/internal/run/runproxy`
- `TestAProcessThatLeavesTheGroupStaysConfined`, `TestAUserNamespaceCannotBeMade` — a child keeps its run's confinement.

### NET-20

- **Where:** `sidecar/internal/run/sandbox/forward.go`, `sidecar/internal/run/sandbox/sandbox_darwin.go`
- **Records:** [a sandboxed forwarder holds a loopback port on macOS](../adr/2026-09-28-a-sandboxed-forwarder-holds-a-loopback-port-on-macos.md)
- `TestAWrongOrDeadTokenIsUnauthorized`, `TestAnotherHostIsUnauthorized` — the proxy behind the port answers 401 without the run's token and `Host`.
- `TestATakenPortFailsTheRun` — a port taken in between fails the run.

### NET-21

- **Where:** `sidecar/internal/tools/bash/proxy.go`
- **Records:** [the run's door](../adr/2026-11-17-a-sandboxed-command-reaches-a-cloud-platform-through-a-proxy-on-its-runs-one-relay.md), [record](../security/2026-11-17-the-runs-door.md)
- Note: a page cannot send a grant's token, so every route answers `401`.
