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

package llm

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The bytes are the stored content schema: a text block is exactly this, and a
// stored message reads back equal.
func TestTextBlocksRoundTripTheStoredFormat(t *testing.T) {
	blocks := []Block{TextBlock("hi")}

	b, err := json.Marshal(blocks)
	require.NoError(t, err)
	assert.Equal(t, `[{"type":"text","text":"hi"}]`, string(b), "no field beyond the two is written")

	var back []Block
	require.NoError(t, json.Unmarshal(b, &back))
	assert.Equal(t, blocks, back)
}

// A question with a card is the card, then the text, in the stored format.
func TestContextContentIsTheCardThenTheText(t *testing.T) {
	b, err := json.Marshal([]Block{ContextBlock("card"), TextBlock("hi")})
	require.NoError(t, err)
	assert.Equal(t, `[{"type":"context","text":"card"},{"type":"text","text":"hi"}]`, string(b))
}

// Prompt is the message as a string wire takes it: the card, then the question,
// a blank line between. A block with no text adds nothing, and what was shown of
// the thinking is not replayed.
func TestPromptIsContextAndTextInOrder(t *testing.T) {
	assert.Equal(t, "<context>\ncard\n</context>\n\nhi", Prompt([]Block{ContextBlock("card"), TextBlock("hi")}))
	assert.Equal(t, "hi", Prompt([]Block{TextBlock("hi")}))
	assert.Equal(t, "<context>\ncard\n</context>\n\nhi", Prompt([]Block{ContextBlock("card"), TextBlock(""), TextBlock("hi")}))
	assert.Equal(t, "hi", Prompt([]Block{ThinkingBlock("I should count them."), TextBlock("hi")}))
	assert.Empty(t, Prompt(nil))
}

// What a reader sees is the text blocks alone: adjacent ones run together as the
// reply split them, and nothing else adds a word.
func TestTextIsTheTextBlocksAlone(t *testing.T) {
	blocks := []Block{
		ThinkingBlock("I count them."),
		TextBlock("There are "),
		withPayload(TextBlock("2"), `{"type":"text","citations":[]}`),
		{Type: BlockNative, Payload: json.RawMessage(`{"type":"web_search_tool_result"}`)},
		TextBlock(" pods."),
		TaskNotificationBlock(TaskNotice{ID: "t1", Status: TaskExited}),
	}

	assert.Equal(t, "There are 2 pods.", Text(blocks))
	assert.Empty(t, Text(nil))
}

// A question's card is what the model was told, not what the user asked.
func TestTextSkipsAContextBlock(t *testing.T) {
	assert.Equal(t, "how many pods?", Text([]Block{ContextBlock("card"), TextBlock("how many pods?")}))
}

// Text on either side of a round was written at different moments, so it reads
// as two paragraphs. A round alone is no text: a result's text is the tool's.
func TestTextJoinsAcrossAToolRoundWithABlankLine(t *testing.T) {
	round := []Block{
		ToolUseBlock("call-1", "list_objects", json.RawMessage(`{"resource":"pods"}`)),
		ToolResultBlock("call-1", "2 pods", false),
	}
	blocks := append(append([]Block{TextBlock("Let me check.")}, round...), TextBlock("There are 2."))

	assert.Equal(t, "Let me check.\n\nThere are 2.", Text(blocks))
	assert.Empty(t, Text(round))
}

// A server call is a moment boundary too, and one ahead of any text opens no
// blank line.
func TestTextSeparatesAcrossAServerUse(t *testing.T) {
	search := ServerUseBlock("srvtoolu_1", "web_search", json.RawMessage(`{"query":"pods"}`))

	assert.Equal(t, "Searching.\n\nFound it.", Text([]Block{TextBlock("Searching."), search, TextBlock("Found it.")}))
	assert.Equal(t, "Found it.", Text([]Block{search, TextBlock("Found it.")}))
}

// An answer is the thinking then the text, each left out when empty, and nothing
// at all when both are.
func TestAnswerBlocksIsTheThinkingThenTheText(t *testing.T) {
	assert.Equal(t, []Block{ThinkingBlock("counting"), TextBlock("twelve")}, AnswerBlocks("counting", "twelve"))
	assert.Equal(t, []Block{ThinkingBlock("counting")}, AnswerBlocks("counting", ""))
	assert.Equal(t, []Block{TextBlock("twelve")}, AnswerBlocks("", "twelve"))
	assert.Nil(t, AnswerBlocks("", ""))
}

