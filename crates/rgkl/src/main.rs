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

use std::{io::stdout, process::ExitCode, thread};

use chrono::{DateTime, Utc};
use clap::{Parser, Subcommand};
use signal_hook::{
    consts::{SIGINT, SIGTERM},
    iterator::Signals,
};

use rgkl::stream_forward::FollowFrom;
use rgkl::{stream_backward, stream_forward, z};

mod error;

// See https://github.com/BurntSushi/ripgrep/blob/master/crates/core/main.rs#L19
#[cfg(all(target_env = "musl", target_pointer_width = "64"))]
#[global_allocator]
static ALLOC: jemallocator::Jemalloc = jemallocator::Jemalloc;

#[derive(Parser, Debug)]
#[command(version, about = "Grep tool for Kubernetes log files, written in Rust")]
struct Cli {
    #[command(subcommand)]
    command: Commands,
}

#[derive(Subcommand, Debug)]
enum Commands {
    StreamForward {
        file: String,

        #[arg(long, value_parser = clap::value_parser!(DateTime<Utc>))]
        start_time: Option<DateTime<Utc>>,

        #[arg(long, value_parser = clap::value_parser!(DateTime<Utc>))]
        stop_time: Option<DateTime<Utc>>,

        #[arg(short, long, default_value = "")]
        grep: String,

        #[arg(long, value_enum, default_value_t = FollowFrom::Noop)]
        follow_from: FollowFrom,
    },
    StreamBackward {
        file: String,

        #[arg(long, value_parser = clap::value_parser!(DateTime<Utc>))]
        start_time: Option<DateTime<Utc>>,

        #[arg(long, value_parser = clap::value_parser!(DateTime<Utc>))]
        stop_time: Option<DateTime<Utc>>,

        #[arg(short, long, default_value = "")]
        grep: String,
    },
    Z {
        file: String,

        #[arg(short, long, default_value = ".*")]
        query: String,
    },
}

fn main() -> ExitCode {
    let (term_tx, term_rx) = crossbeam_channel::unbounded();

    // Listen for kill signals as early as possible
    match Signals::new([SIGINT, SIGTERM]) {
        Ok(mut signals) => {
            thread::spawn(move || {
                let _ = signals.wait().next();
                drop(term_tx);
            });
        }
        Err(err) => {
            eprintln!("Error: {:#}", err);
            return ExitCode::FAILURE;
        }
    }

    let cli = Cli::parse();

    match &cli.command {
        Commands::StreamForward {
            file,
            start_time,
            stop_time,
            grep,
            follow_from,
        } => {
            let mut stdout = stdout().lock();
            match stream_forward::run(
                file,
                *start_time,
                *stop_time,
                grep,
                *follow_from,
                term_rx,
                &mut stdout,
            ) {
                Ok(_) => ExitCode::SUCCESS,
                Err(err) => {
                    eprintln!("Error: {:#}", err);
                    ExitCode::FAILURE
                }
            }
        }
        Commands::StreamBackward {
            file,
            start_time,
            stop_time,
            grep,
        } => {
            let mut stdout = stdout().lock();
            match stream_backward::run(file, *start_time, *stop_time, grep, term_rx, &mut stdout) {
                Ok(_) => ExitCode::SUCCESS,
                Err(err) => {
                    eprintln!("Error: {:#}", err);
                    ExitCode::FAILURE
                }
            }
        }
        Commands::Z { file, query } => z::run(file, query),
    }
}
