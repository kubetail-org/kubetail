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

//! Where the webview keeps its profile, which holds `localStorage`, on Linux
//! and Windows. macOS's WKWebView takes no path and keeps its own under the
//! bundle id.

use std::path::PathBuf;

use tauri::{AppHandle, Manager, Runtime};

use crate::paths::Folders;

/// Where the profile is ([`manage`]).
struct Location(PathBuf);

/// Manages the profile's [`Location`]. `setup` calls it before the first
/// window, whose webview opens the profile: without one it would keep its
/// profile under the bundle id, outside the sidecar's fence.
pub fn manage<R: Runtime>(app: &AppHandle<R>, folders: &Folders) {
    app.manage(Location(folders.webview.clone()));
}

/// The profile directory, as [`manage`] resolved it.
pub fn dir<R: Runtime>(app: &AppHandle<R>) -> Option<PathBuf> {
    app.try_state::<Location>()
        .map(|location| location.0.clone())
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::testutil::{folders, Scratch};

    #[test]
    fn manage_sets_the_directory_dir_reads() {
        let scratch = Scratch::new();
        let folders = folders(scratch.path());
        let app = tauri::test::mock_app();
        assert_eq!(dir(app.handle()), None);

        manage(app.handle(), &folders);

        assert_eq!(dir(app.handle()), Some(folders.state.join("webview")));
    }
}
