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

use std::fs::File;
use std::io::{BufRead, BufReader, Read, Seek, SeekFrom};
use std::path::PathBuf;

use chrono::{DateTime, Utc};
use grep::printer::JSONBuilder;
use grep::searcher::{MmapChoice, SearcherBuilder};
use notify::{Config, Error, Event, EventKind, RecommendedWatcher, RecursiveMode, Watcher};
use tokio::select;
use tokio::sync::broadcast;
use tokio::sync::mpsc::{self, Receiver, Sender};
use tokio_util::sync::CancellationToken;
use tonic::Status;
use types::cluster_agent::{FollowFrom, LogRecord};

use crate::fs_watcher_error::FsWatcherError;
use crate::util::format::FileFormat;
use crate::util::matcher::{LogFileRegexMatcher, PassThroughMatcher};
use crate::util::offset::{find_nearest_offset_since, find_nearest_offset_until};
use crate::util::reader::{LogTrimmerReader, TermReader};
use crate::util::writer::{process_output, CallbackWriter};

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

/// A watcher for file updates.
struct FsWatcher<F>
where
    F: FnMut(&[u8]),
{
    /// Performs the grep search, meant to be used on each new log line.
    search_callback: F,
    /// Reader to get log lines from.
    log_file_reader: BufReader<LogTrimmerReader<std::fs::File>>,
    /// Receives the events that come from notify.
    output_rx: Receiver<Result<Event, Error>>,
    /// Internal notify watcher.
    _notify_watcher: RecommendedWatcher,
}

pub async fn stream_forward(
    ctx: CancellationToken,
    path: &PathBuf,
    start_time: Option<DateTime<Utc>>,
    stop_time: Option<DateTime<Utc>>,
    grep: Option<&str>,
    follow_from: FollowFrom,
    truncate_at_bytes: u64,
    sender: Sender<Result<LogRecord, Status>>,
) {
    stream_forward_with_lifecyle_events(
        ctx,
        path,
        start_time,
        stop_time,
        grep,
        follow_from,
        truncate_at_bytes,
        sender,
        None,
    )
    .await
}

#[allow(clippy::too_many_arguments)]
async fn stream_forward_with_lifecyle_events(
    ctx: CancellationToken,
    path: &PathBuf,
    start_time: Option<DateTime<Utc>>,
    stop_time: Option<DateTime<Utc>>,
    grep: Option<&str>,
    follow_from: FollowFrom,
    truncate_at_bytes: u64,
    sender: Sender<Result<LogRecord, Status>>,
    lifecycle_tx: Option<broadcast::Sender<LifecycleEvent>>,
) {
    let result = setup_fs_watcher(
        ctx.clone(),
        path,
        start_time,
        stop_time,
        grep,
        follow_from,
        truncate_at_bytes,
        &sender,
    );

    emit_lifecycle(&lifecycle_tx, LifecycleEvent::WatcherStarted);

    match result {
        Err(fs_error) => {
            let _ = sender.send(Err(fs_error.into())).await;
        }
        Ok(None) => {}
        Ok(Some(watcher)) => listen_for_changes(ctx.clone(), watcher, sender.clone()).await,
    }
}

type ResultOption<T, E> = Result<Option<T>, E>;

