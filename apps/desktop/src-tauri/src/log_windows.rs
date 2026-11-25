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

//! Log windows (`logs-N`) and the app windows that opened them.
//!
//! [`LogWindows`] holds the pairing behind one lock: each app window's current
//! page and the channel it subscribed with, and each log window's entry. Every frame
//! is sent under that lock, with the change it reports. Its transitions return
//! what the wiring does next, and the wiring builds, surfaces and destroys
//! windows after the lock is released.
//!
//! Every lookup starts from the caller's label, read off its `Webview`, so a
//! log window reaches its own app window and an app window its own log
//! windows. An app window's call also names its page, which ends when the
//! window is destroyed or its page reloads, so a request from a page that is
//! gone is refused. A build that page started is still the window's: it
//! finishes, and the next page hears `opened`.
//!
//! The host destroys the log windows it closes, never `close()`s them: a page
//! may cancel a close, and a log window must not outlive its app window.

use std::collections::BTreeMap;
use std::sync::{Arc, Mutex, MutexGuard};

use serde::Serialize;
use serde_json::value::RawValue;
use serde_json::Value;
use tauri::ipc::Channel;
use tauri::{AppHandle, Manager, Runtime, WebviewWindow};
use tokio::sync::watch;
use tracing::warn;

use crate::error::{AppError, Result};
use crate::state::AppState;
use crate::window_manager::{is_app_label, is_logs_label, on_blocking_pool};
use crate::window_title;

/// The most bytes a cluster, chat or call id holds.
const MAX_ID_BYTES: usize = 256;

/// The most bytes a grep holds.
const MAX_GREP_BYTES: usize = 4 * 1024;

/// The most bytes one viewport push holds, serialized.
const MAX_PUSH_BYTES: usize = 256 * 1024;

/// The most parts a title holds, and the most bytes in one.
const MAX_TITLE_PARTS: usize = 3;
const MAX_TITLE_PART_BYTES: usize = 4 * 1024;

/// The call a log window shows: one tool call of one chat on one cluster.
#[derive(Clone, Debug, PartialEq, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct Call {
    pub cluster_id: String,
    pub chat_id: String,
    pub call_id: String,
}

/// A log window's push, serialized once and kept as it is forwarded.
#[derive(Serialize)]
struct Push<'a> {
    generation: u64,
    grep: Option<&'a str>,
    viewport: &'a Value,
}

/// One log window as its app window sees it.
#[derive(Clone, Debug, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct WindowInfo {
    label: String,
    #[serde(flatten)]
    call: Call,
    focus_seq: u64,
    push: Option<Arc<RawValue>>,
}

/// What the host tells an app window, on the channel that window made. Each
/// serializes as one key.
#[derive(Debug, Serialize)]
#[serde(rename_all = "camelCase", rename_all_fields = "camelCase")]
pub enum Frame {
    /// First on every channel: the caller's live log windows.
    Snapshot {
        windows: Vec<WindowInfo>,
    },
    /// A log window's build finished.
    Opened {
        window: WindowInfo,
    },
    /// A live log window's page pushed, or reloaded (`push` null).
    Viewport {
        label: String,
        push: Option<Arc<RawValue>>,
    },
    Focused {
        label: String,
        focus_seq: u64,
    },
    /// A live log window was removed by a close or its own destroy.
    Closed {
        label: String,
    },
    /// A log window asked its app window to show the chat it came from.
    ShowChat {
        chat_id: String,
    },
}

/// `log_window_subscribe`'s answer.
#[derive(Debug, Serialize)]
pub struct Subscribed {
    pub page: u64,
}

/// `log_window_view`'s answer: what the log window draws.
#[derive(Debug, PartialEq, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct View {
    chat_id: String,
    call_id: String,
    grep: Option<String>,
    generation: u64,
}

/// A build's outcome, as every open on that call hears it.
#[derive(Clone, Debug)]
pub enum Outcome {
    Pending,
    Opened(String),
    Failed(String),
    Cancelled,
}

/// What an open does after [`LogWindows::reserve`].
#[derive(Debug)]
pub enum Reserved {
    /// The call's window is live: surface it.
    Surface(String),
    /// The call's window is building: wait for that build.
    Wait(watch::Receiver<Outcome>),
    /// The call has no window: build this one.
    Build(String),
}

enum State {
    /// Its window is being built; the sender carries the outcome to every
    /// open waiting on it.
    Building(watch::Sender<Outcome>),
    Live,
}

struct Entry {
    app: String,
    state: State,
    call: Call,
    grep: Option<String>,
    generation: u64,
    focus_seq: u64,
    /// The last push, serialized once and shared by every frame carrying it.
    push: Option<Arc<RawValue>>,
}

/// One load of an app window, and the channel it subscribed with.
struct Page {
    page: u64,
    channel: Channel<Frame>,
}

/// Every app window's page and every log window's entry, for the whole app.
/// An entry names its app window in `app`.
#[derive(Default)]
struct Registry {
    /// By app-window label.
    pages: BTreeMap<String, Page>,
    /// By log-window label. A label is never reused.
    entries: BTreeMap<String, Entry>,
    last_page: u64,
    last_focus: u64,
}

impl Entry {
    fn live(&self) -> bool {
        matches!(self.state, State::Live)
    }

    fn info(&self, label: &str) -> WindowInfo {
        WindowInfo {
            label: label.to_string(),
            call: self.call.clone(),
            focus_seq: self.focus_seq,
            push: self.push.clone(),
        }
    }
}

impl Registry {
    /// Sends `frame` to app window `app`'s channel, if it has a page.
    fn send(&self, app: &str, frame: Frame) {
        if let Some(page) = self.pages.get(app) {
            if let Err(err) = page.channel.send(frame) {
                warn!(%err, window = app, "failed to send a log-window frame");
            }
        }
    }

