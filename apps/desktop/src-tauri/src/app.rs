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

//! The app's wiring: what only the real app runs, on the real runtime, with a
//! real sidecar and a real exit. Every decision it makes is a function in
//! `lib.rs` or the module it belongs to, tested there; this file only connects
//! them. Coverage counts the file; each item is marked with what it needs that
//! no test has.

use std::io::IsTerminal;

use tauri::webview::PageLoadEvent;
use tauri::window::Monitor;
use tauri::{AppHandle, Manager, RunEvent, Runtime, WindowEvent};
use tauri_plugin_shell::ShellExt;
use tracing_subscriber::util::SubscriberInitExt;

use crate::paths::{Folders, Roots};
use crate::services::sidecar::SidecarService;
use crate::state::AppState;
use crate::window_manager::Monitors;
use crate::{
    cloud, exit_requested, exited, host_file, invoke_handler, logging, page_started,
    second_instance_launched, tray, wake, window_destroyed, window_focused, window_moved,
    window_state,
};

// coverage:ignore-start AppHandle::exit is unimplemented on the mock runtime
/// Quits via [`AppHandle::exit`] so the `RunEvent::ExitRequested` teardown
/// runs — the same path as the tray and macOS-menu "Quit".
#[tauri::command]
pub fn quit<R: Runtime>(app: AppHandle<R>) {
    app.exit(0);
}
// coverage:ignore-stop

// coverage:ignore-start opens the user's browser
/// Opens the account page in the user's browser — the webview's avatar menu, the
/// same page the tray's account item opens.
///
/// Takes no URL. The webview holds the full cluster surface, so the destination
/// is the host's to name; a command that opened whatever it was handed would be
/// an exfiltration channel for anything executing in there.
#[tauri::command]
pub fn open_account_url<R: Runtime>(app: AppHandle<R>) -> crate::error::Result<()> {
    use tauri_plugin_opener::OpenerExt;
    app.opener().open_url(cloud::account_url(), None::<&str>)?;
    Ok(())
}
// coverage:ignore-stop

// coverage:ignore-start needs a real window system's monitors
/// The real runtime's monitors, which the window manager places windows on.
struct AppMonitors(AppHandle);

impl Monitors for AppMonitors {
    fn of_window(&self, label: &str) -> Option<Monitor> {
        self.0
            .get_webview_window(label)?
            .current_monitor()
            .ok()
            .flatten()
    }

    fn primary(&self) -> Option<Monitor> {
        self.0.primary_monitor().ok().flatten()
    }

    fn all(&self) -> Vec<Monitor> {
        self.0.available_monitors().unwrap_or_default()
    }
}
// coverage:ignore-stop

// coverage:ignore-start needs a real window system's tray
/// Builds and installs the system tray icon and menu. Menu callbacks can't
/// return errors, so failures are logged. Off macOS, panics if the app has no
/// default window icon.
fn build_tray(app: &AppHandle) -> crate::error::Result<()> {
    use tauri::tray::{TrayIconBuilder, TrayIconId};
    use tray::account_menu::ACCOUNT_SETTINGS_ID;
    use tray::{TrayAction, QUIT_ID, TRAY_ID};

    // Signed out; the account watch rebuilds it on its first frame.
    let menu = tray::current_tray_menu(app)?;

    let builder = TrayIconBuilder::with_id(TrayIconId::new(TRAY_ID)).menu(&menu);

    // A template image: macOS recolours it to suit a light or dark menu bar.
    #[cfg(target_os = "macos")]
    let builder = builder
        .icon(tauri::include_image!("icons/tray.png"))
        .icon_as_template(true);

    #[cfg(not(target_os = "macos"))]
    let builder = builder.icon(
        app.default_window_icon()
            .expect("missing default icon")
            .clone(),
    );

    builder
        .on_menu_event(|app, event| match event.id.as_ref() {
            ACCOUNT_SETTINGS_ID => {
                let app = app.clone();
                tauri::async_runtime::spawn(async move {
                    if let Err(err) = open_account_url(app) {
                        tracing::error!(%err, "failed to open account settings URL");
                    }
                });
            }
            QUIT_ID => app.exit(0),
            #[cfg(any(target_os = "macos", windows))]
            tray::UPDATE_ID => crate::updater::platform::install(app, rebuild_tray_menu),
            id => {
                if let Some(action) = TrayAction::from_id(id) {
                    tray::run_action(app, action);
                }
            }
        })
        .build(app)?;

    Ok(())
}
// coverage:ignore-stop