fn setup_fs_watcher<'a>(
    ctx: CancellationToken,
    path: &PathBuf,
    start_time: Option<DateTime<Utc>>,
    stop_time: Option<DateTime<Utc>>,
    grep: Option<&'a str>,
    follow_from: FollowFrom,
    truncate_at_bytes: u64,
    sender: &'a Sender<Result<LogRecord, Status>>,
) -> ResultOption<FsWatcher<impl FnMut(&[u8]) + use<'a>>, FsWatcherError> {
    let mut file = File::open(path)?;

    let max_offset = file.metadata()?.len();

    // Determine format based on filename
    let format = if path.to_string_lossy().ends_with("-json.log") {
        FileFormat::Docker
    } else {
        FileFormat::CRI
    };

    // Get start pos
    let mut start_pos: u64 = 0;
    if follow_from == FollowFrom::End {
        // When following from the end, start at the end of the file
        start_pos = max_offset;
    } else if let Some(start_time) = start_time {
        if let Some(offset) = find_nearest_offset_since(&file, start_time, 0, max_offset, format)? {
            start_pos = offset.byte_offset;
        } else {
            return Ok(None); // No records, exit early
        }
    }

    // Calculate the length to take
    let mut take_length: Option<u64> = None;
    if follow_from != FollowFrom::End {
        if let Some(stop_time) = stop_time {
            if let Some(offset) =
                find_nearest_offset_until(&file, stop_time, start_pos, max_offset, format)?
            {
                take_length = Some(offset.byte_offset + offset.line_length - start_pos);
            } else {
                return Ok(None); // No records, exit early
            }
        }
    }

    // Seek to starting position
    file.seek(SeekFrom::Start(start_pos))?;

    // Init reader with optional tail length restriction
    let reader: Box<dyn Read> = match take_length {
        Some(len) => Box::new(file.take(len)),
        None => Box::new(file),
    };

    // Wrap with truncation reader
    let reader: Box<dyn Read> = match truncate_at_bytes {
        0 => reader,
        limit => Box::new(LogTrimmerReader::new(reader, format, limit)),
    };

    // Wrap with term reader
    let reader = TermReader::new(ctx.clone(), reader);

    // Init searcher
    let mut searcher = SearcherBuilder::new()
        .line_number(false)
        .memory_map(MmapChoice::never())
        .multi_line(false)
        .build();

    let ctx_copy = ctx.clone();
    let writer_fn = move |chunk: Vec<u8>| {
        process_output(ctx_copy.clone(), chunk, sender, format);
    };
    let writer = CallbackWriter::new(writer_fn);
    let mut printer = JSONBuilder::new().build(writer);

    // Remove leading and trailing whitespace
    let trimmed_grep = grep.map(str::trim).filter(|grep| !grep.is_empty());

    if let Some(grep) = trimmed_grep {
        let matcher = LogFileRegexMatcher::new(grep, format).unwrap();
        let sink = printer.sink(&matcher);
        let _ = searcher.search_reader(&matcher, reader, sink);
    } else {
        let matcher = PassThroughMatcher::new();
        let sink = printer.sink(&matcher);
        let _ = searcher.search_reader(&matcher, reader, sink);
    }

    if ctx.is_cancelled() {
        return Ok(None);
    }

    if take_length.is_some() {
        return Ok(None);
    }

    if follow_from == FollowFrom::Noop {
        return Ok(None);
    }

    let search_slice = move |input_str: &[u8]| {
        if let Some(grep) = trimmed_grep {
            let matcher = LogFileRegexMatcher::new(grep, format).unwrap();
            let sink = printer.sink(&matcher);
            let _ = searcher.search_slice(&matcher, input_str, sink);
        } else {
            let matcher = PassThroughMatcher::new();
            let sink = printer.sink(&matcher);
            let _ = searcher.search_slice(&matcher, input_str, sink);
        }
    };

    // Set up watcher
    let (notify_tx, notify_rx) = mpsc::channel(100);

    let mut watcher = RecommendedWatcher::new(
        move |result: Result<Event, Error>| {
            let _ = notify_tx.blocking_send(result);
        },
        Config::default(),
    )?;

    watcher.watch(path, RecursiveMode::NonRecursive)?;

    // Open file
    let mut reader = File::open(path)?;
    reader.seek(SeekFrom::End(0))?;

    // Wrap with truncation reader
    let reader = LogTrimmerReader::new(reader, format, truncate_at_bytes);

    // Wrap with buffered reader
    let reader = BufReader::new(reader);

    /*
    let mut reader = BufReader::new(File::open(path)?);
    reader.seek(SeekFrom::End(0))?;

    // Wrap with truncation reader
    let reader: Box<dyn Read> = match truncate_at_bytes {
        0 => Box::new(reader),
        limit => Box::new(LogTrimmerReader::new(reader, format, limit)),
    };
    */

    Ok(Some(FsWatcher {
        search_callback: search_slice,
        log_file_reader: reader,
        _notify_watcher: watcher,
        output_rx: notify_rx,
    }))
}

