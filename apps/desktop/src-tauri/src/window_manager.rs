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

//! Window lifecycle. Every window is built in code by [`WindowManager`]
//! (`tauri.conf.json` declares none — a test pins this), so all chrome lives in
//! `build_window`; new windows are sized to their monitor ([`fit_size`]) and
//! cascade down-right of their anchor ([`cascade_position`]), at the size of
//! an anchor of their own kind. The main window reopens at its saved frame
//! (`window_state.rs`). Log windows cascade in a group per app window
//! ([`LogGroups`]). See docs/adr/2026-08-09-per-platform-window-chrome.md.
//!
//! Windows are visible from creation, painted with the resolved scheme at t0
//! ([`background_color_for`]) — **no reveal step**; do not add one. On macOS
//! this depends on `tauri/macos-private-api` (clears `WKWebView`'s opaque white
//! backing), which in turn requires the *document* to stay opaque. See
//! docs/adr/2026-08-09-first-paint-theming.md.
//!
//! Closing the last window does not exit the process (see `lib.rs`), so every
//! entry point here recreates `"main"` on demand.

use std::collections::HashMap;
use std::path::PathBuf;
use std::sync::atomic::{AtomicU64, Ordering};
use std::sync::{Mutex, MutexGuard};

use tauri::webview::NewWindowResponse;
use tauri::window::{Color, Monitor};
use tauri::{AppHandle, Manager, Runtime, Url, WebviewUrl, WebviewWindow, WebviewWindowBuilder};
use tokio_util::sync::CancellationToken;
use tracing::warn;

use crate::error::Result;
use crate::host_file::{self, ColorSchemePreference, HostFile};
use crate::window_state::{self, Frame, SavedFrame, Size};

/// Label of the primary window (created at startup by `lib.rs`'s `setup`).
const MAIN_LABEL: &str = "main";

/// Title shown on every window the host creates.
const WINDOW_TITLE: &str = "Kubetail";

/// The label prefixes of the two kinds of window the manager mints, both off
/// one counter: an app window is `main` or `window-N`, a log window `logs-N`.
const APP_PREFIX: &str = "window-";
const LOGS_PREFIX: &str = "logs-";

/// Whether `label` is an app window's.
pub fn is_app_label(label: &str) -> bool {
    label == MAIN_LABEL || label.starts_with(APP_PREFIX)
}

/// Whether `label` is a log window's.
pub fn is_logs_label(label: &str) -> bool {
    label.starts_with(LOGS_PREFIX)
}

/// What one kind of window opens with.
struct WindowSpec {
    /// The page, under the app's origin.
    path: &'static str,
    /// Logical size on a screen with room for it ([`fit_size`]).
    size: (f64, f64),
    /// Whether a label is this kind's. A new window takes the size of an
    /// anchor of its own kind.
    is_kind: fn(&str) -> bool,
    /// The bar the macOS traffic lights center in.
    #[cfg_attr(not(target_os = "macos"), allow(dead_code))]
    bar_height: f64,
}

const APP_WINDOW: WindowSpec = WindowSpec {
    path: "index.html",
    size: (1200.0, 760.0),
    is_kind: is_app_label,
    bar_height: APP_BAR_HEIGHT,
};

/// Logical floor on every window's size, so the layout never renders below its
/// narrowest design.
const MIN_SIZE: (f64, f64) = (600.0, 400.0);

/// The largest share of its monitor's work area a new window takes per side.
const MAX_WORK_AREA_SHARE: f64 = 0.85;

/// A log window's page asks the host what to draw, so its URL carries nothing.
const LOG_WINDOW: WindowSpec = WindowSpec {
    path: "logs",
    size: (960.0, 640.0),
    is_kind: is_logs_label,
    bar_height: LOG_BAR_HEIGHT,
};

// Traffic-light geometry: used only on macOS, but compiled and unit-tested on
// every platform so CI covers the arithmetic (hence `allow(dead_code)`).

/// App-bar height, the strip the macOS traffic lights sit in. Kept in sync with
/// `src/components/shell/app-bar/bar.tsx`'s `h-14`.
const APP_BAR_HEIGHT: f64 = 56.0;

/// A log window's bar height, the strip its traffic lights sit in. Kept in sync
/// with `src/components/logs/log-window/bar.tsx`'s `h-10`.
const LOG_BAR_HEIGHT: f64 = 40.0;

/// Inset from the window's left edge to the first traffic light. The webview's
/// `TRAFFIC_LIGHT_GUTTER` (`traffic-light-gutter.tsx`, shared by both bars)
/// reserves this plus the buttons — bump this and check each bar's first control
/// still clears them.
#[cfg_attr(not(target_os = "macos"), allow(dead_code))]
const TRAFFIC_LIGHT_LEFT_INSET: f64 = 20.0;

/// macOS traffic-light button diameter (logical px).
#[cfg_attr(not(target_os = "macos"), allow(dead_code))]
const TRAFFIC_LIGHT_DIAMETER: f64 = 12.0;

/// Height of the native title bar; the buttons sit centered in it.
#[cfg_attr(not(target_os = "macos"), allow(dead_code))]
const NATIVE_TITLE_BAR_HEIGHT: f64 = 28.0;

/// The buttons' inset inside the native title bar.
#[cfg_attr(not(target_os = "macos"), allow(dead_code))]
const TRAFFIC_LIGHT_NATIVE_INSET: f64 = (NATIVE_TITLE_BAR_HEIGHT - TRAFFIC_LIGHT_DIAMETER) / 2.0;

/// Logical `(x, y)` for the macOS traffic lights: `x` insets the first button from
/// the window's left edge, `y` centers the row in the app bar.
///
/// `y` is not the gap that shows above the buttons. The runtime grows the native
/// title bar to `button + y` and anchors it to the window's top, but only moves the
/// buttons horizontally (tao's `inset_traffic_lights`), so they keep their native
/// inset from that bar's bottom. What shows above them is `y` less that inset, and
/// centering adds it back.
#[cfg_attr(not(target_os = "macos"), allow(dead_code))]
fn traffic_light_position(bar_height: f64) -> (f64, f64) {
    let gap_above = (bar_height - TRAFFIC_LIGHT_DIAMETER) / 2.0;
    (
        TRAFFIC_LIGHT_LEFT_INSET,
        gap_above + TRAFFIC_LIGHT_NATIVE_INSET,
    )
}

/// `@kubetail/ui`'s `--background` token per scheme, tracked by eye — nothing
/// pins them, but the webview paints the real token over this on first frame.
#[cfg_attr(target_os = "linux", allow(dead_code))]
const LIGHT_BACKGROUND: Color = Color(255, 255, 255, 255);
#[cfg_attr(target_os = "linux", allow(dead_code))]
const DARK_BACKGROUND: Color = Color(9, 9, 11, 255);

/// Native window background for a persisted preference — the anti-launch-flash
/// color on screen until the webview's first paint (and during live resize).
///
/// Must always return a concrete color: wry only clears the webview's opaque
/// white backing when one is set, so `system` resolves against
/// `os_prefers_dark` rather than opting out. See
/// docs/adr/2026-08-09-first-paint-theming.md. Called only on opaque platforms,
/// but compiled/tested everywhere.
#[cfg_attr(target_os = "linux", allow(dead_code))]
fn background_color_for(preference: Option<ColorSchemePreference>, os_prefers_dark: bool) -> Color {
    let dark = match preference {
        Some(ColorSchemePreference::Light) => false,
        Some(ColorSchemePreference::Dark) => true,
        // `system` — and a file that predates the setting — follow the OS.
        Some(ColorSchemePreference::System) | None => os_prefers_dark,
    };
    if dark {
        DARK_BACKGROUND
    } else {
        LIGHT_BACKGROUND
    }
}

/// The native background `file`'s color scheme calls for on this machine.
#[cfg(not(target_os = "linux"))]
fn native_background(file: &HostFile) -> Color {
    background_color_for(
        file.color_scheme_preference,
        crate::os_theme::prefers_dark(),
    )
}

/// Brings every open window up to `file` after a write.
pub fn host_file_written<R: Runtime>(app: &AppHandle<R>, file: &HostFile) {
    for window in app.webview_windows().values() {
        send_host_file(window, file);
    }
}

/// Brings `window` up to `file`, so a reload paints its scheme from the first
/// frame: runs [`host_file::update_script`] in it and, on the opaque
/// platforms, repaints its native background. A window that fails keeps its
/// old copy until its page next loads.
pub fn send_host_file<R: Runtime>(window: &WebviewWindow<R>, file: &HostFile) {
    let result = window.eval(host_file::update_script(file));
    #[cfg(not(target_os = "linux"))]
    let result = result.and(window.set_background_color(Some(native_background(file))));
    if let Err(err) = result {
        // coverage:ignore-start the mock runtime never fails a window call
        warn!(%err, label = window.label(), "failed to bring a window up to host.json");
        // coverage:ignore-stop
    }
}

/// Whether the webview may navigate to `url`. The app is a local single-page
/// bundle: every legitimate navigation is client-side routing (`pushState`,
/// which never reaches this), so a document navigation is a page trying to
/// leave — and a window that has left still holds every host command.
///
/// An allowlist that ends in `false`, never a denylist of bad schemes: a scheme
/// nobody thought of is refused rather than admitted.
fn is_app_origin(url: &Url) -> bool {
    // Handed to the callback by some engines during startup.
    if url.as_str() == "about:blank" {
        return true;
    }

    // `pnpm tauri dev` serves the webview off localhost. Behind `cfg` so a
    // release build cannot admit it, whatever port the dev server picked.
    if cfg!(debug_assertions) && (url.scheme(), url.host_str()) == ("http", Some("localhost")) {
        return true;
    }

    // A port would make it a different origin, and the bundle is served on none.
    // `Url::origin` is no help: a non-special scheme like `tauri` is opaque, so
    // every custom-protocol URL serializes to the same "null".
    if url.port().is_some() {
        return false;
    }

    // The bundle's own origin, per platform: macOS/Linux serve it over the
    // `tauri` custom protocol, Windows over a virtual host. Both are admitted
    // everywhere — a build only ever loads its own.
    matches!(
        (url.scheme(), url.host_str()),
        ("tauri", Some("localhost")) | ("http", Some("tauri.localhost"))
    )
}