// coverage:ignore-start needs a real window system's tray
/// Swaps the tray's menu for one drawn from the latest account snapshot. muda
/// requires menu mutation on the main thread, hence
/// [`AppHandle::run_on_main_thread`].
fn rebuild_tray_menu(app: &AppHandle) {
    let menu_app = app.clone();
    let queued = app.run_on_main_thread(move || {
        let Some(tray) = menu_app.tray_by_id(tray::TRAY_ID) else {
            return;
        };
        match tray::current_tray_menu(&menu_app) {
            Ok(menu) => {
                if let Err(err) = tray.set_menu(Some(menu)) {
                    tracing::error!(%err, "failed to set tray menu");
                }
            }
            Err(err) => tracing::error!(%err, "failed to build tray menu"),
        }
    });
    if let Err(err) = queued {
        tracing::error!(%err, "failed to schedule tray menu rebuild");
    }
}
// coverage:ignore-stop

// coverage:ignore-start starts the OS's wake sources on the real runtime
/// Starts the wake/network-return supervisor for the app's lifetime: the
/// platform's sources feed [`wake::supervise`].
fn spawn_wake_poke_supervisor(app: &AppHandle) {
    let app = app.clone();
    tauri::async_runtime::spawn(async move {
        let shutdown = app.state::<AppState>().shutdown.clone();
        let (tx, rx) = tokio::sync::mpsc::channel(wake::CHANNEL_DEPTH);

        // Sources own their teardown (Linux/Windows honor `shutdown`; macOS
        // leaks its passive observers). `app` lets macOS register on the main thread.
        wake::spawn_sources(&app, tx, shutdown);

        wake::supervise(app, rx, wake::DEBOUNCE_WINDOW).await;
    });
}
// coverage:ignore-stop

// coverage:ignore-start exits the real app on a real signal
/// Routes Unix termination signals (`SIGTERM`/`SIGINT`/`SIGHUP`) through
/// [`AppHandle::exit`] — the same graceful path as menu/tray "Quit". Tao
/// installs no signal handlers, so without this a signal skips the `RunEvent`
/// shutdown hooks.
///
/// Unix-only by design: Windows session-end arrives as `WM_ENDSESSION`, which
/// tao already routes to `RunEvent::Exit`; macOS system shutdown is caught in
/// `applicationShouldTerminate:` (see `dock_menu`).
#[cfg(all(desktop, unix))]
fn spawn_signal_handler(app: &AppHandle) {
    use crate::{first_signal, termination_signals};

    let app = app.clone();
    tauri::async_runtime::spawn(async move {
        // A Quit via another path cancels `shutdown`, ending this task.
        let shutdown = app.state::<AppState>().shutdown.clone();

        // A failed registration abandons signal handling entirely rather than
        // leaving a partial set wired up.
        let mut signals = match termination_signals() {
            Ok(signals) => signals,
            Err(err) => {
                tracing::error!(%err, "failed to install termination signal handlers");
                return;
            }
        };

        if let Some(received) = first_signal(&mut signals, &shutdown).await {
            tracing::info!(signal = received, "received termination signal; exiting");
            app.exit(0);
        }
    });
}
// coverage:ignore-stop

