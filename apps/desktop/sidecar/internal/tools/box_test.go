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
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/llm"
)

// actionOf is an action of kind carrying input, or an error for a kind with no
// action to show.
func actionOf(kind ActionKind, input json.RawMessage) (Action, error) {
	text := string(input)
	switch kind {
	case ActionCommand:
		return Action{Command: &CommandAction{Text: text}}, nil
	case ActionRead:
		return Action{Read: &ReadAction{Path: text}}, nil
	case ActionWrite:
		return Action{Write: &WriteAction{Path: text}}, nil
	case ActionEdit:
		return Action{Edit: &EditAction{Path: text}}, nil
	case ActionSearch:
		return Action{Search: &SearchAction{Query: text}}, nil
	}
	return Action{}, errors.New("no action")
}

// reader reads its rows as an action of its kind.
type reader struct {
	name string
	kind ActionKind
}

func (r reader) Name() string           { return r.name }
func (r reader) ActionKind() ActionKind { return r.kind }
func (r reader) ContractName() ContractName {
	return ContractName(strings.ToLower(r.name) + "/v1")
}
func (r reader) Action(input json.RawMessage, _ string, _ bool) (Action, error) {
	return actionOf(r.kind, input)
}

// recontracted is a reader under a contract of its own.
type recontracted struct {
	reader
	contract ContractName
}

func (r recontracted) ContractName() ContractName { return r.contract }

// custom is a tool of the app's.
type custom struct{ reader }

func (c custom) Prompt() string                 { return "## " + c.name }
func (c custom) Definition() llm.ToolDefinition { return llm.ToolDefinition{Name: c.name} }
func (c custom) Run(context.Context, Runtime, json.RawMessage) (string, bool) {
	return c.name, false
}

// server is a vendor's tool the provider runs.
type server struct {
	reader
	maxUses int
}

func (s server) Prompt() string             { return "## " + s.name }
func (s server) ContractName() ContractName { return "acme_" + ContractName(s.name) }
func (s server) MaxUses() int               { return s.maxUses }
func (s server) Allowance() int             { return s.maxUses * 1_000 }

// client is a vendor's tool the sidecar runs.
type client struct{ reader }

func (c client) Prompt() string             { return "## " + c.name }
func (c client) ContractName() ContractName { return "acme_" + ContractName(c.name) }
func (c client) Run(context.Context, Runtime, json.RawMessage) (string, bool) {
	return c.name, false
}

// Tools that are neither app nor vendor tools, or both.
type (
	bare         struct{ reader }
	customTwin   struct{ custom }
	serverRunner struct{ server }
	unrun        struct{ reader }
)

func (b bare) Prompt() string     { return "" }
func (customTwin) MaxUses() int   { return 1 }
func (customTwin) Allowance() int { return 1_000 }
func (u unrun) Prompt() string    { return "" }
func (serverRunner) Run(context.Context, Runtime, json.RawMessage) (string, bool) {
	return "", false
}

var (
	echo   = custom{reader{"echo", ActionCommand}}
	search = server{reader{"anthropic_web_search_20260318", ActionSearch}, 5}
	shell  = client{reader{"anthropic_bash_20250124", ActionCommand}}
)

// NewBox refuses each wiring mistake at startup.
func TestNewBoxRefusesAMiswiredTool(t *testing.T) {
	cases := map[string]func(){
		"neither an app nor a vendor tool": func() { NewBox([]Tool{bare{reader{"x", ActionRead}}}) },
		"both an app and a vendor tool": func() {
			NewBox([]Tool{customTwin{custom{reader{"x", ActionRead}}}})
		},
		"a vendor tool run by both sides": func() { NewBox([]Tool{serverRunner{search}}) },
		"a vendor tool run by neither":    func() { NewBox([]Tool{unrun{reader{"x", ActionRead}}}) },
		"a budget under one": func() {
			NewBox([]Tool{server{reader{"s", ActionSearch}, 0}})
		},
		"a kind outside the set": func() { NewBox([]Tool{custom{reader{"x", "unknown"}}}) },
		"a reader of no kind":    func() { NewBox(nil, reader{"x", ""}) },
		"a definition of another name": func() {
			NewBox([]Tool{renamed{echo}})
		},
		"two tools of one name":      func() { NewBox([]Tool{echo, echo}) },
		"a reader named like a tool": func() { NewBox([]Tool{echo}, reader{"echo", ActionCommand}) },
		"two tools of one contract": func() {
			NewBox([]Tool{echo}, recontracted{reader{"other", ActionCommand}, "echo/v1"})
		},
	}
	for name, build := range cases {
		assert.Panics(t, build, name)
	}
}

