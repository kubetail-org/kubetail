// Copyright 2024 The Kubetail Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

use std::ffi::OsString;
use std::path::Path;
use std::sync::mpsc::{sync_channel, Receiver, SyncSender};
use std::sync::{Arc, Mutex};
use std::time::Duration;

use tauri::ipc::Channel;
use tauri_plugin_shell::process::{Command, CommandChild, CommandEvent};
use tokio::sync::watch;

use crate::error::{AppError, Result};
use crate::paths::Folders;

use super::graphql::{FrameSink, GraphqlResponse, QueryClient, SubscriptionClient};
use super::grpc::{AuthStateStream, GrpcClient};
use super::ipc::{Endpoint, Target};
use super::logs::forward_sidecar_line;

/// First-token marker the sidecar prints on stdout once its listener is up
/// (`sidecar/main.go`); the drain task matches it to flip the readiness watch.
const READY_MARKER: &[u8] = b"READY ";

/// First-token marker the sidecar prints on stdout, followed by the reason,
/// when it cannot compose the app; it exits 1 right after.
const FATAL_MARKER: &[u8] = b"FATAL ";

/// How long [`SidecarService::ready`] waits for [`READY_MARKER`]. Startup is not
/// a dial but a sum: on macOS and Linux the sidecar first runs the user's login
/// shell (`sidecar/internal/run/loginshell`, ten seconds at worst), then
/// migrates and binds. The model catalogs are discovered after `READY`, so they
/// are not in it. The frontend's `ReadyGate` does not retry on its own — one
/// timeout is an error screen — so this clears that sum with room to spare.
const READY_BUDGET: Duration = Duration::from_secs(30);

/// Sidecar child lifecycle state. `graceful_shutdown` takes `child` (dropping
/// it closes stdin) but keeps `pid` so `kill` can still force-terminate;
/// `exited` makes `kill` skip the pid fallback — the number may be reused.
struct State {
    /// The running child, or `None` once graceful_shutdown/kill has run.
    child: Option<CommandChild>,
    /// Retained after `child` is dropped; `None` once force-killed.
    pid: Option<u32>,
    /// Set by the drain task; once true the pid is not safe to kill.
    exited: bool,
}

/// Where the sidecar's startup stands, as its stdout reports it.
#[derive(Debug, Clone, PartialEq, Eq)]
enum Startup {
    Pending,
    Ready,
    /// The reason it printed after [`FATAL_MARKER`].
    Failed(String),
}

/// Owns the bundled sidecar child process and its IPC bridges (query client,
/// SSE reader, gRPC channel), all on the [`Endpoint`] picked at spawn time.
/// Construct via [`SidecarService::spawn`]. Shutdown can be driven from any
/// thread (state behind `Arc<Mutex>`).
pub struct SidecarService {
    state: Arc<Mutex<State>>,
    exit_rx: Mutex<Receiver<()>>,
    /// Leaves `Pending` on [`READY_MARKER`] or [`FATAL_MARKER`]; on process
    /// exit the sender drops, surfacing to `wait_for` as a recv error
    /// ("exited before ready").
    startup_rx: watch::Receiver<Startup>,
    query_client: QueryClient,
    subscription_client: SubscriptionClient,
    /// Host-internal control surface (auth watch/login/logout, poke), h2c over
    /// the same socket as GraphQL. See docs/adr/2026-10-26-single-socket-h2c-from-the-standard-library.md.
    grpc_client: GrpcClient,
}

