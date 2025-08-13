use std::{
    io,
    path::{Path, PathBuf},
    time::Duration,
};

use notify::{Event, EventKind, RecommendedWatcher, RecursiveMode};
use notify_debouncer_full::{DebounceEventResult, Debouncer, RecommendedCache, new_debouncer};
use thiserror::Error;
use tokio::{
    fs::read_dir,
    runtime::Handle,
    select,
    sync::{
        broadcast::Sender as BcSender,
        mpsc::{Receiver, Sender, channel},
    },
};
use tokio_stream::{StreamExt, wrappers::ReadDirStream};
use tonic::Status;
use tracing::{debug, info, warn};
use types::cluster_agent::{LogMetadata, LogMetadataWatchEvent};

use crate::logmetadata::{LOG_FILE_REGEX, LogMetadataImpl};

/// Uses notify crate internally to provide notifications of file updates.
#[derive(Debug)]
pub struct LogMetadataWatcher {
    /// Internal channel to send log metadata updates.
    log_metadata_tx: Sender<Result<LogMetadataWatchEvent, Status>>,
    /// Channel to receive a termination signal and end the watch loop.
    term_tx: BcSender<()>,
    /// K8s namespaces to watch for.
    namespaces: Vec<String>,
    /// Directory to watch for updates.
    directory: PathBuf,
}

impl LogMetadataWatcher {
    /// Returns a new watcher and a channel to receive log metadata updates.
    pub fn new(
        directory: PathBuf,
        namespaces: Vec<String>,
        term_tx: BcSender<()>,
    ) -> (Self, Receiver<Result<LogMetadataWatchEvent, Status>>) {
        let (log_metadata_tx, log_metadata_rx) = channel(100);

        (
            Self {
                log_metadata_tx,
                term_tx,
                namespaces,
                directory,
            },
            log_metadata_rx,
        )
    }

    /// Starts watching the log directory for log updates. Blocks until a message is sent in the
    /// termination channel.
    pub async fn watch(&self) {
        let (internal_tx, internal_rx) = channel(10);
        let setup_result = self.setup_notify_watcher(internal_tx).await;

        if let Err(watcher_error) = setup_result {
            let _ = self.log_metadata_tx.send(Err(watcher_error.into())).await;
            return;
        }

        let debouncer = setup_result.unwrap();
        let term_rx = self.term_tx.subscribe();

        self.listen_for_changes(internal_rx, debouncer, term_rx)
            .await;
    }

    /// Creates the notify fs watcher and adds to the watch list all files
    /// that have the correct k8s namespace.
    ///
    /// # Arguments
    ///
    /// * `internal_tx` - The sender to use to propagate filesystem updates.
    async fn setup_notify_watcher(
        &self,
        internal_tx: Sender<Vec<Result<LogMetadataWatchEvent, WatcherError>>>,
    ) -> Result<Debouncer<RecommendedWatcher, RecommendedCache>, WatcherError> {
        let runtime_handle = Handle::current();
        let namespaces = self.namespaces.clone();

        let mut debouncer = new_debouncer(
            Duration::from_secs(2),
            None,
            move |result: DebounceEventResult| {
                runtime_handle.block_on(async {
                    let _ = internal_tx
                        .send(handle_debounced_events(result, &namespaces))
                        .await;
                });
            },
        )?;

        let paths_to_add = find_log_files(&self.directory, &self.namespaces).await?;

        for path in paths_to_add {
            debouncer.watch(&path, notify::RecursiveMode::NonRecursive)?;
        }

        debouncer.watch(&self.directory, notify::RecursiveMode::NonRecursive)?;

        Ok(debouncer)
    }

    /// Blocks and listens for notify fs changes until either a message is sent to `term_rx` or
    /// `log_metadata_tx` is closed.
    ///
    /// # Arguments
    ///
    /// * `internal_rx` - Receiver of filesystem updates.
    /// * `debouncer` - The notify filesystem watcher.
    /// * `term_rx` - Receiver of the termination channel.
    async fn listen_for_changes(
        &self,
        mut internal_rx: Receiver<Vec<Result<LogMetadataWatchEvent, WatcherError>>>,
        mut debouncer: Debouncer<RecommendedWatcher, RecommendedCache>,
        mut term_rx: tokio::sync::broadcast::Receiver<()>,
    ) {
        'outer: loop {
            select! {
                metadata_events = internal_rx.recv() => {
                    if let Some(metadata_events) = metadata_events {
                        for metadata_event in metadata_events {
                            if let Ok(ref metadata_event) = metadata_event {
                                self.update_watcher(metadata_event, &mut debouncer);
                            }

                            if self.log_metadata_tx.send(metadata_event.map_err(Status::from)).await.is_err() {
                                    info!("Channel closed from client.");
                                    break 'outer;
                            }
                        }
                    } else {
                        warn!("Internal channel closed!");
                        break;
                    }
                }
                _ = term_rx.recv() => {
                        debug!("Received termination message");
                        break;
                    }
            }
        }