// Several tools may do one kind of thing: a turn's list picks one.
func TestNewBoxTakesAlternatives(t *testing.T) {
	other := client{reader{"sandboxed_bash", ActionCommand}}

	assert.NotPanics(t, func() { NewBox([]Tool{echo, shell, other, search}) })
}

// renamed is a tool whose definition names another tool.
type renamed struct{ custom }

func (renamed) Definition() llm.ToolDefinition { return llm.ToolDefinition{Name: "other"} }

// The offer is each app tool's definition and an offer of each vendor tool, in
// box order: the provider's at its cap, the sidecar's at none.
func TestTheOfferIsInBoxOrder(t *testing.T) {
	count := custom{reader{"count", ActionRead}}
	box := NewBox([]Tool{echo, search, count, shell})

	defs, native := box.Offer()

	assert.Equal(t, []llm.ToolDefinition{{Name: "echo"}, {Name: "count"}}, defs)
	assert.Equal(t, []llm.NativeOffer{{Tool: search, Server: true, MaxUses: 5}, {Tool: shell}}, native)
}

// A call is answered by the tool of its name the sidecar runs; the provider's
// tools answer none.
func TestTheRunnerOfACallIsTheSidecarsTool(t *testing.T) {
	box := NewBox([]Tool{echo, search, shell})

	r, ok := box.Runner("echo")
	assert.True(t, ok)
	text, _ := r.Run(t.Context(), Runtime{}, nil)
	assert.Equal(t, "echo", text)
	_, ok = box.Runner(shell.name)
	assert.True(t, ok)
	_, ok = box.Runner(search.name)
	assert.False(t, ok, "the provider runs the search")
	_, ok = box.Runner("missing")
	assert.False(t, ok)
}

// A box has a runner when it holds a tool the sidecar runs, an app tool or a
// vendor's; the provider's tools and a box of readers alone have none.
func TestHasRunnerCountsWhatTheSidecarRuns(t *testing.T) {
	assert.True(t, NewBox([]Tool{echo}).HasRunner())
	assert.True(t, NewBox([]Tool{search, shell}).HasRunner())
	assert.False(t, NewBox([]Tool{search}).HasRunner())
	assert.False(t, NewBox(nil, reader{"bash", ActionCommand}).HasRunner())
	assert.False(t, Box{}.HasRunner())
}

// gated is a tool that asks for the user's decision on every call.
type gated struct{ custom }

func (gated) Approval(_ context.Context, _ Runtime, input json.RawMessage) (Approval, error) {
	return Approval{Cwd: string(input)}, nil
}

// The loop finds a gated tool by asserting what the runner is; the box never
// learns the difference.
func TestAGatedToolIsFoundByAssertion(t *testing.T) {
	box := NewBox([]Tool{echo, gated{custom{reader{"bash", ActionCommand}}}})

	r, _ := box.Runner("echo")
	_, isGated := r.(Gated)
	assert.False(t, isGated)

	r, _ = box.Runner("bash")
	g, isGated := r.(Gated)
	assert.True(t, isGated)
	approval, err := g.Approval(t.Context(), Runtime{}, json.RawMessage(`ls`))
	assert.NoError(t, err)
	assert.Equal(t, Approval{Cwd: "ls"}, approval)
}

