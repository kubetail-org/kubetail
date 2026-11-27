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
	"bytes"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/testutil"
)

// helpers are what the test binary can be started as for a test that needs a
// process of its own, by name in KUBETAIL_SANDBOX_TEST_HELPER.
var helpers = map[string]func() int{}

// TestMain answers sandbox-init and sandbox-shell when the test binary is
// started as either, as a run starts its own executable.
func TestMain(m *testing.M) {
	if code, ok := Main(os.Args); ok {
		testutil.ExitHelper(code)
	}
	if h := helpers[os.Getenv("KUBETAIL_SANDBOX_TEST_HELPER")]; h != nil {
		testutil.ExitHelper(h())
	}
	if port := os.Getenv("KUBETAIL_SANDBOX_TEST_DIAL"); port != "" {
		testutil.ExitHelper(dialOnce(port))
	}
	os.Exit(m.Run())
}

// dialOnce is a child that connects to the forwarder's port the moment it
// starts, sends ping, ends its input, and prints what comes back.
func dialOnce(port string) int {
	c, err := net.Dial("tcp", "127.0.0.1:"+port)
	if err != nil {
		fmt.Println(err)
		return 1
	}
	defer c.Close()
	_, _ = io.WriteString(c, "ping")
	_ = c.(*net.TCPConn).CloseWrite()
	_, _ = io.Copy(os.Stdout, c)
	return 0
}

// freePort is a loopback port nothing listens on.
func freePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port
}

// forwarding is the arguments that forward port to socket and run argv.
func forwarding(socket string, port int, argv ...string) []string {
	return append([]string{"--socket", socket, "--port", strconv.Itoa(port), "--"}, argv...)
}

// flakyListener fails its first Accept, as one does out of descriptors.
type flakyListener struct {
	net.Listener
	failed bool
}

func (l *flakyListener) Accept() (net.Conn, error) {
	if !l.failed {
		l.failed = true
		return nil, syscall.EMFILE
	}
	return l.Listener.Accept()
}

// A failed accept is tried again, and only a closed listener ends the loop.
func TestTheForwarderAcceptsAgainAfterAFailure(t *testing.T) {
	t.Parallel()

	socket := echoSocket(t)
	tcpLn, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	done := make(chan struct{})
	go func() {
		defer close(done)
		accept(&flakyListener{Listener: tcpLn}, socket, time.Microsecond)
	}()

	client, err := net.Dial("tcp", tcpLn.Addr().String())
	require.NoError(t, err)
	defer client.Close()
	_, _ = io.WriteString(client, "ping")
	_ = client.(*net.TCPConn).CloseWrite()
	got, err := io.ReadAll(client)
	require.NoError(t, err)
	assert.Equal(t, "ping|eof", string(got))

	require.NoError(t, tcpLn.Close())
	<-done
}

// A signal reads as 128 plus its number, as a shell reports it; anything else
// is the exit status.
func TestASignalReadsAs128PlusIt(t *testing.T) {
	t.Parallel()

	signalled := func(sig os.Signal) *os.ProcessState {
		cmd := exec.Command("sleep", "60")
		require.NoError(t, cmd.Start())
		require.NoError(t, cmd.Process.Signal(sig))
		_ = cmd.Wait()
		return cmd.ProcessState
	}
	exited := exec.Command("/bin/sh", "-c", "exit 3")
	_ = exited.Run()

	assert.Equal(t, 137, ExitCode(signalled(syscall.SIGKILL)))
	assert.Equal(t, 143, ExitCode(signalled(syscall.SIGTERM)))
	assert.Equal(t, 3, ExitCode(exited.ProcessState))
}

// The forwarder exits as its child did: its code, or 128 plus the signal.
func TestInitExitsWithTheChildsStatus(t *testing.T) {
	t.Parallel()

	for script, want := range map[string]int{"exit 0": 0, "exit 7": 7, "kill -KILL $$": 137} {
		code, _, _ := runInit(t, initCmd(forwarding("unused", freePort(t), "/bin/sh", "-c", script)...))
		assert.Equal(t, want, code, script)
	}
}