        info!("Stopping watcher..");
        debouncer.stop();
    }

    // In case of a new log file creation, it adds the path to the notify watcher in order to
    // receive updates for the file in the future. On removal, the path is removed from the watcher
    // accordingly.
    fn update_watcher(
        &self,
        watch_event: &LogMetadataWatchEvent,
        watcher: &mut Debouncer<RecommendedWatcher, RecommendedCache>,
    ) {
        match LogMetadataWatchEventType::from_str(&watch_event.r#type) {
            // Methods watch and unwatch can fail on adding an existing path or on removing a
            // non-existing one. There are no specific actions needed in case this happens.
            Some(LogMetadataWatchEventType::Added) => {
                let _ = watcher.watch(self.get_file_path(watch_event), RecursiveMode::NonRecursive);
            }
            Some(LogMetadataWatchEventType::Deleted) => {
                let _ = watcher.unwatch(self.get_file_path(watch_event));
            }
            _ => (),
        }
    }

    // Reconstruct the absolut file path from a LogMetadataWatchEvent.
    fn get_file_path(&self, watch_event: &LogMetadataWatchEvent) -> PathBuf {
        let file_metadata = watch_event.object.as_ref().unwrap().spec.as_ref().unwrap();
        let mut file_path = self.directory.clone();
        file_path.set_file_name(format!(
            "{}_{}_{}-{}.log",
            file_metadata.pod_name,
            file_metadata.namespace,
            file_metadata.container_name,
            file_metadata.container_id
        ));
        file_path
    }
}

#[derive(Error, Debug)]
enum WatcherError {
    #[error("Error while accessing file: {0}")]
    Io(#[from] io::Error),

    #[error("Error while trying to watch: {0}")]
    Watch(#[from] notify::Error),

    #[error("Log directory not found: {0}")]
    DirNotFound(String),
}

impl From<WatcherError> for Status {
    fn from(watcher_error: WatcherError) -> Self {
        match watcher_error {
            WatcherError::Io(io_error) => io_error.into(),
            WatcherError::Watch(notify_error) => Self::from_error(Box::new(notify_error)),
            WatcherError::DirNotFound(_) => {
                Self::new(tonic::Code::NotFound, watcher_error.to_string())
            }
        }
    }
}

// Helper method to find the log files in a directory that belonging to the specified namespaces.
async fn find_log_files(
    directory: &Path,
    namespaces: &[String],
) -> Result<Vec<PathBuf>, WatcherError> {
    if !directory.is_dir() {
        return Err(WatcherError::DirNotFound(
            directory.to_string_lossy().to_string(),
        ));
    }

    let result = ReadDirStream::new(read_dir(directory).await?)
        .collect::<Result<Vec<_>, _>>()
        .await?
        .into_iter()
        .filter_map(|file| {
            let filename = file.file_name();
            let filename = filename.to_string_lossy();
            let captures = LOG_FILE_REGEX.captures(&filename);

            if captures.is_some()
                && namespaces.contains(
                    &captures
                        .unwrap()
                        .name("namespace")
                        .unwrap()
                        .as_str()
                        .to_owned(),
                )
            {
                let mut absolute_path = directory.to_path_buf();
                absolute_path.push(file.file_name());

                Some(absolute_path)
            } else {
                None
            }
        })
        .collect();

    Ok(result)
}

// A DebounceEventResult contains many file events. This method breaks it down and transforms each
// event to a LogMetadataWatchEvent or to an error in case the debounced events are errors.
fn handle_debounced_events(
    debounced_event_result: DebounceEventResult,
    namespaces: &[String],
) -> Vec<Result<LogMetadataWatchEvent, WatcherError>> {
    if let Err(errors) = debounced_event_result {
        return errors.into_iter().map(|error| Err(error.into())).collect();
    }

    // TODO: As we are mapping many types of events into three types of LogMetadataWatchEvent, this
    // function can still produce duplicates. We should probably dedup using an IndexSet.
    debounced_event_result
        .unwrap()
        .into_iter()
        .filter(|debounced_event| {
            matches!(
                debounced_event.kind,
                EventKind::Create(_) | EventKind::Modify(_) | EventKind::Remove(_)
            )
        })
        .filter_map(|debounced_event| transform_notify_event(&debounced_event.event, namespaces))
        .collect()
}

// Transform a single Event to a LogMetadataWatchEvent. Fails in cases there is an IO error when
// trying to get the file metadata from the filesystem.
fn transform_notify_event(
    event: &Event,
    namespaces: &[String],
) -> Option<Result<LogMetadataWatchEvent, WatcherError>> {
    let mut event_type = match event.kind {
        EventKind::Modify(_) => LogMetadataWatchEventType::Modified,
        EventKind::Create(_) => LogMetadataWatchEventType::Added,
        EventKind::Remove(_) => LogMetadataWatchEventType::Deleted,
        _ => return None,
    };

    let path = event.paths.first()?;

    let metadata_spec = LogMetadataImpl::get_log_metadata_spec(path, namespaces)?;
    let file_info = LogMetadataImpl::get_file_info(path);

    // In case the file doesn't exist turn the event into a deletion event, otherwise propagete the
    // error.
    if let Err(ref io_error) = file_info {
        if io_error.kind() == std::io::ErrorKind::NotFound {
            event_type = LogMetadataWatchEventType::Deleted;
        } else {
            return Some(Err(file_info.unwrap_err().into()));
        }
    }

    Some(Ok(LogMetadataWatchEvent {
        r#type: event_type.as_str().to_owned(),
        object: Some(LogMetadata {
            id: metadata_spec.container_id.clone(),
            spec: Some(metadata_spec),
            file_info: file_info.ok(),
        }),
    }))
}

#[derive(Debug)]
enum LogMetadataWatchEventType {
    Added,
    Modified,
    Deleted,
}

impl LogMetadataWatchEventType {
    fn from_str(value: &str) -> Option<Self> {
        match value {
            "ADDED" => Some(Self::Added),
            "MODIFIED" => Some(Self::Modified),
            "DELETED" => Some(Self::Deleted),
            _ => None,
        }
    }

    const fn as_str(&self) -> &'static str {
        match self {
            Self::Added => "ADDED",
            Self::Modified => "MODIFIED",
            Self::Deleted => "DELETED",
        }
    }
}

#[cfg(test)]
mod test {
    use std::{
        fs::{File, remove_file, rename},
        io::Write,
    };

