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

// Package taskstop is TaskStop: the model's stop of a background command its
// chat started. Ungated, since it only ends what the user already approved to
// run, and it reaches only the tasks of the chat its call runs in.
package taskstop

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"io"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/llm"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/tools"
)

//go:embed prompts/taskstop.md
var prompt string

//go:embed prompts/schema.json
var inputSchema []byte

// description is the tool description the model reads with the schema.
const description = "Stops a background command, agent or log watch this conversation started, by its ID."

// notRunning answers every id the chat holds no running task by, so the model
// learns nothing of another chat's.
const notRunning = "No background command, agent or watch with that ID is running in this conversation."

// Name is the name the tool is offered under.
const Name = "TaskStop"

// ContractName is the shape of the tool's stored calls, recorded on each row.
const ContractName tools.ContractName = "taskstop/v1"

// errNoAction is every call's action: a stop has nothing to show.
var errNoAction = errors.New("taskstop: a stop shows no action")

var _ tools.Custom = (*Tool)(nil)

// Tool is TaskStop.
type Tool struct{}

// New is the tool.
func New() *Tool { return &Tool{} }

func (t *Tool) Definition() llm.ToolDefinition {
	return llm.ToolDefinition{Name: Name, Description: description, InputSchema: json.RawMessage(inputSchema)}
}

func (t *Tool) Prompt() string { return prompt }

func (t *Tool) Name() string { return Name }

func (t *Tool) ContractName() tools.ContractName { return ContractName }

func (t *Tool) ActionKind() tools.ActionKind { return tools.ActionStop }

func (t *Tool) Action(json.RawMessage, string, bool) (tools.Action, error) {
	return tools.Action{}, errNoAction
}

// Run stops the chat's task by the id the call names.
func (t *Tool) Run(_ context.Context, rt tools.Runtime, raw json.RawMessage) (string, bool) {
	id, err := parse(raw)
	if err != nil {
		return `{"error":"bad-input"}`, true
	}
	if !rt.Tasks.Stop(id) {
		return notRunning, true
	}
	return "Stopped " + id + ".", false
}

var errInput = errors.New(`taskstop input is not {"task_id": <non-empty string>}`)

// parse reads an object whose one key is task_id, spelled exactly and once, a
// string that is not empty, with nothing after it.
func parse(raw json.RawMessage) (string, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	if tok, err := dec.Token(); err != nil || tok != json.Delim('{') {
		return "", errInput
	}
	var id string
	seen := false
	for dec.More() {
		key, err := dec.Token()
		if err != nil || key != "task_id" || seen {
			return "", errInput
		}
		seen = true
		val, err := dec.Token()
		if err != nil {
			return "", errInput
		}
		if id, _ = val.(string); id == "" {
			return "", errInput
		}
	}
	if tok, err := dec.Token(); err != nil || tok != json.Delim('}') {
		return "", errInput
	}
	if err := dec.Decode(new(any)); !errors.Is(err, io.EOF) {
		return "", errInput
	}
	if !seen {
		return "", errInput
	}
	return id, nil
}
