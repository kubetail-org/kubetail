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

use std::fs;
use std::path::PathBuf;

use chrono::{DateTime, Utc};
use tokio::sync::mpsc::{self};
use tokio_stream::wrappers::ReceiverStream;
use tokio_util::sync::CancellationToken;
use tokio_util::task::TaskTracker;
use types::cluster_agent::log_records_service_server::LogRecordsService;
use types::cluster_agent::{LogRecord, LogRecordsStreamRequest};

use rgkl::{stream_backward, stream_forward};

use tonic::{Request, Response, Status};

use crate::authorizer::Authorizer;

#[derive(Debug)]
pub struct LogRecordsImpl {
    ctx: CancellationToken,
    task_tracker: TaskTracker,
    logs_dir: PathBuf,
}

impl LogRecordsImpl {
    pub const fn new(ctx: CancellationToken, task_tracker: TaskTracker, logs_dir: PathBuf) -> Self {
        Self {
            ctx,
            task_tracker,
            logs_dir,
        }
    }

    fn get_log_filename(&self, request: &LogRecordsStreamRequest) -> Result<PathBuf, Status> {
        let container_id = match request.container_id.split_once("://") {
            Some((_, second)) => second,
            None => &request.container_id,
        };

        let path = self.logs_dir.join(format!(
            "{}_{}_{}-{}.log",
            &request.pod_name, &request.namespace, &request.container_name, container_id
        ));

        fs::canonicalize(&path).map_err(|e| {
            Status::new(
                tonic::Code::NotFound,
                format!(
                    "failed to access log file {}: {}",
                    path.to_string_lossy(),
                    e
                ),
            )
        })
    }
}

#[tonic::async_trait]
impl LogRecordsService for LogRecordsImpl {
    type StreamForwardStream = ReceiverStream<Result<LogRecord, Status>>;
    type StreamBackwardStream = ReceiverStream<Result<LogRecord, Status>>;

    #[tracing::instrument]
    async fn stream_backward(
        &self,
        request: Request<LogRecordsStreamRequest>,
    ) -> Result<Response<Self::StreamBackwardStream>, Status> {
        let authorizer = Authorizer::new(request.metadata()).await?;
        let request = request.into_inner();
        let file_path = self.get_log_filename(&request)?;
        let (tx, rx) = mpsc::channel(100);
        let local_ctx = self.ctx.child_token();

        let namespaces = vec![request.namespace.clone()];
        authorizer.is_authorized(&namespaces, "list").await?;

        self.task_tracker.spawn(async move {
            stream_backward::stream_backward(
                local_ctx,
                &file_path,
                request.start_time.parse::<DateTime<Utc>>().ok(),
                request.stop_time.parse::<DateTime<Utc>>().ok(),
                if request.grep.is_empty() {
                    None
                } else {
                    Some(&request.grep)
                },
                tx,
            )
            .await;
        });

        Ok(Response::new(ReceiverStream::new(rx)))
    }

    #[tracing::instrument]
    async fn stream_forward(
        &self,
        request: Request<LogRecordsStreamRequest>,
    ) -> Result<Response<Self::StreamForwardStream>, Status> {
        let authorizer = Authorizer::new(request.metadata()).await?;
        let request = request.into_inner();
        let file_path = self.get_log_filename(&request)?;

        let namespaces = vec![request.namespace.clone()];
        authorizer.is_authorized(&namespaces, "list").await?;

        let (tx, rx) = mpsc::channel(100);
        let local_ctx = self.ctx.child_token();

        self.task_tracker.spawn(async move {
            stream_forward::stream_forward(
                local_ctx,
                &file_path,
                request.start_time.parse::<DateTime<Utc>>().ok(),
                request.stop_time.parse::<DateTime<Utc>>().ok(),
                if request.grep.is_empty() {
                    None
                } else {
                    Some(&request.grep)
                },
                request.follow_from(),
                tx,
            )
            .await;
        });

        Ok(Response::new(ReceiverStream::new(rx)))
    }
}
