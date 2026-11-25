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

// Package anthropicwebsearch is the Messages API's web search: a tool the
// provider runs, capped per turn.
package anthropicwebsearch

import (
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/tools"
)

// prompt is the tool's section, a format string whose one verb is the month.
//
//go:embed prompts/prompt.md
var prompt string

// Name is the search's name in the box, and what its rows are stored under.
const Name = "anthropic_web_search_20260318"

// ContractName is the tool's type on the Messages API, the one the offer sends.
const ContractName tools.ContractName = "web_search_20260318"

// maxUses is the searches one turn may make, spread over its requests.
const maxUses = 5

// searchResultTokens is what one search's results add to the request.
const searchResultTokens = 2_000

// Tool is the search.
type Tool struct {
	now func() time.Time
}

// New is the search, telling the model the month now reads at each prompt: the
// sidecar runs long enough for the month to change.
func New(now func() time.Time) *Tool { return &Tool{now: now} }

func (t *Tool) Name() string { return Name }

func (t *Tool) ContractName() tools.ContractName { return ContractName }

func (t *Tool) ActionKind() tools.ActionKind { return tools.ActionSearch }

func (t *Tool) Action(raw json.RawMessage, cwd string, _ bool) (tools.Action, error) {
	return ActionOf(raw, cwd)
}

func (t *Tool) MaxUses() int { return maxUses }

func (t *Tool) Allowance() int { return maxUses * searchResultTokens }

func (t *Tool) Prompt() string {
	return strings.TrimSpace(fmt.Sprintf(prompt, t.now().Format("January 2006")))
}

// errInput is arguments that are not a search's.
var errInput = errors.New("anthropicwebsearch: not a search's arguments")

// ActionOf is a search's action: its query, off arguments the provider wrote.
// Any other key is the provider's and is ignored, since nothing is approved
// against these arguments; a query that is not a string is refused.
func ActionOf(raw json.RawMessage, _ string) (tools.Action, error) {
	var in map[string]json.RawMessage
	if json.Unmarshal(raw, &in) != nil || in == nil {
		return tools.Action{}, errInput
	}
	q, ok := in["query"]
	if !ok {
		return tools.Action{Search: &tools.SearchAction{}}, nil
	}
	// A pointer, since null decodes into a string as "" with no error.
	var query *string
	if json.Unmarshal(q, &query) != nil || query == nil {
		return tools.Action{}, errInput
	}
	return tools.Action{Search: &tools.SearchAction{Query: *query}}, nil
}
