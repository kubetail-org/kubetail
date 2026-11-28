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

//! The monitor's alerts as OS notifications. The sidecar decides what alerts;
//! the host shows it, with a window open or none. Each notification is fixed
//! text around names a target returned, stripped and bounded here. macOS sends
//! through its notification center (`macos.rs`), Windows and Linux through
//! `tauri-plugin-notification`, which no window is granted. Each reports a
//! press, which [`pressed`] hands a window.

#[cfg(target_os = "macos")]
pub mod macos;
mod permission;

#[cfg(not(target_os = "macos"))]
use std::collections::VecDeque;
use std::future::Future;
#[cfg(not(target_os = "macos"))]
use std::sync::Mutex;
use std::time::Duration;

use futures_util::{Stream, StreamExt};
use tauri::{AppHandle, Manager, Runtime};
use tokio::time::Instant;
use tokio_util::sync::CancellationToken;

pub use permission::{NotificationPermission, PermissionWatch};

use crate::services::sidecar::alert_event_feed_frame::Type as FrameType;
use crate::services::sidecar::{Alert, AlertEventFeedFrame};
use crate::state::AppState;
use crate::window_title::visible;

/// Every notification's title.
pub const TITLE: &str = "Kubetail";

/// How long a live group takes alerts after its first.
const GROUP_WINDOW: Duration = Duration::from_secs(2);

/// The most notifications a group shows; the rest are counted in one more.
const MAX_SHOWN: usize = 5;

/// The most characters of each name a body keeps.
const MAX_SUBJECT_CHARS: usize = 120;
const MAX_TARGET_CHARS: usize = 60;
const MAX_SIGNAL_CHARS: usize = 60;

/// The prefix of the id of a notification that names an incident.
const INCIDENT_PREFIX: &str = "incident/";

/// How many of the newest notices the plugin showed a press still reaches.
#[cfg(not(target_os = "macos"))]
const MAX_PRESSABLE: usize = 64;

/// One notification to show.
#[derive(Clone, Debug, PartialEq, Eq)]
pub struct Notice {
    /// Unique to the process: `incident/<id>`, or `summary/<n>`. macOS keys a
    /// notification and its press by it; the plugin, which keys one by an
    /// integer, maps it through [`PluginNotices`].
    pub id: String,
    pub body: String,
}

/// The incident a notification's id names; none for a summary.
pub fn incident_of(id: &str) -> Option<&str> {
    id.strip_prefix(INCIDENT_PREFIX)
}

/// The body of `alert`'s notification: fixed text around its names, each
/// stripped of what draws as nothing and cut to its bound.
pub fn alert_body(alert: &Alert) -> String {
    format!(
        "{} on {}: {}",
        visible(&alert.subject_label, MAX_SUBJECT_CHARS),
        visible(&alert.target_label, MAX_TARGET_CHARS),
        visible(&alert.signal_label, MAX_SIGNAL_CHARS),
    )
}

/// What the listener keeps across its streams: when the host launched, the
/// highest `alert_seq` it has handled, and the group it is filling.
struct Groups {
    launched_at_ms: i64,
    handled: i64,
    group: Vec<Alert>,
    summaries: u64,
}

impl Groups {
    fn new(launched_at_ms: i64) -> Self {
        Self {
            launched_at_ms,
            handled: 0,
            group: Vec::new(),
            summaries: 0,
        }
    }

    /// Takes `alert` into the group, `false` for one handled already or raised
    /// before the launch. Taken, it is handled. A stream sends in commit order,
    /// so every alert at or below the highest handled was handled, on this
    /// stream or an earlier one.
    fn take(&mut self, alert: Alert) -> bool {
        if alert.alert_seq <= self.handled {
            return false;
        }
        self.handled = alert.alert_seq;
        if alert.alerted_at_unix_ms < self.launched_at_ms {
            return false;
        }
        self.group.push(alert);
        true
    }

