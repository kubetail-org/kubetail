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

// Package agent is the Agent tool: the model hands a task to a subagent, which
// runs in the background while the call answers at once with its id. Its report
// reaches the model later, as a notice. Not gated: the user's gesture is the send,
// and each call the subagent makes is gated on its own.
package agent

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"errors"
	"io"
	"strings"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/llm"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/tools"
)

//go:embed prompts/agent.md
var prompt string

// description is the tool description the model reads with the schema. It
// lists every type in types.
//
//go:embed prompts/description.md
var description string

//go:embed prompts/schema.json
var inputSchema []byte

// Name is the name the tool is offered under.
const Name = "Agent"

// ContractName is the shape of the tool's stored calls, recorded on each row.
const ContractName tools.ContractName = "agent/v1"

// GeneralPurpose is the one agent type.
const GeneralPurpose = "general-purpose"

// types is every agent type a call may name.
var types = []string{GeneralPurpose}

var _ tools.PerTarget = (*Tool)(nil)

// Tool is Agent. schema is its input schema as a turn is offered it: the
// embedded one on the tool the box holds, which is never offered as it is.
type Tool struct {
	schema json.RawMessage
}

// New is the tool.
func New() *Tool { return &Tool{schema: inputSchema} }

func (t *Tool) Definition() llm.ToolDefinition {
	return llm.ToolDefinition{Name: Name, Description: description, InputSchema: t.schema}
}

// For is the tool with model an enum of every model of t's provider that takes
// tools, in catalog order. The turn's own model is one, so the enum is never
// empty.
func (t *Tool) For(target llm.Target) tools.Custom {
	var models []string
	for _, m := range target.Provider.Catalog {
		if m.Tools {
			models = append(models, m.ID)
		}
	}
	return &Tool{schema: schemaFor(models)}
}

// schemaFor is the input schema with model's enum set to models.
func schemaFor(models []string) json.RawMessage {
	var schema map[string]any
	if err := json.Unmarshal(inputSchema, &schema); err != nil { // coverage:ignore the schema is embedded, and the tests pin it
		panic(err)
	}
	schema["properties"].(map[string]any)["model"].(map[string]any)["enum"] = models
	b, err := json.Marshal(schema)
	if err != nil { // coverage:ignore a map decoded from JSON always marshals back
		panic(err)
	}
	return b
}

func (t *Tool) Prompt() string { return prompt }

func (t *Tool) Name() string { return Name }

func (t *Tool) ContractName() tools.ContractName { return ContractName }

func (t *Tool) ActionKind() tools.ActionKind { return tools.ActionDelegate }

// Action is the call's description and the task it hands on.
func (t *Tool) Action(raw json.RawMessage, _ string, _ bool) (tools.Action, error) {
	in, err := parse(raw)
	if err != nil {
		return tools.Action{}, err
	}
	return tools.Action{
		Description: in.description,
		Delegate:    &tools.DelegateAction{Prompt: in.prompt, AgentType: in.agentType, Model: in.model},
	}, nil
}

// input is a call's arguments, the type defaulted.
type input struct {
	description, prompt, agentType, model string
}

// errInput is an input no one field is at fault for: one that will not parse,
// or that carries an unknown key or a key twice.
var errInput = errors.New("agent input is not one object of the schema's keys")

// fieldError is an input whose one field is at fault.
type fieldError string

func (f fieldError) Error() string { return "agent input: bad " + string(f) }

// refusal is the result a refused input answers: the field at fault when there
// is one, never its value.
func refusal(err error) string {
	if f, ok := errors.AsType[fieldError](err); ok {
		return `{"error":"bad-input","field":"` + string(f) + `"}`
	}
	return `{"error":"bad-input"}`
}

// parse reads one object of the schema's keys, each once, with nothing after
// it: description and prompt non-blank strings, subagent_type and model strings
// when present.
func parse(raw json.RawMessage) (input, error) {
	in := input{agentType: GeneralPurpose}
	dec := json.NewDecoder(bytes.NewReader(raw))
	if tok, err := dec.Token(); err != nil || tok != json.Delim('{') {
		return input{}, errInput
	}
	seen := map[string]bool{}
	for dec.More() {
		key, err := dec.Token()
		if err != nil {
			return input{}, errInput
		}
		name, _ := key.(string)
		if seen[name] {
			return input{}, errInput
		}
		seen[name] = true
		var field *string
		switch name {
		case "description":
			field = &in.description
		case "prompt":
			field = &in.prompt
		case "subagent_type":
			field = &in.agentType
		case "model":
			field = &in.model
		default:
			return input{}, errInput
		}
		var val any
		if err := dec.Decode(&val); err != nil {
			return input{}, errInput
		}
		s, ok := val.(string)
		if !ok {
			return input{}, fieldError(name)
		}
		*field = s
	}
	if tok, err := dec.Token(); err != nil || tok != json.Delim('}') {
		return input{}, errInput
	}
	if err := dec.Decode(new(any)); !errors.Is(err, io.EOF) {
		return input{}, errInput
	}
	switch {
	case strings.TrimSpace(in.description) == "":
		return input{}, fieldError("description")
	case strings.TrimSpace(in.prompt) == "":
		return input{}, fieldError("prompt")
	}
	return in, nil
}
