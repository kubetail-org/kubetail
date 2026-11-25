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
	"errors"
	"io"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

// The wait after a failed accept starts at minAcceptWait and doubles up to
// maxAcceptWait.
const (
	minAcceptWait = 5 * time.Millisecond
	maxAcceptWait = time.Second
)

// InitMain is sandbox-init, given the arguments after the subcommand. It
// answers the process's exit code.
func InitMain(args []string) int {
	a, err := parseInitArgs(args)
	if err != nil {
		return fail(os.Stderr, InitCommand, "bad arguments", err)
	}
	if a.stderrOnStdin {
		if err := takeStderr(); err != nil {
			return fail(os.Stderr, InitCommand, "cannot take the run's stderr", err)
		}
	}
	if a.lifeline >= 0 {
		if err := endGroupAtEOF(a.lifeline); err != nil {
			return fail(os.Stderr, InitCommand, "cannot watch the lifeline", err)
		}
	}
	if a.socket != "" {
		// Before the child starts, so a command never races it.
		addr := net.JoinHostPort("127.0.0.1", strconv.Itoa(a.port))
		ln, err := net.Listen("tcp", addr)
		if err != nil {
			return fail(os.Stderr, InitCommand, "cannot listen on "+addr, err)
		}
		go accept(ln, a.socket, minAcceptWait)
	}
	// The forwarder shares the child's process group, so a stop reaches both,
	// and it waits for the child either way. Caught and dropped, never
	// ignored: an ignored signal stays ignored across exec, and a shell cannot
	// trap one that was ignored when it started. A caught one is reset to its
	// default in the child.
	signal.Notify(make(chan os.Signal, 1), syscall.SIGTERM, syscall.SIGINT, syscall.SIGHUP)
	first := os.Getpid() == 1
	if first { // coverage:ignore only the sandbox's PID 1 guards its memory, and it writes no coverage
		if err := guardMemory(); err != nil {
			return fail(os.Stderr, InitCommand, "cannot guard its memory", err)
		}
	}
	// Inherited by the whole run, so no process of it dumps its memory outside
	// the sandbox. The run's other limits are sandbox-shell's, so they never
	// bind the forwarder, which lives as long as the run.
	if err := unix.Setrlimit(unix.RLIMIT_CORE, &unix.Rlimit{}); err != nil { // coverage:ignore lowering the core size to zero never fails
		return fail(os.Stderr, InitCommand, "cannot set the core size", err)
	}
	path, err := exec.LookPath(a.argv[0])
	if err != nil {
		return fail(os.Stderr, InitCommand, "cannot start "+a.argv[0], err)
	}
	pid, err := syscall.ForkExec(path, a.argv, &syscall.ProcAttr{Env: os.Environ(), Files: []uintptr{0, 1, 2}})
	if err != nil {
		return fail(os.Stderr, InitCommand, "cannot start "+a.argv[0], err)
	}
	return waitFor(pid, first)
}

// takeStderr puts the run's stderr, which sandbox-pasta passed on stdin, back
// on stderr and the null device on stdin, then writes runStarted on the stderr
// it replaced, which is sandbox-pasta's.
func takeStderr() error {
	pastas, err := unix.Dup(2)
	if err != nil {
		return err
	}
	defer unix.Close(pastas)
	if err := unix.Dup2(0, 2); err != nil { // coverage:ignore a closed descriptor 0 is the lowest free one, which the Dup above takes, so this Dup2 never fails
		return err
	}
	null, err := unix.Open(os.DevNull, unix.O_RDONLY, 0)
	if err != nil { // coverage:ignore the null device always opens, and with no descriptor free the Dup above fails first
		return err
	}
	defer unix.Close(null)
	if err := unix.Dup2(null, 0); err != nil { // coverage:ignore both descriptors were just opened
		return err
	}
	_, err = unix.Write(pastas, []byte(runStarted))
	return err
}

// waitFor waits for the child and answers how it ended. As a PID namespace's
// first process every orphan is its child too, so it reaps whatever ends
// until the child does; anywhere else it waits on the child alone, since a
// test calls InitMain in a process whose other children are not its own.
func waitFor(pid int, first bool) int {
	who := pid
	if first { // coverage:ignore only the sandbox's PID 1 reaps every child, and it writes no coverage
		who = -1
	}
	for {
		var ws syscall.WaitStatus
		got, err := syscall.Wait4(who, &ws, 0, nil)
		switch {
		case errors.Is(err, syscall.EINTR): // coverage:ignore Go installs its signal handlers with SA_RESTART, so the kernel restarts Wait4
			continue
		case err != nil:
			return fail(os.Stderr, InitCommand, "cannot wait for the child", err)
		case got == pid:
			return codeOf(ws)
		}
	}
}

// accept relays every connection ln accepts to socket until ln is closed. A
// failed accept is usually a passing one, such as a burst of relays holding
// every descriptor, so it waits and tries again, as net/http's server does.
func accept(ln net.Listener, socket string, minWait time.Duration) {
	var wait time.Duration
	for {
		c, err := ln.Accept()
		if errors.Is(err, net.ErrClosed) {
			return
		}
		if err != nil {
			wait = min(max(2*wait, minWait), maxAcceptWait)
			time.Sleep(wait)
			continue
		}
		wait = 0
		go relay(c, socket)
	}
}

// ExitCode is how a process ended as a shell reports it: 128 plus the signal
// that killed it, else its exit status. ProcessState.ExitCode answers -1 for a
// signal.
func ExitCode(state *os.ProcessState) int {
	return codeOf(state.Sys().(syscall.WaitStatus))
}

// codeOf is ExitCode for a raw wait status.
func codeOf(ws syscall.WaitStatus) int {
	if ws.Signaled() {
		return 128 + int(ws.Signal())
	}
	return ws.ExitStatus()
}

// relay carries conn to the run's socket and back, byte for byte, passing
// each side's end of input to the other, and closes both once both are done.
func relay(conn net.Conn, socket string) {
	defer conn.Close()
	up, err := net.Dial("unix", socket)
	if err != nil {
		return
	}
	defer up.Close()
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = io.Copy(up, conn)
		closeWrite(up)
	}()
	_, _ = io.Copy(conn, up)
	closeWrite(conn)
	<-done
}

// closeWrite half-closes c, where its kind can.
func closeWrite(c net.Conn) {
	if hc, ok := c.(interface{ CloseWrite() error }); ok {
		_ = hc.CloseWrite()
	}
}
