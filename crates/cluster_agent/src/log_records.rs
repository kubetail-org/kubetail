use std::fs;
use std::path::PathBuf;

use chrono::{DateTime, Utc};
use tokio::sync::broadcast::Sender;
use tokio::sync::mpsc::{self};
use tokio_stream::wrappers::ReceiverStream;
use tokio_util::task::TaskTracker;
use types::cluster_agent::log_records_service_server::LogRecordsService;
use types::cluster_agent::{LogRecord, LogRecordsStreamRequest};

use rgkl::{stream_backward, stream_forward};

use tonic::{Request, Response, Status};

#[derive(Debug)]
pub struct LogRecordsImpl {
    logs_dir: PathBuf,
    term_tx: Sender<()>,
    task_tracker: TaskTracker,
}

impl LogRecordsImpl {
    pub const fn new(logs_dir: PathBuf, term_tx: Sender<()>, task_tracker: TaskTracker) -> Self {
        Self {
            logs_dir,
            term_tx,
            task_tracker,
        }
    }

    fn get_log_filename(&self, request: &LogRecordsStreamRequest) -> Result<PathBuf, Box<Status>> {
        let container_id = match request.container_id.split_once("://") {
            Some((_, second)) => second,
            None => &request.container_id,
        };

        let path = self.logs_dir.join(format!(
            "{}_{}_{}-{}.log",
            &request.pod_name, &request.namespace, &request.container_name, container_id
        ));

        if path.is_file() {
            Ok(fs::canonicalize(path).unwrap())
        } else {
            Err(Status::new(
                tonic::Code::NotFound,
                format!("log file not found: {}", path.to_string_lossy()),
            )
            .into())
        }
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
        let request = request.into_inner();
        let file_path = self.get_log_filename(&request).map_err(|status| *status)?;
        let (tx, rx) = mpsc::channel(100);
        let term_tx = self.term_tx.clone();

        self.task_tracker.spawn(async move {
            stream_backward::stream_backward(
                &file_path,
                request.start_time.parse::<DateTime<Utc>>().ok(),
                request.stop_time.parse::<DateTime<Utc>>().ok(),
                if request.grep.is_empty() {
                    None
                } else {
                    Some(&request.grep)
                },
                term_tx,
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
        let request = request.into_inner();
        let file_path = self.get_log_filename(&request).map_err(|status| *status)?;

        let (tx, rx) = mpsc::channel(100);
        let term_tx = self.term_tx.clone();

        self.task_tracker.spawn(async move {
            stream_forward::stream_forward(
                &file_path,
                request.start_time.parse::<DateTime<Utc>>().ok(),
                request.stop_time.parse::<DateTime<Utc>>().ok(),
                if request.grep.is_empty() {
                    None
                } else {
                    Some(&request.grep)
                },
                request.follow_from(),
                term_tx,
                tx,
            )
            .await;
        });

        Ok(Response::new(ReceiverStream::new(rx)))
    }
}
