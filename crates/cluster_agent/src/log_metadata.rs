use notify::RecommendedWatcher;
use prost_types::Timestamp;
use regex::{Captures, Regex};
use std::env;
use std::fs::File;
use std::os::unix::fs::MetadataExt;
use std::path::{Path, PathBuf};
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

use crate::authorizer::Authorizer;
use crate::log_metadata::log_metadata_watcher::LogMetadataWatcher;
use crate::stream_util::wrap_with_shutdown;

mod log_metadata_watcher;

pub static LOG_FILE_REGEX: LazyLock<Regex> = LazyLock::new(|| {
    Regex::new(
            r"^(?P<pod_name>[^_]+)_(?P<namespace>[^_]+)_(?P<container_name>.+)-(?P<container_id>[^-]+)\.log$",
        ).unwrap()
});

#[derive(Debug)]
pub struct LogMetadataImpl {
    logs_dir: PathBuf,
    term_tx: Sender<()>,
    task_tracker: TaskTracker,
    node_name: String,
}

impl LogMetadataImpl {
    pub fn new(logs_dir: PathBuf, term_tx: Sender<()>, task_tracker: TaskTracker) -> Self {
        Self {
            logs_dir,
            term_tx,
            task_tracker,
            node_name: env::var("NODE_NAME").unwrap_or_else(|_| "Env variable not set".to_owned()),
        }
    }

