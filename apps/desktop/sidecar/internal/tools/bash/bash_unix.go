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

package bash

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/run/loginshell"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/run/sandbox"
)

// DefaultPath is what a sandboxed run searches before the login shell was
// ever read: the PATH a login shell starts from here.
const DefaultPath = loginshell.DefaultPath

// pipeGrace bounds the wait for the output pipe to close once the group has
// been killed: a descendant that left the group can still hold it, and the
// call must end without it.
const pipeGrace = time.Second

// drainLimit bounds drainPipe, since a process outside the group can keep the
// pipe full. What bash left in it is at most the pipe's capacity.
const drainLimit = 1 << 20

// drainPipe copies what r holds to dst without waiting for more. r's deadline
// has passed, and a read waits on it before trying the pipe, so drainPipe
// clears it and reads the descriptor directly.
func drainPipe(dst io.Writer, r *os.File) {
	rc, err := r.SyscallConn()
	if err != nil || r.SetReadDeadline(time.Time{}) != nil {
		return
	}
	buf := make([]byte, 32<<10)
	left := drainLimit
	_ = rc.Read(func(fd uintptr) bool {
		for left > 0 {
			n, err := syscall.Read(int(fd), buf[:min(len(buf), left)])
			if n <= 0 || err != nil {
				break // empty (EAGAIN), closed, or failed
			}
			_, _ = dst.Write(buf[:n])
			left -= n
		}
		return true
	})
}

// hooks order a race in a test; production runs with none.
type hooks struct {
	after      func(d time.Duration) <-chan time.Time // the call's deadline; time.After when nil
	beforeReap func(ctx context.Context)
	onStop     func(s stop)
	afterReap  func(ctx context.Context)
}

// hideWindow is Windows' concern alone.
func hideWindow(*exec.Cmd) {}

// findShell is the login shell when it is zsh or bash, since the model writes
// bash and the wrapper is written for those two alone; anything else is bash on
// PATH.
func findShell() (path, kind string, ok bool) {
	if shell, ok := loginshell.Find(); ok {
		if base := filepath.Base(shell); base == "zsh" || base == "bash" {
			return shell, base, true
		}
	}
	path, err := exec.LookPath("bash")
	return path, "bash", err == nil
}

// run runs one command under parent. The call's own deadline stops it with
// SIGTERM to its group and SIGKILL after the grace; parent ending stops it with
// SIGKILL at once. The stop is recorded as it is sent, under a lock the reap
// closes, so an exit reaped before the stop is never relabelled.
func run(parent context.Context, s spec) result {
	// The pipe is the package's own so Wait returns the moment bash exits
	// rather than when the last holder of the pipe does — a process bash
	// left behind is killed right after, and its hold ends then.
	r, w, err := os.Pipe()
	if err != nil { // coverage:ignore a pipe fails only when the process is out of descriptors
		return result{Error: err.Error()}
	}
	ctx, cancel := s.hooks.deadline(parent, s.timeout)
	defer cancel()
	g := newGuard(s.hooks)
	cmd, err := shellCmd(ctx, s, w)
	if err != nil {
		_ = w.Close()
		_ = r.Close()
		return result{Error: startError(ctx, err, s.timeout)}
	}
	cmd.Cancel = func() error { return stopBash(parent, cmd, g, s.killGrace) }
	if err := cmd.Start(); err != nil {
		_ = w.Close()
		_ = r.Close()
		return result{Error: startError(ctx, err, s.timeout)}
	}
	_ = w.Close()

	out := &captureWriter{limit: s.capture, onKeep: s.onOutput}
	read := copyOutput(out, r)

	s.hooks.waitBeforeReap(ctx)
	_ = cmd.Wait() // the verdict is ProcessState's; Wait's error can be the context's
	res := result{Stop: g.reap()}
	_ = signalGroup(cmd, syscall.SIGKILL)
	s.hooks.waitAfterReap(ctx)
	awaitCopy(read, r, s.pipeGrace)
	_ = r.Close()

	res.Output, res.Discarded = string(out.buf), out.discarded
	if cmd.ProcessState == nil { // coverage:ignore Wait leaves ProcessState unset only when wait4 fails
		res.CodeUnknown = true
	} else {
		res.ExitCode = sandbox.ExitCode(cmd.ProcessState)
	}
	return res
}

// startError is why bash could not start: err, or, when err is ctx ending,
// which of the call's deadline and a cancel ended it first. Making a sandboxed
// command can take until then: it reads the disk.
func startError(ctx context.Context, err error, timeout time.Duration) string {
	switch {
	case ctx.Err() == nil || !errors.Is(err, ctx.Err()):
		return err.Error()
	case errors.Is(context.Cause(ctx), errCallTimeout):
		return "timed out after " + formatTimeout(timeout)
	default:
		return "cancelled"
	}
}

// shellCmd is bash on s.command in s.dir, through the sandbox when s has one, in
// a process group of its own, its stdout and stderr on w, or why the sandbox
// cannot run it. ctx is what exec kills it on; a task passes none.
//
// A sandboxed run also leads a session of its own, so it has no controlling
// terminal: input pushed into one (TIOCSTI) is run by whatever reads it,
// outside the sandbox. The sidecar has one when started from a terminal.
func shellCmd(ctx context.Context, s spec, w *os.File) (*exec.Cmd, error) {
	var cmd *exec.Cmd
	attr := &syscall.SysProcAttr{Setpgid: true}
	if s.sandboxedRun != nil {
		r := s.sandboxedRun.run
		r.Shell, r.Args, r.Dir = s.shell, []string{"-c", s.command}, s.dir
		if s.args != nil {
			r.Args = s.args
		}
		var err error
		if cmd, err = s.sandboxedRun.boxer.Command(ctx, r); err != nil {
			return nil, err
		}
		attr = &syscall.SysProcAttr{Setsid: true}
	} else {
		cmd = exec.CommandContext(ctx, s.shell, "-c", s.command)
		cmd.Dir, cmd.Env = s.dir, outsideEnv(os.Environ(), s.env, s.dir)
	}
	cmd.Stdout, cmd.Stderr = w, w
	cmd.SysProcAttr = attr
	return cmd, nil
}

// stopBash is cmd.Cancel. exec can run it after the OS has reaped bash and
// before Wait returns, so a bash already collected exited on its own: nothing is
// recorded and nothing is sent to a group id that may be free by now.
func stopBash(parent context.Context, cmd *exec.Cmd, g *guard, killGrace time.Duration) error {
	if errors.Is(cmd.Process.Signal(syscall.Signal(0)), os.ErrProcessDone) {
		return os.ErrProcessDone
	}
	why := stopFor(parent)
	if !g.record(why) {
		return nil
	}
	if why == stopCancel {
		return signalGroup(cmd, syscall.SIGKILL)
	}
	go g.escalate(parent, killGrace, func() { _ = signalGroup(cmd, syscall.SIGKILL) })
	return signalGroup(cmd, syscall.SIGTERM)
}

// signalGroup sends sig to the command's process group: bash and everything it
// started that stayed in it. Called only once Start has succeeded.
func signalGroup(cmd *exec.Cmd, sig syscall.Signal) error {
	return syscall.Kill(-cmd.Process.Pid, sig)
}
