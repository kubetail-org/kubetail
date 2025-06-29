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

use tokio::sync::mpsc::Sender;
use tonic::Status;
use types::cluster_agent::LogRecord;

use chrono::{DateTime, Utc};
use crossbeam_channel::Receiver;
use grep::{
    printer::JSONBuilder,
    searcher::{MmapChoice, SearcherBuilder},
};

use crate::util::{
    format::FileFormat,
    matcher::{LogFileRegexMatcher, PassThroughMatcher},
    offset::{find_nearest_offset_since, find_nearest_offset_until},
    reader::{ReverseLineReader, TermReader},
    writer::{process_output, CallbackWriter},
};

pub async fn stream_backward(
    path: &PathBuf,
    start_time: Option<DateTime<Utc>>,
    stop_time: Option<DateTime<Utc>>,
    grep: Option<&str>,
    term_rx: Receiver<()>,
    sender: Sender<Result<LogRecord, Status>>,
) -> eyre::Result<()> {
    // Open file
    let file = File::open(&path)?;
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
        term_rx,
    );

    // Init searcher
    let mut searcher = SearcherBuilder::new()
        .line_number(false)
        .memory_map(MmapChoice::never())
        .build();

    // Init writer
    let writer_fn = |chunk: Vec<u8>| process_output(chunk, &sender, format);
    let writer = CallbackWriter::new(writer_fn);

    // Init printer
    let mut printer = JSONBuilder::new().build(writer);

    // Remove leading and trailing whitespace
    let trimmed_grep = grep.map(|grep| grep.trim()).filter(|grep| grep.is_empty());

    match trimmed_grep {
        Some(grep) => {
            let matcher = LogFileRegexMatcher::new(grep, format).unwrap();
            let sink = printer.sink(&matcher);
            let _ = searcher.search_reader(&matcher, term_reverse_reader, sink);
        }
        None => {
            let matcher = PassThroughMatcher::new();
            let sink = printer.sink(&matcher);
            let _ = searcher.search_reader(&matcher, term_reverse_reader, sink);
        }
    }

    Ok(())
}

