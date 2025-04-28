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
    error::Error,
    fs::File,
    io::{BufRead, BufReader, Seek, SeekFrom},
};

use chrono::{DateTime, Utc};
use serde_json;

/// Represents an offset result from find_nearest_offset()
#[derive(Debug, PartialEq)]
pub struct Offset {
    pub byte_offset: u64,
    pub line_length: u64,
}

/// Finds the nearest offset to a given timestamp between `min_offset` and
/// `max_offset` greater than or equal to `target_time`.
pub fn find_nearest_offset_since(
    file: &File,
    target_time: DateTime<Utc>,
    min_offset: u64,
    max_offset: u64,
) -> Result<Option<Offset>, Box<dyn Error>> {
    find_nearest_offset(file, target_time, min_offset, max_offset, FindMode::Since)
}

/// Finds the nearest offset to a given timestamp between `min_offset` and
/// `max_offset` less than or equal to `target_time`.
pub fn find_nearest_offset_until(
    file: &File,
    target_time: DateTime<Utc>,
    min_offset: u64,
    max_offset: u64,
) -> Result<Option<Offset>, Box<dyn Error>> {
    find_nearest_offset(file, target_time, min_offset, max_offset, FindMode::Until)
}

enum FindMode {
    Since,
    Until,
}

fn find_nearest_offset(
    file: &File,
    target_time: DateTime<Utc>,
    min_offset: u64,
    max_offset: u64,
    mode: FindMode,
) -> Result<Option<Offset>, Box<dyn Error>> {
    if max_offset == 0 {
        return Ok(None);
    }

    let mut left: i64 = min_offset as i64;
    let mut right: i64 = (max_offset - 1) as i64;

    //    let mut result: Option<u64> = None;

    let mut result: Option<Offset> = None;

    let mut reader = BufReader::new(file);

    while left <= right {
        let mid = (left + right) / 2;

        // Seek to the mid position inside BufReader
        reader.seek(SeekFrom::Start(mid as u64))?;

        // Scan for the next valid timestamp.
        let (new_mid, res_opt) = scan_timestamp(&mut reader, right, mid)?;

        match res_opt {
            Some((ts, line_length)) => {
                // Adjust search boundaries based on comparison.
                match mode {
                    FindMode::Since => {
                        // Adjust search boundaries based on comparison.
                        if ts >= target_time {
                            result = Some(Offset {
                                byte_offset: new_mid as u64,
                                line_length: line_length as u64,
                            });
                            right = new_mid - 1;
                        } else {
                            left = new_mid + 1;
                        }
                    }
                    FindMode::Until => {
                        if ts <= target_time {
                            result = Some(Offset {
                                byte_offset: new_mid as u64,
                                line_length: line_length as u64,
                            });
                            left = new_mid + 1;
                        } else {
                            right = new_mid - 1;
                        }
                    }
                }
            }
            None => {
                // No valid timestamp found, narrow the search.
                right = new_mid - 1;
            }
        }
    }

    Ok(result)
}

type ScanResultTuple = (DateTime<Utc>, usize);

/// Reads from the given buffered reader starting at `start_pos` up to `right`
/// to find a line with a valid timestamp. Returns the position where the
/// timestamp was found along with the parsed timestamp (if any).
fn scan_timestamp(
    reader: &mut BufReader<&File>,
    right: i64,
    start_pos: i64,
) -> Result<(i64, Option<ScanResultTuple>), Box<dyn Error>> {
    let mut pos = start_pos;
    while pos <= right {
        let mut line = String::new();
        let bytes_read = reader.read_line(&mut line)?;
        line = line.trim_end().to_string();

        if bytes_read == 0 {
            // EOF reached; no timestamp found.
            return Ok((start_pos, None));
        }

        if let Ok(ts) = parse_timestamp(&line) {
            return Ok((pos, Some((ts, line.len()))));
        }

        pos += bytes_read as i64;
    }

    Ok((start_pos, None))
}

