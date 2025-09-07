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

use std::{fs::File, path::PathBuf};

use tokio::sync::{broadcast::Sender as BcSender, mpsc::Sender};
use tonic::Status;
use types::cluster_agent::LogRecord;

use chrono::{DateTime, Utc};
use grep::{
    printer::JSONBuilder,
    searcher::{MmapChoice, SearcherBuilder},
};

use crate::{
    fs_watcher_error::FsWatcherError,
    util::{
        format::FileFormat,
        matcher::{LogFileRegexMatcher, PassThroughMatcher},
        offset::{find_nearest_offset_since, find_nearest_offset_until},
        reader::{ReverseLineReader, TermReader},
        writer::{process_output, CallbackWriter},
    },
};

pub async fn stream_backward(
    path: &PathBuf,
    start_time: Option<DateTime<Utc>>,
    stop_time: Option<DateTime<Utc>>,
    grep: Option<&str>,
    term_tx: BcSender<()>,
    sender: Sender<Result<LogRecord, Status>>,
) {
    let result = stream_backward_internal(path, start_time, stop_time, grep, &term_tx, &sender);

    if let Err(error) = result {
        let _ = sender.send(Err(error.into())).await;
    }
}

fn stream_backward_internal(
    path: &PathBuf,
    start_time: Option<DateTime<Utc>>,
    stop_time: Option<DateTime<Utc>>,
    grep: Option<&str>,
    term_tx: &BcSender<()>,
    sender: &Sender<Result<LogRecord, Status>>,
) -> Result<(), FsWatcherError> {
    // Open file
    let file = File::open(path)?;
    let max_offset = file.metadata()?.len();

    // Determine format based on filename
    let format = if path.to_string_lossy().ends_with("-json.log") {
        FileFormat::Docker
    } else {
        FileFormat::CRI
    };

    // Get start pos
    let start_pos: u64 = if let Some(ts) = start_time {
        if let Some(offset) = find_nearest_offset_since(&file, ts, 0, max_offset, format)? {
            offset.byte_offset
        } else {
            return Ok(()); // No records, exit early
        }
    } else {
        0
    };

    // Get end pos
    let end_pos: u64 = if let Some(ts) = stop_time {
        if let Some(offset) = find_nearest_offset_until(&file, ts, 0, max_offset, format)? {
            offset.byte_offset + offset.line_length
        } else {
            return Ok(()); // No records, exit early
        }
    } else {
        max_offset
    };

    // Wrap in term reader
    let term_reverse_reader = TermReader::new(
        ReverseLineReader::new(file, start_pos, end_pos).unwrap(),
        term_tx.subscribe(),
    );

    // Init searcher
    let mut searcher = SearcherBuilder::new()
        .line_number(false)
        .memory_map(MmapChoice::never())
        .multi_line(false)
        .heap_limit(Some(1024 * 1024)) // TODO: Make this configurable
        .build();

    // Init writer
    let writer_fn = |chunk: Vec<u8>| process_output(chunk, sender, format, term_tx.clone());
    let writer = CallbackWriter::new(writer_fn);

    // Init printer
    let mut printer = JSONBuilder::new().build(writer);

    // Remove leading and trailing whitespace
    let trimmed_grep = grep.map(str::trim).filter(|grep| !grep.is_empty());

    if let Some(grep) = trimmed_grep {
        let matcher = LogFileRegexMatcher::new(grep, format).unwrap();
        let sink = printer.sink(&matcher);
        let _ = searcher.search_reader(&matcher, term_reverse_reader, sink);
    } else {
        let matcher = PassThroughMatcher::new();
        let sink = printer.sink(&matcher);
        let _ = searcher.search_reader(&matcher, term_reverse_reader, sink);
    }

    Ok(())
}

#[cfg(test)]
mod test {
    use rstest::rstest;
    use std::{io::Write, sync::LazyLock};
    use tempfile::NamedTempFile;
    use tokio::sync::{broadcast, mpsc};

    use super::*;

    static TEST_FILE: LazyLock<NamedTempFile> = LazyLock::new(|| create_test_file());