    use crate::logmetadata::test::create_test_file;

    use super::*;
    use serial_test::serial;
    use tokio::{
        sync::{broadcast, mpsc::error::TryRecvError},
        task,
        time::sleep,
    };

    #[tokio::test]
    #[serial]
    async fn test_create_events_are_generated() {
        let file = create_test_file("pod-name_namespace_container-name-containerid", 4);
        let namespaces = vec!["namespace".into()];
        let (term_tx, _term_rx) = broadcast::channel(1);
        let logs_dir = file.path().parent().unwrap().to_owned();

        let (log_metadata_watcher, mut log_metadata_rx) =
            LogMetadataWatcher::new(logs_dir, namespaces, term_tx.clone());

        // Start the watcher and give it some time to execute before creating events.
        task::spawn(async move { log_metadata_watcher.watch().await });
        sleep(Duration::from_millis(100)).await;

        // Create three files, one with an unrelated namespace.
        let _first_file =
            create_test_file("pod-name_namespace_firstContainer-name-firstContainerid", 4);
        let _second_file = create_test_file(
            "pod-name_namespace_secondContainer-name-secondContainerid",
            4,
        );
        let _third_file = create_test_file("pod-name_wrongNamespace_container-name-containerid", 4);

        // Get the events and verify them.
        let first_event = log_metadata_rx.recv().await.unwrap().unwrap();
        verify_event(
            first_event,
            "ADDED",
            "firstContainerid",
            "The node name",
            "namespace",
            "pod-name",
            "firstContainer-name",
            Some(4),
        );

        let second_event = log_metadata_rx.recv().await.unwrap().unwrap();
        verify_event(
            second_event,
            "ADDED",
            "secondContainerid",
            "The node name",
            "namespace",
            "pod-name",
            "secondContainer-name",
            Some(4),
        );

        // Ensure no more events are created.
        let result = log_metadata_rx.try_recv();
        assert!(matches!(result, Err(TryRecvError::Empty)));
    }

