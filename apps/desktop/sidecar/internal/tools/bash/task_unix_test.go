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
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/apimeta"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/testutil"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/cluster"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/tools"
)

// startHeld starts trap, then the holder's sleep in the background, then tail,
// as a task whose SIGKILL follows SIGTERM after killGrace, and waits until the
// sleep is up.
func startHeld(t *testing.T, trap, tail string, killGrace time.Duration) (*task, *holder) {
	t.Helper()
	tl := tool(t)
	h := newHolder(t)
	s := tl.spec(trap+"; "+h.command(tail), 1024)
	s.killGrace = killGrace
	tk, err := startTask(t.Context(), s, taskOut(t))
	require.NoError(t, err)
	h.awaitStarted(t)
	return tk, h
}

// A stop sends SIGTERM first: a trap on it runs, and the task reads as stopped
// whatever the trap exits with.
func TestAStopSendsTermFirst(t *testing.T) {
	t.Parallel()

	tk, h := startHeld(t, `trap 'exit 0' TERM`, "; wait", time.Hour)
	tk.Stop(false)
	assert.Equal(t, tools.Exit{Code: 0, OK: true, Stopped: true}, tk.Wait())
	h.awaitGone(t)
}

// A group that ignores SIGTERM gets SIGKILL once the grace is out.
func TestAStopKillsWhatIgnoresTerm(t *testing.T) {
	t.Parallel()

	tk, h := startHeld(t, `trap '' TERM`, blocked(t), time.Millisecond)
	tk.Stop(false)
	assert.Equal(t, tools.Exit{Code: 137, OK: true, Stopped: true}, tk.Wait())
	h.awaitGone(t)
}

// A stop now does not wait the grace out, whatever the command traps.
func TestAStopNowKillsAtOnce(t *testing.T) {
	t.Parallel()

	tk, h := startHeld(t, `trap '' TERM`, blocked(t), time.Hour)
	tk.Stop(false)
	tk.Stop(true)
	assert.Equal(t, tools.Exit{Code: 137, OK: true, Stopped: true}, tk.Wait())
	h.awaitGone(t)
}

// The call's context bounds a task's start alone: the task keeps running once
// it ends, and exits as its command does.
func TestATaskOutlivesTheContextItStartedUnder(t *testing.T) {
	t.Parallel()

	tl := tool(t)
	fifo := filepath.Join(t.TempDir(), "go")
	require.NoError(t, syscall.Mkfifo(fifo, 0o600))
	ctx, cancel := context.WithCancel(t.Context())

	tk, err := startTask(ctx, tl.spec("read -r _ < '"+fifo+"'; exit 5", 1024), taskOut(t))
	require.NoError(t, err)
	cancel()

	// O_RDWR never waits for a reader, so a task killed before it read cannot
	// hold the test.
	f, err := os.OpenFile(fifo, os.O_RDWR, 0)
	require.NoError(t, err)
	_, err = f.WriteString("\n")
	require.NoError(t, err)
	assert.Equal(t, tools.Exit{Code: 5, OK: true}, tk.Wait())
	require.NoError(t, f.Close())
}

// A stop after the reap sends nothing and changes nothing.
func TestATaskStopAfterTheReapDoesNothing(t *testing.T) {
	t.Parallel()

	tl := tool(t)
	tk, err := startTask(t.Context(), tl.spec("exit 4", 1024), taskOut(t))
	require.NoError(t, err)
	assert.Equal(t, tools.Exit{Code: 4, OK: true}, tk.Wait())
	tk.Stop(true)
	tk.Stop(false)
	assert.False(t, tk.g.stop != stopNone, "nothing recorded after the reap")
}

// Bash's exit kills its group, as a foreground command's does: a sleep left
// behind in it is gone once Wait returns.
func TestATasksGroupDiesWithBash(t *testing.T) {
	t.Parallel()

	tl := tool(t)
	h := newHolder(t)
	tk, err := startTask(t.Context(), tl.spec(h.command(""), 1024), taskOut(t))
	require.NoError(t, err)
	assert.Equal(t, tools.Exit{Code: 0, OK: true}, tk.Wait())
	h.awaitStarted(t)
	h.awaitGone(t)
}

