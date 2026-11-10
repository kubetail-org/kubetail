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

use chrono::{DateTime, Utc};

/// The fields of a CRI log line: `<timestamp> <stream> <tags> <message>`.
#[derive(Debug, PartialEq)]
pub struct CriLine<'a> {
    timestamp: &'a [u8],
    pub is_final: bool,
    pub message: &'a [u8],
}

impl CriLine<'_> {
    /// Parses the timestamp, which is RFC 3339. A caller that needs the time
    /// drops the line on `None`.
    pub fn timestamp(&self) -> Option<DateTime<Utc>> {
        let timestamp = std::str::from_utf8(self.timestamp).ok()?;
        let timestamp = DateTime::parse_from_rfc3339(timestamp).ok()?;
        Some(timestamp.with_timezone(&Utc))
    }
}

/// Splits a CRI log line into its fields, by the kubelet's rules: the stream
/// is `stdout` or `stderr`, and the line is partial only when the first of
/// its `:`-joined tags is `P`. Returns `None` for any other line.
///
/// The trailing `\n` is not part of the message; a `\r` before it is. Unlike
/// the kubelet, a line may end at the tags: its message is empty.
pub fn parse(line: &[u8]) -> Option<CriLine<'_>> {
    let line = line.strip_suffix(b"\n").unwrap_or(line);

    let mut fields = line.splitn(4, |&b| b == b' ');

    let timestamp = fields.next()?;

    let stream = fields.next()?;
    if stream != b"stdout" && stream != b"stderr" {
        return None;
    }

    let tags = fields.next()?;

    Some(CriLine {
        timestamp,
        is_final: tags != b"P" && !tags.starts_with(b"P:"),
        message: fields.next().unwrap_or_default(),
    })
}

#[cfg(test)]
mod tests {
    #![allow(clippy::unwrap_used)]

    use rstest::rstest;

    use super::*;

    const TS: &str = "2025-07-31T12:06:00.001936471Z";

    #[rstest]
    #[case("stdout F hello world", true, "hello world")]
    #[case("stderr F hello world", true, "hello world")]
    #[case("stdout P hello world", false, "hello world")]
    #[case("stdout F  leading space", true, " leading space")]
    #[case("stdout F hello\n", true, "hello")]
    #[case("stdout F hello\r\n", true, "hello\r")]
    #[case("stdout F ", true, "")]
    #[case("stdout F", true, "")]
    #[case("stdout P", false, "")]
    #[case("stdout P\n", false, "")]
    #[case("stdout P \r\n", false, "\r")]
    #[case("stdout F:tag hello", true, "hello")]
    #[case("stdout P:tag hello", false, "hello")]
    #[case("stdout P:a:b hello", false, "hello")]
    #[case("stdout tag:P hello", true, "hello")]
    #[case("stdout X hello", true, "hello")]
    #[case("stdout p hello", true, "hello")]
    #[case("stdout PP hello", true, "hello")]
    #[case("stdout  hello", true, "hello")]
    #[case("stdout P\r\n", true, "")]
    fn splits_line(#[case] rest: &str, #[case] is_final: bool, #[case] message: &str) {
        let line = format!("{TS} {rest}");
        assert_eq!(
            parse(line.as_bytes()),
            Some(CriLine {
                timestamp: TS.as_bytes(),
                is_final,
                message: message.as_bytes(),
            })
        );
    }

    #[rstest]
    #[case("stdin F hello")]
    #[case("STDOUT F hello")]
    #[case("stdoutx F hello")]
    #[case(" F hello")]
    #[case("F hello")]
    fn rejects_unknown_stream(#[case] rest: &str) {
        let line = format!("{TS} {rest}");
        assert_eq!(parse(line.as_bytes()), None);
    }

    #[rstest]
    #[case("")]
    #[case("failed")]
    #[case("2025-07-31T12:06:00.001936471Z")]
    #[case("2025-07-31T12:06:00.001936471Z stdout")]
    fn rejects_line_with_missing_fields(#[case] line: &str) {
        assert_eq!(parse(line.as_bytes()), None);
    }

    #[rstest]
    #[case("2025-07-31T12:06:00.001936471Z")]
    #[case("2025-07-31T15:06:00.001936471+03:00")]
    fn timestamp_is_utc(#[case] timestamp: &str) {
        let line = format!("{timestamp} stdout F hello");
        let expected = DateTime::parse_from_rfc3339(TS).unwrap();
        assert_eq!(
            parse(line.as_bytes()).unwrap().timestamp(),
            Some(expected.to_utc())
        );
    }

    #[rstest]
    #[case(b"not-a-timestamp stdout F hello")]
    #[case(b"2025-07-31T12:06:00.001936471+3:00 stdout F hello")]
    #[case(b"2025-07-31T12:06:00 stdout F hello")]
    #[case(b"2025-07-31T12:06:00\xffZ stdout F hello")]
    fn malformed_timestamp_is_none(#[case] line: &[u8]) {
        assert_eq!(parse(line).unwrap().timestamp(), None);
    }
}