    #[tokio::test]
    async fn test_error_is_returned_on_unknown_directory() {
        let namespaces = vec!["namespace".into()];
        let (term_tx, _term_rx) = broadcast::channel(1);
        let logs_dir = PathBuf::from("/a/dir/that/doesnt/exist");

        let (log_metadata_watcher, mut log_metadata_rx) =
            LogMetadataWatcher::new(logs_dir, namespaces, term_tx.clone());

        task::spawn(async move { log_metadata_watcher.watch().await });
        sleep(Duration::from_millis(100)).await;

        let result = log_metadata_rx.recv().await.unwrap();
        assert!(matches!(result, Err(_)));

        let status = result.unwrap_err();
        assert_eq!(status.code(), tonic::Code::NotFound);
        assert!(status.message().contains("/a/dir/that/doesnt/exist"));
    }

    #[tokio::test]
    #[serial]
    async fn test_delete_events_are_generated() {
        let file = create_test_file("pod-name_namespace_container-name-containerid", 4);
        let namespaces = vec!["namespace".into()];
        let (term_tx, _term_rx) = broadcast::channel(1);
        let logs_dir = file.path().parent().unwrap().to_owned();

        let (log_metadata_watcher, mut log_metadata_rx) =
            LogMetadataWatcher::new(logs_dir, namespaces, term_tx.clone());

        // Start the watcher and give it some time to execute before creating events.
        task::spawn(async move { log_metadata_watcher.watch().await });
        sleep(Duration::from_millis(100)).await;

        // Delete the file.
        let _ = file.close();

        // Receive the events and verify them.
        let event = log_metadata_rx.recv().await.unwrap().unwrap();
        verify_event(
            event,
            "DELETED",
            "containerid",
            "The node name",
            "namespace",
            "pod-name",
            "container-name",
            None,
        );
    }

    #[tokio::test]
    #[serial]
    async fn test_renamed_file_is_being_watched() {
        let file = create_test_file("pod-name_namespace_container-name-containerid", 4);
        let namespaces = vec!["namespace".into()];
        let (term_tx, _term_rx) = broadcast::channel(1);
        let logs_dir = file.path().parent().unwrap().to_owned();

        let (log_metadata_watcher, mut log_metadata_rx) =
            LogMetadataWatcher::new(logs_dir, namespaces, term_tx.clone());

        // Start the watcher and give it some time to execute before creating events.
        task::spawn(async move { log_metadata_watcher.watch().await });
        sleep(Duration::from_millis(100)).await;

        // Rename the file.
        let mut new_path = file.path().to_owned();
        new_path.set_file_name("pod-name_namespace_container-name-updatedcontainerid.log");
        let _ = rename(file.path(), &new_path);

        // Edit the file.
        let mut renamed_file = File::options()
            .write(true)
            .append(true)
            .open(&new_path)
            .unwrap();
        let _ = renamed_file.write_all(&vec![1; 5]);

        // Ensure that the old file path is marked as deleted and that the new path is modified.
        for _i in 0..3 {
            let event = log_metadata_rx.recv().await.unwrap().unwrap();

            if event.r#type == "DELETED" {
                verify_event(
                    event,
                    "DELETED",
                    "containerid",
                    "The node name",
                    "namespace",
                    "pod-name",
                    "container-name",
                    None,
                );
            } else {
                verify_event(
                    event,
                    "MODIFIED",
                    "updatedcontainerid",
                    "The node name",
                    "namespace",
                    "pod-name",
                    "container-name",
                    Some(9),
                );
            }
        }

        // The renamed file won't be delted automatically when it gets out of scope.
        let _ = remove_file(&new_path);
    }

    fn verify_event(
        event: LogMetadataWatchEvent,
        event_type: &str,
        container_id: &str,
        node_name: &str,
        namespace: &str,
        pod_name: &str,
        container_name: &str,
        file_size: Option<usize>,
    ) {
        assert_eq!(event.r#type, event_type);
        assert!(event.object.as_ref().unwrap().id.starts_with(container_id));

        let event_spec = event.object.as_ref().unwrap().spec.as_ref().unwrap();
        let event_file_info = event.object.as_ref().unwrap().file_info;

        assert_eq!(event_spec.node_name, node_name);
        assert_eq!(event_spec.namespace, namespace);
        assert_eq!(event_spec.pod_name, pod_name);
        assert_eq!(event_spec.container_name, container_name);

        if let Some(file_size) = file_size {
            assert_eq!(event_file_info.as_ref().unwrap().size, file_size as i64);
        } else {
            assert_eq!(event_file_info, None);
        }
    }
}
