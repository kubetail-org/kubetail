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

use std::path::PathBuf;
use std::sync::{Arc, Mutex};

use tokio_util::sync::CancellationToken;
use tracing_appender::non_blocking::WorkerGuard;

use crate::alerts::PermissionWatch;
#[cfg(not(target_os = "macos"))]
use crate::alerts::PluginNotices;
use crate::log_windows::LogWindows;
use crate::quit::Quit;
use crate::services::sidecar::SidecarService;
use crate::tray::TraySnapshots;
use crate::webview_log::{Limiter, LimiterConfig};
use crate::window_manager::{Monitors, WindowManager};

pub struct AppState {
    pub sidecar: SidecarService,
    pub window_manager: WindowManager,
    /// Latest-state holder for the tray's account watch stream, written by
    /// `tray::watch_auth_state`.
    pub tray: Arc<Mutex<TraySnapshots>>,
    /// App-wide graceful-shutdown signal, cancelled once on Quit *before* the
    /// sidecar is torn down. Every app-lifetime background task (tray/wake
    /// supervisors, signal handler) must hold a clone and select on
    /// [`CancellationToken::cancelled`] so it doesn't retry against a dying
    /// sidecar.
    pub shutdown: CancellationToken,
    /// Where a quit is: `ExitRequested` starts the wait once and prevents every
    /// request during it.
    pub quit: Quit,
    /// The log files' worker-thread guards, taken by [`AppState::flush_log`].
    /// Empty when no log file could be opened.
    pub log_guards: Mutex<Vec<WorkerGuard>>,
    /// Per-webview pacing of `webview_record_error`, keyed by label; entries are
    /// evicted on `WindowEvent::Destroyed`.
    pub webview_log: Mutex<Limiter>,
    /// The log windows and the app windows that opened them.
    pub log_windows: LogWindows,
    /// Whether the OS shows the alerts' notifications, as each Monitoring tab
    /// reads it.
    pub notification_permission: PermissionWatch,
    /// The notices `tauri-plugin-notification` showed, which its presses name.
    #[cfg(not(target_os = "macos"))]
    pub plugin_notices: PluginNotices,
}

impl AppState {
    /// The state `setup` manages: a fresh shutdown signal shared with the
    /// window manager, and the default log pacing.
    pub fn new(
        sidecar: SidecarService,
        log_guards: Vec<WorkerGuard>,
        monitors: impl Monitors + 'static,
        window_state_path: Option<PathBuf>,
    ) -> Self {
        let shutdown = CancellationToken::new();
        Self {
            sidecar,
            window_manager: WindowManager::new(shutdown.clone(), monitors, window_state_path),
            tray: Arc::new(Mutex::new(TraySnapshots::default())),
            shutdown,
            quit: Quit::default(),
            log_guards: Mutex::new(log_guards),
            webview_log: Mutex::new(Limiter::new(LimiterConfig::DEFAULT)),
            log_windows: LogWindows::default(),
            notification_permission: PermissionWatch::default(),
            #[cfg(not(target_os = "macos"))]
            plugin_notices: PluginNotices::default(),
        }
    }

    /// Flushes the log files' worker threads, and must be called on the way out.
    ///
    /// Tauri's `App::run` ends the process with `std::process::exit`, so no
    /// managed state is ever dropped — a guard's own `Drop` never runs, and
    /// records still queued (the last of them the shutdown line itself) go with
    /// the process. Idempotent: the guards are taken, so a second call is a
    /// no-op.
    pub fn flush_log(&self) {
        drop(std::mem::take(
            &mut *self
                .log_guards
                .lock()
                .unwrap_or_else(|poisoned| poisoned.into_inner()),
        ));
    }
}

#[cfg(test)]
mod tests {
    use tauri::Manager;
    use tracing_subscriber::layer::SubscriberExt;

    use super::AppState;
    use crate::logging::{file_layer, FileLog};
    use crate::services::sidecar::SidecarService;
    use crate::testutil::{mock_app, OneMonitor, Scratch};

    /// The guards are the only flush there is: the process never drops them.
    #[test]
    fn flush_log_writes_what_is_queued_and_takes_the_guards() {
        let scratch = Scratch::new();
        let FileLog { layer, guards } = file_layer(scratch.path()).unwrap();
        let state = AppState::new(
            SidecarService::without_process(None),
            guards,
            OneMonitor(None),
            None,
        );
        let subscriber = tracing_subscriber::registry().with(layer);
        tracing::subscriber::with_default(subscriber, || tracing::info!("the last line"));

        state.flush_log();

        let log = std::fs::read_to_string(scratch.path().join("main.log")).unwrap();
        assert!(log.contains("the last line"), "got: {log}");
        assert!(state.log_guards.lock().unwrap().is_empty());
        // Idempotent.
        state.flush_log();
    }

    /// One signal: a quit that cancels it also stops windows from opening.
    #[test]
    fn the_window_manager_shares_the_shutdown_signal() {
        let scratch = Scratch::new();
        let app = mock_app(scratch.path(), SidecarService::without_process(None));
        let state = app.state::<AppState>();

        state.shutdown.cancel();

        state.window_manager.show_main_window(app.handle()).unwrap();
        assert!(app.webview_windows().is_empty());
    }
}
