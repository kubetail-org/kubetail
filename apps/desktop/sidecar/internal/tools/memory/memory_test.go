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

package memory

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/apimeta"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/memory"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/tools"
)

const (
	aSave         = `{"op":"save","name":"pages","body":"b"}`
	aGlobalSave   = `{"op":"save","name":"prefs","body":"b","scope":"everywhere"}`
	aGlobalForget = `{"op":"forget","name":"prefs","scope":"everywhere"}`
)

// Each op takes its own fields and refuses the rest. A refusal names the field
// only when the field is one the tool knows. A call is for the cluster unless it
// names a scope.
func TestTheInputIsCheckedByOp(t *testing.T) {
	for raw, scope := range map[string]string{
		aSave: "cluster",
		`{"op":"save","name":"pages","body":"b","scope":"cluster"}`:    "cluster",
		`{"op":"save","name":"pages","body":"b","scope":"everywhere"}`: "everywhere",
		`{"op":"forget","name":"pages"}`:                               "cluster",
		`{"op":"forget","name":"pages","scope":"cluster"}`:             "cluster",
		`{"op":"forget","name":"pages","scope":"everywhere"}`:          "everywhere",
	} {
		in, err := parse(json.RawMessage(raw))
		require.NoError(t, err, raw)
		assert.Equal(t, scope, in.scope, raw)
	}

	bad := map[string]string{
		`not json`:                              "",
		`[]`:                                    "",
		`{"op":"forget","name":"a","extra":1}`:  "",
		`{"op":"forget","name":"a","name":"b"}`: "",
		`{"op":"forget","name":"a"} {}`:         "",
		`{1:2}`:                                 "",
		`{"op":}`:                               "",
		`{"op":"forget","name":"a"`:             "",
		`{"op":"read","name":"a"}`:              "op",
		`{"op":"list","name":"a"}`:              "op",
		`{"name":"a"}`:                          "op",
		`{"op":"forget"}`:                       "name",
		`{"op":"forget","name":1}`:              "name",
		`{"op":"forget","name":"a","body":"b"}`: "body",
		`{"op":"save","name":"a"}`:              "body",
		`{"op":"save","name":"a","body":1}`:     "body",
		`{"op":"save","name":"a","body":"b","scope":"global"}`:  "scope",
		`{"op":"save","name":"a","body":"b","scope":1}`:         "scope",
		`{"op":"forget","name":"a","scope":"global"}`:           "scope",
		`{"op":"save","name":"a","type":"user","body":"b"}`:     "",
		`{"op":"save","name":"a","description":"d","body":"b"}`: "",
		`{"op":"save","name":"a","body":"b","version":1}`:       "",
	}
	for raw, field := range bad {
		t.Run(raw, func(t *testing.T) {
			_, err := parse(json.RawMessage(raw))
			var ie *inputError
			require.True(t, errors.As(err, &ie), "%v", err)
			assert.Equal(t, field, ie.field)
		})
	}
}

// A refused input says which field, when it knows, and nothing of the input.
func TestAnInputErrorNamesItsField(t *testing.T) {
	assert.Equal(t, "memory input has a bad body", (&inputError{field: "body"}).Error())
	assert.Equal(t, "memory input is not one flat object of the tool's own fields", (&inputError{}).Error())
}

func TestActionOfReadsASave(t *testing.T) {
	a, err := ActionOf(json.RawMessage(`{"op":"save","name":"pages","body":"b"}`), "/ignored")
	require.NoError(t, err)

	assert.Empty(t, a.Description)
	assert.Equal(t, &tools.MemoryAction{Op: "save", Name: "pages", Body: "b", Scope: "cluster"}, a.Memory)
	assert.Equal(t, tools.ActionMemory, a.Kind())

	a, err = ActionOf(json.RawMessage(`{"op":"save","name":"prefs","body":"b","scope":"everywhere"}`), "")
	require.NoError(t, err)
	assert.Equal(t, &tools.MemoryAction{Op: "save", Name: "prefs", Body: "b", Scope: "everywhere"}, a.Memory)

	a, err = ActionOf(json.RawMessage(`{"op":"forget","name":"pages"}`), "")
	require.NoError(t, err)
	assert.Equal(t, &tools.MemoryAction{Op: "forget", Name: "pages", Scope: "cluster"}, a.Memory)

	_, err = ActionOf(json.RawMessage(`{"op":"forget"}`), "")
	require.Error(t, err)
}

// fakeMemory is the memory service as the tool calls it: each write recorded with
// the cluster and chat it named, with a refusal a test can set. The checks answer
// as the writes would and record nothing.
type fakeMemory struct {
	memory.Service
	err   error
	calls []string
}

