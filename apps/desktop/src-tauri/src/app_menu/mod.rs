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

//! The application menu bar, macOS-only: File → New Window / Quit, and Edit.
//! Linux and Windows have none — a native menu would render inside each
//! window — and the webview's `AppMenu` calls `new_window` / `quit` instead.
//!
//! `macos` builds the bar; what an item does is decided here, compiled and
//! tested on every platform.

#[cfg(target_os = "macos")]
pub mod macos;

#[cfg_attr(not(target_os = "macos"), allow(dead_code))]
const NEW_WINDOW_ID: &str = "new_window";
#[cfg_attr(not(target_os = "macos"), allow(dead_code))]
const QUIT_ID: &str = "quit";

/// What an app menu item does.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
#[cfg_attr(not(target_os = "macos"), allow(dead_code))]
enum AppMenuAction {
    NewWindow,
    Quit,
}

#[cfg_attr(not(target_os = "macos"), allow(dead_code))]
impl AppMenuAction {
    /// The action for a menu event's id. The Edit items are predefined and
    /// handled natively, so they arrive as `None`.
    fn from_id(id: &str) -> Option<Self> {
        match id {
            NEW_WINDOW_ID => Some(Self::NewWindow),
            QUIT_ID => Some(Self::Quit),
            _ => None,
        }
    }
}

#[cfg(test)]
mod tests {
    use super::{AppMenuAction, NEW_WINDOW_ID, QUIT_ID};

    #[test]
    fn each_item_id_maps_to_its_action() {
        assert_eq!(
            AppMenuAction::from_id(NEW_WINDOW_ID),
            Some(AppMenuAction::NewWindow)
        );
        assert_eq!(AppMenuAction::from_id(QUIT_ID), Some(AppMenuAction::Quit));
        assert_eq!(AppMenuAction::from_id("copy"), None);
    }
}
