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

// PassThroughMatcher
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
        if start <= haystack.len() {
            Ok(Some(Match::new(start, haystack.len())))
        } else {
            Ok(None)
        }
    }

    fn new_captures(&self) -> Result<Self::Captures, Self::Error> {
        Ok(matcher::NoCaptures::new())
    }
}

// LogFileRegexMatcher
pub struct LogFileRegexMatcher {
    inner: RegexMatcher,
}

impl LogFileRegexMatcher {
    pub fn new(inner_pattern: &str) -> Result<LogFileRegexMatcher, regex::Error> {
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

        Ok(LogFileRegexMatcher { inner })
    }
}

impl Matcher for LogFileRegexMatcher {
    type Captures = regex::RegexCaptures;
    type Error = matcher::NoError;

    fn find_at(&self, haystack: &[u8], start: usize) -> Result<Option<Match>, Self::Error> {
        // Start + 19 starts looking after the non-decimal part of ISO8601 timestamp
        if let Some(offset) = find_log_message_start(haystack, start + 19) {
            self.inner.find_at(haystack, offset)
        } else {
            Ok(None)
        }
    }

    fn new_captures(&self) -> Result<Self::Captures, Self::Error> {
        self.inner.new_captures()
    }
}

pub fn find_log_message_start(haystack: &[u8], start: usize) -> Option<usize> {
    if start >= haystack.len() {
        return None;
    }

    // Define the literal part that follows the timestamp.
    // The prefix is: "<ISO8601 timestamp> stdout f "
    // We assume the timestamp is of variable length and we just search for " stdout f ".

    // Search for the literal in the haystack.
    memmem::find(&haystack[start..], b" ").map(|pos| start + pos + 10)
}
