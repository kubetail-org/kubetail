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

package taskstop

import (
	"encoding/json"
	"io/fs"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/tools"
)

// fakeTasks holds the tasks named in running.
type fakeTasks struct {
	running map[string]bool
	stopped []string
}

func (f *fakeTasks) Start(func(*os.File) (tools.Task, error)) (string, string, error) {
	return "", "", fs.ErrNotExist
}

func (f *fakeTasks) Stop(id string) bool {
	if !f.running[id] {
		return false
	}
	f.stopped = append(f.stopped, id)
	return true
}

// TaskStop stops a task of the chat it runs in by the id its start answered,
// and says so; an id the chat holds no running task by gets one text, whatever
// the reason.
func TestTaskStopStopsTheChatsOwnTask(t *testing.T) {
	tasks := &fakeTasks{running: map[string]bool{"t1": true}}
	tl, rt := New(), tools.Runtime{Tasks: tasks}

	text, isError := tl.Run(t.Context(), rt, json.RawMessage(`{"task_id":"t1"}`))
	assert.False(t, isError)
	assert.Equal(t, "Stopped t1.", text)
	assert.Equal(t, []string{"t1"}, tasks.stopped)

	text, isError = tl.Run(t.Context(), rt, json.RawMessage(`{"task_id":"t2"}`))
	assert.True(t, isError)
	assert.Equal(t, "No background command, agent or watch with that ID is running in this conversation.", text)
}

// The input is task_id alone, a non-empty string, spelled exactly and once.
func TestTaskStopReadsItsInput(t *testing.T) {
	tasks := &fakeTasks{running: map[string]bool{"t1": true}}
	tl, rt := New(), tools.Runtime{Tasks: tasks}
	for _, raw := range []string{
		`{}`, `{"task_id":""}`, `{"task_id":1}`, `{"task_id":null}`, `{"Task_Id":"t1"}`,
		`{"task_id":"t1","task_id":"t1"}`, `{"task_id":"t1","shell_id":"t1"}`, `{"task_id":"t1"} {}`, `[]`,
		`{"task_id":}`, `{"task_id":"t1"`,
	} {
		text, isError := tl.Run(t.Context(), rt, json.RawMessage(raw))
		assert.True(t, isError, raw)
		assert.Equal(t, `{"error":"bad-input"}`, text, raw)
	}
	assert.Empty(t, tasks.stopped)
}

// The offer is ungated: stopping touches only what the user already approved to
// run, and only in the chat that started it.
func TestTheTaskStopOffer(t *testing.T) {
	tl := New()
	def := tl.Definition()
	assert.Equal(t, "TaskStop", def.Name)
	assert.Equal(t, "Stops a background command, agent or log watch this conversation started, by its ID.", def.Description)
	var schema struct {
		Required   []string                   `json:"required"`
		Properties map[string]json.RawMessage `json:"properties"`
	}
	require.NoError(t, json.Unmarshal(def.InputSchema, &schema))
	assert.Equal(t, []string{"task_id"}, schema.Required)
	assert.Len(t, schema.Properties, 1)
	assert.Contains(t, tl.Prompt(), "## TaskStop")

	var tool tools.Tool = tl
	_, gated := tool.(tools.Gated)
	assert.False(t, gated)
}

// TaskStop's calls stop a task and show no action.
func TestTaskStopNamesItsKind(t *testing.T) {
	tool := New()
	assert.Equal(t, Name, tool.Name())
	assert.Equal(t, Name, tool.Definition().Name)
	assert.Equal(t, tools.ActionStop, tool.ActionKind())
	_, err := tool.Action(json.RawMessage(`{"task_id":"t1"}`), "", false)
	assert.Error(t, err)
}

// The prompt names every kind of task TaskStop stops, and that a stopped watch
// still tells what it held.
func TestThePromptNamesEveryKind(t *testing.T) {
	assert.Contains(t, New().Prompt(), "a background command, an agent or a log watch")
	assert.Contains(t, New().Prompt(), "an agent and a watch stop at once")
	assert.Contains(t, New().Prompt(), "except a watch's, which carries the lines it held")
}
