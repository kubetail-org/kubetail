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

//go:build !windows

package sandbox

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"
)

func init() {
	helpers["rlimits"] = func() int {
		fmt.Println(rlimits())
		return 0
	}
}

// rlimits is this process's CPU, open-files, process and core limits, each
// soft/hard, as the resource's name and the values, "-" for unlimited.
func rlimits() string {
	var b strings.Builder
	for _, r := range []struct {
		name     string
		resource int
	}{{"cpu", unix.RLIMIT_CPU}, {"files", unix.RLIMIT_NOFILE}, {"processes", unix.RLIMIT_NPROC}, {"core", unix.RLIMIT_CORE}} {
		var l unix.Rlimit
		if err := unix.Getrlimit(r.resource, &l); err != nil {
			return err.Error()
		}
		fmt.Fprintf(&b, "%s=%s/%s ", r.name, limitText(l.Cur), limitText(l.Max))
	}
	return strings.TrimSpace(b.String())
}

func limitText(v uint64) string {
	if v == unix.RLIM_INFINITY {
		return "-"
	}
	return strconv.FormatUint(v, 10)
}

// ownProcesses is a process limit that changes nothing the test or its
// runtime needs: the test's own soft one, or 1048576 when that is unlimited.
func ownProcesses(t *testing.T) int {
	t.Helper()
	var own unix.Rlimit
	require.NoError(t, unix.Getrlimit(unix.RLIMIT_NPROC, &own))
	if own.Cur == unix.RLIM_INFINITY {
		return 1048576
	}
	return int(own.Cur)
}

// limitsOf is the limits sandbox-shell started with args, after a shell ran
// first, gives what it execs, the test binary as the rlimits helper.
func limitsOf(t *testing.T, first string, args ...string) string {
	t.Helper()
	quoted := make([]string, len(args))
	for i, a := range args {
		quoted[i] = "'" + a + "'"
	}
	self := "'" + os.Args[0] + "'"
	cmd := exec.Command("/bin/sh", "-c", first+"; exec "+self+" "+ShellCommand+" "+strings.Join(quoted, " ")+" -- "+self)
	cmd.Env = append(os.Environ(), "KUBETAIL_SANDBOX_TEST_HELPER=rlimits")
	code, stdout, stderr := runInit(t, cmd)
	require.Equal(t, 0, code, stderr)
	return strings.TrimSpace(stdout)
}

func TestTheShellSetsTheLimits(t *testing.T) {
	p := strconv.Itoa(ownProcesses(t))

	got := limitsOf(t, "true", "--cpu", "7", "--cpu-grace", "5", "--files", "64", "--processes", p)

	assert.Contains(t, got, "cpu=7/12 files=64/64 processes="+p+"/"+p)
}

// A limit above sandbox-shell's own hard one is lowered to it, which an
// unprivileged process cannot raise.
func TestTheShellLowersALimitToItsOwnHardOne(t *testing.T) {
	got := limitsOf(t, "ulimit -n 512", "--files", "4096")

	assert.Contains(t, got, "files=512/512")
}

// With no room above the CPU limit for the grace, soft and hard are equal.
func TestTheCPULimitWithNoRoomHasNoGrace(t *testing.T) {
	got := limitsOf(t, "ulimit -t 7", "--cpu", "600")

	assert.Contains(t, got, "cpu=7/7")
}

func TestClampedAddsTheGraceUnderItsOwnHardLimit(t *testing.T) {
	var own unix.Rlimit
	require.NoError(t, unix.Getrlimit(unix.RLIMIT_CPU, &own))

	got, err := clamped(unix.RLIMIT_CPU, 7, cpuGrace)

	require.NoError(t, err)
	assert.Equal(t, unix.Rlimit{Cur: min(7, own.Max), Max: min(7+cpuGrace, own.Max)}, got)
}

func TestClampedLowersAValueToItsOwnHardLimit(t *testing.T) {
	var own unix.Rlimit
	require.NoError(t, unix.Getrlimit(unix.RLIMIT_NOFILE, &own))
	if own.Max == unix.RLIM_INFINITY {
		t.Skip("the open-files hard limit is unlimited")
	}

	got, err := clamped(unix.RLIMIT_NOFILE, int(own.Max)+1, 0)

	require.NoError(t, err)
	assert.Equal(t, unix.Rlimit{Cur: own.Max, Max: own.Max}, got)
}

func TestClampedFailsOnAnUnknownResource(t *testing.T) {
	_, err := clamped(-1, 7, 0)

	assert.ErrorIs(t, err, unix.EINVAL)
}

// The open-files limit is set soft and hard. At the soft limit the process
// runs under, nothing it can open changes.
func TestSetTimeAndFilesSetsTheOpenFilesLimit(t *testing.T) {
	var own unix.Rlimit
	require.NoError(t, unix.Getrlimit(unix.RLIMIT_NOFILE, &own))
	t.Cleanup(func() { _ = unix.Setrlimit(unix.RLIMIT_NOFILE, &own) })

	require.NoError(t, setTimeAndFiles(shellArgs{files: int(own.Cur)}))

	var got unix.Rlimit
	require.NoError(t, unix.Getrlimit(unix.RLIMIT_NOFILE, &got))
	assert.Equal(t, unix.Rlimit{Cur: own.Cur, Max: own.Cur}, got)
}

// The CPU limit is set soft and hard, the grace between them. Set far past
// any time a test runs, it changes nothing a test notices; the hard limit it
// lowers stays lowered for the rest of the process.
func TestSetTimeAndFilesSetsTheCPULimit(t *testing.T) {
	var own unix.Rlimit
	require.NoError(t, unix.Getrlimit(unix.RLIMIT_CPU, &own))
	const far = 1 << 40 // seconds

	require.NoError(t, setTimeAndFiles(shellArgs{cpu: far, cpuGrace: cpuGrace}))

	var got unix.Rlimit
	require.NoError(t, unix.Getrlimit(unix.RLIMIT_CPU, &got))
	assert.Equal(t, unix.Rlimit{Cur: min(far, own.Max), Max: min(far+cpuGrace, own.Max)}, got)
}

// A run with neither limit sets none.
func TestSetTimeAndFilesWithNoLimitsSetsNone(t *testing.T) {
	assert.NoError(t, setTimeAndFiles(shellArgs{}))
}

func TestSetClampedFailsOnAnUnknownResource(t *testing.T) {
	assert.ErrorIs(t, setClamped(-1, 7, 0), unix.EINVAL)
}
