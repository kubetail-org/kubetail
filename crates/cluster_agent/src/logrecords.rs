use tokio_stream::wrappers::ReceiverStream;
use types::cluster_agent::log_records_service_server::LogRecordsService;
use types::cluster_agent::{LogRecord, LogRecordsStreamRequest};

use tonic::{Request, Response, Status};

#[derive(Debug)]
pub struct LogRecords;

#[tonic::async_trait]
impl LogRecordsService for LogRecords {
    type StreamForwardStream = ReceiverStream<Result<LogRecord, Status>>;
    type StreamBackwardStream = ReceiverStream<Result<LogRecord, Status>>;

    async fn stream_backward(
        &self,
        _request: Request<LogRecordsStreamRequest>,
    ) -> Result<Response<Self::StreamBackwardStream>, Status> {
        todo!()
    }

    async fn stream_forward(
        &self,
        _request: Request<LogRecordsStreamRequest>,
    ) -> Result<Response<Self::StreamForwardStream>, Status> {
        todo!()
    }
}