// The thinking is every thinking block, in order, a blank line between; an empty
// one adds nothing.
func TestThinkingJoinsAcrossBlocksAndSkipsEmptyOnes(t *testing.T) {
	assert.Equal(t, "first\n\nsecond", Thinking([]Block{ThinkingBlock("first"), ThinkingBlock("second")}))
	assert.Equal(t, "first\n\nsecond", Thinking([]Block{ThinkingBlock("first"), ThinkingBlock(""), ThinkingBlock("second")}))
	assert.Equal(t, "only", Thinking([]Block{ThinkingBlock("only"), TextBlock("twelve")}))
}

// Every other kind reads as no thinking, the text the reader sees included.
func TestThinkingIgnoresWhatIsNotASummary(t *testing.T) {
	assert.Empty(t, Thinking([]Block{ContextBlock("card"), TextBlock("twelve")}))
	assert.Empty(t, Thinking(nil))
}

// A round's blocks are the app's own bookkeeping: the readers that turn a message
// into a string pass over both kinds, as they pass over every kind but their own.
func TestToolBlocksAreTheAppsAndNotForReaders(t *testing.T) {
	round := []Block{
		ThinkingBlock("I should count them."),
		TextBlock("Let me look."),
		ToolUseBlock("call-1", "echo", json.RawMessage(`{"say":"hi"}`)),
		ToolResultBlock("call-1", "hi", false),
		TextBlock("Twelve."),
	}

	assert.Equal(t, "Let me look.\n\nTwelve.", Prompt(round))
	assert.Equal(t, "I should count them.", Thinking(round))
}

// Both kinds round-trip through the content column, and a text block's bytes carry
// none of the fields they added.
func TestToolBlocksRoundTrip(t *testing.T) {
	blocks := []Block{
		ToolUseBlock("call-1", "echo", json.RawMessage(`{"say":"hi"}`)),
		ToolResultBlock("call-1", `{"error":"timeout"}`, true),
		TextBlock("hi"),
	}

	b, err := json.Marshal(blocks)
	require.NoError(t, err)
	assert.Equal(t, `[{"type":"tool_use","id":"call-1","name":"echo","input":{"say":"hi"}},`+
		`{"type":"tool_result","text":"{\"error\":\"timeout\"}","id":"call-1","is_error":true},`+
		`{"type":"text","text":"hi"}]`, string(b))

	var back []Block
	require.NoError(t, json.Unmarshal(b, &back))
	assert.Equal(t, blocks, back)
}

// Input is an object or the call is the caller's bug, never the model's: every
// wire hands it what the provider sent or an empty object.
func TestAToolUseWithInvalidInputPanics(t *testing.T) {
	assert.Panics(t, func() { ToolUseBlock("call-1", "echo", json.RawMessage(`{`)) })
}

// A payload rides the block it was read off, and a block without one writes no key
// for it: the stored content of a plain answer is what it was.
func TestAPayloadRoundTrips(t *testing.T) {
	thinking := ThinkingBlock("I count them.")
	thinking.Payload = json.RawMessage(`{"type":"thinking","thinking":"I count them.","signature":"c2ln"}`)
	call := ToolUseBlock("call-1", "echo", json.RawMessage(`{"say":"hi"}`))
	call.Payload = json.RawMessage(`{"type":"function_call","call_id":"call-1","status":"completed"}`)
	blocks := []Block{thinking, call, TextBlock("hi")}

	b, err := json.Marshal(blocks)
	require.NoError(t, err)
	var back []Block
	require.NoError(t, json.Unmarshal(b, &back))
	assert.Equal(t, blocks, back)

	plain, err := json.Marshal([]Block{TextBlock("hi")})
	require.NoError(t, err)
	assert.NotContains(t, string(plain), "payload")
}

// withPayload is b with raw as its payload, for a test that cares only that it has one.
func withPayload(b Block, raw string) Block {
	b.Payload = json.RawMessage(raw)
	return b
}

// withoutPayload is b with its payload cleared, for comparing the app's fields.
func withoutPayload(b Block) Block {
	b.Payload = nil
	return b
}

// A payload is replayed as the API's own item, which is an object: no payload is
// fine, and so is an object however it is padded; anything else is not.
func TestAPayloadIsAnObjectOrNone(t *testing.T) {
	assert.True(t, TextBlock("hi").replayable())
	assert.True(t, withPayload(TextBlock("hi"), ` {"type":"text"} `).replayable())

	for _, raw := range []string{``, `7`, `[1]`, `{`} {
		assert.False(t, withPayload(TextBlock("hi"), raw).replayable(), "%q", raw)
	}
}