// coverage:ignore-start the real runtime's builder, event loop and exit
/// Builds, configures, and runs the Tauri application (the host's entry
/// point).
///
/// The event loop intercepts shutdown: closing the last window keeps the app
/// alive in the background; only an explicit Quit (or a Unix signal — see
/// [`spawn_signal_handler`]) exits and tears down the sidecar.
#[cfg_attr(mobile, tauri::mobile_entry_point)]
pub fn run() {
    // Lives here (not `main`) so it also covers the mobile entry point.
    let ansi = logging::use_ansi(
        std::env::var_os("NO_COLOR"),
        std::io::stderr().is_terminal(),
    );
    let (subscriber, log_reload) =
        logging::process_subscriber(std::io::stderr, ansi, logging::env_filter());
    subscriber.init();

    // Alerts raised before this are in the bell, not notified.
    let launched_at_ms = std::time::SystemTime::now()
        .duration_since(std::time::UNIX_EPOCH)
        .map_or(0, |since| {
            i64::try_from(since.as_millis()).unwrap_or(i64::MAX)
        });

    let mut builder = tauri::Builder::default();

    // Single-instance must be registered before all other plugins so a second
    // launch forwards into this process.
    #[cfg(desktop)]
    {
        builder = builder.plugin(tauri_plugin_single_instance::init(|app, _argv, _cwd| {
            // Show/recreate the main window. Must be `spawn_blocking`, not the
            // main thread (WebView2 main-thread build deadlocks — see
            // `window_manager::on_blocking_pool`) and not `spawn` (the build parks its
            // thread until the window exists).
            let app = app.clone();
            tauri::async_runtime::spawn_blocking(move || {
                if let Err(err) = second_instance_launched(&app) {
                    tracing::error!(%err, "failed to show main window on second-instance launch")
                }
            });
        }));
    }

    builder = builder
        // `Started` fires as a navigation commits, so it covers the reload the
        // webview never tells us about.
        .on_page_load(|webview, payload| {
            if matches!(payload.event(), PageLoadEvent::Started) {
                page_started(webview.app_handle(), webview.label());
            }
        })
        .plugin(tauri_plugin_opener::init())
        .plugin(tauri_plugin_shell::init());

    #[cfg(any(target_os = "macos", windows))]
    {
        builder = builder.plugin(crate::updater::platform::plugin());
    }

    // The alerts' notifications; no capability grants it, so no page sends one.
    #[cfg(not(target_os = "macos"))]
    {
        builder = builder.plugin(tauri_plugin_notification::init());
    }

    let app = builder
        .invoke_handler(invoke_handler())
        .setup(move |app| {
            // First: everything below is what a user is asked for when the app
            // fails to start, and until this lands it reaches stderr alone.
            let folders = Folders::make(&Roots::of(app.handle())?)?;
            let log_guards = logging::install_file_layer(&log_reload, folders.logs.as_deref());
            host_file::manage(app.handle(), &folders);
            #[cfg(not(target_os = "macos"))]
            crate::webview_profile::manage(app.handle(), &folders);

            let state = AppState::new(
                SidecarService::spawn(&folders, |args| {
                    Ok(app.shell().sidecar("kubetail-sidecar")?.args(args))
                })?,
                log_guards,
                AppMonitors(app.handle().clone()),
                Some(window_state::path(&folders)),
            );

            // Create the main window. All windows are built in code
            // (`tauri.conf.json` declares none); `build_window` owns chrome and
            // pre-paint theming — visible from creation, no reveal step. See
            // docs/adr/2026-08-09-first-paint-theming.md.
            state.window_manager.show_main_window(app.handle())?;

            app.manage(state);

            // Build menu and tray
            #[cfg(target_os = "macos")]
            crate::app_menu::macos::install(app.handle())?;
            build_tray(app.handle())?;

            // Keep the tray's account section live off the sidecar's
            // AuthStateWatch stream.
            let watch = tray::watch_auth_state(app.handle().clone(), rebuild_tray_menu);
            tauri::async_runtime::spawn(watch);

            // Notify the monitor's alerts, with a window open or none. macOS
            // first reads the permission, asking for it at the first launch;
            // the plugin needs its press handler before it shows anything.
            #[cfg(target_os = "macos")]
            crate::alerts::macos::install(app.handle());
            #[cfg(not(target_os = "macos"))]
            crate::alerts::listen_for_presses(app.handle());
            let handle = app.handle().clone();
            let alerts =
                crate::alerts::watch(app.handle().clone(), launched_at_ms, move |notice| {
                    crate::alerts::show(&handle, notice)
                });
            tauri::async_runtime::spawn(alerts);

            #[cfg(any(target_os = "macos", windows))]
            crate::updater::platform::spawn_update_checker(app.handle(), rebuild_tray_menu);

            // Poke the sidecar on OS wake / network-return; see
            // docs/adr/2026-08-09-poke-resync-fanout.md.
            spawn_wake_poke_supervisor(app.handle());

            // Custom macOS Dock menu (no Tauri API — see dock_menu module).
            #[cfg(target_os = "macos")]
            crate::dock_menu::macos::install(app.handle());

            #[cfg(all(desktop, unix))]
            spawn_signal_handler(app.handle());

            Ok(())
        })
        .build(tauri::generate_context!())
        .expect("error while running tauri application");

    // Run with an event handler so we can intercept shutdown
    app.run(|app_handle, event| {
        match event {
            RunEvent::Ready => {
                // TODO: app is ready
            }
            #[cfg(target_os = "macos")]
            RunEvent::Reopen {
                has_visible_windows,
                ..
            } => {
                if !has_visible_windows {
                    let state = app_handle.state::<AppState>();
                    if let Err(err) = state.window_manager.show_main_window(app_handle) {
                        tracing::error!(%err, "failed to show main window on dock reopen");
                    }
                }
            }
            RunEvent::WindowEvent {
                label,
                event: WindowEvent::Destroyed,
                ..
            } => window_destroyed(app_handle, &label),
            RunEvent::WindowEvent {
                label,
                event: WindowEvent::Focused(true),
                ..
            } => window_focused(app_handle, &label),
            RunEvent::WindowEvent {
                label,
                event: WindowEvent::Moved(_) | WindowEvent::Resized(_),
                ..
            } => window_moved(app_handle, &label),
            RunEvent::ExitRequested { api, code, .. } => {
                let app = app_handle.clone();
                if exit_requested(app_handle, code, move |code| app.exit(code)) {
                    api.prevent_exit();
                }
            }
            RunEvent::Exit => exited(app_handle),
            _ => {}
        }
    });
}
// coverage:ignore-stop