    /// Ends the group: a notice for each of its first [`MAX_SHOWN`] alerts,
    /// then one counting the rest.
    fn end(&mut self) -> Vec<Notice> {
        let group = std::mem::take(&mut self.group);
        let mut notices: Vec<Notice> = group
            .iter()
            .take(MAX_SHOWN)
            .map(|alert| Notice {
                id: format!("{INCIDENT_PREFIX}{}", alert.incident_id),
                body: alert_body(alert),
            })
            .collect();
        let rest = group.len().saturating_sub(MAX_SHOWN);
        if rest > 0 {
            self.summaries += 1;
            notices.push(Notice {
                id: format!("summary/{}", self.summaries),
                body: if rest == 1 {
                    "1 more incident".to_string()
                } else {
                    format!("{rest} more incidents")
                },
            });
        }
        notices
    }
}

/// How a stream ended.
#[derive(Debug, PartialEq, Eq)]
enum Relayed {
    /// The shutdown: nothing more is shown.
    Shutdown,
    /// The stream ended or broke, after its Bookmark or before it.
    Ended { bookmarked: bool },
}

/// Relays one stream into groups and shows each as it ends. The snapshot is one
/// group, ended by its Bookmark; after it, an alert opens a group that takes
/// every alert of the next `window`. The group open when the stream ends is
/// shown as it stands.
async fn relay<S>(
    frames: &mut S,
    groups: &mut Groups,
    window: Duration,
    shutdown: &CancellationToken,
    show: &impl Fn(Notice),
) -> Relayed
where
    S: Stream<Item = Result<AlertEventFeedFrame, tonic::Status>> + Unpin,
{
    let mut bookmarked = false;
    let mut closes: Option<Instant> = None;
    loop {
        let frame = tokio::select! {
            biased;
            _ = shutdown.cancelled() => return Relayed::Shutdown,
            _ = tokio::time::sleep_until(closes.unwrap_or_else(Instant::now)), if closes.is_some() => {
                closes = None;
                groups.end().into_iter().for_each(show);
                continue;
            }
            frame = frames.next() => frame,
        };
        let frame = match frame {
            Some(Ok(frame)) => frame,
            Some(Err(err)) => {
                tracing::warn!(%err, "alert feed stream error");
                break;
            }
            None => break,
        };
        match frame.r#type() {
            FrameType::Bookmark => {
                bookmarked = true;
                groups.end().into_iter().for_each(show);
            }
            FrameType::Alert => {
                let Some(alert) = frame.alert else { continue };
                if groups.take(alert) && bookmarked && closes.is_none() {
                    closes = Some(Instant::now() + window);
                }
            }
            FrameType::Unspecified => {}
        }
    }
    groups.end().into_iter().for_each(show);
    Relayed::Ended { bookmarked }
}

/// Listens to the sidecar's alert feed for the app's lifetime, until
/// [`AppState::shutdown`] is cancelled, showing each alert raised from
/// `launched_at_ms` on through `show`, once in this process. A feed that ends
/// after its Bookmark reopens at once, one that fails before it with capped
/// backoff.
pub fn watch<R: Runtime>(
    app: AppHandle<R>,
    launched_at_ms: i64,
    show: impl Fn(Notice) + Send + 'static,
) -> impl Future<Output = ()> {
    /// First retry delay; doubles each failure up to `MAX_BACKOFF`.
    const BASE_BACKOFF: Duration = Duration::from_millis(500);
    const MAX_BACKOFF: Duration = Duration::from_secs(10);

    async move {
        let shutdown = app.state::<AppState>().shutdown.clone();
        let mut groups = Groups::new(launched_at_ms);
        let mut backoff = BASE_BACKOFF;
        loop {
            let state = app.state::<AppState>();
            let open = tokio::select! {
                biased;
                _ = shutdown.cancelled() => return,
                open = state.sidecar.subscribe_alert_event_feed() => open,
            };
            let bookmarked = match open {
                Ok(mut frames) => {
                    match relay(&mut frames, &mut groups, GROUP_WINDOW, &shutdown, &show).await {
                        Relayed::Shutdown => return,
                        Relayed::Ended { bookmarked } => bookmarked,
                    }
                }
                Err(err) => {
                    tracing::warn!(%err, ?backoff, "alert feed failed to open; retrying");
                    false
                }
            };
            let delay = if bookmarked {
                backoff = BASE_BACKOFF;
                tracing::info!("alert feed ended; reconnecting");
                BASE_BACKOFF
            } else {
                let delay = backoff;
                backoff = (backoff * 2).min(MAX_BACKOFF);
                delay
            };
            tokio::select! {
                biased;
                _ = shutdown.cancelled() => return,
                _ = tokio::time::sleep(delay) => {}
            }
        }
    }
}

