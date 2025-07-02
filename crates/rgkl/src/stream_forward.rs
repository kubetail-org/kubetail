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

use std::{
    fs::File,
    io::{BufRead, BufReader, Read, Seek, SeekFrom},
    path::PathBuf,
};

use chrono::{DateTime, Utc};
use grep::{
    printer::JSONBuilder,
    searcher::{MmapChoice, SearcherBuilder},
};
use notify::{Config, Error, Event, EventKind, RecommendedWatcher, RecursiveMode, Watcher};
use tokio::{
    select,
    sync::{
        broadcast::Sender as BcSender,
        mpsc::{self, Sender},
    },
};
use tonic::Status;
use types::cluster_agent::{FollowFrom, LogRecord};

use tokio::sync::broadcast::error::TryRecvError::{Closed, Empty, Lagged};

use crate::util::{
    format::FileFormat,
    matcher::{LogFileRegexMatcher, PassThroughMatcher},
    offset::{find_nearest_offset_since, find_nearest_offset_until},
    reader::TermReader,
    writer::{process_output, CallbackWriter},
};

/// Entrypoint
pub async fn stream_forward(
    path: &PathBuf,
    start_time: Option<DateTime<Utc>>,
    stop_time: Option<DateTime<Utc>>,
    grep: Option<&str>,
    follow_from: FollowFrom,
    term_tx: BcSender<()>,
    sender: Sender<Result<LogRecord, Status>>,
) -> Result<(), Box<dyn std::error::Error>> {
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
            return Ok(()); // No records, exit early
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
                return Ok(()); // No records, exit early
            }
        }
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
    let term_reader = TermReader::new(reader, term_tx.subscribe());

    // Init searcher
    let mut searcher = SearcherBuilder::new()
        .line_number(false)
        .memory_map(MmapChoice::never())
        .build();

    // Init writer
    let writer_fn = |chunk: Vec<u8>| process_output(chunk, &sender, format, term_tx.clone());
    let writer = CallbackWriter::new(writer_fn);

    // Init printer
    let mut printer = JSONBuilder::new().build(writer);

    // Remove leading and trailing whitespace
    let trimmed_grep = grep.map(str::trim).filter(|grep| grep.is_empty());

    if let Some(grep) = trimmed_grep {
        let matcher = LogFileRegexMatcher::new(grep, format).unwrap();
        let sink = printer.sink(&matcher);
        let _ = searcher.search_reader(&matcher, term_reader, sink);
    } else {
        let matcher = PassThroughMatcher::new();
        let sink = printer.sink(&matcher);
        let _ = searcher.search_reader(&matcher, term_reader, sink);
    }

    let mut term_rx = term_tx.subscribe();
    // Exit here if termination signal has been received
    match term_rx.try_recv() {
        Ok(_) | Err(Closed | Lagged(_)) => {
            return Ok(()); // Exit cleanly
        }
        Err(Empty) => {} // Channel is empty but still connected
    }

    // Exit if we didn't read to end
    if take_length.is_some() {
        return Ok(());
    }

    // Exit if no follow requested
    if follow_from == FollowFrom::Noop {
        return Ok(());
    }

    // Follow
    let mut search_slice = |input_str: &[u8]| {
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
    let (notify_tx, mut notify_rx) = mpsc::channel(100);

    let mut watcher = RecommendedWatcher::new(
        move |result: Result<Event, Error>| {
            let _ = notify_tx.blocking_send(result);
        },
        Config::default(),
    )?;

    watcher.watch(path, RecursiveMode::NonRecursive)?;

    let mut reader = BufReader::new(File::open(path)?);
    reader.seek(SeekFrom::End(0))?;

    // Listen for changes
    'outer: loop {
        select! {
            ev = notify_rx.recv() => {
                match ev {
                    Some(Ok(event)) => {
                        if let EventKind::Modify(_) = event.kind {
                            for line in (&mut reader).lines() {
                                match term_rx.try_recv() {
                                    Err(Empty) =>{
                                        match line {
                                            Ok(l) => {
                                                search_slice(l.as_bytes());
                                            },
                                            Err(e) => {
                                                return Err(Box::new(e));
                                            }
                                        }
                                    },
                                    _ => {
                                        break 'outer;
                                    }
                                }
                            }
                        }
                    },
                    Some(Err(e)) => {
                        return Err(Box::new(e));
                    }
                    None => {
                        return Err("Notify channel closed".into());
                    }
                }
            },
            _ = term_rx.recv() => {
                    break 'outer;
            },
        }
    }

    Ok(())
}

#[cfg(test)]
mod test {
    use std::{io::Write, path::Path};

    use lazy_static::lazy_static;
    use rstest::rstest;
    use tempfile::NamedTempFile;
    use tokio::sync::broadcast;

    use super::*;

    lazy_static! {
        static ref TEST_FILE: NamedTempFile = create_test_file();
    }

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
        file.flush()?;

        Ok(())
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

        // Create a channel for termination signal
        let (term_tx, _term_rx) = broadcast::channel(5);

        // Create output channel
        let (tx, mut rx) = mpsc::channel(100);

        // Call run method
        let _ = stream_forward(
            &path,
            start_time,
            None,             // No stop time
            None,             // No grep filter
            FollowFrom::Noop, // Don't follow
            term_tx,
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

        // Create a channel for termination signal
        let (term_tx, _term_rx) = broadcast::channel(5);

        // Create output channel
        let (tx, mut rx) = mpsc::channel(100);

        // Call run method
        let _ = stream_forward(
            &path,
            None, // No start time
            stop_time,
            None,             // No grep filter
            FollowFrom::Noop, // Don't follow
            term_tx,
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

        // Create a channel for termination signal
        let (term_tx, _term_rx) = broadcast::channel(5);

        // Create output channel
        let (tx, mut rx) = mpsc::channel(100);

        // Call run method
        let _ = stream_forward(
            &path,
            start_time,
            stop_time,
            None,             // No grep filter
            FollowFrom::Noop, // Don't follow
            term_tx,
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

        // Create a channel for termination signal
        let (term_tx, _term_rx) = broadcast::channel(5);

        // Create output channel
        let (tx, mut rx) = mpsc::channel(100);

        // For follow, we need to update the file after starting the follow
        let is_follow = follow_from == FollowFrom::End || follow_from == FollowFrom::Default;

        // For is_follow, we need to spawn a thread to update the file after a short delay
        if is_follow {
            // Clone the termination channel sender for the thread
            let tx = term_tx.clone();
            std::thread::spawn(move || {
                // Wait a bit to ensure the follow has started
                std::thread::sleep(std::time::Duration::from_millis(100));

                // Update the file with new lines
                update_test_file(test_file.path()).expect("Failed to update test file");

                std::thread::sleep(std::time::Duration::from_millis(100));

                // Terminate the stream
                let _ = tx.send(());
            });
        }

        // Call run method
        let _ = stream_forward(
            &path,
            start_time,
            None, // No stop time
            None, // No grep filter
            follow_from,
            term_tx,
            tx,
        )
        .await;

        // Create a buffer to capture output
        let mut output = Vec::new();

        while let Some(record) = rx.recv().await {
            output.push(record);
        }

        compare_lines(output, expected_lines);
    }
}
