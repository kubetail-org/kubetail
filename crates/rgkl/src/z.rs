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

use std::process::ExitCode;

use grep::{
    cli::stdout, printer::StandardBuilder, regex::RegexMatcherBuilder, searcher::SearcherBuilder,
};
use grep_searcher::MmapChoice;
use termcolor::ColorChoice;

pub fn run(path: &str, query: &str) -> ExitCode {
    let matcher = RegexMatcherBuilder::new()
        .line_terminator(Some(b'\n'))
        .case_smart(false)
        .case_insensitive(false)
        .build(query)
        .unwrap();

    let mut searcher = SearcherBuilder::new()
        .line_number(false)
        .memory_map(MmapChoice::never())
        .build();

    let stdout = stdout(ColorChoice::Never);

    let mut printer = StandardBuilder::new().build(stdout);

    let mut sink = printer.sink(&matcher);

    let _ = searcher.search_path(&matcher, path, &mut sink);

    ExitCode::SUCCESS
}