    /// Sends `frame` to log window `label`'s app window, if the window is
    /// live. A building one is unseen until `opened`.
    fn send_if_live(&self, label: &str, frame: Frame) {
        if let Some(entry) = self.entries.get(label).filter(|entry| entry.live()) {
            self.send(&entry.app, frame);
        }
    }

    /// Refuses a call from anything but app window `app`'s current page.
    fn check_page(&self, app: &str, page: u64) -> Result<()> {
        check_app(app)?;
        match self.pages.get(app) {
            Some(current) if current.page == page => Ok(()),
            _ => Err(AppError::Refused("an ended page")),
        }
    }

    fn next_focus(&mut self) -> u64 {
        self.last_focus += 1;
        self.last_focus
    }

    /// Log window `label`'s entry, for a call it made.
    fn entry(&self, label: &str) -> Result<&Entry> {
        check_logs(label)?;
        self.entries
            .get(label)
            .ok_or(AppError::Refused("no such log window"))
    }

    fn entry_mut(&mut self, label: &str) -> Result<&mut Entry> {
        check_logs(label)?;
        self.entries
            .get_mut(label)
            .ok_or(AppError::Refused("no such log window"))
    }

    /// App window `app`'s entry on `call_id`, with its label.
    fn on_call(&self, app: &str, call_id: &str) -> Option<(&String, &Entry)> {
        self.entries
            .iter()
            .find(|(_, entry)| entry.app == app && entry.call.call_id == call_id)
    }

    /// Removes `label`'s entry. A building one is cancelled; a live one is
    /// reported closed. Whether it was live.
    fn remove(&mut self, label: &str) -> bool {
        let Some(entry) = self.entries.remove(label) else {
            return false;
        };
        match entry.state {
            State::Building(outcome) => {
                outcome.send_replace(Outcome::Cancelled);
                false
            }
            State::Live => {
                let label = label.to_string();
                self.send(&entry.app, Frame::Closed { label });
                true
            }
        }
    }

    /// Removes every entry of app window `app`'s. Returns the live ones'
    /// labels.
    fn remove_all(&mut self, app: &str) -> Vec<String> {
        let labels: Vec<String> = self
            .entries
            .iter()
            .filter(|(_, entry)| entry.app == app)
            .map(|(label, _)| label.clone())
            .collect();
        labels
            .into_iter()
            .filter(|label| self.remove(label))
            .collect()
    }
}

/// Refuses a caller that is not an app window.
fn check_app(label: &str) -> Result<()> {
    if is_app_label(label) {
        Ok(())
    } else {
        Err(AppError::Refused("not an app window"))
    }
}

/// Refuses a caller that is not a log window.
fn check_logs(label: &str) -> Result<()> {
    if is_logs_label(label) {
        Ok(())
    } else {
        Err(AppError::Refused("not a log window"))
    }
}

/// Refuses `text` over `max` bytes.
fn check_len(text: &str, max: usize, what: &'static str) -> Result<()> {
    if text.len() <= max {
        Ok(())
    } else {
        Err(AppError::Refused(what))
    }
}

fn check_grep(grep: Option<&str>) -> Result<()> {
    check_len(
        grep.unwrap_or_default(),
        MAX_GREP_BYTES,
        "a grep over its bound",
    )
}

/// The pairing of log windows to app windows. See the module docs.
#[derive(Default)]
pub struct LogWindows {
    registry: Mutex<Registry>,
}