/// Attempts to parse a timestamp from the beginning of the log line.
/// The log line is expected to start with an RFC 3339 formatted timestamp
/// or be in Docker JSON format with a "timestamp" field.
fn parse_timestamp(line: &str) -> Result<DateTime<Utc>, Box<dyn std::error::Error>> {
    // Check if the line starts with '{' which indicates JSON format (Docker logs)
    if line.starts_with('{') {
        // Parse the JSON
        let json: serde_json::Value = serde_json::from_str(line)?;
        
        // Extract the timestamp field
        if let Some(timestamp) = json.get("timestamp").and_then(|t| t.as_str()) {
            let ts = DateTime::parse_from_rfc3339(timestamp)?.with_timezone(&Utc);
            return Ok(ts);
        } else {
            return Err(format!("missing timestamp field in JSON log: {}", line).into());
        }
    } else {
        // Original CRI format parsing
        let parts: Vec<&str> = line.splitn(2, ' ').collect();
        if parts.len() < 2 {
            return Err(format!("invalid log line: {}", line).into());
        }
        let ts = DateTime::parse_from_rfc3339(parts[0])?.with_timezone(&Utc);
        Ok(ts)
    }
}

#[cfg(test)]
mod common {
    use std::io::Write;

    use tempfile::NamedTempFile;

    use super::*;

    /// Helper to create a temporary log file from a slice of log lines.
    /// Returns the file and a vector of the starting byte offset for each line.
    pub fn create_temp_log(lines: &[&str]) -> Result<(NamedTempFile, Vec<Offset>), Box<dyn Error>> {
        let mut tmpfile = NamedTempFile::new()?;
        let mut offsets = Vec::with_capacity(lines.len());
        let mut byte_offset = 0u64;

        for &line in lines {
            offsets.push(Offset {
                byte_offset: byte_offset,
                line_length: line.len() as u64,
            });
            tmpfile.write_all(line.as_bytes())?;
            tmpfile.write_all(b"\n")?; // Write a newline after each line.
            byte_offset += line.as_bytes().len() as u64 + 1;
        }
        tmpfile.flush()?;
        Ok((tmpfile, offsets))
    }
}

#[cfg(test)]
mod tests_find_nearest_offset_since {
    use chrono::DateTime;

    use super::*;

    #[test]
    fn test_normal() -> Result<(), Box<dyn Error>> {
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
        let (tmpfile, offsets) = common::create_temp_log(&lines)?;

        // Define test cases as (target_timestamp, expected_offset).
        // For targets beyond the last log, we expect -1.
        let test_cases = vec![
            (
                "2024-10-01T05:40:46.960135302Z",
                Some(&offsets[0]), // first log
            ),
            (
                "2024-10-01T05:40:59.103901461Z",
                Some(&offsets[9]), // last log
            ),
            (
                // Before the first log timestamp, should return the first entry.
                "2024-10-01T05:40:46.960135301Z",
                Some(&offsets[0]),
            ),
            (
                // After the last log timestamp, should return -1.
                "2024-10-01T05:40:59.103901462Z",
                None,
            ),
            (
                // Exact match in the middle.
                "2024-10-01T05:40:52.222363431Z",
                Some(&offsets[3]),
            ),
            (
                // Just before an entry in the middle.
                "2024-10-01T05:40:52.222363430Z",
                Some(&offsets[3]),
            ),
        ];

        let file = tmpfile.into_file();
        let max_offset = file.metadata()?.len();

        for (target_str, expected) in test_cases {
            let target_time = DateTime::parse_from_rfc3339(target_str)?.with_timezone(&Utc);
            let offset = find_nearest_offset_since(&file, target_time, 0, max_offset)?;
            assert_eq!(offset.as_ref(), expected, "target: {}", target_str);
        }

        Ok(())
    }

    #[test]
    fn test_one_line() -> Result<(), Box<dyn Error>> {
        let line = "2024-10-01T05:40:23.308676722Z stdout F linenum 1";
        let (tmpfile, offsets) = common::create_temp_log(&[line])?;

        let test_cases = vec![
            (
                "2024-10-01T05:40:23.308676722Z",
                Some(&offsets[0]), // exact match
            ),
            (
                "2024-10-01T05:40:23.308676721Z",
                Some(&offsets[0]), // before the timestamp returns the first entry
            ),
            (
                "2024-10-01T05:40:23.308676723Z",
                None, // after the only log entry returns -1
            ),
        ];

        let file = tmpfile.into_file();
        let max_offset = file.metadata()?.len();

        for (target_str, expected) in test_cases {
            let target_time = DateTime::parse_from_rfc3339(target_str)?.with_timezone(&Utc);
            let offset = find_nearest_offset_since(&file, target_time, 0, max_offset)?;
            assert_eq!(offset.as_ref(), expected, "target: {}", target_str);
        }

        Ok(())
    }