impl SidecarService {
    /// Spawns the sidecar; a background task drains its event stream (log
    /// forwarding + termination reporting). `sidecar` makes the command from
    /// its arguments: `app.rs` resolves the bundled binary, a test a stand-in.
    pub fn spawn(
        folders: &Folders,
        sidecar: impl FnOnce(Vec<OsString>) -> Result<Command>,
    ) -> Result<Self> {
        // The one folder the host places for the sidecar: a squatted name can
        // move it. The sidecar places the rest itself.
        let runtime = &folders.runtime;
        let endpoint = Endpoint::pick(runtime)?;

        let (mut rx, child) = sidecar(cmd_args(&endpoint, runtime))?.spawn()?;
        let pid = child.pid();
        // The sidecar's own account of its startup is in its `sidecar.log`,
        // in the layout's log folder, so main.log records that it was spawned
        // and says where to look.
        tracing::info!(pid, log_dir = ?folders.logs, "sidecar spawned");

        let state = Arc::new(Mutex::new(State {
            child: Some(child),
            pid: Some(pid),
            exited: false,
        }));

        // What the dialers check every peer against. `State.pid` cannot serve:
        // it outlives the child so `kill` can still reach it, and the kernel
        // may have reassigned the number by then.
        let (expect_tx, expect_rx) = watch::channel(Some(pid));

        // Capacity 1 so the drain task never blocks delivering the exit.
        let (exit_tx, exit_rx) = sync_channel::<()>(1);

        let (startup_tx, startup_rx) = watch::channel(Startup::Pending);

        let drain = Drain {
            state: Arc::clone(&state),
            startup_tx,
            expect_tx,
            exit_tx,
        };
        tauri::async_runtime::spawn(async move {
            while let Some(event) = rx.recv().await {
                drain.on_event(event);
            }
        });

        let target = Target::new(endpoint, expect_rx);
        let query_client = QueryClient::new(target.clone());
        let subscription_client = SubscriptionClient::new(target.clone());
        let grpc_client = GrpcClient::new(target);

        Ok(Self {
            state,
            exit_rx: Mutex::new(exit_rx),
            startup_rx,
            query_client,
            subscription_client,
            grpc_client,
        })
    }

    /// A service with no process behind it. [`Self::ready`] answers at once:
    /// `Ok` for `None`, the reason for `Some`. Every dial is refused, and
    /// [`Self::graceful_shutdown`] returns `false` at once. Tests only.
    #[cfg(test)]
    pub fn without_process(fatal: Option<&str>) -> Self {
        Self::detached(None, fatal, Self::nowhere())
    }

    /// A service that [`Self::kill`] treats as still running under `pid`, a
    /// process the test started itself. Tests only.
    #[cfg(all(test, unix))]
    pub fn running_as(pid: u32) -> Self {
        Self::detached(Some(pid), None, Self::nowhere())
    }

    /// A ready service whose dials reach `endpoint`, served by this process.
    /// Tests only.
    #[cfg(test)]
    pub fn dialing(endpoint: Endpoint) -> Self {
        let target = Target::expecting(endpoint, Some(std::process::id()));
        Self::detached(None, None, target)
    }

    /// A target every dial to is refused.
    #[cfg(test)]
    fn nowhere() -> Target {
        let endpoint = Endpoint::pick(&std::env::temp_dir()).expect("pick an endpoint");
        Target::expecting(endpoint, None)
    }

    #[cfg(test)]
    fn detached(pid: Option<u32>, fatal: Option<&str>, target: Target) -> Self {
        let state = Arc::new(Mutex::new(State {
            child: None,
            pid,
            exited: false,
        }));
        // The sender drops here, so a wait for the exit ends at once.
        let (_, exit_rx) = sync_channel::<()>(1);
        let startup = match fatal {
            None => Startup::Ready,
            Some(reason) => Startup::Failed(reason.to_string()),
        };
        let (_, startup_rx) = watch::channel(startup);
        Self {
            state,
            exit_rx: Mutex::new(exit_rx),
            startup_rx,
            query_client: QueryClient::new(target.clone()),
            subscription_client: SubscriptionClient::new(target.clone()),
            grpc_client: GrpcClient::new(target),
        }
    }

    /// Waits for the sidecar's [`READY_MARKER`]; errors with the sidecar's
    /// own reason after [`FATAL_MARKER`], if it exits first (`Io`), or if
    /// [`READY_BUDGET`] elapses (`TimedOut`). The frontend's startup gate —
    /// without it the first GraphQL call silently absorbs the bind-wait
    /// latency.
    pub async fn ready(&self) -> Result<()> {
        wait_ready(self.startup_rx.clone()).await
    }

    /// Forwards a GraphQL query/mutation over UDS HTTP; see [`QueryClient`].
    pub async fn query(&self, body: String) -> Result<GraphqlResponse> {
        self.query_client.query(body).await
    }