impl LogWindows {
    fn lock(&self) -> MutexGuard<'_, Registry> {
        self.registry.lock().unwrap_or_else(|p| p.into_inner())
    }

    /// Mints a page for app window `app`, replacing its earlier page and
    /// channel, and sends a snapshot of its live log windows down `channel`.
    pub fn subscribe(&self, app: &str, channel: Channel<Frame>) -> Result<u64> {
        check_app(app)?;
        let mut registry = self.lock();
        registry.last_page += 1;
        let page = registry.last_page;
        let windows = registry
            .entries
            .iter()
            .filter(|(_, entry)| entry.app == app && entry.live())
            .map(|(label, entry)| entry.info(label))
            .collect();
        registry
            .pages
            .insert(app.to_string(), Page { page, channel });
        registry.send(app, Frame::Snapshot { windows });
        Ok(page)
    }

    /// Reserves the log window for `call`, as app window `app`'s page `page`
    /// asks. `mint` names a new window.
    pub fn reserve(
        &self,
        app: &str,
        page: u64,
        call: Call,
        grep: Option<String>,
        mint: impl FnOnce() -> String,
    ) -> Result<Reserved> {
        for id in [&call.cluster_id, &call.chat_id, &call.call_id] {
            check_len(id, MAX_ID_BYTES, "an id over its bound")?;
        }
        check_grep(grep.as_deref())?;
        let mut registry = self.lock();
        registry.check_page(app, page)?;
        if let Some((label, entry)) = registry.on_call(app, &call.call_id) {
            return Ok(match &entry.state {
                State::Live => Reserved::Surface(label.clone()),
                State::Building(outcome) => Reserved::Wait(outcome.subscribe()),
            });
        }
        let label = mint();
        let focus_seq = registry.next_focus();
        let (outcome, _) = watch::channel(Outcome::Pending);
        let entry = Entry {
            app: app.to_string(),
            state: State::Building(outcome),
            call,
            grep,
            generation: 0,
            focus_seq,
            push: None,
        };
        registry.entries.insert(label.clone(), entry);
        Ok(Reserved::Build(label))
    }

    /// `label`'s build finished with a window. Whether its entry is still
    /// there, now live; when not, a close or a destroy cancelled it, and the
    /// wiring destroys the window.
    pub fn built(&self, label: &str) -> bool {
        let mut registry = self.lock();
        let Some(entry) = registry.entries.get_mut(label) else {
            return false;
        };
        if let State::Building(outcome) = std::mem::replace(&mut entry.state, State::Live) {
            outcome.send_replace(Outcome::Opened(label.to_string()));
        }
        let (app, window) = (entry.app.clone(), entry.info(label));
        registry.send(&app, Frame::Opened { window });
        true
    }

    /// `label`'s build failed, or built nothing as a quit began: its entry is
    /// removed, and every open waiting on it hears `error`.
    pub fn failed(&self, label: &str, error: &AppError) {
        let mut registry = self.lock();
        if let Some(Entry {
            state: State::Building(outcome),
            ..
        }) = registry.entries.remove(label)
        {
            outcome.send_replace(match error {
                AppError::Cancelled => Outcome::Cancelled,
                error => Outcome::Failed(error.to_string()),
            });
        }
    }

    /// Removes every log window of app window `app`'s, as its page `page`
    /// asks. Returns the live ones' labels, for the wiring to destroy.
    pub fn close_all(&self, app: &str, page: u64) -> Result<Vec<String>> {
        let mut registry = self.lock();
        registry.check_page(app, page)?;
        Ok(registry.remove_all(app))
    }

    /// The window `label` was destroyed. Returns the labels of the live log
    /// windows the wiring destroys with it.
    ///
    /// An app window's destroy ends whatever page its label holds. A rebuilt
    /// `main` cannot have subscribed yet: Tauri runs the old window's `Destroyed`
    /// on the event loop before a rebuild queued from another thread creates
    /// a window under the label.
    pub fn destroyed(&self, label: &str) -> Vec<String> {
        let mut registry = self.lock();
        if registry.entries.contains_key(label) {
            registry.remove(label);
            return Vec::new();
        }
        // The page goes first, so the removals below send nothing.
        registry.pages.remove(label);
        registry.remove_all(label)
    }

    /// A page load began in the window `label`. A log window's generation
    /// steps and its push clears; an app window's page ends, and its log
    /// windows stay for the next subscribe to list.
    pub fn page_started(&self, label: &str) {
        let mut registry = self.lock();
        let Some(entry) = registry.entries.get_mut(label) else {
            registry.pages.remove(label);
            return;
        };
        entry.generation += 1;
        entry.push = None;
        let frame = Frame::Viewport {
            label: label.to_string(),
            push: None,
        };
        registry.send_if_live(label, frame);
    }

    /// The window `label` took focus.
    pub fn focused(&self, label: &str) {
        let mut guard = self.lock();
        // Through the guard, so the entry and the counter borrow separate fields.
        let registry = &mut *guard;
        let Some(entry) = registry.entries.get_mut(label) else {
            return;
        };
        registry.last_focus += 1;
        entry.focus_seq = registry.last_focus;
        let focus_seq = entry.focus_seq;
        let frame = Frame::Focused {
            label: label.to_string(),
            focus_seq,
        };
        registry.send_if_live(label, frame);
    }

    /// The live log window of app window `app`'s on `call_id`, for its page
    /// `page` to surface. A building one surfaces when it is built.
    pub fn focus_target(&self, app: &str, page: u64, call_id: &str) -> Result<Option<String>> {
        let registry = self.lock();
        registry.check_page(app, page)?;
        let target = registry.on_call(app, call_id);
        Ok(target
            .filter(|(_, entry)| entry.live())
            .map(|(label, _)| label.clone()))
    }

    /// What log window `label` draws: the grep of its last push, else its
    /// open's, so a reload keeps the toolbar's.
    pub fn view(&self, label: &str) -> Result<View> {
        let registry = self.lock();
        let entry = registry.entry(label)?;
        Ok(View {
            chat_id: entry.call.chat_id.clone(),
            call_id: entry.call.call_id.clone(),
            grep: entry.grep.clone(),
            generation: entry.generation,
        })
    }

    /// Keeps log window `label`'s push, made by its page load `generation`,
    /// and forwards it once the window is live. `viewport` stays opaque.
    pub fn viewport(
        &self,
        label: &str,
        generation: u64,
        grep: Option<String>,
        viewport: Value,
    ) -> Result<()> {
        check_grep(grep.as_deref())?;
        let push = serde_json::value::to_raw_value(&Push {
            generation,
            grep: grep.as_deref(),
            viewport: &viewport,
        })?;
        if push.get().len() > MAX_PUSH_BYTES {
            return Err(AppError::Refused("a push over its bound"));
        }
        let mut registry = self.lock();
        let entry = registry.entry_mut(label)?;
        if entry.generation != generation {
            return Err(AppError::Refused("a push from an earlier page load"));
        }
        let push: Arc<RawValue> = Arc::from(push);
        entry.grep = grep;
        entry.push = Some(push.clone());
        let frame = Frame::Viewport {
            label: label.to_string(),
            push: Some(push),
        };
        registry.send_if_live(label, frame);
        Ok(())
    }

    /// Log window `label`'s native title for `parts`, refused past its bounds.
    pub fn title(&self, label: &str, parts: &[String]) -> Result<String> {
        // The caller is checked like any other log-window call; the title
        // needs nothing from its entry.
        self.lock().entry(label)?;
        if parts.len() > MAX_TITLE_PARTS {
            return Err(AppError::Refused("a title with too many parts"));
        }
        for part in parts {
            check_len(part, MAX_TITLE_PART_BYTES, "a title part over its bound")?;
        }
        Ok(window_title::build(parts))
    }

    /// Asks log window `label`'s app window to show its chat. Returns the app
    /// window's label, for the wiring to surface.
    pub fn show_chat(&self, label: &str) -> Result<String> {
        let registry = self.lock();
        let entry = registry.entry(label)?;
        let (app, chat_id) = (entry.app.clone(), entry.call.chat_id.clone());
        registry.send_if_live(label, Frame::ShowChat { chat_id });
        Ok(app)
    }
}

/// What a build waited on yields: the label, the build's error, or
/// [`AppError::Cancelled`], which a dropped sender reads as too.
pub async fn wait(mut outcome: watch::Receiver<Outcome>) -> Result<String> {
    let done = outcome
        .wait_for(|outcome| !matches!(outcome, Outcome::Pending))
        .await;
    match done.as_deref() {
        Ok(Outcome::Opened(label)) => Ok(label.clone()),
        Ok(Outcome::Failed(message)) => Err(AppError::Build(message.clone())),
        _ => Err(AppError::Cancelled),
    }
}

