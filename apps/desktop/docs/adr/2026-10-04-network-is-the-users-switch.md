---
title: Network is the user's switch
date: 2026-10-04
scope: cross-cutting
status: Accepted
---

# Network is the user's switch

## Context

A sandboxed command reached one thing past its loopback: the cluster proxy, over the run's socket.
`helm repo update`, `gh`, `curl` and `git clone` failed in the sandbox, and the user's only way out
was to switch the chat outside it ([leaving the sandbox is the user's switch for a
chat](2026-09-30-leaving-the-sandbox-is-the-users-switch-for-a-chat.md)), which gives every command
the user's files and credentials too. Step 4C of the agent-security sequence gives a sandboxed
command the internet without that.

On macOS an experiment measured the trust daemon: with `com.apple.trustd.agent` allowed and no
network, a sandboxed program that verifies a certificate it built makes the daemon fetch the
certificate's issuer URL, and with revocation asked for its OCSP URL, from outside the sandbox.

## Decision

**A sandboxed command has no network unless the user turned it on**, in one of three scopes, any
of which turns it on: the chat's switch (`chats.network_enabled`, behind a confirm), a toggle for
one message (`chatSend`'s `networkThisTurn`, which reaches that turn's subagents), and a request for
one command (Bash's `network` argument, which asks). A session's `Network` reads the chat's switch
at each command; the gate decides `tools.Approval.Network`, and the run takes it
(`tools.ApprovedRunner`), never reading the session again. Each call records why it had network
(`tool_calls.network`).

**Network is the internet with nothing filtered by host, minus the host's loopback.** On macOS the
profile allows every outbound address and denies `localhost`, which shuts every address the host
holds. On Linux `pasta` makes the run's
namespace and gives it the internet; bwrap runs inside it as the user with no capability, and the
seccomp filter refuses every mount syscall on every run. A run reads its own `resolv.conf`, which
pasta forwards.

**The trust daemon and the resolver follow the network**: allowed in a run with the internet,
refused in every other.

**Linux network needs `pasta`.** The probe looks for it, never off `PATH`, and runs it once without
reading a route, so a Kubetail started offline still offers network. Without it nothing turns network
on, and a switch left on gives its calls none.

## Alternatives considered

**A per-host allowlist through a proxy.** It reached only proxy-aware tools, and on macOS a proxy
needed the trust daemon refused, which breaks TLS for every Go tool, or allowed, which let any
command send data around the proxy through it. A host allowlist can return later as a third setting
for proxy-aware tools.

**Sharing the host's network namespace on Linux.** It needs nothing installed, but opens the host's
loopback, where Ollama, databases left at their defaults and Kubetail's own forwarders listen.

**Only a chat-wide switch.** A message or a command that needs the internet once would then open
the whole chat for good, and a model that wants a command's internet would have to ask in words.

**Letting the per-command argument leave the sandbox.** Step 1B took `dangerouslyDisableSandbox`
away because a chain of approvals left the sandbox. Here the command stays confined and holds no
credential, and each request names the one command it lets reach the internet.

## Consequences

- A command with network can send what it read to any server. The dialog says so.
- On macOS the local network is reachable, since Seatbelt names `localhost` or `*` and no other
  host, and the trust daemon can send a GET to the loopback for a command with network.
- On Linux a host service on an address other than loopback and the one pasta copies is reachable.
- Every runtime reader of the sandbox holds `sandbox.Status`'s network pair, and `chatSend` gains
  two required arguments.
- The probe adds at most `probeBound` to startup after bwrap's.

## Revisit when

A host allowlist is wanted for proxy-aware tools, Seatbelt gains a way to refuse the local network
by address, or `pasta` stops being available on a supported distribution.