    fn create_test_file() -> NamedTempFile {
        let lines = [
            "2024-10-01T05:40:46.960135302Z stdout F linenum 1",
            "2024-10-01T05:40:48.840712595Z stdout F linenum 2",
            "2024-10-01T05:40:50.075182095Z stdout F linenum 3",
            "2024-10-01T05:40:52.222363431Z stdout F linenum 4",
            "2024-10-01T05:40:54.911909292Z stdout F linenum 5",
            "2024-10-01T05:40:57.041413876Z stdout F linenum 6",
            "2024-10-01T05:40:58.197779961Z stdout F linenum 7",
            "2024-10-01T05:40:58.564018502Z stdout F linenum 8",
            "2024-10-01T05:40:58.612948127Z stdout F linenum 9",
            "2024-10-01T05:40:59.103901461Z stdout F linenum 10",
        ];

        let mut tmpfile = NamedTempFile::new().expect("Failed create");
        writeln!(tmpfile, "{}", lines.join("\n")).expect("Failed write");
        tmpfile
    }

    /// Compare captured binary output with expected lines
    /// Parses the binary output and compares the message fields with expected lines
    fn compare_lines(output: Vec<Result<LogRecord, Status>>, expected_lines: Vec<&'static str>) {
        // Parse the captured output
        let captured_lines: Vec<String> = output
            .into_iter()
            .map(|line| line.unwrap().message)
            .collect();

        // Compare against expected lines
        assert_eq!(
            captured_lines.len(),
            expected_lines.len(),
            "Number of lines doesn't match"
        );

        for (i, expected) in expected_lines.iter().enumerate() {
            assert_eq!(&captured_lines[i], expected, "Line {} doesn't match", i);
        }
    }

    // Test `start_time` arg
    #[tokio::test(flavor = "multi_thread")]
    #[rstest]
    #[case("", vec!["linenum 10", "linenum 9", "linenum 8", "linenum 7", "linenum 6", "linenum 5", "linenum 4", "linenum 3", "linenum 2", "linenum 1"])]
    #[case("2024-10-01T05:40:58.197779961Z", vec!["linenum 10", "linenum 9", "linenum 8", "linenum 7"])]
    #[case("2024-10-01T05:40:58.197779960Z", vec!["linenum 10", "linenum 9", "linenum 8", "linenum 7"])]
    #[case("2024-10-01T05:40:58.197779962Z", vec!["linenum 10", "linenum 9", "linenum 8"])]
    #[case("2024-10-01T05:40:59.103901461Z", vec!["linenum 10"])]
    #[case("2024-10-01T05:40:59.103901462Z", vec![])]
    async fn test_start_time(
        #[case] start_time_str: String,
        #[case] expected_lines: Vec<&'static str>,
    ) {
        let path = TEST_FILE.path().to_path_buf();

        // Parse start time if provided, otherwise use None
        let start_time = if start_time_str.is_empty() {
            None
        } else {
            Some(start_time_str.parse::<DateTime<Utc>>().unwrap())
        };

        // Create a channel for termination signal
        let (term_tx, _term_rx) = broadcast::channel(5);

        // Create output channel
        let (tx, mut rx) = mpsc::channel(100);

        stream_backward(&path, start_time, None, None, term_tx, tx).await;

        // Create a buffer to capture output
        let mut output = Vec::new();

        while let Some(record) = rx.recv().await {
            output.push(record);
        }

        // Compare output with expected lines
        compare_lines(output, expected_lines);
    }