    #[test]
    fn test_empty() -> Result<(), Box<dyn Error>> {
        let (tmpfile, _offsets) = common::create_temp_log(&[])?;

        let file = tmpfile.into_file();
        let max_offset = file.metadata()?.len();

        let target_str = "2024-10-01T05:40:23.308676722Z";
        let target_time = DateTime::parse_from_rfc3339(target_str)?.with_timezone(&Utc);
        let offset = find_nearest_offset_since(&file, target_time, 0, max_offset)?;
        assert_eq!(offset, None, "target: {}", target_str);

        Ok(())
    }

    #[test]
    fn test_malformed_single() -> Result<(), Box<dyn Error>> {
        let line = "failed";
        let (tmpfile, _offsets) = common::create_temp_log(&[line])?;

        let file = tmpfile.into_file();
        let max_offset = file.metadata()?.len();

        let target_str = "2024-10-01T05:40:23.308676722Z";
        let target_time = DateTime::parse_from_rfc3339(target_str)?.with_timezone(&Utc);
        let offset = find_nearest_offset_since(&file, target_time, 0, max_offset)?;
        assert_eq!(offset, None, "target: {}", target_str);

        Ok(())
    }

    #[test]
    fn test_malformed_mixed() -> Result<(), Box<dyn std::error::Error>> {
        let lines = [
            "failed",
            "2024-10-01T05:40:25.221410625Z stdout F linenum 2",
            "2024-10-01T05:40:25.869390042Z stdout F linenum 3",
            "2024-10-01T05:40:27.180909751Z stdout F linenum 4",
            "failed",
            "failed",
            "2024-10-01T05:40:28.706906543Z stdout F linenum 7",
            "failed",
            "2024-10-01T05:40:28.706906543Z stdout F linenum 8",
            "failed",
        ];
        let (tmpfile, offsets) = common::create_temp_log(&lines)?;

        let test_cases = vec![
            (
                "2024-10-01T05:40:25.869390042Z",
                Some(&offsets[2]), // exact match in middle
            ),
            (
                "2024-10-01T05:40:25.221410625Z",
                Some(&offsets[1]), // exact match after malformed
            ),
            (
                "2024-10-01T05:40:28.706906542Z",
                Some(&offsets[6]), // exact match between malformed
            ),
            (
                "2024-10-01T05:40:25.221410621Z",
                Some(&offsets[1]), // before exact match
            ),
            (
                "2024-10-01T05:40:28.706906544Z",
                None, // after last exact match
            ),
        ];

        let file = tmpfile.into_file();
        let max_offset = file.metadata()?.len();

        for (target_str, expected) in test_cases {
            let target_time = DateTime::parse_from_rfc3339(target_str)?.with_timezone(&Utc);
            let offset = find_nearest_offset_since(&file, target_time, 0, max_offset)?;
            assert_eq!(offset.as_ref(), expected, "target: {}", target_str);
        }

        Ok(())
    }

