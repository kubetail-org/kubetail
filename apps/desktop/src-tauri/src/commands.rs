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

use std::time::Instant;

use tauri::ipc::Channel;
use tauri::{AppHandle, Emitter, Manager, Runtime, State, Webview, WebviewWindow};

use crate::error::Result;
use crate::host_file::{self, HostFile, HostFilePatch};
use crate::log_windows::{self, Call, Frame, Subscribed, View};
use crate::services::sidecar::GraphqlResponse;
use crate::state::AppState;
use crate::webview_log::{self, WebviewRecord};
use crate::window_manager::{self, on_blocking_pool};

/// Probes the sidecar's IPC listener; the frontend's startup gate. `Err`
/// (frontend `networkError`) once the connect budget elapses.
#[tauri::command]
pub async fn ready(state: State<'_, AppState>) -> Result<()> {
    state.sidecar.ready().await
}

/// Opens a new application window (webview `AppMenu` "New Window" / `Ctrl+N`
/// on Linux/Windows; macOS uses its native menu, `app_menu.rs`), built on the
/// blocking pool.
// coverage:ignore-start the macro's line counts the last argument's parse failure, which an AppHandle never has
#[tauri::command]
// coverage:ignore-stop
pub async fn new_window<R: Runtime>(app: AppHandle<R>) -> Result<()> {
    on_blocking_pool(move || app.state::<AppState>().window_manager.new_window(&app)).await
}

/// Applies a partial update to `host.json` (see `host_file`); adding a setting
/// extends `HostFilePatch`, not the command list. Hands the merged file to
/// every window (`window_manager::host_file_written`), broadcasts it as
/// `host_file::UPDATED_EVENT` and returns it. See
/// docs/adr/2026-08-09-host-json-settings.md.
#[tauri::command]
pub fn update_host_file<R: Runtime>(app: AppHandle<R>, patch: HostFilePatch) -> Result<HostFile> {
    let file = host_file::update(&host_file::location(&app)?, patch)?;
    window_manager::host_file_written(&app, &file);
    app.emit(host_file::UPDATED_EVENT, &file)?;
    Ok(file)
}

/// Reads `host.json` for a window whose page has just loaded, and brings the
/// window up to it (`window_manager::send_host_file`). A write that lands
/// while a window loads reaches neither its new page nor its listener.
#[tauri::command]
pub fn read_host_file<R: Runtime>(window: WebviewWindow<R>) -> Result<HostFile> {
    let file = host_file::read(&host_file::location(window.app_handle())?.path);
    window_manager::send_host_file(&window, &file);
    Ok(file)
}

/// Writes one webview error record to the host's log (`src/lib/error-log.ts`).
/// Thin: the label is read off the `Webview` — never the payload, since the
/// label is the webview's identity — and `webview_log` does the rest.
///
/// `async` so a hot error path runs on the runtime, not the main thread.
#[tauri::command]
pub async fn webview_record_error<R: Runtime>(
    state: State<'_, AppState>,
    webview: Webview<R>,
    record: WebviewRecord,
) -> Result<()> {
    // Each window holds one webview under the same label, so the window is
    // what resolves it.
    let now = Instant::now();
    webview_log::record_from(&state.webview_log, webview.label(), record, now, |label| {
        webview.get_webview_window(label).is_some()
    });
    Ok(())
}

/// Forwards a GraphQL query/mutation to the sidecar over UDS HTTP. Returns
/// body + real HTTP status; only a transport failure is `Err` (frontend
/// `networkError`/retry). Shape matches `src/lib/graphql/invoke-fetch.ts`.
#[tauri::command]
pub async fn graphql_query(state: State<'_, AppState>, body: String) -> Result<GraphqlResponse> {
    state.sidecar.query(body).await
}

/// Registers a GraphQL subscription (one SSE connection to the sidecar).
/// Returns the op id for [`graphql_unsubscribe`]. Shape matches
/// `src/lib/graphql/subscribe-exchange.ts`.
///
/// Tagged with the calling webview's label so the host can tear the
/// subscription down when that page goes away — a reload or a window close
/// runs no JS teardown (see `lib.rs`).
#[tauri::command]
pub async fn graphql_subscribe<R: Runtime>(
    state: State<'_, AppState>,
    webview: Webview<R>,
    query: String,
    variables: serde_json::Value,
    channel: Channel<String>,
) -> Result<u64> {
    state
        .sidecar
        .subscribe(query, variables, channel, webview.label().to_string())
        .await
}