/// Listens for update Events from notify, process the new log lines to produce `LogRecord` events
/// and pushes them  to the sender. Loops until a signal is sent to the `term_tx` channel.
async fn listen_for_changes(
    ctx: CancellationToken,
    mut fs_watcher: FsWatcher<impl FnMut(&[u8])>,
    sender: Sender<Result<LogRecord, Status>>,
) {
    'outer: loop {
        select! {
            ev = fs_watcher.output_rx.recv() => {
                match ev {
                    Some(Ok(event)) => {
                        if let EventKind::Modify(_) = event.kind {
                            for line in (&mut fs_watcher.log_file_reader).lines() {
                                if ctx.is_cancelled() {
                                    break 'outer;
                                }

                                match line {
                                    Ok(l) => {
                                        (fs_watcher.search_callback)(l.as_bytes());
                                    },
                                    Err(e) => {
                                        let _ = sender.send(Err(Status::from_error(Box::new(e)))).await;
                                        return;
                                    }
                                }
                            }
                        }
                    },
                    Some(Err(e)) => {
                        let _ = sender.send(Err(Status::from(FsWatcherError::Watch(e)))).await;
                        return;
                    }
                    None => {
                        let _ = sender.send(Err(Status::new(tonic::Code::Unknown, "Notify channel closed."))).await;
                        return;
                    }
                }
            },
            _ = ctx.cancelled() => {
                // Send gRPC UNAVAILABLE error to indicate server shutdown
                let shutdown_status = Status::new(tonic::Code::Unavailable, "Server is shutting down");
                let _ = sender.send(Err(shutdown_status)).await;
                break 'outer;
            },
        }
    }
}

#[cfg(test)]
mod test {
    use std::{io::Write, path::Path, sync::LazyLock};

    use rstest::rstest;
    use tempfile::NamedTempFile;
    use tokio::task;

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

    fn update_test_file(path: &Path) -> std::io::Result<()> {
        let additional_lines = [
            "2024-10-01T05:41:00.103901462Z stdout F linenum 11",
            "2024-10-01T05:41:01.204901463Z stdout F linenum 12",
            "2024-10-01T05:41:02.305901464Z stdout F linenum 13",
        ];

        let mut file = std::fs::OpenOptions::new()
            .write(true)
            .append(true)
            .open(path)?;

        // Write the new lines
        writeln!(file, "{}", additional_lines.join("\n"))?;

        // Flush to ensure data is written
        file.sync_all()?;

        Ok(())
    }