/// A press of the notification `id`: surfaces a window and opens its incidents
/// dialog on the incident the id names, or on none for a summary. On the
/// blocking pool, since it may build a window.
pub fn pressed<R: Runtime>(app: &AppHandle<R>, id: &str) -> tauri::async_runtime::JoinHandle<()> {
    let app = app.clone();
    let incident = incident_of(id).map(str::to_string);
    tauri::async_runtime::spawn_blocking(move || {
        let state = app.state::<AppState>();
        if let Err(err) = state
            .window_manager
            .open_incident(&app, incident.as_deref())
        {
            // coverage:ignore-start a window build fails only on a real runtime
            tracing::warn!(%err, "failed to open a pressed notification's incident");
            // coverage:ignore-stop
        }
    })
}

/// The notices the plugin showed, by the integer it reports a press with: the
/// newest [`MAX_PRESSABLE`], so a press of an older one opens nothing.
#[cfg(not(target_os = "macos"))]
#[derive(Default)]
pub struct PluginNotices(Mutex<PluginNoticesInner>);

#[cfg(not(target_os = "macos"))]
#[derive(Default)]
struct PluginNoticesInner {
    last: i32,
    notices: VecDeque<(i32, String)>,
}

#[cfg(not(target_os = "macos"))]
impl PluginNotices {
    /// A new plugin id for the notice `notice_id`, from 1.
    pub fn add(&self, notice_id: &str) -> i32 {
        let mut inner = self.0.lock().unwrap_or_else(|p| p.into_inner());
        inner.last = inner.last.checked_add(1).unwrap_or(1);
        let id = inner.last;
        inner.notices.push_back((id, notice_id.to_string()));
        if inner.notices.len() > MAX_PRESSABLE {
            inner.notices.pop_front();
        }
        id
    }

    /// The notice the plugin showed as `plugin_id`, if it is still held.
    fn notice(&self, plugin_id: i32) -> Option<String> {
        let inner = self.0.lock().unwrap_or_else(|p| p.into_inner());
        inner
            .notices
            .iter()
            .find(|(id, _)| *id == plugin_id)
            .map(|(_, notice)| notice.clone())
    }
}

/// The notice an action on a plugin notification presses: a click on its
/// body, which the plugin reports as `tap`, of one `notices` holds.
#[cfg(not(target_os = "macos"))]
fn pressed_notice(
    notices: &PluginNotices,
    performed: &tauri_plugin_notification::ActionPerformed,
) -> Option<String> {
    if performed.action_id() != "tap" {
        return None;
    }
    notices.notice(performed.notification()?.id())
}

/// The body as the plugin is handed it. A Linux notification server may read
/// a body as markup, where a label could draw a link or bold text, so `&`, `<`
/// and `>` are escaped, whatever markup the server says it reads.
#[cfg(target_os = "linux")]
fn plugin_body(body: &str) -> String {
    body.replace('&', "&amp;")
        .replace('<', "&lt;")
        .replace('>', "&gt;")
}

/// The body as the plugin is handed it: Windows shows it as text.
#[cfg(not(any(target_os = "macos", target_os = "linux")))]
fn plugin_body(body: &str) -> String {
    body.to_string()
}

