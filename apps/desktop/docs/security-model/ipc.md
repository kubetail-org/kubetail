# The socket and the processes

[← Security model](../security-model.md)

The host and the sidecar talk over one Unix socket or named pipe, and each end checks who is on the other. The socket is bound owner-only, and on Windows the named pipe is restricted to the owner by DACL. A sidecar the host launched accepts only the host's PID, and the host refuses any peer that is not the sidecar it spawned. The sidecar takes its OAuth endpoints from the host's arguments, not from inherited environment. The Windows arm of the host's peer check is exercised only by a native run.

## Protections

| ID | Protection | Status | Residual |
| --- | --- | --- | --- |
| IPC-1 | The IPC socket is bound owner-only, with no world-accessible window between bind and chmod. | **Enforced** | — |
| IPC-2 | The named pipe is restricted to the owner by DACL. | **Enforced** | — |
| IPC-3 | The sidecar accepts only the host PID when launched by the host. | **Enforced** | — |
| IPC-4 | The host refuses any IPC peer that is not the sidecar it spawned. The check is wired into the transport, not merely available. | **Enforced** | The Windows arm compiles from the same code but is exercised only by a native run. |
| IPC-5 | The host refuses to connect when no sidecar is running: the exited child's PID may be one the OS has reassigned. | **Enforced** | The Windows arm compiles from the same code but is exercised only by a native run. |
| IPC-6 | OAuth endpoints come from the host's arguments, not from inherited environment. | **Enforced** | — |

## Evidence

### IPC-1

- **Where:** `sidecar/internal/lib/ipc/listen_unix.go`
- `TestListen_IsOwnerOnly` — the socket is bound owner-only.

### IPC-2

- **Where:** `sidecar/internal/lib/ipc/listen_windows.go`
- `TestOwnerOnlyDACL_ParsesAndIsRestrictive` — the DACL parses and restricts the pipe to the owner.

### IPC-3

- **Where:** `sidecar/internal/lib/ipc/authlistener.go`
- `TestAuthenticated_RejectsForeignPeer` — a peer that is not the host PID is rejected.

### IPC-4

- **Where:** `src-tauri/src/services/sidecar/{ipc,peer}.rs`
- `connect_refuses_a_peer_that_is_not_the_expected_process` — binds a hostile listener in the test process and asserts the dial is refused.
- `query_refuses_a_server_that_is_not_the_sidecar` — pins that the check is wired into the transport, not merely available.

### IPC-5

- **Where:** `src-tauri/src/services/sidecar/{ipc,peer}.rs`
- `connect_refuses_when_no_sidecar_is_running` — covers the exited child, whose PID the OS may have reassigned.

### IPC-6

- **Where:** `src-tauri/src/services/sidecar/service.rs`, `sidecar/config.go`
- `cmd_args_passes_the_runtime_directory_host_pid_and_endpoints` — the host passes the runtime directory, host PID and endpoints as arguments.
- `TestConfigFromArgsIgnoresEnvironment` — the sidecar's config ignores the environment.
- `release-desktop.yml` — the untagged-build assertion.
