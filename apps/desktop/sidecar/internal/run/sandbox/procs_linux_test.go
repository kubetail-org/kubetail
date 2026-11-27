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
	"fmt"
	"os"
	"runtime"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"
)

func init() {
	// threads holds 64 threads, makes itself non-dumpable, so root owns its
	// /proc entry, and waits for its input to end.
	helpers["threads"] = func() int {
		var locked sync.WaitGroup
		for range 64 {
			locked.Add(1)
			go func() {
				runtime.LockOSThread()
				locked.Done()
				select {}
			}()
		}
		locked.Wait()
		if err := unix.Prctl(unix.PR_SET_DUMPABLE, 0, 0, 0, 0); err != nil {
			fmt.Println(err)
			return 1
		}
		fmt.Println("ready")
		_, _ = bufio.NewReader(os.Stdin).ReadString('\n')
		return 0
	}
}

// A process's threads are counted by the real uid in its status, though its
// /proc entry is root's.
func TestThreadsAreCounted(t *testing.T) {
	t.Parallel()

	cmd := helperCmd("threads")
	in, err := cmd.StdinPipe()
	require.NoError(t, err)
	out, err := cmd.StdoutPipe()
	require.NoError(t, err)
	require.NoError(t, cmd.Start())
	t.Cleanup(func() { _ = in.Close(); _ = cmd.Wait() })
	line, err := bufio.NewReader(out).ReadString('\n')
	require.NoError(t, err)
	require.Equal(t, "ready\n", line)

	assert.GreaterOrEqual(t, countThreads([]int{cmd.Process.Pid}), 64)
	assert.Zero(t, countThreads([]int{0}), "a pid with no process is skipped")
}

// The base is 0 only on a kernel that counts per namespace, in a run whose
// namespace is its own; anything else scans.
func TestTheBaseIsZeroOnlyInANamespaceOfItsOwn(t *testing.T) {
	t.Parallel()

	for _, s := range []*Sandbox{{perNamespace: true}, {ownUserNS: true}} {
		n, err := s.CountedProcesses(false)
		require.NoError(t, err)
		assert.GreaterOrEqual(t, n, 1, "%+v", s)
	}

	n, err := (&Sandbox{perNamespace: true, ownUserNS: true}).CountedProcesses(false)
	require.NoError(t, err)
	assert.Equal(t, 0, n)

	// A run with the internet asks of pasta's namespace, not bwrap's.
	n, err = (&Sandbox{perNamespace: true, ownUserNS: true}).CountedProcesses(true)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, n, 1)
	n, err = (&Sandbox{perNamespace: true, pastaOwnUserNS: true}).CountedProcesses(true)
	require.NoError(t, err)
	assert.Equal(t, 0, n)
}

func TestANewNamespaceCountsNothing(t *testing.T) {
	t.Parallel()

	s := confining(t)
	if !s.perNamespace || !s.ownUserNS {
		t.Skip("the kernel counts the user's whole machine, or the run has no user namespace of its own")
	}

	n, err := s.CountedProcesses(false)

	require.NoError(t, err)
	assert.Equal(t, 0, n)
}