/// Hands each press of a notification the plugin showed to [`pressed`]. Called
/// in `setup`, before any notification shows: the plugin reports nothing of one
/// shown before its first handler. Windows reports a press only while the
/// notification is on screen, not from the Action Center.
// coverage:ignore-start needs the plugin, which the mock app lacks, and a notification server's click
#[cfg(not(target_os = "macos"))]
pub fn listen_for_presses<R: Runtime>(app: &AppHandle<R>) {
    use tauri_plugin_notification::NotificationExt;
    let handle = app.clone();
    // Never fails on desktop.
    let _ = app.notification().on_action(move |performed| {
        let notices = &handle.state::<AppState>().plugin_notices;
        if let Some(id) = pressed_notice(notices, performed) {
            drop(pressed(&handle, &id));
        }
    });
}
// coverage:ignore-stop

/// Shows `notice` as an OS notification.
// coverage:ignore-start needs a real notification center
pub fn show<R: Runtime>(app: &AppHandle<R>, notice: Notice) {
    #[cfg(target_os = "macos")]
    {
        let _ = app;
        macos::show(&notice);
    }
    #[cfg(not(target_os = "macos"))]
    {
        use tauri_plugin_notification::NotificationExt;
        let id = app.state::<AppState>().plugin_notices.add(&notice.id);
        let shown = app
            .notification()
            .builder()
            .id(id)
            .title(TITLE)
            .body(plugin_body(&notice.body))
            .show();
        if let Err(err) = shown {
            tracing::warn!(%err, "failed to show a notification");
        }
    }
}
// coverage:ignore-stop

#[cfg(test)]
mod tests {
    use std::sync::{Arc, Mutex};

    use super::*;
    use crate::logging::capture_thread;
    use crate::services::sidecar::testutil::FakeGrpc;
    use crate::services::sidecar::SidecarService;
    use crate::testutil::{mock_app, Scratch};

    /// The alert of incident `id`, numbered `seq` in commit order and raised
    /// at `at`.
    fn alert(id: &str, seq: i64, at: i64) -> Alert {
        Alert {
            incident_id: id.to_string(),
            target_label: "dev".to_string(),
            subject_label: format!("Deployment default/{id}"),
            signal_label: "Crash loop".to_string(),
            alerted_at_unix_ms: at,
            alert_seq: seq,
            ..Alert::default()
        }
    }

    fn alert_frame(id: &str, seq: i64, at: i64) -> AlertEventFeedFrame {
        AlertEventFeedFrame {
            r#type: FrameType::Alert as i32,
            alert: Some(alert(id, seq, at)),
        }
    }

    fn bookmark() -> AlertEventFeedFrame {
        AlertEventFeedFrame {
            r#type: FrameType::Bookmark as i32,
            alert: None,
        }
    }

    type Sent = tokio::sync::mpsc::UnboundedSender<Result<AlertEventFeedFrame, tonic::Status>>;

    /// A stream the test feeds frame by frame; dropping the sender ends it.
    fn feed() -> (
        Sent,
        impl Stream<Item = Result<AlertEventFeedFrame, tonic::Status>> + Unpin,
    ) {
        let (tx, rx) = tokio::sync::mpsc::unbounded_channel();
        let stream = futures_util::stream::unfold(rx, |mut rx| async move {
            rx.recv().await.map(|frame| (frame, rx))
        });
        (tx, Box::pin(stream))
    }

    /// Every notice shown, in order.
    #[derive(Clone, Default)]
    struct Shown(Arc<Mutex<Vec<Notice>>>);

    impl Shown {
        fn show(&self) -> impl Fn(Notice) {
            let shown = self.0.clone();
            move |notice| shown.lock().unwrap().push(notice)
        }

        fn ids(&self) -> Vec<String> {
            self.0
                .lock()
                .unwrap()
                .iter()
                .map(|n| n.id.clone())
                .collect()
        }

        fn bodies(&self) -> Vec<String> {
            self.0
                .lock()
                .unwrap()
                .iter()
                .map(|n| n.body.clone())
                .collect()
        }
    }

