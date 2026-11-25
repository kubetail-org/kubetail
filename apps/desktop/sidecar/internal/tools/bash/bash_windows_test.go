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

//go:build windows

package bash

import (
	"bufio"
	"context"
	"errors"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
	"unsafe"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/sys/windows"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/testutil"
)

// shortTempBase is where shortTemp makes its directories: the temp directory,
// which is short enough here.
const shortTempBase = ""

// stubInstallPath points the registry seam at a fixed answer for one test.
func stubInstallPath(t *testing.T, path string, ok bool) {
	t.Helper()
	prev := readInstallPath
	readInstallPath = func() (string, bool) { return path, ok }
	t.Cleanup(func() { readInstallPath = prev })
}

// Only Git's own install record finds a bash: a bash.exe on PATH is never
// consulted, and a recorded install whose bash is gone offers nothing.
func TestNewDeclinesWithoutGitBash(t *testing.T) {
	stubInstallPath(t, "", false)
	bin := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(bin, "bash.exe"), []byte("MZ"), 0o700))
	t.Setenv("PATH", bin)
	_, ok := New(Paths{ShellDir: t.TempDir()}, 0, nil, nil, nil, nil, Cloud{})
	assert.False(t, ok, "a bash.exe on PATH must not be found")

	stubInstallPath(t, t.TempDir(), true) // recorded, but bin\bash.exe absent
	_, ok = New(Paths{ShellDir: t.TempDir()}, 0, nil, nil, nil, nil, Cloud{})
	assert.False(t, ok)
}

// A recorded install whose bin\bash.exe exists is found; New only stats it.
func TestNewFindsTheRecordedBash(t *testing.T) {
	install := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(install, "bin"), 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(install, "bin", "bash.exe"), []byte("MZ"), 0o700))
	stubInstallPath(t, install, true)
	tl, ok := New(Paths{ShellDir: t.TempDir()}, 0, nil, nil, nil, nil, Cloud{})
	require.True(t, ok)
	assert.Equal(t, filepath.Join(install, "bin", "bash.exe"), tl.shell)
}

// The real registry against the machine's Git for Windows.
func TestNewFindsGitBash(t *testing.T) {
	if _, ok := registryInstallPath(); !ok {
		t.Skip("no Git for Windows install record on this machine")
	}
	_, ok := New(Paths{ShellDir: t.TempDir()}, 0, nil, nil, nil, nil, Cloud{})
	assert.True(t, ok)
}

// A key nothing records is read in every view, and answers no install.
func TestAnAbsentInstallRecordIsNoInstall(t *testing.T) {
	_, ok := installPathUnder(`SOFTWARE\KubetailTestNoSuchKey`)
	assert.False(t, ok)
}

// A context that ended before run starts nothing, as on Unix.
func TestRunStartsNothingPastAnEndedContext(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	r := run(ctx, spec{})
	assert.Equal(t, context.Canceled.Error(), r.Error)
}

// notADir is a path under a regular file, which no directory can be made at.
func notADir(t *testing.T) string {
	t.Helper()
	file := filepath.Join(t.TempDir(), "file")
	require.NoError(t, os.WriteFile(file, nil, 0o600))
	return filepath.Join(file, "scripts")
}

// unwritableDir is a directory no file can be made in: an ACL denies everyone
// the right until the test ends.
func unwritableDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	out, err := exec.Command("icacls", dir, "/deny", "*S-1-1-0:(W)").CombinedOutput()
	require.NoError(t, err, string(out))
	t.Cleanup(func() { _ = exec.Command("icacls", dir, "/remove:d", "*S-1-1-0").Run() })
	return dir
}

// A scripts directory that cannot be made, or written in, runs nothing and
// says why.
func TestRunFailsWithoutItsScriptsDirectory(t *testing.T) {
	for _, dir := range []string{notADir(t), unwritableDir(t)} {
		r := run(t.Context(), spec{scripts: dir, command: "echo ran"})
		assert.NotEmpty(t, r.Error, dir)
	}
}

func TestWriteScriptFailsUnderAMissingDirectory(t *testing.T) {
	_, err := writeScript(filepath.Join(t.TempDir(), "missing"), "echo ran")
	assert.Error(t, err)
}

