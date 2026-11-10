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
use tracing::{debug, warn};

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

/// Sends the record in a match from the JSON printer. A matched line that
/// does not parse is dropped with a warning.
pub fn process_output(
    ctx: CancellationToken,
    chunk: Vec<u8>,
    sender: &Sender<Result<LogRecord, Status>>,
    format: FileFormat,
) {
    let Ok(json) = serde_json::from_slice::<serde_json::Value>(&chunk) else {
        return;
    };
    if json["type"] != "match" {
        return;
    }

    // The printer sends a line that is not UTF-8 as base64 `bytes`, with no `text`.
    let record = json["data"]["lines"]["text"]
        .as_str()
        .ok_or("line is not valid UTF-8")
        .and_then(|text| parse_record(text, format));
    let record = match record {
        Ok(record) => record,
        Err(reason) => {
            warn!("Dropped log line: {reason}");
            return;
        }
    };

    let result = task::block_in_place(|| sender.blocking_send(Ok(record)));
    if result.is_err() {
        debug!("Channel closed from client.");
        ctx.cancel();
    }
}

/// Parses one line of a log file, or says why it cannot.
fn parse_record(text: &str, format: FileFormat) -> Result<LogRecord, &'static str> {
    match format {
        FileFormat::Docker => {
            let json: serde_json::Value =
                serde_json::from_str(text).map_err(|_| "malformed Docker JSON")?;
            let (Some(time_str), Some(message)) = (json["time"].as_str(), json["log"].as_str())
            else {
                return Err("Docker line has no time or log");
            };
            let timestamp =
                Timestamp::from_str(time_str).map_err(|_| "invalid Docker timestamp")?;
            Ok(LogRecord {
                timestamp: Some(timestamp),
                message: message.trim_end().to_string(),
                is_final: true,
            })
        }
        FileFormat::CRI => {
            // <timestamp> <stream> <P|F> <message>, the stream being stdout or stderr
            let (time_str, rest) = text.split_once(' ').ok_or("malformed CRI line")?;
            let message = rest.get(9..).ok_or("malformed CRI line")?;
            let timestamp = Timestamp::from_str(time_str).map_err(|_| "invalid CRI timestamp")?;
            Ok(LogRecord {
                timestamp: Some(timestamp),
                message: message.trim_end().to_string(),
                is_final: rest.as_bytes()[7] == b'F',
            })
        }
    }
}

#[cfg(test)]
mod tests {
    #![allow(clippy::unwrap_used)]

    use rstest::rstest;
    use tokio::sync::mpsc;

    use super::*;

    fn match_chunk(text: &str) -> Vec<u8> {
        serde_json::json!({"type": "match", "data": {"lines": {"text": text}}})
            .to_string()
            .into_bytes()
    }

    #[rstest]
    #[case(FileFormat::CRI, "2024-10-01T05:40:46.960135302Z stdout F hello\n")]
    #[case(
        FileFormat::Docker,
        r#"{"log":"hello\n","stream":"stdout","time":"2024-10-01T05:40:46.960135302Z"}"#
    )]
    fn test_parse_record(#[case] format: FileFormat, #[case] text: &str) {
        let record = parse_record(text, format).unwrap();
        assert_eq!(record.message, "hello");
        assert_eq!(
            record.timestamp.unwrap(),
            Timestamp::from_str("2024-10-01T05:40:46.960135302Z").unwrap()
        );
    }

    #[rstest]
    #[case("F", true)]
    #[case("P", false)]
    fn test_parse_record_reads_cri_partial_flag(#[case] flag: &str, #[case] expected: bool) {
        let text = format!("2025-01-02T03:04:05.123456789Z stdout {flag} message");
        let record = parse_record(&text, FileFormat::CRI).unwrap();
        assert_eq!(record.is_final, expected);
        assert_eq!(record.message, "message");
    }

    #[rstest]
    #[case(FileFormat::CRI, "no-space", "malformed CRI line")]
    #[case(FileFormat::CRI, "2024-10-01T05:40:46Z stdout", "malformed CRI line")]
    #[case(FileFormat::CRI, "not-a-time stdout F hello", "invalid CRI timestamp")]
    #[case(FileFormat::Docker, "not json", "malformed Docker JSON")]
    #[case(
        FileFormat::Docker,
        r#"{"log":"hello"}"#,
        "Docker line has no time or log"
    )]
    #[case(
        FileFormat::Docker,
        r#"{"log":"hello","time":"not-a-time"}"#,
        "invalid Docker timestamp"
    )]
    fn test_parse_record_rejects(
        #[case] format: FileFormat,
        #[case] text: &str,
        #[case] reason: &str,
    ) {
        assert_eq!(parse_record(text, format).unwrap_err(), reason);
    }

    #[tokio::test(flavor = "multi_thread")]
    async fn test_process_output_sends_record() {
        let (tx, mut rx) = mpsc::channel(1);
        let chunk = match_chunk("2024-10-01T05:40:46.960135302Z stdout F hello");

        process_output(CancellationToken::new(), chunk, &tx, FileFormat::CRI);

        assert_eq!(rx.recv().await.unwrap().unwrap().message, "hello");
    }

    #[tokio::test(flavor = "multi_thread")]
    async fn test_process_output_cancels_when_client_is_gone() {
        let (tx, rx) = mpsc::channel(1);
        drop(rx);
        let ctx = CancellationToken::new();
        let chunk = match_chunk("2024-10-01T05:40:46.960135302Z stdout F hello");

        process_output(ctx.clone(), chunk, &tx, FileFormat::CRI);

        assert!(ctx.is_cancelled());
    }

    #[rstest]
    #[case::not_json(b"not json".to_vec())]
    #[case::not_a_match(br#"{"type":"begin","data":{}}"#.to_vec())]
    #[case::unparseable_line(match_chunk("not-a-time stdout F hello"))]
    #[case::not_utf8(
        serde_json::json!({"type": "match", "data": {"lines": {"bytes": "/w=="}}})
            .to_string()
            .into_bytes()
    )]
    fn test_process_output_sends_nothing(#[case] chunk: Vec<u8>) {
        let (tx, mut rx) = mpsc::channel(1);

        process_output(CancellationToken::new(), chunk, &tx, FileFormat::CRI);

        assert!(rx.try_recv().is_err());
    }
}