/// Cancels a subscription. Tolerant of unknown ids — the frontend can race
/// teardown with the subscribe resolve (subscribe-exchange.ts).
#[tauri::command]
pub async fn graphql_unsubscribe(state: State<'_, AppState>, id: u64) -> Result<()> {
    state.sidecar.unsubscribe(id).await;
    Ok(())
}

/// An app window subscribes to its log windows on `channel`, and gets the page its
/// other log-window commands name. See `log_windows`.
#[tauri::command]
pub fn log_window_subscribe<R: Runtime>(
    state: State<'_, AppState>,
    webview: Webview<R>,
    channel: Channel<Frame>,
) -> Result<Subscribed> {
    let page = state.log_windows.subscribe(webview.label(), channel)?;
    Ok(Subscribed { page })
}

/// Opens or surfaces the caller's log window on a call. Returns its label
/// once the window exists.
#[tauri::command]
pub async fn log_window_open<R: Runtime>(
    app: AppHandle<R>,
    webview: Webview<R>,
    page: u64,
    cluster_id: String,
    chat_id: String,
    call_id: String,
    grep: Option<String>,
) -> Result<String> {
    let call = Call {
        cluster_id,
        chat_id,
        call_id,
    };
    log_windows::open(app, webview.label(), page, call, grep).await
}

/// Surfaces the caller's live log window on a call.
#[tauri::command]
pub fn log_window_focus<R: Runtime>(
    app: AppHandle<R>,
    state: State<'_, AppState>,
    webview: Webview<R>,
    page: u64,
    call_id: String,
) -> Result<()> {
    let target = state
        .log_windows
        .focus_target(webview.label(), page, &call_id)?;
    if let Some(label) = target {
        state.window_manager.surface_window(&app, &label)?;
    }
    Ok(())
}

/// Removes and destroys the caller's log windows, cancelling any building.
#[tauri::command]
pub fn log_window_close_all<R: Runtime>(
    app: AppHandle<R>,
    webview: Webview<R>,
    page: u64,
) -> Result<()> {
    log_windows::close_all(&app, webview.label(), page)
}

/// What the calling log window draws.
#[tauri::command]
pub fn log_window_view<R: Runtime>(
    state: State<'_, AppState>,
    webview: Webview<R>,
) -> Result<View> {
    state.log_windows.view(webview.label())
}

/// Keeps the calling log window's viewport push and forwards it to its app
/// window. `async`, so a push as often as once a frame stays off the main
/// thread.
#[tauri::command]
pub async fn log_window_viewport<R: Runtime>(
    state: State<'_, AppState>,
    webview: Webview<R>,
    generation: u64,
    grep: Option<String>,
    viewport: serde_json::Value,
) -> Result<()> {
    state
        .log_windows
        .viewport(webview.label(), generation, grep, viewport)
}

/// Sets the calling log window's native title from the parts it sends.
#[tauri::command]
pub fn log_window_title<R: Runtime>(
    state: State<'_, AppState>,
    webview: Webview<R>,
    parts: Vec<String>,
) -> Result<()> {
    let title = state.log_windows.title(webview.label(), &parts)?;
    webview.window().set_title(&title)?;
    Ok(())
}

/// Surfaces the calling log window's app window and asks it to show the chat.
#[tauri::command]
pub fn log_window_show_chat<R: Runtime>(
    app: AppHandle<R>,
    state: State<'_, AppState>,
    webview: Webview<R>,
) -> Result<()> {
    let app_label = state.log_windows.show_chat(webview.label())?;
    state.window_manager.surface_window(&app, &app_label)?;
    Ok(())
}

#[cfg(test)]
mod tests {
    use serde_json::json;
    use tauri::{Listener, Manager};

    use crate::host_file;
    use crate::services::sidecar::SidecarService;
    use crate::state::AppState;
    use crate::testutil::{invoke, mock_app, window, Scratch};
    use crate::webview_log::{Limiter, LimiterConfig};

    #[test]
    fn ready_answers_once_the_sidecar_is_ready() {
        let scratch = Scratch::new();
        let app = mock_app(scratch.path(), SidecarService::without_process(None));
        let main = window(&app, "main");

        assert_eq!(invoke(&main, "ready", json!({})), Ok(json!(null)));
    }

