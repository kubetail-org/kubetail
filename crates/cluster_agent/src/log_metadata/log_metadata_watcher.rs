use std::{
    collections::{HashSet, VecDeque},
    io,
    path::{Path, PathBuf},
    time::Duration,
};

use notify::{Event, EventKind, RecursiveMode, Watcher};
use notify_debouncer_full::{DebounceEventResult, Debouncer, RecommendedCache, new_debouncer_opt};
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
use tracing::{debug, warn};
use types::cluster_agent::{LogMetadata, LogMetadataFileInfo, LogMetadataWatchEvent};

use crate::log_metadata::{LOG_FILE_REGEX, LogMetadataImpl};

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
    /// K8s node name.
    node_name: String,
}

impl LogMetadataWatcher {
    /// Returns a new watcher and a channel to receive log metadata updates.
    pub fn new(
        directory: PathBuf,
        namespaces: Vec<String>,
        term_tx: BcSender<()>,
        node_name: String,
    ) -> (Self, Receiver<Result<LogMetadataWatchEvent, Status>>) {
        let (log_metadata_tx, log_metadata_rx) = channel(100);

        (
            Self {
                log_metadata_tx,
                term_tx,
                namespaces,
                directory,
                node_name,
            },
            log_metadata_rx,
        )
    }

    /// Starts watching the log directory for log updates. Blocks until a message is sent in the
    /// termination channel.
    pub async fn watch<T: Watcher>(&self, watcher_config: Option<notify::Config>) {
        let (internal_tx, internal_rx) = channel(10);
        let debouncer: Debouncer<T, RecommendedCache> =
            match self.setup_notify_watcher(internal_tx, watcher_config).await {
                Ok(debouncer) => debouncer,
                Err(watcher_error) => {
                    let _ = self.log_metadata_tx.send(Err(watcher_error.into())).await;
                    return;
                }
            };

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
    async fn setup_notify_watcher<T: Watcher>(
        &self,
        internal_tx: Sender<VecDeque<Result<LogMetadataWatchEvent, WatcherError>>>,
        watcher_config: Option<notify::Config>,
    ) -> Result<Debouncer<T, RecommendedCache>, WatcherError> {
        let runtime_handle = Handle::current();
        let namespaces = self.namespaces.clone();
        let node_name = self.node_name.clone();

        let mut debouncer = new_debouncer_opt(
            Duration::from_secs(2),
            None,
            move |result: DebounceEventResult| {
                runtime_handle.block_on(async {
                    let _ = internal_tx
                        .send(handle_debounced_events(result, &namespaces, &node_name))
                        .await;
                });
            },
            RecommendedCache::new(),
            watcher_config.unwrap_or_default(),
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
    async fn listen_for_changes<T: Watcher>(
        &self,
        mut internal_rx: Receiver<VecDeque<Result<LogMetadataWatchEvent, WatcherError>>>,
        mut debouncer: Debouncer<T, RecommendedCache>,
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
                                    debug!("Channel closed from client.");
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
                        let shutdown_status = Status::new(tonic::Code::Unavailable, "Server is shutting down");
                        let _ = self.log_metadata_tx.send(Err(shutdown_status)).await;
                        break;
                    }
            }
        }

        debug!("Stopping watcher..");
        debouncer.stop();
    }