// A NUL in the command line, the directory or the environment is refused
// before CreateProcess, which would read the string only up to it.
func TestStartSuspendedRefusesANUL(t *testing.T) {
	for name, c := range map[string]struct {
		bash, dir string
		env       []string
	}{
		"command line": {bash: "bash\x00.exe", dir: `C:\`},
		"directory":    {bash: "bash.exe", dir: "C:\\\x00"},
		"environment":  {bash: "bash.exe", dir: `C:\`, env: []string{"A=\x00"}},
	} {
		_, err := startSuspended(&winSys, c.bash, nil, c.dir, c.env, 0, 0)
		assert.Error(t, err, name)
	}
}

// An empty environment is still a double NUL. Serial: it clears the process's
// environment.
func TestAnEmptyEnvironmentBlockIsADoubleNUL(t *testing.T) {
	env := os.Environ()
	os.Clearenv()
	t.Cleanup(func() {
		for _, e := range env {
			name, value, _ := strings.Cut(e[1:], "=")
			_ = os.Setenv(e[:1]+name, value)
		}
	})

	block, err := environBlock(nil)
	require.NoError(t, err)
	assert.Equal(t, []uint16{0, 0}, block)
}

// An entry with no separator is all name.
func TestEnvNameOfAnEntryWithNoSeparator(t *testing.T) {
	assert.Equal(t, "PATH", envName("PATH"))
	assert.Equal(t, "PATH", envName("PATH=x"))
	assert.Equal(t, "=C:", envName(`=C:=C:\x`))
	assert.Empty(t, envName(""))
}

// An entry of extra replaces the process's own of its name, which Windows
// compares without case, so the child reads the one given.
func TestAnExtraEntryReplacesTheProcesssOwn(t *testing.T) {
	t.Setenv("KUBETAIL_TEST_MARK", "process")

	block, err := environBlock([]string{"kubetail_test_mark=extra"})
	require.NoError(t, err)

	var marks []string
	for len(block) > 1 {
		i := slices.Index(block, 0)
		if e := windows.UTF16ToString(block[:i]); strings.EqualFold(envName(e), "KUBETAIL_TEST_MARK") {
			marks = append(marks, e)
		}
		block = block[i+1:]
	}
	assert.Equal(t, []string{"kubetail_test_mark=extra"}, marks)
}

var errInjected = errors.New("injected")

// failing is hooks whose Windows calls are the real ones but where edit says.
func failing(edit func(s *sys)) hooks {
	s := winSys
	edit(&s)
	return hooks{sys: &s}
}

// failResume makes ResumeThread fail, so bash never runs.
func failResume(s *sys) {
	s.resumeThread = func(windows.Handle) (uint32, error) { return 0, errInjected }
}

// Each call that starts bash fails the run with its own error.
func TestRunFailsOnAFailedWindowsCall(t *testing.T) {
	tl := tool(t)
	for name, edit := range map[string]func(*sys){
		"pipe": func(s *sys) {
			s.pipe = func() (*os.File, *os.File, error) { return nil, nil, errInjected }
		},
		"SetHandleInformation": func(s *sys) {
			s.setHandleInformation = func(windows.Handle, uint32, uint32) error { return errInjected }
		},
		"CreateFile": func(s *sys) {
			s.createFile = func(*uint16, uint32, uint32, *windows.SecurityAttributes, uint32, uint32, windows.Handle) (windows.Handle, error) {
				return 0, errInjected
			}
		},
		"CreateJobObject": func(s *sys) {
			s.createJobObject = func(*windows.SecurityAttributes, *uint16) (windows.Handle, error) { return 0, errInjected }
		},
		"SetInformationJobObject": func(s *sys) {
			s.setInformationJobObject = func(windows.Handle, uint32, uintptr, uint32) (int, error) { return 0, errInjected }
		},
		"NewProcThreadAttributeList": func(s *sys) {
			s.newProcThreadAttributeList = func(uint32) (*windows.ProcThreadAttributeListContainer, error) { return nil, errInjected }
		},
		"UpdateProcThreadAttribute": func(s *sys) {
			s.updateProcThreadAttribute = func(*windows.ProcThreadAttributeListContainer, uintptr, unsafe.Pointer, uintptr) error {
				return errInjected
			}
		},
		"AssignProcessToJobObject": func(s *sys) {
			s.assignProcessToJobObject = func(windows.Handle, windows.Handle) error { return errInjected }
		},
		"ResumeThread": failResume,
	} {
		s := tl.spec("exit 0", 1024)
		s.hooks = failing(edit)
		assert.Equal(t, errInjected.Error(), run(t.Context(), s).Error, name)
	}
}

// A bash whose exit code cannot be read ended all the same.
func TestRunReportsAnUnreadExitCode(t *testing.T) {
	tl := tool(t)
	s := tl.spec("exit 3", 1024)
	s.hooks = failing(func(s *sys) {
		s.getExitCodeProcess = func(windows.Handle, *uint32) error { return errInjected }
	})
	r := run(t.Context(), s)
	assert.Empty(t, r.Error)
	assert.True(t, r.CodeUnknown)
}

// The approval request's invariant as a test: what the package wrote is what
// bash read. The guard leads because bash executes a script command-by-command,
// so the bytes print before a fragment below them is even parsed.
func TestRunDeliversTheCommandByteForByte(t *testing.T) {
	tl := tool(t)
	corpus := []string{
		`"`,
		`\"`,
		`\\`,
		"echo hi\\",
		`*`,
		`%PATH%`,
		"echo one\necho two",
		"echo π 日本語",
		"echo " + strings.Repeat("x", 2000),
	}
	for _, entry := range corpus {
		// The capture comfortably exceeds the script: a truncated capture
		// would fail the assertion reading as a quoting bug.
		script := "cat \"$0\"\n" + entry
		r := run(t.Context(), tl.spec(script, 64<<10))
		assert.Empty(t, r.Error)
		assert.True(t, strings.HasPrefix(r.Output, script),
			"script %q came back as %q", script, r.Output)
	}
}

// holder is a TCP connection a command's sleep holds open — the Unix FIFO's
// event shape, since MSYS has no FIFO the test could share with Git Bash.
// Accept plus the greeting is "started"; read EOF is "gone", arriving exactly
// when every process holding the socket is dead. Events, never a duration.
type holder struct {
	port string
	line chan string
	gone chan struct{}
}

func newHolder(t *testing.T) *holder {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = l.Close() })
	_, port, err := net.SplitHostPort(l.Addr().String())
	require.NoError(t, err)
	h := &holder{port: port, line: make(chan string, 1), gone: make(chan struct{})}
	go func() {
		defer close(h.gone)
		c, err := l.Accept()
		if err != nil {
			return
		}
		defer c.Close()
		br := bufio.NewReader(c)
		s, _ := br.ReadString('\n')
		h.line <- s
		buf := make([]byte, 64)
		for {
			if _, err := br.Read(buf); err != nil {
				return
			}
		}
	}()
	return h
}

