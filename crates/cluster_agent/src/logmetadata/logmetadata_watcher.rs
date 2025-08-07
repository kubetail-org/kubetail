use core::fmt;
use std::{
    path::{Path, PathBuf},
    time::Duration,
};

use notify::{
    Event, EventKind, RecommendedWatcher, RecursiveMode,
    event::{ModifyKind, RenameMode},
};
use notify_debouncer_full::{DebounceEventResult, Debouncer, RecommendedCache, new_debouncer};
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
use types::cluster_agent::{LogMetadata, LogMetadataWatchEvent};

use crate::logmetadata::{LOG_FILE_REGEX, LogMetadataImpl};

#[derive(Debug)]
pub struct LogMetadataWatcher {
    log_metadata_tx: Sender<Result<LogMetadataWatchEvent, Status>>,
    term_tx: BcSender<()>,
    namespaces: Vec<String>,
    directory: PathBuf,
}

impl LogMetadataWatcher {
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

    pub async fn watch(&self) -> Result<(), Box<Status>> {
        let (internal_tx, mut intenral_rx) = channel(10);
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
        )
        .map_err(|error| Box::new(Status::new(tonic::Code::Unknown, format!("{error:?}"))))?;

        let paths_to_add = find_log_files(&self.directory, &self.namespaces).await?;

        for path in paths_to_add {
            let _ = debouncer.watch(&path, notify::RecursiveMode::NonRecursive);
        }
        debouncer
            .watch(&self.directory, notify::RecursiveMode::NonRecursive)
            .map_err(|error| {
                Box::new(Status::new(
                    tonic::Code::NotFound,
                    format!(
                        "Could not watch directory: {:?} {:?}",
                        error.kind, error.paths
                    ),
                ))
            })?;

        let mut term_rx = self.term_tx.subscribe();

