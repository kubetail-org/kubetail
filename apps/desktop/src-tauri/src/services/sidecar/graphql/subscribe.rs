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

//! Per-subscription SSE reader: one HTTP/1 connection per subscription (like
//! `query.rs`, but `Accept: text/event-stream`), translating gqlgen's SSE
//! frames into the webview channel envelope (`subscribe-exchange.ts`):
//! ```text
//!   open     → {"type":"open"}    once, on 200, before any `next`
//!   next     → {"type":"next","payload":<graphql.Response>}
//!   complete → {"type":"complete"}   graceful end (clean end-of-body)
//!   closed   → {"type":"closed"}     abnormal end (mid-stream read failure)
//!   error    → {"type":"error","payload":<message>}  (transport-level only)
//! ```
//! Two load-bearing rules:
//! - **`complete` vs `closed` is read off body framing**, not the `complete`
//!   SSE event: gqlgen's `event: complete` is data-less and SSE dispatch
//!   discards it, so graceful = clean chunked terminator, abnormal = truncated
//!   body (read error). Both reconnect; only `closed` is reported.
//! - **`open` fires only after a successful dial (200)** — never on the ack or
//!   a failed dial (that emits `error`); the frontend keys its accumulator
//!   resets on it. See docs/adr/2026-08-09-transport-status-generation.md.
//!
//! No shared session or demultiplexing — a drop ends one subscription and the
//! frontend reconnects. The only state is the cancel-handle table for
//! `unsubscribe`.
//!
//! Every handle is tagged with the webview that opened it. A reload wipes the
//! page's JS without any teardown reaching us, so the host cancels that
//! webview's subscriptions itself (`lib.rs`) — otherwise each one keeps
//! streaming into a callback id the new page has never heard of, which the
//! Tauri JS runtime reports as "Couldn't find callback id N", once per frame,
//! for as long as the app runs.

use std::collections::HashMap;
use std::sync::atomic::{AtomicU64, Ordering};
use std::sync::{Arc, Mutex};
use std::time::Duration;

use eventsource_stream::Eventsource;
use futures_util::StreamExt;
use http_body_util::{BodyExt, Full};
use hyper::body::{Bytes, Incoming};
use hyper::{Request, Response};
use hyper_util::rt::TokioIo;
use tokio::sync::oneshot;

use super::super::ipc::{Target, DEFAULT_CONNECT_BUDGET};
use crate::error::{AppError, Result};

/// Envelope delivery seam. Production wraps a Tauri `Channel<String>`; tests
/// use an mpsc adapter.
pub trait FrameSink: Send + Sync {
    /// Best-effort: a closed sink drops the message — the consumer is gone.
    fn send_frame(&self, frame: String);
}

/// Graceful end (clean end-of-body): the frontend reconnects silently.
const COMPLETE_FRAME: &str = r#"{"type":"complete"}"#;

/// Abnormal end (truncated body — crash, sleep, network loss): the frontend
/// reconnects and reports the drop.
const CLOSED_FRAME: &str = r#"{"type":"closed"}"#;

/// Sent once, before any `next`, only after a successful dial (200) — never on
/// a failed dial. The frontend resets accumulators on it; see module docs.
const OPEN_FRAME: &str = r#"{"type":"open"}"#;

/// A live subscription: the webview that opened it, so
/// [`SubscriptionClient::cancel_webview`] can find it, and the cancel sender —
/// never read, only dropped, which is what ends the task.
struct Sub {
    webview: String,
    _cancel: oneshot::Sender<()>,
}

type Subs = Arc<Mutex<HashMap<u64, Sub>>>;

/// Locks `subs`, recovering a poisoned lock — the guarded map is a plain table
/// of handles, so a panicking holder leaves nothing half-updated.
fn lock(subs: &Subs) -> std::sync::MutexGuard<'_, HashMap<u64, Sub>> {
    subs.lock().unwrap_or_else(|p| p.into_inner())
}