    #[test]
    fn test_multiple_matches() -> Result<(), Box<dyn std::error::Error>> {
        let lines = [
            "2024-10-01T05:40:46.960135302Z stdout F linenum 1",
            "2024-10-01T05:40:48.840712595Z stdout F linenum 2",
            "2024-10-01T05:40:50.075182095Z stdout F linenum 3",
            "2024-10-01T05:40:52.222363431Z stdout F linenum 4",
            "2024-10-01T05:40:54.911909292Z stdout F linenum 5",
            "2024-10-01T05:40:57.041413876Z stdout F linenum 6",
            "2024-10-01T05:40:58.197779961Z stdout F linenum 7",
            "2024-10-01T05:40:58.197779961Z stdout F linenum 8",
            "2024-10-01T05:40:58.197779961Z stdout F linenum 9",
            "2024-10-01T05:40:59.103901461Z stdout F linenum 10",
        ];
        let (tmpfile, offsets) = common::create_temp_log(&lines)?;

        let test_cases = vec![
            (
                "2024-10-01T05:40:58.197779961Z",
                Some(&offsets[6]), // exact match
            ),
            (
                "2024-10-01T05:40:58.197779960Z",
                Some(&offsets[6]), // before exact match
            ),
        ];

        let file = tmpfile.into_file();
        let max_offset = file.metadata()?.len();

        for (target_str, expected) in test_cases {
            let target_time = DateTime::parse_from_rfc3339(target_str)?.with_timezone(&Utc);
            let offset = find_nearest_offset_since(&file, target_time, 0, max_offset)?;
            assert_eq!(offset.as_ref(), expected, "target: {}", target_str);
        }

        Ok(())
    }
}

#[cfg(test)]
mod tests_find_nearest_offset_until {
    use chrono::DateTime;

    use super::*;

    #[test]
    fn test_normal() -> Result<(), Box<dyn Error>> {
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
        let (tmpfile, offsets) = common::create_temp_log(&lines)?;

        // Define test cases as (target_timestamp, expected_offset).
        // For targets beyond the last log, we expect -1.
        let test_cases = vec![
            (
                "2024-10-01T05:40:46.960135302Z",
                Some(&offsets[0]), // first log
            ),
            (
                "2024-10-01T05:40:59.103901461Z",
                Some(&offsets[9]), // last log
            ),
            (
                // Before the first log timestamp, should return None.
                "2024-10-01T05:40:46.960135301Z",
                None,
            ),
            (
                // After the last log timestamp, should return last.
                "2024-10-01T05:40:59.103901462Z",
                Some(&offsets[9]),
            ),
            (
                // Exact match in the middle.
                "2024-10-01T05:40:52.222363431Z",
                Some(&offsets[3]),
            ),
            (
                // Just after an entry in the middle.
                "2024-10-01T05:40:52.222363432Z",
                Some(&offsets[3]),
            ),
        ];

        let file = tmpfile.into_file();
        let max_offset = file.metadata()?.len();

        for (target_str, expected) in test_cases {
            let target_time = DateTime::parse_from_rfc3339(target_str)?.with_timezone(&Utc);
            let offset = find_nearest_offset_until(&file, target_time, 0, max_offset)?;
            assert_eq!(offset.as_ref(), expected, "target: {}", target_str);
        }

        Ok(())
    }

    #[test]
    fn test_one_line() -> Result<(), Box<dyn Error>> {
        let line = "2024-10-01T05:40:23.308676722Z stdout F linenum 1";
        let (tmpfile, offsets) = common::create_temp_log(&[line])?;

        let test_cases = vec![
            (
                "2024-10-01T05:40:23.308676722Z",
                Some(&offsets[0]), // exact match
            ),
            (
                "2024-10-01T05:40:23.308676721Z",
                None, // before the timestamp returns None
            ),
            (
                "2024-10-01T05:40:23.308676723Z",
                Some(&offsets[0]), // after the only log entry returns entry
            ),
        ];

        let file = tmpfile.into_file();
        let max_offset = file.metadata()?.len();

        for (target_str, expected) in test_cases {
            let target_time = DateTime::parse_from_rfc3339(target_str)?.with_timezone(&Utc);
            let offset = find_nearest_offset_until(&file, target_time, 0, max_offset)?;
            assert_eq!(offset.as_ref(), expected, "target: {}", target_str);
        }

        Ok(())
    }

    #[test]
    fn test_empty() -> Result<(), Box<dyn Error>> {
        let (tmpfile, _offsets) = common::create_temp_log(&[])?;

        let file = tmpfile.into_file();
        let max_offset = file.metadata()?.len();

        let target_str = "2024-10-01T05:40:23.308676722Z";
        let target_time = DateTime::parse_from_rfc3339(target_str)?.with_timezone(&Utc);
        let offset = find_nearest_offset_until(&file, target_time, 0, max_offset)?;
        assert_eq!(offset, None, "target: {}", target_str);

        Ok(())
    }

