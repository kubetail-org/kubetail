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

//! The Tauri host crate: windows, tray/menus, the Go sidecar's lifecycle, and
//! the webview→sidecar GraphQL bridge over an AF_UNIX socket.
//!
//! [`run`] (`app.rs`, the wiring) is the single entry point — `main.rs` (and the
//! mobile entry point) call it. Module map: [`app_menu`], [`commands`],
//! [`dock_menu`], [`error`], [`services`] (`SidecarService`), [`state`]
//! ([`AppState`]), [`quit`], [`tray`], [`window_manager`].

#![warn(clippy::unwrap_used)]
// `unwrap()` is idiomatic in tests.
#![cfg_attr(test, allow(clippy::unwrap_used))]

mod app;
mod app_menu;
mod cloud;
mod commands;
mod dock_menu;
mod error;
mod host_file;
mod log_rotate;
mod log_windows;
mod logging;
mod os_theme;
mod paths;
mod quit;
mod services;
mod state;
mod tray;
mod updater;
mod wake;
mod webview_log;
#[cfg(not(target_os = "macos"))]
mod webview_profile;
mod window_manager;
mod window_state;
mod window_title;

#[cfg(test)]
mod testutil;

use std::time::Duration;

use tauri::ipc::Invoke;
use tauri::{AppHandle, Manager, Runtime};

use crate::quit::ExitDecision;
use crate::state::AppState;

pub use app::run;

/// Quit-path wait before `RunEvent::Exit` force-kills the sidecar: the
/// sidecar's `shutdownBudget` (20s, `sidecar/shutdown.go`) plus the time a
/// goroutine dump and its exit take, so its watchdog, not this kill, ends a
/// wedged stop.
const SIDECAR_SHUTDOWN_GRACE: Duration = Duration::from_secs(23);

#[cfg(all(desktop, unix))]
type TerminationSignals = [tokio::signal::unix::Signal; 3];

/// Takes over `SIGTERM`, `SIGINT` and `SIGHUP`: from here on they no longer
/// end the process, and [`first_signal`] reports them.
#[cfg(all(desktop, unix))]
fn termination_signals() -> std::io::Result<TerminationSignals> {
    use tokio::signal::unix::{signal, SignalKind};

    Ok([
        signal(SignalKind::terminate())?,
        signal(SignalKind::interrupt())?,
        signal(SignalKind::hangup())?,
    ])
}

/// The name of the first termination signal received, or `None` once
/// `shutdown` is cancelled.
#[cfg(all(desktop, unix))]
async fn first_signal(
    signals: &mut TerminationSignals,
    shutdown: &tokio_util::sync::CancellationToken,
) -> Option<&'static str> {
    let [term, int, hup] = signals;
    tokio::select! {
        // A quit already under way wins over a signal.
        biased;
        _ = shutdown.cancelled() => None,
        _ = term.recv() => Some("SIGTERM"),
        _ = int.recv() => Some("SIGINT"),
        _ = hup.recv() => Some("SIGHUP"),
    }
}

/// `ExitRequested`'s answer to `code`: whether to prevent the exit.
///
/// No code means the last window closed, and the app stays in the background.
/// A code is a Quit: the first starts the graceful shutdown, those during it
/// are prevented, and the one [`start_quit`] makes at its end goes through.
fn exit_requested<R: Runtime>(
    app: &AppHandle<R>,
    code: Option<i32>,
    exit: impl FnOnce(i32) + Send + 'static,
) -> bool {
    let Some(code) = code else {
        return true;
    };
    let state = app.state::<AppState>();
    match state.quit.request() {
        ExitDecision::Start => {
            tracing::info!(code, "exit requested; initiating graceful shutdown");
            // Cancel app-lifetime background tasks first so they don't retry
            // against a dying sidecar. Also stops any window from opening.
            state.shutdown.cancel();
            start_quit(app, code, exit);
            true
        }
        ExitDecision::Prevent => true,
        ExitDecision::Allow => false,
    }
}