// #[cfg(test)]
// mod test {
//     use lazy_static::lazy_static;
//     use rstest::rstest;
//     use tempfile::NamedTempFile;
//
//     use super::*;
//
//     lazy_static! {
//         static ref TEST_FILE: NamedTempFile = create_test_file();
//     }
//
//     fn create_test_file() -> NamedTempFile {
//         let lines = [
//             "2024-10-01T05:40:46.960135302Z stdout F linenum 1",
//             "2024-10-01T05:40:48.840712595Z stdout F linenum 2",
//             "2024-10-01T05:40:50.075182095Z stdout F linenum 3",
//             "2024-10-01T05:40:52.222363431Z stdout F linenum 4",
//             "2024-10-01T05:40:54.911909292Z stdout F linenum 5",
//             "2024-10-01T05:40:57.041413876Z stdout F linenum 6",
//             "2024-10-01T05:40:58.197779961Z stdout F linenum 7",
//             "2024-10-01T05:40:58.564018502Z stdout F linenum 8",
//             "2024-10-01T05:40:58.612948127Z stdout F linenum 9",
//             "2024-10-01T05:40:59.103901461Z stdout F linenum 10",
//         ];
//
//         let mut tmpfile = NamedTempFile::new().expect("Failed create");
//         writeln!(tmpfile, "{}", lines.join("\n")).expect("Failed write");
//         tmpfile
//     }
//
//     /// Compare captured binary output with expected lines
//     /// Parses the binary output and compares the message fields with expected lines
//     fn compare_lines(output: &[u8], expected_lines: Vec<&'static str>) {
//         // Parse the captured output
//         let captured_lines: Vec<String> = output
//             .split(|b| *b == b'\n')
//             .filter(|line| !line.is_empty())
//             .map(|line| {
//                 // Parse the JSON manually to extract the message field
//                 let json: serde_json::Value = serde_json::from_slice(line).unwrap();
//                 json["message"].as_str().unwrap().to_string()
//             })
//             .collect();
//
//         // Compare against expected lines
//         assert_eq!(
//             captured_lines.len(),
//             expected_lines.len(),
//             "Number of lines doesn't match"
//         );
//         for (i, expected) in expected_lines.iter().enumerate() {
//             assert_eq!(&captured_lines[i], expected, "Line {} doesn't match", i);
//         }
//     }
//
//     // Test `start_time` arg
//     #[rstest]
//     #[case("", vec!["linenum 10", "linenum 9", "linenum 8", "linenum 7", "linenum 6", "linenum 5", "linenum 4", "linenum 3", "linenum 2", "linenum 1"])]
//     #[case("2024-10-01T05:40:58.197779961Z", vec!["linenum 10", "linenum 9", "linenum 8", "linenum 7"])]
//     #[case("2024-10-01T05:40:58.197779960Z", vec!["linenum 10", "linenum 9", "linenum 8", "linenum 7"])]
//     #[case("2024-10-01T05:40:58.197779962Z", vec!["linenum 10", "linenum 9", "linenum 8"])]
//     #[case("2024-10-01T05:40:59.103901461Z", vec!["linenum 10"])]
//     #[case("2024-10-01T05:40:59.103901462Z", vec![])]
//     fn test_start_time(#[case] start_time_str: String, #[case] expected_lines: Vec<&'static str>) {
//         let path = TEST_FILE.path().to_str().unwrap();
//
//         // Parse start time if provided, otherwise use None
//         let start_time = if start_time_str.is_empty() {
//             None
//         } else {
//             Some(start_time_str.parse::<DateTime<Utc>>().unwrap())
//         };
//
//         // Create a channel for termination signal
//         let (_term_tx, term_rx) = crossbeam_channel::unbounded();
//
//         // Create a buffer to capture output
//         let mut output = Vec::new();
//
//         // Call run method
//         run(
//             path,
//             start_time,
//             None, // No stop time
//             "",   // No grep filter
//             term_rx,
//             &mut output,
//         )
//         .unwrap();
//
//         // Compare output with expected lines
//         compare_lines(&output, expected_lines);
//     }
//
//     // Test `stop_time` arg
//     #[rstest]
//     #[case("", vec!["linenum 10", "linenum 9", "linenum 8", "linenum 7", "linenum 6", "linenum 5", "linenum 4", "linenum 3", "linenum 2", "linenum 1"])]
//     #[case("2024-10-01T05:40:52.222363431Z", vec!["linenum 4", "linenum 3", "linenum 2", "linenum 1"])]
//     #[case("2024-10-01T05:40:52.222363432Z", vec!["linenum 4", "linenum 3", "linenum 2", "linenum 1"])]
//     #[case("2024-10-01T05:40:52.222363430Z", vec!["linenum 3", "linenum 2", "linenum 1"])]
//     #[case("2024-10-01T05:40:46.960135302Z", vec!["linenum 1"])]
//     #[case("2024-10-01T05:40:46.960135301Z", vec![])]
//     fn test_stop_time(#[case] stop_time_str: String, #[case] expected_lines: Vec<&'static str>) {
//         let path = TEST_FILE.path().to_str().unwrap();
//
//         // Parse start time if provided, otherwise use None
//         let stop_time = if stop_time_str.is_empty() {
//             None
//         } else {
//             Some(stop_time_str.parse::<DateTime<Utc>>().unwrap())
//         };
//
//         // Create a channel for termination signal
//         let (_term_tx, term_rx) = crossbeam_channel::unbounded();
//
//         // Create a buffer to capture output
//         let mut output = Vec::new();
//
//         // Call run method
//         run(
//             path,
//             None, // No start time
//             stop_time,
//             "", // No grep filter
//             term_rx,
//             &mut output,
//         )
//         .unwrap();
//
//         // Compare output with expected lines
//         compare_lines(&output, expected_lines);
//     }
//
//     // Test `start_time` and `stop_time` args together
//     #[rstest]
//     #[case("", "", vec!["linenum 10", "linenum 9", "linenum 8", "linenum 7", "linenum 6", "linenum 5", "linenum 4", "linenum 3", "linenum 2", "linenum 1"])]
//     #[case("2024-10-01T05:40:50.075182095Z", "2024-10-01T05:40:58.197779961Z", vec!["linenum 7", "linenum 6", "linenum 5", "linenum 4", "linenum 3"])]
//     #[case("2024-10-01T05:40:50.075182094Z", "2024-10-01T05:40:58.197779961Z", vec!["linenum 7", "linenum 6", "linenum 5", "linenum 4", "linenum 3"])]
//     #[case("2024-10-01T05:40:50.075182095Z", "2024-10-01T05:40:58.197779962Z", vec!["linenum 7", "linenum 6", "linenum 5", "linenum 4", "linenum 3"])]
//     #[case("2024-10-01T05:40:50.075182096Z", "2024-10-01T05:40:58.197779961Z", vec!["linenum 7", "linenum 6", "linenum 5", "linenum 4"])]
//     #[case("2024-10-01T05:40:50.075182095Z", "2024-10-01T05:40:58.197779960Z", vec!["linenum 6", "linenum 5", "linenum 4", "linenum 3"])]
//     #[case("2024-10-01T05:40:50.075182096Z", "2024-10-01T05:40:58.197779960Z", vec!["linenum 6", "linenum 5", "linenum 4"])]
//     fn test_start_time_and_stop_time(
//         #[case] start_time_str: String,
//         #[case] stop_time_str: String,
//         #[case] expected_lines: Vec<&'static str>,
//     ) {
//         let path = TEST_FILE.path().to_str().unwrap();
//
//         // Parse start time if provided, otherwise use None
//         let start_time = if start_time_str.is_empty() {
//             None
//         } else {
//             Some(start_time_str.parse::<DateTime<Utc>>().unwrap())
//         };
//
//         // Parse stop time if provided, otherwise use None
//         let stop_time = if stop_time_str.is_empty() {
//             None
//         } else {
//             Some(stop_time_str.parse::<DateTime<Utc>>().unwrap())
//         };
//
//         // Create a channel for termination signal
//         let (_term_tx, term_rx) = crossbeam_channel::unbounded();
//
//         // Create a buffer to capture output
//         let mut output = Vec::new();
//
//         // Call run method
//         run(
//             path,
//             start_time,
//             stop_time,
//             "", // No grep filter
//             term_rx,
//             &mut output,
//         )
//         .unwrap();
//
//         // Compare output with expected lines
//         compare_lines(&output, expected_lines);
//     }
// }
