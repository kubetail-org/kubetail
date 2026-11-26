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
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/llm"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/tools"
)

// The description lists every type a call may name, the way the reference's
// system reminder does.
func TestTheDescriptionListsEveryType(t *testing.T) {
	def := New().Definition()

	assert.Equal(t, Name, def.Name)
	for _, typ := range types {
		assert.Contains(t, def.Description, "The one agent type, "+typ+",")
	}
}

// The input is one JSON object and nothing after it.
func TestTheAgentInputIsOneValue(t *testing.T) {
	for _, raw := range []string{
		`{"description":"d","prompt":"p"} {}`,
		`{"description":"d","prompt":"p"}x`,
		`[]`,
		``,
		`{"description":"d","prompt":"p"`,
		`{"description":}`,
		`{"description":"d",}`,
	} {
		_, err := parse(json.RawMessage(raw))
		assert.Equal(t, `{"error":"bad-input"}`, refusal(err), raw)
	}
}

// A refusal names the field at fault, never its value, and names none when no
// one field is: an unknown key, or a key given twice.
func TestARefusalNamesTheFieldAtFault(t *testing.T) {
	cases := map[string]string{
		`{"prompt":"p"}`:                                     `{"error":"bad-input","field":"description"}`,
		`{"description":" ","prompt":"p"}`:                   `{"error":"bad-input","field":"description"}`,
		`{"description":"d"}`:                                `{"error":"bad-input","field":"prompt"}`,
		`{"description":"d","prompt":"\n"}`:                  `{"error":"bad-input","field":"prompt"}`,
		`{"description":"d","prompt":{"a":1}}`:               `{"error":"bad-input","field":"prompt"}`,
		`{"description":"d","prompt":"p","subagent_type":1}`: `{"error":"bad-input","field":"subagent_type"}`,
		`{"description":"d","prompt":"p","model":null}`:      `{"error":"bad-input","field":"model"}`,
		`{"description":"d","prompt":"p","isolation":"x"}`:   `{"error":"bad-input"}`,
		`{"description":"d","prompt":"p","prompt":"q"}`:      `{"error":"bad-input"}`,
	}
	for raw, want := range cases {
		_, err := parse(json.RawMessage(raw))
		assert.Equal(t, want, refusal(err), raw)
	}
}

// A field's error names the field alone.
func TestAFieldErrorNamesTheField(t *testing.T) {
	_, err := parse(json.RawMessage(`{"description":"d"}`))
	assert.EqualError(t, err, "agent input: bad prompt")
}

// A call's action is its description and the task it hands on, a general-purpose
// agent on the parent's model unless the call names others.
func TestAnActionReadsTheCall(t *testing.T) {
	a, err := New().Action(json.RawMessage(`{"description":"count pods","prompt":"Count the pods."}`), "", false)
	require.NoError(t, err)
	assert.Equal(t, tools.Action{
		Description: "count pods",
		Delegate:    &tools.DelegateAction{Prompt: "Count the pods.", AgentType: GeneralPurpose},
	}, a)

	a, err = New().Action(json.RawMessage(`{"description":"d","prompt":"p","subagent_type":"general-purpose","model":"big"}`), "", false)
	require.NoError(t, err)
	assert.Equal(t, &tools.DelegateAction{Prompt: "p", AgentType: GeneralPurpose, Model: "big"}, a.Delegate)

	_, err = New().Action(json.RawMessage(`{"prompt":"p"}`), "", false)
	assert.Error(t, err)
}

// A turn is offered model as an enum of its provider's models that take tools,
// in catalog order; the rest of the schema is unchanged.
func TestTheModelEnumIsTheProvidersToolModels(t *testing.T) {
	target := llm.Target{
		Provider: llm.Provider{Catalog: []llm.Model{{ID: "big", Tools: true}, {ID: "plain"}, {ID: "small", Tools: true}}},
		Model:    llm.Model{ID: "small", Tools: true},
	}

	def := New().For(target).Definition()

	var schema struct {
		Properties map[string]struct {
			Enum []string `json:"enum"`
		} `json:"properties"`
		Required []string `json:"required"`
	}
	require.NoError(t, json.Unmarshal(def.InputSchema, &schema))
	assert.Equal(t, []string{"big", "small"}, schema.Properties["model"].Enum)
	assert.Nil(t, schema.Properties["prompt"].Enum)
	assert.Equal(t, []string{"description", "prompt"}, schema.Required)
	assert.Equal(t, New().Definition().Description, def.Description)
}

// The call only starts the agent, so the turn's default timeout bounds it. The
// tool a turn runs is the copy Box.For made, so the bound is read off that.
func TestAnAgentCallHasNoDeadline(t *testing.T) {
	target := llm.Target{Provider: llm.Provider{Dialect: llm.DialectFake}, Model: llm.Model{ID: "m", Tools: true}}
	turn := tools.NewBox([]tools.Tool{New()}).For(target, []string{Name})

	r, ok := turn.Runner(Name)
	require.True(t, ok)
	_, bounded := r.(tools.Bounded)
	assert.False(t, bounded, "a start is quick, so the turn's default timeout covers it")
}

// The prompt and the description say the call answers at once and the report
// arrives later, as a notification the model must not predict.
func TestThePromptSaysTheReportComesLater(t *testing.T) {
	assert.Contains(t, New().Prompt(), "its report reaches you later as a task notification")
	assert.Contains(t, New().Prompt(), "Never fabricate or predict a pending agent's report")
	assert.Contains(t, description, "get its report back as a notification")
	assert.Contains(t, description, "wait for its notification")
	assert.NotContains(t, description, "Your call waits")
}

// Stored rows read the same forever: never edit this table.
func TestActionOfReadsOldRowsTheSame(t *testing.T) {
	for raw, want := range map[string]tools.Action{
		`{"description":"Find the crash","prompt":"Why does web crash?"}`: {
			Description: "Find the crash", Delegate: &tools.DelegateAction{Prompt: "Why does web crash?", AgentType: "general-purpose"},
		},
		`{"description":"Audit","prompt":"Check RBAC.","subagent_type":"general-purpose","model":"claude-haiku-5-5"}`: {
			Description: "Audit", Delegate: &tools.DelegateAction{Prompt: "Check RBAC.", AgentType: "general-purpose", Model: "claude-haiku-5-5"},
		},
	} {
		got, err := (&Tool{}).Action(json.RawMessage(raw), "", false)
		require.NoError(t, err, raw)
		assert.Equal(t, want, got, raw)
	}
}