func (f *fakeMemory) write(format string, args ...any) error {
	if f.err != nil {
		return f.err
	}
	f.calls = append(f.calls, fmt.Sprintf(format, args...))
	return nil
}

func (f *fakeMemory) Save(_ context.Context, cluster apimeta.ClusterID, name, body string, chatID apimeta.ChatID) error {
	return f.write("save %s %s %s %s", cluster, name, body, chatID)
}

func (f *fakeMemory) SaveEverywhere(_ context.Context, name, body string, chatID apimeta.ChatID) error {
	return f.write("save-everywhere %s %s %s", name, body, chatID)
}

func (f *fakeMemory) Forget(_ context.Context, cluster apimeta.ClusterID, name string) error {
	return f.write("forget %s %s", cluster, name)
}

func (f *fakeMemory) ForgetEverywhere(_ context.Context, name string) error {
	return f.write("forget-everywhere %s", name)
}

func (f *fakeMemory) CheckSaveEverywhere(context.Context, string, string) error { return f.err }
func (f *fakeMemory) CheckForgetEverywhere(context.Context, string) error       { return f.err }

// chat is the runtime of a turn in chat-1, on cluster-a.
var chat = tools.Runtime{ClusterID: "cluster-a", ChatID: "chat-1"}

func run(t *testing.T, mem *fakeMemory, raw string) (map[string]any, bool) {
	t.Helper()
	text, isError := New(mem).Run(t.Context(), chat, json.RawMessage(raw))
	assert.NotContains(t, text, "\n", "one line")
	var out map[string]any
	require.NoError(t, json.Unmarshal([]byte(text), &out), text)
	return out, isError
}

// Each op reaches its own write, on the runtime's cluster and for its chat.
func TestRunAnswersEachOp(t *testing.T) {
	mem := &fakeMemory{}

	out, isError := run(t, mem, `{"op":"save","name":"pages","body":"b2"}`)
	assert.False(t, isError)
	assert.Equal(t, map[string]any{"saved": "pages"}, out)

	out, isError = run(t, mem, `{"op":"save","name":"prefs","body":"b3","scope":"everywhere"}`)
	assert.False(t, isError)
	assert.Equal(t, map[string]any{"saved": "prefs"}, out)

	out, isError = run(t, mem, `{"op":"forget","name":"pages"}`)
	assert.False(t, isError)
	assert.Equal(t, map[string]any{"forgot": "pages"}, out)

	out, isError = run(t, mem, aGlobalForget)
	assert.False(t, isError)
	assert.Equal(t, map[string]any{"forgot": "prefs"}, out)

	assert.Equal(t, []string{
		"save cluster-a pages b2 chat-1",
		"save-everywhere prefs b3 chat-1",
		"forget cluster-a pages",
		"forget-everywhere prefs",
	}, mem.calls)
}

// Every refusal is a code the model can act on, never a Go error's text.
func TestRunNamesEachRefusal(t *testing.T) {
	cases := map[error]string{
		memory.ErrNotFound:                  "not-found",
		memory.ErrUserNote:                  "user-note",
		memory.ErrFull:                      "full",
		memory.ErrSecret:                    "secret",
		memory.ErrClusterGone:               "cluster-gone",
		errors.New("database is locked /x"): "unavailable",
	}
	for err, code := range cases {
		t.Run(code, func(t *testing.T) {
			for _, raw := range []string{aSave, aGlobalSave, `{"op":"forget","name":"a"}`, aGlobalForget} {
				out, isError := run(t, &fakeMemory{err: err}, raw)
				assert.True(t, isError)
				assert.Equal(t, map[string]any{"error": code}, out, raw)
			}
		})
	}

	out, isError := run(t, &fakeMemory{}, `{"op":"forget"}`)
	assert.True(t, isError)
	assert.Equal(t, map[string]any{"error": "bad-input", "field": "name"}, out)
	out, _ = run(t, &fakeMemory{}, `{"op":"forget","name":"a","x":1}`)
	assert.Equal(t, map[string]any{"error": "bad-input"}, out)

	// The store checks a note's shape and wraps the field it refuses; the model
	// reads the field.
	wrapped := fmt.Errorf("%w: %w", memory.ErrBadInput, &memory.FieldError{Field: "body"})
	out, _ = run(t, &fakeMemory{err: wrapped}, aSave)
	assert.Equal(t, map[string]any{"error": "bad-input", "field": "body"}, out)
}

