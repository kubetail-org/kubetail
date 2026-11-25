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

//go:build race

package testutil

import (
	"os"
	"runtime"
	"syscall"
)

// raceExitCode is the race detector's default exit code for a run that raced.
const raceExitCode = 66

// ExitHelper ends a test binary started as a helper with code. It exits by
// syscall.Exit, which skips the second os.Exit sleeps under -race and also
// os.Exit's hooks, so a -race -cover helper writes no coverage. A helper that
// raced still fails: os.Exit reports the race and exits raceExitCode.
func ExitHelper(code int) { // coverage:ignore built only under -race, which coverage runs leave out
	if runtime.RaceErrors() > 0 {
		os.Exit(raceExitCode)
	}
	syscall.Exit(code)
}