    fn get_log_metadata_spec(
        filepath: &Path,
        namespaces: &[String],
        node_name: &str,
    ) -> Option<LogMetadataSpec> {
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

        if !namespaces.is_empty() && !namespaces.contains(&namespace) {
            return None;
        }

        Some(LogMetadataSpec {
            container_id,
            container_name,
            pod_name,
            node_name: node_name.to_owned(),
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
        let authorizer = Authorizer::new(request.metadata()).await?;
        let request = request.into_inner();

        if !self.logs_dir.is_dir() {
            return Err(Status::new(
                tonic::Code::NotFound,
                format!(
                    "Log directory not found: {}",
                    self.logs_dir.to_string_lossy()
                ),
            ));
        }

        let namespaces: Vec<String> = request
            .namespaces
            .into_iter()
            .filter(|namespace| !namespace.is_empty())
            .collect();

        authorizer.is_authorized(&namespaces, "list").await?;

        let mut files = ReadDirStream::new(read_dir(&self.logs_dir).await?);

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
                Self::get_log_metadata_spec(&file.path(), &namespaces, &self.node_name)
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
        let authorizer = Authorizer::new(request.metadata()).await?;
        let request = request.into_inner();
        let term_tx = self.term_tx.clone();

        let namespaces: Vec<String> = request
            .namespaces
            .into_iter()
            .filter(|namespace| !namespace.is_empty())
            .collect();

        authorizer.is_authorized(&namespaces, "watch").await?;

        let (log_metadata_watcher, log_metadata_rx) = LogMetadataWatcher::new(
            Path::new(&self.logs_dir).to_path_buf(),
            namespaces,
            term_tx,
            self.node_name.clone(),
        );

        self.task_tracker.spawn(async move {
            log_metadata_watcher.watch::<RecommendedWatcher>(None).await;
        });

        Ok(Response::new(wrap_with_shutdown(
            log_metadata_rx,
            self.term_tx.clone(),
        )))
    }
}

#[cfg(test)]
mod test {
    use crate::log_metadata::LogMetadataImpl;
    use serial_test::parallel;
    use std::io::Write;
    use tempfile::{Builder, NamedTempFile};
    use tokio::sync::broadcast;
    use tokio_util::task::TaskTracker;
    use tonic::Request;
    use types::cluster_agent::{
        LogMetadata, LogMetadataListRequest, log_metadata_service_server::LogMetadataService,
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
    #[parallel]
    async fn test_single_file_is_returned() {
        let file = create_test_file("pod-name_single-namespace_container-name-containerid", 4);
        let (term_tx, _term_rx) = broadcast::channel(1);
        let logs_dir = file.path().parent().unwrap().to_path_buf();

        let metadata_service = LogMetadataImpl {
            logs_dir,
            term_tx,
            task_tracker: TaskTracker::new(),
            node_name: "Node name".to_owned(),
        };

        let mut result = metadata_service
            .list(Request::new(LogMetadataListRequest {
                namespaces: vec!["single-namespace".into()],
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
        assert_eq!(
            "single-namespace",
            log_metadata.spec.as_ref().unwrap().namespace
        );
        assert_eq!("pod-name", log_metadata.spec.as_ref().unwrap().pod_name);
        assert_eq!("container-name", log_metadata.spec.unwrap().container_name);
        assert_eq!(4, log_metadata.file_info.unwrap().size);
    }

    #[tokio::test]
    #[parallel]
    async fn test_namespaces_are_filtered() {
        let _first_file = create_test_file(
            "pod-name_filter-firstnamespace_container-name1-containerid1",
            4,
        );
        let _second_file = create_test_file(
            "pod-name_filter-firstnamespace_container-name2-containerid2",
            4,
        );
        let third_file = create_test_file(
            "pod-name_filter-secondnamespace_container-name2-containerid2",
            4,
        );
        let (term_tx, _term_rx) = broadcast::channel(1);
        let logs_dir = third_file.path().parent().unwrap().to_path_buf();

        let metadata_service = LogMetadataImpl {
            logs_dir,
            term_tx,
            task_tracker: TaskTracker::new(),
            node_name: "Node name".to_owned(),
        };

        let mut result = metadata_service
            .list(Request::new(LogMetadataListRequest {
                namespaces: vec!["filter-firstnamespace".into()],
            }))
            .await
            .unwrap()
            .into_inner();

        assert_eq!(2, result.items.len());

        let log_metadata = result.items.pop().unwrap();

        assert_eq!(
            "filter-firstnamespace",
            log_metadata.spec.unwrap().namespace
        );

        let log_metadata = result.items.pop().unwrap();

        assert_eq!(
            "filter-firstnamespace",
            log_metadata.spec.unwrap().namespace
        );

        let mut result = metadata_service
            .list(Request::new(LogMetadataListRequest {
                namespaces: vec!["filter-secondnamespace".into()],
            }))
            .await
            .unwrap()
            .into_inner();

        assert_eq!(1, result.items.len());

        let log_metadata = result.items.pop().unwrap();

        assert_eq!(
            "filter-secondnamespace",
            log_metadata.spec.unwrap().namespace
        );
    }

    #[tokio::test]
    #[parallel]
    async fn test_empty_namespaces_returns_everything() {
        let first_file = create_test_file(
            "pod-name_empty-firstnamespace_container-name1-containerid1",
            4,
        );
        let _second_file = create_test_file(
            "pod-name_empty-secondnamespace_container-name2-containerid2",
            4,
        );

        let (term_tx, _term_rx) = broadcast::channel(1);
        let logs_dir = first_file.path().parent().unwrap().to_path_buf();

        let metadata_service = LogMetadataImpl {
            logs_dir,
            term_tx,
            task_tracker: TaskTracker::new(),
            node_name: "Node name".to_owned(),
        };

        let result = metadata_service
            .list(Request::new(LogMetadataListRequest {
                namespaces: Vec::new(),
            }))
            .await
            .unwrap()
            .into_inner();

        let namespaces = vec![
            String::from("empty-firstnamespace"),
            String::from("empty-secondnamespace"),
        ];

        // Since files for all namespaces are returned, we filtered the ones that were only created
        // during this test.
        let filtered_files: Vec<LogMetadata> = result
            .items
            .into_iter()
            .filter(|log_metadata| {
                namespaces.contains(&log_metadata.spec.as_ref().unwrap().namespace)
            })
            .collect();

        assert_eq!(2, filtered_files.len());
    }
}
