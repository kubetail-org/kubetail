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

//! Where the app is in quitting. Every Quit path — the tray, the app menu, the
//! Dock's re-drive, the webview's command — reaches the same `app.exit(0)`, so
//! a second Quit during the wait arrives as one more `ExitRequested`. Letting it
//! through would run `RunEvent::Exit`, which kills the sidecar mid-stop.

use std::sync::{Mutex, MutexGuard};

/// What `ExitRequested` does with a request that carries a code.
#[derive(Debug, PartialEq, Eq)]
pub enum ExitDecision {
    /// The first request: prevent it, hide the windows, and wait for the sidecar.
    Start,
    /// A request while waiting: ignored, not a force quit.
    Prevent,
    /// The waiting thread's own exit, once the wait is over.
    Allow,
}

#[derive(Default)]
enum State {
    #[default]
    Running,
    Waiting,
    Done,
}

#[derive(Default)]
pub struct Quit(Mutex<State>);

impl Quit {
    /// Decides an exit request, moving *running* to *waiting* on the first.
    pub fn request(&self) -> ExitDecision {
        let mut state = self.state();
        match *state {
            State::Running => {
                *state = State::Waiting;
                ExitDecision::Start
            }
            State::Waiting => ExitDecision::Prevent,
            State::Done => ExitDecision::Allow,
        }
    }

    /// Ends the wait, so the next request goes through.
    pub fn finish(&self) {
        *self.state() = State::Done;
    }

    fn state(&self) -> MutexGuard<'_, State> {
        self.0.lock().unwrap_or_else(|p| p.into_inner())
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn the_first_request_starts_the_wait() {
        let quit = Quit::default();
        assert_eq!(quit.request(), ExitDecision::Start);
    }

    #[test]
    fn a_request_while_waiting_is_prevented() {
        let quit = Quit::default();
        quit.request();
        assert_eq!(quit.request(), ExitDecision::Prevent);
    }

    #[test]
    fn the_request_after_the_wait_goes_through() {
        let quit = Quit::default();
        quit.request();
        quit.finish();
        assert_eq!(quit.request(), ExitDecision::Allow);
    }
}
