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

package bash

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/tools"
)

// taskOut is a task's output file in a directory of the test's own.
func taskOut(t *testing.T) *os.File {
	t.Helper()
	f, err := os.Create(filepath.Join(t.TempDir(), "task.output"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = f.Close() })
	return f
}

// contents is what a file holds.
func contents(t *testing.T, f *os.File) string {
	t.Helper()
	b, err := os.ReadFile(f.Name())
	require.NoError(t, err)
	return string(b)
}

// A task writes stdout and stderr to its file as one stream, and its exit is
// bash's own.
func TestATaskWritesItsOutputAndReportsItsExit(t *testing.T) {
	tl := tool(t)
	out := taskOut(t)
	tk, err := startTask(t.Context(), tl.spec("echo out; echo err >&2; exit 3", 1024), out)
	require.NoError(t, err)
	assert.Equal(t, tools.Exit{Code: 3, OK: true}, tk.Wait())
	assert.Equal(t, "out\nerr\n", contents(t, out))
}

// The file stops at the limit and the rest is drained and dropped, so a command
// writing far past a pipe's buffer still runs to its end.
func TestTheOutputFileStopsAtTheLimit(t *testing.T) {
	tl := tool(t)
	out := taskOut(t)
	tk, err := startTask(t.Context(), tl.spec("yes | head -c 1000000; echo done >&2", 1000), out)
	require.NoError(t, err)
	assert.Equal(t, tools.Exit{Code: 0, OK: true}, tk.Wait())
	assert.Len(t, contents(t, out), 1000)
}

// A write that fails keeps the pipe draining, so the command is not blocked on
// a file it can no longer fill.
func TestAFailedWriteStillDrains(t *testing.T) {
	out := taskOut(t)
	require.NoError(t, out.Close())
	w := &taskWriter{w: out, limit: 100}
	n, err := w.Write([]byte("hello"))
	assert.Equal(t, 5, n)
	require.NoError(t, err)
	assert.Zero(t, w.written)
	n, _ = w.Write([]byte("again"))
	assert.Equal(t, 5, n)
}

// fakeTasks starts tasks with their files in dir, or refuses with err.
type fakeTasks struct {
	dir     chatDir
	err     error
	started []tools.Task
	outs    []*os.File
}

// newFakeTasks is a fakeTasks over a directory of its own, closing each output
// file it opened before the directory is removed: Windows removes no open file.
func newFakeTasks(t *testing.T) *fakeTasks {
	f := &fakeTasks{dir: chatDir(t.TempDir())}
	t.Cleanup(func() {
		for _, out := range f.outs {
			_ = out.Close()
		}
	})
	return f
}

func (f *fakeTasks) Start(start func(*os.File) (tools.Task, error)) (string, string, error) {
	if f.err != nil {
		return "", "", f.err
	}
	path := filepath.Join(string(f.dir), "t1.output")
	out, err := os.Create(path)
	if err != nil {
		return "", "", err
	}
	f.outs = append(f.outs, out)
	tk, err := start(out)
	if err != nil {
		return "", "", err
	}
	f.started = append(f.started, tk)
	return "t1", path, nil
}

func (f *fakeTasks) Stop(string) bool { return false }

// background is the input that runs line in the background.
func background(line string) json.RawMessage {
	b, _ := json.Marshal(map[string]any{"command": line, "run_in_background": true})
	return b
}

// A background call answers at once in the reference's words, with the task
// still running behind it.
func TestABackgroundCallAnswersAtOnce(t *testing.T) {
	tl := tool(t)
	tasks := newFakeTasks(t)
	text, isError := tl.Run(t.Context(), tools.Runtime{Dir: testChatDir(t), Tasks: tasks}, background("sleep 60"))
	assert.False(t, isError, text)
	assert.Equal(t, "Command running in background with ID: t1. Output is being written to: "+
		filepath.Join(string(tasks.dir), "t1.output")+
		". You will be notified when it completes. To check interim output, use Read on that file path.", text)
	require.Len(t, tasks.started, 1)
	tasks.started[0].Stop(true)
	assert.True(t, tasks.started[0].Wait().Stopped, "it was still running when stopped")
}

// A start the chat refuses answers the model in words: each limit its own, and
// anything else as a command that could not start.
func TestARefusedStartIsAnswered(t *testing.T) {
	tl := tool(t)
	for err, want := range map[error]string{
		tools.ErrChatTaskLimit:     "At most 4 background commands, agents and watches run at once in a chat. Wait for a notification, or stop one with TaskStop.",
		tools.ErrTaskLimit:         "At most 16 background commands, agents and watches run at once in Kubetail. Wait for one to finish.",
		errors.New("disk is full"): "could not start: disk is full",
	} {
		text, isError := tl.Run(t.Context(), tools.Runtime{Dir: testChatDir(t), Tasks: &fakeTasks{err: err}}, background("sleep 60"))
		assert.True(t, isError)
		assert.Equal(t, want, text)
	}
}

// A background call is checked as a foreground one is: a workdir resolveWorkdir
// refuses is bad input, a missing directory starts nothing, and a shell that
// will not start is a command that could not start.
func TestABackgroundCallIsCheckedLikeAnyOther(t *testing.T) {
	tl := tool(t)
	tasks := newFakeTasks(t)
	in := func(line, workdir string) json.RawMessage {
		b, _ := json.Marshal(map[string]any{"command": line, "workdir": workdir, "run_in_background": true})
		return b
	}

	text, isError := tl.Run(t.Context(), tools.Runtime{Dir: testChatDir(t), Tasks: tasks}, in("ls", "~other"))
	assert.True(t, isError)
	assert.Equal(t, `{"error":"bad-input"}`, text)

	missing := filepath.Join(t.TempDir(), "gone")
	text, isError = tl.Run(t.Context(), tools.Runtime{Dir: testChatDir(t), Tasks: tasks}, in("ls", missing))
	assert.True(t, isError)
	assert.Equal(t, "could not start: "+missing+" is not a directory", text)

	tl.shell = filepath.Join(t.TempDir(), "no-such-shell")
	text, isError = tl.Run(t.Context(), tools.Runtime{Dir: testChatDir(t), Tasks: tasks}, background("ls"))
	assert.True(t, isError)
	assert.Contains(t, text, "could not start: ")
	assert.Empty(t, tasks.started)
}

// A cancel that lands while the call waits on the snapshot starts nothing: the
// command would otherwise run after the turn ended, without the user's profile.
func TestACancelDuringTheSnapshotWaitStartsNoTask(t *testing.T) {
	tl := tool(t)
	tl.ready = make(chan struct{}) // a snapshot still being taken
	ctx, cancel := context.WithCancel(t.Context())
	tl.onSnapshotWait = cancel
	tasks := newFakeTasks(t)

	text, isError := tl.Run(ctx, tools.Runtime{Dir: testChatDir(t), Tasks: tasks}, background("echo ran"))
	assert.True(t, isError)
	assert.Equal(t, "could not start: context canceled", text)
	assert.Empty(t, tasks.started)
}