// A switch costs the other provider's payloads and nothing else: every app field
// stays, and a message naming no writer is foreign to everyone.
func TestStripForeignKeepsEveryAppField(t *testing.T) {
	blocks := []Block{
		withPayload(ThinkingBlock("I count them."), `{"type":"thinking","signature":"c2ln"}`),
		withPayload(ToolUseBlock("call-1", "echo", json.RawMessage(`{"say":"hi"}`)), `{"type":"function_call"}`),
		ToolResultBlock("call-1", "hi", false),
		TextBlock("twelve"),
	}
	m := Message{Role: "assistant", Blocks: blocks, ProviderID: "openai", Dialect: DialectResponses}

	assert.Equal(t, blocks, stripForeign("openai", DialectResponses, m), "its writer reads its payloads")

	stripped := stripForeign("anthropic", DialectMessages, m)
	assert.Equal(t, []Block{
		ThinkingBlock("I count them."),
		ToolUseBlock("call-1", "echo", json.RawMessage(`{"say":"hi"}`)),
		ToolResultBlock("call-1", "hi", false),
		TextBlock("twelve"),
	}, stripped)
	assert.Equal(t, blocks, m.Blocks, "the caller's blocks are not written to")

	unknown := Message{Role: "assistant", Blocks: blocks}
	assert.Equal(t, stripped, stripForeign("openai", DialectResponses, unknown), "a payload of unknown origin is replayed by no one")
}

// A payload is keyed on the provider and the dialect it was written on: the
// same provider on another API is sent the app's fields alone.
func TestStripForeignKeysOnTheDialect(t *testing.T) {
	blocks := []Block{
		withPayload(ThinkingBlock("I count them."), `{"type":"reasoning","encrypted_content":"ZW5j"}`),
		TextBlock("twelve"),
	}
	m := Message{Role: "assistant", Blocks: blocks, ProviderID: "openai", Dialect: DialectChatCompletions}

	assert.Equal(t, WithoutPayloads(blocks), stripForeign("openai", DialectResponses, m))
	assert.Equal(t, blocks, stripForeign("openai", DialectChatCompletions, m))
}

// A cited text goes to another provider as its text alone: the citations are the
// writer's payload, which only the writer takes back. Its writer keeps it whole.
func TestStripForeignSendsAForeignCitedTextAsItsText(t *testing.T) {
	cited := withPayload(TextBlock("There are 2 pods."), `{"type":"text","citations":[{"type":"web_search_result_location","encrypted_index":"ZW5j"}]}`)
	m := Message{Role: "assistant", Blocks: []Block{cited}, ProviderID: "anthropic", Dialect: DialectMessages}

	assert.Equal(t, []Block{TextBlock("There are 2 pods.")}, stripForeign("openai", DialectResponses, m))
	assert.Equal(t, []Block{cited}, stripForeign("anthropic", DialectMessages, m))
}

// A reasoning item with no summary is a block the replay needs and a reader has
// nothing to show for.
func TestThinkingSkipsAPayloadWithNoText(t *testing.T) {
	blocks := []Block{
		withPayload(ThinkingBlock(""), `{"type":"reasoning","encrypted_content":"c2Vr"}`),
		ThinkingBlock("I count them."),
	}

	assert.Equal(t, "I count them.", Thinking(blocks))
}

// The record keeps a call's arguments only when they are the object a wire takes
// back; anything else is the reply the wire could not read.
func TestToolInputKeepsAnObjectAlone(t *testing.T) {
	input, ok := toolInput(json.RawMessage(" {\"say\":\"hi\"} "))
	assert.True(t, ok)
	assert.JSONEq(t, `{"say":"hi"}`, string(input))
	assert.Equal(t, `{"say":"hi"}`, string(input), "trimmed, as the record keeps it")

	for _, raw := range []string{`{"say":`, `[1]`, `"hi"`, ``} {
		_, ok := toolInput(json.RawMessage(raw))
		assert.False(t, ok, raw)
	}
}

