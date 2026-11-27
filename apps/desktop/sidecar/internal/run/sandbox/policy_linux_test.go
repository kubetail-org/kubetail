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
	"os"
	"os/exec"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A process that ignores SIGXCPU is killed at the hard limit, the grace past
// the soft one.
func TestAnIgnoredSIGXCPUIsKilledAtTheHardLimit(t *testing.T) {
	t.Parallel()

	s := confining(t)
	s.testCPUGrace = 1

	code, _ := limitedRun(t, s, Limits{CPUSeconds: 1}, nil, "/bin/sh", "-c", "trap '' XCPU; while :; do :; done")

	assert.Equal(t, 137, code)
}

// An allocation past the memory limit fails, where with none it succeeds.
func TestAllocatingPastTheMemoryLimitFails(t *testing.T) {
	t.Parallel()

	s := confining(t)
	dd := []string{"/bin/sh", "-c", "dd if=/dev/zero of=/dev/null bs=1G count=1 2>&1"}

	code, out := limitedRun(t, s, Limits{}, nil, dd...)
	require.Equal(t, 0, code, out)

	code, out = limitedRun(t, s, Limits{MemoryBytes: 512 << 20, OpenFiles: 4096}, nil, dd...)
	assert.NotEqual(t, 0, code)
	assert.Contains(t, out, "memory", "dd says why it stopped")
}

// A fork loop stops at the run's process limit, which counts the run's own
// namespace alone, and holds forwarderTasks for the forwarder: the test starts
// a process while the run is full.
func TestAForkLoopStopsAtTheProcessLimit(t *testing.T) {
	t.Parallel()

	if os.Getuid() == 0 {
		t.Skip("the kernel holds root to no process limit")
	}
	s := confining(t)
	if n, err := s.CountedProcesses(false); err != nil || n != 0 {
		t.Skip("the run has no namespace of its own to count")
	}

	n, eagain, more := forkLoop(t, s, Limits{Processes: 128, OpenFiles: 4096}, 256, func() error {
		return exec.Command("/bin/true").Run()
	})

	assert.Less(t, n, 128+forwarderTasks)
	assert.True(t, eagain)
	assert.NoError(t, more, "a process outside the run")
}