/// Hides every window and waits for the sidecar on a thread of its own, then
/// calls `exit` with `code`. The wait can take the whole grace, and on the
/// event loop it would be a frozen app with its windows open.
fn start_quit<R: Runtime>(app: &AppHandle<R>, code: i32, exit: impl FnOnce(i32) + Send + 'static) {
    app.state::<AppState>().window_manager.hide_all(app);
    let app = app.clone();
    std::thread::spawn(move || {
        stop_sidecar(&app.state::<AppState>());
        exit(code);
    });
}

/// Waits for the sidecar to stop, then lets the next exit request through.
fn stop_sidecar(state: &AppState) {
    if !state.sidecar.graceful_shutdown(SIDECAR_SHUTDOWN_GRACE) {
        tracing::warn!("sidecar did not exit within grace period");
    }
    state.quit.finish();
}

/// A quit's stop, run on the caller's thread, for the updater: before the
/// restart it requests, whose `ExitRequested` cannot be prevented, and on
/// Windows before the installer, which ends the process itself. Returns
/// whether it stopped the sidecar; a quit already under way is stopping it.
/// Linux runs no updater.
#[cfg_attr(target_os = "linux", allow(dead_code))]
fn stop_for_install<R: Runtime>(app: &AppHandle<R>) -> bool {
    let state = app.state::<AppState>();
    if state.quit.request() != ExitDecision::Start {
        return false;
    }
    tracing::info!("installing an update; stopping the sidecar");
    state.shutdown.cancel();
    state.window_manager.hide_all(app);
    stop_sidecar(&state);
    true
}

/// `RunEvent::Exit`: kills a sidecar the graceful path left running, then
/// flushes the log.
fn exited<R: Runtime>(app: &AppHandle<R>) {
    let state = app.state::<AppState>();
    if state.sidecar.kill() {
        tracing::warn!("sidecar was still running at exit; killed it");
    }

    state.window_manager.save_frames();

    tracing::info!("shutdown complete");

    // Last: the process exits without dropping managed state, so nothing after
    // this reaches the log file.
    state.flush_log();
}

/// Drops every subscription a webview opened. A reload replaces the page's JS
/// without running any teardown, and a closed window runs none either, so
/// without this the host keeps streaming SSE frames at a webview that can no
/// longer route them — leaking a sidecar connection and a cluster watch per
/// subscription, and (on reload) filling the console with the Tauri runtime's
/// "Couldn't find callback id N" for every frame.
fn cancel_webview_subscriptions<R: Runtime>(app: &AppHandle<R>, label: &str) {
    // `try_state`: the first page load can beat `setup`'s `manage`.
    if let Some(state) = app.try_state::<AppState>() {
        state.sidecar.cancel_webview(label);
    }
}

/// Forgets a destroyed webview's log pacing, reporting what it withheld.
fn forget_webview_log<R: Runtime>(app: &AppHandle<R>, label: &str) {
    if let Some(state) = app.try_state::<AppState>() {
        webview_log::destroyed(&state.webview_log, label);
    }
}

/// A navigation began in `label`'s webview: a load, or a reload, which runs no
/// JS teardown.
fn page_started<R: Runtime>(app: &AppHandle<R>, label: &str) {
    cancel_webview_subscriptions(app, label);
    if let Some(state) = app.try_state::<AppState>() {
        state.log_windows.page_started(label);
    }
    // A reload keeps the label and replaces the JS context; the marker is how
    // two crashes across it read as two.
    webview_log::page_loaded(label);
}

/// `label`'s window was destroyed: its subscriptions and log pacing go, and so
/// do its log windows, or its own entry if it is one.
fn window_destroyed<R: Runtime>(app: &AppHandle<R>, label: &str) {
    cancel_webview_subscriptions(app, label);
    forget_webview_log(app, label);
    log_windows::destroyed(app, label);
    if let Some(state) = app.try_state::<AppState>() {
        state.window_manager.window_destroyed(label);
    }
}