/// Opens or surfaces app window `caller`'s log window on `call`, as its page
/// `page` asks. Returns the label once the window exists.
pub async fn open<R: Runtime>(
    app: AppHandle<R>,
    caller: &str,
    page: u64,
    call: Call,
    grep: Option<String>,
) -> Result<String> {
    let state = app.state::<AppState>();
    let manager = &state.window_manager;
    let mint = || manager.next_logs_label();
    match state.log_windows.reserve(caller, page, call, grep, mint)? {
        Reserved::Surface(label) => match manager.surface_window(&app, &label)? {
            true => Ok(label),
            // Destroyed before it could be shown.
            false => Err(AppError::Cancelled),
        },
        Reserved::Wait(outcome) => wait(outcome).await,
        Reserved::Build(label) => build(app.clone(), label, caller.to_string()).await,
    }
}

/// Builds the reserved log window `label` on the blocking pool, and settles
/// its entry with the result.
async fn build<R: Runtime>(app: AppHandle<R>, label: String, anchor: String) -> Result<String> {
    // A panicked build fails like any other, so no entry is left building.
    let built = {
        let (app, label) = (app.clone(), label.clone());
        on_blocking_pool(move || {
            let manager = &app.state::<AppState>().window_manager;
            manager.open_log_window(&app, &label, &anchor)
        })
        .await
    };
    let log_windows = &app.state::<AppState>().log_windows;
    match built {
        Ok(Some(_)) => {
            if log_windows.built(&label) {
                return Ok(label);
            }
            // A close or a destroy cancelled it while it built. Its own
            // destroy has already taken the window, so look it up again.
            destroy_labels(&app, vec![label]);
            Err(AppError::Cancelled)
        }
        Ok(None) => {
            log_windows.failed(&label, &AppError::Cancelled);
            Err(AppError::Cancelled)
        }
        Err(err) => {
            log_windows.failed(&label, &err);
            Err(err)
        }
    }
}

/// Removes and destroys app window `caller`'s log windows, as its page `page`
/// asks.
pub fn close_all<R: Runtime>(app: &AppHandle<R>, caller: &str, page: u64) -> Result<()> {
    let labels = app
        .state::<AppState>()
        .log_windows
        .close_all(caller, page)?;
    destroy_labels(app, labels);
    Ok(())
}

/// The window `label` was destroyed: the registry forgets it, and its live log
/// windows are destroyed with it.
pub fn destroyed<R: Runtime>(app: &AppHandle<R>, label: &str) {
    if let Some(state) = app.try_state::<AppState>() {
        let labels = state.log_windows.destroyed(label);
        destroy_labels(app, labels);
    }
}

fn destroy_labels<R: Runtime>(app: &AppHandle<R>, labels: Vec<String>) {
    for label in labels {
        if let Some(window) = app.get_webview_window(&label) {
            destroy_window(&window);
        }
    }
}

/// Destroys `window` without asking its page, which could cancel a close.
fn destroy_window<R: Runtime>(window: &WebviewWindow<R>) {
    if let Err(err) = window.destroy() {
        // coverage:ignore-start the mock runtime's destroy never fails
        warn!(%err, window = window.label(), "failed to destroy a log window");
        // coverage:ignore-stop
    }
}

/// Makes `label` app window `main`'s live log window on call `a`, as page
/// `page`, for other modules' tests.
#[cfg(test)]
pub fn live_for_test(windows: &LogWindows, page: u64, label: &str) {
    let call = Call {
        cluster_id: "c".into(),
        chat_id: "chat".into(),
        call_id: "a".into(),
    };
    let reserved = windows.reserve("main", page, call, None, || label.to_string());
    assert!(matches!(reserved, Ok(Reserved::Build(_))));
    assert!(windows.built(label));
}

#[cfg(test)]
mod tests {
    use std::sync::{Arc, Mutex};

    use serde_json::{json, Value};
    use tauri::ipc::Channel;

    use super::*;
    use crate::services::sidecar::SidecarService;
    use crate::testutil::{mock_app, window, Scratch};

    /// A channel that records each frame it carries.
    fn recorder() -> (Channel<Frame>, Arc<Mutex<Vec<Value>>>) {
        let frames = Arc::new(Mutex::new(Vec::new()));
        let sink = frames.clone();
        let channel = Channel::new(move |body| {
            sink.lock()
                .unwrap()
                .push(body.deserialize::<Value>().unwrap());
            Ok(())
        });
        (channel, frames)
    }

    /// The frames recorded since the last call.
    fn take(frames: &Arc<Mutex<Vec<Value>>>) -> Vec<Value> {
        std::mem::take(&mut *frames.lock().unwrap())
    }

    fn call(id: &str) -> Call {
        Call {
            cluster_id: "c".into(),
            chat_id: "chat".into(),
            call_id: id.into(),
        }
    }

    /// Counts the labels a test mints, as the window manager does.
    struct Mint(u64);

    impl Mint {
        fn next(&mut self) -> String {
            self.0 += 1;
            format!("logs-{}", self.0)
        }
    }

    /// A pairing where `main` subscribes, with its page and frames.
    fn subscribed() -> (LogWindows, u64, Arc<Mutex<Vec<Value>>>) {
        let windows = LogWindows::default();
        let (channel, frames) = recorder();
        let page = windows.subscribe("main", channel).unwrap();
        take(&frames);
        (windows, page, frames)
    }

    fn reserve(windows: &LogWindows, page: u64, call_id: &str, mint: &mut Mint) -> Reserved {
        windows
            .reserve("main", page, call(call_id), None, || mint.next())
            .unwrap()
    }