/// Answers a page's `window.open`: always no. Nothing in the app opens a
/// window this way, and a popup is a webview the navigation policy does not
/// govern. An external link stays host-owned — the opener plugin, as the
/// tray's account item does.
fn refuse_new_window<R: Runtime>(url: &Url) -> NewWindowResponse<R> {
    warn!(%url, "refused a new-window request from the webview");
    NewWindowResponse::Deny
}

/// Runs `build` on the blocking pool. Must stay `spawn_blocking`, never `spawn`
/// or the main thread: `build()` parks its thread until the webview exists — a
/// main-thread build deadlocks Windows (WebView2's controller is created by the
/// main-thread event loop), and parking an async worker would starve
/// `graphql_query`.
pub async fn on_blocking_pool<T: Send + 'static>(
    build: impl FnOnce() -> Result<T> + Send + 'static,
) -> Result<T> {
    tauri::async_runtime::spawn_blocking(build)
        .await
        .map_err(|err| std::io::Error::other(format!("window build task failed: {err}")))?
}

/// Logs a window call that failed. None of them is worth failing the caller.
fn warn_on_error<T>(result: tauri::Result<T>, what: &str) {
    if let Err(err) = result {
        warn!(%err, "{what}");
    }
}

/// Down-right step per cascade — about a title bar, so the window beneath
/// stays grabbable.
const CASCADE_STEP: f64 = 28.0;

/// Logical `(x, y)` for a new window: one [`CASCADE_STEP`] down-right of
/// `anchor` within `work_area` (`(x, y, w, h)` logical px, monitor minus
/// taskbar/dock); no anchor → centered.
///
/// The step is taken only if the result fits entirely inside the work area,
/// else the cascade restarts from center. That single fit check bounds the walk
/// (no step cap) and absorbs a pathological anchor (dragged off-screen or onto
/// another monitor). A window larger than the work area pins to its top-left.
fn cascade_position(
    anchor: Option<(f64, f64)>,
    window: (f64, f64),
    work_area: (f64, f64, f64, f64),
) -> (f64, f64) {
    let (win_w, win_h) = window;
    let (area_x, area_y, area_w, area_h) = work_area;

    // Center, but never above/left of the work area's origin — an oversized
    // window would otherwise be centered into negative coordinates, hiding its
    // title bar (and with it the traffic lights / custom window controls).
    let centered = (
        area_x + ((area_w - win_w) / 2.0).max(0.0),
        area_y + ((area_h - win_h) / 2.0).max(0.0),
    );

    let Some((anchor_x, anchor_y)) = anchor else {
        return centered;
    };

    let (x, y) = (anchor_x + CASCADE_STEP, anchor_y + CASCADE_STEP);
    let fits =
        x >= area_x && y >= area_y && x + win_w <= area_x + area_w && y + win_h <= area_y + area_h;

    if fits {
        (x, y)
    } else {
        centered
    }
}

/// Logical size for a new window of `size` on `work_area` (`(x, y, w, h)`):
/// at most [`MAX_WORK_AREA_SHARE`] of the work area per side, never below
/// [`MIN_SIZE`].
fn fit_size(size: (f64, f64), work_area: (f64, f64, f64, f64)) -> (f64, f64) {
    let (_, _, area_w, area_h) = work_area;
    (
        size.0.min(area_w * MAX_WORK_AREA_SHARE).max(MIN_SIZE.0),
        size.1.min(area_h * MAX_WORK_AREA_SHARE).max(MIN_SIZE.1),
    )
}

/// The logical px of a restored window's title bar that must stay on a work
/// area, so it can be dragged back.
const MIN_VISIBLE_TITLE_BAR: f64 = 100.0;

/// Whether `frame` still fits `work_area`: no larger than it, its title bar
/// (the app bar's height across its top) on it for at least
/// [`MIN_VISIBLE_TITLE_BAR`] of its width.
fn fits(frame: &Frame, work_area: (f64, f64, f64, f64)) -> bool {
    let (x, y, width, height) = work_area;
    let visible = (frame.x + frame.width).min(x + width) - frame.x.max(x);
    frame.width <= width
        && frame.height <= height
        && frame.y >= y
        && frame.y + APP_BAR_HEIGHT <= y + height
        && visible >= MIN_VISIBLE_TITLE_BAR
}

/// Whether `frame` is as large as `work_area`, as a maximized window's is.
fn covers(frame: &Frame, work_area: (f64, f64, f64, f64)) -> bool {
    let (_, _, width, height) = work_area;
    frame.width >= width && frame.height >= height
}

/// A new window of `size` on `monitor`: [`fit_size`]d, then placed by
/// [`cascade_position`].
fn frame_on(monitor: &Monitor, anchor: Option<(f64, f64)>, size: (f64, f64)) -> Frame {
    let area = logical_work_area(monitor);
    let (width, height) = fit_size(size, area);
    let (x, y) = cascade_position(anchor, (width, height), area);
    Frame {
        x,
        y,
        width,
        height,
    }
}

/// Where an app window's next log window opens, given its group's `last`
/// frame (see [`LogGroups`]): a cascade step off `last` while the group has a
/// window open, else at `last` itself. `fallback` when there is no `last`, or
/// it fits none of the work `areas`.
fn next_in_group(
    last: Option<Frame>,
    group_open: bool,
    areas: &[(f64, f64, f64, f64)],
    fallback: Option<Frame>,
) -> Option<Frame> {
    let Some((last, area)) =
        last.and_then(|last| Some((last, *areas.iter().find(|area| fits(&last, **area))?)))
    else {
        return fallback;
    };
    if !group_open {
        return Some(last);
    }
    let (width, height) = fit_size((last.width, last.height), area);
    let (x, y) = cascade_position(Some((last.x, last.y)), (width, height), area);
    Some(Frame {
        x,
        y,
        width,
        height,
    })
}

/// Each app window's group of log windows.
#[derive(Default)]
struct LogGroups {
    /// Each open log window's app window, from placement on, so a window
    /// still being built counts.
    app_of: HashMap<String, String>,
    /// Each app window's group's last frame: the window last placed, moved,
    /// resized or focused. Kept when the group's windows close, so its next
    /// window reopens there; dropped with the app window.
    last: HashMap<String, Frame>,
}

/// Unminimizes, shows and focuses `window`.
fn surface<R: Runtime>(window: &WebviewWindow<R>) -> Result<()> {
    // A minimized window won't surface on show()/set_focus() alone.
    let minimized = window.is_minimized().unwrap_or(false);
    minimized.then(|| window.unminimize()).transpose()?;
    window.show()?;
    window.set_focus()?;
    Ok(())
}

/// `window`'s outer position and inner size in logical px. `None` when the
/// window cannot be read, or is minimized or fullscreen, neither of which is
/// its normal frame (Windows reports a minimized window far off-screen).
fn current_frame<R: Runtime>(window: &WebviewWindow<R>) -> Option<Frame> {
    let normal = !window.is_minimized().ok()? && !window.is_fullscreen().ok()?;
    let scale = window.scale_factor().ok()?;
    let position = window.outer_position().ok()?.to_logical::<f64>(scale);
    let size = window.inner_size().ok()?.to_logical::<f64>(scale);
    normal.then_some(Frame {
        x: position.x,
        y: position.y,
        width: size.width,
        height: size.height,
    })
}

/// `window`'s inner size in logical px while it is at its normal frame:
/// `None` when it is minimized, fullscreen or maximized.
fn normal_size<R: Runtime>(window: &WebviewWindow<R>) -> Option<(f64, f64)> {
    let frame = current_frame(window)?;
    let maximized = window.is_maximized().ok()?;
    (!maximized).then_some((frame.width, frame.height))
}

/// A monitor's work area as logical `(x, y, width, height)` — Tauri only
/// exposes it in physical pixels.
fn logical_work_area(monitor: &Monitor) -> (f64, f64, f64, f64) {
    let scale = monitor.scale_factor();
    let area = monitor.work_area();
    let position = area.position.to_logical::<f64>(scale);
    let size = area.size.to_logical::<f64>(scale);
    (position.x, position.y, size.width, size.height)
}

/// The monitors a window is placed against. `app.rs` reads the real
/// runtime's; Tauri's mock runtime has none, so tests pass their own.
pub trait Monitors: Send + Sync {
    /// The monitor the window labelled `label` is on.
    fn of_window(&self, label: &str) -> Option<Monitor>;

    /// The system's primary monitor.
    fn primary(&self) -> Option<Monitor>;

    /// Every connected monitor.
    fn all(&self) -> Vec<Monitor>;
}

/// Owns window creation and focus for the host.
pub struct WindowManager {
    /// Monotonic counter feeding unique window labels.
    next_id: AtomicU64,

    /// Most recently built window — the cascade's fallback anchor when no
    /// window is focused (tray/Dock menus). A label, not a `WebviewWindow`, so
    /// a closed window drops normally and is simply not found next lookup.
    last_built: Mutex<Option<String>>,

    /// The app-wide shutdown signal, cancelled once a quit begins. Every path
    /// that opens or shows a window ends in this type's two methods, which open
    /// nothing after it and hide what they presented as it began: a window
    /// would come back over a sidecar that is stopping and vanish when the exit
    /// lands.
    shutdown: CancellationToken,

    monitors: Box<dyn Monitors>,

    /// The main window's frame as last seen, written to `state_path` when it
    /// is destroyed and on exit. Seeded when [`Self::main_window_placement`]
    /// places it. Never held while the runtime is queried, nor are the two
    /// below.
    main_frame: Mutex<Option<SavedFrame>>,

    /// The size of the log window last moved, resized or focused, written to
    /// `state_path` when a log window is destroyed and on exit. Seeded from
    /// the file by the first log window placed.
    log_size: Mutex<Option<Size>>,

    log_groups: Mutex<LogGroups>,

    /// Held across each write, which reads the file to keep the other entry.
    save_lock: Mutex<()>,

    /// Where the frames are kept across launches ([`window_state::path`]).
    /// `None` keeps them for this session only.
    state_path: Option<PathBuf>,
}

impl WindowManager {
    pub fn new(
        shutdown: CancellationToken,
        monitors: impl Monitors + 'static,
        state_path: Option<PathBuf>,
    ) -> Self {
        Self {
            next_id: AtomicU64::new(0),
            last_built: Mutex::new(None),
            shutdown,
            monitors: Box::new(monitors),
            main_frame: Mutex::new(None),
            log_size: Mutex::new(None),
            log_groups: Mutex::new(LogGroups::default()),
            save_lock: Mutex::new(()),
            state_path,
        }
    }