// A descendant that left the group and holds the pipe does not hold Wait past
// the pipe's grace.
func TestATaskPipeHeldPastTheGraceIsLetGo(t *testing.T) {
	// t.Setenv sets the process's environment, so this test stays serial.
	tl := tool(t)
	exe, err := os.Executable()
	require.NoError(t, err)
	dir := t.TempDir()
	ready, release := filepath.Join(dir, "ready"), filepath.Join(dir, "release")
	require.NoError(t, syscall.Mkfifo(ready, 0o600))
	require.NoError(t, syscall.Mkfifo(release, 0o600))
	t.Setenv("KUBETAIL_BASH_HELPER_READY", ready)
	t.Setenv("KUBETAIL_BASH_HELPER_RELEASE", release)

	s := tl.spec("'"+exe+"' & read -r _ < '"+ready+"'; echo done; exit 0", 1024)
	s.pipeGrace = 0
	out := taskOut(t)
	tk, err := startTask(t.Context(), s, out)
	require.NoError(t, err)
	assert.Equal(t, tools.Exit{Code: 0, OK: true}, tk.Wait())

	// The helper holds the pipe until release reaches EOF.
	f, err := os.OpenFile(release, os.O_WRONLY, 0)
	require.NoError(t, err)
	require.NoError(t, f.Close())
	assert.Equal(t, "done\n", contents(t, out))
}

// A sandboxed task's run directory stays while the task runs and goes once
// Wait has reaped it.
func TestATasksRunDirectoryGoesAtWait(t *testing.T) {
	t.Parallel()

	tl := clusterTool(t, map[apimeta.ClusterID]*cluster.Cluster{"7": kubeCluster("prod", "uid")})
	rt := tools.Runtime{ClusterID: "7", Dir: testChatDir(t), Tasks: newFakeTasks(t)}
	release := filepath.Join(t.TempDir(), "release")
	require.NoError(t, syscall.Mkfifo(release, 0o600))

	text, isError := tl.Run(t.Context(), rt, background(`echo "$ZDOTDIR $TMPDIR" > where; cat `+release))
	require.False(t, isError, text)
	tasks := rt.Tasks.(*fakeTasks)
	require.Len(t, tasks.started, 1)
	where := filepath.Join(tools.WorkspacePath(rt.Dir), "where")
	var dirs []string
	require.Eventually(t, func() bool {
		b, _ := os.ReadFile(where)
		dirs = strings.Fields(string(b))
		return len(dirs) == 2
	}, testutil.Timeout, time.Millisecond)
	assert.FileExists(t, filepath.Join(dirs[0], "kubeconfig"), "while the task runs")
	assert.DirExists(t, dirs[1], "while the task runs")

	f, err := os.OpenFile(release, os.O_WRONLY, 0)
	require.NoError(t, err)
	require.NoError(t, f.Close())
	assert.Equal(t, 0, tasks.started[0].Wait().Code)
	assert.NoDirExists(t, dirs[0])
	assert.NoDirExists(t, dirs[1])
}

// A task that never starts leaves no run directory: one the chat refused, and
// one whose call was cancelled before its start.
func TestATaskThatNeverStartsLeavesNoRunDirectory(t *testing.T) {
	t.Parallel()

	cancelled, cancel := context.WithCancel(t.Context())
	cancel()
	for name, c := range map[string]struct {
		ctx   context.Context
		tasks *fakeTasks
	}{
		"refused":   {t.Context(), &fakeTasks{dir: chatDir(t.TempDir()), err: tools.ErrChatTaskLimit}},
		"cancelled": {cancelled, newFakeTasks(t)},
	} {
		t.Run(name, func(t *testing.T) {
			tl := tool(t)
			tl.sandboxer = &fakeSandboxer{}

			_, isError := tl.Run(c.ctx, tools.Runtime{Dir: testChatDir(t), Tasks: c.tasks}, background("true"))

			assert.True(t, isError)
			assert.Empty(t, c.tasks.started)
			for _, dir := range []string{tl.runsDir, tl.tmpDir} {
				entries, err := os.ReadDir(dir)
				require.NoError(t, err)
				assert.Empty(t, entries, dir)
			}
		})
	}
}

// A background call on a cluster that is gone could not start, as a
// foreground one could not.
func TestAGoneClusterCouldNotStartATask(t *testing.T) {
	t.Parallel()

	tl := clusterTool(t, nil)
	tasks := newFakeTasks(t)

	text, isError := tl.Run(t.Context(), tools.Runtime{ClusterID: "7", Dir: testChatDir(t), Tasks: tasks}, background("true"))

	assert.True(t, isError)
	assert.True(t, strings.HasPrefix(text, "could not start: "), text)
	assert.Empty(t, tasks.started)
}