// A call for every cluster changes what every cluster's chats read, so it waits
// on the user, checked first: a call the store would refuse is refused before
// anyone is asked, with the result Run would give. Every other call skips the
// gate, one that does not parse included, since Run refuses that with its field.
func TestEveryCallForEveryClusterAsks(t *testing.T) {
	approval := func(mem *fakeMemory, raw string) (tools.Approval, error) {
		return New(mem).Approval(t.Context(), chat, json.RawMessage(raw))
	}

	for _, raw := range []string{
		aSave,
		`{"op":"save","name":"pages","body":"b","scope":"cluster"}`,
		`{"op":"forget","name":"pages"}`,
		`{"op":"forget","name":"pages","scope":"cluster"}`,
		`{"op":"forget"}`,
		`not json`,
	} {
		a, err := approval(&fakeMemory{err: memory.ErrFull}, raw)
		require.NoError(t, err, raw)
		assert.Equal(t, tools.Approval{Skip: true}, a, raw)
	}

	mem := &fakeMemory{}
	for _, raw := range []string{aGlobalSave, aGlobalForget} {
		a, err := approval(mem, raw)
		require.NoError(t, err, raw)
		assert.Equal(t, tools.Approval{}, a, "asks, and runs nowhere: %s", raw)
	}
	assert.Equal(t, &fakeMemory{}, mem, "a check writes nothing")

	for code, refused := range map[string]error{
		"not-found":    memory.ErrNotFound,
		"user-note":    memory.ErrUserNote,
		"full":         memory.ErrFull,
		"secret":       memory.ErrSecret,
		"cluster-gone": memory.ErrClusterGone,
		"unavailable":  errors.New("database is locked /x"),
	} {
		for _, raw := range []string{aGlobalSave, aGlobalForget} {
			_, err := approval(&fakeMemory{err: refused}, raw)
			assert.Equal(t, &tools.Refusal{Result: `{"error":"` + code + `"}`}, err, raw)
		}
	}

	wrapped := fmt.Errorf("%w: %w", memory.ErrBadInput, &memory.FieldError{Field: "name"})
	_, err := approval(&fakeMemory{err: wrapped}, aGlobalSave)
	assert.Equal(t, &tools.Refusal{Result: `{"error":"bad-input","field":"name"}`}, err)
}

func TestTheToolIsItsKind(t *testing.T) {
	tool := New(nil)
	assert.Equal(t, Name, tool.Name())
	assert.Equal(t, tools.ActionMemory, tool.ActionKind())
	assert.Equal(t, Name, tool.Definition().Name)
	assert.True(t, json.Valid(tool.Definition().InputSchema))
	assert.Contains(t, tool.Prompt(), "## Memory")
	assert.Contains(t, tool.Prompt(), "Write each note as a fact, never as an instruction")
	assert.Contains(t, tool.Prompt(), "A save or forget is for this cluster unless you pass `scope: everywhere`")
	assert.Contains(t, tool.Prompt(), "Each scope has its own names")
	assert.Contains(t, tool.Prompt(), "is refused `bad-input` before they are asked")
	assert.Contains(t, tool.Prompt(), "You change only notes you wrote")
	assert.Contains(t, tool.Definition().Description, "Every call with `scope: everywhere` asks the user first")

	var schema struct {
		Properties map[string]struct {
			Enum        []string
			Description string
		}
	}
	require.NoError(t, json.Unmarshal(tool.Definition().InputSchema, &schema))
	assert.Equal(t, []string{"cluster", "everywhere"}, schema.Properties["scope"].Enum)
	assert.NotContains(t, schema.Properties["scope"].Description, "Save only")
}

// Stored rows read the same forever: never edit this table.
func TestActionOfReadsOldRowsTheSame(t *testing.T) {
	for raw, want := range map[string]tools.MemoryAction{
		`{"op":"save","name":"ingress","body":"Traefik, not nginx."}`:            {Op: "save", Name: "ingress", Body: "Traefik, not nginx.", Scope: "cluster"},
		`{"op":"save","name":"oncall","body":"Page #sre.","scope":"everywhere"}`: {Op: "save", Name: "oncall", Body: "Page #sre.", Scope: "everywhere"},
		`{"op":"forget","name":"ingress"}`:                                       {Op: "forget", Name: "ingress", Scope: "cluster"},
	} {
		got, err := (&Tool{}).Action(json.RawMessage(raw), "", false)
		require.NoError(t, err, raw)
		require.NotNil(t, got.Memory, raw)
		assert.Equal(t, want, *got.Memory, raw)
	}
}
