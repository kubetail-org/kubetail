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

//! macOS wake/network sources. **Wake**: an `NSObject` observer for
//! `NSWorkspaceDidWakeNotification` (same `define_class` idiom as
//! [`dock_menu::macos`](crate::dock_menu::macos)), registered on the main thread.
//! **Network**: `SCNetworkReachability` on the default route, callback on a
//! dedicated `CFRunLoop` thread; the core derives the edge.
//!
//! Both are passive and leaked for the process lifetime (no shutdown
//! teardown); once the supervisor's receiver drops, `try_send`s no-op.

use std::net::{Ipv4Addr, SocketAddr};
use std::sync::OnceLock;

use core_foundation::runloop::{kCFRunLoopCommonModes, CFRunLoop};
use objc2::rc::Retained;
use objc2::runtime::{AnyObject, NSObject};
use objc2::{define_class, msg_send, sel, ClassType};
use objc2_app_kit::{NSWorkspace, NSWorkspaceDidWakeNotification};
use objc2_foundation::NSNotificationCenter;
use system_configuration::network_reachability::{ReachabilityFlags, SCNetworkReachability};
use tauri::AppHandle;
use tokio::sync::mpsc::Sender;
use tokio_util::sync::CancellationToken;

use super::core::{self, RawEvent};

/// Wraps a non-`Sync` value so it can live in a `static`. Sound because every
/// access is on the main thread (see [`crate::dock_menu::macos`] for the same idiom).
struct MainThreadStatic<T>(#[allow(dead_code)] T);
// SAFETY: access is main-thread-only by construction.
unsafe impl<T> Send for MainThreadStatic<T> {}
unsafe impl<T> Sync for MainThreadStatic<T> {}

/// A global because the selector callback carries no user context.
static WAKE_TX: OnceLock<Sender<RawEvent>> = OnceLock::new();

/// Keeps the observer alive — the notification center holds it only weakly.
static WAKE_OBSERVER: OnceLock<MainThreadStatic<Retained<WakeObserver>>> = OnceLock::new();

// coverage:ignore-start AppKit calls it on a real wake
define_class!(
    /// Observer target for `NSWorkspaceDidWakeNotification`. Stateless — it
    /// forwards through the [`WAKE_TX`] global.
    #[unsafe(super(NSObject))]
    #[name = "KubetailWakeObserver"]
    struct WakeObserver;

    impl WakeObserver {
        #[unsafe(method(workspaceDidWake:))]
        fn workspace_did_wake(&self, _notification: Option<&AnyObject>) {
            if let Some(tx) = WAKE_TX.get() {
                // try_send: best-effort, never blocks the main thread. A full or
                // closed channel just drops the signal.
                let _ = tx.try_send(RawEvent::Resumed);
            }
        }
    }
);
// coverage:ignore-stop

// coverage:ignore-start registers with the OS on the real runtime's main thread
/// Spawns the macOS wake + network sources. `shutdown` unused — both sources
/// die with the process (module docs); accepted to match the cross-platform
/// shape.
pub fn spawn_sources(app: &AppHandle, tx: Sender<RawEvent>, _shutdown: CancellationToken) {
    spawn_network_source(tx.clone());

    // Wake observer must register on the main thread. Storing the sender is
    // the idempotency gate.
    if WAKE_TX.set(tx).is_err() {
        return;
    }
    if let Err(err) = app.run_on_main_thread(register_wake_observer) {
        tracing::warn!(%err, "failed to schedule wake observer registration");
    }
}

/// Registers the `NSWorkspaceDidWakeNotification` observer. Must run on the main
/// thread (queued via `run_on_main_thread`).
fn register_wake_observer() {
    let observer: Retained<WakeObserver> = unsafe { msg_send![WakeObserver::class(), new] };

    // NSWorkspace notifications are posted to the workspace's *own* center, not
    // the default one.
    let workspace = NSWorkspace::sharedWorkspace();
    let center: Retained<NSNotificationCenter> = workspace.notificationCenter();
    unsafe {
        center.addObserver_selector_name_object(
            &observer,
            sel!(workspaceDidWake:),
            Some(NSWorkspaceDidWakeNotification),
            None,
        );
    }

    let _ = WAKE_OBSERVER.set(MainThreadStatic(observer));
}

/// Starts the SCNetworkReachability monitor on a dedicated CFRunLoop thread:
/// primes the baseline, then forwards every transition. Never unscheduled —
/// idle, dies with the process.
fn spawn_network_source(tx: Sender<RawEvent>) {
    let spawned = std::thread::Builder::new()
        .name("kubetail-net-reachability".into())
        .spawn(move || {
            // 0.0.0.0 — reachability of the default route ("can we reach the
            // network without user intervention").
            let addr = SocketAddr::from((Ipv4Addr::UNSPECIFIED, 0));
            let mut reachability = SCNetworkReachability::from(addr);

            // Prime: record the baseline so a later offline→online is an edge.
            if let Ok(flags) = reachability.reachability() {
                let _ = tx.try_send(RawEvent::NetworkChanged {
                    online: is_online(flags),
                });
            }

            let cb_tx = tx.clone();
            if let Err(err) = reachability.set_callback(move |flags| {
                let _ = cb_tx.try_send(RawEvent::NetworkChanged {
                    online: is_online(flags),
                });
            }) {
                tracing::warn!(?err, "failed to set reachability callback");
                return;
            }

            // SAFETY: scheduling on the current thread's run loop with a valid,
            // non-null mode constant. The reachability ref outlives the run loop
            // (it lives on this thread's stack until the process exits).
            unsafe {
                if let Err(err) = reachability
                    .schedule_with_runloop(&CFRunLoop::get_current(), kCFRunLoopCommonModes)
                {
                    tracing::warn!(?err, "failed to schedule reachability run loop");
                    return;
                }
            }

            CFRunLoop::run_current();
        });

    if let Err(err) = spawned {
        tracing::warn!(%err, "failed to spawn reachability thread");
    }
}
// coverage:ignore-stop

fn is_online(flags: ReachabilityFlags) -> bool {
    core::sc_reachability_is_online(flags.bits())
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn online_needs_a_reachable_route_without_a_connection_first() {
        assert!(is_online(ReachabilityFlags::REACHABLE));
        assert!(!is_online(
            ReachabilityFlags::REACHABLE | ReachabilityFlags::CONNECTION_REQUIRED
        ));
        assert!(!is_online(ReachabilityFlags::empty()));
    }
}
