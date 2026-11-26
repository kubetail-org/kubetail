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

package tools

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/llm"
)

// Box is every tool the app knows, in offer order, and the readers of tools
// this machine cannot offer. Callers ask it questions rather than range over
// it, so what kind each tool is stays inside this package.
type Box struct {
	tools []Tool
	// readers is every tool, then the readers passed beside them: whatever
	// reads a stored call.
	readers []Reader
}

// NewBox is the box of tools, in offer order, reading the rows of alsoReads
// too: a stored call still shows on a machine that no longer offers its tool.
// It panics on a wiring mistake, which the app's startup would hit first.
func NewBox(tools []Tool, alsoReads ...Reader) Box {
	b := Box{tools: tools}
	for _, t := range tools {
		checkTool(t)
		b.readers = append(b.readers, t)
	}
	b.readers = append(b.readers, alsoReads...)
	names := map[string]bool{}
	contracts := map[ContractName]bool{}
	for _, r := range b.readers {
		if !r.ActionKind().valid() {
			panic(fmt.Sprintf("tools: %s does no kind of action in the set", r.Name()))
		}
		if names[r.Name()] {
			panic(fmt.Sprintf("tools: two tools are named %s", r.Name()))
		}
		if contracts[r.ContractName()] {
			panic(fmt.Sprintf("tools: two tools have the contract %s", r.ContractName()))
		}
		names[r.Name()], contracts[r.ContractName()] = true, true
	}
	return b
}

// checkTool panics unless t is run by exactly one side, defined under its own
// name when it is custom, and capped at one use or more when the provider runs
// it.
func checkTool(t Tool) {
	c, custom := t.(Custom)
	_, runner := t.(Runner)
	b, budgeted := t.(Budgeted)
	switch {
	case custom && c.Definition().Name != t.Name():
		panic(fmt.Sprintf("tools: %s is defined as %s", t.Name(), c.Definition().Name))
	case runner == budgeted:
		panic(fmt.Sprintf("tools: %s is not run by exactly one of the sidecar and the provider", t.Name()))
	case budgeted && b.MaxUses() < 1:
		panic(fmt.Sprintf("tools: %s may be called less than once a turn", t.Name()))
	}
}

// For is the box a turn on t is offered. A model that takes no tools is
// offered none. Otherwise the turn gets each tool named in names, in box order:
// an app tool as it is, or as t sees it when it is PerTarget, and a vendor tool
// when t.Takes it. A name the box holds no tool of is skipped. The turn's box
// reads no stored call.
func (b Box) For(t llm.Target, names []string) Box {
	if !t.Model.Tools {
		return Box{}
	}
	var turn Box
	for _, tool := range b.tools {
		if !slices.Contains(names, tool.Name()) {
			continue
		}
		if _, custom := tool.(Custom); !custom {
			_, runner := tool.(Runner)
			if !t.Takes(tool, !runner) {
				continue
			}
		}
		if p, ok := tool.(PerTarget); ok {
			tool = p.For(t)
		}
		turn.tools = append(turn.tools, tool)
	}
	return turn
}

// Without is the box less every tool of kinds, whatever its name. The readers
// stay: a stored call still reads the same.
func (b Box) Without(kinds ...ActionKind) Box {
	out := Box{readers: b.readers}
	for _, t := range b.tools {
		if !slices.Contains(kinds, t.ActionKind()) {
			out.tools = append(out.tools, t)
		}
	}
	return out
}

// WithoutNames is the box less every tool named in names, so a tool the user turned
// off loses its offer, its prompt section and its runner: a call the model
// makes to it is an unknown tool. The readers stay, so a stored call still
// reads the same.
func (b Box) WithoutNames(names []string) Box {
	out := Box{readers: b.readers}
	for _, t := range b.tools {
		if !slices.Contains(names, t.Name()) {
			out.tools = append(out.tools, t)
		}
	}
	return out
}

// Has reports whether the box holds a tool named name; readers do not count.
func (b Box) Has(name string) bool {
	return slices.ContainsFunc(b.tools, func(t Tool) bool { return t.Name() == name })
}

// Empty reports whether the box holds no tools; readers do not count.
func (b Box) Empty() bool { return len(b.tools) == 0 }

// Offer is the definitions of the app's tools and an offer of each vendor tool,
// in box order: the provider's at its cap, the sidecar's at none.
func (b Box) Offer() ([]llm.ToolDefinition, []llm.NativeOffer) {
	var defs []llm.ToolDefinition
	var native []llm.NativeOffer
	for _, t := range b.tools {
		switch t := t.(type) {
		case Custom:
			defs = append(defs, t.Definition())
		case Budgeted:
			native = append(native, llm.NativeOffer{Tool: t, Server: true, MaxUses: t.MaxUses()})
		default:
			native = append(native, llm.NativeOffer{Tool: t})
		}
	}
	return defs, native
}