    #[test]
    fn test_malformed_single() -> Result<(), Box<dyn Error>> {
        let line = "failed";
        let (tmpfile, _offsets) = common::create_temp_log(&[line])?;

        let file = tmpfile.into_file();
        let max_offset = file.metadata()?.len();

        let target_str = "2024-10-01T05:40:23.308676722Z";
        let target_time = DateTime::parse_from_rfc3339(target_str)?.with_timezone(&Utc);
        let offset = find_nearest_offset_until(&file, target_time, 0, max_offset)?;
        assert_eq!(offset, None, "target: {}", target_str);

        Ok(())
    }

    #[test]
    fn test_malformed_mixed() -> Result<(), Box<dyn std::error::Error>> {
        let lines = [
            "failed",
            "2024-10-01T05:40:25.221410625Z stdout F linenum 2",
            "2024-10-01T05:40:25.869390042Z stdout F linenum 3",
            "2024-10-01T05:40:27.180909751Z stdout F linenum 4",
            "failed",
            "failed",
            "2024-10-01T05:40:28.706906543Z stdout F linenum 7",
            "failed",
            "2024-10-01T05:40:29.706906543Z stdout F linenum 8",
            "failed",
        ];
        let (tmpfile, offsets) = common::create_temp_log(&lines)?;

        let test_cases = vec![
            (
                "2024-10-01T05:40:25.869390042Z",
                Some(&offsets[2]), // exact match in middle
            ),
            (
                "2024-10-01T05:40:25.221410625Z",
                Some(&offsets[1]), // exact match after malformed
            ),
            (
                "2024-10-01T05:40:28.706906543Z",
                Some(&offsets[6]), // exact match between malformed
            ),
            (
                "2024-10-01T05:40:25.221410621Z",
                None, // before first exact match
            ),
            (
                "2024-10-01T05:40:29.706906544Z",
                Some(&offsets[8]), // after last exact match
            ),
        ];

        let file = tmpfile.into_file();
        let max_offset = file.metadata()?.len();

        for (target_str, expected) in test_cases {
            let target_time = DateTime::parse_from_rfc3339(target_str)?.with_timezone(&Utc);
            let offset = find_nearest_offset_until(&file, target_time, 0, max_offset)?;
            assert_eq!(offset.as_ref(), expected, "target: {}", target_str);
        }

        Ok(())
    }

    #[test]
    fn test_multiple_matches() -> Result<(), Box<dyn std::error::Error>> {
        let lines = [
            "2024-10-01T05:40:46.960135302Z stdout F linenum 1",
            "2024-10-01T05:40:48.840712595Z stdout F linenum 2",
            "2024-10-01T05:40:50.075182095Z stdout F linenum 3",
            "2024-10-01T05:40:52.222363431Z stdout F linenum 4",
            "2024-10-01T05:40:54.911909292Z stdout F linenum 5",
            "2024-10-01T05:40:57.041413876Z stdout F linenum 6",
            "2024-10-01T05:40:58.197779961Z stdout F linenum 7",
            "2024-10-01T05:40:58.197779961Z stdout F linenum 8",
            "2024-10-01T05:40:58.197779961Z stdout F linenum 9",
            "2024-10-01T05:40:59.103901461Z stdout F linenum 10",
        ];
        let (tmpfile, offsets) = common::create_temp_log(&lines)?;

        let test_cases = vec![
            (
                "2024-10-01T05:40:58.197779961Z",
                Some(&offsets[8]), // exact match
            ),
            (
                "2024-10-01T05:40:58.197779962Z",
                Some(&offsets[8]), // after exact match
            ),
        ];

        let file = tmpfile.into_file();
        let max_offset = file.metadata()?.len();

        for (target_str, expected) in test_cases {
            let target_time = DateTime::parse_from_rfc3339(target_str)?.with_timezone(&Utc);
            let offset = find_nearest_offset_until(&file, target_time, 0, max_offset)?;
            assert_eq!(offset.as_ref(), expected, "target: {}", target_str);
        }

        Ok(())
    }
}