    /// Compare captured binary output with expected lines
    /// Parses the binary output and compares the message fields with expected lines
    fn compare_lines(output: Vec<Result<LogRecord, Status>>, expected_lines: Vec<&'static str>) {
        // Parse the captured output, filtering out shutdown errors
        let captured_lines: Vec<String> = output
            .into_iter()
            .filter_map(|line| match line {
                Ok(record) => Some(record.message),
                Err(status)
                    if status.code() == tonic::Code::Unavailable
                        && status.message() == "Server is shutting down" =>
                {
                    None
                } // Filter out shutdown errors
                Err(_) => panic!("Unexpected error in test output"), // Other errors should still cause test failure
            })
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
    #[case("", vec!["linenum 1", "linenum 2", "linenum 3", "linenum 4", "linenum 5", "linenum 6", "linenum 7", "linenum 8", "linenum 9", "linenum 10"])]
    #[case("2024-10-01T05:40:46.960135302Z", vec!["linenum 1", "linenum 2", "linenum 3", "linenum 4", "linenum 5", "linenum 6", "linenum 7", "linenum 8", "linenum 9", "linenum 10"])]
    #[case("2024-10-01T05:40:58.564018502Z", vec!["linenum 8", "linenum 9", "linenum 10"])]
    #[case("2024-10-01T05:40:58.564018503Z", vec!["linenum 9", "linenum 10"])]
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

        // Create output channel
        let (tx, mut rx) = mpsc::channel(100);

        // Call run method
        stream_forward(
            CancellationToken::new(),
            &path,
            start_time,
            None,             // No stop time
            None,             // No grep filter
            FollowFrom::Noop, // Don't follow
            0,                // No truncation
            tx,
        )
        .await;

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
    #[case("", vec!["linenum 1", "linenum 2", "linenum 3", "linenum 4", "linenum 5", "linenum 6", "linenum 7", "linenum 8", "linenum 9", "linenum 10"])]
    #[case("2024-10-01T05:40:59.103901461Z", vec!["linenum 1", "linenum 2", "linenum 3", "linenum 4", "linenum 5", "linenum 6", "linenum 7", "linenum 8", "linenum 9", "linenum 10"])]
    #[case("2024-10-01T05:40:59.103901462Z", vec!["linenum 1", "linenum 2", "linenum 3", "linenum 4", "linenum 5", "linenum 6", "linenum 7", "linenum 8", "linenum 9", "linenum 10"])]
    #[case("2024-10-01T05:40:50.075182095Z", vec!["linenum 1", "linenum 2", "linenum 3"])]
    #[case("2024-10-01T05:40:50.075182096Z", vec!["linenum 1", "linenum 2", "linenum 3"])]
    #[case("2024-10-01T05:40:50.075182094Z", vec!["linenum 1", "linenum 2"])]
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

        // Create output channel
        let (tx, mut rx) = mpsc::channel(100);

        // Call run method
        stream_forward(
            CancellationToken::new(),
            &path,
            None, // No start time
            stop_time,
            None,             // No grep filter
            FollowFrom::Noop, // Don't follow
            0,                // No truncation
            tx,
        )
        .await;

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
    #[case("", "", vec!["linenum 1", "linenum 2", "linenum 3", "linenum 4", "linenum 5", "linenum 6", "linenum 7", "linenum 8", "linenum 9", "linenum 10"])]
    #[case("2024-10-01T05:40:46.960135302Z", "2024-10-01T05:40:59.103901461Z", vec!["linenum 1", "linenum 2", "linenum 3", "linenum 4", "linenum 5", "linenum 6", "linenum 7", "linenum 8", "linenum 9", "linenum 10"])]
    #[case("2024-10-01T05:40:46.960135301Z", "2024-10-01T05:40:59.103901461Z", vec!["linenum 1", "linenum 2", "linenum 3", "linenum 4", "linenum 5", "linenum 6", "linenum 7", "linenum 8", "linenum 9", "linenum 10"])]
    #[case("2024-10-01T05:40:46.960135302Z", "2024-10-01T05:40:59.103901462Z", vec!["linenum 1", "linenum 2", "linenum 3", "linenum 4", "linenum 5", "linenum 6", "linenum 7", "linenum 8", "linenum 9", "linenum 10"])]
    #[case("2024-10-01T05:40:46.960135303Z", "2024-10-01T05:40:59.103901461Z", vec!["linenum 2", "linenum 3", "linenum 4", "linenum 5", "linenum 6", "linenum 7", "linenum 8", "linenum 9", "linenum 10"])]
    #[case("2024-10-01T05:40:46.960135302Z", "2024-10-01T05:40:59.103901460Z", vec!["linenum 1", "linenum 2", "linenum 3", "linenum 4", "linenum 5", "linenum 6", "linenum 7", "linenum 8", "linenum 9"])]
    #[case("2024-10-01T05:40:46.960135303Z", "2024-10-01T05:40:59.103901460Z", vec!["linenum 2", "linenum 3", "linenum 4", "linenum 5", "linenum 6", "linenum 7", "linenum 8", "linenum 9"])]
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

        // Create output channel
        let (tx, mut rx) = mpsc::channel(100);

        // Call run method
        stream_forward(
            CancellationToken::new(),
            &path,
            start_time,
            stop_time,
            None,             // No grep filter
            FollowFrom::Noop, // Don't follow
            0,                // No truncation
            tx,
        )
        .await;

        // Create a buffer to capture output
        let mut output = Vec::new();

        while let Some(record) = rx.recv().await {
            output.push(record);
        }

        // Compare output with expected lines
        compare_lines(output, expected_lines);
    }