    #[test]
    fn alert_body_strips_control_characters_and_bounds_each_part() {
        let mut a = alert("i1", 1, 0);
        a.subject_label = format!("Deployment\n\u{1b}[31mweb\u{202e}{}", "x".repeat(10_000));
        a.target_label = format!("dev\r\n{}", "y".repeat(10_000));
        a.signal_label = "Crash\tloop".to_string();

        let body = alert_body(&a);

        assert!(!body.chars().any(char::is_control), "{body:?}");
        assert!(!body.contains('\u{202e}'));
        assert!(
            body.starts_with("Deployment[31mweb"),
            "an escape's printable tail stays as text: {body}"
        );
        assert!(body.contains(" on devyy"), "{body}");
        assert!(body.ends_with(": Crashloop"), "{body}");
        let chars = body.chars().count();
        let bound = MAX_SUBJECT_CHARS + MAX_TARGET_CHARS + MAX_SIGNAL_CHARS + " on : ".len();
        assert!(chars <= bound, "{chars}");
    }

    /// A Linux notification server may read the body as markup: a label's `<`,
    /// `>` and `&` reach it escaped, so it draws no link and no bold text.
    #[cfg(target_os = "linux")]
    #[test]
    fn a_linux_body_escapes_markup_in_labels() {
        let mut a = alert("i1", 1, 0);
        a.subject_label = r#"<a href="https://evil.example">web</a>"#.to_string();
        a.target_label = "<b>dev</b> &amp;".to_string();

        assert_eq!(
            plugin_body(&alert_body(&a)),
            r#"&lt;a href="https://evil.example"&gt;web&lt;/a&gt; on &lt;b&gt;dev&lt;/b&gt; &amp;amp;: Crash loop"#
        );
    }

    /// Windows shows the body as text, so it is handed over as it is.
    #[cfg(windows)]
    #[test]
    fn a_windows_body_is_handed_over_as_it_is() {
        assert_eq!(plugin_body("<b>web</b> & api"), "<b>web</b> & api");
    }

    /// The plugin's ids start at 1, each notice its own, and a press reaches
    /// the newest [`MAX_PRESSABLE`] alone.
    #[cfg(not(target_os = "macos"))]
    #[test]
    fn the_plugin_ids_name_the_newest_notices() {
        let notices = PluginNotices::default();
        assert_eq!(notices.add("incident/i0"), 1);
        assert_eq!(notices.notice(1).as_deref(), Some("incident/i0"));
        for n in 1..=MAX_PRESSABLE {
            notices.add(&format!("incident/i{n}"));
        }
        assert_eq!(notices.notice(1), None, "the oldest is dropped");
        assert_eq!(notices.notice(2).as_deref(), Some("incident/i1"));
        assert_eq!(notices.notice(0), None);

        let wrapped = PluginNotices::default();
        wrapped.0.lock().unwrap().last = i32::MAX;
        assert_eq!(wrapped.add("summary/1"), 1, "the ids wrap to 1");
    }

    /// A click on a plugin notification's body presses its notice; another
    /// action, or a notification it did not show, presses none.
    #[cfg(not(target_os = "macos"))]
    #[test]
    fn a_tap_on_a_plugin_notification_presses_its_notice() {
        let notices = PluginNotices::default();
        let id = notices.add("incident/i1");
        let performed = |action: &str, notification: serde_json::Value| {
            serde_json::from_value::<tauri_plugin_notification::ActionPerformed>(
                serde_json::json!({ "actionId": action, "notification": notification }),
            )
            .unwrap()
        };

        let tap = performed("tap", serde_json::json!({ "id": id }));
        assert_eq!(
            pressed_notice(&notices, &tap).as_deref(),
            Some("incident/i1")
        );
        let other = performed("snooze", serde_json::json!({ "id": id }));
        assert_eq!(pressed_notice(&notices, &other), None);
        let unknown = performed("tap", serde_json::json!({ "id": id + 1 }));
        assert_eq!(pressed_notice(&notices, &unknown), None);
        let unnamed = performed("tap", serde_json::Value::Null);
        assert_eq!(pressed_notice(&notices, &unnamed), None);
    }

