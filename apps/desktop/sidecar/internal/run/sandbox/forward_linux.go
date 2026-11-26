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

import "golang.org/x/sys/unix"

// forwarderTasks is how many tasks a run's process limit holds for the
// forwarder: the kernel counts each of its threads, and with one P its
// runtime keeps them to a handful (TestTheForwarderStaysUnderItsTasks).
const forwarderTasks = 32

// guardMemory makes the forwarder non-dumpable, so nothing under the filter
// can read or write its memory through /proc/<pid>/mem or ptrace: the shell
// has no capability in bwrap's user namespace to override that.
func guardMemory() error { // coverage:ignore only the sandbox's PID 1 calls this, and it writes no coverage
	return unix.Prctl(unix.PR_SET_DUMPABLE, 0, 0, 0, 0)
}