// A forwarder given neither flag runs its child alone, and exits as it did.
func TestInitWithNoSocketRunsTheChild(t *testing.T) {
	t.Parallel()

	code, stdout, stderr := runInit(t, initCmd("--", "/bin/sh", "-c", "echo ran; exit 3"))

	assert.Equal(t, 3, code, stderr)
	assert.Equal(t, "ran\n", stdout)
}

// The port is open before the child starts, so a child that connects at once
// is relayed.
func TestInitListensBeforeTheChildStarts(t *testing.T) {
	t.Parallel()

	port := freePort(t)
	cmd := initCmd(forwarding(echoSocket(t), port, os.Args[0])...)
	cmd.Env = append(os.Environ(), "KUBETAIL_SANDBOX_TEST_DIAL="+strconv.Itoa(port))

	code, stdout, stderr := runInit(t, cmd)

	assert.Equal(t, 0, code, stderr)
	assert.Equal(t, "ping|eof", stdout)
}

// A port another process holds is the forwarder's failure: 125, the listen
// line, and a child that never starts.
func TestInitExitsWhenThePortIsTaken(t *testing.T) {
	t.Parallel()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer ln.Close()
	port := ln.Addr().(*net.TCPAddr).Port
	marker := filepath.Join(t.TempDir(), "started")

	code, _, stderr := runInit(t, initCmd(forwarding("unused", port, "/bin/sh", "-c", "touch "+marker)...))

	assert.Equal(t, 125, code)
	assert.True(t, strings.HasPrefix(stderr, "sandbox-init: cannot listen on 127.0.0.1:"+strconv.Itoa(port)+": "), stderr)
	assert.NoFileExists(t, marker)
}

// A SIGTERM to the group, as a stop sends, ends the child at once, and the
// forwarder outlives it to exit with its 143 rather than dying of the signal.
func TestInitWaitsForTheChildOnTerm(t *testing.T) {
	t.Parallel()

	cmd := initCmd(forwarding("unused", freePort(t), "/bin/sh", "-c", "echo up; exec sleep 60")...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	out, err := cmd.StdoutPipe()
	require.NoError(t, err)
	require.NoError(t, cmd.Start())
	line, err := bufio.NewReader(out).ReadString('\n')
	require.NoError(t, err)
	require.Equal(t, "up\n", line)

	require.NoError(t, syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM))
	_ = cmd.Wait()

	ws := cmd.ProcessState.Sys().(syscall.WaitStatus)
	assert.False(t, ws.Signaled(), "the forwarder died of the signal")
	assert.Equal(t, 143, ws.ExitStatus())
}

// The forwarder's signals are caught, never ignored: an ignored signal stays
// ignored across exec, and a shell cannot trap a signal ignored when it
// started.
func TestTheChildCanTrapTheSignals(t *testing.T) {
	t.Parallel()

	for _, sig := range []string{"TERM", "INT", "HUP"} {
		script := `trap 'echo trapped; exit 3' ` + sig + `; kill -` + sig + ` $$; exit 9`
		code, stdout, stderr := runInit(t, initCmd(forwarding("unused", freePort(t), "/bin/sh", "-c", script)...))
		assert.Equal(t, 3, code, sig+": "+stderr)
		assert.Equal(t, "trapped\n", stdout, sig)
	}
}

// A command that cannot start is the forwarder's failure too.
func TestInitExitsWhenTheChildCannotStart(t *testing.T) {
	t.Parallel()

	code, _, stderr := runInit(t, initCmd(forwarding("unused", freePort(t), "/nonexistent/shell")...))

	assert.Equal(t, 125, code)
	assert.True(t, strings.HasPrefix(stderr, "sandbox-init: cannot start /nonexistent/shell: "), stderr)
}

// helperCmd is the test binary as the helper named, outside any sandbox.
func helperCmd(name string) *exec.Cmd {
	cmd := exec.Command(os.Args[0])
	cmd.Env = append(os.Environ(), "KUBETAIL_SANDBOX_TEST_HELPER="+name)
	return cmd
}

// initCmd is the test binary as the forwarder, given args.
func initCmd(args ...string) *exec.Cmd {
	return exec.Command(os.Args[0], append([]string{InitCommand}, args...)...)
}

