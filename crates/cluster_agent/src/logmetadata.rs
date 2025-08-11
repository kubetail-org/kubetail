use prost_types::Timestamp;
use regex::{Captures, Regex};
use std::fs::File;
use std::os::unix::fs::MetadataExt;
use std::path::Path;
use std::sync::LazyLock;
use tracing::debug;

use tokio::fs::read_dir;
use tokio::sync::broadcast::Sender;
use tokio_stream::StreamExt;
use tokio_stream::wrappers::{ReadDirStream, ReceiverStream};
use tokio_util::task::TaskTracker;
use tonic::{Request, Response, Status};
use types::cluster_agent::log_metadata_service_server::LogMetadataService;
use types::cluster_agent::{
    LogMetadata, LogMetadataFileInfo, LogMetadataList, LogMetadataListRequest, LogMetadataSpec,
    LogMetadataWatchEvent, LogMetadataWatchRequest,
};

use crate::logmetadata::logmetadata_watcher::LogMetadataWatcher;

mod logmetadata_watcher;

pub static LOG_FILE_REGEX: LazyLock<Regex> = LazyLock::new(|| {
    Regex::new(
            r"^(?P<pod_name>[^_]+)_(?P<namespace>[^_]+)_(?P<container_name>.+)-(?P<container_id>[^-]+)\.log$",
        ).unwrap()
});

#[derive(Debug)]
pub struct LogMetadataImpl {
    logs_dir: String,
    term_tx: Sender<()>,
    task_tracker: TaskTracker,
}

impl LogMetadataImpl {
    pub fn new(term_tx: Sender<()>, task_tracker: TaskTracker) -> Self {
        Self {
            logs_dir: "/var/log/containers".into(),
            term_tx,
            task_tracker,
        }
    }

    fn get_log_metadata_spec(filepath: &Path, namespaces: &[String]) -> Option<LogMetadataSpec> {
        let filename = filepath.file_name()?.to_string_lossy();
        let captures = LOG_FILE_REGEX.captures(filename.as_ref());

        if captures.is_none() {
            debug!("Filename could not be parsed: {}", filename.as_ref());
            return None;
        }

        let captures: Captures = captures.unwrap();
        let container_id = captures["container_id"].to_string();
        let container_name = captures["container_name"].to_string();
        let pod_name = captures["pod_name"].to_string();
        let namespace = captures["namespace"].to_string();

        if !namespaces.contains(&namespace) {
            return None;
        }

        Some(LogMetadataSpec {
            container_id,
            container_name,
            pod_name,
            node_name: "The node name".to_string(),
            namespace,
        })
    }

    fn get_file_info(filepath: &Path) -> Result<LogMetadataFileInfo, std::io::Error> {
        let file = File::open(filepath)?;
        let metadata = file.metadata()?;

        Ok(LogMetadataFileInfo {
            size: metadata.size().try_into().unwrap(),
            last_modified_at: metadata.modified().ok().map(Timestamp::from),
        })
    }
}

#[tonic::async_trait]
impl LogMetadataService for LogMetadataImpl {
    type WatchStream = ReceiverStream<Result<LogMetadataWatchEvent, Status>>;

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

        let mut metadata_items = Vec::new();

        while let Some(file) = files.next().await {
            if let Err(io_error) = file {
                match io_error.kind() {
                    std::io::ErrorKind::NotFound => {
                        debug!("Could not open file: {}", io_error);
                        continue;
                    }
                    _ => return Err(io_error.into()),
                }
            }

            let file = file.unwrap();

            let Some(metadata_spec) =
                Self::get_log_metadata_spec(&file.path(), &request.namespaces)
            else {
                continue;
            };

            let file_info = Self::get_file_info(&file.path());

            if let Err(io_error) = file_info {
                match io_error.kind() {
                    std::io::ErrorKind::NotFound => {
                        debug!(
                            "Could not open file {}, error {}",
                            file.path().to_string_lossy(),
                            io_error
                        );
                        continue;
                    }
                    _ => return Err(io_error.into()),
                }
            }

            metadata_items.push(LogMetadata {
                id: metadata_spec.container_id.clone(),
                spec: Some(metadata_spec),
                file_info: Some(file_info.unwrap()),
            });
        }

        return Ok(Response::new(LogMetadataList {
            items: metadata_items,
        }));
    }

    #[tracing::instrument]
    async fn watch(
        &self,
        request: Request<LogMetadataWatchRequest>,
    ) -> Result<Response<Self::WatchStream>, Status> {
        let request = request.into_inner();
        let term_tx = self.term_tx.clone();

        let (log_metadata_watcher, log_metadata_rx) = LogMetadataWatcher::new(
            Path::new(&self.logs_dir).to_path_buf(),
            request.namespaces,
            term_tx,
        );

        self.task_tracker.spawn(async move {
            let _ = log_metadata_watcher.watch().await;
        });

        Ok(Response::new(ReceiverStream::new(log_metadata_rx)))
    }
}

#[cfg(test)]
mod test {
    use crate::logmetadata::LogMetadataImpl;
    use serial_test::serial;
    use std::io::Write;
    use tempfile::{Builder, NamedTempFile};
    use tokio::sync::broadcast;
    use tokio_util::task::TaskTracker;
    use tonic::Request;
    use types::cluster_agent::{
        LogMetadataListRequest, log_metadata_service_server::LogMetadataService,
    };

    pub fn create_test_file(name: &str, num_bytes: usize) -> NamedTempFile {
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
    #[serial]
    async fn test_single_file_is_returned() {
        let file = create_test_file("pod-name_namespace_container-name-containerid", 4);
        let (term_tx, _term_rx) = broadcast::channel(1);
        let logs_dir = file.path().parent().unwrap().to_string_lossy().into_owned();

        let metadata_service = LogMetadataImpl {
            logs_dir,
            term_tx,
            task_tracker: TaskTracker::new(),
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
    #[serial]
    async fn test_namespaces_are_filtered() {
        let _first_file =
            create_test_file("pod-name_firstnamespace_container-name1-containerid1", 4);
        let _second_file =
            create_test_file("pod-name_firstnamespace_container-name2-containerid2", 4);
        let third_file =
            create_test_file("pod-name_secondnamespace_container-name2-containerid2", 4);
        let (term_tx, _term_rx) = broadcast::channel(1);
        let logs_dir = third_file
            .path()
            .parent()
            .unwrap()
            .to_string_lossy()
            .into_owned();

        let metadata_service = LogMetadataImpl {
            logs_dir,
            term_tx,
            task_tracker: TaskTracker::new(),
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
