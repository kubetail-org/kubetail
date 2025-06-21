use std::pin::Pin;

use tokio_stream::Stream;
use tonic::{Request, Response, Status};
use types::cluster_agent::log_metadata_service_server::LogMetadataService;
use types::cluster_agent::log_records_service_server::LogRecordsService;
use types::cluster_agent::{
    LogMetadataList, LogMetadataListRequest, LogMetadataWatchEvent, LogMetadataWatchRequest,
    LogRecord, LogRecordsStreamRequest,
};

type AgentResult<T> = Result<Response<T>, Status>;
type WatchResponseStream =
    Pin<Box<dyn Stream<Item = Result<LogMetadataWatchEvent, Status>> + Send>>;
type BackwardForwardResponseStream = Pin<Box<dyn Stream<Item = Result<LogRecord, Status>> + Send>>;

#[derive(Debug, Clone)]
pub struct ClusterAgent;

#[tonic::async_trait]
impl LogMetadataService for ClusterAgent {
    type WatchStream = WatchResponseStream;
    async fn list(
        &self,
        _request: Request<LogMetadataListRequest>,
    ) -> Result<Response<LogMetadataList>, Status> {
        todo!()
    }
    async fn watch(
        &self,
        _request: Request<LogMetadataWatchRequest>,
    ) -> AgentResult<Self::WatchStream> {
        todo!()
    }
}

#[tonic::async_trait]
impl LogRecordsService for ClusterAgent {
    type StreamForwardStream = BackwardForwardResponseStream;
    type StreamBackwardStream = BackwardForwardResponseStream;

    async fn stream_backward(
        &self,
        _request: Request<LogRecordsStreamRequest>,
    ) -> AgentResult<Self::StreamBackwardStream> {
        todo!()
    }

    async fn stream_forward(
        &self,
        _request: Request<LogRecordsStreamRequest>,
    ) -> AgentResult<Self::StreamForwardStream> {
        todo!()
    }
}