// command holds the socket on fd 3 and says "started" before the sleep, so
// the greeting is written before bash can exit.
func (h *holder) command(tail string) string {
	return "exec 3<>/dev/tcp/127.0.0.1/" + h.port + "; echo started >&3; sleep 60 " + tail
}

func (h *holder) awaitStarted(t *testing.T) {
	t.Helper()
	assert.Equal(t, "started\n", testutil.Recv(t, h.line, "the sleep to start"))
}

func (h *holder) awaitGone(t *testing.T) {
	t.Helper()
	testutil.Wait(t, h.gone, "the sleep to be gone")
}

// A command past its own deadline is killed with everything in its job, and the
// result reads as timed out with the conventional code.
func TestRunKillsTheJobOnTimeout(t *testing.T) {
	tl := tool(t)
	h := newHolder(t)
	c := newClock()
	s := tl.spec(h.command("& wait"), 1024)
	s.hooks = hooks{after: c.after, beforeReap: func(context.Context) {
		h.awaitStarted(t)
		c.fire()
	}}
	r := run(t.Context(), s)
	assert.Equal(t, stopTimeout, r.Stop)
	assert.Equal(t, 143, r.ExitCode)
	assert.True(t, strings.HasPrefix(resultText(r, time.Minute, nil, unconfined), "Command timed out after 60s (exit code 143)\n"))
	h.awaitGone(t)
}

