use std::pin::Pin;

use tokio_stream::Stream;
use tonic::{Request, Response, Status};
use types::cluster_agent::log_metadata_service_server::LogMetadataService;
use types::cluster_agent::{
    LogMetadataList, LogMetadataListRequest, LogMetadataWatchEvent, LogMetadataWatchRequest,
};

type AgentResult<T> = Result<Response<T>, Status>;
type WatchResponseStream =
    Pin<Box<dyn Stream<Item = Result<LogMetadataWatchEvent, Status>> + Send>>;

#[derive(Debug)]
pub struct LogMetadata;

#[tonic::async_trait]
impl LogMetadataService for LogMetadata {
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
