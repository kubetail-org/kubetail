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
use crate::util::offset::{end_of_last_line, find_nearest_offset_since, find_nearest_offset_until};
use crate::util::reader::TermReader;
use crate::util::writer::{process_output, CallbackWriter};

/// The longest line a follow buffers. A longer one is skipped whole.
const MAX_LINE_LEN: usize = 1024 * 1024;

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
    log_file_reader: BufReader<File>,
    /// The start of a line whose newline has not been written yet.
    partial_line: Vec<u8>,
    /// The line being read has outgrown `partial_line`, so the rest of it is discarded.
    skip_line: bool,
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
) -> ResultOption<FsWatcher<impl FnMut(&[u8]) + use<'a>>, FsWatcherError> {
    let mut file = File::open(path)?;

    let max_offset = file.metadata()?.len();

    // Determine format based on filename
    let format = if path.to_string_lossy().ends_with("-json.log") {
        FileFormat::Docker
    } else {
        FileFormat::CRI
    };

    let follow = follow_from == FollowFrom::End
        || (follow_from == FollowFrom::Default && stop_time.is_none());

    // The follow reads on from here, so the search stops at the last whole line.
    let search_end = if follow {
        end_of_last_line(&file, max_offset)?
    } else {
        max_offset
    };

    // Get start pos
    let mut start_pos: u64 = 0;
    if follow_from == FollowFrom::End {
        start_pos = search_end;
    } else if let Some(start_time) = start_time {
        match find_nearest_offset_since(&file, start_time, 0, max_offset, format)? {
            Some(offset) => start_pos = offset.byte_offset,
            None if follow => start_pos = search_end,
            None => return Ok(None), // No records, exit early
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

    if follow {
        take_length = Some(search_end.saturating_sub(start_pos));
    }

    // Seek to starting position
    let _ = file.seek(SeekFrom::Start(start_pos));

    // Init reader
    let reader: Box<dyn Read> = if let Some(len) = take_length {
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
        .heap_limit(Some(1024 * 1024)) // TODO: Make this configurable
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
        let _ = searcher.search_reader(matcher, term_reader, sink);
    } else {
        let matcher = PassThroughMatcher::new();
        let sink = printer.sink(&matcher);
        let _ = searcher.search_reader(&matcher, term_reader, sink);
    }

    if ctx.is_cancelled() || !follow {
        return Ok(None);
    }

    let search_slice = move |input_str: &[u8]| {
        if let Some(matcher) = matcher.as_ref() {
            let sink = printer.sink(matcher);
            let _ = searcher.search_slice(matcher, input_str, sink);
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

    let mut reader = BufReader::new(File::open(path)?);
    reader.seek(SeekFrom::Start(search_end))?;

    Ok(Some(FsWatcher {
        search_callback: search_slice,
        log_file_reader: reader,
        partial_line: Vec::new(),
        skip_line: false,
        _notify_watcher: watcher,
        output_rx: notify_rx,
    }))
}

impl<F: FnMut(&[u8])> FsWatcher<F> {
    /// Searches each line the file has gained. A line still being written stays in
    /// `partial_line` until its newline arrives.
    fn search_new_lines(&mut self, ctx: &CancellationToken) -> io::Result<()> {
        while !ctx.is_cancelled() {
            let room = (MAX_LINE_LEN - self.partial_line.len()) as u64;
            let mut reader = (&mut self.log_file_reader).take(room);
            if reader.read_until(b'\n', &mut self.partial_line)? == 0 {
                break;
            }

            let complete = self.partial_line.ends_with(b"\n");
            if complete && !self.skip_line {
                (self.search_callback)(&self.partial_line);
            }
            if complete || self.partial_line.len() == MAX_LINE_LEN {
                self.skip_line = !complete;
                self.partial_line.clear();
            }
        }
        Ok(())
    }
}

/// Reads the lines the file gains and pushes them to the sender as `LogRecord`s: once for what
/// landed since the search, then on each `Modify` event. Loops until `ctx` is cancelled, the
/// receiver is dropped or the watch fails.
async fn listen_for_changes(
    ctx: CancellationToken,
    mut fs_watcher: FsWatcher<impl FnMut(&[u8])>,
    sender: Sender<Result<LogRecord, Status>>,
) {
    let mut modified = true;

    loop {
        if modified {
            if let Err(e) = fs_watcher.search_new_lines(&ctx) {
                let _ = sender.send(Err(Status::from_error(Box::new(e)))).await;
                return;
            }
        }

        let status = select! {
            // A dropped response stream also cancels `ctx`: a lost client is not a shutdown.
            biased;
            _ = sender.closed() => return,
            _ = ctx.cancelled() => Status::new(tonic::Code::Unavailable, "Server is shutting down"),
            ev = fs_watcher.output_rx.recv() => match ev {
                Some(Ok(event)) => {
                    modified = matches!(event.kind, EventKind::Modify(_));
                    continue;
                }
                Some(Err(e)) => FsWatcherError::Watch(e).into(),
                None => Status::new(tonic::Code::Unknown, "Notify channel closed."),
            },
        };

        let _ = sender.send(Err(status)).await;
        return;
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

    async fn wait_for_watcher(lifecycle_rx: &mut broadcast::Receiver<LifecycleEvent>) {
        let event = time::timeout(Duration::from_secs(10), lifecycle_rx.recv())
            .await
            .expect("Timed out waiting for the watcher to start");
        assert!(
            matches!(event, Ok(LifecycleEvent::WatcherStarted)),
            "stream ended before the watcher started"
        );
    }

    fn append(path: &Path, bytes: &[u8]) {
        let mut file = std::fs::OpenOptions::new().append(true).open(path).unwrap();
        file.write_all(bytes).unwrap();
        file.sync_all().unwrap();
    }

    async fn next_message(rx: &mut Receiver<Result<LogRecord, Status>>) -> String {
        time::timeout(Duration::from_secs(10), rx.recv())
            .await
            .expect("Timed out waiting for a record")
            .expect("stream ended early")
            .expect("stream sent an error")
            .message
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
                tx,
                Some(lifecycle_tx_clone),
            )
            .await;
        });

        // Create a buffer to capture output
        let mut output = Vec::new();

        // Wait until stream_forward goes into the wait loop to signal the writing thread to start.
        if is_follow {
            wait_for_watcher(&mut lifecycle_rx).await;

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

        wait_for_watcher(&mut lifecycle_rx).await;

        // Trigger termination and assert the forwarded shutdown error
        ctx.cancel();

        let last = rx.recv().await.expect("should forward shutdown error");
        let status = last.unwrap_err();
        assert_eq!(status.code(), tonic::Code::Unavailable);
        assert_eq!(status.message(), "Server is shutting down");

        // Channel should close after sending the shutdown error
        assert!(rx.recv().await.is_none());
    }

    /// Starts a follow of `path` and returns its records once the watcher is up.
    async fn follow(
        path: &Path,
        follow_from: FollowFrom,
    ) -> (task::JoinHandle<()>, Receiver<Result<LogRecord, Status>>) {
        let path = path.to_path_buf();
        let (tx, rx) = mpsc::channel(100);
        let (lifecycle_tx, mut lifecycle_rx) = broadcast::channel(1);

        let handle = task::spawn(async move {
            stream_forward_with_lifecyle_events(
                CancellationToken::new(),
                &path,
                None,
                None,
                None,
                follow_from,
                tx,
                Some(lifecycle_tx),
            )
            .await;
        });

        wait_for_watcher(&mut lifecycle_rx).await;
        (handle, rx)
    }

    #[tokio::test]
    async fn test_follow_ends_when_receiver_dropped() {
        let test_file = create_test_file();
        let (handle, rx) = follow(test_file.path(), FollowFrom::End).await;

        // The file stays idle, so only the dropped receiver can end the follow.
        drop(rx);

        time::timeout(Duration::from_secs(10), handle)
            .await
            .expect("follow did not end after the receiver was dropped")
            .unwrap();
    }

    #[tokio::test(flavor = "multi_thread")]
    async fn test_follow_holds_an_unfinished_line_and_skips_invalid_utf8() {
        let test_file = create_test_file();
        append(
            test_file.path(),
            b"2024-10-01T05:41:00.103901462Z stdout F written before",
        );

        let (_handle, mut rx) = follow(test_file.path(), FollowFrom::Default).await;
        for i in 1..=10 {
            assert_eq!(next_message(&mut rx).await, format!("linenum {i}"));
        }

        append(
            test_file.path(),
            b" the follow\n\
              2024-10-01T05:41:01.204901463Z stdout F \xff\xfe\n\
              2024-10-01T05:41:02.305901464Z stdout F whole\n\
              2024-10-01T05:41:03.406901465Z stdout F first half,",
        );
        assert_eq!(next_message(&mut rx).await, "written before the follow");
        assert_eq!(next_message(&mut rx).await, "whole");

        append(test_file.path(), b" second half\n");
        assert_eq!(next_message(&mut rx).await, "first half, second half");
    }

    #[tokio::test(flavor = "multi_thread")]
    async fn test_follow_waits_when_no_line_is_since_start_time() {
        let test_file = create_test_file();
        let path = test_file.path().to_path_buf();
        let (tx, mut rx) = mpsc::channel(100);
        let (lifecycle_tx, mut lifecycle_rx) = broadcast::channel(1);

        task::spawn(async move {
            stream_forward_with_lifecyle_events(
                CancellationToken::new(),
                &path,
                Some("2024-10-01T05:41:00Z".parse().unwrap()),
                None,
                None,
                FollowFrom::Default,
                tx,
                Some(lifecycle_tx),
            )
            .await;
        });

        wait_for_watcher(&mut lifecycle_rx).await;

        update_test_file(test_file.path()).unwrap();
        assert_eq!(next_message(&mut rx).await, "linenum 11");
    }

    /// Runs the follow loop over a watcher that reads `file` and has `event` waiting, and
    /// returns the status the loop ends with.
    async fn follow_failure(file: File, event: Option<Result<Event, Error>>) -> Status {
        let (notify_tx, notify_rx) = mpsc::channel(1);
        let (tx, mut rx) = mpsc::channel(1);

        let fs_watcher = FsWatcher {
            search_callback: |_: &[u8]| {},
            log_file_reader: BufReader::new(file),
            partial_line: Vec::new(),
            skip_line: false,
            output_rx: notify_rx,
            _notify_watcher: RecommendedWatcher::new(
                |_: Result<Event, Error>| {},
                Config::default(),
            )
            .unwrap(),
        };

        if let Some(event) = event {
            notify_tx.send(event).await.unwrap();
        }
        drop(notify_tx);

        time::timeout(
            Duration::from_secs(10),
            listen_for_changes(CancellationToken::new(), fs_watcher, tx),
        )
        .await
        .expect("follow did not end");

        rx.recv().await.unwrap().unwrap_err()
    }

    #[tokio::test]
    async fn test_follow_ends_with_the_watch_error() {
        let file = File::open(TEST_FILE.path()).unwrap();

        let status = follow_failure(file, Some(Err(Error::generic("watch failed")))).await;

        assert!(status.message().contains("watch failed"));
    }

    #[tokio::test]
    async fn test_follow_ends_when_the_notify_channel_closes() {
        let file = File::open(TEST_FILE.path()).unwrap();

        let status = follow_failure(file, None).await;

        assert_eq!(status.message(), "Notify channel closed.");
    }

    // Linux opens a directory but fails to read it.
    #[tokio::test]
    async fn test_follow_ends_with_the_read_error() {
        let dir = tempfile::tempdir().unwrap();
        let file = File::open(dir.path()).unwrap();

        let status = follow_failure(file, None).await;

        assert!(status.message().contains("directory"), "{status:?}");
    }

    #[tokio::test(flavor = "multi_thread")]
    async fn test_follow_from_end_starts_at_the_last_whole_line() {
        let test_file = create_test_file();
        append(
            test_file.path(),
            b"2024-10-01T05:41:00.103901462Z stdout F first half,",
        );

        let (_handle, mut rx) = follow(test_file.path(), FollowFrom::End).await;

        append(test_file.path(), b" second half\n");
        assert_eq!(next_message(&mut rx).await, "first half, second half");
    }

    #[tokio::test(flavor = "multi_thread")]
    async fn test_follow_does_not_parse_the_tail_of_an_overlong_line_as_a_record() {
        let test_file = create_test_file();
        let (_handle, mut rx) = follow(test_file.path(), FollowFrom::End).await;

        // The line's tail starts where the buffer fills and is shaped like a record.
        let mut lines = b"2024-10-01T05:41:00.103901462Z stdout F ".to_vec();
        lines.resize(MAX_LINE_LEN, b'a');
        lines.extend_from_slice(
            b"2020-01-01T00:00:00Z stdout F forged\n\
              2024-10-01T05:41:01.204901463Z stdout F after\n",
        );
        append(test_file.path(), &lines);

        assert_eq!(next_message(&mut rx).await, "after");
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
