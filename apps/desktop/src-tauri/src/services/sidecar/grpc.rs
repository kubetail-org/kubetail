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

//! gRPC client for the sidecar's host-internal control channel (auth-state
//! watch + login/logout, resync poke, the alert feed). Shares the one socket
//! with GraphQL via h2c — tonic dials [`ipc::Stream`] with HTTP/2 prior
//! knowledge (no TLS/ALPN; the socket is user-restricted) through a
//! `tower::service_fn` connector that reuses [`ipc::connect`]'s retry. One
//! cached [`Channel`], re-dialed on loss.
//! See docs/adr/2026-10-26-single-socket-h2c-from-the-standard-library.md.

use hyper_util::rt::TokioIo;
use tokio::sync::Mutex;
use tonic::transport::{Channel, Endpoint as TonicEndpoint, Uri};
use tower::service_fn;

use super::ipc::Target;
use crate::error::{AppError, Result};

// Generated bindings for proto/auth.proto (see build.rs); module name matches
// the proto `package`.
pub mod auth {
    tonic::include_proto!("auth");
}

// Generated bindings for proto/poke.proto.
pub mod poke {
    tonic::include_proto!("poke");
}

// Generated bindings for proto/monitor.proto.
pub mod monitor {
    tonic::include_proto!("monitor");
}

use auth::auth_service_client::AuthServiceClient;
pub use auth::AuthState;
#[cfg(test)]
pub use auth::Identity;
use auth::{AuthStateWatchRequest, LogoutRequest, StartLoginRequest};

use poke::poke_service_client::PokeServiceClient;
use poke::PokeRequest;

use monitor::monitor_service_client::MonitorServiceClient;
use monitor::SubscribeAlertEventFeedRequest;
pub use monitor::{alert_event_feed_frame, Alert, AlertEventFeedFrame};

/// Server-streamed `AuthStateWatch`: each item a full snapshot, or a transport
/// error ending the stream.
pub type AuthStateStream = tonic::Streaming<AuthState>;

/// Server-streamed `SubscribeAlertEventFeed`: the retained alerts, a Bookmark,
/// then each alert as it is raised, or a transport error ending the stream.
pub type AlertEventFeed = tonic::Streaming<AlertEventFeedFrame>;

/// Lazily-dialed, cached gRPC channel; re-dials if lost (sidecar restart).
/// Reach the RPCs through [`SidecarService`](super::SidecarService).
pub struct GrpcClient {
    target: Target,
    // Guarded so concurrent callers share one channel.
    channel: Mutex<Option<Channel>>,
}

impl GrpcClient {
    pub fn new(target: Target) -> Self {
        Self {
            target,
            channel: Mutex::new(None),
        }
    }

    /// Cached channel, or a fresh dial over the IPC socket.
    async fn channel(&self) -> Result<Channel> {
        let mut guard = self.channel.lock().await;
        if let Some(ch) = guard.as_ref() {
            return Ok(ch.clone());
        }
        let ch = connect(self.target.clone()).await?;
        *guard = Some(ch.clone());
        Ok(ch)
    }

    /// Drops the cached channel so the next call re-dials — a stale `Channel`
    /// after a transport failure would keep failing.
    async fn reset(&self) {
        *self.channel.lock().await = None;
    }

    /// Runs the sidecar's synchronous login setup (loopback bind + browser
    /// open); the async sign-in tail reports via [`Self::watch_auth_state`].
    pub async fn start_login(&self) -> Result<()> {
        let mut client = AuthServiceClient::new(self.channel().await?);
        match client.start_login(StartLoginRequest {}).await {
            Ok(_) => Ok(()),
            Err(status) => {
                self.reset().await;
                Err(status_to_err(status))
            }
        }
    }

    /// Clears local credentials, revokes the refresh token (fire-and-forget).
    pub async fn logout(&self) -> Result<()> {
        let mut client = AuthServiceClient::new(self.channel().await?);
        match client.logout(LogoutRequest {}).await {
            Ok(_) => Ok(()),
            Err(status) => {
                self.reset().await;
                Err(status_to_err(status))
            }
        }
    }

