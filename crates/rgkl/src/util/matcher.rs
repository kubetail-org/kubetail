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

use grep::{
    matcher::{self, Match, Matcher},
    regex::{self, RegexMatcher, RegexMatcherBuilder},
};
use memchr::memmem;
use serde::Deserialize;

use crate::util::format::FileFormat;

// PassThroughMatcher
#[derive(Default)]
pub struct PassThroughMatcher {}

impl PassThroughMatcher {
    // Create a new PassThroughMatcher with a zeroed counter.
    pub fn new() -> Self {
        Self {}
    }
}

impl Matcher for PassThroughMatcher {
    type Captures = matcher::NoCaptures;
    type Error = matcher::NoError;

    fn find_at(&self, haystack: &[u8], start: usize) -> Result<Option<Match>, Self::Error> {
        // Ignore haystacks with multiple messages
        if start > 0 {
            return Ok(None);
        }
        Ok(Some(Match::new(start, haystack.len())))
    }

    fn new_captures(&self) -> Result<Self::Captures, Self::Error> {
        Ok(matcher::NoCaptures::new())
    }
}

// LogFileRegexMatcher
pub struct LogFileRegexMatcher {
    inner: RegexMatcher,
    format: FileFormat,
}

impl LogFileRegexMatcher {
    pub fn new(
        inner_pattern: &str,
        format: FileFormat,
    ) -> Result<LogFileRegexMatcher, regex::Error> {
        // Replaces spaces with ANSI-tolerant pattern
        let regex_pattern = &inner_pattern.replace(
            " ",
            r"(?:(?:\x1B\[[0-9;]*[mK])?)*\s(?:(?:\x1B\[[0-9;]*[mK])?)*",
        );

        let inner = RegexMatcherBuilder::new()
            .line_terminator(Some(b'\n'))
            .case_smart(false)
            .case_insensitive(false)
            .build(regex_pattern)?;

        Ok(LogFileRegexMatcher { inner, format })
    }
}

impl Matcher for LogFileRegexMatcher {
    type Captures = matcher::NoCaptures;
    type Error = matcher::NoError;

    fn find_at(&self, haystack: &[u8], start: usize) -> Result<Option<Match>, Self::Error> {
        // We can ignore haystacks with multiple messages
        if start > 0 {
            return Ok(None);
        }

        // Execute format‐specific check, then convert the bool into an Option<Match>
        let result = (match self.format {
            FileFormat::Docker => self.has_match_docker(haystack)?,
            FileFormat::CRI => self.has_match_cri(haystack)?,
        })
        .then(|| Match::new(start, haystack.len()));

        Ok(result)
    }

    fn new_captures(&self) -> Result<Self::Captures, Self::Error> {
        Ok(matcher::NoCaptures::new())
    }
}

impl LogFileRegexMatcher {
    fn has_match_docker(&self, haystack: &[u8]) -> Result<bool, matcher::NoError> {
        if let Some(msg) = extract_message_docker(haystack) {
            if self.inner.find(msg.as_slice())?.is_some() {
                return Ok(true);
            }
        }
        Ok(false)
    }

    fn has_match_cri(&self, haystack: &[u8]) -> Result<bool, matcher::NoError> {
        if let Some(msg) = extract_message_cri(haystack) {
            if self.inner.find(msg)?.is_some() {
                return Ok(true);
            }
        }
        Ok(false)
    }
}

// Extract <message> from docker format
pub fn extract_message_docker(line: &[u8]) -> Option<Vec<u8>> {
    /// A helper struct that only deserializes the top-level "log" field.
    #[derive(Deserialize)]
    struct LogOnlyJson {
        log: String,
    }

    // Deserialize JSON and extract the "log" field as a string
    let v: LogOnlyJson = serde_json::from_slice(line).ok()?;
    Some(v.log.into_bytes())
}

// Extract <message> from CRI format (<isotimestamp> <stdout/stderr> <P/F> <message>)
pub fn extract_message_cri(line: &[u8]) -> Option<&[u8]> {
    // Advance past the non-decimal part of the ISO8601 timestamp
    let start_pos = 19;
    let partial = line.get(start_pos..)?;

    // Find the space that precedes "<stdout/stderr>"
    let space_idx = memmem::find(partial, b" ")?;

    // Skip to beginning of <message>
    let log_start = start_pos + space_idx + 10;

    // Check bounds
    if log_start >= line.len() {
        return None;
    }

    // Return <message>
    Some(&line[log_start..line.len()])
}

#[cfg(test)]
mod tests {
    use super::*;

    use rstest::rstest;

    #[rstest]
    #[case("2025-07-31T12:06:00.001936471Z stdout F hello world", "hello world")]
    #[case("2025-07-31T12:06:00.001936471Z stderr F hello world", "hello world")]
    #[case("2025-07-31T12:06:00.001936471Z stdout P hello world", "hello world")]
    #[case("2025-07-31T12:06:00.00Z stdout F hello world", "hello world")]
    #[case(
        "2025-07-31T12:06:00.001936471+3:00 stdout F hello world",
        "hello world"
    )]
    fn test_extract_message_cri(#[case] line_str: String, #[case] expected_msg: String) {
        let msg_maybe = extract_message_cri(line_str.as_bytes());
        assert_eq!(msg_maybe, Some(expected_msg.as_bytes()));
    }

    #[rstest]
    #[case(
        r#"{"log": "hello world","stream":"stdout","time":"2025-07-31T12:06:00.001936471Z"}"#,
        "hello world"
    )]
    #[case(
        r#"{"log": "hello world\n","stream":"stderr","time":"2025-07-31T12:06:00.001936471Z"}"#,
        "hello world\n"
    )]
    #[case(r#"{"log":"multi line\nmessage","stream":"stdout","time":"2025-07-31T12:06:00.001936471Z"}"#, "multi line\nmessage")]
    #[case(
        r#"{"log":"","stream":"stdout","time":"2025-07-31T12:06:00.001936471Z"}"#,
        ""
    )]
    #[case(
        r#"{"log":"with \"quotes\"","stream":"stdout","time":"2025-07-31T12:06:00.001936471Z"}"#,
        "with \"quotes\""
    )]
    fn test_extract_message_docker(#[case] line_str: String, #[case] expected_msg: String) {
        let msg_maybe = extract_message_docker(line_str.as_bytes());
        assert_eq!(msg_maybe, Some(expected_msg.into_bytes()));
    }
}