    // Test `follow-from` arg
    #[tokio::test(flavor = "multi_thread")]
    #[rstest]
    #[case("", FollowFrom::Noop, vec!["linenum 1", "linenum 2", "linenum 3", "linenum 4", "linenum 5", "linenum 6", "linenum 7", "linenum 8", "linenum 9", "linenum 10"])]
    #[case("", FollowFrom::Default, vec!["linenum 1", "linenum 2", "linenum 3", "linenum 4", "linenum 5", "linenum 6", "linenum 7", "linenum 8", "linenum 9", "linenum 10", "linenum 11", "linenum 12", "linenum 13"])]
    #[case("2024-10-01T05:40:58.612948127Z", FollowFrom::Default, vec!["linenum 9", "linenum 10", "linenum 11", "linenum 12", "linenum 13"])]
    #[case("2024-10-01T05:40:58.612948127Z", FollowFrom::Default, vec!["linenum 9", "linenum 10", "linenum 11", "linenum 12", "linenum 13"])]
    #[case("", FollowFrom::End, vec!["linenum 11", "linenum 12", "linenum 13"])]
    #[case("2024-10-01T05:40:58.612948127Z", FollowFrom::End, vec!["linenum 11", "linenum 12", "linenum 13"])]
    async fn test_follow_from(
        #[case] start_time_str: String,
        #[case] follow_from: FollowFrom,
        #[case] expected_lines: Vec<&'static str>,
    ) {
        let test_file = create_test_file();
        let path = test_file.path().to_path_buf();

        // Parse start time if provided, otherwise use None
        let start_time = if start_time_str.is_empty() {
            None
        } else {
            Some(start_time_str.parse::<DateTime<Utc>>().unwrap())
        };

        // Create a cancellation token
        let ctx = CancellationToken::new();

        // Create output channel
        let (tx, mut rx) = mpsc::channel(100);

        // Create lifecycle broadcast channel
        let (lifecycle_tx, mut lifecycle_rx) = broadcast::channel(1);

        // For follow, we need to update the file after starting the follow
        let is_follow = follow_from == FollowFrom::End || follow_from == FollowFrom::Default;

        let ctx_clone = ctx.clone();
        let lifecycle_tx_clone = lifecycle_tx.clone();

        task::spawn(async move {
            // Call run method
            stream_forward_with_lifecyle_events(
                ctx_clone,
                &path,
                start_time,
                None, // No stop time
                None, // No grep filter
                follow_from,
                0, // No truncation
                tx,
                Some(lifecycle_tx_clone),
            )
            .await;
        });

        // Wait until stream_forward goes into the wait loop to signal the writing thread to start.
        if is_follow {
            // Wait for WatcherStartedEvent
            while !matches!(
                lifecycle_rx.recv().await,
                Ok(LifecycleEvent::WatcherStarted)
            ) {}

            // Update the file with new lines
            update_test_file(test_file.path()).expect("Failed to update test file");

            // Stop watcher
            ctx.cancel()
        }

        // Create a buffer to capture output
        let mut output = Vec::new();

        while let Some(record) = rx.recv().await {
            output.push(record);
        }

        compare_lines(output, expected_lines);
    }

    #[tokio::test]
    async fn test_errors_are_propagated_to_client() {
        let path = PathBuf::from("/a/dir/that/doesnt/exist");

        // Create output channel
        let (tx, mut rx) = mpsc::channel(100);

        // Call run method
        stream_forward(
            CancellationToken::new(),
            &path,
            None,
            None,
            None,             // No grep filter
            FollowFrom::Noop, // Don't follow
            0,                // No truncation
            tx,
        )
        .await;

        let result = rx.recv().await.unwrap();
        assert!(matches!(result, Err(_)));

        let status = result.unwrap_err();
        assert_eq!(status.code(), tonic::Code::NotFound);
        assert!(status.message().contains("No such file or directory"));
    }

    #[tokio::test]
    async fn test_shutdown_error_sent_on_termination() {
        // Prepare a fresh temp file and paths
        let test_file = create_test_file();
        let path = test_file.path().to_path_buf();

        // Create context token
        let ctx = CancellationToken::new();

        // Create output channel
        let (tx, mut rx) = mpsc::channel(100);

        // Create lifecycle broadcast channel
        let (lifecycle_tx, mut lifecycle_rx) = broadcast::channel(1);

        // Start stream_forward in follow mode so it enters the listen loop
        let ctx_copy = ctx.clone();
        let lifecycle_tx_clone = lifecycle_tx.clone();

        task::spawn(async move {
            stream_forward_with_lifecyle_events(
                ctx_copy,
                &path,
                None,            // No start time
                None,            // No stop time
                None,            // No grep filter
                FollowFrom::End, // Enter listen loop immediately
                0,               // No truncation
                tx,
                Some(lifecycle_tx_clone),
            )
            .await;
        });

        // Wait for WatcherStartedEvent
        while !matches!(
            lifecycle_rx.recv().await,
            Ok(LifecycleEvent::WatcherStarted)
        ) {}

        // Trigger termination and assert the forwarded shutdown error
        ctx.cancel();

        let last = rx.recv().await.expect("should forward shutdown error");
        let status = last.unwrap_err();
        assert_eq!(status.code(), tonic::Code::Unavailable);
        assert_eq!(status.message(), "Server is shutting down");

        // Channel should close after sending the shutdown error
        assert!(rx.recv().await.is_none());
    }
}
