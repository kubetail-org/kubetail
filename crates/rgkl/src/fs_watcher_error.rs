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