/// `label`'s window moved or resized.
fn window_moved<R: Runtime>(app: &AppHandle<R>, label: &str) {
    if let Some(state) = app.try_state::<AppState>() {
        state.window_manager.window_moved(app, label);
    }
}

/// `label`'s window took focus.
fn window_focused<R: Runtime>(app: &AppHandle<R>, label: &str) {
    if let Some(state) = app.try_state::<AppState>() {
        state.log_windows.focused(label);
        state.window_manager.window_focused(app, label);
    }
}

/// A second launch forwarded here: surface the main window. One that arrives
/// before `setup` manages the state is dropped, since `setup` shows the window.
#[cfg(desktop)]
fn second_instance_launched<R: Runtime>(app: &AppHandle<R>) -> error::Result<()> {
    match app.try_state::<AppState>() {
        Some(state) => state.window_manager.show_main_window(app),
        None => Ok(()),
    }
}

/// Every command the webview may invoke. `build.rs`'s `COMMANDS` names the
/// same set, so each has a permission a capability can grant.
fn invoke_handler<R: Runtime>() -> impl Fn(Invoke<R>) -> bool + Send + Sync + 'static {
    tauri::generate_handler![
        commands::ready,
        commands::graphql_query,
        commands::graphql_subscribe,
        commands::graphql_unsubscribe,
        commands::new_window,
        commands::update_host_file,
        commands::read_host_file,
        app::quit,
        commands::webview_record_error,
        app::open_account_url,
        commands::log_window_subscribe,
        commands::log_window_open,
        commands::log_window_focus,
        commands::log_window_close_all,
        commands::log_window_view,
        commands::log_window_viewport,
        commands::log_window_title,
        commands::log_window_show_chat,
    ]
}

#[cfg(test)]
mod tests {
    use std::sync::mpsc;

    use tauri::Manager;

    use super::*;
    use crate::logging::{captured, file_layer, FileLog};
    use crate::services::sidecar::SidecarService;
    use crate::testutil::{mock_app, Scratch};
    use crate::webview_log::{Limiter, LimiterConfig, WebviewRecord};