    // Test `stop_time` arg
    #[tokio::test(flavor = "multi_thread")]
    #[rstest]
    #[case("", vec!["linenum 10", "linenum 9", "linenum 8", "linenum 7", "linenum 6", "linenum 5", "linenum 4", "linenum 3", "linenum 2", "linenum 1"])]
    #[case("2024-10-01T05:40:52.222363431Z", vec!["linenum 4", "linenum 3", "linenum 2", "linenum 1"])]
    #[case("2024-10-01T05:40:52.222363432Z", vec!["linenum 4", "linenum 3", "linenum 2", "linenum 1"])]
    #[case("2024-10-01T05:40:52.222363430Z", vec!["linenum 3", "linenum 2", "linenum 1"])]
    #[case("2024-10-01T05:40:46.960135302Z", vec!["linenum 1"])]
    #[case("2024-10-01T05:40:46.960135301Z", vec![])]
    async fn test_stop_time(
        #[case] stop_time_str: String,
        #[case] expected_lines: Vec<&'static str>,
    ) {
        let path = TEST_FILE.path().to_path_buf();

        // Parse start time if provided, otherwise use None
        let stop_time = if stop_time_str.is_empty() {
            None
        } else {
            Some(stop_time_str.parse::<DateTime<Utc>>().unwrap())
        };

        // Create a channel for termination signal
        let (term_tx, _term_rx) = broadcast::channel(5);

        // Create output channel
        let (tx, mut rx) = mpsc::channel(100);

        stream_backward(&path, None, stop_time, None, term_tx, tx).await;

        // Create a buffer to capture output
        let mut output = Vec::new();

        while let Some(record) = rx.recv().await {
            output.push(record);
        }

        // Compare output with expected lines
        compare_lines(output, expected_lines);
    }

    // Test `start_time` and `stop_time` args together
    #[tokio::test(flavor = "multi_thread")]
    #[rstest]
    #[case("", "", vec!["linenum 10", "linenum 9", "linenum 8", "linenum 7", "linenum 6", "linenum 5", "linenum 4", "linenum 3", "linenum 2", "linenum 1"])]
    #[case("2024-10-01T05:40:50.075182095Z", "2024-10-01T05:40:58.197779961Z", vec!["linenum 7", "linenum 6", "linenum 5", "linenum 4", "linenum 3"])]
    #[case("2024-10-01T05:40:50.075182094Z", "2024-10-01T05:40:58.197779961Z", vec!["linenum 7", "linenum 6", "linenum 5", "linenum 4", "linenum 3"])]
    #[case("2024-10-01T05:40:50.075182095Z", "2024-10-01T05:40:58.197779962Z", vec!["linenum 7", "linenum 6", "linenum 5", "linenum 4", "linenum 3"])]
    #[case("2024-10-01T05:40:50.075182096Z", "2024-10-01T05:40:58.197779961Z", vec!["linenum 7", "linenum 6", "linenum 5", "linenum 4"])]
    #[case("2024-10-01T05:40:50.075182095Z", "2024-10-01T05:40:58.197779960Z", vec!["linenum 6", "linenum 5", "linenum 4", "linenum 3"])]
    #[case("2024-10-01T05:40:50.075182096Z", "2024-10-01T05:40:58.197779960Z", vec!["linenum 6", "linenum 5", "linenum 4"])]
    async fn test_start_time_and_stop_time(
        #[case] start_time_str: String,
        #[case] stop_time_str: String,
        #[case] expected_lines: Vec<&'static str>,
    ) {
        let path = TEST_FILE.path().to_path_buf();

        // Parse start time if provided, otherwise use None
        let start_time = if start_time_str.is_empty() {
            None
        } else {
            Some(start_time_str.parse::<DateTime<Utc>>().unwrap())
        };

        // Parse stop time if provided, otherwise use None
        let stop_time = if stop_time_str.is_empty() {
            None
        } else {
            Some(stop_time_str.parse::<DateTime<Utc>>().unwrap())
        };

        // Create a channel for termination signal
        let (term_tx, _term_rx) = broadcast::channel(5);

        // Create output channel
        let (tx, mut rx) = mpsc::channel(100);

        stream_backward(&path, start_time, stop_time, None, term_tx, tx).await;

        // Create a buffer to capture output
        let mut output = Vec::new();

        while let Some(record) = rx.recv().await {
            output.push(record);
        }

        // Compare output with expected lines
        compare_lines(output, expected_lines);
    }

    #[tokio::test]
    async fn test_error_propagates_to_client() {
        let path = PathBuf::from("/a/dir/that/doesnt/exist");

        // Create a channel for termination signal
        let (term_tx, _term_rx) = broadcast::channel(5);

        // Create output channel
        let (tx, mut rx) = mpsc::channel(100);

        stream_backward(&path, None, None, None, term_tx, tx).await;

        let result = rx.recv().await.unwrap();
        assert!(matches!(result, Err(_)));

        let status = result.unwrap_err();
        assert_eq!(status.code(), tonic::Code::NotFound);
        assert!(status.message().contains("No such file or directory"));
    }
}
