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

//! Stand-ins for the sidecar: its gRPC services, served by the test process on
//! an endpoint of its own ([`SidecarService::dialing`] reaches it), and the
//! reading half of a hand-written HTTP server.

use std::pin::Pin;
use std::sync::Arc;
use std::task::{Context, Poll};

use interprocess::local_socket::tokio::Stream;
use interprocess::local_socket::traits::tokio::Listener as _;
use tokio::io::{AsyncBufReadExt, AsyncRead, AsyncWrite, BufReader, ReadBuf};
use tokio::sync::watch;
use tonic::transport::server::Connected;
use tonic::transport::Server;
use tonic::{Request, Response, Status};

use super::grpc::auth::auth_service_server::{AuthService, AuthServiceServer};
use super::grpc::auth::{
    AuthState, AuthStateWatchRequest, LogoutRequest, LogoutResponse, StartLoginRequest,
    StartLoginResponse,
};
use super::grpc::monitor::monitor_service_server::{MonitorService, MonitorServiceServer};
use super::grpc::monitor::{AlertEventFeedFrame, SubscribeAlertEventFeedRequest};
use super::grpc::poke::poke_service_server::{PokeService, PokeServiceServer};
use super::grpc::poke::{PokeRequest, PokeResponse};
use super::ipc::{bind_listener, cleanup_path, Endpoint};

/// Reads a request's head off `stream`, through its blank line.
pub async fn read_head(stream: &mut Stream) {
    let mut reader = BufReader::new(stream);
    let mut line = String::new();
    while reader.read_line(&mut line).await.unwrap() > 0 && line != "\r\n" {
        line.clear();
    }
}

/// What the fake answers. With `refuse` set every call fails with it;
/// otherwise each watch sends `watch`, and each alert feed `alerts`, then ends.
#[derive(Clone, Default)]
pub struct FakeGrpc {
    pub refuse: Option<tonic::Code>,
    pub watch: Vec<Result<AuthState, Status>>,
    pub alerts: Vec<Result<AlertEventFeedFrame, Status>>,
    /// Holds each alert feed open after `alerts`, as a live sidecar does.
    pub hold_alerts: bool,
    /// Calls answered, of every kind.
    pub calls: Arc<watch::Sender<usize>>,
}

impl FakeGrpc {
    /// Serves the fake on a fresh endpoint until the returned guard drops.
    pub fn serve(self) -> Served {
        let endpoint = Endpoint::pick(&std::env::temp_dir()).expect("pick an endpoint");
        let listener = bind_listener(endpoint.as_arg());
        let incoming = futures_util::stream::unfold(listener, |listener| async move {
            Some((listener.accept().await.map(Conn), listener))
        });
        let server = Server::builder()
            .add_service(AuthServiceServer::new(self.clone()))
            .add_service(PokeServiceServer::new(self.clone()))
            .add_service(MonitorServiceServer::new(self))
            .serve_with_incoming(incoming);
        // On Tauri's own runtime, so a test's paused clock does not reach it.
        let task = tauri::async_runtime::spawn(server);
        Served { endpoint, task }
    }

    fn answer<T>(&self, reply: T) -> Result<Response<T>, Status> {
        self.calls.send_modify(|calls| *calls += 1);
        match self.refuse {
            Some(code) => Err(Status::new(code, "refused")),
            None => Ok(Response::new(reply)),
        }
    }
}

/// A served fake; dropping it stops the server.
pub struct Served {
    pub endpoint: Endpoint,
    task: tauri::async_runtime::JoinHandle<Result<(), tonic::transport::Error>>,
}

impl Drop for Served {
    fn drop(&mut self) {
        self.task.abort();
        cleanup_path(self.endpoint.as_arg());
    }
}

#[tonic::async_trait]
impl AuthService for FakeGrpc {
    async fn start_login(
        &self,
        _: Request<StartLoginRequest>,
    ) -> Result<Response<StartLoginResponse>, Status> {
        self.answer(StartLoginResponse {})
    }

    async fn logout(&self, _: Request<LogoutRequest>) -> Result<Response<LogoutResponse>, Status> {
        self.answer(LogoutResponse {})
    }

    type AuthStateWatchStream =
        futures_util::stream::Iter<std::vec::IntoIter<Result<AuthState, Status>>>;

    async fn auth_state_watch(
        &self,
        _: Request<AuthStateWatchRequest>,
    ) -> Result<Response<Self::AuthStateWatchStream>, Status> {
        self.answer(futures_util::stream::iter(self.watch.clone()))
    }
}

#[tonic::async_trait]
impl PokeService for FakeGrpc {
    async fn poke(&self, _: Request<PokeRequest>) -> Result<Response<PokeResponse>, Status> {
        self.answer(PokeResponse {})
    }
}

#[tonic::async_trait]
impl MonitorService for FakeGrpc {
    type SubscribeAlertEventFeedStream =
        Pin<Box<dyn futures_util::Stream<Item = Result<AlertEventFeedFrame, Status>> + Send>>;

    async fn subscribe_alert_event_feed(
        &self,
        _: Request<SubscribeAlertEventFeedRequest>,
    ) -> Result<Response<Self::SubscribeAlertEventFeedStream>, Status> {
        use futures_util::StreamExt;
        let frames = futures_util::stream::iter(self.alerts.clone());
        let frames: Self::SubscribeAlertEventFeedStream = if self.hold_alerts {
            Box::pin(frames.chain(futures_util::stream::pending()))
        } else {
            Box::pin(frames)
        };
        self.answer(frames)
    }
}

/// An accepted connection, as tonic's server takes one.
struct Conn(Stream);

impl Connected for Conn {
    type ConnectInfo = ();

    fn connect_info(&self) {}
}

impl AsyncRead for Conn {
    fn poll_read(
        mut self: Pin<&mut Self>,
        cx: &mut Context<'_>,
        buf: &mut ReadBuf<'_>,
    ) -> Poll<std::io::Result<()>> {
        Pin::new(&mut self.0).poll_read(cx, buf)
    }
}

impl AsyncWrite for Conn {
    fn poll_write(
        mut self: Pin<&mut Self>,
        cx: &mut Context<'_>,
        buf: &[u8],
    ) -> Poll<std::io::Result<usize>> {
        Pin::new(&mut self.0).poll_write(cx, buf)
    }

    fn poll_flush(mut self: Pin<&mut Self>, cx: &mut Context<'_>) -> Poll<std::io::Result<()>> {
        Pin::new(&mut self.0).poll_flush(cx)
    }

    fn poll_shutdown(mut self: Pin<&mut Self>, cx: &mut Context<'_>) -> Poll<std::io::Result<()>> {
        Pin::new(&mut self.0).poll_shutdown(cx)
    }
}