// The provider's tools are the budgeted ones; every tool has a section, in box
// order; a box of readers alone is empty.
func TestTheBoxAnswersForItsTools(t *testing.T) {
	box := NewBox([]Tool{echo, search, shell})

	assert.Equal(t, []Budgeted{search}, box.Budgeted())
	assert.Equal(t, []string{"## echo", "## " + search.name, "## " + shell.name}, box.Prompts())
	assert.False(t, box.Empty())
	assert.True(t, NewBox(nil, reader{"bash", ActionCommand}).Empty())
	assert.True(t, Box{}.Empty())
}

// A row is read by the tool of its name, a tool or a reader alike, the
// provider's included.
func TestAStoredCallIsReadByItsTool(t *testing.T) {
	box := NewBox([]Tool{echo, search}, reader{"bash", ActionCommand})

	got, ok := box.Action("echo", "echo/v1", json.RawMessage(`hi`), "", false)
	assert.True(t, ok)
	assert.Equal(t, Action{Command: &CommandAction{Text: "hi"}}, got)
	got, ok = box.Action("bash", "bash/v1", json.RawMessage(`ls`), "", false)
	assert.True(t, ok)
	assert.Equal(t, Action{Command: &CommandAction{Text: "ls"}}, got)
	got, ok = box.Action(search.name, "acme_"+ContractName(search.name), json.RawMessage(`q`), "", false)
	assert.True(t, ok)
	assert.Equal(t, Action{Search: &SearchAction{Query: "q"}}, got)
	_, ok = box.Action("missing", "missing/v1", json.RawMessage(`x`), "", false)
	assert.False(t, ok)
}

// A row's kind is its reader's, found by name, a tool or a reader alike. None
// for a name no reader has.
func TestAStoredCallsKindIsItsReaders(t *testing.T) {
	stop := custom{reader{"TaskStop", ActionStop}}
	box := NewBox([]Tool{echo, stop, search}, reader{"bash", ActionCommand})

	for name, want := range map[string]ActionKind{"echo": ActionCommand, "TaskStop": ActionStop, "bash": ActionCommand, search.name: ActionSearch} {
		got, ok := box.ActionKind(name)
		assert.True(t, ok, name)
		assert.Equal(t, want, got, name)
	}
	_, ok := box.ActionKind("missing")
	assert.False(t, ok)
}

// liar reads its calls as an action of another kind than its own.
type liar struct{ custom }

func (liar) Action(input json.RawMessage, _ string, _ bool) (Action, error) {
	return actionOf(ActionWrite, input)
}

// rowReader keeps the row's cwd and flag it was handed.
type rowReader struct {
	custom
	cwd       *string
	sandboxed *bool
}

func (r rowReader) Action(input json.RawMessage, cwd string, sandboxed bool) (Action, error) {
	*r.cwd, *r.sandboxed = cwd, sandboxed
	return actionOf(r.kind, input)
}

// A name's contract is its reader's, a tool or a reader alike; none for a name
// no reader has.
func TestAToolsContractIsItsReaders(t *testing.T) {
	box := NewBox([]Tool{echo, search}, reader{"bash", ActionCommand})

	for name, want := range map[string]ContractName{"echo": "echo/v1", "bash": "bash/v1", search.name: "acme_" + ContractName(search.name)} {
		assert.Equal(t, want, box.ContractOf(name), name)
	}
	assert.Empty(t, box.ContractOf("missing"))
}

// A row is read only under its reader's contract: not under another tool's, and
// not with none.
func TestAStoredCallIsReadUnderItsContractAlone(t *testing.T) {
	box := NewBox([]Tool{echo}, reader{"bash", ActionCommand})

	for _, c := range []ContractName{"bash/v1", "echo/v2", ""} {
		_, ok := box.Action("echo", c, json.RawMessage(`hi`), "", false)
		assert.False(t, ok, c)
	}
}

