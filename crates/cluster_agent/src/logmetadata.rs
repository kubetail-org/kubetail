use prost_wkt_types::Timestamp;
use regex::Regex;
use std::fs::File;
use std::os::unix::fs::MetadataExt;
use std::path::{Path, PathBuf};
use std::pin::Pin;

use tokio::fs::read_dir;
use tokio::sync::broadcast::Sender;
use tokio::sync::mpsc;
use tokio_stream::wrappers::ReadDirStream;
use tokio_stream::{Stream, StreamExt};
use tokio_util::task::TaskTracker;
use tonic::{Request, Response, Status};
use types::cluster_agent::log_metadata_service_server::LogMetadataService;
use types::cluster_agent::{
    LogMetadata, LogMetadataFileInfo, LogMetadataList, LogMetadataListRequest, LogMetadataSpec,
    LogMetadataWatchEvent, LogMetadataWatchRequest,
};

type AgentResult<T> = Result<Response<T>, Status>;
type WatchResponseStream =
    Pin<Box<dyn Stream<Item = Result<LogMetadataWatchEvent, Status>> + Send>>;

#[derive(Debug)]
pub struct LogMetadataImpl {
    logs_dir: &'static str,
    term_tx: Sender<()>,
    task_tracker: TaskTracker,
}

impl LogMetadataImpl {
    pub const fn new(term_tx: Sender<()>, task_tracker: TaskTracker) -> Self {
        Self {
            logs_dir: "/var/log/containers",
            term_tx,
            task_tracker,
        }
    }

    fn get_file_info(filepath: PathBuf) -> Result<LogMetadataFileInfo, Status> {
        let metadata = File::open(filepath)?.metadata()?;

        Ok(LogMetadataFileInfo {
            size: metadata.size().try_into().unwrap(),
            last_modified_at: metadata.modified().ok().map(Timestamp::from),
        })
    }
}

#[tonic::async_trait]
impl LogMetadataService for LogMetadataImpl {
    type WatchStream = WatchResponseStream;

    async fn list(
        &self,
        request: Request<LogMetadataListRequest>,
    ) -> Result<Response<LogMetadataList>, Status> {
        let request = request.into_inner();
        let logs_dir_path = Path::new(self.logs_dir);

        if !logs_dir_path.is_dir() {
            return Err(Status::new(
                tonic::Code::NotFound,
                format!(
                    "log directory not found: {}",
                    logs_dir_path.to_string_lossy()
                ),
            ));
        }

        let mut files = ReadDirStream::new(read_dir(logs_dir_path).await?);

        let filename_regex = Regex::new(
            r"^(?P<pod_name>[^_]+)_(?P<namespace>[^_]+)_(?P<container_name>.+)-(?P<container_id>[^-]+)\.log$",
        ).unwrap();

        let mut metadata_items = Vec::new();

        while let Some(file) = files.next().await {
            let file = match file {
                Ok(file) => file,
                Err(error) => return Err(Status::new(tonic::Code::Unknown, error.to_string())),
            };

            let filepath: PathBuf = file.file_name().into();
            let filename = filepath.to_string_lossy();
            let captures = filename_regex.captures(filename.as_ref());

            if captures.is_none() {
                println!("Filename could not be parsed: {}", filename.as_ref());
                continue;
            }

            let captures = captures.unwrap();
            let container_id = captures["container_id"].to_string();
            let container_name = captures["container_name"].to_string();
            let pod_name = captures["pod_name"].to_string();
            let namespace = captures["namespace"].to_string();
            let mut absolute_file_path = logs_dir_path.to_path_buf();
            absolute_file_path.push(filepath);

            if !request.namespaces.contains(&namespace) {
                continue;
            }

            metadata_items.push(LogMetadata {
                id: container_id.clone(),
                spec: Some(LogMetadataSpec {
                    container_id,
                    container_name,
                    pod_name,
                    node_name: "The node name".to_string(),
                    namespace,
                }),
                file_info: Some(Self::get_file_info(absolute_file_path)?),
            });
        }

        return Ok(Response::new(LogMetadataList {
            items: metadata_items,
        }));
    }

    async fn watch(
        &self,
        _request: Request<LogMetadataWatchRequest>,
    ) -> AgentResult<Self::WatchStream> {
        todo!()
    }
}
