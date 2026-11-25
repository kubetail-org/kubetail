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
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"testing/iotest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// What pasta and bwrap wrote for a run that never started is kept for it.
func TestUntilStartedKeepsTheOutputOfARunThatNeverStarted(t *testing.T) {
	assert.Equal(t, "External interface not usable\n",
		string(untilStarted(strings.NewReader("External interface not usable\n"))))
}

// Once the run has started, pasta's notices before it and anything after are
// dropped, and the pipe is read to its end, so pasta never blocks on it.
func TestUntilStartedDropsTheOutputOfARunThatStarted(t *testing.T) {
	r := strings.NewReader("Couldn't get any nameserver address\n" + runStarted + "later\n")

	assert.Nil(t, untilStarted(r))
	assert.Zero(t, r.Len())
}

// A marker split across reads is still found.
func TestUntilStartedFindsASplitMarker(t *testing.T) {
	assert.Nil(t, untilStarted(iotest.OneByteReader(strings.NewReader("notice\n"+runStarted))))
}

// A run that never started reports at most pastaOutputLimit bytes.
func TestUntilStartedKeepsAtMostTheLimit(t *testing.T) {
	got := untilStarted(iotest.OneByteReader(strings.NewReader(strings.Repeat("x", pastaOutputLimit+10))))

	assert.Len(t, got, pastaOutputLimit)
}

func TestTheLauncherNeedsACommand(t *testing.T) {
	assert.Equal(t, initFailed, PastaMain([]string{"/usr/bin/pasta"}))
	assert.Equal(t, initFailed, PastaMain([]string{"--"}))
}

// What pasta writes before the run starts stays out of the run's output, while
// the command's own stderr reaches it and its stdin is the null device. The
// pasta here writes a notice and runs bwrap in the sidecar's own network.
func TestPastasNoticesStayOutOfTheRun(t *testing.T) {
	s := *confining(t)
	s.pasta = fakeBwrap(t, filepath.Join(t.TempDir(), "pasta"),
		`echo "Couldn't get any nameserver address" >&2; while [ "$1" != -- ]; do shift; done; shift; exec "$@"`)

	cmd := command(t, &s, t.Context(), internetRun(t, &s, "-c", `echo out; echo err >&2; read -r x; echo "read $?"`))
	out, err := cmd.CombinedOutput()

	require.NoError(t, err, string(out))
	assert.Equal(t, "out\nerr\nread 1\n", withoutCoverWarning(out))
}

// stderrTo points os.Stderr at a new file for the rest of the test, as the
// launcher reads it when called, and answers the file.
func stderrTo(t *testing.T) *os.File {
	t.Helper()
	f, err := os.Create(filepath.Join(t.TempDir(), "stderr"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = f.Close() })
	orig := os.Stderr
	os.Stderr = f
	t.Cleanup(func() { os.Stderr = orig })
	return f
}

// Called in a test's own process, the launcher answers its command's status
// and passes on what the command wrote before the run started.
func TestPastaMainInProcessReportsARunThatNeverStarted(t *testing.T) {
	stderr := stderrTo(t)

	code := PastaMain([]string{"--", "/bin/sh", "-c", "echo 'External interface not usable' >&2; exit 3"})

	assert.Equal(t, 3, code)
	got, err := os.ReadFile(stderr.Name())
	require.NoError(t, err)
	assert.Equal(t, "External interface not usable\n", string(got))
}

// Once the run has started, what the command wrote on the launcher's pipe is
// dropped.
func TestPastaMainInProcessDropsTheOutputOfARunThatStarted(t *testing.T) {
	stderr := stderrTo(t)

	code := PastaMain([]string{"--", "/bin/sh", "-c", `printf 'notice\n\0sandbox-init: started\0' >&2`})

	assert.Equal(t, 0, code)
	got, err := os.ReadFile(stderr.Name())
	require.NoError(t, err)
	assert.Empty(t, got)
}

// A command the launcher cannot start is its own failure.
func TestPastaMainInProcessRefusesAMissingCommand(t *testing.T) {
	stderr := stderrTo(t)

	assert.Equal(t, initFailed, PastaMain([]string{"--", "/nonexistent/pasta"}))
	got, err := os.ReadFile(stderr.Name())
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(string(got), PastaCommand+": cannot start /nonexistent/pasta: "), string(got))
}

// sandbox-pasta told of a lifeline it was not given fails before pasta
// starts.
func TestTheLauncherNeedsTheLifelineItIsTold(t *testing.T) {
	assert.Equal(t, initFailed, PastaMain([]string{lifelineFlag, "1000", "--", "/bin/true"}))
	assert.Equal(t, initFailed, PastaMain([]string{lifelineFlag, "2", "--", "/bin/true"}))
	assert.Equal(t, initFailed, PastaMain([]string{lifelineFlag, "x", "--", "/bin/true"}))
	assert.Equal(t, initFailed, PastaMain([]string{lifelineFlag + "=2", "--", "/bin/true"}))
	assert.Equal(t, initFailed, PastaMain([]string{"stray", "--", "/bin/true"}))
}

// With no descriptor free for the pipe pasta's output goes through, the
// launcher fails before pasta starts. Serial: it lowers this process's
// open-files limit.
func TestTheLauncherNeedsADescriptorForItsPipe(t *testing.T) {
	stderrTo(t) // keeps the failure line out of the test's output
	openFilesLimit(t, 3)

	assert.Equal(t, initFailed, PastaMain([]string{"--", "/bin/true"}))
}

// At the lifeline's EOF the launcher kills its group: the pasta it started,
// and the child pasta left there.
func TestTheLauncherEndsPastasGroupAtTheLifelinesEOF(t *testing.T) {
	r, w := pipe(t)
	pasta := fakeBwrap(t, filepath.Join(t.TempDir(), "pasta"), "sleep 1000 & echo up; wait")
	cmd := exec.Command(os.Args[0], PastaCommand, lifelineFlag+"="+strconv.Itoa(lifelineFD), "--", pasta)
	cmd.ExtraFiles = []*os.File{r}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}

	requireEndsWithTheLifeline(t, cmd, w)

	assert.Equal(t, 128+int(syscall.SIGKILL), ExitCode(cmd.ProcessState), "the kill reached the launcher too")
}
