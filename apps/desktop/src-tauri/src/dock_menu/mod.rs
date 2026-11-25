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

//! The macOS Dock menu and Quit reply: New Window, Show Main Window, and
//! `applicationShouldTerminate:`'s answer.
//!
//! `macos` patches them onto AppKit; what an item does and how a quit is
//! answered are decided here, compiled and tested on every platform.

#[cfg(target_os = "macos")]
pub mod macos;

use tauri::{AppHandle, Manager, Runtime};

use crate::state::AppState;

/// `NSApplicationTerminateReply` discriminants.
#[cfg_attr(not(target_os = "macos"), allow(dead_code))]
const NS_TERMINATE_CANCEL: usize = 0;
#[cfg_attr(not(target_os = "macos"), allow(dead_code))]
const NS_TERMINATE_NOW: usize = 1;

/// `kAEQuitApplication` and `kAEQuitReason` from `<CoreServices/.../AppleEvents.h>`,
/// where they are the four-character literals `'quit'` and `'why?'`.
#[cfg_attr(not(target_os = "macos"), allow(dead_code))]
const K_AE_QUIT_APPLICATION: u32 = u32::from_be_bytes(*b"quit");
#[cfg_attr(not(target_os = "macos"), allow(dead_code))]
const K_AE_QUIT_REASON: u32 = u32::from_be_bytes(*b"why?");

/// What a Dock menu item does.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
#[cfg_attr(not(target_os = "macos"), allow(dead_code))]
enum DockAction {
    NewWindow,
    ShowMainWindow,
}

/// Runs `action`. `app` is `None` when the menu fired before `install` stored
/// the handle; a menu callback has no caller to return a failure to, so both
/// are logged.
#[cfg_attr(not(target_os = "macos"), allow(dead_code))]
fn run_action<R: Runtime>(app: Option<&AppHandle<R>>, action: DockAction) {
    let Some(app) = app else {
        tracing::error!("dock menu fired before install()");
        return;
    };
    let window_manager = &app.state::<AppState>().window_manager;
    let (result, what) = match action {
        DockAction::NewWindow => (
            window_manager.new_window(app),
            "failed to open window from dock menu",
        ),
        DockAction::ShowMainWindow => (
            window_manager.show_main_window(app),
            "failed to show main window from dock menu",
        ),
    };
    if let Err(err) = result {
        tracing::error!(%err, "{what}");
    }
}

/// `applicationShouldTerminate:`'s reply. A system quit (logout, restart,
/// shutdown) gets `NSTerminateNow`: cancelling would abort the logout, and
/// cleanup still runs in `RunEvent::Exit`. A user Quit is cancelled and
/// re-driven through `exit`, which is `AppHandle::exit`: `terminate:` would
/// skip Tauri's `RunEvent::ExitRequested` hook, and `exit` does not route back
/// through `terminate:`. `exit` is `None` when the quit fired before `install`
/// stored the handle; AppKit then terminates.
#[cfg_attr(not(target_os = "macos"), allow(dead_code))]
fn terminate_reply(system_quit: bool, exit: Option<impl FnOnce()>) -> usize {
    if system_quit {
        return NS_TERMINATE_NOW;
    }
    let Some(exit) = exit else {
        tracing::error!("applicationShouldTerminate: fired before install()");
        return NS_TERMINATE_NOW;
    };
    exit();
    NS_TERMINATE_CANCEL
}

/// Whether the current Apple Event is the OS's quit: a `kAEQuitApplication`
/// event carrying a `kAEQuitReason` param. A user Quit posts `terminate:`
/// directly, with no event.
#[cfg_attr(not(target_os = "macos"), allow(dead_code))]
fn is_system_quit_event(event_id: u32, has_quit_reason: bool) -> bool {
    event_id == K_AE_QUIT_APPLICATION && has_quit_reason
}

#[cfg(test)]
mod tests {
    use std::cell::Cell;

    use super::*;
    use crate::logging::captured;
    use crate::services::sidecar::SidecarService;
    use crate::testutil::{mock_app, window, Scratch};

    #[test]
    fn a_system_quit_terminates_now_without_re_driving_the_exit() {
        let exited = Cell::new(false);
        let reply = terminate_reply(true, Some(|| exited.set(true)));
        assert_eq!(reply, NS_TERMINATE_NOW);
        assert!(!exited.get());
    }

    #[test]
    fn a_user_quit_is_cancelled_and_re_driven_through_exit() {
        let exited = Cell::new(false);
        let reply = terminate_reply(false, Some(|| exited.set(true)));
        assert_eq!(reply, NS_TERMINATE_CANCEL);
        assert!(exited.get());
    }

    #[test]
    fn a_user_quit_before_install_terminates_now_and_logs() {
        let out = captured(|| {
            let reply = terminate_reply(false, None::<fn()>);
            assert_eq!(reply, NS_TERMINATE_NOW);
        });
        assert!(out.contains("fired before install()"), "{out}");
    }

    #[test]
    fn only_a_quit_event_with_a_reason_is_a_system_quit() {
        assert!(is_system_quit_event(K_AE_QUIT_APPLICATION, true));
        assert!(!is_system_quit_event(K_AE_QUIT_APPLICATION, false));
        assert!(!is_system_quit_event(u32::from_be_bytes(*b"odoc"), true));
    }

    #[test]
    fn the_quit_codes_are_apple_events_h_values() {
        assert_eq!(K_AE_QUIT_APPLICATION, 0x7175_6974);
        assert_eq!(K_AE_QUIT_REASON, 0x7768_793f);
    }

    #[test]
    fn an_action_before_install_is_logged() {
        let out = captured(|| {
            run_action::<tauri::test::MockRuntime>(None, DockAction::NewWindow);
        });
        assert!(out.contains("dock menu fired before install()"), "{out}");
    }

    #[test]
    fn show_main_window_surfaces_the_open_main_window() {
        let scratch = Scratch::new();
        let app = mock_app(scratch.path(), SidecarService::without_process(None));
        window(&app, "main");

        run_action(Some(app.handle()), DockAction::ShowMainWindow);

        assert_eq!(app.webview_windows().len(), 1);
    }

    #[test]
    fn new_window_opens_nothing_once_a_quit_has_begun() {
        let scratch = Scratch::new();
        let app = mock_app(scratch.path(), SidecarService::without_process(None));
        app.state::<AppState>().shutdown.cancel();

        run_action(Some(app.handle()), DockAction::NewWindow);

        assert!(app.webview_windows().is_empty());
    }

    /// The window manager mints `window-1` first, and Tauri refuses a second
    /// window with a label already in use.
    #[test]
    fn a_failed_action_is_logged() {
        let scratch = Scratch::new();
        let app = mock_app(scratch.path(), SidecarService::without_process(None));
        window(&app, "window-1");

        let out = captured(|| run_action(Some(app.handle()), DockAction::NewWindow));

        assert!(
            out.contains("failed to open window from dock menu"),
            "{out}"
        );
    }
}