    // In case of a new log file creation, it adds the path to the notify watcher in order to
    // receive updates for the file in the future. On removal, the path is removed from the watcher
    // accordingly.
    fn update_watcher<T: Watcher>(
        &self,
        watch_event: &LogMetadataWatchEvent,
        watcher: &mut Debouncer<T, RecommendedCache>,
    ) {
        let Some(event_type) = LogMetadataWatchEventType::from_str(&watch_event.r#type) else {
            return;
        };

        if !matches!(
            event_type,
            LogMetadataWatchEventType::Added | LogMetadataWatchEventType::Deleted
        ) {
            return;
        }

        let Some(file_path) = self.get_file_path(watch_event) else {
            return;
        };

        let watch_result = match event_type {
            // Methods watch and unwatch can fail on adding an existing path or on removing a
            // non-existing one. There are no specific actions needed in case this happens.
            LogMetadataWatchEventType::Added => {
                watcher.watch(&file_path, RecursiveMode::NonRecursive)
            }
            LogMetadataWatchEventType::Deleted => watcher.unwatch(&file_path),
            LogMetadataWatchEventType::Modified => return,
        };

        if let Err(error) = watch_result {
            debug!(
                "Failed to update watcher for event {} file {} with error {}",
                event_type.as_str(),
                file_path.to_string_lossy(),
                error
            );
        } else {
            debug!(
                "Successfully updated watcher for event {} and file {}",
                event_type.as_str(),
                file_path.to_string_lossy()
            );
        }
    }