    /// Reserves `call_id`'s new window, returning its label.
    fn building(windows: &LogWindows, page: u64, call_id: &str, mint: &mut Mint) -> String {
        let reserved = reserve(windows, page, call_id, mint);
        let label = format!("logs-{}", mint.0);
        assert!(
            matches!(reserved, Reserved::Build(ref built) if *built == label),
            "a new call builds, got {reserved:?}"
        );
        label
    }

    /// Reserves and builds `call_id`'s window, returning its label.
    fn live(windows: &LogWindows, page: u64, call_id: &str, mint: &mut Mint) -> String {
        let label = building(windows, page, call_id, mint);
        assert!(windows.built(&label));
        label
    }

    fn receiver(reserved: Reserved) -> watch::Receiver<Outcome> {
        match reserved {
            Reserved::Wait(outcome) => outcome,
            // coverage:ignore-start runs only when a test fails
            other => panic!("expected a wait, got {other:?}"),
            // coverage:ignore-stop
        }
    }

    /// Whether a wait has finished, without waiting on it.
    fn finished(outcome: &watch::Receiver<Outcome>) -> bool {
        !matches!(*outcome.borrow(), Outcome::Pending)
    }

    /// A `close()` raises `CloseRequested`, which a page listening on
    /// `tauri://close-requested` cancels, so every window the host closes it
    /// destroys. The mock runtime never runs a page, so read the source, as
    /// `build_window_installs_the_navigation_policy` does.
    #[test]
    fn the_host_destroys_the_log_windows_it_closes() {
        let source = include_str!("log_windows.rs");
        let (wiring, _) = source.split_once("#[cfg(test)]").unwrap();

        assert!(wiring.contains(".destroy()"));
        assert!(!wiring.contains(".close()"), "a page could refuse it");
    }

    // The page.

    #[test]
    fn page_each_watch_mints_a_new_page_and_sends_a_snapshot_first() {
        let windows = LogWindows::default();
        let (first, first_frames) = recorder();
        let (second, second_frames) = recorder();

        let one = windows.subscribe("main", first).unwrap();
        let two = windows.subscribe("main", second).unwrap();

        assert_ne!(one, two);
        assert_eq!(take(&first_frames), [json!({"snapshot": {"windows": []}})]);
        assert_eq!(take(&second_frames), [json!({"snapshot": {"windows": []}})]);
    }

    /// The second subscribe replaces the first: its page ends, and frames reach
    /// the new channel alone.
    #[test]
    fn page_a_second_watch_replaces_the_first() {
        let windows = LogWindows::default();
        let (first, first_frames) = recorder();
        let (second, second_frames) = recorder();
        let old = windows.subscribe("main", first).unwrap();
        let new = windows.subscribe("main", second).unwrap();
        take(&first_frames);
        take(&second_frames);
        let mut mint = Mint(0);

        let refused = windows.reserve("main", old, call("a"), None, || mint.next());
        assert!(matches!(refused, Err(AppError::Refused(_))));
        live(&windows, new, "a", &mut mint);

        assert!(take(&first_frames).is_empty());
        assert_eq!(take(&second_frames).len(), 1);
    }

    /// A page ends with its window's destroy and with its page's reload, and
    /// an ended page's commands change nothing.
    #[test]
    fn page_an_ended_page_is_refused_by_every_app_window_call() {
        for (end, kept) in [("destroy", false), ("reload", true)] {
            let (windows, page, _) = subscribed();
            let mut mint = Mint(0);
            let label = live(&windows, page, "a", &mut mint);
            if kept {
                windows.page_started("main");
            } else {
                windows.destroyed("main");
            }

            let reserved = windows.reserve("main", page, call("b"), None, || mint.next());
            assert!(matches!(reserved, Err(AppError::Refused(_))), "{end}");
            let focus = windows.focus_target("main", page, "a");
            assert!(matches!(focus, Err(AppError::Refused(_))), "{end}");
            let closed = windows.close_all("main", page);
            assert!(matches!(closed, Err(AppError::Refused(_))), "{end}");
            assert_eq!(mint.0, 1, "{end}: no label minted");
            // A reload keeps the log window; a destroy takes it.
            assert_eq!(windows.view(&label).is_ok(), kept, "{end}");
        }
    }

    /// `main` destroyed and rebuilt: the new window's page works, and an open
    /// the old page queued before the destroy records nothing.
    #[test]
    fn page_a_rebuilt_main_gets_a_new_page_and_the_old_one_stays_ended() {
        let (windows, old, _) = subscribed();
        windows.destroyed("main");
        let (channel, frames) = recorder();
        let new = windows.subscribe("main", channel).unwrap();
        take(&frames);
        let mut mint = Mint(0);

        let queued = windows.reserve("main", old, call("a"), None, || mint.next());
        assert!(matches!(queued, Err(AppError::Refused(_))));
        assert_eq!(mint.0, 0);

        live(&windows, new, "a", &mut mint);
        assert_eq!(take(&frames).len(), 1, "opened");
    }

    /// A `logs-N` caller mints no page, and a non-app-window caller is
    /// refused by every app-window call.
    #[test]
    fn page_a_log_window_cannot_act_as_an_app_window() {
        let windows = LogWindows::default();
        let (channel, _) = recorder();
        let mut mint = Mint(0);

        assert!(matches!(
            windows.subscribe("logs-1", channel),
            Err(AppError::Refused(_))
        ));
        let reserved = windows.reserve("logs-1", 1, call("a"), None, || mint.next());
        assert!(matches!(reserved, Err(AppError::Refused(_))));
        assert!(windows.close_all("logs-1", 1).is_err());
        assert!(windows.focus_target("logs-1", 1, "a").is_err());

        let (channel, _) = recorder();
        assert_eq!(
            windows.subscribe("main", channel).unwrap(),
            1,
            "no page minted"
        );
    }

