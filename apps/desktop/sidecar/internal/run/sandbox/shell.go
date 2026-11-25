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

// kubetail-sidecar sandbox-shell: the process between a run's forwarder and its
// shell, which sets the run's limits and, on Linux, installs the filter
// (seccomp_linux.go) before exec'ing the shell, so both hold the shell and
// everything under it and neither holds the forwarder.
package sandbox

import (
	"errors"
	"flag"
	"io"
	"slices"
	"strconv"
)

// ShellCommand is the subcommand that makes this executable a run's shell
// launcher.
const ShellCommand = "sandbox-shell"

// shellArgs is sandbox-shell's command line. Each limit is 0 for the
// platform's.
type shellArgs struct {
	cpu       int      // CPU seconds per process
	cpuGrace  int      // how far the hard CPU limit sits above cpu
	files     int      // open descriptors per process
	memory    int      // the address space each process may map
	processes int      // the kernel's count of tasks (Linux) or processes (macOS)
	argv      []string // the command to exec
}

// shellCommand is sandbox-shell's command line for limits l, after the
// executable: ShellCommand, each limit that is set, the CPU limit with its
// grace, and the -- the shell follows. The forwarder starts before the limits
// are set, so the process limit holds forwarderTasks over l's.
func shellCommand(l Limits, grace int) []string {
	args := []string{ShellCommand}
	add := func(flag string, v int) {
		if v > 0 {
			args = append(args, flag, strconv.Itoa(v))
		}
	}
	add("--cpu", l.CPUSeconds)
	if l.CPUSeconds > 0 {
		add("--cpu-grace", grace)
	}
	add("--files", l.OpenFiles)
	add("--memory", l.MemoryBytes)
	if l.Processes > 0 {
		add("--processes", l.Processes+forwarderTasks)
	}
	return append(args, "--")
}

// parseShellArgs reads [--cpu <s>] [--cpu-grace <s>] [--files <n>]
// [--memory <bytes>] [--processes <n>] -- <argv…>. The grace defaults to
// cpuGrace.
func parseShellArgs(args []string) (shellArgs, error) {
	i := slices.Index(args, "--")
	if i < 0 || i == len(args)-1 {
		return shellArgs{}, errors.New("no command after --")
	}
	a := shellArgs{cpuGrace: cpuGrace, argv: args[i+1:]}
	fs := flag.NewFlagSet(ShellCommand, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.Func("cpu", "", func(v string) (err error) { a.cpu, err = count(v); return err })
	fs.Func("cpu-grace", "", func(v string) (err error) { a.cpuGrace, err = count(v); return err })
	fs.Func("files", "", func(v string) (err error) { a.files, err = count(v); return err })
	fs.Func("memory", "", func(v string) (err error) { a.memory, err = count(v); return err })
	fs.Func("processes", "", func(v string) (err error) { a.processes, err = count(v); return err })
	if err := fs.Parse(args[:i]); err != nil {
		return shellArgs{}, err
	}
	if fs.NArg() > 0 {
		return shellArgs{}, errors.New("an argument before --")
	}
	return a, nil
}
