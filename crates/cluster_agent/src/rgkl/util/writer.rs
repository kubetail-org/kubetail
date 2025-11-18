// Copyright 2024-2025 Andres Morey
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

use std::io::{self, Write};
use std::str::FromStr;

use prost_types::Timestamp;
use serde_json;
use tokio::sync::mpsc::Sender;
use tokio::task;
use tokio_util::sync::CancellationToken;
use tonic::Status;
use tracing::debug;

use types::cluster_agent::LogRecord;

use crate::rgkl::util::format::FileFormat;

/// A custom writer that calls a callback function whenever data is written.
pub struct CallbackWriter<F>
where
    F: Fn(Vec<u8>),
{
    callback: F,
    buffer: Vec<u8>,
}

impl<F> CallbackWriter<F>
where
    F: Fn(Vec<u8>),
{
    /// Creates a new CallbackWriter with an empty buffer.
    pub const fn new(callback: F) -> Self {
        Self {
            callback,
            buffer: Vec::new(),
        }
    }
}

impl<F> Write for CallbackWriter<F>
where
    F: Fn(Vec<u8>),
{
    fn write(&mut self, buf: &[u8]) -> io::Result<usize> {
        // Append new data to the internal buffer.
        self.buffer.extend_from_slice(buf);

        // Process complete lines in the buffer.
        while let Some(pos) = self.buffer.iter().position(|&b| b == b'\n') {
            // Drain the buffer up to and including the newline character.
            let line: Vec<u8> = self.buffer.drain(..=pos).collect();
            // Call the callback with the complete line.

            (self.callback)(line);
        }

        // Report that all bytes were "written".
        Ok(buf.len())
    }

    fn flush(&mut self) -> io::Result<()> {
        // If there's any leftover data in the buffer (e.g. a partial line),
        // process it as well.
        if !self.buffer.is_empty() {
            let line: Vec<u8> = self.buffer.drain(..).collect();
            (self.callback)(line);
        }

        Ok(())
    }
}

/// Function that processes the output.
pub fn process_output(
    ctx: CancellationToken,
    chunk: Vec<u8>,
    sender: &Sender<Result<LogRecord, Status>>,
    format: FileFormat,
) {
    // For example, convert to string and print.
    let json: serde_json::Value = serde_json::from_slice(&chunk).unwrap();
    if let (Some(t), Some(data)) = (json["type"].as_str(), json["data"].as_object()) {
        if t != "match" {
            return;
        }

        if let Some(lines) = data["lines"].as_object() {
            if let Some(text) = lines["text"].as_str() {
                match format {
                    FileFormat::Docker => {
                        // Parse as JSON (Docker format)
                        if let Ok(log_json) = serde_json::from_str::<serde_json::Value>(text) {
                            if let (Some(time_str), Some(log_msg)) =
                                (log_json["time"].as_str(), log_json["log"].as_str())
                            {
                                let record = LogRecord {
                                    timestamp: Some(
                                        Timestamp::from_str(time_str).unwrap_or_default(),
                                    ),
                                    message: log_msg.trim_end().to_string(),
                                };

                                let result =
                                    task::block_in_place(|| sender.blocking_send(Ok(record)));
                                if result.is_err() {
                                    debug!("Channel closed from client.");
                                    ctx.cancel();
                                }
                            }
                        }
                    }
                    FileFormat::CRI => {
                        // Original logic for CRI format
                        if let Some((first, rest)) = text.split_once(' ') {
                            // TODO: Should we return an error on parsing issues?
                            if rest.len() < 9 {
                                return;
                            }

                            let record = LogRecord {
                                timestamp: Some(Timestamp::from_str(first).unwrap()),
                                message: rest[9..].trim_end().to_string(),
                            };

                            let result = task::block_in_place(|| sender.blocking_send(Ok(record)));
                            if result.is_err() {
                                debug!("Channel closed from client.");
                                ctx.cancel();
                            }
                        }
                    }
                }
            }
        }
    }
}