// runInit runs cmd to its end and answers its exit code, stdout and stderr.
func runInit(t *testing.T, cmd *exec.Cmd) (code int, stdout, stderr string) {
	t.Helper()
	var out, errOut bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errOut
	err := cmd.Run()
	var exit *exec.ExitError
	if err != nil && !errors.As(err, &exit) {
		require.NoError(t, err)
	}
	return ExitCode(cmd.ProcessState), out.String(), errOut.String()
}

// Arguments the forwarder cannot read are its own failure: 125, and one line
// saying so.
func TestInitRefusesBadArguments(t *testing.T) {
	t.Parallel()

	for name, args := range map[string][]string{
		"no socket":  {"--port", "1", "--", "true"},
		"no port":    {"--socket", "s", "--", "true"},
		"no command": {"--socket", "s", "--port", "1", "--"},
	} {
		code, _, stderr := runInit(t, initCmd(args...))
		assert.Equal(t, 125, code, name)
		assert.True(t, strings.HasPrefix(stderr, "sandbox-init: bad arguments: "), stderr)
		assert.Equal(t, 1, strings.Count(stderr, "\n"), stderr)
	}
}

// Called in a test's own process, off PID 1, the forwarder waits on its child
// alone and answers its status.
func TestInitMainInProcessAnswersTheChildsStatus(t *testing.T) {
	t.Parallel()

	assert.Equal(t, 3, InitMain([]string{"--", "/bin/sh", "-c", "exit 3"}))
	assert.Equal(t, 137, InitMain([]string{"--", "/bin/sh", "-c", "kill -KILL $$"}))
}

// A command the forwarder cannot find is its own failure.
func TestInitMainInProcessRefusesAMissingCommand(t *testing.T) {
	t.Parallel()

	assert.Equal(t, 125, InitMain([]string{"--", "/nonexistent/shell"}))
}

// A command the kernel will not exec is the forwarder's failure too.
func TestInitMainInProcessRefusesWhatCannotExec(t *testing.T) {
	t.Parallel()

	garbage := filepath.Join(t.TempDir(), "garbage")
	require.NoError(t, os.WriteFile(garbage, []byte{0, 1, 2, 3}, 0o700))

	assert.Equal(t, 125, InitMain([]string{"--", garbage}))
}

// echoSocket is a Unix socket, in a short directory of its own so its path
// fits, whose server echoes each connection and writes |eof once the client's
// input ends, then ends its own.
func echoSocket(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "relay")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	socket := filepath.Join(dir, "s")
	ln, err := net.Listen("unix", socket)
	require.NoError(t, err)
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer c.Close()
				_, _ = io.Copy(c, c)
				_, _ = io.WriteString(c, "|eof")
				_ = c.(*net.UnixConn).CloseWrite()
			}()
		}
	}()
	return socket
}

// The relay carries bytes both ways unchanged, and passes each side's
// half-close to the other: the echo server sees the client's end of input,
// and the client sees the server's.
func TestTheRelayIsByteForByte(t *testing.T) {
	t.Parallel()

	socket := echoSocket(t)
	tcpLn, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = tcpLn.Close() })
	go func() {
		c, err := tcpLn.Accept()
		if err == nil {
			relay(c, socket)
		}
	}()

	client, err := net.Dial("tcp", tcpLn.Addr().String())
	require.NoError(t, err)
	defer client.Close()
	sent := make([]byte, 1<<20)
	_, _ = rand.Read(sent)
	go func() {
		_, _ = client.Write(sent)
		_ = client.(*net.TCPConn).CloseWrite()
	}()
	got, err := io.ReadAll(client)

	require.NoError(t, err)
	assert.Equal(t, append(sent, "|eof"...), got)
}

// A connection whose socket cannot be reached is closed, so its client sees
// the end at once.
func TestTheRelayClosesWhatItCannotCarry(t *testing.T) {
	t.Parallel()

	client, relayed := net.Pipe()
	done := make(chan struct{})
	go func() {
		defer close(done)
		relay(relayed, filepath.Join(t.TempDir(), "missing"))
	}()

	_, err := client.Read(make([]byte, 1))

	assert.ErrorIs(t, err, io.EOF)
	<-done
}