// A reader is shown the blocks without their payloads, and a slice holding none
// comes back as it is.
func TestWithoutPayloadsLeavesEverythingElse(t *testing.T) {
	plain := []Block{ThinkingBlock("I count them."), TextBlock("twelve")}
	assert.Equal(t, plain, WithoutPayloads(plain))

	withPayloads := []Block{
		withPayload(ThinkingBlock("I count them."), `{"type":"thinking","signature":"c2ln"}`),
		TextBlock("twelve"),
	}
	assert.Equal(t, plain, WithoutPayloads(withPayloads))
	assert.NotNil(t, withPayloads[0].Payload, "the caller's blocks are not written to")
}

// searchUse is a search as the Messages wire records it, payload and all.
func searchUse(id, query string) Block {
	return withPayload(ServerUseBlock(id, searchName, json.RawMessage(`{"query":"`+query+`"}`)),
		`{"type":"server_tool_use","id":"`+id+`","name":"web_search","input":{"query":"`+query+`"}}`)
}

// searchResult is the result of the search under id, kept for the replay alone.
func searchResult(id string) Block {
	return withPayload(Block{Type: BlockNative, ID: id},
		`{"type":"web_search_tool_result","tool_use_id":"`+id+`","content":[{"type":"web_search_result","url":"https://kubernetes.io/releases","title":"Releases","encrypted_content":"ZW5j"}]}`)
}

// citedText is a text block citing the search result, payload and all.
func citedText(text string) Block {
	return withPayload(TextBlock(text), `{"type":"text","text":"`+text+`","citations":[{"type":"web_search_result_location","url":"https://kubernetes.io/releases","title":"Releases","cited_text":"1.36","encrypted_index":"aWR4"}]}`)
}

// releases is the citation citedText makes, as a reader is shown it.
var releases = Citation{Type: "web_search_result_location", URL: "https://kubernetes.io/releases", Title: "Releases", CitedText: "1.36"}

// searchRow is an answer that searched: text, the call, its result, and a text
// citing it.
func searchRow() []Block {
	return []Block{TextBlock("Let me search."), searchUse("srv_1", "kubernetes 1.36"), searchResult("srv_1"), citedText("It shipped.")}
}

// A search is stored in a call's fields: its id, its tool's name and its
// arguments, beside the provider's item; the result is its payload alone.
func TestASearchIsStoredInTheAppsShape(t *testing.T) {
	blocks := searchRow()

	b, err := json.Marshal(blocks)
	require.NoError(t, err)
	var back []Block
	require.NoError(t, json.Unmarshal(b, &back))

	assert.Equal(t, blocks, back)
	assert.Contains(t, string(b), `{"type":"server_use","id":"srv_1","name":"acme_search","input":{"query":"kubernetes 1.36"},"payload":`)
	assert.Equal(t, "Let me search.\n\nIt shipped.", Prompt(blocks), "a query is never resent as text")
}

// A server call's arguments are an object the record can replay, as a
// tool_use's are.
func TestAServerUseWithInvalidInputPanics(t *testing.T) {
	assert.Panics(t, func() { ServerUseBlock("srv_1", searchName, json.RawMessage(`{`)) })
}

// A request that offers no tools, and a row that did not settle Complete, carry
// no search: the call and its result go, and the cited text stays as text.
func TestWithoutRoundsDropsASearch(t *testing.T) {
	blocks := searchRow()

	got := WithoutRounds(blocks)

	assert.Equal(t, []Block{TextBlock("Let me search."), withoutPayload(citedText("It shipped."))}, got)
	assert.NotNil(t, blocks[1].Payload, "the caller's blocks are not written to")
}

// A native block is nothing but its payload, so a reader is shown none of it.
func TestWithoutPayloadsDropsANativeBlock(t *testing.T) {
	blocks := searchRow()

	got := WithoutPayloads(blocks)

	assert.Equal(t, []Block{
		TextBlock("Let me search."),
		withoutPayload(searchUse("srv_1", "kubernetes 1.36")),
		withoutPayload(citedText("It shipped.")),
	}, got)
	assert.Len(t, blocks, 4, "the caller's slice is not written to")
}

// exitedNotice is a notice of a task that exited with code.
func exitedNotice(code int) Block {
	return TaskNotificationBlock(TaskNotice{
		ID: "t1", ToolUseID: "call-1", OutputFile: "/r/c/tasks/t1.output", Status: TaskExited,
		ExitCode: &code, Description: "Serve the site", Command: "make serve",
	})
}

