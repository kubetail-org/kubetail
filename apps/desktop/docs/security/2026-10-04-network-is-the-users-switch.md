# Security record — network is the user's switch, 4 October 2026

**Subject:** a sandboxed command can reach the internet, but only when the user says so. This is
step 4C of Kstack's agent-security spec sequence. The living model is
[security-model.md](../security-model.md); the decision is [network is the user's
switch](../adr/2026-10-04-network-is-the-users-switch.md).

## What changed

Until now a sandboxed command reached nothing past its loopback but the cluster proxy
([Bash runs in a sandbox](2026-09-28-bash-runs-in-a-sandbox.md)). Now the user gives a sandboxed
command the internet in one of three scopes:

- **For the chat**: a switch beside the sandbox switch, off for a new chat. Turning it on opens a
  dialog that says commands can send what they read to any server; only its confirm calls the
  mutation, and turning it off needs no dialog (`network-switch.test.tsx`). The switch is the
  chat's row, reaches every window through the list watch, and leaves `updatedAt` alone
  (`TestTheNetworkSwitchIsTheChats`, `TestChatNetworkEnabledSetServesTheSwitchedChat`). It is on
  only while the chat is in the sandbox: leaving the sandbox turns it off in the same write, and
  turning it on outside is refused, so a chat back in the sandbox starts without network and the
  dialog is seen again (`TestLeavingTheSandboxTurnsNetworkOff`).
- **For one message**: a toggle in the composer. Every command of that turn has network, its
  subagents' included, and the next turn has none (`TestTheTurnsToggleReachesItsCommandsAndItsSubagents`).
  The toggle is cleared once a send is accepted (`chat-outbox.test.tsx`, `chat-composer.test.tsx`).
- **For one command**: Bash takes a `network` argument. With network off for the chat and the
  turn, a sandboxed call that sets it asks: *Run this command with network access?*, drawn as any
  command's request (`TestTheApprovalNamesTheNetwork`, `chat-transcript.test.tsx`).

**A send runs with the network its sender saw.** `chatSend` carries the chat's network switch as
the composer showed it, and the transaction that reserves the turn refuses one that differs with
`KUBETAIL_CHAT_NETWORK_CHANGED`; a replay is answered by its key alone
(`TestASendWithAStaleNetworkSwitchIsRefused`).

**The switch is read live, the decision is the gate's.** Each command's session reads the chat's
switch as it starts, so a switch turned on or off mid-turn reaches the next command
(`TestTheSwitchIsReadLive`). The run then takes the network its approval decided and never reads
the session again, so an approved call keeps network the switch took away since, and an unasked one
gets none the switch gave since (`TestTheRunTakesTheGatesApproval`,
`TestTheRunTakesTheApprovalsNetwork`). A background command keeps what it started with
(`TestABackgroundCommandKeepsItsNetwork`). A session with no `Network` — the monitor, which nothing
builds yet — never has it, and its call that asks is refused unasked (`TestAMonitorNeverHasNetwork`).

**Every call records why it had network.** `tool_calls.network` is written on the row that marks
the call running, so a call that waited on the user and was denied keeps none
(`TestTheRecordSaysTheNetworkOnceTheCallRuns`), and its disclosure says `with network`.