/// Opens one SSE connection per subscription and tracks a cancel handle for
/// each so [`SubscriptionClient::unsubscribe`] can tear it down.
pub struct SubscriptionClient {
    target: Target,
    /// Live subscriptions by host-side op id; dropping an entry's sender ends
    /// its task.
    subs: Subs,
    /// Monotonic for the process lifetime — distinct ids across reconnects.
    next_id: AtomicU64,
    /// Dial budget before emitting an `error` frame; tests shrink it.
    connect_budget: Duration,
}

impl SubscriptionClient {
    pub fn new(target: Target) -> Self {
        Self::new_with_budget(target, DEFAULT_CONNECT_BUDGET)
    }

    /// [`SubscriptionClient::new`] with a caller-supplied connect budget
    /// (tests pass a short one).
    fn new_with_budget(target: Target, connect_budget: Duration) -> Self {
        Self {
            target,
            subs: Arc::new(Mutex::new(HashMap::new())),
            next_id: AtomicU64::new(1),
            connect_budget,
        }
    }

    /// Registers a subscription and returns its op id. `Ok` as soon as the
    /// streaming task is spawned — connect/stream failures surface on the sink
    /// (`error`/`closed`), never synchronously.
    pub async fn subscribe(
        &self,
        query: String,
        variables: serde_json::Value,
        sink: Arc<dyn FrameSink>,
        webview: String,
    ) -> Result<u64> {
        let id = self.next_id.fetch_add(1, Ordering::Relaxed);
        let body = serde_json::json!({ "query": query, "variables": variables }).to_string();

        let (cancel_tx, cancel_rx) = oneshot::channel();
        // Register before spawning — the frontend only learns `id` after this
        // returns, so `unsubscribe(id)` can't race ahead of the insert.
        lock(&self.subs).insert(
            id,
            Sub {
                webview,
                _cancel: cancel_tx,
            },
        );

        let target = self.target.clone();
        let budget = self.connect_budget;
        let subs = Arc::clone(&self.subs);
        tokio::spawn(run_subscription(
            target, budget, body, sink, cancel_rx, subs, id,
        ));
        Ok(id)
    }

    /// Cancels a subscription, dropping its connection. Tolerant of unknown
    /// ids — urql can race teardown with the subscribe resolve.
    pub async fn unsubscribe(&self, id: u64) {
        // Removing drops the cancel sender → the task's cancel future fires.
        let _ = lock(&self.subs).remove(&id);
    }

    /// Cancels every subscription `webview` opened — its page reloaded or its
    /// window closed, so nothing is left to receive them.
    pub fn cancel_webview(&self, webview: &str) {
        lock(&self.subs).retain(|_, sub| sub.webview != webview);
    }
}

/// Drives one subscription end-to-end. Always removes `id` from `subs` on the
/// way out, so the table never leaks completed subscriptions.
async fn run_subscription(
    target: Target,
    budget: Duration,
    body: String,
    sink: Arc<dyn FrameSink>,
    mut cancel_rx: oneshot::Receiver<()>,
    subs: Subs,
    id: u64,
) {
    // Honor an unsubscribe that lands while we're still dialing.
    let opened = tokio::select! {
        biased;
        _ = &mut cancel_rx => { lock(&subs).remove(&id); return; }
        r = open_stream(&target, budget, body) => r,
    };

    match opened {
        Ok(resp) => {
            // `open` before the snapshot, and only on a successful dial — a
            // failed dial takes the `Err` arm and emits no `open`.
            sink.send_frame(OPEN_FRAME.to_string());
            stream_to_sink(resp, &sink, cancel_rx).await
        }
        Err(err) => {
            // Connect / handshake / non-200: the frontend treats it like a
            // drop and reconnects.
            sink.send_frame(error_frame(&err.to_string()));
        }
    }

    lock(&subs).remove(&id);
}