    /// A frame its channel fails to carry is dropped, and the pair carries on.
    #[test]
    fn page_a_failed_send_is_dropped() {
        let windows = LogWindows::default();
        let failing = Channel::new(|_| Err(tauri::Error::UnknownPath));

        let page = windows.subscribe("main", failing).unwrap();

        live(&windows, page, "a", &mut Mint(0));
    }

    /// An app window's focus and a missing label's removal change nothing.
    #[test]
    fn page_only_a_log_window_is_tracked() {
        let (windows, page, frames) = subscribed();
        live(&windows, page, "a", &mut Mint(0));
        take(&frames);

        windows.focused("main");
        assert!(!windows.lock().remove("logs-9"));

        assert!(take(&frames).is_empty());
    }

    // The build.

    /// A building entry is in no snapshot and sends nothing; `opened` carries
    /// what was stamped while it built.
    #[test]
    fn build_a_building_window_is_unseen_until_opened() {
        let (windows, page, frames) = subscribed();
        let mut mint = Mint(0);
        let label = building(&windows, page, "a", &mut mint);

        windows.page_started(&label);
        windows
            .viewport(&label, 1, Some("err".into()), json!({"n": 1}))
            .unwrap();
        windows.focused(&label);
        assert_eq!(windows.show_chat(&label).unwrap(), "main");
        assert!(take(&frames).is_empty());
        let (channel, snapshot) = recorder();
        windows.subscribe("main", channel).unwrap();
        assert_eq!(take(&snapshot), [json!({"snapshot": {"windows": []}})]);

        assert!(windows.built(&label));

        let push = json!({"generation": 1, "grep": "err", "viewport": {"n": 1}});
        let opened = json!({"opened": {"window": {
            "label": "logs-1", "clusterId": "c", "chatId": "chat", "callId": "a",
            "focusSeq": 2, "push": push,
        }}});
        assert_eq!(take(&snapshot), [opened]);
        let view = windows.view(&label).unwrap();
        assert_eq!((view.generation, view.grep.as_deref()), (1, Some("err")));
    }

    /// A second open on the same call while the first builds builds nothing,
    /// and hears the first's label.
    #[tokio::test]
    async fn build_a_second_open_waits_on_the_first() {
        let (windows, page, _) = subscribed();
        let mut mint = Mint(0);
        let label = building(&windows, page, "a", &mut mint);
        let outcome = receiver(reserve(&windows, page, "a", &mut mint));

        assert!(windows.built(&label));

        assert_eq!(wait(outcome).await.unwrap(), label);
        assert_eq!(mint.0, 1);
    }

    /// A failed build sends nothing, and both waiting opens hear its error.
    #[tokio::test]
    async fn build_a_failed_build_reaches_every_waiter() {
        let (windows, page, frames) = subscribed();
        let mut mint = Mint(0);
        let label = building(&windows, page, "a", &mut mint);
        let first = receiver(reserve(&windows, page, "a", &mut mint));
        let second = receiver(reserve(&windows, page, "a", &mut mint));

        windows.failed(&label, &AppError::Build("no webview".into()));

        for outcome in [first, second] {
            let err = wait(outcome).await.unwrap_err();
            assert_eq!(err.to_string(), "no webview");
        }
        assert!(take(&frames).is_empty());
        assert!(windows.view(&label).is_err(), "removed");
    }

    /// A build that made nothing as a quit began records nothing live, and its
    /// waiters hear `Cancelled`.
    #[tokio::test]
    async fn build_one_refused_for_a_quit_is_cancelled() {
        let (windows, page, frames) = subscribed();
        let mut mint = Mint(0);
        let label = building(&windows, page, "a", &mut mint);
        let outcome = receiver(reserve(&windows, page, "a", &mut mint));

        windows.failed(&label, &AppError::Cancelled);

        assert!(matches!(wait(outcome).await, Err(AppError::Cancelled)));
        assert!(take(&frames).is_empty());
        assert!(!windows.built(&label));
    }

    #[tokio::test]
    async fn build_a_dropped_sender_reads_as_cancelled() {
        let (sender, outcome) = watch::channel(Outcome::Pending);
        drop(sender);
        assert!(matches!(wait(outcome).await, Err(AppError::Cancelled)));
    }

    /// An open on a live window's call surfaces it and keeps its grep; another
    /// call builds.
    #[test]
    fn build_an_open_surfaces_a_live_window_on_the_same_call() {
        let (windows, page, _) = subscribed();
        let mut mint = Mint(0);
        let reserved = windows.reserve("main", page, call("a"), Some("one".into()), || mint.next());
        assert!(matches!(reserved, Ok(Reserved::Build(_))));
        let label = "logs-1".to_string();
        windows.built(&label);

        let again = windows.reserve("main", page, call("a"), Some("two".into()), || mint.next());
        assert!(matches!(again, Ok(Reserved::Surface(ref l)) if *l == label));
        assert_eq!(windows.view(&label).unwrap().grep.as_deref(), Some("one"));

        assert!(matches!(
            reserve(&windows, page, "b", &mut mint),
            Reserved::Build(_)
        ));
        assert_eq!(
            windows.focus_target("main", page, "a").unwrap(),
            Some(label)
        );
        assert_eq!(windows.focus_target("main", page, "z").unwrap(), None);
    }

    /// An app window's page reload during a build keeps the entry, and the new
    /// page hears `opened`.
    #[test]
    fn build_a_reload_of_the_app_window_keeps_the_build() {
        let (windows, page, frames) = subscribed();
        let mut mint = Mint(0);
        let label = building(&windows, page, "a", &mut mint);
        windows.page_started("main");
        let (channel, reloaded) = recorder();
        windows.subscribe("main", channel).unwrap();
        take(&reloaded);

        assert!(windows.built(&label));

        assert!(take(&frames).is_empty());
        let frames = take(&reloaded);
        assert_eq!(frames.len(), 1);
        assert_eq!(frames[0]["opened"]["window"]["label"], label);
    }

