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

//! Fixtures for tests that need a Tauri app: Tauri's mock runtime, with the
//! app's own directories under a scratch directory and a sidecar with no
//! process behind it.

use std::path::{Path, PathBuf};
use std::sync::atomic::{AtomicU64, Ordering};

use tauri::ipc::{CallbackFn, InvokeBody};
use tauri::test::{mock_builder, mock_context, noop_assets, MockRuntime, INVOKE_KEY};
use tauri::utils::config::AppDirectoriesOverride;
use tauri::webview::InvokeRequest;
use tauri::window::Monitor;
use tauri::{
    App, Manager, PhysicalPosition, PhysicalSize, WebviewUrl, WebviewWindow, WebviewWindowBuilder,
};

use crate::paths::{Folders, Roots};
use crate::services::sidecar::SidecarService;
use crate::state::AppState;
use crate::window_manager::Monitors;

/// A fresh directory under the OS temp dir, removed on drop.
pub struct Scratch(PathBuf);

impl Scratch {
    pub fn new() -> Self {
        static COUNTER: AtomicU64 = AtomicU64::new(0);
        let n = COUNTER.fetch_add(1, Ordering::Relaxed);
        let dir =
            std::env::temp_dir().join(format!("kubetail-app-test-{}-{n}", std::process::id()));
        let _ = std::fs::remove_dir_all(&dir);
        std::fs::create_dir_all(&dir).unwrap();
        // As the kernel names it, so a test compares it with resolved paths
        // (macOS's temp dir is under the link `/var`).
        Self(dunce::canonicalize(&dir).unwrap())
    }

    pub fn path(&self) -> &Path {
        &self.0
    }
}

impl Drop for Scratch {
    fn drop(&mut self) {
        let _ = std::fs::remove_dir_all(&self.0);
    }
}

/// An app on the mock runtime serving every command, its app directories
/// under `root`, managing an [`AppState`] over `sidecar` that keeps the main
/// window's frame in `root/window-state.json`.
pub fn mock_app(root: &Path, sidecar: SidecarService) -> App<MockRuntime> {
    let app = bare_app(root);
    app.manage(AppState::new(
        sidecar,
        Vec::new(),
        OneMonitor(Some(monitor((0, 0, 1920, 1080), 1.0))),
        Some(root.join("window-state.json")),
    ));
    app
}

/// [`mock_app`] before `setup` manages its state, with `host.json` under
/// `root`.
pub fn bare_app(root: &Path) -> App<MockRuntime> {
    let mut context = mock_context(noop_assets());
    context.config_mut().app.app_directories_override =
        Some(AppDirectoriesOverride::Root(root.to_path_buf()));
    let app = mock_builder()
        .plugin(tauri_plugin_shell::init())
        .invoke_handler(crate::invoke_handler())
        .build(context)
        .unwrap();
    crate::host_file::manage(app.handle(), &folders(root));
    app
}

/// The monitors for the mock runtime, which has none: every window is on the
/// one given, which is also the primary. `None` is a system whose monitors
/// cannot be read.
pub struct OneMonitor(pub Option<Monitor>);

impl Monitors for OneMonitor {
    fn of_window(&self, _label: &str) -> Option<Monitor> {
        self.0.clone()
    }

    fn primary(&self) -> Option<Monitor> {
        self.0.clone()
    }

    fn all(&self) -> Vec<Monitor> {
        self.0.iter().cloned().collect()
    }
}

/// A monitor whose work area is `(x, y, width, height)` physical px.
pub fn monitor(work_area: (i32, i32, u32, u32), scale_factor: f64) -> Monitor {
    let (x, y, width, height) = work_area;
    let position = PhysicalPosition::new(x, y);
    let size = PhysicalSize::new(width, height);
    tauri_runtime::monitor::Monitor {
        name: None,
        size,
        position,
        work_area: tauri_runtime::dpi::PhysicalRect { position, size },
        scale_factor,
    }
    .into()
}

/// A sidecar that is `sh -c script`, its directories under `root`.
#[cfg(unix)]
pub fn script_sidecar(root: &Path, script: &str) -> SidecarService {
    use tauri_plugin_shell::ShellExt;

    // Any app with the shell plugin makes the command; the process outlives it.
    let shell = mock_builder()
        .plugin(tauri_plugin_shell::init())
        .build(mock_context(noop_assets()))
        .unwrap();
    SidecarService::spawn(&folders(root), |_| {
        Ok(shell.shell().command("sh").args(["-c", script]))
    })
    .unwrap()
}

/// Platform roots under `root`, so nothing a test makes lands in the user's
/// own directories. `root` is the home, which holds them all, as Windows'
/// profile holds every layout folder.
pub fn roots(root: &Path) -> Roots {
    Roots {
        config: Some(root.join("config")),
        local_data: root.join("local"),
        cache: root.join("cache"),
        home: root.to_path_buf(),
        state: root.join("state"),
    }
}

/// The host's folders over [`roots`] under `root`, made.
pub fn folders(root: &Path) -> Folders {
    Folders::make(&roots(root)).unwrap()
}

/// Opens a window directly, past the window manager.
pub fn window(app: &App<MockRuntime>, label: &str) -> WebviewWindow<MockRuntime> {
    WebviewWindowBuilder::new(app, label, WebviewUrl::default())
        .build()
        .unwrap()
}

/// Invokes `cmd` from `window`'s webview with JSON arguments, as the frontend
/// would.
pub fn invoke(
    window: &WebviewWindow<MockRuntime>,
    cmd: &str,
    args: serde_json::Value,
) -> Result<serde_json::Value, serde_json::Value> {
    // The app's own origin on each platform (`window_manager::is_app_origin`).
    let url = if cfg!(windows) {
        "http://tauri.localhost"
    } else {
        "tauri://localhost"
    };
    let request = InvokeRequest {
        cmd: cmd.into(),
        callback: CallbackFn(0),
        error: CallbackFn(1),
        url: url.parse().unwrap(),
        body: InvokeBody::Json(args),
        headers: Default::default(),
        invoke_key: INVOKE_KEY.to_string(),
    };
    tauri::test::get_ipc_response(window, request).map(|body| body.deserialize().unwrap())
}
