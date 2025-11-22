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

use crate::util::format::FileFormat;
use crate::util::reader::TRUNCATION_SENTINEL;

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
                                let (message, original_size_bytes, is_truncated) =
                                    normalize_message(log_msg);

                                let record = LogRecord {
                                    timestamp: Some(
                                        Timestamp::from_str(time_str).unwrap_or_default(),
                                    ),
                                    message,
                                    original_size_bytes,
                                    is_truncated,
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

                            let (message, original_size_bytes, is_truncated) =
                                normalize_message(&rest[9..]);

                            let record = LogRecord {
                                timestamp: Some(Timestamp::from_str(first).unwrap()),
                                message,
                                original_size_bytes,
                                is_truncated,
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

// Returns decoded string, original_size_bytes, is_truncated
fn normalize_message(raw: &str) -> (String, u64, bool) {
    let trimmed = raw.trim_end_matches(|c| c == '\n' || c == '\r');
    let bytes = trimmed.as_bytes();
    const MARKER_LEN: usize = std::mem::size_of::<u64>() + 1; // 8-byte count + sentinel

    if bytes.len() >= MARKER_LEN && bytes.last().copied() == Some(TRUNCATION_SENTINEL) {
        let suffix_start = bytes.len() - MARKER_LEN;
        let (message_bytes, suffix) = bytes.split_at(suffix_start);
        let truncated_bytes =
            u64::from_be_bytes(suffix[..std::mem::size_of::<u64>()].try_into().unwrap());

        let message = String::from_utf8_lossy(message_bytes).to_string();
        (message, message_bytes.len() as u64 + truncated_bytes, true)
    } else {
        (trimmed.to_string(), trimmed.len() as u64, false)
    }
}

#[cfg(test)]
mod tests {
    use super::normalize_message;

    #[test]
    fn normalize_message_returns_truncated_count_and_strips_marker() {
        let mut raw_bytes = b"hello".to_vec();
        raw_bytes.extend_from_slice(&3u64.to_be_bytes());
        raw_bytes.push(super::TRUNCATION_SENTINEL);
        raw_bytes.push(b'\n');
        let raw = String::from_utf8(raw_bytes).unwrap();

        let (normalized, original_size_bytes, is_truncated) = normalize_message(&raw);
        assert_eq!(normalized, "hello");
        assert_eq!(original_size_bytes, 8);
        assert!(is_truncated);
    }

    #[test]
    fn normalize_message_trims_newlines() {
        let raw = "hello\n";
        assert_eq!(normalize_message(raw), ("hello".to_string(), 5, false));
    }

    #[test]
    fn normalize_message_ignores_embedded_sentinel() {
        let raw = format!("hel{}lo\n", char::from(super::TRUNCATION_SENTINEL));
        let (normalized, original_size_bytes, is_truncated) = normalize_message(&raw);
        assert_eq!(
            normalized,
            format!("hel{}lo", char::from(super::TRUNCATION_SENTINEL))
        );
        assert_eq!(original_size_bytes, 6);
        assert!(!is_truncated);
    }
}
