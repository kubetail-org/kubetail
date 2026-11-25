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

//! Builds the macOS menu bar, once during app setup. Wiring alone: AppKit
//! builds menus on the main thread, which no test runs on, so coverage ignores
//! this file.

use tauri::menu::{MenuBuilder, MenuItem, SubmenuBuilder};
use tauri::{AppHandle, Manager};

use super::{AppMenuAction, NEW_WINDOW_ID, QUIT_ID};
use crate::error::Result;
use crate::state::AppState;

/// Builds and installs the menu bar.
///
/// The Edit menu is what carries ⌘C / ⌘V / ⌘X / ⌘A / ⌘Z: WKWebView receives
/// them as the menu items' native selectors, so without it the keys do
/// nothing. A page that handles the keydown and prevents its default still
/// wins (the log viewer's copy).
///
/// "Quit" must stay a custom item routed through [`AppHandle::exit`]:
/// [`SubmenuBuilder::quit`] terminates natively (`terminate:`) and skips the
/// `RunEvent::ExitRequested` graceful-shutdown hook.
pub fn install(app: &AppHandle) -> Result<()> {
    let new_window =
        MenuItem::with_id(app, NEW_WINDOW_ID, "New Window", true, Some("CmdOrCtrl+N"))?;
    let quit = MenuItem::with_id(app, QUIT_ID, "Quit", true, Some("CmdOrCtrl+Q"))?;

    let file_menu = SubmenuBuilder::new(app, "File")
        .item(&new_window)
        .separator()
        .item(&quit)
        .build()?;

    let edit_menu = SubmenuBuilder::new(app, "Edit")
        .undo()
        .redo()
        .separator()
        .cut()
        .copy()
        .paste()
        .select_all()
        .build()?;

    let menu = MenuBuilder::new(app)
        .item(&file_menu)
        .item(&edit_menu)
        .build()?;
    app.set_menu(menu)?;

    app.on_menu_event(
        |app, event| match AppMenuAction::from_id(event.id().as_ref()) {
            Some(AppMenuAction::NewWindow) => {
                let state = app.state::<AppState>();
                if let Err(err) = state.window_manager.new_window(app) {
                    tracing::error!(%err, "failed to open window from app menu");
                }
            }
            Some(AppMenuAction::Quit) => app.exit(0),
            None => {}
        },
    );

    Ok(())
}