    /// Hides every window, as a quit begins.
    pub fn hide_all<R: Runtime>(&self, app: &AppHandle<R>) {
        for window in app.webview_windows().values() {
            warn_on_error(window.hide(), "failed to hide a window on quit");
        }
    }

    /// Create a fresh window with a unique label (`window-1`, `window-2`, …) —
    /// never reuses an existing one. Opens nothing once a quit has begun.
    pub fn new_window<R: Runtime>(&self, app: &AppHandle<R>) -> Result<()> {
        let label = self.next_label();
        let placed = self
            .placement(app, None, &APP_WINDOW)
            .map(|frame| SavedFrame {
                frame,
                maximized: false,
            });
        self.open(app, &label, &APP_WINDOW, placed)?;
        Ok(())
    }

    /// Builds the log window `label` for the app window `anchor`, at
    /// [`Self::log_window_placement`]. `None` when a quit has begun and it
    /// built nothing.
    pub fn open_log_window<R: Runtime>(
        &self,
        app: &AppHandle<R>,
        label: &str,
        anchor: &str,
    ) -> Result<Option<WebviewWindow<R>>> {
        let placed = self.log_window_placement(app, label, anchor);
        let opened = self.open(app, label, &LOG_WINDOW, placed);
        if !matches!(opened, Ok(Some(_))) {
            self.log_groups().app_of.remove(label);
        }
        opened
    }

    /// Where the log window `label` opens, as [`next_in_group`] places it in
    /// app window `anchor`'s group, falling back to a step off `anchor` at the
    /// saved size. Joins `label` to the group and makes its frame the group's
    /// last, so the next window steps off it even before it is built.
    fn log_window_placement<R: Runtime>(
        &self,
        app: &AppHandle<R>,
        label: &str,
        anchor: &str,
    ) -> Option<SavedFrame> {
        // The runtime is read before the lock is taken: from the blocking
        // pool it waits on the event loop, which may be waiting on the lock.
        let size = self
            .log_size()
            .map_or(LOG_WINDOW.size, |size| (size.width, size.height));
        let fallback = self.placed_off(self.anchor_window(app, Some(anchor)), size);
        let areas: Vec<_> = self.monitors.all().iter().map(logical_work_area).collect();

        let mut groups = self.log_groups();
        let group_open = groups.app_of.values().any(|app| app == anchor);
        let frame = next_in_group(
            groups.last.get(anchor).copied(),
            group_open,
            &areas,
            fallback,
        )?;
        groups.app_of.insert(label.to_string(), anchor.to_string());
        groups.last.insert(anchor.to_string(), frame);
        Some(SavedFrame {
            frame,
            maximized: false,
        })
    }

    /// The log windows' size: remembered, else read from `state_path` and
    /// remembered.
    fn log_size(&self) -> Option<Size> {
        let mut size = self.log_size.lock().unwrap_or_else(|p| p.into_inner());
        if size.is_none() {
            *size = window_state::read_log_size(self.state_path.as_deref()?);
        }
        *size
    }

