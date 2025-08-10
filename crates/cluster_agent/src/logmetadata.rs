use prost_wkt_types::Timestamp;
use regex::Regex;
use std::fs::File;
use std::os::unix::fs::MetadataExt;
use std::path::{Path, PathBuf};
use std::pin::Pin;

use tokio::fs::read_dir;
use tokio::sync::broadcast::Sender;
use tokio_stream::wrappers::ReadDirStream;
use tokio_stream::{Stream, StreamExt};
use tokio_util::task::TaskTracker;
use tonic::{Request, Response, Status};
use tracing::warn;
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
    logs_dir: String,
    _term_tx: Sender<()>,
    _task_tracker: TaskTracker,
}

impl LogMetadataImpl {
    pub fn new(_term_tx: Sender<()>, _task_tracker: TaskTracker) -> Self {
        Self {
            logs_dir: "/var/log/containers".into(),
            _term_tx,
            _task_tracker,
        }
    }

    fn get_file_info(filepath: PathBuf) -> Result<LogMetadataFileInfo, Box<Status>> {
        let file = File::open(filepath).map_err(|err| Box::new(err.into()))?;
        let metadata = file.metadata().map_err(|err| Box::new(err.into()))?;

        Ok(LogMetadataFileInfo {
            size: metadata.size().try_into().unwrap(),
            last_modified_at: metadata.modified().ok().map(Timestamp::from),
        })
    }
}

#[tonic::async_trait]
impl LogMetadataService for LogMetadataImpl {
    type WatchStream = WatchResponseStream;

    #[tracing::instrument]
    async fn list(
        &self,
        request: Request<LogMetadataListRequest>,
    ) -> Result<Response<LogMetadataList>, Status> {
        let request = request.into_inner();
        let logs_dir_path = Path::new(self.logs_dir.as_str());

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
                warn!("Filename could not be parsed: {}", filename.as_ref());
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
                file_info: Some(Self::get_file_info(absolute_file_path).map_err(|status| *status)?),
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

#[cfg(test)]
mod test {
    use crate::logmetadata::LogMetadataImpl;
    use std::io::Write;
    use tempfile::{Builder, NamedTempFile};
    use tokio::sync::broadcast;
    use tokio_util::task::TaskTracker;
    use tonic::Request;
    use tracing_test::traced_test;
    use types::cluster_agent::{
        LogMetadataListRequest, log_metadata_service_server::LogMetadataService,
    };

    fn create_test_file(name: &str, num_bytes: usize) -> NamedTempFile {
        let mut test_file = Builder::new()
            .prefix(name)
            .suffix(".log")
            .tempfile()
            .expect("Failed to create file");

        test_file
            .write_all(&vec![0; num_bytes])
            .expect("Failed to write to file");

        test_file
    }

    #[tokio::test]
    #[traced_test]
    async fn test_single_file_is_returned() {
        let file = create_test_file("pod-name_namespace_container-name-containerid", 4);
        let (_term_tx, _term_rx) = broadcast::channel(1);
        let logs_dir = file.path().parent().unwrap().to_string_lossy().into_owned();

        let metadata_service = LogMetadataImpl {
            logs_dir,
            _term_tx,
            _task_tracker: TaskTracker::new(),
        };

        let mut result = metadata_service
            .list(Request::new(LogMetadataListRequest {
                namespaces: vec!["namespace".into()],
            }))
            .await
            .unwrap()
            .into_inner();

        assert_eq!(1, result.items.len());

        let log_metadata = result.items.pop().unwrap();

        assert!(log_metadata.id.starts_with("containerid"));

        assert!(
            log_metadata
                .spec
                .as_ref()
                .unwrap()
                .container_id
                .starts_with("containerid")
        );
        assert_eq!("namespace", log_metadata.spec.as_ref().unwrap().namespace);
        assert_eq!("pod-name", log_metadata.spec.as_ref().unwrap().pod_name);
        assert_eq!("container-name", log_metadata.spec.unwrap().container_name);
        assert_eq!(4, log_metadata.file_info.unwrap().size);
    }

    #[tokio::test]
    #[traced_test]
    async fn test_namespaces_are_filtered() {
        let _first_file =
            create_test_file("pod-name_firstnamespace_container-name1-containerid1", 4);
        let _second_file =
            create_test_file("pod-name_firstnamespace_container-name2-containerid2", 4);
        let third_file =
            create_test_file("pod-name_secondnamespace_container-name2-containerid2", 4);
        let (_term_tx, _term_rx) = broadcast::channel(1);
        let logs_dir = third_file
            .path()
            .parent()
            .unwrap()
            .to_string_lossy()
            .into_owned();

        let metadata_service = LogMetadataImpl {
            logs_dir,
            _term_tx,
            _task_tracker: TaskTracker::new(),
        };

        let mut result = metadata_service
            .list(Request::new(LogMetadataListRequest {
                namespaces: vec!["firstnamespace".into()],
            }))
            .await
            .unwrap()
            .into_inner();

        assert_eq!(2, result.items.len());

        let log_metadata = result.items.pop().unwrap();

        assert_eq!("firstnamespace", log_metadata.spec.unwrap().namespace);

        let log_metadata = result.items.pop().unwrap();

        assert_eq!("firstnamespace", log_metadata.spec.unwrap().namespace);

        let mut result = metadata_service
            .list(Request::new(LogMetadataListRequest {
                namespaces: vec!["secondnamespace".into()],
            }))
            .await
            .unwrap()
            .into_inner();

        assert_eq!(1, result.items.len());

        let log_metadata = result.items.pop().unwrap();

        assert_eq!("secondnamespace", log_metadata.spec.unwrap().namespace);
    }
}
