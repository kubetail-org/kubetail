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

//! Whether the OS lets the app show notifications, as a state watch each app
//! window's Monitoring tab reads. macOS holds the answer and signals no change,
//! so the host reads it again whenever a window gains focus.

use std::collections::HashMap;
use std::sync::Mutex;

use serde::Serialize;
use tauri::ipc::Channel;

/// The OS's answer.
#[cfg_attr(
    not(target_os = "macos"),
    allow(dead_code, reason = "only macOS asks; elsewhere the answer is Granted")
)]
#[derive(Clone, Copy, Debug, PartialEq, Eq, Serialize)]
pub enum NotificationPermission {
    Granted,
    Denied,
    /// Until the first launch's prompt is answered, and always outside a bundle.
    NotDetermined,
}

impl NotificationPermission {
    /// The answer `UNAuthorizationStatus` holds: 1 is denied, 2 authorized, 3
    /// provisional and 4 ephemeral, which each show a notification; 0, or a
    /// value a later macOS adds, is not determined.
    #[cfg(any(target_os = "macos", test))]
    pub fn from_authorization_status(status: isize) -> Self {
        match status {
            1 => Self::Denied,
            2..=4 => Self::Granted,
            _ => Self::NotDetermined,
        }
    }

    /// The answer before any reading: Windows and Linux ask nothing, and macOS
    /// is read at launch.
    fn at_launch() -> Option<Self> {
        (!cfg!(target_os = "macos")).then_some(Self::Granted)
    }
}

/// The answer and each window's channel, keyed by its webview's label, so a
/// reload's subscription replaces its last.
pub struct PermissionWatch {
    inner: Mutex<Inner>,
}

#[derive(Default)]
struct Inner {
    value: Option<NotificationPermission>,
    channels: HashMap<String, Channel<NotificationPermission>>,
}

impl Default for PermissionWatch {
    fn default() -> Self {
        Self {
            inner: Mutex::new(Inner {
                value: NotificationPermission::at_launch(),
                channels: HashMap::new(),
            }),
        }
    }
}

impl PermissionWatch {
    fn lock(&self) -> std::sync::MutexGuard<'_, Inner> {
        self.inner.lock().unwrap_or_else(|p| p.into_inner())
    }

    /// Sends `channel` the answer at once, when there is one, and each change
    /// after it.
    pub fn subscribe(&self, label: &str, channel: Channel<NotificationPermission>) {
        let mut inner = self.lock();
        if let Some(value) = inner.value {
            send(&channel, value);
        }
        inner.channels.insert(label.to_string(), channel);
    }

    /// Drops the channel of the webview `label`, as its window goes.
    pub fn unsubscribe(&self, label: &str) {
        self.lock().channels.remove(label);
    }

    /// Takes `value` as the answer, sending it to every channel when it changed.
    #[cfg(any(target_os = "macos", test))]
    pub fn set(&self, value: NotificationPermission) {
        let mut inner = self.lock();
        if inner.value == Some(value) {
            return;
        }
        inner.value = Some(value);
        for channel in inner.channels.values() {
            send(channel, value);
        }
    }
}

/// Sends a frame; a page gone since is the webview's own teardown to report.
fn send(channel: &Channel<NotificationPermission>, value: NotificationPermission) {
    if let Err(err) = channel.send(value) {
        tracing::debug!(%err, "a notification permission frame went undelivered");
    }
}

#[cfg(test)]
mod tests {
    use std::sync::{Arc, Mutex};

    use tauri::ipc::{Channel, InvokeResponseBody};

    use super::*;

    /// A channel that keeps what it is sent, as JSON text.
    fn recording() -> (Channel<NotificationPermission>, Arc<Mutex<Vec<String>>>) {
        let sent = Arc::new(Mutex::new(Vec::new()));
        let kept = sent.clone();
        let channel = Channel::new(move |body| {
            if let InvokeResponseBody::Json(json) = body {
                kept.lock().unwrap().push(json);
            }
            Ok(())
        });
        (channel, sent)
    }

    #[test]
    fn each_authorization_status_is_an_answer() {
        use NotificationPermission::*;
        for (status, want) in [
            (0, NotDetermined),
            (1, Denied),
            (2, Granted),
            (3, Granted),
            (4, Granted),
            (5, NotDetermined),
            (-1, NotDetermined),
        ] {
            assert_eq!(
                NotificationPermission::from_authorization_status(status),
                want,
                "{status}"
            );
        }
    }

    /// Windows and Linux send `Granted` at once; macOS sends nothing until it
    /// has read the answer.
    #[test]
    fn the_watch_sends_its_value_at_once() {
        let watch = PermissionWatch::default();
        let (channel, sent) = recording();

        watch.subscribe("main", channel);

        let want: Vec<String> = NotificationPermission::at_launch()
            .map(|p| serde_json::to_string(&p).unwrap())
            .into_iter()
            .collect();
        assert_eq!(*sent.lock().unwrap(), want);
    }

    /// A change reaches every window; a reading that finds the answer
    /// unchanged sends no frame; a window gone, or reloaded, keeps no channel.
    #[test]
    fn the_watch_sends_a_change_alone() {
        let watch = PermissionWatch::default();
        watch.set(NotificationPermission::Denied);
        let (main, to_main) = recording();
        let (stale, to_stale) = recording();
        let (reloaded, to_reloaded) = recording();
        let (other, to_other) = recording();
        watch.subscribe("main", main);
        watch.subscribe("window-1", stale);
        watch.subscribe("window-1", reloaded);
        watch.subscribe("window-2", other);
        watch.unsubscribe("window-2");

        watch.set(NotificationPermission::Denied);
        watch.set(NotificationPermission::Granted);

        assert_eq!(*to_main.lock().unwrap(), ["\"Denied\"", "\"Granted\""]);
        assert_eq!(*to_stale.lock().unwrap(), ["\"Denied\""]);
        assert_eq!(*to_reloaded.lock().unwrap(), ["\"Denied\"", "\"Granted\""]);
        assert_eq!(*to_other.lock().unwrap(), ["\"Denied\""]);
    }

    /// A channel whose page is gone is logged and kept no worse.
    #[test]
    fn an_undelivered_frame_is_swallowed() {
        let watch = PermissionWatch::default();
        watch.subscribe("main", Channel::new(|_| Err(tauri::Error::WebviewNotFound)));
        watch.set(NotificationPermission::Denied);
    }
}
