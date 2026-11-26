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
	"path/filepath"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"
)

// Each refusal ShellMain makes before the exec answers, in this process.
func TestShellMainAnswersWhatItCannotRun(t *testing.T) {
	assert.Equal(t, initFailed, ShellMain([]string{"--memory", "1", "--", "/bin/sh"}), "a memory limit")
	assert.Equal(t, initFailed, ShellMain([]string{"--", filepath.Join(t.TempDir(), "missing")}), "a missing command")
}

// An exec the kernel refuses returns, so it answers too. The process limit is
// raised to the hard one, which changes nothing the test needs, and restored.
// Serial: it sets this process's limit.
func TestShellMainAnswersAnExecTheKernelRefuses(t *testing.T) {
	var own unix.Rlimit
	require.NoError(t, unix.Getrlimit(unix.RLIMIT_NPROC, &own))
	t.Cleanup(func() { _ = unix.Setrlimit(unix.RLIMIT_NPROC, &own) })
	if own.Max == unix.RLIM_INFINITY {
		t.Skip("the process hard limit is unlimited")
	}

	assert.Equal(t, initFailed, ShellMain([]string{"--processes", strconv.FormatUint(own.Max, 10), "--", garbage(t)}))
}

// garbage is an executable in no format the kernel runs, so execve fails
// with ENOEXEC.
func garbage(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "garbage")
	require.NoError(t, os.WriteFile(path, []byte{0, 1, 2, 3}, 0o700))
	return path
}
