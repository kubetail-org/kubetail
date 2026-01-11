// Copyright 2024-2026 The Kubetail Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

use std::collections::{HashSet, VecDeque};
use std::io;
use std::path::{Path, PathBuf};
use std::time::Duration;

use notify::{Event, EventKind, RecursiveMode, Watcher};
use notify_debouncer_full::{DebounceEventResult, Debouncer, RecommendedCache, new_debouncer_opt};
use thiserror::Error;
use tokio::fs::read_dir;
use tokio::runtime::Handle;
use tokio::select;
use tokio::sync::broadcast;
use tokio::sync::mpsc::{Receiver, Sender, channel};
use tokio_stream::{StreamExt, wrappers::ReadDirStream};
use tokio_util::sync::CancellationToken;
use tonic::Status;
use tracing::{debug, warn};
use types::cluster_agent::{LogMetadata, LogMetadataFileInfo, LogMetadataWatchEvent};

use crate::log_metadata::{LOG_FILE_REGEX, LogMetadataImpl};

/// Lifecycle events emitted by stream_forward
#[derive(Debug, Clone)]
pub enum LifecycleEvent {
    WatcherStarted,
}

/// Helper: best-effort lifecycle emission.
/// If there is no lifecycle sender, or receiver lagged, we ignore the error.
fn emit_lifecycle(tx: &Option<broadcast::Sender<LifecycleEvent>>, event: LifecycleEvent) {
    if let Some(tx) = tx {
        // broadcast::Sender::send is synchronous and non-blocking.
        let _ = tx.send(event);
    }
}

/// Uses notify crate internally to provide notifications of file updates.
#[derive(Debug)]
pub struct LogMetadataWatcher {
    /// Context token to receive a termination signal and end the watch loop.
    ctx: CancellationToken,
    /// Internal channel to send log metadata updates.
    log_metadata_tx: Sender<Result<LogMetadataWatchEvent, Status>>,
    /// K8s namespaces to watch for.
    namespaces: Vec<String>,
    /// Directory to watch for updates.
    directory: PathBuf,
    /// K8s node name.
    node_name: String,
    /// Lifecycle event channel
    lifecycle_tx: Option<broadcast::Sender<LifecycleEvent>>,
}