// A shown action is answered only for a row the box reads, whose JSON decodes
// to an action of its tool's kind; each refusal is a bare sentinel, never the
// decoder's error, which can quote the row.
func TestAShownActionIsReadUnderItsContract(t *testing.T) {
	box := NewBox([]Tool{echo}, reader{"read", ActionRead})

	got, err := box.Shown("echo", "echo/v1", json.RawMessage(`{"description":"d","command":{"text":"ls"}}`))
	require.NoError(t, err)
	assert.Equal(t, Action{Description: "d", Command: &CommandAction{Text: "ls"}}, got)

	for name, c := range map[string]struct {
		name     string
		contract ContractName
		stored   string
		want     error
	}{
		"no reader":        {"missing", "missing/v1", `{"command":{"text":"ls"}}`, ErrNoReader},
		"another contract": {"echo", "echo/v2", `{"command":{"text":"ls"}}`, ErrContract},
		"no contract":      {"echo", "", `{"command":{"text":"ls"}}`, ErrContract},
		"not json":         {"echo", "echo/v1", `{"command":`, ErrDecode},
		"a field retyped":  {"echo", "echo/v1", `{"command":{"text":7}}`, ErrDecode},
		"another kind":     {"read", "read/v1", `{"command":{"text":"ls"}}`, ErrKind},
	} {
		_, err := box.Shown(c.name, c.contract, json.RawMessage(c.stored))
		assert.Equal(t, c.want, err, name)
	}
}

// A watch's result is answered only for a LogsWatch row the box reads, whose
// JSON decodes, each refusal a bare sentinel.
func TestAWatchResultIsReadUnderItsContract(t *testing.T) {
	box := NewBox(nil, reader{"watch", ActionLogsWatch}, reader{"read", ActionRead})

	got, err := box.WatchResult("watch", "watch/v1", json.RawMessage(`{"text":"t"}`))
	require.NoError(t, err)
	assert.Equal(t, WatchResult{Text: "t"}, got)

	for name, c := range map[string]struct {
		name     string
		contract ContractName
		stored   string
		want     error
	}{
		"no reader":        {"missing", "missing/v1", `{}`, ErrNoReader},
		"another contract": {"watch", "watch/v2", `{}`, ErrContract},
		"a field retyped":  {"watch", "watch/v1", `{"text":1}`, ErrDecode},
		"another kind":     {"read", "read/v1", `{}`, ErrKind},
	} {
		_, err := box.WatchResult(c.name, c.contract, json.RawMessage(c.stored))
		assert.Equal(t, c.want, err, name)
	}
}

// The reader is handed the row's cwd and whether a sandbox confined the call.
func TestAStoredCallIsReadWithItsRow(t *testing.T) {
	var cwd string
	var sandboxed bool
	box := NewBox([]Tool{rowReader{custom{reader{"echo", ActionCommand}}, &cwd, &sandboxed}})

	_, ok := box.Action("echo", "echo/v1", json.RawMessage(`x`), "/work", true)
	require.True(t, ok)
	assert.Equal(t, "/work", cwd)
	assert.True(t, sandboxed)
}

// A call shows only an action of its tool's kind, and nothing when its tool
// refuses the arguments.
func TestAStoredCallShowsOnlyItsToolsKind(t *testing.T) {
	stop := custom{reader{"TaskStop", ActionStop}}
	box := NewBox([]Tool{liar{custom{reader{"echo", ActionCommand}}}, stop})

	_, ok := box.Action("echo", "echo/v1", json.RawMessage(`x`), "", false)
	assert.False(t, ok)
	_, ok = box.Action("TaskStop", "taskstop/v1", json.RawMessage(`{}`), "", false)
	assert.False(t, ok)
}

// target is a model on a wire of dialect d that takes tools.
func target(d llm.Dialect) llm.Target {
	return llm.Target{Provider: llm.Provider{Dialect: d}, Model: llm.Model{ID: "m", Tools: true}}
}

