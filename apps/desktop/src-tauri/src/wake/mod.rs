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

//! Fires the sidecar's `Poke` on OS wake-from-sleep and network-return, so
//! long-lived connections resync ahead of the sidecar's ~30s wall-clock
//! backstop. See docs/adr/2026-08-09-poke-resync-fanout.md.
//!
//! [`core`] is the platform-agnostic, unit-tested heart (edge detection +
//! debounce); `supervisor` wires it to Tauri; `macos`/`windows`/`linux` are the
//! `#[cfg]`-gated native sources emitting [`core::RawEvent`]s.

mod core;
mod supervisor;

// Each source module exposes the same `spawn_sources` entry point; `platform`
// aliases the current target's so `supervisor` stays platform-agnostic.
#[cfg(target_os = "macos")]
mod macos;
#[cfg(target_os = "macos")]
use macos as platform;

#[cfg(windows)]
mod windows;
#[cfg(windows)]
use windows as platform;

#[cfg(target_os = "linux")]
mod linux;
#[cfg(target_os = "linux")]
use linux as platform;

// Other targets get no native sources — the sidecar's wall-clock detector is
// the backstop.
#[cfg(not(any(target_os = "macos", windows, target_os = "linux")))]
mod platform {
    use tauri::AppHandle;
    use tokio::sync::mpsc::Sender;
    use tokio_util::sync::CancellationToken;

    use super::core::RawEvent;

    pub fn spawn_sources(_app: &AppHandle, _tx: Sender<RawEvent>, _shutdown: CancellationToken) {}
}

pub use platform::spawn_sources;
pub use supervisor::{supervise, CHANNEL_DEPTH, DEBOUNCE_WINDOW};