// A notice goes to the model as the reference's <task-notification>, its summary
// naming the task by its description, else its command, and saying how it ended.
func TestANoticeIsATaskNotification(t *testing.T) {
	assert.Equal(t, "<task-notification>\n<task-id>t1</task-id>\n<tool-use-id>call-1</tool-use-id>\n"+
		"<output-file>/r/c/tasks/t1.output</output-file>\n<status>exited</status>\n"+
		"<summary>Background command \"Serve the site\" exited with code 0.</summary>\n</task-notification>",
		Prompt([]Block{exitedNotice(0)}))

	for n, want := range map[TaskNotice]string{
		{Status: TaskExited, Command: "make serve"}:                       `Background command "make serve" exited; its code could not be read.`,
		{Status: TaskStopped, StoppedBy: TaskStoppedByUser, Command: "x"}: `Background command "x" was stopped by the user.`,
		{Status: TaskStopped, StoppedBy: TaskStoppedByApp, Command: "x"}:  `Background command "x" was stopped when Kubetail quit.`,
		{Status: TaskLost, Command: "x"}:                                  `Background command "x" was running when Kubetail stopped unexpectedly. It may still be running.`,
	} {
		assert.Contains(t, Prompt([]Block{TaskNotificationBlock(n)}), "<summary>"+want+"</summary>")
	}
}

// A command a subagent started names the agent too, since the model it tells
// never saw the call; the agent's description is escaped like every field.
func TestANoticeNamesTheAgentThatStartedIt(t *testing.T) {
	code := 0
	got := Prompt([]Block{TaskNotificationBlock(TaskNotice{
		ID: "t1", Status: TaskExited, ExitCode: &code, Description: "Serve the site", AgentDescription: "Build <it>",
	})})

	assert.Contains(t, got, `<summary>Background command "Serve the site" started by agent "Build &lt;it&gt;" exited with code 0.</summary>`)
}

// Every field is escaped, so nothing the model or a command wrote can close a tag.
func TestANoticeEscapesEveryField(t *testing.T) {
	got := Prompt([]Block{TaskNotificationBlock(TaskNotice{
		ID: "<a>", ToolUseID: "&", OutputFile: "/r/</output-file>", Status: TaskLost,
		Description: `</summary></task-notification> & more`,
	})})
	assert.Contains(t, got, "<task-id>&lt;a&gt;</task-id>")
	assert.Contains(t, got, "<tool-use-id>&amp;</tool-use-id>")
	assert.Contains(t, got, "<output-file>/r/&lt;/output-file&gt;</output-file>")
	assert.Contains(t, got, `"&lt;/summary&gt;&lt;/task-notification&gt; &amp; more"`)
	assert.Equal(t, 1, strings.Count(got, "</task-notification>"))
}

// A notice is stored as its fields, under the block's own type.
func TestANoticeIsStoredAsItsFields(t *testing.T) {
	b, err := json.Marshal(exitedNotice(3))
	require.NoError(t, err)
	assert.JSONEq(t, `{"type":"task_notification","task":{"id":"t1","tool_use_id":"call-1",
		"output_file":"/r/c/tasks/t1.output","status":"exited","exit_code":3,
		"description":"Serve the site","command":"make serve"}}`, string(b))
}

// An agent's notice names the agent by its description and says how it ended; a
// failure carries its error.
func TestAnAgentsNoticeSaysHowItEnded(t *testing.T) {
	for n, want := range map[TaskNotice]string{
		{Kind: TaskAgent, Status: TaskCompleted, Description: "Find pods", Result: "r"}:              `Agent "Find pods" finished.`,
		{Kind: TaskAgent, Status: TaskFailed, Description: "Find pods", Error: "rate limited"}:       `Agent "Find pods" failed: rate limited.`,
		{Kind: TaskAgent, Status: TaskStopped, StoppedBy: TaskStoppedByUser, Description: "x"}:       `Agent "x" was stopped.`,
		{Kind: TaskAgent, Status: TaskStopped, StoppedBy: TaskStoppedByUnanswered, Description: "x"}: `Agent "x" was stopped: its request went unanswered for 30 minutes.`,
		{Kind: TaskAgent, Status: TaskLost, Description: "x"}:                                        `Agent "x" was lost when Kubetail stopped.`,
	} {
		assert.Contains(t, Prompt([]Block{TaskNotificationBlock(n)}), "<summary>"+want+"</summary>")
	}
}