    #[test]
    fn ready_fails_with_the_sidecars_own_reason() {
        let scratch = Scratch::new();
        let sidecar = SidecarService::without_process(Some("database is locked"));
        let app = mock_app(scratch.path(), sidecar);
        let main = window(&app, "main");

        let err = invoke(&main, "ready", json!({})).unwrap_err();
        assert!(
            err.as_str().unwrap().contains("database is locked"),
            "got {err}"
        );
    }

    /// A transport failure is the webview's `networkError`: an `Err` carrying
    /// the reason, never a response.
    #[test]
    fn a_query_with_no_sidecar_fails_as_a_transport_error() {
        let scratch = Scratch::new();
        let app = mock_app(scratch.path(), SidecarService::without_process(None));
        let main = window(&app, "main");

        let err = invoke(&main, "graphql_query", json!({ "body": "{}" })).unwrap_err();
        assert!(
            err.as_str().unwrap().contains("no sidecar is running"),
            "got {err}"
        );
    }

    #[test]
    fn a_call_missing_an_argument_is_refused_by_name() {
        let scratch = Scratch::new();
        let app = mock_app(scratch.path(), SidecarService::without_process(None));
        let main = window(&app, "main");

        let err = invoke(&main, "graphql_query", json!({})).unwrap_err();
        assert!(err.as_str().unwrap().contains("body"), "got {err}");
    }

    /// Every command refuses an argument it cannot read, naming it.
    #[test]
    fn a_malformed_argument_is_refused_by_name() {
        let scratch = Scratch::new();
        let app = mock_app(scratch.path(), SidecarService::without_process(None));
        let main = window(&app, "main");

        for (cmd, args, name) in [
            ("webview_record_error", json!({ "record": 1 }), "record"),
            (
                "graphql_subscribe",
                json!({ "query": "", "variables": {}, "channel": 1 }),
                "channel",
            ),
            ("graphql_unsubscribe", json!({ "id": "x" }), "id"),
            (
                "log_window_open",
                json!({ "page": 1, "clusterId": "c", "chatId": "c", "callId": "c", "grep": 1 }),
                "grep",
            ),
        ] {
            let err = invoke(&main, cmd, args).unwrap_err();
            assert!(err.as_str().unwrap().contains(name), "{cmd}: got {err}");
        }
    }

    /// The commands read their state from the app; before `setup` manages it
    /// there is none to read.
    #[test]
    fn a_command_before_the_state_is_managed_is_refused() {
        let scratch = Scratch::new();
        let app = crate::testutil::bare_app(scratch.path());
        let main = window(&app, "main");

        let err = invoke(&main, "ready", json!({})).unwrap_err();
        assert!(err.as_str().unwrap().contains("state"), "got {err}");
    }

    /// A subscription answers with its id at once; a failed dial reaches the
    /// channel, not the call. Unsubscribing tolerates any id.
    #[test]
    fn subscribe_returns_fresh_ids_and_unsubscribe_tolerates_any() {
        let scratch = Scratch::new();
        let app = mock_app(scratch.path(), SidecarService::without_process(None));
        let main = window(&app, "main");
        let subscribe = || {
            let args = json!({
                "query": "subscription { x }",
                "variables": {},
                "channel": "__CHANNEL__:7",
            });
            invoke(&main, "graphql_subscribe", args).unwrap()
        };

        let first = subscribe();
        let second = subscribe();
        assert_ne!(first, second);

        for id in [first, json!(999)] {
            assert_eq!(
                invoke(&main, "graphql_unsubscribe", json!({ "id": id })),
                Ok(json!(null))
            );
        }
    }

    #[test]
    fn new_window_opens_nothing_once_a_quit_has_begun() {
        let scratch = Scratch::new();
        let app = mock_app(scratch.path(), SidecarService::without_process(None));
        let main = window(&app, "main");
        app.state::<AppState>().shutdown.cancel();

        assert_eq!(invoke(&main, "new_window", json!({})), Ok(json!(null)));
        assert_eq!(app.webview_windows().len(), 1);
    }

