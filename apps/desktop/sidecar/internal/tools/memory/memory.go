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

// Package memory is the tool that saves and forgets the model's notes, for a
// chat's cluster or, once the user approves, for every cluster, through the memory
// service. The cluster and the chat are the runtime's; the model names neither.
package memory

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"io"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/llm"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/memory"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/tools"
)

//go:embed prompts/memory.md
var prompt string

// description is the tool description the model reads with the schema.
//
//go:embed prompts/description.md
var description string

//go:embed prompts/schema.json
var inputSchema []byte

// Name is the name Memory is offered under.
const Name = "Memory"

// ContractName is the shape of the tool's stored calls, recorded on each row.
const ContractName tools.ContractName = "memory/v1"

// The ops.
const (
	opSave   = "save"
	opForget = "forget"
)

// The scopes a save names, as the section spells them.
const (
	scopeCluster    = "cluster"
	scopeEverywhere = "everywhere"
)

var (
	_ tools.Custom = (*Tool)(nil)
	_ tools.Gated  = (*Tool)(nil)
)

// Tool is the Memory tool, over the memory service. A call for every cluster waits
// on the user; a call for the cluster runs unasked, since it reaches only chats on
// the cluster whose data may have shaped it, and the transcript shows every save.
type Tool struct{ svc memory.Service }

// New is the tool over svc.
func New(svc memory.Service) *Tool { return &Tool{svc: svc} }

func (t *Tool) Name() string { return Name }

func (t *Tool) ContractName() tools.ContractName { return ContractName }

func (t *Tool) ActionKind() tools.ActionKind { return tools.ActionMemory }

func (t *Tool) Action(raw json.RawMessage, cwd string, _ bool) (tools.Action, error) {
	return ActionOf(raw, cwd)
}

// Definition is the offer: a function named Memory, run by the sidecar.
func (t *Tool) Definition() llm.ToolDefinition {
	return llm.ToolDefinition{Name: Name, Description: description, InputSchema: json.RawMessage(inputSchema)}
}

// Prompt is the tool's section of the system prompt.
func (t *Tool) Prompt() string { return prompt }

// Approval asks about a call for every cluster alone, once the store says it
// would take it, so the user is never asked to approve a call that must fail and
// a credential never reaches the request. A refused check is a *tools.Refusal
// carrying the result Run would give, never a skip: a skip would change every
// cluster's notes unasked should the store change its answer by then. A call
// that does not parse skips, and Run refuses it.
func (t *Tool) Approval(ctx context.Context, _ tools.Runtime, raw json.RawMessage) (tools.Approval, error) {
	in, err := parse(raw)
	if err != nil || in.scope != scopeEverywhere {
		return tools.Approval{Skip: true}, nil
	}
	check := t.svc.CheckForgetEverywhere(ctx, in.name)
	if in.op == opSave {
		check = t.svc.CheckSaveEverywhere(ctx, in.name, in.body)
	}
	if check != nil {
		text, _ := refusal(check)
		return tools.Approval{}, &tools.Refusal{Result: text}
	}
	return tools.Approval{}, nil
}

// Run answers one call through the memory service, on the runtime's cluster and
// for its chat, as one JSON object on one line. A refusal is a code, never a Go
// error's text: an error from the store can carry a path or a row.
func (t *Tool) Run(ctx context.Context, rt tools.Runtime, raw json.RawMessage) (string, bool) {
	in, err := parse(raw)
	if err != nil {
		return refusal(err)
	}
	switch {
	case in.op == opSave && in.scope == scopeEverywhere:
		err = t.svc.SaveEverywhere(ctx, in.name, in.body, rt.ChatID)
	case in.op == opSave:
		err = t.svc.Save(ctx, rt.ClusterID, in.name, in.body, rt.ChatID)
	case in.scope == scopeEverywhere:
		err = t.svc.ForgetEverywhere(ctx, in.name)
	default:
		err = t.svc.Forget(ctx, rt.ClusterID, in.name)
	}
	if err != nil {
		return refusal(err)
	}
	if in.op == opSave {
		return result(map[string]any{"saved": in.name})
	}
	return result(map[string]any{"forgot": in.name})
}

