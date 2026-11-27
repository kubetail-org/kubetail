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
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/tools"
)

// A task keeps its script and its job while it runs, since bash reads the
// script as it goes and closing the job's handle kills the tree; the reap
// releases both.
func TestATaskKeepsItsScriptAndJob(t *testing.T) {
	t.Parallel()

	tl := tool(t)
	tk, err := startTask(t.Context(), tl.spec("sleep 60", 1024), taskOut(t))
	require.NoError(t, err)
	_, err = os.Stat(tk.script)
	require.NoError(t, err, "the script is kept while bash runs")

	tk.Stop(true)
	assert.Equal(t, tools.Exit{Code: cancelCode, OK: true, Stopped: true}, tk.Wait())
	_, err = os.Stat(tk.script)
	assert.True(t, os.IsNotExist(err), "the reap removes the script")
}

// A stop without now still ends the job at once: there is no SIGTERM to give.
func TestAStopEndsTheJobAtOnce(t *testing.T) {
	t.Parallel()

	tl := tool(t)
	tk, err := startTask(t.Context(), tl.spec("sleep 60", 1024), taskOut(t))
	require.NoError(t, err)
	tk.Stop(false)
	assert.Equal(t, tools.Exit{Code: timeoutCode, OK: true, Stopped: true}, tk.Wait())
}

// A scripts directory that cannot be made, or written in, starts no task.
func TestATaskWithoutItsScriptsDirectoryDoesNotStart(t *testing.T) {
	t.Parallel()

	for _, dir := range []string{notADir(t), unwritableDir(t)} {
		_, err := startTask(t.Context(), spec{scripts: dir, command: "echo ran"}, taskOut(t))
		assert.Error(t, err, dir)
	}
}

// A stop once the reap has begun records nothing and sends nothing.
func TestAStopAfterTheReapIsIgnored(t *testing.T) {
	t.Parallel()

	tk := &task{g: newGuard(hooks{})}
	tk.g.reap()
	tk.Stop(true)
	assert.Equal(t, stopNone, tk.g.stop)
}

// A task whose bash cannot be resumed does not start, and leaves no script.
func TestATaskThatCannotResumeDoesNotStart(t *testing.T) {
	t.Parallel()

	tl := tool(t)
	s := tl.spec("echo ran", 1024)
	s.hooks = failing(failResume)
	_, err := startTask(t.Context(), s, taskOut(t))
	assert.ErrorIs(t, err, errInjected)
	left, err := filepath.Glob(filepath.Join(tl.scripts, "cmd-*"))
	require.NoError(t, err)
	assert.Empty(t, left)
}