    fn log_groups(&self) -> MutexGuard<'_, LogGroups> {
        self.log_groups.lock().unwrap_or_else(|p| p.into_inner())
    }

    /// Builds the window `label` unless a quit has begun.
    fn open<R: Runtime>(
        &self,
        app: &AppHandle<R>,
        label: &str,
        spec: &WindowSpec,
        placed: Option<SavedFrame>,
    ) -> Result<Option<WebviewWindow<R>>> {
        if self.quitting() {
            return Ok(None);
        }
        let window = self.build_window(app, label, spec, placed)?;
        self.hide_if_quitting(app, window.clone());
        Ok(Some(window))
    }

    /// Unminimizes, shows and focuses the window `label`. `false` when there is
    /// no such window, or a quit has begun and it showed nothing.
    pub fn surface_window<R: Runtime>(&self, app: &AppHandle<R>, label: &str) -> Result<bool> {
        if self.quitting() {
            return Ok(false);
        }
        let Some(window) = app.get_webview_window(label) else {
            return Ok(false);
        };
        // Queued whatever surface returns: a step may fail after show() went out.
        let surfaced = surface(&window);
        self.hide_if_quitting(app, window);
        surfaced.map(|()| true)
    }

    /// Whether a quit has begun, logged when it has: the ask that reached here
    /// came from a click or a second launch, and it is answered with nothing.
    fn quitting(&self) -> bool {
        let quitting = self.shutdown.is_cancelled();
        if quitting {
            warn!("a window was asked for while quitting; it opens nothing");
        }
        quitting
    }

    /// Hides `window` if a quit has begun, for a presentation that passed the
    /// entry check just before it. From another thread, a window call is a
    /// message to the event loop, so this check queues behind the presentation:
    /// either the quit came first and this hides the window, or the window was
    /// up when `hide_all` ran.
    fn hide_if_quitting<R: Runtime>(&self, app: &AppHandle<R>, window: WebviewWindow<R>) {
        let shutdown = self.shutdown.clone();
        let queued = app.run_on_main_thread(move || {
            if shutdown.is_cancelled() {
                let hidden = window.hide();
                warn_on_error(hidden, "failed to hide a window opened as the quit began");
            }
        });
        warn_on_error(queued, "failed to queue the quit check for a window");
    }

    /// Next unique app-window label, `window-N`.
    fn next_label(&self) -> String {
        self.mint(APP_PREFIX)
    }

    /// Next unique log-window label, `logs-N`.
    pub fn next_logs_label(&self) -> String {
        self.mint(LOGS_PREFIX)
    }

    /// Next unique label under `prefix`, off one counter for both kinds;
    /// never reused for the process lifetime, even across concurrent callers.
    fn mint(&self, prefix: &str) -> String {
        let id = self.next_id.fetch_add(1, Ordering::Relaxed) + 1;
        format!("{prefix}{id}")
    }

    /// The cascade anchor: the window `label` names, else the focused window,
    /// else the last one built (the tray/Dock menus can be clicked while
    /// another app holds focus). `None` when that window is gone, or no windows
    /// are open.
    fn anchor_window<R: Runtime>(
        &self,
        app: &AppHandle<R>,
        label: Option<&str>,
    ) -> Option<WebviewWindow<R>> {
        if let Some(label) = label {
            return app.get_webview_window(label);
        }
        let focused = app
            .webview_windows()
            .into_values()
            .find(|window| window.is_focused().unwrap_or(false));

        focused.or_else(|| {
            let last_built = self.last_built.lock().unwrap_or_else(|p| p.into_inner());
            last_built
                .as_deref()
                .and_then(|label| app.get_webview_window(label))
        })
    }

    /// Where the next window of `spec` opens: sized to its monitor, a
    /// cascade step off the anchor (see [`Self::anchor_window`]) on its
    /// monitor, else centered on the primary. It takes the size of an anchor
    /// of its own kind. `None` when no monitor can be read.
    fn placement<R: Runtime>(
        &self,
        app: &AppHandle<R>,
        anchor: Option<&str>,
        spec: &WindowSpec,
    ) -> Option<Frame> {
        let anchor = self.anchor_window(app, anchor);
        let size = anchor
            .as_ref()
            .filter(|window| (spec.is_kind)(window.label()))
            .and_then(normal_size)
            .unwrap_or(spec.size);
        self.placed_off(anchor, size)
    }

    /// A new window of `size`, a cascade step off `anchor` on its monitor,
    /// else centered on the primary. `None` when no monitor can be read.
    fn placed_off<R: Runtime>(
        &self,
        anchor: Option<WebviewWindow<R>>,
        size: (f64, f64),
    ) -> Option<Frame> {
        // Anchor and its monitor resolve together — an anchor whose monitor
        // can't be read is no anchor at all. Anything unreadable degrades to a
        // centered window, never a failed build.
        let anchored = anchor.and_then(|window| {
            let monitor = self.monitors.of_window(window.label())?;
            let position = window
                .outer_position()
                .ok()?
                .to_logical::<f64>(monitor.scale_factor());
            Some((monitor, (position.x, position.y)))
        });

        let (monitor, anchor_position) = match anchored {
            Some((monitor, position)) => (monitor, Some(position)),
            // The session's first window, or an anchor that vanished mid-build.
            None => (self.monitors.primary()?, None),
        };

        Some(frame_on(&monitor, anchor_position, size))
    }

    fn main_frame(&self) -> MutexGuard<'_, Option<SavedFrame>> {
        self.main_frame.lock().unwrap_or_else(|p| p.into_inner())
    }

    /// Where the main window opens: at its saved frame (remembered, else read
    /// from `state_path`) while that fits a connected monitor, else at the
    /// default size centered on the primary; maximized if it was saved so.
    /// `None` when neither can be had.
    ///
    /// The frame chosen is remembered as the normal frame, so a window that
    /// opens maximized and is never unmaximized saves it.
    fn main_window_placement(&self) -> Option<SavedFrame> {
        // Not held while the monitors are read: from the blocking pool that
        // waits on the event loop, which may itself be waiting on this lock
        // (a tray click racing a Dock reopen).
        let remembered = *self.main_frame();
        let saved = remembered.or_else(|| window_state::read_main(self.state_path.as_deref()?));

        let monitors = self.monitors.all();
        let restored = saved.map(|saved| saved.frame).filter(|frame| {
            monitors
                .iter()
                .any(|monitor| fits(frame, logical_work_area(monitor)))
        });
        let frame = match restored {
            Some(frame) => frame,
            None => frame_on(&self.monitors.primary()?, None, APP_WINDOW.size),
        };
        let placed = SavedFrame {
            frame,
            maximized: saved.is_some_and(|saved| saved.maximized),
        };
        *self.main_frame() = Some(placed);
        Some(placed)
    }

    /// `label`'s window's frame and whether it reads as maximized. `None`
    /// when it is gone, minimized or fullscreen.
    ///
    /// A frame covering its work area counts as maximized: Windows reports a
    /// maximize's move before the window reads as maximized.
    fn observed<R: Runtime>(&self, app: &AppHandle<R>, label: &str) -> Option<SavedFrame> {
        let window = app.get_webview_window(label)?;
        let frame = current_frame(&window)?;
        let covered = self
            .monitors
            .of_window(label)
            .is_some_and(|monitor| covers(&frame, logical_work_area(&monitor)));
        let maximized = covered || window.is_maximized().unwrap_or(false);
        Some(SavedFrame { frame, maximized })
    }

    /// Remembers the frame of `label`'s window after it moved or resized:
    /// the main window's ([`window_state::next_saved`]), or a log window's
    /// (see [`Self::log_window_seen`]).
    pub fn window_moved<R: Runtime>(&self, app: &AppHandle<R>, label: &str) {
        if is_logs_label(label) {
            self.log_window_seen(app, label);
        } else if label == MAIN_LABEL {
            if let Some(current) = self.observed(app, label) {
                let mut remembered = self.main_frame();
                *remembered = Some(window_state::next_saved(remembered.take(), current));
            }
        }
    }

    /// Makes the log window `label` its group's last after it took focus, so
    /// the next one steps off it.
    pub fn window_focused<R: Runtime>(&self, app: &AppHandle<R>, label: &str) {
        if is_logs_label(label) {
            self.log_window_seen(app, label);
        }
    }

    fn log_window_seen<R: Runtime>(&self, app: &AppHandle<R>, label: &str) {
        if let Some(seen) = self.observed(app, label) {
            self.remember_log_frame(label, seen);
        }
    }

    /// Remembers the log window `label`'s frame as its group's last and its
    /// size as the log windows'. A maximized frame is ignored, since a log
    /// window never opens maximized, and so is a sizeless one, which a window
    /// not yet mapped can report.
    fn remember_log_frame(&self, label: &str, seen: SavedFrame) {
        let frame = seen.frame;
        if seen.maximized || frame.width <= 0.0 || frame.height <= 0.0 {
            return;
        }
        *self.log_size.lock().unwrap_or_else(|p| p.into_inner()) = Some(Size {
            width: frame.width,
            height: frame.height,
        });
        let mut groups = self.log_groups();
        if let Some(anchor) = groups.app_of.get(label).cloned() {
            groups.last.insert(anchor, frame);
        }
    }

    /// Settles what `label`'s window leaves behind once it is destroyed: the
    /// main window's frame or the log windows' size is saved, a log window
    /// leaves its group, and an app window's group is forgotten.
    pub fn window_destroyed(&self, label: &str) {
        if is_logs_label(label) {
            self.log_groups().app_of.remove(label);
            self.save_log_size();
            return;
        }
        if label == MAIN_LABEL {
            self.save_main_frame();
        }
        // Its log windows leave too, so one focused before its own destroy
        // cannot bring the group back.
        let mut groups = self.log_groups();
        groups.last.remove(label);
        groups.app_of.retain(|_, app| app != label);
    }

    /// Writes the main window's frame and the log windows' size to
    /// `state_path`.
    pub fn save_frames(&self) {
        self.save_main_frame();
        self.save_log_size();
    }

    fn save_main_frame(&self) {
        self.save(|| *self.main_frame(), window_state::write_main);
    }

    fn save_log_size(&self) {
        self.save(
            || *self.log_size.lock().unwrap_or_else(|p| p.into_inner()),
            window_state::write_log_size,
        );
    }

    /// Writes the value `read` returns to `state_path` with `write`, when
    /// there are both. Failures are logged.
    fn save<T>(
        &self,
        read: impl FnOnce() -> Option<T>,
        write: fn(&std::path::Path, &T) -> std::io::Result<()>,
    ) {
        let Some(path) = &self.state_path else {
            return;
        };
        // Read under the lock, so saves that race write in the order they read.
        let _lock = self.save_lock.lock().unwrap_or_else(|p| p.into_inner());
        let Some(value) = read() else {
            return;
        };
        if let Err(err) = write(path, &value) {
            warn!(%err, "failed to save a window's frame");
        }
    }

    /// Surface the main window: recreate it if the user closed it, unminimize it
    /// if minimized, then show and focus it. Opens nothing once a quit has begun.
    pub fn show_main_window<R: Runtime>(&self, app: &AppHandle<R>) -> Result<()> {
        if self.quitting() {
            return Ok(());
        }
        let window = match app.get_webview_window(MAIN_LABEL) {
            Some(window) => window,
            None => {
                let placed = self.main_window_placement();
                self.build_window(app, MAIN_LABEL, &APP_WINDOW, placed)?
            }
        };
        // Queued whatever surface returns: a step may fail after show() went out.
        let surfaced = surface(&window);
        self.hide_if_quitting(app, window);
        surfaced
    }

    /// Build a webview window. Per-platform chrome: macOS keeps decorations
    /// (Overlay title bar, hidden title, repositioned traffic lights —
    /// `traffic_light_position` requires decorations on); Linux/Windows are
    /// frameless, Linux additionally transparent. See
    /// docs/adr/2026-08-09-per-platform-window-chrome.md.
    ///
    /// Positioned explicitly on every platform at `placed` when there is one
    /// ([`Self::placement`]): AppKit auto-cascades an unpositioned window,
    /// Linux/Windows pile up.
    ///
    /// Every window it builds refuses to navigate off the app origin
    /// ([`is_app_origin`]) and refuses `window.open` outright.
    fn build_window<R: Runtime>(
        &self,
        app: &AppHandle<R>,
        label: &str,
        spec: &WindowSpec,
        placed: Option<SavedFrame>,
    ) -> Result<WebviewWindow<R>> {
        let url = WebviewUrl::App(spec.path.into());
        let mut builder = WebviewWindowBuilder::new(app, label, url)
            .title(WINDOW_TITLE)
            .min_inner_size(MIN_SIZE.0, MIN_SIZE.1)
            .maximized(placed.is_some_and(|placed| placed.maximized))
            .on_navigation(is_app_origin)
            .on_new_window(|url, _features| refuse_new_window(&url));

        builder = match placed {
            Some(SavedFrame { frame, .. }) => builder
                .inner_size(frame.width, frame.height)
                .position(frame.x, frame.y),
            // No monitor to measure against — let the OS place the window.
            None => builder.inner_size(spec.size.0, spec.size.1),
        };

        #[cfg(target_os = "macos")]
        {
            let (x, y) = traffic_light_position(spec.bar_height);
            builder = builder
                .title_bar_style(tauri::TitleBarStyle::Overlay)
                .hidden_title(true)
                .traffic_light_position(tauri::LogicalPosition::new(x, y));
        }

        // Non-macOS is frameless — the webview draws its own `WindowControls`.
        // Its webview takes the profile directory by path.
        #[cfg(not(target_os = "macos"))]
        {
            builder = builder.decorations(false);
            if let Some(dir) = crate::webview_profile::dir(app) {
                builder = builder.data_directory(dir);
            }
        }

        // Linux goes transparent so the webview's `WindowFrame` paints its own
        // border + shadow; Windows stays opaque (DWM already draws a borderless
        // window's shadow — a custom one would double-stack).
        #[cfg(target_os = "linux")]
        {
            builder = builder.transparent(true);
        }

        // One `host.json` read feeds both pre-paint paths below. A file with
        // no location degrades to defaults, never a failed build.
        let host_file = host_file::location(app)
            .map(|location| host_file::read(&location.path))
            .unwrap_or_default();

        // 1. Expose the file as `window.__KUBETAIL_HOST__` before any page
        //    script runs.
        builder = builder.initialization_script(host_file::init_script(&host_file));

        // 2. Opaque platforms: paint the native surface with the resolved
        //    scheme. Must be set unconditionally — it also clears the webview's
        //    white backing (see module docs). Linux is transparent: background
        //    must stay unset.
        #[cfg(not(target_os = "linux"))]
        {
            builder = builder.background_color(native_background(&host_file));
        }

        let window = builder.build()?;

        // Anchor the next cascade step (for paths with no focused window).
        *self.last_built.lock().unwrap_or_else(|p| p.into_inner()) = Some(label.to_string());

        // `debug-prod` compiles the inspector into a release build for
        // examining production behavior; never enabled in shipped builds — see
        // `Cargo.toml`.
        #[cfg(feature = "debug-prod")]
        window.open_devtools();

        Ok(window)
    }
}

#[cfg(test)]
mod tests {
    use super::{
        background_color_for, cascade_position, covers, fit_size, fits, is_app_origin,
        next_in_group, refuse_new_window, warn_on_error, Frame, WindowManager, APP_WINDOW,
    };
    use crate::host_file::ColorSchemePreference;
    use crate::logging::captured;
    use crate::services::sidecar::SidecarService;
    use crate::testutil::{mock_app, monitor, window, OneMonitor, Scratch};
    use crate::window_state::{self, SavedFrame, Size};
    use std::collections::HashSet;
    use std::sync::Arc;
    use std::thread;
    use tauri::test::MockRuntime;
    use tauri::webview::NewWindowResponse;
    use tauri::window::Monitor;
    use tauri::Manager;
    use tauri::Url;
    use tokio_util::sync::CancellationToken;

    #[test]
    fn the_bundled_app_origins_are_admitted() {
        // What the window actually loads: `tauri://localhost` on macOS/Linux,
        // `http://tauri.localhost` on Windows. `about:blank` is handed to the
        // callback by some engines during startup.
        for url in [
            "tauri://localhost",
            "tauri://localhost/dashboard",
            "http://tauri.localhost",
            "http://tauri.localhost/dashboard",
            "about:blank",
        ] {
            let url = Url::parse(url).expect("test URL parses");
            assert!(is_app_origin(&url), "{url} must be admitted");
        }
    }

    #[test]
    fn everything_that_is_not_the_app_is_refused() {
        // The reason the policy exists — a remote origin — plus the schemes a
        // script reaches for to carry data out or run code in, and a host that
        // merely looks like the app's. The allowlist ends in `false`, so an
        // exotic scheme nobody listed here is refused with them.
        for url in [
            "https://example.com",
            "http://example.com",
            "https://tauri.localhost.example.com",
            "http://tauri.localhost:8080",
            "https://tauri.localhost",
            "file:///etc/passwd",
            "data:text/html,<h1>x</h1>",
            "javascript:alert(1)",
            "about:srcdoc",
            "kubetail://localhost",
        ] {
            let url = Url::parse(url).expect("test URL parses");
            assert!(!is_app_origin(&url), "{url} must be refused");
        }
    }