    #[test]
    fn a_notice_names_its_incident_and_a_summary_none() {
        assert_eq!(incident_of("incident/i1"), Some("i1"));
        assert_eq!(incident_of("summary/1"), None);
    }

    /// A group shows its first five and counts the rest in one more; a group of
    /// six counts one.
    #[test]
    fn a_group_shows_five_then_a_summary() {
        let mut groups = Groups::new(0);
        for n in 0..40 {
            assert!(groups.take(alert(&format!("i{n}"), n + 1, 1)));
        }
        let notices = groups.end();
        assert_eq!(notices.len(), 6);
        assert_eq!(notices[0].id, "incident/i0");
        assert_eq!(notices[5].body, "35 more incidents");
        assert_eq!(notices[5].id, "summary/1");

        for n in 40..46 {
            groups.take(alert(&format!("i{n}"), n + 1, 1));
        }
        let notices = groups.end();
        assert_eq!(notices[5].body, "1 more incident");
        assert_eq!(
            notices[5].id, "summary/2",
            "each summary has an id of its own"
        );
        assert!(groups.end().is_empty());
    }

    /// An alert raised before the launch is skipped, one raised at it or after is
    /// taken, and none is taken twice.
    #[test]
    fn a_group_takes_each_alert_raised_since_the_launch_once() {
        let mut groups = Groups::new(1000);
        assert!(!groups.take(alert("before", 1, 999)));
        assert!(groups.take(alert("at", 2, 1500)));
        assert!(!groups.take(alert("at", 2, 1500)));
        groups.end();
        assert!(
            !groups.take(alert("at", 2, 1500)),
            "shown once in the process"
        );
        assert!(
            groups.take(alert("after", 3, 1200)),
            "committed later, though its clock stepped back"
        );
    }

    /// An alert a reconnect's snapshot no longer holds, and that comes back
    /// into a later one as a newer incident is deleted, is not shown again.
    #[tokio::test(start_paused = true)]
    async fn an_older_alert_reentering_a_snapshot_is_not_shown_again() {
        let shown = Shown::default();
        let shutdown = CancellationToken::new();
        let mut groups = Groups::new(0);
        let show = shown.show();
        let window = Duration::from_secs(2);
        for snapshot in [
            vec![("old", 3), ("i4", 4)],
            vec![("i4", 4)],
            vec![("old", 3)],
        ] {
            let (tx, mut frames) = feed();
            for (id, seq) in snapshot {
                tx.send(Ok(alert_frame(id, seq, 1))).unwrap();
            }
            tx.send(Ok(bookmark())).unwrap();
            drop(tx);
            relay(&mut frames, &mut groups, window, &shutdown, &show).await;
        }

        assert_eq!(shown.ids(), ["incident/old", "incident/i4"]);
    }

