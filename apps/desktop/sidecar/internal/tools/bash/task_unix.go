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
	"io"
	"os"
	"os/exec"
	"syscall"
	"time"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/run/sandbox"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/tools"
)

// task is bash in a process group of its own, its output copied to a file.
type task struct {
	cmd       *exec.Cmd
	g         *guard
	killGrace time.Duration
	pipeGrace time.Duration
	r         *os.File        // the output pipe's read end
	read      <-chan struct{} // closed when the copy off it ends
}

var _ tools.Task = (*task)(nil)

// startTask starts s's command as run does, but ctx bounds only its start —
// the sandbox's preparation and Start — and it then runs until it exits or
// Stop ends it. Its output goes to out as it arrives, up to s.capture.
func startTask(ctx context.Context, s spec, out io.Writer) (*task, error) {
	r, w, err := os.Pipe()
	if err != nil { // coverage:ignore a pipe fails only when the process is out of descriptors
		return nil, err
	}
	cmd, err := shellCmd(ctx, s, w)
	if err != nil {
		_ = w.Close()
		_ = r.Close()
		return nil, err
	}
	// With no Cancel, ctx ending after Start leaves the task running.
	cmd.Cancel = nil
	if err := cmd.Start(); err != nil {
		_ = w.Close()
		_ = r.Close()
		return nil, err
	}
	_ = w.Close()
	return &task{
		cmd: cmd, g: newGuard(s.hooks), killGrace: s.killGrace, pipeGrace: s.pipeGrace,
		r: r, read: copyOutput(&taskWriter{w: out, limit: s.capture}, r),
	}, nil
}

// Wait reaps bash, kills what it left in its group, as run does, and waits for
// the copy to drain.
func (t *task) Wait() tools.Exit {
	_ = t.cmd.Wait()
	exit := tools.Exit{Stopped: t.g.reap() != stopNone}
	_ = signalGroup(t.cmd, syscall.SIGKILL)
	awaitCopy(t.read, t.r, t.pipeGrace)
	_ = t.r.Close()
	if t.cmd.ProcessState != nil {
		exit.Code, exit.OK = sandbox.ExitCode(t.cmd.ProcessState), true
	}
	return exit
}

// Stop signals the group under the guard's lock, so nothing is sent once bash
// is reaped and its group id may belong to another process.
func (t *task) Stop(now bool) {
	t.g.mu.Lock()
	defer t.g.mu.Unlock()
	if t.g.reaped {
		return
	}
	t.g.stop = stopCancel
	if now {
		_ = signalGroup(t.cmd, syscall.SIGKILL)
		return
	}
	_ = signalGroup(t.cmd, syscall.SIGTERM)
	go t.g.escalate(context.Background(), t.killGrace, func() { _ = signalGroup(t.cmd, syscall.SIGKILL) })
}