// A cancelled command is killed with everything in its job, and the result says
// so.
func TestRunKillsTheJobOnCancel(t *testing.T) {
	tl := tool(t)
	h := newHolder(t)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	res := make(chan result, 1)
	go func() { res <- run(ctx, tl.spec(h.command("& wait"), 1024)) }()
	h.awaitStarted(t)
	cancel()
	r := testutil.Recv(t, res, "the run to end")
	assert.Equal(t, stopCancel, r.Stop)
	assert.Equal(t, 137, r.ExitCode)
	h.awaitGone(t)
}

// A deadline after the reap sends nothing and records nothing: the lock is what
// disarms the watcher, which is alive through the drain.
func TestAnExitReapedBeforeTheDeadlineIsNotRelabelled(t *testing.T) {
	tl := tool(t)
	c := newClock()
	s := tl.spec("exit 0", 1024)
	s.hooks = hooks{after: c.after, afterReap: func(ctx context.Context) {
		c.fire()
		<-ctx.Done()
	}}
	r := run(t.Context(), s)
	assert.Equal(t, stopNone, r.Stop)
	assert.False(t, r.failed())
}

// A pipe held from outside the job past the deadline is not the command's: a
// command that exited 0 is a success however long the drain takes.
func TestASlowDrainAfterTheExitIsNotATimeout(t *testing.T) {
	tl := tool(t)
	c := newClock()
	s := tl.spec("exit 0", 1024)
	var held windows.Handle
	s.hooks = hooks{
		after:  c.after,
		onPipe: func(w windows.Handle) { held = w },
		afterReap: func(ctx context.Context) {
			c.fire()
			go func() {
				<-ctx.Done()
				windows.CloseHandle(held)
			}()
		},
	}
	r := run(t.Context(), s)
	assert.Equal(t, stopNone, r.Stop)
	assert.False(t, r.failed())
}

// A command that exits leaving a process behind does not leave it: the job is
// terminated after bash exits, before the result is read. The gone event is
// also the job-closed-after-exit assertion — the sleep's own Windows pid is
// unreachable from bash ($! is an MSYS pid), so the socket says it died.
func TestRunKillsWhatACommandLeftBehind(t *testing.T) {
	tl := tool(t)
	h := newHolder(t)
	r := run(t.Context(), tl.spec(h.command("&"), 1024))
	assert.Zero(t, r.ExitCode)
	assert.Equal(t, stopNone, r.Stop)
	h.awaitStarted(t)
	h.awaitGone(t)
}

// The command sees the process's own environment through Git Bash's
// translation: HOME falls back to %USERPROFILE%, paths come back in POSIX
// form and cygpath -w undoes them.
func TestACommandsEnvironmentIsTheProcessOwn(t *testing.T) {
	// A BASH_ENV in the test's own environment would have bash source a file
	// that rewrites PATH before the command runs; empty is unset to bash.
	t.Setenv("BASH_ENV", "")
	t.Setenv("KUBETAIL_TEST_MARK", "marked")
	os.Unsetenv("KUBETAIL_TEST_UNSET")
	tl := tool(t)
	r := run(t.Context(), tl.spec("echo $KUBETAIL_TEST_MARK; echo $KUBETAIL_TEST_UNSET", 1024))
	assert.Equal(t, "marked\n\n", r.Output)
	r = run(t.Context(), tl.spec(`cygpath -w "$HOME"`, 4096))
	assert.True(t, strings.EqualFold(strings.TrimSpace(r.Output), os.Getenv("USERPROFILE")),
		"HOME %q is not USERPROFILE %q", r.Output, os.Getenv("USERPROFILE"))
	r = run(t.Context(), tl.spec(`cygpath -w "$(pwd)"`, 4096))
	assert.True(t, strings.EqualFold(strings.TrimSpace(r.Output), tl.home),
		"pwd %q is not the start directory %q", r.Output, tl.home)
}