        'outer: loop {
            select! {
                metadata_events = intenral_rx.recv() => {
                    if let Some(metadata_events) = metadata_events {
                        for metadata_event in metadata_events {
                            if self.log_metadata_tx.send(metadata_event.clone().map_err(|error| *error)).await.is_err() {
                                    println!("Channel closed from client.");
                                    break 'outer;
                            }

                            if let Ok(metadata_event) = metadata_event {
                                self.update_watcher(metadata_event, &mut debouncer);
                            }
                        }
                    } else {
                        println!("Internal channel closed!");
                        break;
                    }
                }
                _ = term_rx.recv() => {
                        println!("Finished");
                        break;
                    }
            }
        }

        println!("Stopping watcher");
        debouncer.stop();

        Ok(())
    }

    fn update_watcher(
        &self,
        watch_event: LogMetadataWatchEvent,
        watcher: &mut Debouncer<RecommendedWatcher, RecommendedCache>,
    ) {
        match LogMetadataWatchEventType::from_str(&watch_event.r#type) {
            Some(LogMetadataWatchEventType::Added) => {
                let _ = watcher.watch(self.get_file_path(watch_event), RecursiveMode::NonRecursive);
            }
            Some(LogMetadataWatchEventType::Deleted) => {
                let _ = watcher.unwatch(self.get_file_path(watch_event));
            }
            _ => (),
        }
    }

    fn get_file_path(&self, watch_event: LogMetadataWatchEvent) -> PathBuf {
        let file_metadata = watch_event.object.unwrap().spec.unwrap();
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

async fn find_log_files(directory: &Path, namespaces: &[String]) -> Result<Vec<PathBuf>, Status> {
    if !directory.is_dir() {
        return Err(Status::new(
            tonic::Code::NotFound,
            format!("log directory not found: {}", directory.to_string_lossy()),
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

fn handle_debounced_events(
    debounced_event_result: DebounceEventResult,
    namespaces: &[String],
) -> Vec<Result<LogMetadataWatchEvent, Box<Status>>> {
    if let Err(errors) = debounced_event_result {
        return errors
            .into_iter()
            .map(|error| {
                Err(Box::new(Status::new(
                    tonic::Code::Unknown,
                    format!("{error:?}"),
                )))
            })
            .collect();
    }

    debounced_event_result
        .unwrap()
        .into_iter()
        .filter(|debounced_event| {
            matches!(
                debounced_event.kind,
                EventKind::Create(_) | EventKind::Modify(_) | EventKind::Remove(_)
            )
        })
        .flat_map(|debounced_event| transform_notify_event(&debounced_event.event, namespaces))
        .collect()
}

fn transform_notify_event(
    event: &Event,
    namespaces: &[String],
) -> Vec<Result<LogMetadataWatchEvent, Box<Status>>> {
    let mut result = Vec::new();

    if let EventKind::Modify(ModifyKind::Name(rename_mode)) = event.kind {
        match rename_mode {
            RenameMode::Both => {
                push_watch_event(
                    &mut result,
                    LogMetadataWatchEventType::Deleted,
                    event.paths.first(),
                    namespaces,
                );
                push_watch_event(
                    &mut result,
                    LogMetadataWatchEventType::Added,
                    event.paths.get(1),
                    namespaces,
                );
            }
            RenameMode::From => push_watch_event(
                &mut result,
                LogMetadataWatchEventType::Deleted,
                event.paths.first(),
                namespaces,
            ),
            RenameMode::To => push_watch_event(
                &mut result,
                LogMetadataWatchEventType::Added,
                event.paths.first(),
                namespaces,
            ),
            _ => {
                return result;
            }
        }
    } else {
        let event_type = match event.kind {
            EventKind::Modify(_) => LogMetadataWatchEventType::Modified,
            EventKind::Create(_) => LogMetadataWatchEventType::Added,
            EventKind::Remove(_) => LogMetadataWatchEventType::Deleted,
            _ => return result,
        };

        push_watch_event(&mut result, event_type, event.paths.first(), namespaces);
    }

    result
}

#[derive(Debug)]
enum LogMetadataWatchEventType {
    Added,
    Modified,
    Deleted,
}

impl fmt::Display for LogMetadataWatchEventType {
    fn fmt(&self, f: &mut fmt::Formatter) -> fmt::Result {
        fmt::Debug::fmt(self, f)
    }
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

fn push_watch_event(
    events: &mut Vec<Result<LogMetadataWatchEvent, Box<Status>>>,
    mut event_type: LogMetadataWatchEventType,
    path: Option<&PathBuf>,
    namespaces: &[String],
) {
    let path = path.unwrap();

    let Some(metadata_spec) = LogMetadataImpl::get_log_metadata_spec(path, namespaces) else {
        return;
    };

    let file_info = LogMetadataImpl::get_file_info(path);

    if let Err(ref status) = file_info {
        if status.code() == tonic::Code::NotFound {
            event_type = LogMetadataWatchEventType::Deleted;
        } else {
            events.push(Err(status.to_owned()));
            return;
        }
    }

    let watch_event = LogMetadataWatchEvent {
        r#type: event_type.as_str().to_owned(),
        object: Some(LogMetadata {
            id: metadata_spec.container_id.clone(),
            spec: Some(metadata_spec),
            file_info: file_info.ok(),
        }),
    };

    events.push(Ok(watch_event));
}

#[cfg(test)]
mod test {
    use crate::logmetadata::test::create_test_file;

    use super::*;
    use tokio::{
        sync::{broadcast, mpsc::error::TryRecvError},
        task,
        time::sleep,
    };

    #[tokio::test]
    async fn test_create_events_are_generated() {
        let file = create_test_file("pod-name_namespace_container-name-containerid", 4);
        let namespaces = vec!["namespace".into()];
        let (term_tx, _term_rx) = broadcast::channel(1);
        let logs_dir = file.path().parent().unwrap().to_owned();

        let (log_metadata_watcher, mut log_metadata_rx) =
            LogMetadataWatcher::new(logs_dir, namespaces, term_tx.clone());

        task::spawn(async move { log_metadata_watcher.watch().await });

        sleep(Duration::from_millis(100)).await;

        let _first_file =
            create_test_file("pod-name_namespace_firstContainer-name-firstContainerid", 4);
        let _second_file = create_test_file(
            "pod-name_namespace_secondContainer-name-secondContainerid",
            4,
        );
        let _third_file = create_test_file("pod-name_wrongNamespace_container-name-containerid", 4);

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

        let result = log_metadata_rx.try_recv();

        assert!(matches!(result, Err(TryRecvError::Empty)));
    }

    #[tokio::test]
    async fn test_delete_events_are_generated() {
        let file = create_test_file("pod-name_namespace_container-name-containerid", 4);
        let namespaces = vec!["namespace".into()];
        let (term_tx, _term_rx) = broadcast::channel(1);
        let logs_dir = file.path().parent().unwrap().to_owned();

        let (log_metadata_watcher, mut log_metadata_rx) =
            LogMetadataWatcher::new(logs_dir, namespaces, term_tx.clone());

        task::spawn(async move { log_metadata_watcher.watch().await });

        sleep(Duration::from_millis(100)).await;

        let _ = file.close();

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