// Runner is the tool that answers a call named name. The provider's tools are
// never one, so a call naming one is an unknown tool.
func (b Box) Runner(name string) (Runner, bool) {
	for _, t := range b.tools {
		if r, ok := t.(Runner); ok && t.Name() == name {
			return r, true
		}
	}
	return nil, false
}

// HasRunner reports whether the box holds a tool the sidecar runs; readers do not
// count.
func (b Box) HasRunner() bool {
	for _, t := range b.tools {
		if _, ok := t.(Runner); ok {
			return true
		}
	}
	return false
}

// Budgeted is the tools the provider runs, in box order.
func (b Box) Budgeted() []Budgeted {
	var out []Budgeted
	for _, t := range b.tools {
		if bt, ok := t.(Budgeted); ok {
			out = append(out, bt)
		}
	}
	return out
}

// Prompts is every tool's section, in box order.
func (b Box) Prompts() []string {
	out := make([]string, 0, len(b.tools))
	for _, t := range b.tools {
		out = append(out, t.Prompt())
	}
	return out
}

// Why a stored payload is refused. Each is bare: a decoder's error can quote the
// stored text, so a caller logs the sentinel alone.
var (
	ErrNoReader = errors.New("no tool reads the row")
	ErrContract = errors.New("the row is not under its tool's contract")
	ErrDecode   = errors.New("the stored json does not decode")
	ErrKind     = errors.New("the stored payload is not of its tool's kind")
)

// ContractOf is the contract a new row of name records: its reader's, or none
// when no reader has the name.
func (b Box) ContractOf(name string) ContractName {
	r, ok := b.reader(name)
	if !ok {
		return ""
	}
	return r.ContractName()
}

// Action is what a stored call does, read by the tool or reader its row names,
// whether or not this turn offers it. False when the box does not read the row
// (readerOf), when the tool refuses the arguments, or when the action is not of
// the tool's kind, so a call never shows as something its tool does not do.
func (b Box) Action(name string, contract ContractName, input json.RawMessage, cwd string, sandboxed bool) (Action, bool) {
	r, err := b.readerOf(name, contract)
	if err != nil {
		return Action{}, false
	}
	a, err := r.Action(input, cwd, sandboxed)
	if err != nil || a.Kind() != r.ActionKind() {
		return Action{}, false
	}
	return a, true
}

// Shown is the action a Shown tool's run stored for a row, when the box reads
// the row, the JSON decodes, and the action is of the tool's kind.
func (b Box) Shown(name string, contract ContractName, stored json.RawMessage) (Action, error) {
	r, err := b.readerOf(name, contract)
	if err != nil {
		return Action{}, err
	}
	var a Action
	if err := json.Unmarshal(stored, &a); err != nil {
		return Action{}, ErrDecode
	}
	if a.Kind() != r.ActionKind() {
		return Action{}, ErrKind
	}
	return a, nil
}

// WatchResult is the result a LogsWatch call's task stored, when the box reads
// the call's row and the JSON decodes.
func (b Box) WatchResult(name string, contract ContractName, stored json.RawMessage) (WatchResult, error) {
	r, err := b.readerOf(name, contract)
	if err != nil {
		return WatchResult{}, err
	}
	if r.ActionKind() != ActionLogsWatch {
		return WatchResult{}, ErrKind
	}
	var w WatchResult
	if err := json.Unmarshal(stored, &w); err != nil {
		return WatchResult{}, ErrDecode
	}
	return w, nil
}

// ActionKind is what a stored call does, by the kind of the tool or reader its
// row names, whatever its arguments hold. False when none reads it.
func (b Box) ActionKind(name string) (ActionKind, bool) {
	r, ok := b.reader(name)
	if !ok {
		return "", false
	}
	return r.ActionKind(), true
}

// readerOf is the reader of a row of name stored under contract: ErrNoReader
// when none has the name, ErrContract when the contract is not the reader's
// own, so a row with no contract, or one a newer build wrote, is never read.
func (b Box) readerOf(name string, contract ContractName) (Reader, error) {
	r, ok := b.reader(name)
	switch {
	case !ok:
		return nil, ErrNoReader
	case r.ContractName() != contract:
		return nil, ErrContract
	}
	return r, nil
}

// reader is the reader of the row named name. Every row is written under its
// tool's name, which NewBox holds unique.
func (b Box) reader(name string) (Reader, bool) {
	for _, r := range b.readers {
		if r.Name() == name {
			return r, true
		}
	}
	return nil, false
}
