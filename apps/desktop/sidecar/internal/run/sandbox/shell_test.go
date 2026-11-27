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
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The command follows a --, and there must be one. With no --cpu-grace, the
// grace is cpuGrace.
func TestParseShellArgs(t *testing.T) {
	t.Parallel()

	got, err := parseShellArgs([]string{"--", "/bin/sh", "-c", "true"})
	require.NoError(t, err)
	assert.Equal(t, shellArgs{cpuGrace: cpuGrace, argv: []string{"/bin/sh", "-c", "true"}}, got)
	for _, args := range [][]string{nil, {"--"}, {"/bin/sh"}, {"--memory", "1", "/bin/sh"}} {
		_, err := parseShellArgs(args)
		assert.Error(t, err, args)
	}
}

func TestShellArgsReadTheLimits(t *testing.T) {
	t.Parallel()

	got, err := parseShellArgs([]string{
		"--cpu", "7", "--cpu-grace", "5", "--files", "64", "--memory", "536870912", "--processes", "128", "--", "/bin/sh", "--memory", "1",
	})
	require.NoError(t, err)
	assert.Equal(t, shellArgs{cpu: 7, cpuGrace: 5, files: 64, memory: 536870912, processes: 128, argv: []string{"/bin/sh", "--memory", "1"}}, got)

	for _, args := range [][]string{
		{"--memory", "-1", "--", "x"}, {"--processes", "x", "--", "x"}, {"--memory", "0x10", "--", "x"}, {"--cpu", "", "--", "x"},
		{"--other", "1", "--", "x"}, {"x", "--", "y"},
	} {
		_, err := parseShellArgs(args)
		assert.Error(t, err, args)
	}
}

// sandbox-shell's command line carries each limit that is set, and parses
// back to them, the process limit holding forwarderTasks more, since the
// forwarder starts before it is set.
func TestShellCommandRoundTrip(t *testing.T) {
	t.Parallel()

	assert.Equal(t, []string{ShellCommand, "--"}, shellCommand(Limits{}, 3))

	args := shellCommand(Limits{CPUSeconds: 7, MemoryBytes: 1 << 30, OpenFiles: 64, Processes: 9}, 3)
	require.Equal(t, ShellCommand, args[0])
	got, err := parseShellArgs(append(args[1:], "/bin/sh"))
	require.NoError(t, err)
	assert.Equal(t, shellArgs{cpu: 7, cpuGrace: 3, files: 64, memory: 1 << 30, processes: 9 + forwarderTasks, argv: []string{"/bin/sh"}}, got)
}