// The forwarder starts its child with a core size of zero, so no process of a
// run dumps its memory outside the sandbox.
func TestTheCoreSizeIsZero(t *testing.T) {
	t.Parallel()

	self := "'" + os.Args[0] + "'"
	cmd := exec.Command("/bin/sh", "-c", "ulimit -c unlimited 2>/dev/null; exec "+self+" "+InitCommand+" -- "+self)
	cmd.Env = append(os.Environ(), "KUBETAIL_SANDBOX_TEST_HELPER=rlimits")

	code, stdout, stderr := runInit(t, cmd)

	require.Equal(t, 0, code, stderr)
	assert.Contains(t, stdout, "core=0/0")
}

func init() {
	// lookup resolves KUBETAIL_SANDBOX_TEST_NAME and prints its addresses, or
	// not-found for a name the resolver answered does not exist, or the error.
	helpers["lookup"] = func() int {
		addrs, err := net.LookupHost(os.Getenv("KUBETAIL_SANDBOX_TEST_NAME"))
		var dnsErr *net.DNSError
		switch {
		case errors.As(err, &dnsErr) && dnsErr.IsNotFound:
			fmt.Print("not-found")
		case err != nil:
			fmt.Print(err)
			return 1
		default:
			fmt.Print(strings.Join(addrs, ","))
		}
		return 0
	}
}

// Under sandbox-pasta the forwarder takes the run's stderr off stdin: the
// child writes there, reads the null device, and the launcher's stderr gets
// runStarted alone. In-process it moves the test's own descriptors 0 and 2,
// so it saves and restores them.
func TestInitMainInProcessTakesTheStderrOnStdin(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	runStderr, err := os.Create(filepath.Join(dir, "run"))
	require.NoError(t, err)
	defer runStderr.Close()
	launcher, err := os.Create(filepath.Join(dir, "launcher"))
	require.NoError(t, err)
	defer launcher.Close()
	for _, fd := range []int{0, 2} {
		saved, err := unix.Dup(fd)
		require.NoError(t, err)
		defer func() {
			_ = unix.Dup2(saved, fd)
			_ = unix.Close(saved)
		}()
	}
	require.NoError(t, unix.Dup2(int(runStderr.Fd()), 0))
	require.NoError(t, unix.Dup2(int(launcher.Fd()), 2))

	code := InitMain([]string{stderrOnStdinFlag, "--", "/bin/sh", "-c", `echo err >&2; read -r x; echo "read $?" >&2`})

	assert.Equal(t, 0, code)
	got, err := os.ReadFile(runStderr.Name())
	require.NoError(t, err)
	assert.Equal(t, "err\nread 1\n", string(got))
	got, err = os.ReadFile(launcher.Name())
	require.NoError(t, err)
	assert.Equal(t, runStarted, string(got))
}

// With no stdin to take the run's stderr from, the forwarder fails before the
// child starts. In-process it closes the test's own descriptor 0, so it saves
// and restores it.
func TestInitMainInProcessFailsWithoutTheStderrOnStdin(t *testing.T) {
	t.Parallel()

	saved, err := unix.Dup(0)
	require.NoError(t, err)
	defer func() {
		_ = unix.Dup2(saved, 0)
		_ = unix.Close(saved)
	}()
	require.NoError(t, unix.Close(0))
	marker := filepath.Join(t.TempDir(), "started")

	assert.Equal(t, initFailed, InitMain([]string{stderrOnStdinFlag, "--", "/bin/sh", "-c", "touch " + marker}))
	assert.NoFileExists(t, marker)
}

// With no stderr of sandbox-pasta's to keep, the forwarder fails before the
// child starts. In-process it closes the test's own descriptor 2, so it saves
// and restores it.
func TestInitMainInProcessFailsWithoutAStderr(t *testing.T) {
	t.Parallel()

	saved, err := unix.Dup(2)
	require.NoError(t, err)
	defer func() {
		_ = unix.Dup2(saved, 2)
		_ = unix.Close(saved)
	}()
	require.NoError(t, unix.Close(2))
	marker := filepath.Join(t.TempDir(), "started")

	assert.Equal(t, initFailed, InitMain([]string{stderrOnStdinFlag, "--", "/bin/sh", "-c", "touch " + marker}))
	assert.NoFileExists(t, marker)
}

// A child the forwarder cannot wait for, here one that is not its child,
// fails the forwarder.
func TestWaitForFailsOnWhatIsNotItsChild(t *testing.T) {
	t.Parallel()

	assert.Equal(t, initFailed, waitFor(os.Getpid(), false))
}
