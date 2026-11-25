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

// A background command: started like a foreground one, its output written to
// a file as it arrives rather than captured, and its process left to the
// holder's Wait and Stop. The process itself is task_unix.go and task_windows.go.
package bash

import (
	"context"
	"io"
	"os"
	"slices"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/safe"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/tools"
)

// runTask starts a background command as the chat's task, checked as a
// foreground one is, and answers in the reference's words once it is running.
// It keeps the network and the variables its approval decided, a, for its
// whole life.
func (t *Tool) runTask(ctx context.Context, rt tools.Runtime, in input, a tools.Approval) (string, bool) {
	cwd, err := t.startDir(in, rt)
	if err != nil {
		return `{"error":"bad-input"}`, true
	}
	if err := makeWorkspace(rt.Dir); err != nil {
		return "could not start: " + safe.String(err.Error()), true
	}
	if err := t.checkDir(ctx, cwd); err != nil {
		return "could not start: " + safe.String(err.Error()), true
	}
	boxer := t.sandboxerFor(rt)
	snapshot, err := t.snapshotFor(ctx, boxer != nil)
	if err != nil {
		return "could not start: " + safe.String(err.Error()), true
	}
	s := spec{
		shell: t.shell, dir: cwd, scripts: t.scripts, env: slices.Concat(a.Env, t.env),
		command: wrapper(t.kind, snapshot, in.Command),
		capture: tools.TaskOutputLimit, killGrace: killGrace, pipeGrace: pipeGrace,
	}
	// Spelled out rather than passed through: a nil *task is not a nil Task.
	id, path, err := rt.Tasks.Start(func(out *os.File) (tools.Task, error) {
		// The call's context bounds the start alone, so it is checked here too,
		// before the run's directory is made: a cancel during the snapshot wait
		// or the row write starts nothing.
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if boxer == nil {
			return startTask(ctx, s, out)
		}
		// Made here, with the process, so a start the chat refuses makes nothing.
		sandboxedRun, err := t.sandboxedRunFor(ctx, boxer, rt, cwd, true, a.Network)
		if err != nil {
			return nil, err
		}
		s.sandboxedRun = sandboxedRun
		task, err := startTask(ctx, s, out)
		if err != nil {
			sandboxedRun.end()
			return nil, err
		}
		return sandboxedTask{Task: task, run: sandboxedRun}, nil
	})
	if err != nil {
		return tools.StartRefusal(err), true
	}
	return "Command running in background with ID: " + id + ". Output is being written to: " + path +
		". You will be notified when it completes. To check interim output, use Read on that file path.", false
}

// sandboxedTask is a sandboxed task, which owns its run: its proxy and its
// directory end once Wait has reaped the task.
type sandboxedTask struct {
	tools.Task
	run *sandboxedRun
}

func (t sandboxedTask) Wait() tools.Exit {
	exit := t.Task.Wait()
	t.run.end()
	return exit
}

// taskWriter writes to a task's file up to limit and discards the rest, while
// the process keeps running. A failed write is dropped the same way: the pipe
// must keep draining or the command blocks on it.
type taskWriter struct {
	w       io.Writer
	limit   int
	written int
}

func (w *taskWriter) Write(p []byte) (int, error) {
	if keep := min(len(p), max(w.limit-w.written, 0)); keep > 0 {
		n, _ := w.w.Write(p[:keep])
		w.written += n
		if n < keep {
			w.limit = w.written
		}
	}
	return len(p), nil
}