    /// Registers a GraphQL subscription on its own SSE connection, forwarding
    /// envelopes to `channel`. Returns the op id for
    /// [`SidecarService::unsubscribe`]. `webview` is the label of the webview
    /// that owns the channel, so [`SidecarService::cancel_webview`] can reach
    /// it.
    pub async fn subscribe(
        &self,
        query: String,
        variables: serde_json::Value,
        channel: Channel<String>,
        webview: String,
    ) -> Result<u64> {
        // `FrameSink` is the delivery seam; the webview's `Channel` is wrapped
        // in `TauriChannelSink`.
        self.subscription_client
            .subscribe(
                query,
                variables,
                Arc::new(TauriChannelSink(channel)),
                webview,
            )
            .await
    }

    /// Cancels a subscription; tolerant of unknown ids — see
    /// [`SubscriptionClient::unsubscribe`].
    pub async fn unsubscribe(&self, id: u64) {
        self.subscription_client.unsubscribe(id).await;
    }

    /// Cancels every subscription a webview opened — see
    /// [`SubscriptionClient::cancel_webview`].
    pub fn cancel_webview(&self, webview: &str) {
        self.subscription_client.cancel_webview(webview);
    }

    /// Runs the sidecar's synchronous login setup (loopback bind + browser
    /// open); the resulting session change arrives via
    /// [`Self::watch_auth_state`].
    pub async fn start_login(&self) -> Result<()> {
        self.grpc_client.start_login().await
    }

    /// Clears local credentials and revokes the refresh token.
    pub async fn logout(&self) -> Result<()> {
        self.grpc_client.logout().await
    }

    /// Opens the auth-state watch stream (current snapshot first, then one per
    /// session change). Error / end-of-stream means the connection dropped —
    /// the supervisor re-opens it.
    pub async fn watch_auth_state(&self) -> Result<AuthStateStream> {
        self.grpc_client.watch_auth_state().await
    }

    /// Best-effort resync nudge, driven by [`crate::wake`]; failures are the
    /// caller's to swallow. See docs/adr/2026-08-09-poke-resync-fanout.md.
    pub async fn poke(&self) -> Result<()> {
        self.grpc_client.poke().await
    }

    /// Graceful shutdown: dropping the [`CommandChild`] closes stdin, and the
    /// sidecar treats stdin EOF as "parent gone" (cross-platform, unlike POSIX
    /// signals). Blocks until the process exits or `timeout` lapses — safe from
    /// the `RunEvent` handler. Returns `false` on timeout (follow up with
    /// [`SidecarService::kill`]; the pid is retained for that). No-op → `true`
    /// if already shut down.
    pub fn graceful_shutdown(&self, timeout: Duration) -> bool {
        {
            let mut state = self
                .state
                .lock()
                .unwrap_or_else(|poisoned| poisoned.into_inner());
            if state.exited {
                return true;
            }

            // Drops stdin → EOF. `pid` stays. Guard released before blocking so
            // the drain task can set `exited`.
            drop(state.child.take());
        }

        // recv_timeout errs on both timeout and a dropped sender; either way
        // the process is not known to have exited.
        let exit_rx = self
            .exit_rx
            .lock()
            .unwrap_or_else(|poisoned| poisoned.into_inner());
        exit_rx.recv_timeout(timeout).is_ok()
    }

    /// Force-terminates the sidecar. Kills through the live child handle if
    /// held, else by retained pid — but only if the process hasn't been
    /// observed to exit (a reused pid must not be killed). Best-effort and
    /// idempotent; for a clean exit call
    /// [`SidecarService::graceful_shutdown`] first. Returns whether it sent a
    /// kill, so a quit can say the graceful path did not finish.
    pub fn kill(&self) -> bool {
        let mut state = self
            .state
            .lock()
            .unwrap_or_else(|poisoned| poisoned.into_inner());

        // Prefer the live handle: reaps the child, no pid-reuse race.
        if let Some(child) = state.child.take() {
            let _ = child.kill();
            state.pid = None;
            return true;
        }

        // Handle gone: pid fallback, unless the process already exited (the
        // kernel may have reused the number).
        if state.exited {
            state.pid = None;
            return false;
        }

        match state.pid.take() {
            Some(pid) => {
                force_kill_by_pid(pid);
                true
            }
            None => false,
        }
    }
}

/// What the drain task updates as the sidecar's events arrive.
struct Drain {
    state: Arc<Mutex<State>>,
    startup_tx: watch::Sender<Startup>,
    expect_tx: watch::Sender<Option<u32>>,
    exit_tx: SyncSender<()>,
}

