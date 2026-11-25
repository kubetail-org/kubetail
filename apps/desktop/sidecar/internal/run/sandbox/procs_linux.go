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
	"bufio"
	"os"
	"strconv"
	"strings"
)

// CountedProcesses is how many tasks the kernel already counts against a run
// that starts now, with the internet or without: none in a user namespace of
// the run's own on a kernel that counts per namespace, else every thread of
// the user's that this process's /proc shows. Any doubt scans, since a base
// too high loosens the limit while 0 under a machine-wide count would refuse
// the shell.
func (s *Sandbox) CountedProcesses(internet bool) (int, error) {
	own := s.ownUserNS
	if internet {
		own = s.pastaOwnUserNS
	}
	if s.perNamespace && own {
		return 0, nil
	}
	proc, err := os.Open("/proc")
	if err != nil { // coverage:ignore /proc is mounted wherever the sidecar runs on Linux
		return 0, err
	}
	names, err := proc.Readdirnames(-1)
	_ = proc.Close()
	if err != nil { // coverage:ignore /proc is mounted wherever the sidecar runs on Linux
		return 0, err
	}
	var pids []int
	for _, name := range names {
		if pid, err := strconv.Atoi(name); err == nil {
			pids = append(pids, pid)
		}
	}
	return countThreads(pids), nil
}

// countThreads is how many tasks the kernel counts against this process's
// real uid among pids: the Threads of each whose status names that uid as its
// real one. It never reads who owns /proc/<pid>, the effective uid, which is
// root's for a non-dumpable process. A process gone before its read is
// skipped.
func countThreads(pids []int) int {
	uid := strconv.Itoa(os.Getuid())
	n := 0
	for _, pid := range pids {
		n += threadsOf(pid, uid)
	}
	return n
}

// threadsOf is the Threads line of pid's status when its real uid is uid,
// else 0. Uid comes before Threads, so another user's process is read no
// further.
func threadsOf(pid int, uid string) int {
	f, err := os.Open("/proc/" + strconv.Itoa(pid) + "/status")
	if err != nil {
		return 0
	}
	defer f.Close()
	lines := bufio.NewScanner(f)
	for lines.Scan() {
		key, value, _ := strings.Cut(lines.Text(), ":")
		switch key {
		case "Uid":
			if fields := strings.Fields(value); len(fields) == 0 || fields[0] != uid {
				return 0
			}
		case "Threads":
			threads, _ := strconv.Atoi(strings.TrimSpace(value))
			return threads
		}
	}
	return 0 // coverage:ignore every /proc/<pid>/status holds a Threads line
}
