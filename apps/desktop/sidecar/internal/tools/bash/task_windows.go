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
	"context"
	"io"
	"os"
	"time"

	"golang.org/x/sys/windows"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/tools"
)

// task is bash on a script file inside a job object, its output copied to a
// file. It keeps the script and the job's handle until the reap: closing the
// handle kills the tree.
type task struct {
	c         child
	script    string
	g         *guard
	pipeGrace time.Duration
	read      <-chan struct{} // closed when the copy off c.out ends
}

var _ tools.Task = (*task)(nil)

// startTask starts s's command as run does, under no context: it runs until it
// exits or Stop ends it. Its output goes to out as it arrives, up to s.capture.
// Windows has no sandbox to prepare, so there is nothing for the call's ctx to
// bound.
func startTask(_ context.Context, s spec, out io.Writer) (*task, error) {
	if err := os.MkdirAll(s.scripts, 0o700); err != nil {
		return nil, err
	}
	script, err := writeScript(s.scripts, s.command)
	if err != nil {
		return nil, err
	}
	c, err := start(s.shell, []string{script}, s.dir, s.env, s.hooks)
	if err != nil {
		_ = os.Remove(script)
		return nil, err
	}
	if err := c.resume(); err != nil {
		_ = windows.TerminateJobObject(c.job, 1)
		c.close()
		_ = os.Remove(script)
		return nil, err
	}
	return &task{
		c: c, script: script, g: newGuard(s.hooks), pipeGrace: s.pipeGrace,
		read: copyOutput(&taskWriter{w: out, limit: s.capture}, c.out),
	}, nil
}

// Wait reaps bash, ends what it left in the job, as run does, waits for the
// copy to drain, then releases the job and the script.
func (t *task) Wait() tools.Exit {
	_, _ = windows.WaitForSingleObject(t.c.pi.Process, windows.INFINITE)
	exit := tools.Exit{Stopped: t.g.reap() != stopNone}
	_ = windows.TerminateJobObject(t.c.job, 1)
	awaitCopy(t.read, t.c.out, t.pipeGrace)
	var code uint32
	if t.c.sys.getExitCodeProcess(t.c.pi.Process, &code) == nil {
		exit.Code, exit.OK = int(code), true
	}
	t.c.close()
	_ = os.Remove(t.script)
	return exit
}

// Stop terminates the job at once, since there is no SIGTERM to give one, with
// the code a shell reports for the signal Unix would send. Under the guard's
// lock, so nothing is sent once the reap has begun releasing the job.
func (t *task) Stop(now bool) {
	t.g.mu.Lock()
	defer t.g.mu.Unlock()
	if t.g.reaped {
		return
	}
	t.g.stop = stopCancel
	code := uint32(timeoutCode)
	if now {
		code = cancelCode
	}
	_ = windows.TerminateJobObject(t.c.job, code)
}