**The model is told.** The question's `## Sandbox` section says `off`, `on for this chat`, `on
for this message` or `unavailable on this machine`, and goes again whenever that changes, a
notice turn's included (`TestTheContextSaysTheNetwork`,
`TestANoticeTurnSaysTheNetworkWhenTheSwitchMoved`). A failed run's trailer says the network it
had (`TestTheTrailerSaysTheRunsNetwork`).

## What the internet is

The internet with nothing filtered by host, minus the machine's own loopback.

- **macOS**: the profile allows every outbound address but the host's own, loopback included, and
  allows the resolver's socket, `com.apple.dnssd.service` and `com.apple.trustd.agent`
  (`TestTheCompiledProfileMatchesTheGolden`'s `internet` case, `TestTheProfileNamesNoRefusedService`).
  The deny is Seatbelt's `localhost` under `ip4` and `ip6`, which matches every address the host
  holds and the unspecified and IPv4-mapped forms of loopback; a refused connect fails with EPERM
  (`TestTheInternetReachesNoHostLoopback`, `TestTheInternetReachesNoHostAddress`). The relay's own
  port follows the deny, so the cluster stays reachable.
- **Linux**: `pasta` makes the run a user and network namespace of its own and gives it the
  internet; bwrap runs inside it, makes its own user namespace, runs the command as the user's ids
  and drops every capability (`TestANetworkRunHoldsNoCapability`). `--no-map-gw` and `-T none -U
  none` map nothing to the host's loopback (`TestTheInternetReachesNoHostLoopback`). A run reads a
  `resolv.conf` of its own, `nameserver 169.254.1.53`, which pasta forwards to the host's resolver
  (`TestTheInternetResolves`, `TestANetworkCallRunsUnderPasta`).
- **No run mounts anything.** The seccomp filter refuses `mount`, `umount2`, `pivot_root` and the
  new mount API on every run, so a capability that slipped through unmounts no denial
  (`TestTheFilterRefusesMounts`, `TestTheMountSyscallsAreRefused`,
  `TestANetworkRunCannotUnmountANeverPath`).
- **Stop and the timeout end the whole run**: pasta, bwrap and the command
  (`TestStopEndsANetworkRun`, `TestATimeoutEndsANetworkRun`, `TestKillingPastaEndsTheRun`). A run
  whose pasta fails, fails with pasta's words (`TestAPastaFailureFailsTheRun`).
- **Where network is unavailable nothing turns it on.** The probe looks for pasta and runs it once
  without reading a route (`TestTheProbeNeedsNoRoute`, `TestTheProbeIsBounded`,
  `TestNoPastaNoNetwork`). Without it the switch's mutation and a send with the toggle are refused
  (`TestNothingTurnsNetworkOnWhereItIsUnavailable`), a switch already on gives its calls none, and
  a `network: true` call is refused (`TestTheApprovalNamesTheNetwork`).

## The trust daemon follows the network

`com.apple.trustd.agent` verifies a certificate for a Go program or Security.framework, and while
doing so fetches the certificate's issuer and OCSP URLs from outside the sandbox. Allowed with no
network, it would be the network by another door. So it is refused in every run without the
internet and allowed with it (`TestTrustFetchesNothingWithoutTheInternet`).

## What still holds

The command holds no credential: the cluster's stay in the cluster proxy, and the denied-always list
keeps `~/.ssh`, `~/.kube` and the rest shut. Every cluster write still goes through the cluster
proxy and the user's modes and rules; a command can reach the API server directly, with nothing to
log in with. Secret values stay redacted. Writes stay in the workspace. The host's loopback stays
shut, so Ollama, a database left at its default and Kubetail's own forwarders are out of reach.

## The per-command request

The request for a command with network is a call's own approval under [the bash
tool](2026-09-18-bash-tool.md): it shows the exact command, and Approve is a click on it. It is
never a way out of the sandbox: the command stays confined, holds no credential and reaches the
cluster through its proxy ([outside the sandbox is the user's
choice](2026-09-30-outside-the-sandbox-is-the-users-choice.md)).

## Prompt injection

Text in the cluster can make the model ask for network, in words or with `network: true`. It
cannot give a command network: the chat's switch and the toggle are the user's clicks, and the
argument asks.

## Residuals

- **A command with network can send what it read to any server**: cluster data short of Secret
  values, and the workspace. The dialog says so, and this is the trade the user makes.
- **The local network.** On macOS Seatbelt names `localhost` or `*` and no other host, so the local
  network is reachable. On Linux a host service on any address
  but loopback and the one pasta copies is reachable, a Docker bridge's among them. A port Docker
  publishes listens on every address of the host by default, so a container's published port is
  reachable on Linux and shut on macOS.
- **The trust daemon reaches the loopback.** On macOS, with network on, a command can write a URL
  into a certificate and have `trustd` fetch it from outside the sandbox: a GET to any host and
  path, the loopback and the local network included. The command never reads the response, only
  whether the certificate verified, but the GET can have side effects. Without network the daemon
  is refused.
- **What started with network keeps it.** A background command keeps the network it started with
  until it exits, and a subagent started in a turn with the toggle keeps it for its whole life.
- **macOS is confirmed on one version.** The macOS tests pass on macOS 27.0.1; Seatbelt's network
  matching is undocumented, so another version may differ, and CI's macOS job is where it shows.
