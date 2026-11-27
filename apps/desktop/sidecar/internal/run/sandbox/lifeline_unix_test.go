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
	"bufio"
	"io"
	"os"
	"os/exec"
	"strconv"
	"syscall"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/testutil"
)

// pipe is a lifeline a test holds the write end of, both ends closed when the
// test ends.
func pipe(t *testing.T) (r, w *os.File) {
	t.Helper()
	r, w, err := os.Pipe()
	require.NoError(t, err)
	t.Cleanup(func() { _ = r.Close(); _ = w.Close() })
	return r, w
}

// The watch ends once the write end closes, and the descriptor closes on
// exec, so no child holds it.
func TestTheWatchEndsAtTheLifelinesEOF(t *testing.T) {
	t.Parallel()

	r, w := pipe(t)
	fd, err := unix.Dup(int(r.Fd()))
	require.NoError(t, err)
	ended := testutil.NewSignal()

	require.NoError(t, watchLifeline(fd, func() { ended.Fire() }))
	flags, err := unix.FcntlInt(uintptr(fd), unix.F_GETFD, 0)
	require.NoError(t, err)
	assert.NotZero(t, flags&unix.FD_CLOEXEC)

	require.NoError(t, w.Close())
	ended.Wait(t, "the watch to end")
}

// A descriptor that is not open is no lifeline.
func TestTheWatchRefusesAClosedDescriptor(t *testing.T) {
	t.Parallel()

	r, _ := pipe(t)
	fd, err := unix.Dup(int(r.Fd()))
	require.NoError(t, err)
	require.NoError(t, unix.Close(fd))

	assert.Error(t, watchLifeline(fd, func() {}))
}

// lifeline is a Sandbox holding a lifeline and nothing else, for a test to
// end with Close; the test's end closes it otherwise.
func lifeline(t *testing.T) *Sandbox {
	t.Helper()
	r, held, err := newLifeline()
	require.NoError(t, err)
	s := &Sandbox{lifeline: r, held: held}
	closing(t, s)
	return s
}

// A run is handed the lifeline on lifelineFD and told so; a Sandbox with
// none hands nothing and says nothing.
func TestARunIsHandedTheLifeline(t *testing.T) {
	t.Parallel()

	cmd := exec.Command("true")

	(&Sandbox{}).handLifeline(cmd)
	assert.Empty(t, cmd.ExtraFiles)
	assert.Empty(t, (&Sandbox{}).lifelineArgs())

	s := lifeline(t)
	s.handLifeline(cmd)
	assert.Equal(t, []*os.File{s.lifeline}, cmd.ExtraFiles)
	assert.Equal(t, []string{lifelineFlag, "3"}, s.lifelineArgs())
}

// A closed Sandbox has nothing left to close, and hands no run anything; one
// with no lifeline closes all the same.
func TestCloseClosesOnce(t *testing.T) {
	t.Parallel()

	s := lifeline(t)

	require.NoError(t, s.Close())
	assert.NoError(t, s.Close())
	assert.Nil(t, s.lifeline)
	assert.Empty(t, s.lifelineArgs())
	assert.ErrorIs(t, s.handLifeline(exec.Command("true")), errClosed)
	assert.NoError(t, (&Sandbox{}).Close(), "a Sandbox with no lifeline has nothing to close")
}

// When the lifeline closes, the forwarder kills its whole group: the shell,
// and the sleep it left running.
func TestTheForwarderEndsTheRunAtTheLifelinesEOF(t *testing.T) {
	t.Parallel()

	r, w := pipe(t)
	cmd := initCmd(lifelineFlag, strconv.Itoa(lifelineFD), "--", "/bin/sh", "-c", "sleep 1000 & echo up; wait")
	cmd.ExtraFiles = []*os.File{r}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}

	requireEndsWithTheLifeline(t, cmd, w)

	assert.Equal(t, 128+int(syscall.SIGKILL), ExitCode(cmd.ProcessState), "the kill reached the forwarder too")
}

// A forwarder told of a lifeline it was not given fails before its child
// starts.
func TestTheForwarderNeedsTheLifelineItIsTold(t *testing.T) {
	t.Parallel()

	code, stdout, stderr := runInit(t, initCmd(lifelineFlag, "1000", "--", "/bin/sh", "-c", "echo ran"))

	assert.Equal(t, initFailed, code)
	assert.Empty(t, stdout)
	assert.Contains(t, stderr, "sandbox-init: cannot watch the lifeline")
}

// requireEndsWithTheLifeline starts cmd, a run that prints up and then waits
// on a sleep it started in the background, closes the lifeline's write end
// (a pipe's, or a Sandbox) once the run is up, and waits for its stdout to
// reach EOF, which it does only once every process of the run holding it,
// the sleep among them, has ended. Then it reaps cmd.
func requireEndsWithTheLifeline(t *testing.T, cmd *exec.Cmd, held io.Closer) {
	t.Helper()
	stdout, err := cmd.StdoutPipe()
	require.NoError(t, err)
	require.NoError(t, cmd.Start())
	t.Cleanup(func() { _ = cmd.Process.Kill() })

	lines := bufio.NewReader(stdout)
	up, err := lines.ReadString('\n')
	require.NoError(t, err)
	require.Equal(t, "up\n", up)

	require.NoError(t, held.Close())
	drained := make(chan error, 1)
	go func() {
		_, err := io.Copy(io.Discard, lines)
		drained <- err
	}()
	require.NoError(t, testutil.Recv(t, drained, "the run to let go of its stdout"))
	_ = cmd.Wait()
}

// openFilesLimit lowers this process's open-files limit to cur until the test
// ends. A test calling it is serial: the limit is the process's.
func openFilesLimit(t *testing.T, cur uint64) {
	t.Helper()
	var own unix.Rlimit
	require.NoError(t, unix.Getrlimit(unix.RLIMIT_NOFILE, &own))
	t.Cleanup(func() { _ = unix.Setrlimit(unix.RLIMIT_NOFILE, &own) })
	low := own
	low.Cur = cur
	require.NoError(t, unix.Setrlimit(unix.RLIMIT_NOFILE, &low))
}

// lowestFree answers the two lowest descriptors not open.
func lowestFree(t *testing.T) (a, b int) {
	t.Helper()
	a, err := unix.Open(os.DevNull, unix.O_RDONLY, 0)
	require.NoError(t, err)
	b, err = unix.Open(os.DevNull, unix.O_RDONLY, 0)
	require.NoError(t, err)
	require.NoError(t, unix.Close(b))
	require.NoError(t, unix.Close(a))
	return a, b
}

// The lifeline takes three descriptors, the pipe's two and the held
// duplicate; short of any it fails and leaves none open. Serial: it lowers
// this process's open-files limit.
func TestTheLifelineNeedsThreeDescriptors(t *testing.T) {
	a, b := lowestFree(t)
	openFilesLimit(t, uint64(b)+1)
	_, _, err := newLifeline()
	assert.Error(t, err, "the pipe took the two free descriptors, so the duplicate had none")
	x, y := lowestFree(t)
	assert.Equal(t, [2]int{a, b}, [2]int{x, y}, "the pipe is closed")

	openFilesLimit(t, 3)
	_, _, err = newLifeline()
	assert.Error(t, err, "no pipe")
}