    /// The snapshot is one group, ended by its Bookmark; 40 alerts after it,
    /// in one read or over several inside the window, are one group too, and
    /// an alert after the window opens another.
    #[tokio::test(start_paused = true)]
    async fn forty_alerts_raise_five_notifications_and_a_summary() {
        let (tx, mut frames) = feed();
        let shown = Shown::default();
        let shutdown = CancellationToken::new();
        let mut groups = Groups::new(0);
        let window = Duration::from_secs(2);
        let relayed = {
            let shutdown = shutdown.clone();
            let shown = shown.clone();
            tokio::spawn(async move {
                relay(&mut frames, &mut groups, window, &shutdown, &shown.show()).await
            })
        };

        for n in 0..7 {
            tx.send(Ok(alert_frame(&format!("s{n}"), n + 1, 1)))
                .unwrap();
        }
        tx.send(Ok(bookmark())).unwrap();
        tokio::time::sleep(Duration::from_millis(10)).await;
        assert_eq!(shown.bodies().len(), 6, "the snapshot's group");
        assert_eq!(shown.bodies()[5], "2 more incidents");

        for n in 0..40 {
            tx.send(Ok(alert_frame(&format!("l{n}"), n + 8, 1)))
                .unwrap();
            tokio::time::sleep(Duration::from_millis(40)).await;
        }
        assert_eq!(shown.bodies().len(), 6, "the live group is open");
        tokio::time::sleep(Duration::from_millis(500)).await;
        assert_eq!(shown.bodies().len(), 12);
        assert_eq!(shown.bodies()[11], "35 more incidents");

        tx.send(Ok(alert_frame("late", 48, 1))).unwrap();
        tokio::time::sleep(Duration::from_secs(3)).await;
        assert_eq!(
            shown.ids().last().unwrap(),
            "incident/late",
            "a group of its own"
        );
        shutdown.cancel();
        assert_eq!(relayed.await.unwrap(), Relayed::Shutdown);
    }

    /// A disconnect 1 second into a live group of 8 shows it at once, 5 and a
    /// summary of 3; a snapshot cut short before its Bookmark shows what
    /// arrived. Neither counts as bookmarked but the first.
    #[tokio::test(start_paused = true)]
    async fn a_disconnect_shows_the_open_group_as_it_stands() {
        let shown = Shown::default();
        let shutdown = CancellationToken::new();
        let mut groups = Groups::new(0);
        let window = Duration::from_secs(2);

        let (tx, mut frames) = feed();
        tx.send(Ok(AlertEventFeedFrame::default())).unwrap(); // a type this build lacks
        tx.send(Ok(bookmark())).unwrap();
        for n in 0..8 {
            tx.send(Ok(alert_frame(&format!("i{n}"), n + 1, 1)))
                .unwrap();
        }
        let relayed = {
            let shutdown = shutdown.clone();
            let show = shown.show();
            async move {
                let first = {
                    let first = relay(&mut frames, &mut groups, window, &shutdown, &show);
                    tokio::pin!(first);
                    tokio::select! {
                        _ = &mut first => unreachable!("the feed is open"),
                        _ = tokio::time::sleep(Duration::from_secs(1)) => {}
                    }
                    drop(tx);
                    first.await
                };
                let (cut, mut frames) = feed();
                cut.send(Ok(alert_frame("i3", 4, 1))).unwrap();
                cut.send(Ok(alert_frame("j0", 9, 1))).unwrap();
                cut.send(Err(tonic::Status::unavailable("gone"))).unwrap();
                let second = relay(&mut frames, &mut groups, window, &shutdown, &show).await;
                (first, second)
            }
        };
        let (_capture, _guard) = capture_thread();

        let (first, second) = relayed.await;

        assert_eq!(first, Relayed::Ended { bookmarked: true });
        assert_eq!(second, Relayed::Ended { bookmarked: false });
        let bodies = shown.bodies();
        assert_eq!(bodies.len(), 7);
        assert_eq!(bodies[5], "3 more incidents");
        assert_eq!(shown.ids()[6], "incident/j0", "the replayed i3 is handled");
    }

    /// An alert raised while the sidecar restarts is notified once the listener
    /// resubscribes, and one already shown is not shown again; alerts raised
    /// before the launch raise none. The listener ends on quit.
    #[tokio::test]
    async fn the_listener_resubscribes_and_shows_each_alert_once() {
        let fake = FakeGrpc {
            alerts: vec![
                Ok(alert_frame("old", 1, 10)),
                Ok(alert_frame("i1", 2, 2000)),
                Ok(bookmark()),
            ],
            ..FakeGrpc::default()
        };
        let mut calls = fake.calls.subscribe();
        let served = fake.serve();
        let scratch = Scratch::new();
        let app = mock_app(
            scratch.path(),
            SidecarService::dialing(served.endpoint.clone()),
        );
        let shown = Shown::default();

        let listener = tokio::spawn(watch(app.handle().clone(), 1000, shown.show()));
        calls.wait_for(|calls| *calls >= 3).await.unwrap();
        app.state::<AppState>().shutdown.cancel();
        listener.await.unwrap();

        assert_eq!(shown.ids(), ["incident/i1"]);
    }