// A turn is offered each tool its list names that the box holds, in box order:
// a custom tool as it is, a vendor tool when its target takes it. Its box reads
// no stored call.
func TestATurnIsOfferedItsList(t *testing.T) {
	stop := custom{reader{"TaskStop", ActionStop}}
	box := NewBox([]Tool{echo, stop, search, shell}, reader{"bash", ActionCommand})

	turn := box.For(target(llm.DialectFake), []string{search.name, "echo", "bash", "missing"})
	defs, native := turn.Offer()
	assert.Equal(t, []llm.ToolDefinition{{Name: "echo"}}, defs, "stop is not listed, and bash is a reader alone")
	assert.Equal(t, []llm.NativeOffer{{Tool: search, Server: true, MaxUses: 5}}, native, "in box order, whatever the list's")
	assert.Equal(t, []string{"## echo", "## " + search.name}, turn.Prompts())
	_, ok := turn.Action("bash", "bash/v1", json.RawMessage(`ls`), "", false)
	assert.False(t, ok)

	turn = box.For(target(llm.DialectMessages), []string{"echo", search.name})
	defs, native = turn.Offer()
	assert.Equal(t, []llm.ToolDefinition{{Name: "echo"}}, defs)
	assert.Empty(t, native, "the fixture has no Messages binding")

	noTools := target(llm.DialectFake)
	noTools.Model.Tools = false
	assert.True(t, box.For(noTools, []string{"echo", search.name}).Empty())
	assert.True(t, box.For(target(llm.DialectFake), nil).Empty(), "no list, no tools")
}

// Without leaves out every tool of the kinds it names, and keeps the rest in
// box order.
func TestWithoutDropsEveryToolOfTheKind(t *testing.T) {
	other := custom{reader{"echo2", ActionCommand}}
	read := custom{reader{"read", ActionRead}}
	box := NewBox([]Tool{echo, read, other, search})

	defs, native := box.Without(ActionCommand).Offer()

	assert.Equal(t, []llm.ToolDefinition{{Name: "read"}}, defs)
	assert.Equal(t, []llm.NativeOffer{{Tool: search, Server: true, MaxUses: 5}}, native)
	assert.True(t, box.Without(ActionCommand, ActionRead, ActionSearch).Empty())
}

// A tool left out by name is neither offered, prompted nor run, and its
// stored calls still read.
func TestWithoutNamesDropsTheNamedTools(t *testing.T) {
	read := custom{reader{"read", ActionRead}}
	box := NewBox([]Tool{echo, read, search})

	turn := box.WithoutNames([]string{"echo", search.Name()})

	defs, native := turn.Offer()
	assert.Equal(t, []llm.ToolDefinition{{Name: "read"}}, defs)
	assert.Empty(t, native)
	assert.Len(t, turn.Prompts(), 1)
	_, ok := turn.Runner("echo")
	assert.False(t, ok, "a call to a tool turned off is an unknown tool")
	assert.False(t, turn.Has("echo"))
	assert.True(t, turn.Has("read"))
	kind, ok := turn.ActionKind("echo")
	assert.True(t, ok)
	assert.Equal(t, ActionCommand, kind, "its stored calls still read")
}

// perTarget is a tool whose offer names the model of the turn it is offered to.
type perTarget struct {
	custom
	model string
}

func (p perTarget) Definition() llm.ToolDefinition {
	return llm.ToolDefinition{Name: p.name, Description: p.model}
}

func (p perTarget) For(t llm.Target) Custom { return perTarget{p.custom, t.Model.ID} }

// A per-target tool is offered as the turn's target sees it, and its calls run
// on that copy.
func TestForOffersAPerTargetToolForItsTarget(t *testing.T) {
	tool := perTarget{custom{reader{"agent", ActionCommand}}, ""}
	box := NewBox([]Tool{echo, tool})

	turn := box.For(target(llm.DialectFake), []string{"echo", "agent"})

	defs, _ := turn.Offer()
	assert.Equal(t, []llm.ToolDefinition{{Name: "echo"}, {Name: "agent", Description: "m"}}, defs)
	r, ok := turn.Runner("agent")
	assert.True(t, ok)
	assert.Equal(t, perTarget{tool.custom, "m"}, r)
}
