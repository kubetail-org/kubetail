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

package filelock

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// childEnv starts the test binary as a process that locks the file it names,
// says so on stdout, and holds the lock until it is killed or its stdin closes.
const childEnv = "KUBETAIL_FILELOCK_TEST_CHILD"

func TestMain(m *testing.M) {
	if path := os.Getenv(childEnv); path != "" {
		f, err := TryLock(path)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
		fmt.Println("locked")
		_, _ = io.Copy(io.Discard, os.Stdin)
		_ = f.Close()
		os.Exit(0)
	}
	os.Exit(m.Run())
}

// A second open of a locked file is refused, in the same process too, and
// takes the lock once the first is closed. The file is created when missing.
func TestASecondLockIsRefusedUntilTheFirstCloses(t *testing.T) {
	path := filepath.Join(t.TempDir(), "x.lock")
	first, err := TryLock(path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = first.Close() })

	_, err = TryLock(path)
	require.ErrorIs(t, err, ErrLocked)

	require.NoError(t, first.Close())
	second, err := TryLock(path)
	require.NoError(t, err)
	require.NoError(t, second.Close())
}

// A file that cannot be opened is that error, not ErrLocked.
func TestAFileThatCannotBeOpenedIsAnError(t *testing.T) {
	_, err := TryLock(filepath.Join(t.TempDir(), "missing", "x.lock"))
	require.Error(t, err)
	assert.NotErrorIs(t, err, ErrLocked)
}

// Another process's lock refuses this one's, and a process killed while it
// holds the lock leaves the file lockable: the kernel drops the lock.
func TestAKilledHolderLeavesTheFileLockable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "x.lock")
	exe, err := os.Executable()
	require.NoError(t, err)
	cmd := exec.Command(exe)
	cmd.Env = append(os.Environ(), childEnv+"="+path)
	stdin, err := cmd.StdinPipe()
	require.NoError(t, err)
	stdout, err := cmd.StdoutPipe()
	require.NoError(t, err)
	cmd.Stderr = os.Stderr
	require.NoError(t, cmd.Start())
	t.Cleanup(func() {
		_ = stdin.Close()
		_ = cmd.Wait()
	})
	line, err := bufio.NewReader(stdout).ReadString('\n')
	require.NoError(t, err)
	require.Equal(t, "locked\n", line)

	_, err = TryLock(path)
	require.ErrorIs(t, err, ErrLocked)

	require.NoError(t, cmd.Process.Kill())
	_ = cmd.Wait()
	f, err := TryLock(path)
	require.NoError(t, err)
	require.NoError(t, f.Close())
}