impl Drain {
    fn on_event(&self, event: CommandEvent) {
        match event {
            CommandEvent::Stdout(line) => stdout_line(&line, &self.startup_tx),
            CommandEvent::Stderr(line) => forward_sidecar_line(&line),
            CommandEvent::Terminated(payload) => {
                if exited_cleanly(payload.code, payload.signal) {
                    tracing::info!(?payload, "sidecar exited");
                } else {
                    tracing::warn!(?payload, "sidecar exited uncleanly");
                }

                // No sidecar, so no peer is legitimate any more.
                let _ = self.expect_tx.send(None);

                // `kill` must skip the pid fallback after this (pid may be
                // reused).
                self.state
                    .lock()
                    .unwrap_or_else(|poisoned| poisoned.into_inner())
                    .exited = true;

                // Wake any thread blocked in graceful_shutdown.
                let _ = self.exit_tx.try_send(());
            }
            // `Error`: a pipe could not be read.
            other => tracing::warn!(event = ?other, "unexpected sidecar event"),
        }
    }
}

/// Whether the sidecar's exit was a completed stop: code 0 and no signal. Its
/// watchdog exits 1 on a wedged stop, and the host's kill is a signal.
fn exited_cleanly(code: Option<i32>, signal: Option<i32>) -> bool {
    code == Some(0) && signal.is_none()
}

/// The endpoints the app signs in against. Constants, never read from this
/// process's environment: the sidecar inherits that environment, so reading
/// them here would move the redirection risk rather than close it. A dev run
/// overrides them inside the sidecar's own debug build.
const OAUTH_ISSUER: &str = "https://oauth.kubetail.com";
const OAUTH_CLIENT_ID: &str = "kubetail-desktop";

/// The OS-keychain service the sidecar stores sign-in under: the product name,
/// as the entry is user-visible in Keychain Access and Credential Manager.
/// Renaming it orphans every stored sign-in, so it is its own constant even
/// though it matches the data directory's leaf today.
const KEYCHAIN_SERVICE: &str = if cfg!(debug_assertions) {
    "Kubetail-dev"
} else {
    "Kubetail"
};

/// CLI flags for the sidecar; a free function so the contract is unit-testable
/// without the Tauri runtime.
///
/// `--host-pid` is the only process the sidecar's endpoint will serve. We are
/// its sole client — the webview reaches it through us — so this closes the
/// endpoint to every other process running as the user.
///
/// `--dev` is this build's profile, so the sidecar's folders take the host's
/// leaf whatever the sidecar was built with.
///
/// `runtime` is the one folder the host places for the sidecar; it places the
/// rest itself, its log file included. Paths go as the OS spells them: a lossy
/// conversion would hand the sidecar a different directory than the host uses.
fn cmd_args(socket: &Endpoint, runtime: &Path) -> Vec<OsString> {
    let mut args: Vec<OsString> = [
        ("--socket", socket.as_arg().into()),
        ("--runtime-dir", runtime.into()),
        ("--host-pid", std::process::id().to_string().into()),
        ("--oauth-issuer", OAUTH_ISSUER.into()),
        ("--oauth-client-id", OAUTH_CLIENT_ID.into()),
        ("--keychain-service", KEYCHAIN_SERVICE.into()),
    ]
    .into_iter()
    .flat_map(|(flag, value): (&str, OsString)| [flag.into(), value])
    .collect();
    // A Go bool flag takes its value after `=` alone.
    args.push(format!("--dev={}", cfg!(debug_assertions)).into());

    // A developer's terminal wants both processes' records; a release build's
    // pipes stay quiet so nothing is stored twice.
    if cfg!(debug_assertions) {
        args.push("--log-stderr".into());
    }
    args
}

/// Best-effort kill by pid — the fallback once [`SidecarService::graceful_shutdown`]
/// dropped the child handle. Stale pids are ignored.
#[cfg(unix)]
fn force_kill_by_pid(pid: u32) {
    // SAFETY: kill(2) has no memory effects; a stale pid returns ESRCH.
    unsafe {
        libc::kill(pid as libc::pid_t, libc::SIGKILL);
    }
}

#[cfg(windows)]
fn force_kill_by_pid(pid: u32) {
    use windows_sys::Win32::Foundation::CloseHandle;
    use windows_sys::Win32::System::Threading::{OpenProcess, TerminateProcess, PROCESS_TERMINATE};

    // SAFETY: OpenProcess returns null on failure (checked); TerminateProcess
    // and CloseHandle are sound on the handle it returns.
    unsafe {
        let handle = OpenProcess(PROCESS_TERMINATE, 0, pid);
        if handle.is_null() {
            return;
        }
        TerminateProcess(handle, 1);
        CloseHandle(handle);
    }
}