// codes are the refusals the model can act on, each by the code it reads.
var codes = []struct {
	err  error
	code string
}{
	{memory.ErrNotFound, "not-found"},
	{memory.ErrUserNote, "user-note"},
	{memory.ErrFull, "full"},
	{memory.ErrSecret, "secret"},
	{memory.ErrClusterGone, "cluster-gone"},
}

// refusal is err as the result the model reads: a code, and the field for a
// bad input on a field the tool knows. Anything else is unavailable: a failure the
// model can do nothing about.
func refusal(err error) (string, bool) {
	out := map[string]string{"error": "unavailable"}
	var ie *inputError
	var fe *memory.FieldError
	switch {
	case errors.As(err, &ie):
		out["error"] = "bad-input"
		if ie.field != "" {
			out["field"] = ie.field
		}
	case errors.As(err, &fe):
		out["error"] = "bad-input"
		out["field"] = fe.Field
	default:
		for _, c := range codes {
			if errors.Is(err, c.err) {
				out["error"] = c.code
				break
			}
		}
	}
	text, _ := result(out)
	return text, true
}

// result is v as one line of JSON.
func result(v any) (string, bool) {
	raw, _ := json.Marshal(v) // strings, ints and bools always marshal
	return string(raw), false
}

// ActionOf is the call as its arguments name it, through the parse Run uses. The
// tool runs nowhere, so cwd is never read.
func ActionOf(raw json.RawMessage, _ string) (tools.Action, error) {
	in, err := parse(raw)
	if err != nil {
		return tools.Action{}, err
	}
	return tools.Action{Memory: &tools.MemoryAction{
		Op: in.op, Name: in.name, Body: in.body, Scope: in.scope,
	}}, nil
}

// input is one call's arguments, scope cluster when the call names none.
type input struct {
	op, name, body, scope string
}

// inputError is arguments the tool refuses. field names the argument when it is
// one the tool knows, "" otherwise.
type inputError struct{ field string }

func (e *inputError) Error() string {
	if e.field == "" {
		return "memory input is not one flat object of the tool's own fields"
	}
	return "memory input has a bad " + e.field
}

// parse reads an object whose keys are the tool's fields, each spelled exactly and
// at most once, with nothing after it. Each value's type is checked off its token,
// since a typed decode takes null as the zero value.
func parse(raw json.RawMessage) (input, error) {
	var in input
	dec := json.NewDecoder(bytes.NewReader(raw))
	if tok, err := dec.Token(); err != nil || tok != json.Delim('{') {
		return input{}, &inputError{}
	}
	seen := map[string]bool{}
	for dec.More() {
		key, err := dec.Token()
		if err != nil {
			return input{}, &inputError{}
		}
		name, _ := key.(string)
		if seen[name] {
			return input{}, &inputError{}
		}
		seen[name] = true
		val, err := dec.Token()
		if err != nil {
			return input{}, &inputError{}
		}
		var ok bool
		switch name {
		case "op":
			in.op, ok = val.(string)
		case "name":
			in.name, ok = val.(string)
		case "body":
			in.body, ok = val.(string)
		case "scope":
			in.scope, ok = val.(string)
		default:
			return input{}, &inputError{}
		}
		if !ok {
			return input{}, &inputError{field: name}
		}
	}
	if tok, err := dec.Token(); err != nil || tok != json.Delim('}') {
		return input{}, &inputError{}
	}
	if err := dec.Decode(new(any)); !errors.Is(err, io.EOF) {
		return input{}, &inputError{}
	}

	if in.op != opSave && in.op != opForget {
		return input{}, &inputError{field: "op"}
	}
	if !seen["name"] {
		return input{}, &inputError{field: "name"}
	}
	// A save needs a body; a forget takes none.
	if seen["body"] != (in.op == opSave) {
		return input{}, &inputError{field: "body"}
	}
	if !seen["scope"] {
		in.scope = scopeCluster
	} else if in.scope != scopeCluster && in.scope != scopeEverywhere {
		return input{}, &inputError{field: "scope"}
	}
	return in, nil
}