    #[test]
    fn update_host_file_writes_the_merged_file_and_broadcasts_it() {
        let scratch = Scratch::new();
        let app = mock_app(scratch.path(), SidecarService::without_process(None));
        let main = window(&app, "main");
        let (tx, rx) = std::sync::mpsc::channel();
        app.listen_any(host_file::UPDATED_EVENT, move |event| {
            tx.send(event.payload().to_string()).unwrap();
        });

        let patch = json!({ "patch": { "colorSchemePreference": "dark" } });
        let file = invoke(&main, "update_host_file", patch).unwrap();

        assert_eq!(file["colorSchemePreference"], "dark");
        let path = host_file::location(app.handle()).unwrap().path;
        assert!(path.starts_with(scratch.path()), "{}", path.display());
        assert_eq!(serde_json::to_value(host_file::read(&path)).unwrap(), file);
        // Emitted before the command returned.
        let broadcast: serde_json::Value = serde_json::from_str(&rx.try_recv().unwrap()).unwrap();
        assert_eq!(broadcast, file);
    }

    /// A window that missed a write's broadcast reads the written file.
    #[test]
    fn read_host_file_returns_a_write_the_window_never_heard() {
        let scratch = Scratch::new();
        let app = mock_app(scratch.path(), SidecarService::without_process(None));
        let main = window(&app, "main");
        let patch = host_file::HostFilePatch {
            color_scheme_preference: Some(host_file::ColorSchemePreference::Dark),
        };
        let written =
            host_file::update(&host_file::location(app.handle()).unwrap(), patch).unwrap();

        let file = invoke(&main, "read_host_file", json!({})).unwrap();

        assert_eq!(file, serde_json::to_value(written).unwrap());
    }

    /// The label records are paced under is the calling webview's.
    #[test]
    fn webview_record_error_paces_records_under_the_calling_webviews_label() {
        let scratch = Scratch::new();
        let app = mock_app(scratch.path(), SidecarService::without_process(None));
        let state = app.state::<AppState>();
        *state.webview_log.lock().unwrap() = Limiter::new(LimiterConfig {
            burst: 1,
            error_reserve: 0,
            ..LimiterConfig::DEFAULT
        });
        let other = window(&app, "window-1");

        for message in ["one", "two"] {
            let args = json!({ "record": { "source": "graphql", "message": message } });
            assert_eq!(
                invoke(&other, "webview_record_error", args),
                Ok(json!(null))
            );
        }

        let withheld = state.webview_log.lock().unwrap().evict("window-1");
        assert_eq!(withheld, 1, "the second record is withheld");
    }

    /// `main` subscribed, with its page.
    fn subscribing(main: &tauri::WebviewWindow<tauri::test::MockRuntime>) -> u64 {
        let args = json!({ "channel": "__CHANNEL__:1" });
        let subscribed = invoke(main, "log_window_subscribe", args).unwrap();
        subscribed["page"].as_u64().unwrap()
    }

    fn open_args(page: u64, call_id: &str) -> serde_json::Value {
        json!({ "page": page, "clusterId": "c", "chatId": "chat", "callId": call_id })
    }

    /// An open builds the call's window and returns its label once it exists;
    /// a second open on that call surfaces it, and another call builds.
    #[test]
    fn log_window_open_builds_once_per_call() {
        let scratch = Scratch::new();
        let app = mock_app(scratch.path(), SidecarService::without_process(None));
        let main = window(&app, "main");
        let page = subscribing(&main);

        let first = invoke(&main, "log_window_open", open_args(page, "a")).unwrap();
        let again = invoke(&main, "log_window_open", open_args(page, "a")).unwrap();
        let other = invoke(&main, "log_window_open", open_args(page, "b")).unwrap();

        assert_eq!(first, again);
        assert_ne!(first, other);
        let label = first.as_str().unwrap();
        assert!(label.starts_with("logs-"), "{label}");
        assert!(app.get_webview_window(label).is_some());
        assert_eq!(app.webview_windows().len(), 3);
    }

    /// An open once a quit has begun builds nothing and is cancelled; the call
    /// can be opened again.
    #[test]
    fn log_window_open_while_quitting_is_cancelled() {
        let scratch = Scratch::new();
        let app = mock_app(scratch.path(), SidecarService::without_process(None));
        let main = window(&app, "main");
        let page = subscribing(&main);
        app.state::<AppState>().shutdown.cancel();

        let err = invoke(&main, "log_window_open", open_args(page, "a")).unwrap_err();

        assert_eq!(err, json!("cancelled"));
        assert_eq!(app.webview_windows().len(), 1);
    }

