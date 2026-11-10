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

use std::io::Read;

use grep::printer::{JSONBuilder, JSON};
use grep::searcher::{MmapChoice, Searcher, SearcherBuilder};
use tokio::sync::mpsc::Sender;
use tokio_util::sync::CancellationToken;
use tonic::Status;
use types::cluster_agent::LogRecord;

use crate::util::format::FileFormat;
use crate::util::matcher::{LogFileRegexMatcher, PassThroughMatcher};
use crate::util::writer::{process_output, CallbackWriter};
use crate::watcher_error::WatcherError;

/// Greps log lines and sends each match to the stream's sender as a `LogRecord`.
pub struct LogSearcher<F>
where
    F: Fn(Vec<u8>),
{
    searcher: Searcher,
    printer: JSON<CallbackWriter<F>>,
    /// `None` when there is no grep: every line matches.
    matcher: Option<LogFileRegexMatcher>,
}

/// Builds the searcher both streams use. A blank `grep` matches every line.
pub fn new_log_searcher<'a>(
    ctx: CancellationToken,
    format: FileFormat,
    grep: Option<&str>,
    sender: &'a Sender<Result<LogRecord, Status>>,
) -> Result<LogSearcher<impl Fn(Vec<u8>) + use<'a>>, WatcherError> {
    let matcher = grep
        .map(str::trim)
        .filter(|grep| !grep.is_empty())
        .map(|grep| LogFileRegexMatcher::new(grep, format))
        .transpose()?;

    let searcher = SearcherBuilder::new()
        .line_number(false)
        .memory_map(MmapChoice::never())
        .multi_line(false)
        .heap_limit(Some(1024 * 1024)) // TODO: Make this configurable
        .build();

    let writer = CallbackWriter::new(move |chunk: Vec<u8>| {
        process_output(ctx.clone(), chunk, sender, format)
    });

    Ok(LogSearcher {
        searcher,
        printer: JSONBuilder::new().build(writer),
        matcher,
    })
}

impl<F> LogSearcher<F>
where
    F: Fn(Vec<u8>),
{
    pub fn search_reader<R: Read>(&mut self, reader: R) {
        if let Some(matcher) = &self.matcher {
            let sink = self.printer.sink(matcher);
            let _ = self.searcher.search_reader(matcher, reader, sink);
        } else {
            let matcher = PassThroughMatcher::new();
            let sink = self.printer.sink(&matcher);
            let _ = self.searcher.search_reader(&matcher, reader, sink);
        }
    }

    pub fn search_slice(&mut self, slice: &[u8]) {
        if let Some(matcher) = &self.matcher {
            let sink = self.printer.sink(matcher);
            let _ = self.searcher.search_slice(matcher, slice, sink);
        } else {
            let matcher = PassThroughMatcher::new();
            let sink = self.printer.sink(&matcher);
            let _ = self.searcher.search_slice(&matcher, slice, sink);
        }
    }
}

#[cfg(test)]
mod test {
    #![allow(clippy::unwrap_used)]

    use rstest::rstest;
    use tokio::sync::mpsc;

    use super::*;

    const LINES: &[u8] = b"2024-10-01T05:40:46.960135302Z stdout F hello world\n\
2024-10-01T05:40:48.840712595Z stdout F goodbye\n";

    #[tokio::test(flavor = "multi_thread")]
    #[rstest]
    #[case(None, vec!["hello world", "goodbye"])]
    #[case(Some("  "), vec!["hello world", "goodbye"])]
    #[case(Some(" hello world "), vec!["hello world"])]
    #[case(Some("nomatch"), vec![])]
    async fn test_search(#[case] grep: Option<&str>, #[case] expected: Vec<&str>) {
        let (tx, mut rx) = mpsc::channel(10);

        let mut searcher =
            new_log_searcher(CancellationToken::new(), FileFormat::CRI, grep, &tx).unwrap();
        searcher.search_reader(LINES);
        searcher.search_slice(LINES);
        drop(searcher);
        drop(tx);

        let mut messages = Vec::new();
        while let Some(record) = rx.recv().await {
            messages.push(record.unwrap().message);
        }
        let twice: Vec<&str> = expected.iter().chain(expected.iter()).copied().collect();
        assert_eq!(messages, twice);
    }

    #[test]
    fn test_invalid_grep_is_an_error() {
        let (tx, _rx) = mpsc::channel(1);
        let result = new_log_searcher(CancellationToken::new(), FileFormat::CRI, Some("("), &tx);
        assert!(matches!(result, Err(WatcherError::InvalidGrep(_))));
    }
}
