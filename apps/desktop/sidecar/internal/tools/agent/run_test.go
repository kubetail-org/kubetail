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

package agent

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/tools"
)

// spawner answers every delegation with id and err, and keeps what it was asked.
type spawner struct {
	id    string
	err   error
	asked []tools.Delegation
}

func (s *spawner) Start(_ context.Context, d tools.Delegation) (string, error) {
	s.asked = append(s.asked, d)
	return s.id, s.err
}

// chatDir is a chat's directory of the test's own.
type chatDir string

func (d chatDir) Path() string { return string(d) }

func (d chatDir) Root(create bool) (*os.Root, error) {
	if create {
		if err := os.MkdirAll(string(d), 0o700); err != nil {
			return nil, err
		}
	}
	return os.OpenRoot(string(d))
}

// run is one call of the tool with sp as the chat's spawner.
func run(t *testing.T, sp tools.Spawner, input string) (string, bool) {
	t.Helper()
	return New().Run(t.Context(), tools.Runtime{Agent: sp, Dir: chatDir(filepath.Join(t.TempDir(), "c1"))}, json.RawMessage(input))
}

// A call hands its task to the spawner and answers at once with the agent's id,
// telling the model its report comes later.
func TestTheCallAnswersWithTheId(t *testing.T) {
	sp := &spawner{id: "task-7"}

	got, isError := run(t, sp, `{"description":"find pending pods","prompt":"List the pending pods.","model":"big"}`)

	assert.False(t, isError)
	assert.Equal(t, "Agent launched: task-7. It is working in the background. You will be told when it finishes. "+
		"You know nothing about its report until then — do not report, assume, or predict it; continue with other work "+
		"or answer the user in the meantime. Do not duplicate its work. To stop it, call TaskStop with its id.", got)
	assert.Equal(t, []tools.Delegation{{Type: GeneralPurpose, Prompt: "List the pending pods.", Model: "big"}}, sp.asked)
}

// Each way a start can fail is a refusal the model can act on: a limit says what
// to do, and any other error is the start's, as bash's is.
func TestALimitIsARefusal(t *testing.T) {
	cases := map[string]struct {
		err  error
		want string
	}{
		"the chat's limit": {tools.ErrChatTaskLimit, "At most 4 background commands, agents and watches run at once in a chat. " +
			"Wait for a notification, or stop one with TaskStop."},
		"the app's limit":  {tools.ErrTaskLimit, "At most 16 background commands, agents and watches run at once in Kubetail. Wait for one to finish."},
		"an unknown model": {tools.ErrUnknownModel, `{"error":"bad-input","field":"model"}`},
		"a failed write":   {errors.New("disk I/O error"), "could not start: disk I/O error"},
	}
	for name, c := range cases {
		got, isError := run(t, &spawner{id: "ignored", err: c.err}, `{"description":"d","prompt":"p"}`)
		assert.True(t, isError, name)
		assert.Equal(t, c.want, got, name)
	}
}

// A chat with no spawner runs no agent: a subagent's runtime has none.
func TestWithNoSpawnerNothingRuns(t *testing.T) {
	got, isError := New().Run(t.Context(), tools.Runtime{}, json.RawMessage(`{"description":"d","prompt":"p"}`))

	assert.True(t, isError)
	assert.Equal(t, `{"error":"not-run"}`, got)
}

// A type no agent has is refused with the list, and nothing is spawned.
func TestAnUnknownTypeIsRefusedWithTheList(t *testing.T) {
	sp := &spawner{}

	got, isError := run(t, sp, `{"description":"d","prompt":"p","subagent_type":"Explore"}`)

	assert.True(t, isError)
	assert.Equal(t, "Unknown agent type. Available: general-purpose.", got)
	assert.Empty(t, sp.asked)
}

// An input the tool refuses spawns nothing.
func TestABadPromptInsertsNothing(t *testing.T) {
	sp := &spawner{}

	got, isError := run(t, sp, `{"description":"d","prompt":"  "}`)

	assert.True(t, isError)
	assert.Equal(t, `{"error":"bad-input","field":"prompt"}`, got)
	assert.Empty(t, sp.asked)
}
