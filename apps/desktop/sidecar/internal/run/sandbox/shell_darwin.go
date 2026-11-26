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

package sandbox

import (
	"errors"
	"os"
	"os/exec"

	"golang.org/x/sys/unix"
)

// ShellMain is sandbox-shell, given the arguments after the subcommand: it
// sets the run's limits and execs the command under them. It answers only if
// it cannot. macOS counts processes, never threads, so nothing between the
// process limit and the exec can trip it.
func ShellMain(args []string) int {
	a, err := parseShellArgs(args)
	if err != nil {
		return fail(os.Stderr, ShellCommand, "bad arguments", err)
	}
	if a.memory > 0 {
		return fail(os.Stderr, ShellCommand, "bad arguments", errNoMemoryLimit)
	}
	path, err := exec.LookPath(a.argv[0])
	if err != nil {
		return fail(os.Stderr, ShellCommand, "cannot start "+a.argv[0], err)
	}
	if err := setTimeAndFiles(a); err != nil { // coverage:ignore each limit is clamped to the process's own hard one, which the kernel accepts, and a test reaching this would set its own process's limits
		return fail(os.Stderr, ShellCommand, "cannot set limits", err)
	}
	if a.processes > 0 {
		if err := setClamped(unix.RLIMIT_NPROC, a.processes, 0); err != nil { // coverage:ignore each limit is clamped to the process's own hard one, which the kernel accepts, and a test reaching this would set its own process's limits
			return fail(os.Stderr, ShellCommand, "cannot set limits", err)
		}
	}
	err = unix.Exec(path, a.argv, os.Environ())
	return fail(os.Stderr, ShellCommand, "cannot start "+a.argv[0], err)
}

// errNoMemoryLimit refuses a memory limit, which no macOS process can set
// usefully: every one already maps the shared region, about 466 GiB.
var errNoMemoryLimit = errors.New("no memory limit on macOS")