/// Dials the sidecar and issues the SSE `POST /graphql`, returning the live
/// streaming response.
async fn open_stream(
    target: &Target,
    budget: Duration,
    body: String,
) -> Result<Response<Incoming>> {
    let stream = target.connect_with_budget(budget).await?;
    let io = TokioIo::new(stream);
    let (mut sender, conn) = hyper::client::conn::http1::handshake(io)
        .await
        .map_err(|e| AppError::Io(std::io::Error::other(e)))?;
    // Drive the connection in the background; it ends when the body drops.
    // Its error also fails the request or the body, which report it.
    tokio::spawn(conn);

    let req = Request::builder()
        .method("POST")
        .uri("/graphql")
        // hyper requires a Host header on HTTP/1.1 even over UDS.
        .header("host", "sidecar.local")
        .header("content-type", "application/json")
        // Selects gqlgen's SSE transport over POST.
        .header("accept", "text/event-stream")
        .body(Full::new(Bytes::from(body)))
        .map_err(|e| AppError::Io(std::io::Error::other(format!("build request: {e}"))))?;

    let resp = sender
        .send_request(req)
        .await
        .map_err(|e| AppError::Io(std::io::Error::other(e)))?;
    if resp.status() != hyper::StatusCode::OK {
        return Err(AppError::Io(std::io::Error::other(format!(
            "sidecar returned {} for subscription",
            resp.status()
        ))));
    }
    Ok(resp)
}

/// Reads the SSE body, forwarding decoded events to `sink`, until the server
/// completes, the stream ends, or `cancel_rx` fires (consumer unsubscribed).
async fn stream_to_sink(
    resp: Response<Incoming>,
    sink: &Arc<dyn FrameSink>,
    mut cancel_rx: oneshot::Receiver<()>,
) {
    // `eventsource-stream` owns the SSE wire parsing (line framing, comments,
    // chunk-boundary splits).
    let mut events = resp.into_body().into_data_stream().eventsource();
    loop {
        tokio::select! {
            biased;
            // Consumer torn down: drop the stream (cancels the sidecar
            // resolver), emit nothing.
            _ = &mut cancel_rx => return,
            next = events.next() => {
                match next {
                    Some(Ok(event)) => {
                        if event.event == "complete" {
                            // Only reachable if a `complete` carries data;
                            // gqlgen's is data-less and lands at the EOF arm.
                            sink.send_frame(COMPLETE_FRAME.to_string());
                            return;
                        } else if !event.data.is_empty() {
                            // Non-empty data = a `graphql.Response`.
                            sink.send_frame(next_frame(&event.data));
                        }
                    }
                    // Clean end-of-body: graceful completion (where gqlgen's
                    // data-less `event: complete` lands).
                    None => {
                        sink.send_frame(COMPLETE_FRAME.to_string());
                        return;
                    }
                    // Truncated body (crash, sleep, network loss): `closed` so
                    // the frontend reconnects and reports.
                    Some(Err(_)) => {
                        sink.send_frame(CLOSED_FRAME.to_string());
                        return;
                    }
                }
            }
        }
    }
}

