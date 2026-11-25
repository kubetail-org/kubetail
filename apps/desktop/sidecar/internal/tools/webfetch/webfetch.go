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

// Package webfetch is the tool that fetches a web page the user approves, from
// the user's own machine, and returns it as markdown. It reaches no local or
// private address, follows a redirect only on the approved host, and sends no
// credentials.
package webfetch

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/llm"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/tools"
)

//go:embed prompts/webfetch.md
var prompt string

// description is the tool description the model reads with the schema.
//
//go:embed prompts/description.md
var description string

// inputSchema is the reference's url, without its format: Chat Completions
// sends a schema as written, and a vendor that rejects a format it does not
// know fails the whole turn.
//
//go:embed prompts/schema.json
var inputSchema []byte

// Name is the name WebFetch is offered under.
const Name = "WebFetch"

// ContractName is the shape of the tool's stored calls, recorded on each row.
const ContractName tools.ContractName = "webfetch/v1"

// FetchTimeout bounds a fetch: every hop and the body read.
const FetchTimeout = 30 * time.Second

// callTimeout is the loop's bound on one call: the fetch's, then the conversion.
const callTimeout = FetchTimeout + 15*time.Second

var _ interface {
	tools.Custom
	tools.Gated
	tools.Bounded
} = (*Tool)(nil)

// badInput answers a call whose input parse refused. The loop asks Approval
// first, so a call never gets this far with one.
const badInput = `{"error":"bad-input"}`

// Tool fetches the pages the user approves.
type Tool struct {
	transport http.RoundTripper
	bound     time.Duration
	// convert is readPage; a test swaps in one that blocks.
	convert func(contentType string, body []byte, base *url.URL) (page, error)
}

// New is the tool over transport, NewTransport's, each fetch bounded by bound.
func New(transport http.RoundTripper, bound time.Duration) *Tool {
	return &Tool{transport: transport, bound: bound, convert: readPage}
}

// Definition is the offer: a function named WebFetch, run by the sidecar.
func (t *Tool) Definition() llm.ToolDefinition {
	return llm.ToolDefinition{Name: Name, Description: description, InputSchema: json.RawMessage(inputSchema)}
}

// Prompt is the tool's section of the system prompt.
func (t *Tool) Prompt() string { return prompt }

// CallTimeout is the loop's bound on one call, whatever it asks for.
func (t *Tool) CallTimeout(json.RawMessage) time.Duration { return callTimeout }

func (t *Tool) Name() string { return Name }

func (t *Tool) ContractName() tools.ContractName { return ContractName }

func (t *Tool) ActionKind() tools.ActionKind { return tools.ActionFetch }

func (t *Tool) Action(raw json.RawMessage, cwd string, _ bool) (tools.Action, error) {
	return ActionOf(raw, cwd)
}

// Approval asks about every URL but one target refuses by name, which Run
// answers without anyone being asked. WebFetch runs nowhere, so no Cwd.
func (t *Tool) Approval(_ context.Context, _ tools.Runtime, raw json.RawMessage) (tools.Approval, error) {
	in, err := parse(raw)
	if err != nil {
		return tools.Approval{}, err
	}
	if _, err := target(in); err != nil {
		return tools.Approval{Skip: true}, nil
	}
	return tools.Approval{}, nil
}

// Run fetches the page a call names.
func (t *Tool) Run(ctx context.Context, rt tools.Runtime, raw json.RawMessage) (string, bool) {
	in, err := parse(raw)
	if err != nil {
		return badInput, true
	}
	u, err := target(in)
	if err != nil {
		return err.Error(), true
	}
	return t.fetch(ctx, rt, u)
}

// ActionOf is the page a call fetches: the URL as it would be requested and the
// host it dials. It reads the URL as target does, less target's refusals by
// name, so a refusal added later leaves every stored call's action as it was.
// WebFetch runs nowhere, so cwd is never set.
func ActionOf(raw json.RawMessage, _ string) (tools.Action, error) {
	in, err := parse(raw)
	if err != nil {
		return tools.Action{}, err
	}
	u, err := rebuild(in, true)
	if err != nil {
		return tools.Action{}, err
	}
	return tools.Action{Fetch: &tools.FetchAction{URL: u.String(), Host: strings.TrimSuffix(u.Host, ":443")}}, nil
}

var errInput = errors.New(`webfetch input is not {"url": <non-empty string>}`)

// parse reads an object whose one key is url, spelled exactly and once, with
// nothing after it, as read's parse does. The value's type is checked off its
// token, since a typed decode takes null as the zero value.
func parse(raw json.RawMessage) (string, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	if tok, err := dec.Token(); err != nil || tok != json.Delim('{') {
		return "", errInput
	}
	var u string
	seen := false
	for dec.More() {
		key, err := dec.Token()
		if err != nil || key != "url" || seen {
			return "", errInput
		}
		seen = true
		val, err := dec.Token()
		if err != nil {
			return "", errInput
		}
		var ok bool
		if u, ok = val.(string); !ok {
			return "", errInput
		}
	}
	if tok, err := dec.Token(); err != nil || tok != json.Delim('}') {
		return "", errInput
	}
	if err := dec.Decode(new(any)); !errors.Is(err, io.EOF) {
		return "", errInput
	}
	if u == "" {
		return "", errInput
	}
	return u, nil
}