    #[test]
    fn the_dev_server_is_admitted_only_by_a_debug_build() {
        // `pnpm tauri dev` serves the webview from localhost, so a debug build
        // must admit it — and a release build must not, which is what keeps the
        // shipped app's allowlist to the bundle alone. One assertion, so
        // whichever mode the suite is built in pins its own half.
        for url in ["http://localhost:1420", "http://localhost:1420/dashboard"] {
            let url = Url::parse(url).expect("test URL parses");
            assert_eq!(is_app_origin(&url), cfg!(debug_assertions), "{url}");
        }
    }

    #[test]
    fn build_window_installs_the_navigation_policy() {
        // Both callbacks are the containment for a page that tries to leave, and
        // nothing else in the process reinstates them — an edit to the builder
        // that drops one is silent at runtime, and the mock runtime never
        // navigates or opens a popup to call them. So read the source, the way
        // `config_declares_no_windows` reads the config.
        let source = include_str!("window_manager.rs");
        let (_, after) = source
            .split_once("fn build_window")
            .expect("window_manager defines build_window");
        // Stop at the test module, whose own assertions quote the strings below.
        let build_window = after
            .split_once("#[cfg(test)]")
            .expect("window_manager has a test module")
            .0;

        assert!(
            build_window.contains(".on_navigation(is_app_origin)"),
            "build_window must refuse navigation off the app origin"
        );
        assert!(
            build_window.contains("refuse_new_window"),
            "build_window must refuse `window.open`"
        );
    }

    #[test]
    fn a_new_window_request_is_refused() {
        let url = Url::parse("https://example.com").unwrap();
        assert!(matches!(
            refuse_new_window::<MockRuntime>(&url),
            NewWindowResponse::Deny
        ));
    }

    #[test]
    fn a_failed_window_call_is_logged() {
        let logged = crate::logging::captured(|| {
            warn_on_error(Ok(()), "not this");
            warn_on_error(Err::<(), _>(tauri::Error::UnknownPath), "failed to hide");
        });
        assert!(logged.contains("failed to hide"), "got: {logged}");
        assert!(!logged.contains("not this"), "got: {logged}");
    }

    #[test]
    fn explicit_preference_ignores_the_os_scheme() {
        // An explicit choice wins over the OS in both directions — that's what
        // makes the app's scheme, not the OS's, the one on screen at t0.
        for os_prefers_dark in [false, true] {
            assert_eq!(
                background_color_for(Some(ColorSchemePreference::Light), os_prefers_dark),
                super::LIGHT_BACKGROUND
            );
            assert_eq!(
                background_color_for(Some(ColorSchemePreference::Dark), os_prefers_dark),
                super::DARK_BACKGROUND
            );
        }
    }

    #[test]
    fn system_preference_follows_the_os_scheme() {
        // `system`, and a file predating the setting, defer to the OS. Never
        // `None`: wry only clears the webview's white backing when a color is
        // set, so opting out here would reinstate the launch flash.
        for preference in [Some(ColorSchemePreference::System), None] {
            assert_eq!(
                background_color_for(preference, true),
                super::DARK_BACKGROUND
            );
            assert_eq!(
                background_color_for(preference, false),
                super::LIGHT_BACKGROUND
            );
        }
    }

    fn frame(x: f64, y: f64, width: f64, height: f64) -> Frame {
        Frame {
            x,
            y,
            width,
            height,
        }
    }

    /// A manager on no readable monitor.
    fn manager() -> WindowManager {
        WindowManager::new(CancellationToken::new(), OneMonitor(None), None)
    }

    #[test]
    fn labels_start_at_one() {
        let wm = manager();
        assert_eq!(wm.next_label(), "window-1");
    }

    /// The first window centers on the primary monitor at the default size;
    /// the next steps off it, on its monitor, at its size. The mock reports
    /// every window at the origin with no size, which is raised to the minimum.
    #[test]
    fn windows_open_centered_then_cascade() {
        let scratch = Scratch::new();
        let app = mock_app(scratch.path(), SidecarService::without_process(None));
        // A 1920x1080 logical work area at scale 2.
        let wm = WindowManager::new(
            CancellationToken::new(),
            OneMonitor(Some(monitor((0, 0, 3840, 2160), 2.0))),
            None,
        );

        assert_eq!(
            wm.placement(app.handle(), None, &APP_WINDOW),
            Some(frame(360.0, 160.0, 1200.0, 760.0))
        );
        wm.new_window(app.handle()).unwrap();
        assert!(app.get_webview_window("window-1").is_some());

        assert_eq!(
            wm.placement(app.handle(), None, &APP_WINDOW),
            Some(frame(28.0, 28.0, 600.0, 400.0))
        );
        wm.new_window(app.handle()).unwrap();
        assert!(app.get_webview_window("window-2").is_some());
    }

    /// With no monitor to measure against, the OS places the window.
    #[test]
    fn a_window_opens_with_no_monitor_readable() {
        let scratch = Scratch::new();
        let app = mock_app(scratch.path(), SidecarService::without_process(None));
        let wm = manager();

        assert_eq!(wm.placement(app.handle(), None, &APP_WINDOW), None);
        wm.new_window(app.handle()).unwrap();
        assert!(app.get_webview_window("window-1").is_some());
    }

    #[test]
    fn a_closed_main_window_is_rebuilt() {
        let scratch = Scratch::new();
        let app = mock_app(scratch.path(), SidecarService::without_process(None));

        manager().show_main_window(app.handle()).unwrap();

        assert!(app.get_webview_window("main").is_some());
    }

    #[test]
    fn an_open_main_window_is_surfaced_not_rebuilt() {
        let scratch = Scratch::new();
        let app = mock_app(scratch.path(), SidecarService::without_process(None));
        window(&app, "main");

        manager().show_main_window(app.handle()).unwrap();

        assert_eq!(app.webview_windows().len(), 1);
    }

    /// A quit hides windows and closes none.
    #[test]
    fn hiding_for_a_quit_closes_no_window() {
        let scratch = Scratch::new();
        let app = mock_app(scratch.path(), SidecarService::without_process(None));
        let main = window(&app, "main");
        let shutdown = CancellationToken::new();
        let wm = WindowManager::new(shutdown.clone(), OneMonitor(None), None);
        shutdown.cancel();

        wm.hide_if_quitting(app.handle(), main);
        wm.hide_all(app.handle());

        assert_eq!(app.webview_windows().len(), 1);
    }

    /// With no window focused (a tray click), the cascade anchors on the last
    /// window built, while it is still open.
    #[test]
    fn the_anchor_is_the_last_window_built_while_it_is_open() {
        let scratch = Scratch::new();
        let app = mock_app(scratch.path(), SidecarService::without_process(None));
        let wm = manager();
        window(&app, "window-1");
        assert!(
            wm.anchor_window(app.handle(), None).is_none(),
            "none built yet"
        );

        *wm.last_built.lock().unwrap() = Some("window-1".into());
        let anchor = wm.anchor_window(app.handle(), None).unwrap();
        assert_eq!(anchor.label(), "window-1");

        *wm.last_built.lock().unwrap() = Some("window-2".into());
        assert!(wm.anchor_window(app.handle(), None).is_none(), "closed");
    }

    #[test]
    fn a_log_window_is_labelled_off_the_window_counter() {
        let wm = manager();
        assert_eq!(wm.next_label(), "window-1");
        assert_eq!(wm.next_logs_label(), "logs-2");
    }

    #[test]
    fn labels_have_a_kind() {
        use super::{is_app_label, is_logs_label};
        for label in ["main", "window-1"] {
            assert!(is_app_label(label) && !is_logs_label(label), "{label}");
        }
        assert!(is_logs_label("logs-2") && !is_app_label("logs-2"));
        for label in ["mainx", "windows", "logs", "other"] {
            assert!(!is_app_label(label) && !is_logs_label(label), "{label}");
        }
    }

    /// The anchor is the app window that opened it, even when another window
    /// was built last. The mock puts every window at the origin, so this checks
    /// the window chosen, not the position.
    #[test]
    fn a_log_window_anchors_on_its_app_window() {
        let scratch = Scratch::new();
        let app = mock_app(scratch.path(), SidecarService::without_process(None));
        let wm = manager();
        window(&app, "main");
        window(&app, "window-1");
        *wm.last_built.lock().unwrap() = Some("window-1".into());

        let anchor = wm.anchor_window(app.handle(), Some("main")).unwrap();

        assert_eq!(anchor.label(), "main");
    }

    /// Tauri makes the data directory a window is built with, so its
    /// existence shows the window took the managed profile.
    #[cfg(not(target_os = "macos"))]
    #[test]
    fn a_window_opens_the_managed_webview_profile() {
        let scratch = Scratch::new();
        let app = mock_app(scratch.path(), SidecarService::without_process(None));
        crate::webview_profile::manage(app.handle(), &crate::testutil::folders(scratch.path()));
        let profile = crate::webview_profile::dir(app.handle()).unwrap();
        assert!(!profile.exists());
        let wm = WindowManager::new(CancellationToken::new(), OneMonitor(None), None);

        wm.new_window(app.handle()).unwrap();

        assert!(profile.is_dir());
    }

    /// An app window takes the default size off a log window, not its size.
    /// The mock's windows have no size.
    #[test]
    fn an_app_window_does_not_take_the_size_of_a_log_window() {
        let scratch = Scratch::new();
        let app = mock_app(scratch.path(), SidecarService::without_process(None));
        let wm = WindowManager::new(
            CancellationToken::new(),
            OneMonitor(Some(monitor((0, 0, 1920, 1080), 1.0))),
            None,
        );
        window(&app, "logs-1");

        assert_eq!(
            wm.placement(app.handle(), Some("logs-1"), &APP_WINDOW),
            Some(frame(28.0, 28.0, 1200.0, 760.0))
        );
    }

    const AREAS: [(f64, f64, f64, f64); 2] =
        [(0.0, 0.0, 1920.0, 1080.0), (1920.0, 0.0, 1920.0, 1080.0)];

    #[test]
    fn a_group_with_no_last_frame_opens_at_the_fallback() {
        let fallback = frame(28.0, 28.0, 960.0, 640.0);

        assert_eq!(
            next_in_group(None, false, &AREAS, Some(fallback)),
            Some(fallback)
        );
        assert_eq!(next_in_group(None, false, &AREAS, None), None);
    }

