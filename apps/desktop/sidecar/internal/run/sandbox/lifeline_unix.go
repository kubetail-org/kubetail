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
	"os"
	"os/exec"
	"strconv"

	"golang.org/x/sys/unix"
)

// newLifeline makes the lifeline (Sandbox.lifeline, Sandbox.held). held is a
// close-on-exec duplicate of the write end, since the os.File's finalizer
// would close the original.
func newLifeline() (lifeline *os.File, held int, err error) {
	r, w, err := os.Pipe()
	if err != nil {
		return nil, 0, err
	}
	defer w.Close()
	held, err = unix.FcntlInt(w.Fd(), unix.F_DUPFD_CLOEXEC, 0)
	if err != nil {
		r.Close()
		return nil, 0, err
	}
	return r, held, nil
}

// errClosed is Command's answer once Close ran.
var errClosed = errors.New("the sandbox is closed")

// Close frees the lifeline, which ends every run s started as this process's
// end would, and refuses every run after. A second Close, or one on a nil
// Sandbox, does nothing.
func (s *Sandbox) Close() error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true
	if s.lifeline == nil {
		return nil
	}
	err := errors.Join(s.lifeline.Close(), unix.Close(s.held))
	s.lifeline, s.held = nil, -1
	return err
}

// lifelineArgs is the --lifeline flag that names lifelineFD to the process
// handLifeline hands it to; nil when s has none.
func (s *Sandbox) lifelineArgs() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.lifeline == nil {
		return nil
	}
	return []string{lifelineFlag, strconv.Itoa(lifelineFD)}
}

// handLifeline hands s's lifeline, if any, to cmd on lifelineFD, or refuses
// cmd once s is closed. Command calls it after lifelineArgs, so a Close
// between the two refuses the run rather than handing it a flag with no
// descriptor behind it.
func (s *Sandbox) handLifeline(cmd *exec.Cmd) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return errClosed
	}
	if s.lifeline != nil {
		cmd.ExtraFiles = []*os.File{s.lifeline}
	}
	return nil
}

// endGroupAtEOF watches the lifeline on fd and, at its EOF, kills the
// caller's process group. The forwarder leads the run's group, so one kill
// reaches every process in it, the caller too, and the caller waits for its
// own: macOS delivers it after kill returns, so an exit would race it. As a
// PID namespace's first process the kill passes it by, and its exit ends the
// namespace. sandbox-pasta leads pasta's: pasta killed during setup leaves
// the child it made for the run's namespaces, which no Pdeathsig reaches.
func endGroupAtEOF(fd int) error {
	return watchLifeline(fd, func() { // coverage:ignore runs at the lifeline's EOF in sandbox-init or sandbox-pasta, a child that writes no coverage
		_ = unix.Kill(0, unix.SIGKILL)
		if os.Getpid() != 1 {
			select {}
		}
		os.Exit(initFailed)
	})
}

// watchLifeline closes fd on exec, so no child inherits it, and calls end
// once it reads EOF. Nothing writes to the lifeline, so any read's end is the
// sidecar's. It fails when fd is not open.
func watchLifeline(fd int, end func()) error {
	if _, err := unix.FcntlInt(uintptr(fd), unix.F_SETFD, unix.FD_CLOEXEC); err != nil {
		return err
	}
	f := os.NewFile(uintptr(fd), "lifeline")
	go func() {
		_, _ = io.Copy(io.Discard, f)
		end()
	}()
	return nil
}
