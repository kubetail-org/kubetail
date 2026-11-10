// Copyright 2024 The Kubetail Authors
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
            let line = std::mem::take(&mut self.buffer);
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
    let json: serde_json::Value = match serde_json::from_slice(&chunk) {
        Ok(v) => v,
        Err(_) => {
            return;
        }
    };

    if let (Some(t), Some(data)) = (json["type"].as_str(), json["data"].as_object()) {
        if t != "match" {
            return;
        }

        // The printer encodes a line that is not UTF-8 under `bytes`, with no `text`.
        if let Some(lines) = data.get("lines") {
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
                                    is_final: true,
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
                            // `get` also rejects an offset inside a multi-byte character.
                            let Some(message) = rest.get(9..) else {
                                return;
                            };

                            let timestamp = match Timestamp::from_str(first) {
                                Ok(ts) => Some(ts),
                                Err(_) => return, // malformed timestamp
                            };

                            let record = LogRecord {
                                timestamp,
                                message: message.trim_end().to_string(),
                                is_final: rest.as_bytes()[7] == b'F',
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

#[cfg(test)]
mod test {
    #![allow(clippy::unwrap_used)]

    use rstest::rstest;
    use serde_json::json;
    use tokio::sync::mpsc;

    use super::*;

    // Each chunk is a match the JSON printer can emit for an attacker-written line.
    #[rstest]
    #[case::not_utf8(json!({"bytes": "//4K"}))]
    #[case::multi_byte_char_across_the_message_offset(
        json!({"text": "2024-10-01T05:40:46.960135302Z stdout \u{20ac}\u{20ac}\u{20ac}\n"})
    )]
    fn test_process_output_skips_a_malformed_line(#[case] lines: serde_json::Value) {
        let (tx, mut rx) = mpsc::channel(1);
        let chunk = json!({"type": "match", "data": {"lines": lines}}).to_string();

        process_output(
            CancellationToken::new(),
            chunk.into_bytes(),
            &tx,
            FileFormat::CRI,
        );

        assert!(rx.try_recv().is_err());
    }

    #[rstest]
    #[case::full("F", true)]
    #[case::partial("P", false)]
    #[tokio::test(flavor = "multi_thread")]
    async fn test_process_output_reads_the_cri_tag(#[case] tag: &str, #[case] is_final: bool) {
        let (tx, mut rx) = mpsc::channel(1);
        let text = format!("2024-10-01T05:40:46.960135302Z stdout {tag} message\n");
        let chunk = json!({"type": "match", "data": {"lines": {"text": text}}}).to_string();

        process_output(
            CancellationToken::new(),
            chunk.into_bytes(),
            &tx,
            FileFormat::CRI,
        );

        let record = rx.try_recv().unwrap().unwrap();
        assert_eq!(record.message, "message");
        assert_eq!(record.is_final, is_final);
    }
}