    /// On the second monitor, at a size larger than a new window's share.
    #[test]
    fn an_open_group_cascades_off_its_last_frame() {
        let last = frame(1930.0, 10.0, 1700.0, 900.0);

        assert_eq!(
            next_in_group(Some(last), true, &AREAS, None),
            Some(frame(1958.0, 38.0, 1632.0, 900.0))
        );
    }

    #[test]
    fn a_closed_group_reopens_at_its_last_frame() {
        let last = frame(1930.0, 10.0, 1700.0, 900.0);

        assert_eq!(next_in_group(Some(last), false, &AREAS, None), Some(last));
    }

    #[test]
    fn a_last_frame_on_a_missing_monitor_gives_way_to_the_fallback() {
        let last = frame(4000.0, 10.0, 960.0, 640.0);
        let fallback = frame(28.0, 28.0, 960.0, 640.0);

        assert_eq!(
            next_in_group(Some(last), true, &AREAS, Some(fallback)),
            Some(fallback)
        );
    }

    /// The step off the app window takes the size the file saved.
    #[test]
    fn a_groups_first_log_window_steps_off_its_app_window_at_the_saved_size() {
        let scratch = Scratch::new();
        let (app, wm) = on_one_monitor(&scratch);
        window(&app, "main");
        let size = Size {
            width: 1100.0,
            height: 700.0,
        };
        window_state::write_log_size(&state_path(&scratch), &size).unwrap();

        assert_eq!(
            wm.log_window_placement(app.handle(), "logs-1", "main"),
            Some(saved(frame(28.0, 28.0, 1100.0, 700.0), false))
        );
    }

    /// A size remembered this session wins over the file's, and a manager
    /// with neither opens a log window at the default size.
    #[test]
    fn a_remembered_log_size_wins_over_the_file() {
        let scratch = Scratch::new();
        let (_app, wm) = on_one_monitor(&scratch);
        let filed = Size {
            width: 1100.0,
            height: 700.0,
        };
        window_state::write_log_size(&state_path(&scratch), &filed).unwrap();
        assert_eq!(wm.log_size(), Some(filed));

        wm.remember_log_frame("logs-1", saved(frame(0.0, 0.0, 900.0, 600.0), false));
        let remembered = Size {
            width: 900.0,
            height: 600.0,
        };
        assert_eq!(wm.log_size(), Some(remembered));
        assert_eq!(manager().log_size(), None, "no state path");
    }

    /// Each app window's log windows step off their own group's last one,
    /// even while none of them is built yet. The mock puts every window at
    /// the origin.
    #[test]
    fn each_app_windows_log_windows_cascade_together() {
        let scratch = Scratch::new();
        let (app, wm) = on_one_monitor(&scratch);
        window(&app, "main");
        window(&app, "window-1");
        let place = |label, anchor| {
            wm.log_window_placement(app.handle(), label, anchor)
                .map(|placed| (placed.frame.x, placed.frame.y))
        };

        assert_eq!(place("logs-2", "main"), Some((28.0, 28.0)));
        assert_eq!(place("logs-3", "main"), Some((56.0, 56.0)));
        assert_eq!(place("logs-4", "window-1"), Some((28.0, 28.0)));
        assert_eq!(place("logs-5", "main"), Some((84.0, 84.0)));
    }

    /// A group whose windows all closed reopens where its last one was, until
    /// its app window goes.
    #[test]
    fn a_group_reopens_where_its_last_window_was() {
        let scratch = Scratch::new();
        let (app, wm) = on_one_monitor(&scratch);
        window(&app, "main");
        let first = wm.log_window_placement(app.handle(), "logs-1", "main");
        wm.window_destroyed("logs-1");

        assert_eq!(
            wm.log_window_placement(app.handle(), "logs-2", "main"),
            first
        );

        wm.window_destroyed("logs-2");
        wm.window_destroyed("main");
        assert!(
            wm.log_groups().last.is_empty(),
            "forgotten with its app window"
        );
    }

    /// A log window seen last, by a move, resize or focus, is the one the
    /// next steps off. The mock reports every window at the origin with no
    /// size, which is ignored.
    #[test]
    fn a_seen_log_window_becomes_its_groups_last() {
        let scratch = Scratch::new();
        let (app, wm) = on_one_monitor(&scratch);
        window(&app, "main");
        window(&app, "logs-1");
        wm.log_window_placement(app.handle(), "logs-1", "main");
        let placed = wm.log_window_placement(app.handle(), "logs-2", "main");

        wm.window_focused(app.handle(), "logs-1");
        wm.window_focused(app.handle(), "main");
        assert_eq!(
            wm.log_groups().last.get("main").copied(),
            placed.map(|placed| placed.frame),
            "a sizeless frame is ignored"
        );

        let seen = frame(300.0, 200.0, 900.0, 600.0);
        wm.remember_log_frame("logs-1", saved(seen, false));
        assert_eq!(wm.log_groups().last.get("main"), Some(&seen));
    }

    /// A log window's size is saved when it is destroyed, beside the main
    /// window's frame.
    #[test]
    fn a_log_windows_size_is_saved_beside_the_main_windows_frame() {
        let scratch = Scratch::new();
        let (app, wm) = on_one_monitor(&scratch);
        let path = state_path(&scratch);
        let main = saved(frame(300.0, 250.0, 1200.0, 760.0), false);
        save(&scratch, &main);
        window(&app, "logs-1");

        wm.window_moved(app.handle(), "logs-1");
        wm.window_destroyed("logs-1");
        assert_eq!(window_state::read_log_size(&path), None, "sizeless");

        wm.remember_log_frame("logs-1", saved(frame(0.0, 0.0, 900.0, 600.0), false));
        wm.window_destroyed("logs-1");
        let size = Size {
            width: 900.0,
            height: 600.0,
        };
        assert_eq!(window_state::read_log_size(&path), Some(size));
        assert_eq!(window_state::read_main(&path), Some(main));
    }

    #[test]
    fn a_maximized_or_sizeless_log_window_is_not_remembered() {
        let scratch = Scratch::new();
        let (_app, wm) = on_one_monitor(&scratch);

        wm.remember_log_frame("logs-1", saved(frame(0.0, 0.0, 900.0, 600.0), true));
        wm.remember_log_frame("logs-1", saved(frame(0.0, 0.0, 900.0, 0.0), false));
        wm.remember_log_frame("logs-1", saved(frame(0.0, 0.0, 0.0, 600.0), false));

        assert_eq!(*wm.log_size.lock().unwrap(), None);
    }

    /// Once its app window is gone, a log window still open no longer
    /// belongs to a group.
    #[test]
    fn an_app_windows_destroy_ends_its_group() {
        let scratch = Scratch::new();
        let (app, wm) = on_one_monitor(&scratch);
        window(&app, "main");
        wm.log_window_placement(app.handle(), "logs-1", "main");

        wm.window_destroyed("main");
        wm.remember_log_frame("logs-1", saved(frame(0.0, 0.0, 900.0, 600.0), false));

        assert!(wm.log_groups().last.is_empty());
        assert!(wm.log_groups().app_of.is_empty());
    }

    /// A build that opened nothing leaves its group, so the group's next
    /// window reopens at its last frame rather than stepping off it.
    #[test]
    fn a_log_window_that_opens_nothing_leaves_its_group() {
        let scratch = Scratch::new();
        let app = mock_app(scratch.path(), SidecarService::without_process(None));
        let shutdown = CancellationToken::new();
        let wm = WindowManager::new(shutdown.clone(), OneMonitor(None), None);
        shutdown.cancel();

        wm.open_log_window(app.handle(), "logs-1", "main").unwrap();

        assert!(wm.log_groups().app_of.is_empty());
    }

    #[test]
    fn a_log_window_is_built_and_surfaced() {
        let scratch = Scratch::new();
        let app = mock_app(scratch.path(), SidecarService::without_process(None));
        let wm = manager();
        window(&app, "main");

        let built = wm.open_log_window(app.handle(), "logs-1", "main").unwrap();

        assert_eq!(built.unwrap().label(), "logs-1");
        assert!(wm.surface_window(app.handle(), "logs-1").unwrap());
        assert!(!wm.surface_window(app.handle(), "logs-9").unwrap(), "gone");
    }

    #[test]
    fn no_log_window_opens_or_shows_once_a_quit_has_begun() {
        let scratch = Scratch::new();
        let app = mock_app(scratch.path(), SidecarService::without_process(None));
        let shutdown = CancellationToken::new();
        let wm = WindowManager::new(shutdown.clone(), OneMonitor(None), None);
        window(&app, "logs-1");
        shutdown.cancel();

        let built = wm.open_log_window(app.handle(), "logs-2", "main").unwrap();

        assert!(built.is_none());
        assert!(app.get_webview_window("logs-2").is_none());
        assert!(!wm.surface_window(app.handle(), "logs-1").unwrap());
    }

    #[test]
    fn traffic_lights_center_vertically_in_the_app_bar() {
        let (x, y) = super::traffic_light_position(super::APP_BAR_HEIGHT);
        assert_eq!(x, super::TRAFFIC_LIGHT_LEFT_INSET);

        // Assert what shows on screen, not the formula: the gap above the buttons
        // is `y` less their native inset, and it must match the gap below.
        let above = y - super::TRAFFIC_LIGHT_NATIVE_INSET;
        let below = super::APP_BAR_HEIGHT - above - super::TRAFFIC_LIGHT_DIAMETER;
        assert_eq!(above, below);
    }

    #[test]
    fn config_declares_no_windows() {
        // Every window is created in code (`build_window`) so the chrome — and
        // the pre-paint background/init-script it will carry — is defined in
        // exactly one place. A window declared in the config would bypass all
        // of it, so pin the config to declaring none.
        let config: serde_json::Value =
            serde_json::from_str(include_str!("../tauri.conf.json")).expect("valid config JSON");
        assert_eq!(
            config["app"]["windows"].as_array().map(Vec::len),
            Some(0),
            "windows must be created via build_window, not declared in tauri.conf.json"
        );
    }