    /// A quit ends the listener while its feed is open.
    #[tokio::test]
    async fn the_listener_ends_on_quit_with_its_feed_open() {
        let fake = FakeGrpc {
            alerts: vec![Ok(alert_frame("i1", 1, 1)), Ok(bookmark())],
            hold_alerts: true,
            ..FakeGrpc::default()
        };
        let served = fake.serve();
        let scratch = Scratch::new();
        let app = mock_app(
            scratch.path(),
            SidecarService::dialing(served.endpoint.clone()),
        );
        let (shown, mut snapshot) = tokio::sync::mpsc::unbounded_channel();

        let listener = tokio::spawn(watch(app.handle().clone(), 0, move |notice| {
            shown.send(notice).unwrap();
        }));
        snapshot.recv().await.unwrap();
        app.state::<AppState>().shutdown.cancel();
        listener.await.unwrap();
    }

    /// A press with no window open builds one, its press waiting on the
    /// incident its id names, or on none for a summary.
    #[tokio::test]
    async fn a_press_opens_the_incident_it_names() {
        let scratch = Scratch::new();
        let app = mock_app(scratch.path(), SidecarService::without_process(None));
        let state = app.state::<AppState>();

        pressed(app.handle(), "incident/0193").await.unwrap();
        let built = app.webview_windows().into_keys().next().unwrap();
        assert_eq!(
            state.window_manager.take_press(&built),
            Some(crate::window_manager::Press {
                incident_id: Some("0193".to_string())
            })
        );

        let empty_scratch = Scratch::new();
        let empty = mock_app(empty_scratch.path(), SidecarService::without_process(None));
        pressed(empty.handle(), "summary/1").await.unwrap();
        let built = empty.webview_windows().into_keys().next().unwrap();
        assert_eq!(
            empty.state::<AppState>().window_manager.take_press(&built),
            Some(crate::window_manager::Press { incident_id: None }),
            "a summary opens the dialog with nothing selected"
        );
    }

    /// With no sidecar, each open fails and waits out the next backoff; the
    /// shutdown ends the listener in the middle of one.
    #[tokio::test(start_paused = true)]
    async fn the_listener_backs_off_until_shutdown() {
        let scratch = Scratch::new();
        let app = mock_app(scratch.path(), SidecarService::without_process(None));
        let (capture, _guard) = capture_thread();

        let listener = tokio::spawn(watch(app.handle().clone(), 0, |_| {}));
        tokio::time::sleep(Duration::from_secs(8)).await;
        app.state::<AppState>().shutdown.cancel();
        listener.await.unwrap();

        let out = capture.text();
        assert_eq!(out.matches("alert feed failed to open").count(), 5, "{out}");
        assert!(out.contains("backoff=8s"), "{out}");
    }

    /// A feed that ends before its Bookmark backs off before the next.
    #[tokio::test]
    async fn a_feed_ended_before_its_bookmark_backs_off() {
        let fake = FakeGrpc {
            alerts: vec![Err(tonic::Status::internal("broken"))],
            ..FakeGrpc::default()
        };
        let mut calls = fake.calls.subscribe();
        let served = fake.serve();
        let scratch = Scratch::new();
        let app = mock_app(
            scratch.path(),
            SidecarService::dialing(served.endpoint.clone()),
        );
        let (capture, _guard) = capture_thread();

        let listener = tokio::spawn(watch(app.handle().clone(), 0, |_| {}));
        calls.wait_for(|calls| *calls >= 2).await.unwrap();
        app.state::<AppState>().shutdown.cancel();
        listener.await.unwrap();

        assert!(capture.text().contains("alert feed stream error"));
    }
}