/// Adapts Tauri's `Channel<String>` to [`FrameSink`]. `send` errors only if
/// the webview is gone; the frame is safely dropped.
struct TauriChannelSink(Channel<String>);

impl FrameSink for TauriChannelSink {
    fn send_frame(&self, frame: String) {
        let _ = self.0.send(frame);
    }
}

/// Handles one line of the sidecar's stdout. stdout carries the readiness
/// protocol and nothing else; any other line there is as unexpected as a
/// stderr line and forwarded the same way.
fn stdout_line(line: &[u8], startup: &watch::Sender<Startup>) {
    if line.starts_with(READY_MARKER) {
        tracing::info!("sidecar ready");
        let _ = startup.send(Startup::Ready);
    } else if let Some(reason) = line.strip_prefix(FATAL_MARKER) {
        // The line reader keeps the line's end.
        let reason = String::from_utf8_lossy(reason).trim_end().to_owned();
        let _ = startup.send(Startup::Failed(reason));
    } else {
        forward_sidecar_line(line);
    }
}

/// [`SidecarService::ready`] over the startup watch. A `Failed` stored before
/// the sender dropped wins: `wait_for` checks the current value before it
/// reports a closed channel.
async fn wait_ready(mut rx: watch::Receiver<Startup>) -> Result<()> {
    // `.clone()` releases the `watch::Ref` before the match, whose borrow on
    // `rx` would otherwise trip the borrow checker.
    let waited = tokio::time::timeout(
        READY_BUDGET,
        rx.wait_for(|startup| *startup != Startup::Pending),
    )
    .await
    .map(|res| res.map(|startup| startup.clone()));
    match waited {
        Ok(Ok(Startup::Failed(reason))) => Err(AppError::Io(std::io::Error::other(reason))),
        Ok(Ok(_)) => Ok(()),
        Ok(Err(_)) => Err(AppError::Io(std::io::Error::other(
            "sidecar exited before announcing readiness",
        ))),
        Err(_) => Err(AppError::Io(std::io::Error::new(
            std::io::ErrorKind::TimedOut,
            "sidecar did not announce readiness within budget",
        ))),
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    #[cfg(unix)]
    use crate::testutil::{folders, mock_app, roots, Scratch};

    #[test]
    fn a_ready_line_marks_the_sidecar_ready() {
        let (tx, rx) = watch::channel(Startup::Pending);
        stdout_line(b"READY unix:/run/kubetail/s.sock\n", &tx);
        assert_eq!(*rx.borrow(), Startup::Ready);
    }

    /// The line reader keeps the newline; the start screen must not show it.
    #[test]
    fn a_fatal_line_carries_its_reason_without_the_newline() {
        let (tx, rx) = watch::channel(Startup::Pending);
        stdout_line(b"FATAL app.db was written by a newer Kubetail\n", &tx);
        assert_eq!(
            *rx.borrow(),
            Startup::Failed("app.db was written by a newer Kubetail".to_owned())
        );
    }

    #[test]
    fn any_other_line_leaves_startup_pending() {
        let (tx, rx) = watch::channel(Startup::Pending);
        stdout_line(b"panic: boom\n", &tx);
        assert_eq!(*rx.borrow(), Startup::Pending);
    }

    #[tokio::test(start_paused = true)]
    async fn wait_ready_returns_once_the_sidecar_is_ready() {
        let (tx, rx) = watch::channel(Startup::Pending);
        tx.send(Startup::Ready).expect("a receiver is open");

        wait_ready(rx).await.expect("ready");
    }

    /// The sidecar exits right after its `FATAL` line, so the reason must win
    /// over the closed channel.
    #[tokio::test(start_paused = true)]
    async fn wait_ready_returns_the_sidecars_reason() {
        let (tx, rx) = watch::channel(Startup::Pending);
        tx.send(Startup::Failed(
            "app.db was written by a newer Kubetail".to_owned(),
        ))
        .expect("a receiver is open");
        drop(tx);

        let err = wait_ready(rx).await.expect_err("a failed start");
        assert_eq!(err.to_string(), "app.db was written by a newer Kubetail");
    }

    #[tokio::test(start_paused = true)]
    async fn wait_ready_fails_when_the_sidecar_exits_unannounced() {
        let (tx, rx) = watch::channel(Startup::Pending);
        drop(tx);

        let err = wait_ready(rx).await.expect_err("an exit");
        assert_eq!(
            err.to_string(),
            "sidecar exited before announcing readiness"
        );
    }

    #[tokio::test(start_paused = true)]
    async fn wait_ready_times_out_on_a_silent_sidecar() {
        let (_tx, rx) = watch::channel(Startup::Pending);

        let err = wait_ready(rx).await.expect_err("a timeout");
        assert!(
            matches!(&err, AppError::Io(e) if e.kind() == std::io::ErrorKind::TimedOut),
            "got: {err:?}"
        );
    }

    /// Pins the host↔sidecar CLI contract (`--socket`, the runtime directory,
    /// `--host-pid`, and the endpoints); changing it without updating the
    /// sidecar silently misroutes the socket, drops the peer check, or leaves
    /// sign-in on an inherited environment variable. The sidecar places every
    /// other folder itself; `sidecar/internal/app/layout/testdata/layouts.json` pins where.
    #[test]
    fn cmd_args_passes_the_runtime_directory_host_pid_and_endpoints() {
        let base = std::env::temp_dir();
        let path = Endpoint::pick(&base).expect("pick should succeed");
        let runtime = std::path::PathBuf::from("/some/app/run");
        let mut want = vec![
            "--socket".to_owned(),
            path.as_arg().to_owned(),
            "--runtime-dir".to_owned(),
            "/some/app/run".to_owned(),
            "--host-pid".to_owned(),
            std::process::id().to_string(),
            "--oauth-issuer".to_owned(),
            OAUTH_ISSUER.to_owned(),
            "--oauth-client-id".to_owned(),
            OAUTH_CLIENT_ID.to_owned(),
            "--keychain-service".to_owned(),
            KEYCHAIN_SERVICE.to_owned(),
            format!("--dev={}", cfg!(debug_assertions)),
        ];
        // Debug builds put the sidecar's records on the terminal beside the
        // host's; a release build's stderr stays quiet.
        if cfg!(debug_assertions) {
            want.push("--log-stderr".to_owned());
        }
        let want: Vec<OsString> = want.into_iter().map(OsString::from).collect();
        assert_eq!(cmd_args(&path, &runtime), want);
    }

    /// Linux paths are bytes, not UTF-8. The sidecar must fence the directory
    /// the host writes, not a lossy spelling of it.
    #[cfg(unix)]
    #[test]
    fn cmd_args_keeps_a_path_that_is_not_utf8() {
        use std::os::unix::ffi::OsStringExt;

        let base = std::env::temp_dir();
        let path = Endpoint::pick(&base).expect("pick should succeed");
        let runtime = OsString::from_vec(b"/run/user/1/\xffkubetail".to_vec());
        let args = cmd_args(&path, std::path::Path::new(&runtime));
        let at = args.iter().position(|a| a == "--runtime-dir").unwrap();
        assert_eq!(args[at + 1], runtime);
    }

    #[test]
    fn only_a_zero_code_with_no_signal_is_clean() {
        assert!(exited_cleanly(Some(0), None));
        assert!(!exited_cleanly(Some(1), None));
        assert!(!exited_cleanly(None, Some(9)));
        assert!(!exited_cleanly(Some(0), Some(15)));
        assert!(!exited_cleanly(None, None));
    }

    /// Spawns `sh -c script` as the sidecar, returning the service and the
    /// arguments it was handed.
    #[cfg(unix)]
    fn spawn_script(
        app: &tauri::App<tauri::test::MockRuntime>,
        folders: &Folders,
        script: &str,
    ) -> (SidecarService, Vec<OsString>) {
        use tauri_plugin_shell::ShellExt;

        let mut handed = Vec::new();
        let service = SidecarService::spawn(folders, |args| {
            handed = args;
            Ok(app.shell().command("sh").args(["-c", script]))
        })
        .unwrap();
        (service, handed)
    }

    /// The drain task reads the readiness line and forwards stderr; closing
    /// stdin is the stop, and the exit it brings is seen.
    #[cfg(unix)]
    #[tokio::test]
    async fn a_spawned_sidecar_announces_ready_and_stops_on_eof() {
        let scratch = Scratch::new();
        let app = mock_app(scratch.path(), SidecarService::without_process(None));
        let (service, _) = spawn_script(
            &app,
            &folders(scratch.path()),
            "echo 'READY x'; echo 'a log line' >&2; cat >/dev/null",
        );

        service.ready().await.unwrap();
        assert!(service.graceful_shutdown(Duration::from_secs(10)));
        assert!(service.graceful_shutdown(Duration::ZERO), "already stopped");
        assert!(!service.kill(), "an exited sidecar is not killed");
    }

    #[cfg(unix)]
    #[tokio::test]
    async fn a_sidecar_that_exits_before_ready_fails_startup() {
        let scratch = Scratch::new();
        let app = mock_app(scratch.path(), SidecarService::without_process(None));
        let (service, _) = spawn_script(&app, &folders(scratch.path()), "exit 3");

        let err = service.ready().await.unwrap_err();
        assert!(err.to_string().contains("exited before"), "got: {err}");
    }

    #[cfg(unix)]
    #[test]
    fn kill_ends_a_running_sidecar_through_its_handle() {
        let scratch = Scratch::new();
        let app = mock_app(scratch.path(), SidecarService::without_process(None));
        let (service, _) = spawn_script(&app, &folders(scratch.path()), "read _");

        assert!(service.kill());
        assert!(!service.kill(), "idempotent");
    }

    /// The sidecar places and fences every folder itself, and the host
    /// makes none of the sidecar's: it is handed the runtime directory alone,
    /// which a squatted name can move, and no log file.
    #[cfg(unix)]
    #[test]
    fn the_sidecar_is_handed_no_folder_but_the_runtime_directory() {
        let scratch = Scratch::new();
        let app = mock_app(scratch.path(), SidecarService::without_process(None));
        let roots = roots(scratch.path());
        let folders = Folders::make(&roots).unwrap();

        let (service, args) = spawn_script(&app, &folders, "exit 0");

        let flags: Vec<_> = args
            .iter()
            .filter_map(|a| a.to_str()?.strip_suffix("-dir"))
            .collect();
        assert_eq!(flags, ["--runtime"]);
        assert!(!args.iter().any(|a| a == "--log-file"), "got: {args:?}");
        let layout = crate::paths::current_layout(&roots);
        assert!(!layout.data.exists() && !layout.cache.exists());
        let runtime = args
            .iter()
            .position(|a| a == "--runtime-dir")
            .map(|i| args[i + 1].as_os_str());
        assert_eq!(
            runtime,
            Some(folders.runtime.as_os_str()),
            "as runtime_dir names it"
        );
        service.kill();
    }

    /// A pipe read error is logged; it neither readies nor ends the sidecar.
    #[test]
    fn a_stream_error_changes_nothing() {
        let state = Arc::new(Mutex::new(State {
            child: None,
            pid: Some(1),
            exited: false,
        }));
        let (startup_tx, startup_rx) = watch::channel(Startup::Pending);
        let (expect_tx, expect_rx) = watch::channel(Some(1));
        let (exit_tx, exit_rx) = sync_channel(1);
        let drain = Drain {
            state: Arc::clone(&state),
            startup_tx,
            expect_tx,
            exit_tx,
        };

        drain.on_event(CommandEvent::Error("bad utf-8".into()));

        assert_eq!(*startup_rx.borrow(), Startup::Pending);
        assert_eq!(*expect_rx.borrow(), Some(1));
        assert!(exit_rx.try_recv().is_err());
        assert!(!state.lock().unwrap().exited);
    }

    #[cfg(windows)]
    #[test]
    fn force_kill_by_pid_ends_a_running_process() {
        let mut child = std::process::Command::new("ping")
            .args(["-n", "600", "127.0.0.1"])
            .stdout(std::process::Stdio::null())
            .spawn()
            .unwrap();

        force_kill_by_pid(child.id());

        // TerminateProcess's exit code; ping's own would be 0.
        assert_eq!(child.wait().unwrap().code(), Some(1));
    }

    /// A pid naming no process is ignored.
    #[cfg(windows)]
    #[test]
    fn force_kill_by_pid_ignores_a_pid_with_no_process() {
        // Windows pids are small multiples of four; none is this large.
        force_kill_by_pid(u32::MAX - 3);
    }
}