    #[test]
    fn production_csp_admits_no_remote_code() {
        // The CSP is the containment for a compromised page: the host forwards
        // whatever operation the webview sends, so script execution there carries
        // the whole cluster surface. See
        // docs/adr/2026-09-03-no-graphql-operation-allowlist.md. It is pinned here
        // because `build_window` creates every webview the policy governs.
        let config: serde_json::Value =
            serde_json::from_str(include_str!("../tauri.conf.json")).expect("valid config JSON");
        let csp = &config["app"]["security"]["csp"];

        assert_eq!(
            csp["script-src"].as_str(),
            Some("'self'"),
            "script-src must admit the bundle alone — no remote origin, no 'unsafe-inline', no 'unsafe-eval'"
        );
        assert_eq!(csp["default-src"].as_str(), Some("'self'"));
        for directive in ["object-src", "base-uri", "frame-src", "form-action"] {
            assert_eq!(
                csp[directive].as_str(),
                Some("'none'"),
                "{directive} must admit nothing"
            );
        }

        // Every directive, so a source added to one nobody thought to name above is
        // still caught. Tauri's asset and IPC hosts are the only origins with a
        // host part; everything else is a keyword or a scheme (`data:`, `ipc:`).
        for (directive, value) in csp.as_object().expect("csp is a directive map") {
            let value = value
                .as_str()
                .unwrap_or_else(|| panic!("{directive} must be a string"));
            assert!(
                !value.contains("unsafe-eval"),
                "{directive} admits 'unsafe-eval'"
            );
            for source in value.split_whitespace() {
                assert!(
                    !source.contains("//")
                        || source == "http://asset.localhost"
                        || source == "http://ipc.localhost",
                    "{directive} admits the remote origin {source}"
                );
            }
        }
    }

    #[test]
    fn default_capability_grants_what_the_app_window_calls() {
        // A capability is authority handed to a page that already holds the whole
        // cluster surface, so it is granted when a consumer needs it and not before.
        // Nothing judges that but a reader, so pin the list: a new permission has to
        // be added here too, where the diff is about authority rather than a feature.
        // The list grants no emit, so a page cannot send an event as the host, and
        // no menu, tray, image, resources, path or app API, which nothing in `src/`
        // calls. Pinned beside the CSP because `build_window` creates the windows this
        // capability names.
        let capability: serde_json::Value =
            serde_json::from_str(include_str!("../capabilities/default.json"))
                .expect("valid capability JSON");
        let permissions: Vec<&str> = capability["permissions"]
            .as_array()
            .expect("permissions is a list")
            .iter()
            .map(|p| p.as_str().expect("permission is a string"))
            .collect();
        assert_eq!(
            permissions,
            [
                "core:window:default",
                "core:webview:default",
                "core:event:allow-listen",
                "core:event:allow-unlisten",
                "core:window:allow-start-dragging",
                "core:window:allow-start-resize-dragging",
                "core:window:allow-minimize",
                "core:window:allow-toggle-maximize",
                "core:window:allow-close",
                "allow-ready",
                "allow-graphql-query",
                "allow-graphql-subscribe",
                "allow-graphql-unsubscribe",
                "allow-new-window",
                "allow-update-host-file",
                "allow-read-host-file",
                "allow-quit",
                "allow-webview-record-error",
                "allow-open-account-url",
                "allow-log-window-subscribe",
                "allow-log-window-open",
                "allow-log-window-focus",
                "allow-log-window-close-all",
            ],
            "the app window holds what it calls and nothing else — add a permission here only with the consumer that needs it"
        );
        assert_eq!(
            capability["windows"],
            serde_json::json!(["main", "window-*"])
        );
    }

    #[test]
    fn logs_capability_grants_the_viewer_and_nothing_else() {
        // A log window reads logs and draws its own bar. It opens no window,
        // changes no preference, quits nothing, opens no URL and calls no
        // app-window log command.
        let capability: serde_json::Value =
            serde_json::from_str(include_str!("../capabilities/logs.json"))
                .expect("valid capability JSON");
        assert_eq!(capability["windows"], serde_json::json!(["logs-*"]));
        let permissions: Vec<&str> = capability["permissions"]
            .as_array()
            .expect("permissions is a list")
            .iter()
            .map(|p| p.as_str().expect("permission is a string"))
            .collect();
        assert_eq!(
            permissions,
            [
                "core:window:default",
                "core:webview:default",
                "core:event:allow-listen",
                "core:event:allow-unlisten",
                "core:window:allow-start-dragging",
                "core:window:allow-start-resize-dragging",
                "core:window:allow-minimize",
                "core:window:allow-toggle-maximize",
                "core:window:allow-close",
                "allow-ready",
                "allow-graphql-query",
                "allow-graphql-subscribe",
                "allow-graphql-unsubscribe",
                "allow-webview-record-error",
                "allow-log-window-view",
                "allow-log-window-viewport",
                "allow-log-window-title",
                "allow-log-window-show-chat",
            ],
            "a log window holds what it calls and nothing else"
        );
    }

    #[test]
    fn labels_increase_monotonically() {
        let wm = manager();
        assert_eq!(wm.next_label(), "window-1");
        assert_eq!(wm.next_label(), "window-2");
        assert_eq!(wm.next_label(), "window-3");
    }

    #[test]
    fn each_manager_has_its_own_counter() {
        let a = manager();
        let b = manager();
        assert_eq!(a.next_label(), "window-1");
        assert_eq!(a.next_label(), "window-2");
        // `b` is independent — it is not advanced by calls on `a`.
        assert_eq!(b.next_label(), "window-1");
    }

    #[test]
    fn concurrent_labels_are_unique() {
        const THREADS: usize = 8;
        const PER_THREAD: usize = 250;

        let wm = Arc::new(manager());
        let handles: Vec<_> = (0..THREADS)
            .map(|_| {
                let wm = Arc::clone(&wm);
                thread::spawn(move || (0..PER_THREAD).map(|_| wm.next_label()).collect::<Vec<_>>())
            })
            .collect();

        let mut seen = HashSet::new();
        for handle in handles {
            for label in handle.join().expect("worker thread panicked") {
                assert!(
                    seen.insert(label.clone()),
                    "label {label} was handed out twice"
                );
            }
        }
        assert_eq!(seen.len(), THREADS * PER_THREAD);
    }

    // A roomy 1920x1080 work area at the origin, and a window well inside it.
    const WINDOW: (f64, f64) = (800.0, 600.0);
    const ROOMY: (f64, f64, f64, f64) = (0.0, 0.0, 1920.0, 1080.0);
    const STEP: f64 = super::CASCADE_STEP;

    #[test]
    fn the_first_window_of_the_session_is_centered() {
        // Nothing to cascade off, so the app opens where the user expects
        // rather than pre-offset toward one corner.
        assert_eq!(cascade_position(None, WINDOW, ROOMY), (560.0, 240.0));
    }

    #[test]
    fn a_window_opens_one_step_down_right_of_its_anchor() {
        // The whole point: cascade off wherever the anchor actually is — including
        // somewhere the user dragged it, nowhere near the centered position — and
        // not off a position derived from how many windows have been opened.
        for anchor in [
            (100.0, 80.0),
            (12.0, 400.0),
            cascade_position(None, WINDOW, ROOMY),
        ] {
            assert_eq!(
                cascade_position(Some(anchor), WINDOW, ROOMY),
                (anchor.0 + STEP, anchor.1 + STEP)
            );
        }
    }

    #[test]
    fn the_cascade_restarts_from_the_center_when_the_next_step_would_not_fit() {
        // An anchor flush against the work area's bottom-right corner: stepping
        // again would hang the window off-screen, so wrap to the center.
        let centered = cascade_position(None, WINDOW, ROOMY);
        let corner = (1920.0 - WINDOW.0, 1080.0 - WINDOW.1);
        assert_eq!(cascade_position(Some(corner), WINDOW, ROOMY), centered);
    }

    #[test]
    fn an_anchor_dragged_off_screen_cannot_drag_the_new_window_with_it() {
        // A window mostly off the right edge, or on a monitor we are not
        // measuring against: the step does not fit, so the new window lands
        // centered and visible instead of following it into the void.
        let centered = cascade_position(None, WINDOW, ROOMY);
        for anchor in [(1900.0, 40.0), (-780.0, 40.0), (3000.0, 3000.0)] {
            assert_eq!(cascade_position(Some(anchor), WINDOW, ROOMY), centered);
        }
    }

    #[test]
    fn a_chain_of_cascades_never_leaves_the_work_area() {
        // The invariant the function exists for, exercised the way the app uses
        // it: each window anchors on the one before it, for many windows, on
        // several monitor shapes — including one too short to step even once.
        for area in [
            ROOMY,
            (0.0, 0.0, 1024.0, 700.0),
            (100.0, 50.0, 1280.0, 800.0),
            // 640px tall leaves 20px below the centered window — less than one
            // step, so this wraps on every single window.
            (0.0, 0.0, 1920.0, 640.0),
        ] {
            let (area_x, area_y, area_w, area_h) = area;
            let mut anchor = None;
            for window in 0..32 {
                let (x, y) = cascade_position(anchor, WINDOW, area);
                assert!(
                    x >= area_x && y >= area_y,
                    "window {window} at ({x}, {y}) starts before {area:?}"
                );
                assert!(
                    x + WINDOW.0 <= area_x + area_w && y + WINDOW.1 <= area_y + area_h,
                    "window {window} at ({x}, {y}) overflows {area:?}"
                );
                anchor = Some((x, y));
            }
        }
    }

    #[test]
    fn a_window_larger_than_the_work_area_pins_to_the_origin() {
        // Nowhere to cascade into, and centering would push the title bar (with
        // the traffic lights / window controls) off the top-left of the screen.
        let tiny = (0.0, 0.0, 640.0, 480.0);
        assert_eq!(cascade_position(None, WINDOW, tiny), (0.0, 0.0));
        assert_eq!(cascade_position(Some((0.0, 0.0)), WINDOW, tiny), (0.0, 0.0));
    }

    #[test]
    fn the_cascade_is_relative_to_the_work_areas_origin() {
        // A second monitor to the right of the primary, with a 30px menu bar:
        // positions are absolute in the virtual desktop, so the work area's
        // origin must carry through instead of being dropped.
        let secondary = (1920.0, 30.0, 1920.0, 1050.0);
        let centered = cascade_position(None, WINDOW, secondary);
        assert_eq!(centered, (2480.0, 255.0));
        assert_eq!(
            cascade_position(Some(centered), WINDOW, secondary),
            (2480.0 + STEP, 255.0 + STEP)
        );
    }

