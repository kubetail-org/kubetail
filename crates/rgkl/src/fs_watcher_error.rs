use std::io;

use thiserror::Error;
use tonic::Status;

#[derive(Error, Debug)]
pub enum FsWatcherError {
    #[error("Error while accessing file: {0}")]
    Io(#[from] io::Error),

    #[error("Error while trying to watch: {0}")]
    Watch(#[from] notify::Error),

    #[error("Log directory not found: {0}")]
    DirNotFound(String),
}

impl From<FsWatcherError> for Status {
    fn from(watcher_error: FsWatcherError) -> Self {
        match watcher_error {
            FsWatcherError::Io(io_error) => io_error.into(),
            FsWatcherError::Watch(notify_error) => Self::from_error(Box::new(notify_error)),
            FsWatcherError::DirNotFound(_) => {
                Self::new(tonic::Code::NotFound, watcher_error.to_string())
            }
        }
    }
}