    /// A build's late failure, after a close cancelled it, changes nothing:
    /// its waiters already heard `Cancelled`.
    #[tokio::test]
    async fn build_a_failure_after_a_close_is_ignored() {
        let (windows, page, _) = subscribed();
        let mut mint = Mint(0);
        let label = building(&windows, page, "a", &mut mint);
        let outcome = receiver(reserve(&windows, page, "a", &mut mint));
        windows.close_all("main", page).unwrap();

        windows.failed(&label, &AppError::Build("late".into()));

        assert!(matches!(wait(outcome).await, Err(AppError::Cancelled)));
    }

    /// An open on a call whose window is building waits for that build's
    /// label.
    #[test]
    fn build_open_waits_on_a_building_window() {
        use std::future::Future;
        use std::task::{Context, Poll, Waker};

        let scratch = Scratch::new();
        let app = mock_app(scratch.path(), SidecarService::without_process(None));
        let log_windows = &app.state::<AppState>().log_windows;
        let page = log_windows.subscribe("main", recorder().0).unwrap();
        let reserved = log_windows.reserve("main", page, call("a"), None, || "logs-9".into());
        assert!(matches!(reserved, Ok(Reserved::Build(_))));

        let mut opening = Box::pin(open(app.handle().clone(), "main", page, call("a"), None));
        let mut cx = Context::from_waker(Waker::noop());
        assert!(matches!(opening.as_mut().poll(&mut cx), Poll::Pending));
        assert!(log_windows.built("logs-9"));

        let opened = tauri::async_runtime::block_on(opening);
        assert_eq!(opened.unwrap(), "logs-9");
    }

    /// A build that a close cancelled while it ran destroys its window and is
    /// cancelled.
    #[test]
    fn build_one_cancelled_while_it_ran_is_cancelled() {
        let scratch = Scratch::new();
        let app = mock_app(scratch.path(), SidecarService::without_process(None));
        window(&app, "main");
        let log_windows = &app.state::<AppState>().log_windows;
        let page = log_windows.subscribe("main", recorder().0).unwrap();
        let reserved = log_windows.reserve("main", page, call("a"), None, || "logs-9".into());
        assert!(matches!(reserved, Ok(Reserved::Build(_))));
        log_windows.close_all("main", page).unwrap();

        let label = "logs-9".to_string();
        let built = build(app.handle().clone(), label, "main".into());

        let built = tauri::async_runtime::block_on(built);
        assert!(matches!(built, Err(AppError::Cancelled)));
        assert!(log_windows.view("logs-9").is_err());
    }

    // Cancellation.

    /// With a build in flight and two opens waiting on it, `close_all` and the
    /// app window's destroy each answer both waiters at once; the build then
    /// finds no entry and its window is destroyed. Nothing is sent.
    #[tokio::test]
    async fn cancel_a_close_or_destroy_cancels_a_build_and_its_waiters() {
        for cancel in ["close_all", "destroyed"] {
            let (windows, page, frames) = subscribed();
            let mut mint = Mint(0);
            let label = building(&windows, page, "a", &mut mint);
            let first = receiver(reserve(&windows, page, "a", &mut mint));
            let second = receiver(reserve(&windows, page, "a", &mut mint));

            let destroy = match cancel {
                "close_all" => windows.close_all("main", page).unwrap(),
                _ => windows.destroyed("main"),
            };

            assert!(destroy.is_empty(), "{cancel}: nothing was live");
            assert!(finished(&first) && finished(&second), "{cancel}");
            for outcome in [first, second] {
                assert!(matches!(wait(outcome).await, Err(AppError::Cancelled)));
            }
            assert!(!windows.built(&label), "{cancel}: the build is destroyed");
            assert!(take(&frames).is_empty(), "{cancel}");
        }
    }

    /// `close_all` reports each live window closed and returns its label;
    /// their later `Destroyed` sends nothing.
    #[test]
    fn cancel_close_all_closes_the_live_windows() {
        let (windows, page, frames) = subscribed();
        let mut mint = Mint(0);
        let a = live(&windows, page, "a", &mut mint);
        let b = live(&windows, page, "b", &mut mint);
        take(&frames);

        let destroy = windows.close_all("main", page).unwrap();

        assert_eq!(destroy, [a.as_str(), b.as_str()]);
        assert_eq!(
            take(&frames),
            [
                json!({"closed": {"label": a}}),
                json!({"closed": {"label": b}})
            ]
        );
        windows.destroyed(&a);
        windows.destroyed(&b);
        assert!(take(&frames).is_empty());
    }

    /// The app window's destroy returns its live windows' labels and sends
    /// nothing, its channel gone.
    #[test]
    fn cancel_the_app_windows_destroy_returns_its_live_windows() {
        let (windows, page, frames) = subscribed();
        let mut mint = Mint(0);
        let a = live(&windows, page, "a", &mut mint);
        take(&frames);

        assert_eq!(windows.destroyed("main"), [a.as_str()]);

        assert!(take(&frames).is_empty());
        assert!(windows.view(&a).is_err());
    }

    /// A log window's own destroy while it builds removes the entry and sends
    /// nothing; once live, it sends `closed`.
    #[test]
    fn cancel_a_log_windows_own_destroy_removes_it() {
        let (windows, page, frames) = subscribed();
        let mut mint = Mint(0);
        let building = building(&windows, page, "a", &mut mint);
        let live = live(&windows, page, "b", &mut mint);
        take(&frames);

        assert!(windows.destroyed(&building).is_empty());
        assert!(take(&frames).is_empty());
        assert!(!windows.built(&building));

        assert!(windows.destroyed(&live).is_empty());
        assert_eq!(take(&frames), [json!({"closed": {"label": live}})]);
    }

    // The log window's own calls.