    // Reconstruct the absolut file path from a LogMetadataWatchEvent.
    fn get_file_path(&self, watch_event: &LogMetadataWatchEvent) -> Option<PathBuf> {
        let file_metadata = watch_event.object.as_ref()?.spec.as_ref()?;
        let filename = format!(
            "{}_{}_{}-{}.log",
            file_metadata.pod_name,
            file_metadata.namespace,
            file_metadata.container_name,
            file_metadata.container_id
        );
        Some(self.directory.join(filename))
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
            let captures = LOG_FILE_REGEX.captures(&filename)?;

            if namespaces.is_empty()
                || namespaces.contains(&captures.name("namespace").unwrap().as_str().to_owned())
            {
                Some(directory.to_path_buf().join(file.file_name()))
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
    node_name: &str,
) -> VecDeque<Result<LogMetadataWatchEvent, WatcherError>> {
    let events = match debounced_event_result {
        Err(errors) => return errors.into_iter().map(|error| Err(error.into())).collect(),
        Ok(debounced_events) => debounced_events
            .into_iter()
            .filter(|debounced_event| {
                matches!(
                    debounced_event.kind,
                    EventKind::Create(_) | EventKind::Modify(_) | EventKind::Remove(_)
                )
            })
            .filter_map(|debounced_event| {
                transform_notify_event(&debounced_event.event, namespaces, node_name)
            })
            .collect(),
    };

    deduplicate_metadata_events(events)
}

// Deduplicates a list of metadata events by discarding the duplicate events which are oldest.
fn deduplicate_metadata_events(
    metadata_events: Vec<Result<LogMetadataWatchEvent, WatcherError>>,
) -> VecDeque<Result<LogMetadataWatchEvent, WatcherError>> {
    let mut deduped_events = VecDeque::new();
    let mut event_index = HashSet::new();

    for result in metadata_events.into_iter().rev() {
        match &result {
            Err(_) => deduped_events.push_front(result),
            Ok(event) => {
                if event_index.insert(event.clone()) {
                    deduped_events.push_front(result);
                }
            }
        }
    }

    deduped_events
}

// Transform a single Event to a LogMetadataWatchEvent. Fails in cases there is an IO error when
// trying to get the file metadata from the filesystem.
fn transform_notify_event(
    event: &Event,
    namespaces: &[String],
    node_name: &str,
) -> Option<Result<LogMetadataWatchEvent, WatcherError>> {
    let mut event_type = match event.kind {
        EventKind::Modify(_) => LogMetadataWatchEventType::Modified,
        EventKind::Create(_) => LogMetadataWatchEventType::Added,
        EventKind::Remove(_) => LogMetadataWatchEventType::Deleted,
        _ => return None,
    };

    let path = event.paths.first()?;

    let metadata_spec = LogMetadataImpl::get_log_metadata_spec(path, namespaces, node_name)?;
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
            file_info: Some(file_info.unwrap_or(LogMetadataFileInfo {
                size: 0,
                last_modified_at: None,
            })),
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
    #[cfg(not(target_os = "macos"))]
    use std::{
        fs::{File, remove_file, rename},
        io::Write,
    };

    use crate::log_metadata::test::create_test_file;

    use super::*;
    use notify::{PollWatcher, RecommendedWatcher};
    use serial_test::{parallel, serial};
    use tokio::{
        sync::{broadcast, mpsc::error::TryRecvError},
        task,
        time::sleep,
    };

    #[tokio::test]
    #[serial]
    async fn test_create_events_are_generated() {
        let file = create_test_file("pod-name_create-namespace_container-name-containerid", 4);
        let namespaces = vec!["create-namespace".into()];
        let (term_tx, _term_rx) = broadcast::channel(1);
        let logs_dir = file.path().parent().unwrap().to_owned();

        let (log_metadata_watcher, mut log_metadata_rx) = LogMetadataWatcher::new(
            logs_dir,
            namespaces,
            term_tx.clone(),
            "The node name".to_owned(),
        );

        task::spawn(async move {
            log_metadata_watcher
                .watch::<PollWatcher>(Some(
                    notify::Config::default().with_poll_interval(Duration::from_millis(100)),
                ))
                .await
        });

        // Wait until the watcher has started listening for changes
        while term_tx.receiver_count() != 2 {
            sleep(Duration::from_millis(50)).await;
        }

        // Create three files, one with an unrelated namespace.
        let _first_file = create_test_file(
            "pod-name_create-namespace_firstContainer-name-firstContainerid",
            4,
        );
        let _second_file = create_test_file(
            "pod-name_create-namespace_secondContainer-name-secondContainerid",
            4,
        );
        let _third_file = create_test_file("pod-name_wrongNamespace_container-name-containerid", 4);

        // Get the events and verify them. Events can appear in different order when using PollWatcher.
        for _i in 0..2 {
            let event = log_metadata_rx.recv().await.unwrap().unwrap();

            if event
                .object
                .as_ref()
                .unwrap()
                .id
                .starts_with("firstContainerid")
            {
                verify_event(
                    event,
                    "ADDED",
                    "firstContainerid",
                    "The node name",
                    "create-namespace",
                    "pod-name",
                    "firstContainer-name",
                    Some(4),
                );
            } else {
                verify_event(
                    event,
                    "ADDED",
                    "secondContainerid",
                    "The node name",
                    "create-namespace",
                    "pod-name",
                    "secondContainer-name",
                    Some(4),
                );
            }
        }

        // Ensure no more events are created.
        let result = log_metadata_rx.try_recv();
        assert!(matches!(result, Err(TryRecvError::Empty)));
    }

    #[tokio::test]
    #[parallel]
    async fn test_error_is_returned_on_unknown_directory() {
        let namespaces = vec!["namespace".into()];
        let (term_tx, _term_rx) = broadcast::channel(1);
        let logs_dir = PathBuf::from("/a/dir/that/doesnt/exist");

        let (log_metadata_watcher, mut log_metadata_rx) = LogMetadataWatcher::new(
            logs_dir,
            namespaces,
            term_tx.clone(),
            "The node name".to_owned(),
        );

        task::spawn(async move { log_metadata_watcher.watch::<RecommendedWatcher>(None).await });

        let result = log_metadata_rx.recv().await.unwrap();
        assert!(matches!(result, Err(_)));

        let status = result.unwrap_err();
        assert_eq!(status.code(), tonic::Code::NotFound);
        assert!(status.message().contains("/a/dir/that/doesnt/exist"));
    }

    #[tokio::test]
    #[parallel]
    async fn test_delete_events_are_generated() {
        let file = create_test_file("pod-name_delete-namespace_container-name-containerid", 4);
        let namespaces = vec!["delete-namespace".into()];
        let (term_tx, _term_rx) = broadcast::channel(1);
        let logs_dir = file.path().parent().unwrap().to_owned();

        let (log_metadata_watcher, mut log_metadata_rx) = LogMetadataWatcher::new(
            logs_dir,
            namespaces,
            term_tx.clone(),
            "The node name".to_owned(),
        );

        // File deletions return errors when using PollWatcher so we use RecommendedWatcher
        task::spawn(async move { log_metadata_watcher.watch::<RecommendedWatcher>(None).await });

        // Wait until the watcher has started listening for changes
        while term_tx.receiver_count() != 2 {
            sleep(Duration::from_millis(50)).await;
        }

        // Delete the file.
        let _ = file.close();

        // Receive the events and verify them.
        let event = log_metadata_rx.recv().await.unwrap().unwrap();
        verify_event(
            event,
            "DELETED",
            "containerid",
            "The node name",
            "delete-namespace",
            "pod-name",
            "container-name",
            None,
        );
    }

    #[tokio::test]
    #[cfg(not(target_os = "macos"))]
    #[parallel]
    async fn test_renamed_file_is_being_watched() {
        let file = create_test_file("pod-name_rename-namespace_container-name-containerid", 4);
        let namespaces = vec!["rename-namespace".into()];
        let (term_tx, _term_rx) = broadcast::channel(1);
        let logs_dir = file.path().parent().unwrap().to_owned();

        let (log_metadata_watcher, mut log_metadata_rx) = LogMetadataWatcher::new(
            logs_dir,
            namespaces,
            term_tx.clone(),
            "The node name".to_owned(),
        );

        // Start the watcher and give it some time to execute before creating events.
        task::spawn(async move { log_metadata_watcher.watch::<RecommendedWatcher>(None).await });

        // Wait until the watcher has started listening for changes
        while term_tx.receiver_count() != 2 {
            sleep(Duration::from_millis(50)).await;
        }

        // Rename the file.
        let mut new_path = file.path().to_owned();
        new_path.set_file_name("pod-name_rename-namespace_container-name-updatedcontainerid.log");
        let _ = rename(file.path(), &new_path);

        // Edit the file.
        let mut renamed_file = File::options()
            .write(true)
            .append(true)
            .open(&new_path)
            .unwrap();
        let _ = renamed_file.write_all(&vec![1; 5]);

        // Get the events and verify them. Events can appear in different order when using PollWatcher.
        for _i in 0..2 {
            let event = log_metadata_rx.recv().await.unwrap().unwrap();

            if event.r#type == "DELETED" {
                verify_event(
                    event,
                    "DELETED",
                    "containerid",
                    "The node name",
                    "rename-namespace",
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
                    "rename-namespace",
                    "pod-name",
                    "container-name",
                    Some(9),
                );
            }
        }

        let result = log_metadata_rx.try_recv();
        assert!(matches!(result, Err(TryRecvError::Empty)));

        // The renamed file won't be delted automatically when it gets out of scope.
        let _ = remove_file(&new_path);
    }

    #[tokio::test]
    #[parallel]
    async fn test_sends_unavailable_on_termination_signal() {
        // Prepare a valid logs directory using a temp file's parent.
        let file = create_test_file("pod-name_term-namespace_container-name-containerid", 1);
        let namespaces = vec!["term-namespace".into()];
        let (term_tx, _term_rx) = broadcast::channel(1);
        let logs_dir = file.path().parent().unwrap().to_owned();

        let (log_metadata_watcher, mut log_metadata_rx) = LogMetadataWatcher::new(
            logs_dir,
            namespaces,
            term_tx.clone(),
            "The node name".to_owned(),
        );

        // Start the watcher in the background.
        task::spawn(async move { log_metadata_watcher.watch::<RecommendedWatcher>(None).await });

        // Wait until the watcher has subscribed to the termination channel.
        while term_tx.receiver_count() != 2 {
            sleep(Duration::from_millis(50)).await;
        }

        // Send termination signal and expect an UNAVAILABLE status to be forwarded.
        let _ = term_tx.send(());
        let result = log_metadata_rx
            .recv()
            .await
            .expect("channel should yield a value");
        assert!(matches!(result, Err(_)));

        let status = result.unwrap_err();
        assert_eq!(status.code(), tonic::Code::Unavailable);
        assert_eq!(status.message(), "Server is shutting down");
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
            assert_eq!(
                event_file_info,
                Some(LogMetadataFileInfo {
                    size: 0,
                    last_modified_at: None,
                })
            );
        }
    }
}
