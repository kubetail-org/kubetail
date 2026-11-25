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

//! Linux wake/network sources over the system D-Bus (`zbus`). **Wake**:
//! logind's `PrepareForSleep(false)` = resume. **Network**: NetworkManager's
//! `StateChanged` (plus a one-time `State` read to prime the baseline) via
//! [`core::nm_state_is_online`]. Each source is a tokio task selecting on
//! `shutdown`; on bus/service unavailability it logs and exits — the other
//! source and the wall-clock detector still function.

use futures_util::StreamExt;
use tauri::AppHandle;
use tokio::sync::mpsc::Sender;
use tokio_util::sync::CancellationToken;

use super::core::{self, RawEvent};

/// systemd-logind manager — just the `PrepareForSleep` signal.
#[zbus::proxy(
    interface = "org.freedesktop.login1.Manager",
    default_service = "org.freedesktop.login1",
    default_path = "/org/freedesktop/login1"
)]
trait LogindManager {
    /// `start = true` just before sleep; `start = false` just after resume.
    #[zbus(signal)]
    fn prepare_for_sleep(&self, start: bool) -> zbus::Result<()>;
}

/// NetworkManager — the `StateChanged` signal plus the `State` property.
#[zbus::proxy(
    interface = "org.freedesktop.NetworkManager",
    default_service = "org.freedesktop.NetworkManager",
    default_path = "/org/freedesktop/NetworkManager"
)]
trait NetworkManager {
    #[zbus(signal)]
    fn state_changed(&self, state: u32) -> zbus::Result<()>;

    // Named `nm_state` (D-Bus name `State`) so it doesn't collide with the
    // `state_changed` signal: zbus would derive `receive_state_changed` for both.
    #[zbus(property, name = "State")]
    fn nm_state(&self) -> zbus::Result<u32>;
}

/// Spawns the logind wake source and the NetworkManager network source. `app` is
/// unused (no main-thread registration); accepted to match the cross-platform shape.
pub fn spawn_sources(_app: &AppHandle, tx: Sender<RawEvent>, shutdown: CancellationToken) {
    let wake_tx = tx.clone();
    let wake_shutdown = shutdown.clone();
    tauri::async_runtime::spawn(async move {
        if let Err(err) = run_wake_source(wake_tx, wake_shutdown).await {
            tracing::warn!(%err, "logind wake source ended");
        }
    });

    tauri::async_runtime::spawn(async move {
        if let Err(err) = run_network_source(tx, shutdown).await {
            tracing::warn!(%err, "NetworkManager network source ended");
        }
    });
}

/// Forwards a [`RawEvent::Resumed`] on each logind resume, until shutdown or
/// stream end.
async fn run_wake_source(tx: Sender<RawEvent>, shutdown: CancellationToken) -> zbus::Result<()> {
    let conn = zbus::Connection::system().await?;
    let proxy = LogindManagerProxy::new(&conn).await?;
    let mut signals = proxy.receive_prepare_for_sleep().await?;

    loop {
        let signal = tokio::select! {
            biased;
            _ = shutdown.cancelled() => return Ok(()),
            signal = signals.next() => signal,
        };
        let Some(signal) = signal else { return Ok(()) };

        // `start == false` is the resume edge.
        if !signal.args()?.start && tx.send(RawEvent::Resumed).await.is_err() {
            return Ok(());
        }
    }
}

/// Primes the baseline from NetworkManager's `State`, then forwards a
/// [`RawEvent::NetworkChanged`] on each `StateChanged`, until shutdown or stream
/// end.
async fn run_network_source(tx: Sender<RawEvent>, shutdown: CancellationToken) -> zbus::Result<()> {
    let conn = zbus::Connection::system().await?;
    let proxy = NetworkManagerProxy::new(&conn).await?;

    // Prime so a later offline→online reads as an edge.
    if let Ok(state) = proxy.nm_state().await {
        let _ = tx
            .send(RawEvent::NetworkChanged {
                online: core::nm_state_is_online(state),
            })
            .await;
    }

    let mut signals = proxy.receive_state_changed().await?;
    loop {
        let signal = tokio::select! {
            biased;
            _ = shutdown.cancelled() => return Ok(()),
            signal = signals.next() => signal,
        };
        let Some(signal) = signal else { return Ok(()) };

        let online = core::nm_state_is_online(signal.args()?.state);
        if tx.send(RawEvent::NetworkChanged { online }).await.is_err() {
            return Ok(());
        }
    }
}