    /// A log window's page load steps its generation, clears its push and
    /// sends the null frame; `view` answers the same twice.
    #[test]
    fn own_a_page_load_steps_the_generation_and_clears_the_push() {
        let (windows, page, frames) = subscribed();
        let mut mint = Mint(0);
        let label = live(&windows, page, "a", &mut mint);
        windows
            .viewport(&label, 0, Some("x".into()), json!(1))
            .unwrap();
        take(&frames);

        windows.page_started(&label);

        assert_eq!(
            take(&frames),
            [json!({"viewport": {"label": label, "push": null}})]
        );
        let view = windows.view(&label).unwrap();
        assert_eq!(view, windows.view(&label).unwrap());
        assert_eq!(view.generation, 1);
        assert_eq!(view.grep.as_deref(), Some("x"), "the grep last pushed");
        assert_eq!(
            (view.chat_id.as_str(), view.call_id.as_str()),
            ("chat", "a")
        );
    }

    /// A push is kept and forwarded; one from an earlier page load is refused.
    #[test]
    fn own_a_push_from_a_stale_generation_is_refused() {
        let (windows, page, frames) = subscribed();
        let mut mint = Mint(0);
        let label = live(&windows, page, "a", &mut mint);
        windows.page_started(&label);
        take(&frames);

        let stale = windows.viewport(&label, 0, None, json!(1));
        assert!(matches!(stale, Err(AppError::Refused(_))));
        assert!(take(&frames).is_empty());

        windows.viewport(&label, 1, None, json!(2)).unwrap();
        let push = json!({"generation": 1, "grep": null, "viewport": 2});
        assert_eq!(
            take(&frames),
            [json!({"viewport": {"label": label, "push": push}})]
        );
    }

    /// A new window ranks above the ones before it, and `focused` carries a
    /// rising sequence.
    #[test]
    fn own_focus_rises() {
        let (windows, page, frames) = subscribed();
        let mut mint = Mint(0);
        let a = live(&windows, page, "a", &mut mint);
        live(&windows, page, "b", &mut mint);
        let opened = take(&frames);
        let seq = |i: usize| opened[i]["opened"]["window"]["focusSeq"].as_u64().unwrap();
        assert!(seq(1) > seq(0));

        windows.focused(&a);

        let focused = take(&frames);
        assert_eq!(focused[0]["focused"]["label"], a);
        assert!(focused[0]["focused"]["focusSeq"].as_u64().unwrap() > seq(1));
        let (channel, snapshot) = recorder();
        windows.subscribe("main", channel).unwrap();
        let listed = &take(&snapshot)[0]["snapshot"]["windows"];
        assert_eq!(listed.as_array().unwrap().len(), 2);
    }

    /// Frames reach the paired app window's channel and no other, and a log
    /// window's call reaches only its own app window.
    #[test]
    fn own_frames_reach_the_paired_app_window_alone() {
        let windows = LogWindows::default();
        let (main_channel, main_frames) = recorder();
        let (other_channel, other_frames) = recorder();
        let main = windows.subscribe("main", main_channel).unwrap();
        windows.subscribe("window-1", other_channel).unwrap();
        take(&main_frames);
        take(&other_frames);
        let mut mint = Mint(0);
        let label = live(&windows, main, "a", &mut mint);

        windows.viewport(&label, 0, None, json!(1)).unwrap();
        assert_eq!(windows.show_chat(&label).unwrap(), "main");

        let frames = take(&main_frames);
        assert_eq!(frames.len(), 3, "opened, viewport, showChat");
        assert_eq!(frames[2], json!({"showChat": {"chatId": "chat"}}));
        assert!(take(&other_frames).is_empty());
        assert_eq!(windows.focus_target("window-1", 2, "a").unwrap(), None);
    }

    /// An app window is refused every log-window call, and changes nothing.
    #[test]
    fn own_an_app_window_cannot_act_as_a_log_window() {
        let (windows, page, frames) = subscribed();
        let mut mint = Mint(0);
        live(&windows, page, "a", &mut mint);
        take(&frames);

        assert!(matches!(windows.view("main"), Err(AppError::Refused(_))));
        let push = windows.viewport("main", 0, None, json!(1));
        assert!(matches!(push, Err(AppError::Refused(_))));
        assert!(matches!(
            windows.show_chat("window-1"),
            Err(AppError::Refused(_))
        ));
        assert!(take(&frames).is_empty());
    }

    #[test]
    fn own_every_bound_is_refused() {
        let (windows, page, _) = subscribed();
        let mut mint = Mint(0);
        let long_id = "x".repeat(super::MAX_ID_BYTES + 1);
        let long_grep = "x".repeat(super::MAX_GREP_BYTES + 1);
        for (what, call, grep) in [
            (
                "cluster",
                Call {
                    cluster_id: long_id.clone(),
                    ..call("a")
                },
                None,
            ),
            (
                "chat",
                Call {
                    chat_id: long_id.clone(),
                    ..call("a")
                },
                None,
            ),
            ("call", call(&long_id), None),
            ("grep", call("a"), Some(long_grep.clone())),
        ] {
            let reserved = windows.reserve("main", page, call, grep, || mint.next());
            assert!(matches!(reserved, Err(AppError::Refused(_))), "{what}");
        }
        assert_eq!(mint.0, 0);

        let label = live(&windows, page, "a", &mut mint);
        let big = json!("x".repeat(super::MAX_PUSH_BYTES));
        assert!(windows.viewport(&label, 0, None, big).is_err());
        assert!(windows
            .viewport(&label, 0, Some(long_grep), json!(1))
            .is_err());
        assert_eq!(windows.view(&label).unwrap().grep, None);

        let title = |parts: &[String]| windows.title(&label, parts);
        let part = |n: usize| "x".repeat(n);
        assert!(title(&[part(1), part(1), part(1)]).is_ok());
        assert!(title(&[part(1), part(1), part(1), part(1)]).is_err());
        assert!(title(&[part(super::MAX_TITLE_PART_BYTES + 1)]).is_err());
        assert_eq!(title(&["pod".into()]).unwrap(), "Logs · pod");
        let titled = windows.title("main", &["pod".into()]);
        assert!(matches!(titled, Err(AppError::Refused(_))));
    }
}