    /// A failed build returns its error and leaves no entry, so the next open
    /// on the call builds again.
    #[test]
    fn log_window_open_returns_a_failed_build() {
        let scratch = Scratch::new();
        let app = mock_app(scratch.path(), SidecarService::without_process(None));
        let main = window(&app, "main");
        let page = subscribing(&main);
        // Holds the label the open mints, so its build fails.
        window(&app, "logs-1");

        let err = invoke(&main, "log_window_open", open_args(page, "a")).unwrap_err();
        let again = invoke(&main, "log_window_open", open_args(page, "a"));

        assert!(err.as_str().unwrap().contains("logs-1"), "got {err}");
        assert_eq!(again, Ok(json!("logs-2")));
    }

    /// An open whose live window is gone before it is surfaced is cancelled.
    #[test]
    fn log_window_open_of_a_vanished_window_is_cancelled() {
        let scratch = Scratch::new();
        let app = mock_app(scratch.path(), SidecarService::without_process(None));
        let main = window(&app, "main");
        let page = subscribing(&main);
        // Live in the pair, with no window behind it.
        let log_windows = &app.state::<AppState>().log_windows;
        crate::log_windows::live_for_test(log_windows, page, "logs-9");

        let err = invoke(&main, "log_window_open", open_args(page, "a")).unwrap_err();

        assert_eq!(err, json!("cancelled"));
    }

    /// The mock runtime keeps a destroyed window listed until its event loop
    /// runs, so this reads the pair: the closed window's entry is gone.
    #[test]
    fn log_window_close_all_removes_the_callers_log_windows() {
        let scratch = Scratch::new();
        let app = mock_app(scratch.path(), SidecarService::without_process(None));
        let main = window(&app, "main");
        let page = subscribing(&main);
        let label = invoke(&main, "log_window_open", open_args(page, "a")).unwrap();
        let log_window = app.get_webview_window(label.as_str().unwrap()).unwrap();
        let focus = json!({ "page": page, "callId": "a" });
        assert_eq!(invoke(&main, "log_window_focus", focus), Ok(json!(null)));
        let none = json!({ "page": page, "callId": "z" });
        assert_eq!(invoke(&main, "log_window_focus", none), Ok(json!(null)));

        let closed = invoke(&main, "log_window_close_all", json!({ "page": page }));

        assert_eq!(closed, Ok(json!(null)));
        assert!(invoke(&log_window, "log_window_view", json!({})).is_err());
    }

    /// A log window reads its view, pushes, titles itself and shows its chat
    /// through its own label. The mock runtime keeps no title, so `title`'s
    /// tests pin what is set.
    #[test]
    fn a_log_window_drives_its_own_entry() {
        let scratch = Scratch::new();
        let app = mock_app(scratch.path(), SidecarService::without_process(None));
        let main = window(&app, "main");
        let page = subscribing(&main);
        let label = invoke(&main, "log_window_open", open_args(page, "a")).unwrap();
        let log_window = app.get_webview_window(label.as_str().unwrap()).unwrap();

        let view = invoke(&log_window, "log_window_view", json!({})).unwrap();
        assert_eq!(view["callId"], "a");
        let push = json!({ "generation": view["generation"], "grep": "x", "viewport": {} });
        assert_eq!(
            invoke(&log_window, "log_window_viewport", push),
            Ok(json!(null))
        );
        let missing = json!({ "generation": view["generation"], "grep": "x" });
        assert!(invoke(&log_window, "log_window_viewport", missing).is_err());
        let parts = json!({ "parts": ["chat", "pod"] });
        assert_eq!(
            invoke(&log_window, "log_window_title", parts),
            Ok(json!(null))
        );
        let shown = invoke(&log_window, "log_window_show_chat", json!({}));
        assert_eq!(shown, Ok(json!(null)));
    }

    /// Each command checks its caller's kind, beside the capability.
    #[test]
    fn a_log_window_command_is_refused_to_the_wrong_kind_of_window() {
        let scratch = Scratch::new();
        let app = mock_app(scratch.path(), SidecarService::without_process(None));
        let main = window(&app, "main");
        let logs = window(&app, "logs-7");

        let args = json!({ "channel": "__CHANNEL__:1" });
        assert!(invoke(&logs, "log_window_subscribe", args).is_err());
        assert!(invoke(&logs, "log_window_open", open_args(1, "a")).is_err());
        assert!(invoke(&main, "log_window_view", json!({})).is_err());
        assert!(invoke(&main, "log_window_show_chat", json!({})).is_err());
        let parts = json!({ "parts": ["x"] });
        assert!(invoke(&main, "log_window_title", parts).is_err());
    }
}
