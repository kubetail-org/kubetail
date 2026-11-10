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

use std::fs::File;
use std::io::{self, BufRead, BufReader, Read, Seek, SeekFrom};
use std::path::PathBuf;

use chrono::{DateTime, Utc};
use grep::printer::JSONBuilder;
use grep::searcher::{MmapChoice, SearcherBuilder};
use notify::{Config, Error, Event, RecommendedWatcher, RecursiveMode, Watcher};
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
use crate::util::reader::TermReader;
use crate::util::writer::{process_output, CallbackWriter};
use crate::util::MAX_LINE_LEN;

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
    F: FnMut(&[u8]) -> io::Result<()>,
{
    /// Performs the grep search, meant to be used on each new log line.
    search_callback: F,
    /// Reader to get log lines from.
    log_file_reader: BufReader<File>,
    /// The start of a line the file does not yet end.
    partial_line: Vec<u8>,
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
    sender: Sender<Result<LogRecord, Status>>,
) {
    stream_forward_with_lifecyle_events(
        ctx,
        path,
        start_time,
        stop_time,
        grep,
        follow_from,
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
    sender: &'a Sender<Result<LogRecord, Status>>,
) -> ResultOption<FsWatcher<impl FnMut(&[u8]) -> io::Result<()> + use<'a>>, FsWatcherError> {
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
        match find_nearest_offset_since(&file, start_time, 0, max_offset, format)? {
            Some(offset) => start_pos = offset.byte_offset,
            // No records, exit early unless the stream goes on to follow
            None if follow_from == FollowFrom::Noop || stop_time.is_some() => return Ok(None),
            None => start_pos = max_offset,
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

    // A followed search ends after the last complete line, where the follow picks up.
    let follow_pos = if follow_from != FollowFrom::Noop && take_length.is_none() {
        let pos = last_line_end(&mut file, max_offset)?;
        start_pos = start_pos.min(pos);
        Some(pos)
    } else {
        None
    };
    let search_length = take_length.or(follow_pos.map(|pos| pos - start_pos));

    // Seek to starting position
    file.seek(SeekFrom::Start(start_pos))?;

    // Init reader
    let reader: Box<dyn Read> = if let Some(len) = search_length {
        Box::new(file.take(len))
    } else {
        Box::new(file)
    };

    // Wrap in term reader
    let term_reader = TermReader::new(ctx.clone(), reader);

    // Init searcher
    let mut searcher = SearcherBuilder::new()
        .line_number(false)
        .memory_map(MmapChoice::never())
        .multi_line(false)
        .heap_limit(Some(MAX_LINE_LEN))
        .build();

    let ctx_copy = ctx.clone();
    let writer_fn = move |chunk: Vec<u8>| {
        process_output(ctx_copy.clone(), chunk, sender, format);
    };
    let writer = CallbackWriter::new(writer_fn);
    let mut printer = JSONBuilder::new().build(writer);

    // Remove leading and trailing whitespace
    let trimmed_grep = grep.map(str::trim).filter(|grep| !grep.is_empty());
    let matcher = trimmed_grep
        .map(|grep| LogFileRegexMatcher::new(grep, format))
        .transpose()?;

    if let Some(matcher) = matcher.as_ref() {
        let sink = printer.sink(matcher);
        searcher.search_reader(matcher, term_reader, sink)?;
    } else {
        let matcher = PassThroughMatcher::new();
        let sink = printer.sink(&matcher);
        searcher.search_reader(&matcher, term_reader, sink)?;
    }

    if ctx.is_cancelled() {
        return Ok(None);
    }

    let Some(follow_pos) = follow_pos else {
        return Ok(None);
    };

    let search_slice = move |input_str: &[u8]| {
        if let Some(matcher) = matcher.as_ref() {
            let sink = printer.sink(matcher);
            searcher.search_slice(matcher, input_str, sink)
        } else {
            let matcher = PassThroughMatcher::new();
            let sink = printer.sink(&matcher);
            searcher.search_slice(&matcher, input_str, sink)
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

    let mut reader = BufReader::new(File::open(path)?);
    reader.seek(SeekFrom::Start(follow_pos))?;

    Ok(Some(FsWatcher {
        search_callback: search_slice,
        log_file_reader: reader,
        partial_line: Vec::new(),
        _notify_watcher: watcher,
        output_rx: notify_rx,
    }))
}

fn line_too_long() -> io::Error {
    io::Error::other("line exceeds the length limit")
}

/// The offset just past the last newline in the first `len` bytes of `file`.
/// Looks back no further than `MAX_LINE_LEN`.
fn last_line_end(file: &mut File, len: u64) -> io::Result<u64> {
    let mut buf = [0; 8192];
    let floor = len.saturating_sub(MAX_LINE_LEN as u64);
    let mut end = len;
    while end > floor {
        let start = end.saturating_sub(buf.len() as u64).max(floor);
        let chunk = &mut buf[..(end - start) as usize];
        file.seek(SeekFrom::Start(start))?;
        file.read_exact(chunk)?;
        if let Some(i) = chunk.iter().rposition(|&b| b == b'\n') {
            return Ok(start + i as u64 + 1);
        }
        end = start;
    }
    if floor > 0 {
        return Err(line_too_long());
    }
    Ok(0)
}

/// Searches each complete line left in `reader`. A line still being written
/// stays in `line` until its newline arrives, and may not pass `MAX_LINE_LEN`.
fn search_new_lines(
    ctx: &CancellationToken,
    reader: &mut impl BufRead,
    line: &mut Vec<u8>,
    mut search: impl FnMut(&[u8]) -> io::Result<()>,
) -> io::Result<()> {
    while !ctx.is_cancelled() {
        let room = (MAX_LINE_LEN - line.len()) as u64;
        reader.by_ref().take(room).read_until(b'\n', line)?;
        if !line.ends_with(b"\n") {
            if line.len() == MAX_LINE_LEN {
                return Err(line_too_long());
            }
            break;
        }
        search(line)?;
        line.clear();
    }
    Ok(())
}

/// Listens for update Events from notify, process the new log lines to produce `LogRecord` events
/// and pushes them to the sender. Loops until `ctx` is cancelled or an error is sent.
async fn listen_for_changes(
    ctx: CancellationToken,
    mut fs_watcher: FsWatcher<impl FnMut(&[u8]) -> io::Result<()>>,
    sender: Sender<Result<LogRecord, Status>>,
) {
    loop {
        // The first pass reads what was written while the watcher was set up.
        let result = search_new_lines(
            &ctx,
            &mut fs_watcher.log_file_reader,
            &mut fs_watcher.partial_line,
            &mut fs_watcher.search_callback,
        );
        if let Err(e) = result {
            let _ = sender.send(Err(FsWatcherError::from(e).into())).await;
            return;
        }

        select! {
            ev = fs_watcher.output_rx.recv() => {
                match ev {
                    Some(Ok(_)) => {},
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
                return;
            },
        }
    }
}

#[cfg(test)]
mod test {
    #![allow(clippy::unwrap_used)]

    use std::{io::Write, path::Path, sync::LazyLock, time::Duration};

    use rstest::rstest;
    use tempfile::NamedTempFile;
    use tokio::{task, time};

    use super::*;

    static TEST_FILE: LazyLock<NamedTempFile> = LazyLock::new(create_test_file);

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

        let mut file = std::fs::OpenOptions::new().append(true).open(path)?;

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
        #[values(None, Some("linenum"))] grep: Option<&'static str>,
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
                grep,
                follow_from,
                tx,
                Some(lifecycle_tx_clone),
            )
            .await;
        });

        // Create a buffer to capture output
        let mut output = Vec::new();

        // Wait until stream_forward goes into the wait loop to signal the writing thread to start.
        if is_follow {
            // Wait for WatcherStartedEvent
            while !matches!(
                lifecycle_rx.recv().await,
                Ok(LifecycleEvent::WatcherStarted)
            ) {}

            // Update the file with new lines
            update_test_file(test_file.path()).expect("Failed to update test file");

            // Cancelling stops the stream before it reads lines still on disk, so
            // receive every expected line first.
            time::timeout(Duration::from_secs(10), async {
                while output.len() < expected_lines.len() {
                    let record = rx.recv().await.expect("stream ended early");
                    output.push(record);
                }
            })
            .await
            .expect("Timed out waiting for followed lines");

            // Stop watcher
            ctx.cancel()
        }

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
            tx,
        )
        .await;

        let result = rx.recv().await.unwrap();
        assert!(result.is_err());

        let status = result.unwrap_err();
        assert_eq!(status.code(), tonic::Code::NotFound);
        assert!(status.message().contains("No such file or directory"));
    }

    #[tokio::test]
    #[rstest]
    async fn test_search_error_propagates_to_client(#[values(None, Some("x"))] grep: Option<&str>) {
        // A line longer than the searcher's 1 MiB heap limit.
        let mut test_file = NamedTempFile::new().unwrap();
        let message = "x".repeat(2 * 1024 * 1024);
        writeln!(
            test_file,
            "2024-10-01T05:40:46.960135302Z stdout F {message}"
        )
        .unwrap();
        let path = test_file.path().to_path_buf();
        let (tx, mut rx) = mpsc::channel(100);

        stream_forward(
            CancellationToken::new(),
            &path,
            None,
            None,
            grep,
            FollowFrom::Noop,
            tx,
        )
        .await;

        let status = rx.recv().await.unwrap().unwrap_err();
        assert!(status.message().contains("allocation limit"));
        assert!(rx.recv().await.is_none());
    }

    /// Follows a file that holds `initial`, appends `appended` once the
    /// watcher runs, and returns the first item the stream sends.
    async fn follow_and_append(
        initial: &[u8],
        appended: &[u8],
        start_time: Option<DateTime<Utc>>,
        follow_from: FollowFrom,
    ) -> Result<LogRecord, Status> {
        let mut test_file = NamedTempFile::new().unwrap();
        test_file.write_all(initial).unwrap();
        let path = test_file.path().to_path_buf();
        let (tx, mut rx) = mpsc::channel(100);
        let (lifecycle_tx, mut lifecycle_rx) = broadcast::channel(1);

        task::spawn(async move {
            stream_forward_with_lifecyle_events(
                CancellationToken::new(),
                &path,
                start_time,
                None,
                None,
                follow_from,
                tx,
                Some(lifecycle_tx),
            )
            .await;
        });

        while !matches!(
            lifecycle_rx.recv().await,
            Ok(LifecycleEvent::WatcherStarted)
        ) {}

        test_file.write_all(appended).unwrap();

        time::timeout(Duration::from_secs(10), rx.recv())
            .await
            .expect("Timed out waiting for the stream")
            .unwrap()
    }

    #[tokio::test(flavor = "multi_thread")]
    async fn test_follow_drops_line_that_is_not_utf8() {
        let appended = b"\xff\n2024-10-01T05:40:46.960135302Z stdout F hello\n";
        let record = follow_and_append(b"", appended, None, FollowFrom::Default).await;
        assert_eq!(record.unwrap().message, "hello");
    }

    #[tokio::test(flavor = "multi_thread")]
    #[rstest]
    async fn test_follow_finishes_line_cut_short_by_the_search(
        #[values(FollowFrom::Default, FollowFrom::End)] follow_from: FollowFrom,
    ) {
        let initial = b"2024-10-01T05:40:46.960135302Z stdout F hel";
        let record = follow_and_append(initial, b"lo\n", None, follow_from).await;
        assert_eq!(record.unwrap().message, "hello");
    }

    #[tokio::test(flavor = "multi_thread")]
    async fn test_follow_starts_when_start_time_is_past_every_record() {
        let initial = b"2024-10-01T05:40:46.960135302Z stdout F old\n";
        let appended = b"2024-10-03T05:40:46.960135302Z stdout F new\n";
        let start_time = "2024-10-02T00:00:00Z".parse().unwrap();
        let record =
            follow_and_append(initial, appended, Some(start_time), FollowFrom::Default).await;
        assert_eq!(record.unwrap().message, "new");
    }

    #[tokio::test(flavor = "multi_thread")]
    async fn test_follow_error_propagates_to_client() {
        let appended = vec![b'x'; MAX_LINE_LEN];
        let status = follow_and_append(b"", &appended, None, FollowFrom::Default).await;
        assert!(status.unwrap_err().message().contains("length limit"));
    }

    #[rstest]
    #[case(b"", 0)]
    #[case(b"ab", 0)]
    #[case(b"a\nb", 2)]
    #[case(b"a\nb\n", 4)]
    fn test_last_line_end(#[case] content: &[u8], #[case] expected: u64) {
        // The padding puts the newline in an earlier chunk than the end.
        for padding in [0, 10_000] {
            let mut test_file = NamedTempFile::new().unwrap();
            test_file.write_all(content).unwrap();
            test_file.write_all(&vec![b'x'; padding]).unwrap();
            let len = (content.len() + padding) as u64;

            assert_eq!(
                last_line_end(test_file.as_file_mut(), len).unwrap(),
                expected
            );
        }
    }

    #[test]
    fn test_last_line_end_rejects_line_over_the_limit() {
        let mut test_file = NamedTempFile::new().unwrap();
        let content = vec![b'x'; MAX_LINE_LEN + 1];
        test_file.write_all(&content).unwrap();

        let result = last_line_end(test_file.as_file_mut(), content.len() as u64);

        assert!(result.is_err());
    }

    #[test]
    fn test_search_new_lines_waits_for_the_newline() {
        let mut test_file = NamedTempFile::new().unwrap();
        let mut reader = BufReader::new(test_file.reopen().unwrap());
        let ctx = CancellationToken::new();
        let mut partial_line = Vec::new();
        let mut lines = Vec::new();
        let mut search = |line: &[u8]| {
            lines.push(line.to_vec());
            Ok(())
        };

        test_file.write_all(b"one\ntw").unwrap();
        search_new_lines(&ctx, &mut reader, &mut partial_line, &mut search).unwrap();
        test_file.write_all(b"o\n").unwrap();
        search_new_lines(&ctx, &mut reader, &mut partial_line, &mut search).unwrap();

        assert_eq!(lines, [b"one\n", b"two\n"]);
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

    #[tokio::test]
    async fn test_invalid_grep_returns_invalid_argument() {
        let test_file = create_test_file();
        let path = test_file.path().to_path_buf();
        let ctx = CancellationToken::new();
        let (tx, mut rx) = mpsc::channel(1);

        stream_forward(
            ctx,
            &path,
            None,
            None,
            Some("(?=.*settings)"),
            FollowFrom::Noop,
            tx,
        )
        .await;

        let status = rx.recv().await.unwrap().unwrap_err();
        assert_eq!(status.code(), tonic::Code::InvalidArgument);
        assert!(status.message().contains("Invalid grep pattern"));
    }
}