impl LogMetadataWatcher {
    /// Returns a new watcher and a channel to receive log metadata updates.
    pub fn new(
        ctx: CancellationToken,
        directory: PathBuf,
        namespaces: Vec<String>,
        node_name: String,
    ) -> (Self, Receiver<Result<LogMetadataWatchEvent, Status>>) {
        let (log_metadata_tx, log_metadata_rx) = channel(100);

        (
            Self {
                ctx,
                log_metadata_tx,
                namespaces,
                directory,
                node_name,
                lifecycle_tx: None,
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

        emit_lifecycle(&self.lifecycle_tx, LifecycleEvent::WatcherStarted);

        self.listen_for_changes(internal_rx, debouncer).await;
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

    /// Blocks and listens for notify fs changes until either `ctx` is cancelled or
    /// `log_metadata_tx` is closed.
    ///
    /// # Arguments
    ///
    /// * `internal_rx` - Receiver of filesystem updates.
    /// * `debouncer` - The notify filesystem watcher.
    async fn listen_for_changes<T: Watcher>(
        &self,
        mut internal_rx: Receiver<VecDeque<Result<LogMetadataWatchEvent, WatcherError>>>,
        mut debouncer: Debouncer<T, RecommendedCache>,
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
                _ = self.ctx.cancelled() => {
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

    // Reconstruct the absolute file path from a LogMetadataWatchEvent.
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
    use std::fs::{File, remove_file};
    use std::io::Write;

    #[cfg(not(target_os = "macos"))]
    use std::fs::rename;

    use notify::{PollWatcher, RecommendedWatcher};
    use serial_test::{parallel, serial};
    use tokio::sync::{broadcast, mpsc::error::TryRecvError};
    use tokio::task;

    use crate::log_metadata::test::create_test_file;

    use super::*;

    #[tokio::test]
    #[serial]
    async fn test_create_events_are_generated() {
        let file = create_test_file("pod-name_create-namespace_container-name-containerid", 4);
        let namespaces = vec!["create-namespace".into()];
        let logs_dir = file.path().parent().unwrap().to_owned();
        let ctx = CancellationToken::new();

        let (mut log_metadata_watcher, mut log_metadata_rx) = LogMetadataWatcher::new(
            ctx.clone(),
            logs_dir,
            namespaces,
            "The node name".to_owned(),
        );

        // Create lifecycle broadcast channel
        let (lifecycle_tx, mut lifecycle_rx) = broadcast::channel(1);
        log_metadata_watcher.lifecycle_tx = Some(lifecycle_tx);

        task::spawn(async move {
            log_metadata_watcher
                .watch::<PollWatcher>(Some(
                    notify::Config::default().with_poll_interval(Duration::from_millis(100)),
                ))
                .await
        });

        // Wait for WatcherStartedEvent
        while !matches!(
            lifecycle_rx.recv().await,
            Ok(LifecycleEvent::WatcherStarted)
        ) {}

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

        // Kill
        ctx.cancel();

        // Ensure no more events are created.
        let result = log_metadata_rx.try_recv();
        assert!(matches!(result, Err(TryRecvError::Empty)));
    }

    #[tokio::test]
    #[parallel]
    async fn test_error_is_returned_on_unknown_directory() {
        let namespaces = vec!["namespace".into()];
        let logs_dir = PathBuf::from("/a/dir/that/doesnt/exist");

        let (log_metadata_watcher, mut log_metadata_rx) = LogMetadataWatcher::new(
            CancellationToken::new(),
            logs_dir,
            namespaces,
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
        let logs_dir = file.path().parent().unwrap().to_owned();
        let ctx = CancellationToken::new();

        let (mut log_metadata_watcher, mut log_metadata_rx) = LogMetadataWatcher::new(
            ctx.clone(),
            logs_dir,
            namespaces,
            "The node name".to_owned(),
        );

        // Create lifecycle broadcast channel
        let (lifecycle_tx, mut lifecycle_rx) = broadcast::channel(1);
        log_metadata_watcher.lifecycle_tx = Some(lifecycle_tx);

        // File deletions return errors when using PollWatcher so we use RecommendedWatcher
        task::spawn(async move { log_metadata_watcher.watch::<RecommendedWatcher>(None).await });

        // Wait for WatcherStartedEvent
        while !matches!(
            lifecycle_rx.recv().await,
            Ok(LifecycleEvent::WatcherStarted)
        ) {}

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
        let logs_dir = file.path().parent().unwrap().to_owned();
        let ctx = CancellationToken::new();

        let (mut log_metadata_watcher, mut log_metadata_rx) = LogMetadataWatcher::new(
            ctx.clone(),
            logs_dir,
            namespaces,
            "The node name".to_owned(),
        );

        // Create lifecycle broadcast channel
        let (lifecycle_tx, mut lifecycle_rx) = broadcast::channel(1);
        log_metadata_watcher.lifecycle_tx = Some(lifecycle_tx);

        // Start the watcher and give it some time to execute before creating events.
        task::spawn(async move { log_metadata_watcher.watch::<RecommendedWatcher>(None).await });

        // Wait for WatcherStartedEvent
        while !matches!(
            lifecycle_rx.recv().await,
            Ok(LifecycleEvent::WatcherStarted)
        ) {}

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

        // Kill
        ctx.cancel();

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
        let logs_dir = file.path().parent().unwrap().to_owned();
        let ctx = CancellationToken::new();

        let (mut log_metadata_watcher, mut log_metadata_rx) = LogMetadataWatcher::new(
            ctx.clone(),
            logs_dir,
            namespaces,
            "The node name".to_owned(),
        );

        // Create lifecycle broadcast channel
        let (lifecycle_tx, mut lifecycle_rx) = broadcast::channel(1);
        log_metadata_watcher.lifecycle_tx = Some(lifecycle_tx);

        // Start the watcher in the background.
        task::spawn(async move { log_metadata_watcher.watch::<RecommendedWatcher>(None).await });

        // Wait for WatcherStartedEvent
        while !matches!(
            lifecycle_rx.recv().await,
            Ok(LifecycleEvent::WatcherStarted)
        ) {}

        // Send termination signal and expect an UNAVAILABLE status to be forwarded.
        ctx.cancel();

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

    #[tokio::test]
    #[cfg(not(target_os = "macos"))]
    #[parallel]
    async fn test_newly_created_file_receives_modify_events() {
        let temp_dir = tempfile::tempdir().expect("Failed to create temp dir");
        let namespaces = vec!["modify-namespace".into()];
        let logs_dir = temp_dir.path().to_owned();
        let ctx = CancellationToken::new();

        let (mut log_metadata_watcher, mut log_metadata_rx) = LogMetadataWatcher::new(
            ctx.clone(),
            logs_dir.clone(),
            namespaces,
            "The node name".to_owned(),
        );

        let (lifecycle_tx, mut lifecycle_rx) = broadcast::channel(1);
        log_metadata_watcher.lifecycle_tx = Some(lifecycle_tx);

        task::spawn(async move { log_metadata_watcher.watch::<RecommendedWatcher>(None).await });

        while !matches!(
            lifecycle_rx.recv().await,
            Ok(LifecycleEvent::WatcherStarted)
        ) {}

        let file_path =
            logs_dir.join("pod-name_modify-namespace_container-name-newcontainerid.log");
        let mut file = File::create(&file_path).expect("Failed to create file");
        file.write_all(&vec![0; 4])
            .expect("Failed to write initial data");
        file.flush().expect("Failed to flush");

        let first_event = tokio::time::timeout(Duration::from_secs(5), log_metadata_rx.recv())
            .await
            .expect("Timeout waiting for ADDED event")
            .expect("Channel closed")
            .expect("Error receiving event");

        assert_eq!(first_event.r#type, "ADDED");
        let added_size = first_event
            .object
            .as_ref()
            .unwrap()
            .file_info
            .as_ref()
            .unwrap()
            .size;

        file.write_all(&vec![1; 3])
            .expect("Failed to write additional data");
        file.flush().expect("Failed to flush");
        drop(file);

        let second_event = tokio::time::timeout(Duration::from_secs(5), log_metadata_rx.recv())
            .await
            .expect("Timeout waiting for MODIFIED event")
            .expect("Channel closed")
            .expect("Error receiving event");

        assert_eq!(second_event.r#type, "MODIFIED");
        let modified_size = second_event
            .object
            .as_ref()
            .unwrap()
            .file_info
            .as_ref()
            .unwrap()
            .size;

        assert_eq!(modified_size, 7, "Final size should be 4 + 3 = 7 bytes");
        assert!(added_size <= 7, "Added size should be <= 7");

        ctx.cancel();
        let _ = remove_file(&file_path);
    }

    #[tokio::test]
    #[cfg(not(target_os = "macos"))]
    #[parallel]
    async fn test_multiple_updates_to_newly_created_file() {
        let temp_dir = tempfile::tempdir().expect("Failed to create temp dir");
        let namespaces = vec!["multi-modify-namespace".into()];
        let logs_dir = temp_dir.path().to_owned();
        let ctx = CancellationToken::new();

        let (mut log_metadata_watcher, mut log_metadata_rx) = LogMetadataWatcher::new(
            ctx.clone(),
            logs_dir.clone(),
            namespaces,
            "The node name".to_owned(),
        );

        let (lifecycle_tx, mut lifecycle_rx) = broadcast::channel(1);
        log_metadata_watcher.lifecycle_tx = Some(lifecycle_tx);

        task::spawn(async move { log_metadata_watcher.watch::<RecommendedWatcher>(None).await });

        while !matches!(
            lifecycle_rx.recv().await,
            Ok(LifecycleEvent::WatcherStarted)
        ) {}

        let file_path = logs_dir.join("pod-name_multi-modify-namespace_container-name-multiid.log");
        let mut file = File::create(&file_path).expect("Failed to create file");
        file.write_all(&vec![0; 2])
            .expect("Failed to write initial data");
        file.flush().expect("Failed to flush");

        let added_event = tokio::time::timeout(Duration::from_secs(5), log_metadata_rx.recv())
            .await
            .expect("Timeout waiting for ADDED event")
            .expect("Channel closed")
            .expect("Error receiving event");

        assert_eq!(added_event.r#type, "ADDED");

        // Perform multiple modifications with delays exceeding debounce window (2 seconds)
        for i in 1..=3 {
            file.write_all(&vec![i; 2]).expect("Failed to write data");
            file.flush().expect("Failed to flush");

            // Wait for the MODIFIED event after each write
            let modified_event =
                tokio::time::timeout(Duration::from_secs(5), log_metadata_rx.recv())
                    .await
                    .expect("Timeout waiting for MODIFIED event")
                    .expect("Channel closed")
                    .expect("Error receiving event");

            assert_eq!(modified_event.r#type, "MODIFIED");

            let expected_size = 2 + (i as i64 * 2);
            let file_size = modified_event
                .object
                .as_ref()
                .unwrap()
                .file_info
                .as_ref()
                .unwrap()
                .size;
            assert_eq!(
                file_size, expected_size,
                "File size after modification {} should be {}",
                i, expected_size
            );
        }

        drop(file);

        ctx.cancel();
        let _ = remove_file(&file_path);
    }

    #[tokio::test]
    #[cfg(not(target_os = "macos"))]
    #[parallel]
    async fn test_create_and_immediate_modify_before_watcher_starts() {
        let temp_dir = tempfile::tempdir().expect("Failed to create temp dir");
        let namespaces = vec!["immediate-namespace".into()];
        let logs_dir = temp_dir.path().to_owned();

        let file_path =
            logs_dir.join("pod-name_immediate-namespace_container-name-immediateid.log");
        let mut file = File::create(&file_path).expect("Failed to create file");
        file.write_all(&vec![0; 5])
            .expect("Failed to write initial data");
        file.flush().expect("Failed to flush");
        drop(file);

        let ctx = CancellationToken::new();

        let (mut log_metadata_watcher, mut log_metadata_rx) = LogMetadataWatcher::new(
            ctx.clone(),
            logs_dir.clone(),
            namespaces,
            "The node name".to_owned(),
        );

        let (lifecycle_tx, mut lifecycle_rx) = broadcast::channel(1);
        log_metadata_watcher.lifecycle_tx = Some(lifecycle_tx);

        task::spawn(async move { log_metadata_watcher.watch::<RecommendedWatcher>(None).await });

        while !matches!(
            lifecycle_rx.recv().await,
            Ok(LifecycleEvent::WatcherStarted)
        ) {}

        let mut file = File::options()
            .write(true)
            .append(true)
            .open(&file_path)
            .expect("Failed to open file");
        file.write_all(&vec![1; 3]).expect("Failed to write data");
        file.flush().expect("Failed to flush");
        drop(file);

        let event = tokio::time::timeout(Duration::from_secs(5), log_metadata_rx.recv())
            .await
            .expect("Timeout waiting for event")
            .expect("Channel closed")
            .expect("Error receiving event");

        verify_event(
            event,
            "MODIFIED",
            "immediateid",
            "The node name",
            "immediate-namespace",
            "pod-name",
            "container-name",
            Some(8),
        );

        let result = tokio::time::timeout(Duration::from_millis(100), log_metadata_rx.recv()).await;
        assert!(
            result.is_err() || matches!(result, Ok(None)),
            "Should not have more events"
        );

        ctx.cancel();
        let _ = remove_file(&file_path);
    }

    #[tokio::test]
    #[parallel]
    async fn test_created_file_with_wrong_namespace_not_watched() {
        let temp_dir = tempfile::tempdir().expect("Failed to create temp dir");
        let namespaces = vec!["correct-namespace".into()];
        let logs_dir = temp_dir.path().to_owned();
        let ctx = CancellationToken::new();

        let (mut log_metadata_watcher, mut log_metadata_rx) = LogMetadataWatcher::new(
            ctx.clone(),
            logs_dir.clone(),
            namespaces,
            "The node name".to_owned(),
        );

        let (lifecycle_tx, mut lifecycle_rx) = broadcast::channel(1);
        log_metadata_watcher.lifecycle_tx = Some(lifecycle_tx);

        task::spawn(async move {
            log_metadata_watcher
                .watch::<PollWatcher>(Some(
                    notify::Config::default().with_poll_interval(Duration::from_millis(100)),
                ))
                .await
        });

        while !matches!(
            lifecycle_rx.recv().await,
            Ok(LifecycleEvent::WatcherStarted)
        ) {}

        let wrong_file_path = logs_dir.join("pod-name_wrong-namespace_container-name-wrongid.log");
        let mut wrong_file = File::create(&wrong_file_path).expect("Failed to create file");
        wrong_file
            .write_all(&vec![0; 4])
            .expect("Failed to write data");
        wrong_file.flush().expect("Failed to flush");

        let correct_file_path =
            logs_dir.join("pod-name_correct-namespace_container-name-correctid.log");
        let mut correct_file = File::create(&correct_file_path).expect("Failed to create file");
        correct_file
            .write_all(&vec![0; 4])
            .expect("Failed to write data");
        correct_file.flush().expect("Failed to flush");

        let added_event = tokio::time::timeout(Duration::from_secs(5), log_metadata_rx.recv())
            .await
            .expect("Timeout waiting for ADDED event")
            .expect("Channel closed")
            .expect("Error receiving event");

        assert_eq!(added_event.r#type, "ADDED");
        let namespace = &added_event
            .object
            .as_ref()
            .unwrap()
            .spec
            .as_ref()
            .unwrap()
            .namespace;
        assert_eq!(namespace, "correct-namespace");

        wrong_file
            .write_all(&vec![1; 2])
            .expect("Failed to write to wrong file");
        wrong_file.flush().expect("Failed to flush");
        correct_file
            .write_all(&vec![1; 2])
            .expect("Failed to write to correct file");
        correct_file.flush().expect("Failed to flush");

        drop(wrong_file);
        drop(correct_file);

        let modified_event = tokio::time::timeout(Duration::from_secs(5), log_metadata_rx.recv())
            .await
            .expect("Timeout waiting for MODIFIED event")
            .expect("Channel closed")
            .expect("Error receiving event");

        assert_eq!(modified_event.r#type, "MODIFIED");
        let namespace = &modified_event
            .object
            .as_ref()
            .unwrap()
            .spec
            .as_ref()
            .unwrap()
            .namespace;
        assert_eq!(namespace, "correct-namespace");

        let result = tokio::time::timeout(Duration::from_millis(100), log_metadata_rx.recv()).await;
        assert!(
            result.is_err() || matches!(result, Ok(None)),
            "Should not have more events"
        );

        ctx.cancel();
        let _ = remove_file(&wrong_file_path);
        let _ = remove_file(&correct_file_path);
    }
}
