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

package chat

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/rawjson"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/llm"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/tools"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/tools/anthropicwebsearch"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/tools/bash"
)

// An answer with nothing written yet stores as the empty array, not as an empty
// column: the content column is JSON and "" is not.
func TestContentOfNoBlocksIsTheEmptyArray(t *testing.T) {
	t.Parallel()

	assert.Equal(t, emptyContent, marshalBlocks(nil))
	assert.Equal(t, emptyContent, marshalBlocks([]llm.Block{}))
}

// A message's thinking is what its blocks hold of it: empty for a question, for a
// message with none, and for content that does not parse.
func TestThinkingIsWhatTheContentHoldsOfIt(t *testing.T) {
	t.Parallel()

	answer := ChatMessage{Content: marshalBlocks(llm.AnswerBlocks("counting", "twelve"))}
	assert.Equal(t, "counting", answer.Thinking())

	question := ChatMessage{Content: marshalBlocks([]llm.Block{llm.TextBlock("how many?")})}
	assert.Empty(t, question.Thinking())
	assert.Empty(t, ChatMessage{Content: emptyContent}.Thinking())
	assert.Empty(t, ChatMessage{Content: rawjson.RawJSON("not json")}.Thinking())
}

func TestContentRoundTripsThroughTheColumn(t *testing.T) {
	t.Parallel()

	blocks := []llm.Block{llm.TextBlock("hi")}

	back, err := unmarshalBlocks(marshalBlocks(blocks))

	require.NoError(t, err)
	assert.Equal(t, blocks, back)
}

// A content column that is not JSON is the read's error, never an empty answer: a
// message silently read as having nothing to say would leave the wire accepting it.
func TestBlocksOfRefusesAColumnThatIsNotJSON(t *testing.T) {
	t.Parallel()

	_, err := unmarshalBlocks(rawjson.RawJSON("not json"))

	assert.Error(t, err)
}

// Citations that are not JSON are the read's error, never a message citing nothing.
func TestCitationListRefusesAStringThatIsNotJSON(t *testing.T) {
	t.Parallel()

	_, err := ChatMessage{Citations: "not json"}.CitationList()

	assert.ErrorContains(t, err, "citations")
}

// Only a failed answer has a kind, and an error runOutcome did not name is Other.
func TestFailureIsAFailedAnswersKind(t *testing.T) {
	t.Parallel()

	assert.Nil(t, ChatMessage{Status: StatusComplete}.Failure())
	assert.Nil(t, ChatMessage{Status: StatusCancelled, Error: usageLimitText}.Failure())
	assert.Equal(t, new(FailureOther), ChatMessage{Status: StatusFailed, Error: "fake: 503"}.Failure())
	assert.Equal(t, new(FailureOther), ChatMessage{Status: StatusFailed}.Failure())
}

// A block whose Input is not JSON fails loudly rather than writing a row the read
// cannot parse. llm.ToolUseBlock is what keeps it from happening.
func TestContentOfABlockWithBrokenInputPanics(t *testing.T) {
	t.Parallel()

	broken := []llm.Block{{Type: llm.BlockToolUse, Input: json.RawMessage("{")}}

	assert.Panics(t, func() { marshalBlocks(broken) })
}

// The list a message carries parses back into its calls, and a string that is not
// a list is an error rather than an empty list.
func TestAToolCallListRoundTrips(t *testing.T) {
	t.Parallel()

	msg := ChatMessage{ToolCalls: marshalToolCalls([]toolCallEntry{{ID: "t", Name: "bash", Arguments: `{}`, Status: toolSucceeded, Result: "ok"}}, tools.Box{})}
	calls, err := msg.ToolCallList()
	require.NoError(t, err)
	require.Len(t, calls, 1)
	assert.Equal(t, ToolCallSucceeded, calls[0].Status)
	assert.Equal(t, "ok", calls[0].Output)

	calls, err = ChatMessage{ToolCalls: emptyToolCalls}.ToolCallList()
	require.NoError(t, err)
	assert.Empty(t, calls)

	_, err = ChatMessage{}.ToolCallList()
	assert.Error(t, err)
}