    #[test]
    fn a_large_screen_gets_the_default_size() {
        assert_eq!(
            fit_size(APP_WINDOW.size, (0.0, 0.0, 2560.0, 1415.0)),
            (1200.0, 760.0)
        );
    }

    /// A 13.6" MacBook Air at its default 1470x956, less the menu bar and a
    /// visible Dock: the width keeps the default and the height is capped.
    #[test]
    fn a_macbook_air_keeps_the_default_width() {
        let (width, height) = fit_size(APP_WINDOW.size, (0.0, 37.0, 1470.0, 845.0));
        assert_eq!(width, 1200.0);
        assert!((height - 718.25).abs() < 1e-9, "{height}");
    }

    #[test]
    fn a_small_screen_gets_a_share_of_its_work_area() {
        // A 1366x768 laptop less a 40px taskbar.
        let (width, height) = fit_size(APP_WINDOW.size, (0.0, 0.0, 1366.0, 728.0));
        assert!((width - 1161.1).abs() < 1e-9, "{width}");
        assert!((height - 618.8).abs() < 1e-9, "{height}");
    }

    #[test]
    fn no_window_is_sized_below_the_minimum() {
        assert_eq!(
            fit_size(APP_WINDOW.size, (0.0, 0.0, 640.0, 400.0)),
            (600.0, 400.0)
        );
    }

    /// A frame fits a work area it is no larger than, with enough of its
    /// title bar on it to grab; a sliver or a title bar off the top does not.
    #[test]
    fn a_frame_fits_a_work_area_its_title_bar_is_reachable_on() {
        let area = (0.0, 0.0, 1920.0, 1080.0);
        let at = |x, y| frame(x, y, 1200.0, 760.0);

        assert!(fits(&at(100.0, 50.0), area));
        assert!(fits(&at(1820.0, 50.0), area), "100px left");
        assert!(fits(&at(-1100.0, 50.0), area), "100px right");
        assert!(!fits(&at(1919.0, 50.0), area), "a sliver");
        assert!(!fits(&at(-1150.0, 50.0), area), "50px");
        assert!(!fits(&at(100.0, -10.0), area), "above");
        assert!(!fits(&at(100.0, 1050.0), area), "below");
        assert!(!fits(&at(3000.0, 50.0), area), "on a monitor that is gone");
        assert!(!fits(&frame(0.0, 0.0, 2560.0, 760.0), area), "too wide");
        assert!(!fits(&frame(0.0, 0.0, 1200.0, 1440.0), area), "too tall");
    }

    #[test]
    fn a_frame_as_large_as_its_work_area_covers_it() {
        let area = (0.0, 40.0, 1920.0, 1040.0);

        assert!(covers(&frame(0.0, 40.0, 1920.0, 1040.0), area));
        assert!(
            covers(&frame(-8.0, -8.0, 1936.0, 1096.0), area),
            "overhangs"
        );
        assert!(!covers(&frame(0.0, 40.0, 960.0, 1040.0), area), "half");
        assert!(!covers(&frame(0.0, 40.0, 1920.0, 760.0), area), "shorter");
    }

    /// A manager on `monitors`, the first the primary and every window's,
    /// keeping its frame in `scratch`; and an app.
    fn on(monitors: Vec<Monitor>, scratch: &Scratch) -> (tauri::App<MockRuntime>, WindowManager) {
        let app = mock_app(scratch.path(), SidecarService::without_process(None));
        let wm = WindowManager::new(
            CancellationToken::new(),
            These(monitors),
            Some(state_path(scratch)),
        );
        (app, wm)
    }

    /// These monitors, the first the primary and every window's.
    struct These(Vec<Monitor>);

    impl super::Monitors for These {
        fn of_window(&self, _label: &str) -> Option<Monitor> {
            self.0.first().cloned()
        }

        fn primary(&self) -> Option<Monitor> {
            self.0.first().cloned()
        }

        fn all(&self) -> Vec<Monitor> {
            self.0.clone()
        }
    }

    /// [`on`] one 1920x1080 monitor at 100%.
    fn on_one_monitor(scratch: &Scratch) -> (tauri::App<MockRuntime>, WindowManager) {
        on(vec![monitor((0, 0, 1920, 1080), 1.0)], scratch)
    }

    fn state_path(scratch: &Scratch) -> std::path::PathBuf {
        scratch.path().join("window-state.json")
    }

    fn save(scratch: &Scratch, saved: &SavedFrame) {
        window_state::write_main(&state_path(scratch), saved).unwrap();
    }

    fn saved(frame: Frame, maximized: bool) -> SavedFrame {
        SavedFrame { frame, maximized }
    }

    /// The default, centered on [`on_one_monitor`]'s: what the main window
    /// opens at with nothing saved, or nothing it can restore.
    const CENTERED: Frame = Frame {
        x: 360.0,
        y: 160.0,
        width: 1200.0,
        height: 760.0,
    };

    #[test]
    fn with_nothing_saved_the_main_window_opens_centered_and_remembers_it() {
        let scratch = Scratch::new();
        let (_app, wm) = on_one_monitor(&scratch);

        assert_eq!(wm.main_window_placement(), Some(saved(CENTERED, false)));
        assert_eq!(*wm.main_frame.lock().unwrap(), Some(saved(CENTERED, false)));
    }

    /// A frame on the second monitor, maximized.
    #[test]
    fn the_main_window_opens_at_its_saved_frame() {
        let scratch = Scratch::new();
        let monitors = vec![
            monitor((0, 0, 2880, 1800), 2.0),
            monitor((1440, 0, 1920, 1080), 1.0),
        ];
        let (app, wm) = on(monitors, &scratch);
        let on_second = saved(frame(1600.0, 50.0, 1200.0, 760.0), true);
        save(&scratch, &on_second);

        assert_eq!(wm.main_window_placement(), Some(on_second));
        wm.show_main_window(app.handle()).unwrap();
        assert!(app.get_webview_window("main").is_some());
    }

    /// A maximized window saved on a monitor that is gone opens centered on
    /// the primary, and that is the frame it saves if it is never
    /// unmaximized, not the one on the missing monitor.
    #[test]
    fn a_frame_saved_on_a_missing_monitor_is_replaced_by_the_centered_one() {
        let scratch = Scratch::new();
        let (_app, wm) = on_one_monitor(&scratch);
        save(&scratch, &saved(frame(2500.0, 50.0, 1200.0, 760.0), true));

        assert_eq!(wm.main_window_placement(), Some(saved(CENTERED, true)));
        wm.save_frames();
        assert_eq!(
            window_state::read_main(&state_path(&scratch)),
            Some(saved(CENTERED, true))
        );
    }

    /// With no monitor to measure against, the OS places the main window.
    #[test]
    fn the_main_window_has_no_placement_with_no_monitor_readable() {
        let scratch = Scratch::new();
        let (_app, wm) = on(Vec::new(), &scratch);
        assert_eq!(wm.main_window_placement(), None);
    }

    /// Moving the main window is remembered and written on save; another
    /// window's move is not. The mock reports every window at the origin with
    /// no size.
    #[test]
    fn the_main_windows_frame_is_remembered_and_saved() {
        let scratch = Scratch::new();
        let (app, wm) = on_one_monitor(&scratch);
        let path = state_path(&scratch);
        window(&app, "window-1");

        wm.window_moved(app.handle(), "window-1");
        wm.window_destroyed("window-1");
        wm.save_frames();
        assert_eq!(window_state::read_main(&path), None, "not the main window");

        window(&app, "main");
        wm.window_moved(app.handle(), "main");
        wm.save_frames();
        let remembered = saved(frame(0.0, 0.0, 0.0, 0.0), false);
        assert_eq!(window_state::read_main(&path), Some(remembered));
    }

    /// Windows reports a maximize's move before the window reads as
    /// maximized. The mock's sizeless window covers a sizeless work area.
    #[test]
    fn a_frame_covering_its_work_area_is_read_as_maximized() {
        let scratch = Scratch::new();
        let (app, wm) = on(vec![monitor((0, 0, 0, 0), 1.0)], &scratch);
        window(&app, "main");
        *wm.main_frame.lock().unwrap() = Some(saved(CENTERED, false));

        wm.window_moved(app.handle(), "main");

        assert_eq!(*wm.main_frame.lock().unwrap(), Some(saved(CENTERED, true)));
    }

    /// A frame that cannot be written is logged, not raised.
    #[test]
    fn a_frame_that_cannot_be_saved_is_logged() {
        let scratch = Scratch::new();
        let (_app, wm) = on_one_monitor(&scratch);
        // A directory where the file should be, so the rename onto it fails.
        std::fs::create_dir_all(state_path(&scratch)).unwrap();
        *wm.main_frame.lock().unwrap() = Some(saved(CENTERED, false));

        let out = captured(|| wm.save_frames());

        assert!(
            out.contains("failed to save a window's frame"),
            "got: {out}"
        );
    }

    /// A manager with no state path keeps its frames for the session alone.
    #[test]
    fn a_manager_with_no_state_path_writes_nothing() {
        let scratch = Scratch::new();
        let wm = WindowManager::new(CancellationToken::new(), OneMonitor(None), None);
        *wm.main_frame.lock().unwrap() = Some(saved(CENTERED, false));

        wm.save_frames();

        assert!(!state_path(&scratch).exists());
        assert_eq!(*wm.main_frame.lock().unwrap(), Some(saved(CENTERED, false)));
    }

    /// A rebuilt main window takes the frame remembered this session over the
    /// file, and a closed one leaves nothing to read.
    #[test]
    fn a_remembered_frame_wins_over_the_file() {
        let scratch = Scratch::new();
        let (app, wm) = on_one_monitor(&scratch);
        save(&scratch, &saved(frame(40.0, 30.0, 1500.0, 900.0), false));
        let remembered = saved(frame(10.0, 20.0, 800.0, 600.0), false);
        *wm.main_frame.lock().unwrap() = Some(remembered);

        assert_eq!(wm.main_window_placement(), Some(remembered));
        wm.window_moved(app.handle(), "main");
        assert_eq!(
            *wm.main_frame.lock().unwrap(),
            Some(remembered),
            "no main window to read"
        );
    }
}