    /// The items of the `[...]` list that opens at the first `marker` in `source`.
    fn items_after<'a>(source: &'a str, marker: &str) -> Vec<&'a str> {
        let (_, after) = source.split_once(marker).expect("source has the list");
        let (list, _) = after.split_once(']').expect("the list closes");
        list.split(',')
            .map(str::trim)
            .filter(|item| !item.is_empty())
            .collect()
    }

    #[test]
    fn every_command_is_in_the_app_manifest() {
        // A command left out of `build.rs`'s `COMMANDS` has no permission for a
        // capability to grant, so Tauri denies it in every window. The mock
        // runtime checks no capability, so read both lists from source.
        // `generate_handler!` names paths (`commands::ready`); the command is
        // the last segment. This test's own literals come after the handler,
        // so the first match is the list.
        let mut handled: Vec<&str> = items_after(include_str!("lib.rs"), "generate_handler![")
            .into_iter()
            .map(|path| path.rsplit("::").next().unwrap())
            .collect();
        let mut manifest: Vec<&str> =
            items_after(include_str!("../build.rs"), "COMMANDS: &[&str] = &[")
                .into_iter()
                .map(|name| name.trim_matches('"'))
                .collect();
        handled.sort_unstable();
        manifest.sort_unstable();
        assert_eq!(
            handled, manifest,
            "build.rs's COMMANDS must name every handler and nothing else"
        );
    }

    /// The installed app, its packages and every file they install take the
    /// Kubetail Desktop names. Tauri names the deb's `Package:` and the rpm's
    /// `Name:` after `productName`, so Linux has its own.
    #[test]
    fn the_bundle_takes_the_kubetail_desktop_names() {
        let config: serde_json::Value =
            serde_json::from_str(include_str!("../tauri.conf.json")).unwrap();
        assert_eq!(config["productName"], "Kubetail");
        assert_eq!(config["identifier"], "com.kubetail.desktop");
        assert_eq!(
            config["bundle"]["externalBin"][0],
            "binaries/kubetail-sidecar"
        );

        let linux: serde_json::Value =
            serde_json::from_str(include_str!("../tauri.linux.conf.json")).unwrap();
        assert_eq!(linux["productName"], "kubetail-desktop");
        let roots = [
            "/usr/lib/kubetail-desktop/",
            "/usr/share/doc/kubetail-desktop/",
            "/usr/share/licenses/kubetail-desktop/",
        ];
        for format in ["deb", "rpm"] {
            let package = &linux["bundle"]["linux"][format];
            assert_eq!(package["desktopTemplate"], "linux/kubetail-desktop.desktop");
            for dest in package["files"].as_object().unwrap().keys() {
                assert!(
                    roots.iter().any(|root| dest.starts_with(root)),
                    "{format} installs {dest}"
                );
            }
        }
        let deb = &linux["bundle"]["linux"]["deb"]["files"];
        for profile in ["kubetail-desktop-bwrap", "kubetail-desktop-pasta"] {
            assert_eq!(
                deb[format!("/usr/lib/kubetail-desktop/apparmor/{profile}")],
                format!("linux/{profile}")
            );
        }
        assert!(include_str!("../linux/kubetail-desktop.desktop")
            .lines()
            .any(|line| line == "Name=Kubetail"));
    }

    #[test]
    fn closing_the_last_window_keeps_the_app_running() {
        let scratch = Scratch::new();
        let app = mock_app(scratch.path(), SidecarService::without_process(None));

        assert!(exit_requested(app.handle(), None, |_| panic!("no exit")));
        assert!(!app.state::<AppState>().shutdown.is_cancelled());
    }

    /// The first Quit is held while the sidecar stops; the exit it makes at the
    /// end carries its code and goes through.
    #[test]
    fn a_quit_stops_the_sidecar_then_exits_with_its_code() {
        let scratch = Scratch::new();
        let app = mock_app(scratch.path(), SidecarService::without_process(None));
        let (tx, rx) = mpsc::channel();

        let prevented = exit_requested(app.handle(), Some(3), move |code| tx.send(code).unwrap());

        assert!(prevented);
        assert!(app.state::<AppState>().shutdown.is_cancelled());
        assert_eq!(rx.recv().unwrap(), 3);
        assert!(!exit_requested(app.handle(), Some(3), |_| panic!(
            "no exit"
        )));
    }

    /// A running sidecar is stopped before the exit, not killed after it.
    #[cfg(unix)]
    #[test]
    fn a_quit_waits_for_a_running_sidecar_to_stop() {
        let scratch = Scratch::new();
        let sidecar = crate::testutil::script_sidecar(scratch.path(), "cat >/dev/null");
        let app = mock_app(scratch.path(), sidecar);
        let (tx, rx) = mpsc::channel();

        start_quit(app.handle(), 0, move |code| tx.send(code).unwrap());

        assert_eq!(rx.recv().unwrap(), 0);
        assert!(!app.state::<AppState>().sidecar.kill(), "it had exited");
    }

    #[test]
    fn a_quit_while_one_is_under_way_is_prevented() {
        let scratch = Scratch::new();
        let app = mock_app(scratch.path(), SidecarService::without_process(None));
        let state = app.state::<AppState>();
        state.quit.request();

        assert!(exit_requested(app.handle(), Some(0), |_| panic!("no exit")));
        // Only the first Quit starts anything.
        assert!(!state.shutdown.is_cancelled());
    }

    /// The install's stop waits for the sidecar on the calling thread, and the
    /// exit after it goes through.
    #[cfg(unix)]
    #[test]
    fn an_install_stops_the_sidecar_before_it_returns() {
        let scratch = Scratch::new();
        let sidecar = crate::testutil::script_sidecar(scratch.path(), "cat >/dev/null");
        let app = mock_app(scratch.path(), sidecar);
        let state = app.state::<AppState>();

        assert!(stop_for_install(app.handle()));

        assert!(state.shutdown.is_cancelled());
        assert!(!state.sidecar.kill(), "it had exited");
        assert!(!exit_requested(app.handle(), Some(0), |_| panic!(
            "no exit"
        )));
    }

    #[test]
    fn an_install_during_a_quit_leaves_the_stop_to_the_quit() {
        let scratch = Scratch::new();
        let app = mock_app(scratch.path(), SidecarService::without_process(None));
        let state = app.state::<AppState>();
        state.quit.request();

        assert!(!stop_for_install(app.handle()));
        assert!(!state.shutdown.is_cancelled());
    }

    #[test]
    fn the_exit_flushes_the_log() {
        let scratch = Scratch::new();
        let app = mock_app(scratch.path(), SidecarService::without_process(None));
        let FileLog { guards, .. } = file_layer(scratch.path()).unwrap();
        let state = app.state::<AppState>();
        *state.log_guards.lock().unwrap() = guards;

        exited(app.handle());

        assert!(state.log_guards.lock().unwrap().is_empty());
    }

    /// A sidecar the graceful path left running is killed, and the kill is
    /// logged. The "sidecar" is a `sleep` this test starts, so the kill lands
    /// on nothing else.
    #[cfg(unix)]
    #[test]
    fn the_exit_kills_a_sidecar_still_running() {
        use std::os::unix::process::ExitStatusExt;

        let mut child = std::process::Command::new("sleep")
            .arg("60")
            .spawn()
            .unwrap();
        let scratch = Scratch::new();
        let app = mock_app(scratch.path(), SidecarService::running_as(child.id()));

        let out = captured(|| exited(app.handle()));

        assert!(
            out.contains("sidecar was still running at exit"),
            "got: {out}"
        );
        assert!(
            child.wait().unwrap().signal().is_some(),
            "killed by a signal"
        );
    }

    /// What a destroyed webview withheld is reported, and its pacing starts
    /// over if the label comes back.
    #[test]
    fn a_destroyed_webview_is_forgotten() {
        let scratch = Scratch::new();
        let app = mock_app(scratch.path(), SidecarService::without_process(None));
        let state = app.state::<AppState>();
        *state.webview_log.lock().unwrap() = Limiter::new(LimiterConfig {
            burst: 1,
            error_reserve: 0,
            ..LimiterConfig::DEFAULT
        });
        let record = |message: &str| WebviewRecord {
            source: "graphql".into(),
            message: message.into(),
            stack: None,
            component_stack: None,
            context: None,
        };
        for message in ["one", "two"] {
            let now = std::time::Instant::now();
            webview_log::record_from(&state.webview_log, "main", record(message), now, |_| true);
        }

        let out = captured(|| {
            cancel_webview_subscriptions(app.handle(), "main");
            forget_webview_log(app.handle(), "main");
        });

        assert!(out.contains("dropped=1"), "got: {out}");
        assert_eq!(state.webview_log.lock().unwrap().evict("main"), 0);
    }

    /// An app window's destroy removes its live log windows. The mock runtime
    /// keeps a destroyed window listed until its event loop runs, so this reads
    /// the pair.
    #[test]
    fn an_app_windows_destroy_takes_its_log_windows() {
        let scratch = Scratch::new();
        let app = mock_app(scratch.path(), SidecarService::without_process(None));
        let state = app.state::<AppState>();
        let channel = tauri::ipc::Channel::new(|_| Ok(()));
        let page = state.log_windows.subscribe("main", channel).unwrap();
        crate::log_windows::live_for_test(&state.log_windows, page, "logs-1");
        crate::testutil::window(&app, "logs-1");

        window_destroyed(app.handle(), "main");

        assert!(state.log_windows.view("logs-1").is_err());
    }

    /// The main window's last frame is saved when it is destroyed, so a
    /// rebuild or the next launch finds it.
    #[test]
    fn a_windows_frame_is_saved_when_it_is_destroyed() {
        use crate::window_state::read_main;

        let scratch = Scratch::new();
        let app = mock_app(scratch.path(), SidecarService::without_process(None));
        let path = scratch.path().join("window-state.json");
        crate::testutil::window(&app, "main");

        crate::testutil::window(&app, "logs-1");

        window_moved(app.handle(), "main");
        window_moved(app.handle(), "logs-1");
        window_destroyed(app.handle(), "window-1");
        assert!(read_main(&path).is_none(), "keeps no frame");

        window_destroyed(app.handle(), "logs-1");
        assert!(read_main(&path).is_none(), "not the main window");

        window_destroyed(app.handle(), "main");
        assert!(read_main(&path).is_some());
    }

    /// A live log window's focus and page load reach its app window.
    #[test]
    fn a_log_windows_focus_and_page_load_reach_its_app_window() {
        let scratch = Scratch::new();
        let app = mock_app(scratch.path(), SidecarService::without_process(None));
        let state = app.state::<AppState>();
        let (tx, rx) = mpsc::channel();
        let channel = tauri::ipc::Channel::new(move |body| {
            let frame: serde_json::Value = body.deserialize().unwrap();
            tx.send(frame.as_object().unwrap().keys().next().unwrap().clone())
                .unwrap();
            Ok(())
        });
        let page = state.log_windows.subscribe("main", channel).unwrap();
        crate::log_windows::live_for_test(&state.log_windows, page, "logs-1");

        page_started(app.handle(), "logs-1");
        window_focused(app.handle(), "logs-1");

        let frames: Vec<String> = rx.try_iter().collect();
        assert_eq!(frames, ["snapshot", "opened", "viewport", "focused"]);
    }

    /// The first page load can beat `setup`, so it finds no state to touch.
    #[test]
    fn a_page_load_before_setup_is_still_marked() {
        let app = tauri::test::mock_app();

        let out = captured(|| page_started(app.handle(), "main"));
        forget_webview_log(app.handle(), "main");

        assert!(out.contains("window=\"main\""), "got: {out}");
    }

    #[cfg(desktop)]
    #[test]
    fn a_second_launch_before_setup_is_ignored() {
        let app = tauri::test::mock_app();

        second_instance_launched(app.handle()).unwrap();

        assert!(app.get_webview_window("main").is_none());
    }

    #[cfg(desktop)]
    #[test]
    fn a_second_launch_surfaces_the_main_window() {
        let scratch = Scratch::new();
        let app = mock_app(scratch.path(), SidecarService::without_process(None));

        second_instance_launched(app.handle()).unwrap();

        assert!(app.get_webview_window("main").is_some());
    }

    /// Registering takes the signals over first, so the one sent here reaches
    /// the stream rather than ending the test process.
    #[cfg(all(desktop, unix))]
    #[tokio::test]
    async fn a_termination_signal_is_named() {
        let mut signals = termination_signals().unwrap();
        let shutdown = tokio_util::sync::CancellationToken::new();

        // SAFETY: `kill` on this process's own pid only delivers the signal.
        assert_eq!(unsafe { libc::kill(libc::getpid(), libc::SIGHUP) }, 0);

        assert_eq!(first_signal(&mut signals, &shutdown).await, Some("SIGHUP"));
    }

    #[cfg(all(desktop, unix))]
    #[tokio::test]
    async fn a_quit_under_way_wins_over_a_signal() {
        let mut signals = termination_signals().unwrap();
        let shutdown = tokio_util::sync::CancellationToken::new();
        shutdown.cancel();

        assert_eq!(first_signal(&mut signals, &shutdown).await, None);
    }
}