// A row reads whatever it holds: arguments that do not parse are the text as a
// string, and a status the loop never writes reads as not run.
func TestAToolCallRowReadsWhateverItHolds(t *testing.T) {
	t.Parallel()

	var calls []ToolCall
	require.NoError(t, json.Unmarshal([]byte(marshalToolCalls([]toolCallEntry{{Arguments: "not json", Status: "pending"}}, tools.Box{})), &calls))
	assert.Equal(t, rawjson.RawJSON(`"not json"`), calls[0].Arguments)
	assert.Equal(t, ToolCallNotRun, calls[0].Status)
}

// A call whose tool has no way to show it, and one whose arguments its tool
// refuses, are served with no action and the rest of the call as it is.
func TestACallWithNoActionIsServedWithout(t *testing.T) {
	t.Parallel()

	var calls []ToolCall
	require.NoError(t, json.Unmarshal([]byte(marshalToolCalls([]toolCallEntry{
		{ID: "a", Name: "TaskStop", Arguments: `{"task_id":"t"}`, Status: toolSucceeded, Result: "Stopped t."},
		{ID: "b", Name: "Bash", Arguments: `{"command":""}`, Status: toolFailed, Result: `{"error":"bad-input"}`, Error: `{"error":"bad-input"}`},
		{ID: "c", Name: "Bash", Arguments: `{"command":"ls","workdir":"~bob"}`, Status: toolFailed, Result: `{"error":"bad-input"}`, Error: `{"error":"bad-input"}`},
	}, testReaders)), &calls))
	require.Len(t, calls, 3)
	for _, c := range calls {
		assert.Nil(t, c.Action, c.ID)
	}
	assert.Equal(t, "Stopped t.", calls[0].Output)
	assert.JSONEq(t, `{"command":""}`, string(calls[1].Arguments))
	assert.True(t, calls[2].IsError)
}

// A row is read by its name alone: a provider row keeps its contract beside
// the action, a call of ours has none, and a name no tool has shows nothing.
func TestAnActionIsReadByItsName(t *testing.T) {
	t.Parallel()

	var calls []ToolCall
	require.NoError(t, json.Unmarshal([]byte(marshalToolCalls([]toolCallEntry{
		{ID: "a", Name: testSearch.Name(), Contract: string(anthropicwebsearch.ContractName), ByProvider: true, Arguments: `{"query":"q"}`, Status: toolSucceeded},
		{ID: "b", Name: bash.Name, Contract: string(bash.ContractName), Arguments: `{"command":"ls"}`, Status: toolSucceeded},
		{ID: "c", Name: "web_search", Contract: string(anthropicwebsearch.ContractName), ByProvider: true, Arguments: `{"query":"q"}`},
	}, testReaders)), &calls))
	require.Len(t, calls, 3)

	assert.Equal(t, &tools.SearchAction{Query: "q"}, calls[0].Action.Search)
	assert.Equal(t, "web_search_20260318", calls[0].Contract)
	assert.Equal(t, new(tools.ActionSearch), calls[0].ActionKind)
	assert.Equal(t, "ls", calls[1].Action.Command.Text)
	assert.Equal(t, "bash/v1", calls[1].Contract)
	assert.Equal(t, new(tools.ActionCommand), calls[1].ActionKind)
	assert.Nil(t, calls[2].Action)
	assert.Nil(t, calls[2].ActionKind)
}

// A call's kind is its tool's, whatever its arguments hold, and none for a tool
// the box does not know.
func TestACallsKindIsItsTools(t *testing.T) {
	t.Parallel()

	var calls []ToolCall
	require.NoError(t, json.Unmarshal([]byte(marshalToolCalls([]toolCallEntry{
		{ID: "a", Name: bash.Name, Arguments: `{"command":""}`, Status: toolFailed, Result: `{"error":"bad-input"}`, Error: `{"error":"bad-input"}`},
		{ID: "b", Name: "Nope", Arguments: `{}`, Status: toolFailed, Result: `{"error":"unknown-tool"}`, Error: `{"error":"unknown-tool"}`},
	}, testReaders)), &calls))
	require.Len(t, calls, 2)

	assert.Nil(t, calls[0].Action)
	assert.Equal(t, new(tools.ActionCommand), calls[0].ActionKind)
	assert.Nil(t, calls[1].ActionKind)
}
