use std::fs;
use std::path::{Path, PathBuf};

use tokio::sync::mpsc;
use tokio_stream::wrappers::ReceiverStream;
use types::cluster_agent::log_records_service_server::LogRecordsService;
use types::cluster_agent::{LogRecord, LogRecordsStreamRequest};

use rgkl::{stream_backward, stream_forward};

use tonic::{Request, Response, Status};

#[derive(Debug)]
pub struct LogRecords {
    logs_dir: &'static str,
}

impl LogRecords {
    pub fn new() -> Self {
        Self {
            logs_dir: "/var/log/containers",
        }
    }

    fn get_log_filename(&self, request: &LogRecordsStreamRequest) -> Result<PathBuf, Status> {
        let container_id = match request.container_id.split_once("://") {
            Some((_, second)) => second,
            None => &request.container_id,
        };

        let path = Path::new(self.logs_dir).join(format!(
            "{}_{}_{}-{}.log",
            &request.pod_name, &request.namespace, &request.container_name, container_id
        ));

        if path.is_file() {
            Ok(fs::canonicalize(path).unwrap())
        } else {
            Err(Status::new(
                tonic::Code::NotFound,
                format!("log file not found: {}", path.to_string_lossy()),
            ))
        }
    }
}

#[tonic::async_trait]
impl LogRecordsService for LogRecords {
    type StreamForwardStream = ReceiverStream<Result<LogRecord, Status>>;
    type StreamBackwardStream = ReceiverStream<Result<LogRecord, Status>>;

    async fn stream_backward(
        &self,
        request: Request<LogRecordsStreamRequest>,
    ) -> Result<Response<Self::StreamBackwardStream>, Status> {
        println!("Request = {:?}", request);

        let file_path = self.get_log_filename(request.get_ref())?;
        let (tx, rx) = mpsc::channel(10);

        tokio::spawn(async move {
            let (term_tx, term_rx) = crossbeam_channel::unbounded();
            stream_backward::stream_backward(&file_path, None, None, None, term_rx, tx).await;
        });

        Ok(Response::new(ReceiverStream::new(rx)))
    }

    async fn stream_forward(
        &self,
        request: Request<LogRecordsStreamRequest>,
    ) -> Result<Response<Self::StreamForwardStream>, Status> {
        println!("Request = {:?}", request);

        let file_path = self.get_log_filename(request.get_ref())?;

        let (tx, rx) = mpsc::channel(10);

        tokio::spawn(async move {
            let (term_tx, term_rx) = crossbeam_channel::unbounded();
            stream_forward::stream_forward(
                &file_path,
                None,
                None,
                None,
                stream_forward::FollowFrom::End,
                term_rx,
                tx,
            )
            .await;
        });

        Ok(Response::new(ReceiverStream::new(rx)))
    }
}