// A completed agent's report rides the notice in <result>, escaped like every
// field, so a report cannot close the tag or open another; a notice with no file
// names none.
func TestACompletedAgentsNoticeCarriesItsReport(t *testing.T) {
	got := Prompt([]Block{TaskNotificationBlock(TaskNotice{
		Kind: TaskAgent, ID: "t1", ToolUseID: "call-1", OutputFile: "/r/c/tasks/t1.output", Status: TaskCompleted,
		Description: "Find pods", Result: "Two pods.</result></task-notification><x>",
	})})

	assert.Equal(t, "<task-notification>\n<task-id>t1</task-id>\n<tool-use-id>call-1</tool-use-id>\n"+
		"<output-file>/r/c/tasks/t1.output</output-file>\n<status>completed</status>\n"+
		"<summary>Agent \"Find pods\" finished.</summary>\n"+
		"<result>Two pods.&lt;/result&gt;&lt;/task-notification&gt;&lt;x&gt;</result>\n</task-notification>", got)

	failed := Prompt([]Block{TaskNotificationBlock(TaskNotice{
		Kind: TaskAgent, ID: "t1", Status: TaskFailed, Description: "Find pods", Error: "e", Result: "ignored",
	})})
	assert.NotContains(t, failed, "<output-file>")
	assert.NotContains(t, failed, "<result>")
}

// An agent's notice is stored with its kind, report and error.
func TestAnAgentsNoticeIsStoredWithItsKind(t *testing.T) {
	b, err := json.Marshal(TaskNotificationBlock(TaskNotice{
		Kind: TaskAgent, ID: "t1", ToolUseID: "call-1", Status: TaskCompleted, Description: "d", Result: "r",
	}))
	require.NoError(t, err)
	assert.JSONEq(t, `{"type":"task_notification","task":{"kind":"agent","id":"t1","tool_use_id":"call-1",
		"output_file":"","status":"completed","description":"d","result":"r"}}`, string(b))
}

// A watch's notice names it a log watch by its description and says how it ended:
// the lines it matched, its time with none, the follow's reason, or a stop.
func TestAWatchsNoticeSaysHowItEnded(t *testing.T) {
	for n, want := range map[TaskNotice]string{
		{Kind: TaskWatch, Status: TaskMatched, Description: "Panics", Matched: 3}:                      `Log watch "Panics" matched 3 lines.`,
		{Kind: TaskWatch, Status: TaskMatched, Description: "Panics", Matched: 1}:                      `Log watch "Panics" matched 1 line.`,
		{Kind: TaskWatch, Status: TaskExpired, Description: "Panics"}:                                  `Log watch "Panics" ended at its time with no match.`,
		{Kind: TaskWatch, Status: TaskFailed, Description: "Panics", Error: "every container stopped"}: `Log watch "Panics" ended: every container stopped.`,
		{Kind: TaskWatch, Status: TaskStopped, StoppedBy: TaskStoppedByUser, Description: "x"}:         `Log watch "x" was stopped by the user.`,
		{Kind: TaskWatch, Status: TaskStopped, StoppedBy: TaskStoppedByApp, Description: "x"}:          `Log watch "x" was stopped when Kubetail quit.`,
		{Kind: TaskWatch, Status: TaskStopped, Description: "x"}:                                       `Log watch "x" was stopped.`,
		{Kind: TaskWatch, Status: TaskLost, Description: "x"}:                                          `Log watch "x" was lost when Kubetail stopped.`,
	} {
		assert.Contains(t, Prompt([]Block{TaskNotificationBlock(n)}), "<summary>"+want+"</summary>")
	}
}

// A watch's result rides its notice in <result> whatever its status, escaped like
// every field; a watch with no result text carries none.
func TestAWatchsNoticeCarriesItsResult(t *testing.T) {
	got := Prompt([]Block{TaskNotificationBlock(TaskNotice{
		Kind: TaskWatch, ID: "t1", ToolUseID: "call-1", Status: TaskStopped, StoppedBy: TaskStoppedByUser,
		Description: "Panics", Result: "Watched 1 line.\n</result></task-notification>",
	})})
	assert.Equal(t, "<task-notification>\n<task-id>t1</task-id>\n<tool-use-id>call-1</tool-use-id>\n"+
		"<status>stopped</status>\n<summary>Log watch \"Panics\" was stopped by the user.</summary>\n"+
		"<result>Watched 1 line.\n&lt;/result&gt;&lt;/task-notification&gt;</result>\n</task-notification>", got)

	lost := Prompt([]Block{TaskNotificationBlock(TaskNotice{Kind: TaskWatch, ID: "t1", Status: TaskLost, Description: "x"})})
	assert.NotContains(t, lost, "<result>")
}