/// Wraps a `graphql.Response` (already valid JSON) in the `next` envelope —
/// no parse round-trip.
fn next_frame(data: &str) -> String {
    format!(r#"{{"type":"next","payload":{data}}}"#)
}

/// Builds the `error` envelope, escaping `message` via serde.
fn error_frame(message: &str) -> String {
    let payload = serde_json::to_string(message).unwrap_or_else(|_| "\"\"".to_string());
    format!(r#"{{"type":"error","payload":{payload}}}"#)
}

#[cfg(test)]
mod tests {
    use super::super::super::ipc::Endpoint;
    use super::super::super::testutil::read_head;
    use super::*;
    use interprocess::local_socket::{
        traits::tokio::Listener as _, GenericFilePath, ListenerOptions, ToFsName,
    };
    use tokio::io::AsyncWriteExt;
    use tokio::sync::mpsc;

    /// Targets `path`, expecting the server the test process runs itself.
    fn ours(path: Endpoint) -> Target {
        Target::expecting(path, Some(std::process::id()))
    }

    /// Test sink that forwards into an mpsc — the easy way to assert what the
    /// reader actually delivers without spinning up a Tauri runtime.
    struct MpscSink(mpsc::UnboundedSender<String>);
    impl FrameSink for MpscSink {
        fn send_frame(&self, frame: String) {
            let _ = self.0.send(frame);
        }
    }

    /// One-shot HTTP/1 server on `path`: reads the request headers, writes a
    /// `200 text/event-stream` head + `events` as one chunk of a chunked body
    /// (matching gqlgen, whose chunked framing is what lets the client tell the
    /// endings apart), then runs `after` (e.g. return, or wait to hold the stream
    /// open). `clean_end` picks the ending: `true` writes the chunked terminator
    /// (graceful completion), `false` just closes (truncated body — abnormal
    /// drop). Uses `interprocess` for both Unix UDS and Windows named-pipe tests.
    async fn spawn_sse_server<Fut>(
        path: String,
        events: &'static str,
        clean_end: bool,
        after: impl FnOnce() -> Fut + Send + 'static,
    ) where
        Fut: std::future::Future<Output = ()> + Send,
    {
        let name = path
            .as_str()
            .to_fs_name::<GenericFilePath>()
            .expect("to_fs_name");
        let listener = ListenerOptions::new()
            .name(name)
            .create_tokio()
            .expect("bind listener");
        tokio::spawn(async move {
            let mut stream = listener.accept().await.unwrap();
            read_head(&mut stream).await;
            let head = "HTTP/1.1 200 OK\r\nContent-Type: text/event-stream\r\n\
                        Cache-Control: no-cache\r\nTransfer-Encoding: chunked\r\n\r\n";
            let _ = stream.write_all(head.as_bytes()).await;
            let chunk = format!("{:x}\r\n{events}\r\n", events.len());
            let _ = stream.write_all(chunk.as_bytes()).await;
            let _ = stream.flush().await;
            after().await;
            if clean_end {
                let _ = stream.write_all(b"0\r\n\r\n").await;
                let _ = stream.flush().await;
            }
            let _ = stream.shutdown().await;
        });
        // The listener is bound+listening synchronously above (before the spawn), so a
        // client connect queues in the listen backlog even before accept() is polled —
        // no wait-for-server sleep is needed.
    }

    fn sink_pair() -> (Arc<dyn FrameSink>, mpsc::UnboundedReceiver<String>) {
        let (tx, rx) = mpsc::unbounded_channel();
        (Arc::new(MpscSink(tx)), rx)
    }

    async fn recv(rx: &mut mpsc::UnboundedReceiver<String>) -> String {
        tokio::time::timeout(Duration::from_secs(2), rx.recv())
            .await
            .expect("frame within 2s")
            .expect("sink open")
    }

    /// Happy path: two `next` events then `complete` arrive as the matching
    /// envelopes, with the raw `data` JSON passed through as `payload`.
    #[tokio::test]
    async fn forwards_next_then_complete() {
        let path = Endpoint::pick(&std::env::temp_dir()).expect("pick");
        let arg = path.as_arg().to_owned();
        spawn_sse_server(
            arg.clone(),
            "event: next\ndata: {\"data\":{\"tick\":1}}\n\n\
             event: next\ndata: {\"data\":{\"tick\":2}}\n\n\
             event: complete\n\n",
            true, // graceful: gqlgen's data-less `complete` + chunked terminator
            || async {},
        )
        .await;

        let client = SubscriptionClient::new(ours(path));
        let (sink, mut rx) = sink_pair();
        let _id = client
            .subscribe(
                "subscription { tick }".to_string(),
                serde_json::json!({}),
                sink,
                "main".to_string(),
            )
            .await
            .expect("subscribe");

        assert_eq!(recv(&mut rx).await, OPEN_FRAME);
        assert_eq!(
            recv(&mut rx).await,
            r#"{"type":"next","payload":{"data":{"tick":1}}}"#
        );
        assert_eq!(
            recv(&mut rx).await,
            r#"{"type":"next","payload":{"data":{"tick":2}}}"#
        );
        assert_eq!(recv(&mut rx).await, COMPLETE_FRAME);
        let _ = std::fs::remove_file(arg);
    }

    /// A connection that dies mid-stream (sidecar crash, sleep, network loss —
    /// modelled as a chunked body truncated without its terminator) yields
    /// `closed` — distinct from the graceful `complete` — so the frontend
    /// reconnects *and* reports it.
    #[tokio::test]
    async fn truncated_stream_synthesizes_closed() {
        let path = Endpoint::pick(&std::env::temp_dir()).expect("pick");
        let arg = path.as_arg().to_owned();
        spawn_sse_server(
            arg.clone(),
            "event: next\ndata: {\"data\":{\"tick\":1}}\n\n",
            false, // abrupt: close without the chunked terminator
            || async {},
        )
        .await;

        let client = SubscriptionClient::new(ours(path));
        let (sink, mut rx) = sink_pair();
        client
            .subscribe(
                "subscription { tick }".to_string(),
                serde_json::json!({}),
                sink,
                "main".to_string(),
            )
            .await
            .expect("subscribe");

        assert_eq!(recv(&mut rx).await, OPEN_FRAME);
        assert_eq!(
            recv(&mut rx).await,
            r#"{"type":"next","payload":{"data":{"tick":1}}}"#
        );
        assert_eq!(recv(&mut rx).await, CLOSED_FRAME);
        let _ = std::fs::remove_file(arg);
    }

    /// The leading `:` comment and `: ping` keep-alives are dropped; only the
    /// real events reach the sink.
    #[tokio::test]
    async fn ignores_comments_and_keepalives() {
        let path = Endpoint::pick(&std::env::temp_dir()).expect("pick");
        let arg = path.as_arg().to_owned();
        spawn_sse_server(
            arg.clone(),
            ":\n\n: ping\n\nevent: next\ndata: {\"data\":1}\n\nevent: complete\n\n",
            true,
            || async {},
        )
        .await;

        let client = SubscriptionClient::new(ours(path));
        let (sink, mut rx) = sink_pair();
        client
            .subscribe(
                "subscription { tick }".to_string(),
                serde_json::json!({}),
                sink,
                "main".to_string(),
            )
            .await
            .expect("subscribe");

        assert_eq!(recv(&mut rx).await, OPEN_FRAME);
        assert_eq!(
            recv(&mut rx).await,
            r#"{"type":"next","payload":{"data":1}}"#
        );
        assert_eq!(recv(&mut rx).await, COMPLETE_FRAME);
        let _ = std::fs::remove_file(arg);
    }

    /// Unsubscribe drops the connection without delivering a `complete` — the
    /// consumer is gone, so nothing more should land on the sink.
    #[tokio::test]
    async fn unsubscribe_stops_delivery() {
        let path = Endpoint::pick(&std::env::temp_dir()).expect("pick");
        let arg = path.as_arg().to_owned();
        let (release_tx, release_rx) = oneshot::channel::<()>();
        spawn_sse_server(
            arg.clone(),
            "event: next\ndata: {\"data\":1}\n\n",
            false,
            // Hold the stream open until released so the client must actively
            // tear it down rather than seeing a natural EOF.
            || async move {
                let _ = release_rx.await;
            },
        )
        .await;

        let client = SubscriptionClient::new(ours(path));
        let (sink, mut rx) = sink_pair();
        let id = client
            .subscribe(
                "subscription { tick }".to_string(),
                serde_json::json!({}),
                sink,
                "main".to_string(),
            )
            .await
            .expect("subscribe");

        assert_eq!(recv(&mut rx).await, OPEN_FRAME);
        assert_eq!(
            recv(&mut rx).await,
            r#"{"type":"next","payload":{"data":1}}"#
        );
        client.unsubscribe(id).await;

        // No `complete` (or any other frame) should arrive — the consumer
        // unsubscribed. The sink simply closes when the task tears down, which
        // surfaces as a timeout (nothing pending) or a clean channel close.
        let after = tokio::time::timeout(Duration::from_millis(300), rx.recv()).await;
        assert!(
            !matches!(after, Ok(Some(_))),
            "unexpected frame after unsubscribe: {after:?}"
        );

        // The entry is gone from the table.
        assert!(lock(&client.subs).is_empty());
        let _ = release_tx.send(());
        let _ = std::fs::remove_file(arg);
    }

    /// A reloaded (or closed) webview takes its own subscriptions down and
    /// leaves every other webview's alone — the host has no other way to learn
    /// the page is gone.
    #[tokio::test]
    async fn cancel_webview_drops_only_that_webviews_subscriptions() {
        // Never dialed: `subscribe` returns as soon as the task is spawned, and
        // the table is populated before that, so a dead socket exercises the
        // bookkeeping without a server.
        let path = Endpoint::pick(&std::env::temp_dir()).expect("pick");
        let client = SubscriptionClient::new_with_budget(ours(path), Duration::from_millis(150));

        let mut ids = Vec::new();
        for webview in ["main", "main", "logs"] {
            let (sink, _rx) = sink_pair();
            ids.push(
                client
                    .subscribe(
                        "subscription { tick }".to_string(),
                        serde_json::json!({}),
                        sink,
                        webview.to_string(),
                    )
                    .await
                    .expect("subscribe"),
            );
        }

        client.cancel_webview("main");

        let remaining = lock(&client.subs);
        assert_eq!(remaining.len(), 1);
        assert!(remaining.contains_key(&ids[2]));
    }

    /// A dead socket surfaces as an `error` frame (not a hang) within the
    /// connect budget — the frontend's reconnect path. The `error` is the
    /// *first* frame: no `open` precedes it, so the frontend won't reset
    /// accumulators on a failed dial.
    #[tokio::test]
    async fn connect_failure_emits_error_frame() {
        let path = Endpoint::pick(&std::env::temp_dir()).expect("pick");
        let client = SubscriptionClient::new_with_budget(ours(path), Duration::from_millis(150));
        let (sink, mut rx) = sink_pair();
        client
            .subscribe(
                "subscription { tick }".to_string(),
                serde_json::json!({}),
                sink,
                "main".to_string(),
            )
            .await
            .expect("subscribe returns Ok even when the socket is dead");

        let frame = recv(&mut rx).await;
        assert!(frame.starts_with(r#"{"type":"error"#), "got {frame}");
    }

    /// A `complete` that carries data ends the stream there; nothing after it
    /// is forwarded.
    #[tokio::test]
    async fn a_complete_with_data_ends_the_stream() {
        let path = Endpoint::pick(&std::env::temp_dir()).expect("pick");
        let arg = path.as_arg().to_owned();
        spawn_sse_server(
            arg.clone(),
            "event: complete\ndata: {}\n\nevent: next\ndata: {\"data\":1}\n\n",
            true,
            || async {},
        )
        .await;

        let client = SubscriptionClient::new(ours(path));
        let (sink, mut rx) = sink_pair();
        client
            .subscribe(
                "subscription { tick }".to_string(),
                serde_json::json!({}),
                sink,
                "main".to_string(),
            )
            .await
            .expect("subscribe");

        assert_eq!(recv(&mut rx).await, OPEN_FRAME);
        assert_eq!(recv(&mut rx).await, COMPLETE_FRAME);
        assert_eq!(rx.recv().await, None, "the sink closes with nothing more");
        let _ = std::fs::remove_file(arg);
    }

    /// A sidecar that answers anything but 200 opens nothing: the first frame
    /// is the error.
    #[tokio::test]
    async fn a_non_200_answer_is_an_error_frame() {
        let path = Endpoint::pick(&std::env::temp_dir()).expect("pick");
        let arg = path.as_arg().to_owned();
        let name = arg.as_str().to_fs_name::<GenericFilePath>().unwrap();
        let listener = ListenerOptions::new().name(name).create_tokio().unwrap();
        tokio::spawn(async move {
            let mut stream = listener.accept().await.unwrap();
            read_head(&mut stream).await;
            let response = "HTTP/1.1 500 Internal Server Error\r\nContent-Length: 0\r\n\r\n";
            stream.write_all(response.as_bytes()).await.unwrap();
        });

        let client = SubscriptionClient::new(ours(path));
        let (sink, mut rx) = sink_pair();
        client
            .subscribe(
                "subscription { tick }".to_string(),
                serde_json::json!({}),
                sink,
                "main".to_string(),
            )
            .await
            .expect("subscribe");

        let frame = recv(&mut rx).await;
        assert!(frame.contains("500"), "got {frame}");
        let _ = std::fs::remove_file(arg);
    }
}