    /// Best-effort resync nudge (unary), driven by [`crate::wake`]. See
    /// docs/adr/2026-08-09-poke-resync-fanout.md.
    pub async fn poke(&self) -> Result<()> {
        let mut client = PokeServiceClient::new(self.channel().await?);
        match client.poke(PokeRequest {}).await {
            Ok(_) => Ok(()),
            Err(status) => {
                self.reset().await;
                Err(status_to_err(status))
            }
        }
    }

    /// Opens the auth-state watch stream (current snapshot first, then one per
    /// session change); an error resets the cached channel.
    pub async fn watch_auth_state(&self) -> Result<AuthStateStream> {
        let mut client = AuthServiceClient::new(self.channel().await?);
        match client.auth_state_watch(AuthStateWatchRequest {}).await {
            Ok(resp) => Ok(resp.into_inner()),
            Err(status) => {
                self.reset().await;
                Err(status_to_err(status))
            }
        }
    }

    /// Opens the alert feed (the retained alerts, a Bookmark, then each new
    /// one); an error resets the cached channel.
    pub async fn subscribe_alert_event_feed(&self) -> Result<AlertEventFeed> {
        let mut client = MonitorServiceClient::new(self.channel().await?);
        match client
            .subscribe_alert_event_feed(SubscribeAlertEventFeedRequest {})
            .await
        {
            Ok(resp) => Ok(resp.into_inner()),
            Err(status) => {
                self.reset().await;
                Err(status_to_err(status))
            }
        }
    }
}

/// Dials the IPC socket and completes the h2c handshake. The `http://` origin
/// is a placeholder — tonic needs a valid URI for `:authority`, but the
/// connector ignores it and dials via [`ipc::connect`].
async fn connect(target: Target) -> Result<Channel> {
    TonicEndpoint::from_static("http://kubetail.local")
        .connect_with_connector(service_fn(move |_: Uri| {
            let target = target.clone();
            async move {
                let stream = target
                    .connect()
                    .await
                    .map_err(|err| std::io::Error::other(err.to_string()))?;
                Ok::<_, std::io::Error>(TokioIo::new(stream))
            }
        }))
        .await
        .map_err(|err| AppError::Io(std::io::Error::other(err.to_string())))
}

/// Maps a gRPC `Status` to `AppError::Io` (matching the GraphQL transport's
/// error shape), preserving code + message in the text.
fn status_to_err(status: tonic::Status) -> AppError {
    AppError::Io(std::io::Error::other(format!(
        "grpc {}: {}",
        status.code(),
        status.message()
    )))
}

#[cfg(test)]
mod tests {
    use super::super::testutil::FakeGrpc;
    use super::super::SidecarService;
    use super::{AuthState, Identity};

    #[tokio::test]
    async fn every_call_reaches_the_sidecar_over_one_channel() {
        let fake = FakeGrpc {
            watch: vec![Ok(AuthState {
                authenticated: true,
                identity: Some(Identity::default()),
                restoring: false,
            })],
            ..FakeGrpc::default()
        };
        let calls = fake.calls.clone();
        let served = fake.serve();
        let sidecar = SidecarService::dialing(served.endpoint.clone());

        sidecar.start_login().await.unwrap();
        sidecar.logout().await.unwrap();
        sidecar.poke().await.unwrap();
        let mut stream = sidecar.watch_auth_state().await.unwrap();

        let state = stream.message().await.unwrap().unwrap();
        assert!(state.authenticated);
        assert!(stream.message().await.unwrap().is_none(), "then it ends");
        assert_eq!(*calls.borrow(), 4);
    }

    /// A refused call carries its code and message, and drops the channel so
    /// the next call dials again.
    #[tokio::test]
    async fn a_refused_call_is_an_error_naming_its_status() {
        let served = FakeGrpc {
            refuse: Some(tonic::Code::Unavailable),
            ..FakeGrpc::default()
        }
        .serve();
        let sidecar = SidecarService::dialing(served.endpoint.clone());

        let errors = [
            sidecar.start_login().await.unwrap_err(),
            sidecar.logout().await.unwrap_err(),
            sidecar.poke().await.unwrap_err(),
            sidecar.watch_auth_state().await.err().unwrap(),
            sidecar.subscribe_alert_event_feed().await.err().unwrap(),
        ];

        for err in errors {
            let text = err.to_string();
            assert!(
                text.contains("grpc") && text.contains("refused"),
                "got: {text}"
            );
        }
    }
}
