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

//! `tauri-plugin-updater`'s wiring: the check, the offer and the install, on
//! the real runtime against a real release. Wiring alone; what it decides is in
//! `mod.rs` and `lib.rs`. Listed in `.github/codecov.yml`'s `ignore`.

use std::sync::Mutex;

use tauri::plugin::TauriPlugin;
use tauri::{AppHandle, Manager, Runtime};
use tauri_plugin_updater::{Config, Error, Update, Updater, UpdaterExt};

use crate::state::AppState;

/// The update the tray offers, kept until the user takes it.
#[derive(Default)]
struct Pending(Mutex<Option<Update>>);

impl Pending {
    fn slot(&self) -> std::sync::MutexGuard<'_, Option<Update>> {
        self.0.lock().unwrap_or_else(|p| p.into_inner())
    }
}

/// The plugin, configured by `plugins.updater` in `tauri.conf.json`: the
/// current key and its manifest. It registers commands, but no capability
/// grants them.
pub fn plugin<R: Runtime>() -> TauriPlugin<R, Config> {
    tauri_plugin_updater::Builder::new().build()
}

/// Checks for an update until one is found or the app quits, and offers it in
/// the tray. A debug build checks nothing: it is not an installed app.
pub fn spawn_update_checker(app: &AppHandle, rebuild: fn(&AppHandle)) {
    app.manage(Pending::default());
    if cfg!(debug_assertions) {
        return;
    }
    let app = app.clone();
    tauri::async_runtime::spawn(async move {
        let updaters = match updaters(&app) {
            Ok(updaters) => updaters,
            Err(err) => {
                tracing::error!(%err, "failed to build the updater");
                return;
            }
        };
        let shutdown = app.state::<AppState>().shutdown.clone();
        let found = super::find(shutdown, super::CHECK_INTERVAL, || {
            super::check_each(updaters.iter().map(check))
        })
        .await;
        if let Some(update) = found {
            tracing::info!(version = %update.version, "update available");
            offer_update(&app, update, rebuild);
        }
    });
}

/// The tray's update item: downloads the offered update, checks its signature,
/// installs it, stops the sidecar and restarts. The offer is withdrawn while it
/// runs and comes back if it fails before the sidecar is stopped.
pub fn install(app: &AppHandle, rebuild: fn(&AppHandle)) {
    let Some(update) = app.state::<Pending>().slot().take() else {
        return;
    };
    super::offer(app, None);
    rebuild(app);

    let app = app.clone();
    tauri::async_runtime::spawn(async move {
        tracing::info!(version = %update.version, "downloading update");
        // `download` refuses bytes whose signature or signed version is not
        // the pinned key's.
        let bytes = match update.download(|_, _| {}, || {}).await {
            Ok(bytes) => bytes,
            Err(err) => {
                tracing::error!(%err, "update download failed");
                offer_update(&app, update, rebuild);
                return;
            }
        };
        if app.state::<AppState>().shutdown.is_cancelled() {
            return;
        }

        // Blocking: on macOS `install` swaps the app bundle, and `stop_for_install`
        // waits for the sidecar. On Windows `install` stops the sidecar through
        // `builder`'s `on_before_exit`, then ends the process itself.
        let installing = update.clone();
        let installing_app = app.clone();
        let installed = tauri::async_runtime::spawn_blocking(move || {
            installing.install(bytes)?;
            tauri_plugin_updater::Result::Ok(crate::stop_for_install(&installing_app))
        })
        .await;
        match installed {
            // The restart's `ExitRequested` cannot be prevented, so the sidecar
            // is stopped before it is raised.
            Ok(Ok(true)) => {
                tracing::info!(version = %update.version, "update installed; restarting");
                app.request_restart();
            }
            Ok(Ok(false)) => {
                tracing::info!(version = %update.version, "update installed; a quit is under way");
            }
            Ok(Err(err)) => {
                tracing::error!(%err, "update install failed");
                if app.state::<AppState>().shutdown.is_cancelled() {
                    // The sidecar was stopped for an installer that did not start.
                    app.exit(0);
                } else {
                    offer_update(&app, update, rebuild);
                }
            }
            Err(err) => tracing::error!(%err, "update install did not finish"),
        }
    });
}

fn offer_update(app: &AppHandle, update: Update, rebuild: fn(&AppHandle)) {
    super::offer(app, Some(update.version.clone()));
    *app.state::<Pending>().slot() = Some(update);
    rebuild(app);
}

/// One key's check. A key with no manifest on `updates.kubetail.com`, as the
/// standby has until it is promoted, finds nothing.
async fn check(updater: &Updater) -> tauri_plugin_updater::Result<Option<Update>> {
    match updater.check().await {
        Err(Error::ReleaseNotFound) => Ok(None),
        checked => checked,
    }
}

/// An updater for each key, the current one first. The update a check returns
/// is verified against the key that found it.
fn updaters(app: &AppHandle) -> tauri_plugin_updater::Result<[Updater; 2]> {
    let standby = super::standby();
    Ok([
        builder(app).build()?,
        builder(app)
            .pubkey(standby.pubkey)
            .endpoints(vec![standby.endpoint.parse()?])?
            .build()?,
    ])
}

/// The builder, with the stop the Windows installer runs before it starts
/// `msiexec` and ends the process: that path raises no `ExitRequested` and no
/// `RunEvent::Exit`, so it stops the sidecar, releases the plugins and runs
/// `exited` itself, whose kill ends a sidecar the stop left running before
/// the installer finds its executable locked.
fn builder(app: &AppHandle) -> tauri_plugin_updater::UpdaterBuilder {
    let hook = app.clone();
    // The plugin builds the check's and the download's clients alike, naming
    // itself, and `configure_client` runs last on both.
    let user_agent = super::user_agent(&app.package_info().version);
    // `timeout` bounds the check alone: the plugin gives the `Update` it
    // returns none, so the read timeout is what ends a stalled download.
    app.updater_builder()
        .timeout(super::CHECK_TIMEOUT)
        .configure_client(move |client| {
            client
                .read_timeout(super::DOWNLOAD_STALL)
                .user_agent(user_agent.clone())
        })
        .on_before_exit(move || {
            crate::stop_for_install(&hook);
            hook.cleanup_before_exit();
            crate::exited(&hook);
        })
}
