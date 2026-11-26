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
	"bytes"
	"encoding/json"
	"slices"
	"strconv"
	"strings"
	"time"
)

// BlockType names a kind of Block, and is the stored "type" field's word.
type BlockType string

// Block is one piece of a message's content, the shape an answer is stored in. A
// message's content is a []Block, marshalled as it stands. The type names the
// fields that are set; omitempty keeps the other kinds out of a text block's bytes.
type Block struct {
	Type    BlockType       `json:"type"`
	Text    string          `json:"text,omitempty"`
	ID      string          `json:"id,omitempty"`
	Name    string          `json:"name,omitempty"`
	Input   json.RawMessage `json:"input,omitempty"`
	IsError bool            `json:"is_error,omitempty"`
	// Payload is the provider's own item, verbatim, on a block only its writer
	// can take back whole: a signed thinking block, a reasoning item with its
	// encrypted content, a call a vendor hung its own data on. The message names
	// the writer, its provider and dialect, who replays the payload; every other
	// target is sent the app's fields alone.
	Payload json.RawMessage `json:"payload,omitempty"`
	// Task is a task notification's notice.
	Task *TaskNotice `json:"task,omitempty"`
}

// The kinds, and the fields each sets beside Type.
const (
	// BlockText is text the model wrote or the user sent: Text.
	BlockText BlockType = "text"
	// BlockContext is what the app tells the model on a question, one block ahead
	// of the text holding a section per card the app attaches, the cluster card
	// first: Text. Every dialect sends it as text ahead of the message's own.
	BlockContext BlockType = "context"
	// BlockThinking is what the provider showed of an answer's thinking, a summary
	// or the reasoning itself: Text; Payload when the provider signed or
	// encrypted it. One block per provider item, in the reply's order, since a
	// payload must sit where the wire placed it. A block with a payload and no
	// text is kept for the replay and reads as no thinking.
	BlockThinking BlockType = "thinking"
	// BlockToolUse is the model asking for one tool call: ID, the provider's id for
	// it; Name, the offer's; Input, the arguments, a JSON object.
	BlockToolUse BlockType = "tool_use"
	// BlockToolResult is the sidecar answering one call: ID, the tool_use's; Text,
	// what the model reads; IsError, that the call did not produce one.
	BlockToolResult BlockType = "tool_result"
	// BlockTaskNotification is the app telling the model how a background task
	// ended, on a user message: Task. Every dialect sends it as text.
	BlockTaskNotification BlockType = "task_notification"
	// BlockServerUse is one call of a tool the provider ran, in a tool_use's
	// fields: ID, the call's id; Name, its tool's name; Input, its arguments, a
	// JSON object; Payload, the provider's item. One with no payload is never
	// sent: a call seen on the stream before the reply ended, or a fake's.
	BlockServerUse BlockType = "server_use"
	// BlockNative is a provider item kept for the replay and never drawn:
	// Payload alone, and ID when it answers a call (a search's result, under the
	// call's id).
	BlockNative BlockType = "native"
)

// TaskStatus is how a background task ended.
type TaskStatus string

const (
	TaskExited  TaskStatus = "exited"
	TaskStopped TaskStatus = "stopped"
	// TaskLost is a task that was running when the app stopped unexpectedly.
	TaskLost TaskStatus = "lost"
	// TaskCompleted is an agent that answered, and TaskFailed one that could not,
	// or a watch that ended on an error or the follow's end with no line.
	TaskCompleted TaskStatus = "completed"
	TaskFailed    TaskStatus = "failed"
	// TaskMatched is a watch that ended holding lines, and TaskExpired one whose
	// time ran out with none.
	TaskMatched TaskStatus = "matched"
	TaskExpired TaskStatus = "expired"
)

// TaskKind is what a task is: a background command, an agent, or a log watch.
type TaskKind string

const (
	TaskCommand TaskKind = "command"
	TaskAgent   TaskKind = "agent"
	TaskWatch   TaskKind = "watch"
)

// Who stopped a task, when a notice is written for its stop: the model's own
// stop tells it in the call's result instead.
const (
	TaskStoppedByUser = "user"
	TaskStoppedByApp  = "app"
	// TaskStoppedByUnanswered is an agent whose approval request waited out its
	// bound, UnansweredLimit.
	TaskStoppedByUnanswered = "unanswered"
)

// UnansweredLimit is how long an agent's approval request waits on the user
// before the agent is stopped. The notice names it, so it lives beside the words.
const UnansweredLimit = 30 * time.Minute

// TaskNotice is what a notice says of one task. Description and Command are one
// line each, cut by whoever writes the notice.
type TaskNotice struct {
	// Kind is empty on some stored notices, which read as a command's.
	Kind       TaskKind   `json:"kind,omitempty"`
	ID         string     `json:"id"`
	ToolUseID  string     `json:"tool_use_id"`
	OutputFile string     `json:"output_file"`
	Status     TaskStatus `json:"status"`
	// StoppedBy is set on a stopped task alone.
	StoppedBy string `json:"stopped_by,omitempty"`
	// ExitCode is set on an exited task whose code could be read.
	ExitCode    *int   `json:"exit_code,omitempty"`
	Description string `json:"description,omitempty"`
	Command     string `json:"command,omitempty"`
	// AgentDescription is the description of the Agent call whose subagent started
	// the task, empty for a task the answer's own call started.
	AgentDescription string `json:"agent_description,omitempty"`
	// Result is a completed agent's report, or a finished watch's sample; Error is
	// a failed agent's error, or a failed watch's reason.
	Result string `json:"result,omitempty"`
	Error  string `json:"error,omitempty"`
	// Matched is how many lines a watch held.
	Matched int `json:"matched,omitempty"`
}

// TextBlock is a block holding s.
func TextBlock(s string) Block {
	return Block{Type: BlockText, Text: s}
}

// ContextBlock is a block holding the context's text: its sections alone, with
// no tags around them.
func ContextBlock(text string) Block {
	return Block{Type: BlockContext, Text: text}
}

// TaskNotificationBlock is a block holding the notice n.
func TaskNotificationBlock(n TaskNotice) Block {
	return Block{Type: BlockTaskNotification, Task: &n}
}

// readsAsText is whether b goes to the model as text: a text or context block
// with something in it, or a notice.
func (b Block) readsAsText() bool {
	switch b.Type {
	case BlockText, BlockContext:
		return b.Text != ""
	case BlockTaskNotification:
		return b.Task != nil
	}
	return false
}

// wireText is a block readsAsText holds as the model reads it. A context block
// goes inside <context> tags, the convention the system prompt names, so the
// model sees where the app's data ends; the record holds the sections alone. A
// notice is the reference's <task-notification>.
func (b Block) wireText() string {
	switch b.Type {
	case BlockContext:
		return "<context>\n" + b.Text + "\n</context>"
	case BlockTaskNotification:
		return b.Task.wireText()
	}
	return b.Text
}

// noticeEscaper escapes what could close or open a tag, in every field: the
// description and command are the model's, and the path is a file's.
var noticeEscaper = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;")

// wireText is the notice in the reference's shape: the output file when there is
// one, and a completed agent's report or a watch's sample as <result>.
func (n *TaskNotice) wireText() string {
	e := noticeEscaper.Replace
	s := "<task-notification>\n<task-id>" + e(n.ID) + "</task-id>\n<tool-use-id>" + e(n.ToolUseID) + "</tool-use-id>\n"
	if n.OutputFile != "" {
		s += "<output-file>" + e(n.OutputFile) + "</output-file>\n"
	}
	s += "<status>" + e(string(n.Status)) + "</status>\n<summary>" + e(n.summary()) + "</summary>\n"
	if n.Kind == TaskAgent && n.Status == TaskCompleted || n.Kind == TaskWatch && n.Result != "" {
		s += "<result>" + e(n.Result) + "</result>\n"
	}
	return s + "</task-notification>"
}

// summary is one sentence on how the task ended, naming it by its description,
// else its command, and the agent that started it when one did.
func (n *TaskNotice) summary() string {
	switch n.Kind {
	case TaskAgent:
		return n.agentSummary()
	case TaskWatch:
		return n.watchSummary()
	}
	name := n.Description
	if name == "" {
		name = n.Command
	}
	head := `Background command "` + name + `" `
	if n.AgentDescription != "" {
		head += `started by agent "` + n.AgentDescription + `" `
	}
	switch {
	case n.Status == TaskExited && n.ExitCode != nil:
		return head + "exited with code " + strconv.Itoa(*n.ExitCode) + "."
	case n.Status == TaskExited:
		return head + "exited; its code could not be read."
	case n.Status == TaskStopped && n.StoppedBy == TaskStoppedByApp:
		return head + "was stopped when Kubetail quit."
	case n.Status == TaskStopped:
		return head + "was stopped by the user."
	}
	return head + "was running when Kubetail stopped unexpectedly. It may still be running."
}

// agentSummary is summary for an agent, named by its call's description.
func (n *TaskNotice) agentSummary() string {
	head := `Agent "` + n.Description + `" `
	switch {
	case n.Status == TaskCompleted:
		return head + "finished."
	case n.Status == TaskFailed:
		return head + "failed: " + n.Error + "."
	case n.Status == TaskStopped && n.StoppedBy == TaskStoppedByUnanswered:
		return head + "was stopped: its request went unanswered for " + strconv.Itoa(int(UnansweredLimit.Minutes())) + " minutes."
	case n.Status == TaskStopped:
		return head + "was stopped."
	}
	return head + "was lost when Kubetail stopped."
}

// watchSummary is summary for a watch, named by its call's description.
func (n *TaskNotice) watchSummary() string {
	head := `Log watch "` + n.Description + `" `
	switch {
	case n.Status == TaskMatched && n.Matched == 1:
		return head + "matched 1 line."
	case n.Status == TaskMatched:
		return head + "matched " + strconv.Itoa(n.Matched) + " lines."
	case n.Status == TaskExpired:
		return head + "ended at its time with no match."
	case n.Status == TaskFailed:
		return head + "ended: " + n.Error + "."
	case n.Status == TaskStopped && n.StoppedBy == TaskStoppedByUser:
		return head + "was stopped by the user."
	case n.Status == TaskStopped && n.StoppedBy == TaskStoppedByApp:
		return head + "was stopped when Kubetail quit."
	case n.Status == TaskStopped:
		return head + "was stopped."
	}
	return head + "was lost when Kubetail stopped."
}

// ThinkingBlock is a block holding what was shown of the thinking.
func ThinkingBlock(s string) Block {
	return Block{Type: BlockThinking, Text: s}
}

// ToolUseBlock is the model's call of name under id. input must be valid JSON, so
// a []Block always marshals: a caller handing it anything else has a bug, and a
// stored block's bytes were checked by the unmarshal that read them.
func ToolUseBlock(id, name string, input json.RawMessage) Block {
	if !json.Valid(input) {
		panic("llm: a tool_use block's input must be valid JSON")
	}
	return Block{Type: BlockToolUse, ID: id, Name: name, Input: input}
}

// ServerUseBlock is the provider's call, under id, of the tool named name. input
// must be valid JSON, as ToolUseBlock's must.
func ServerUseBlock(id, name string, input json.RawMessage) Block {
	if !json.Valid(input) {
		panic("llm: a server_use block's input must be valid JSON")
	}
	return Block{Type: BlockServerUse, ID: id, Name: name, Input: input}
}

// toolInput is a call's arguments as the record keeps them: the object, trimmed,
// and false for anything else. The wires refuse a non-object on replay, so a
// call the cap cut off inside its arguments is stored as {} and answered
// not-run, and one that ended normally with arguments that are not an object
// fails the reply as one the wire could not read.
func toolInput(raw json.RawMessage) (json.RawMessage, bool) {
	trimmed := bytes.TrimSpace(raw)
	if !bytes.HasPrefix(trimmed, []byte("{")) || !json.Valid(trimmed) {
		return nil, false
	}
	return trimmed, true
}

// replayable reports whether b's payload, if it has one, is a JSON object. A
// payload is replayed as the API's own item, and the API takes nothing else.
func (b Block) replayable() bool {
	if b.Payload == nil {
		return true
	}
	_, ok := toolInput(b.Payload)
	return ok
}

// ToolResultBlock is the answer to the call under id.
func ToolResultBlock(id, text string, isError bool) Block {
	return Block{Type: BlockToolResult, ID: id, Text: text, IsError: isError}
}

// isToolBlock reports whether b is a call or its result.
func (b Block) isToolBlock() bool { return b.Type == BlockToolUse || b.Type == BlockToolResult }

// isRoundBlock reports whether b belongs to a round: a call or its result, the
// app's or the provider's.
func (b Block) isRoundBlock() bool {
	return b.isToolBlock() || b.Type == BlockServerUse || b.Type == BlockNative
}

// WithoutRounds is blocks with every call and result left out, a search and its
// result included, and every payload with them: a payload is the provider's item
// in the reply it was read off, and the Responses API refuses a reasoning item
// whose following call is gone. The text stays, since that is what a reader and
// the model see. A copy: the caller's slice is not written to.
func WithoutRounds(blocks []Block) []Block {
	out := slices.DeleteFunc(slices.Clone(blocks), Block.isRoundBlock)
	for i := range out {
		out[i].Payload = nil
	}
	return out
}

// withoutServerCalls is blocks with every server call and its result left out,
// and every text's payload with them, since a citation in it points into a
// result that is gone. A copy: the caller's slice is not written to.
func withoutServerCalls(blocks []Block) []Block {
	out := slices.DeleteFunc(slices.Clone(blocks), func(b Block) bool {
		return b.Type == BlockServerUse || b.Type == BlockNative
	})
	for i := range out {
		if out[i].Type == BlockText {
			out[i].Payload = nil
		}
	}
	return out
}

// WithoutPayloads is blocks with every payload removed, and every native block
// with it, being nothing but its payload: what a reader is shown, since a
// payload is the provider's alone and can run to kilobytes. A slice with no
// payload comes back as it is. chat applies it to what it publishes and
// serves, never to the history it replays.
func WithoutPayloads(blocks []Block) []Block {
	if !slices.ContainsFunc(blocks, func(b Block) bool { return b.Payload != nil }) {
		return blocks
	}
	out := slices.DeleteFunc(slices.Clone(blocks), func(b Block) bool { return b.Type == BlockNative })
	for i := range out {
		out[i].Payload = nil
	}
	return out
}

// ThinkingSeparator goes between the sections of a summary, on the stream and in
// the record alike, so the two read the same.
const ThinkingSeparator = "\n\n"

// AnswerBlocks is an answer as its blocks: the thinking, then the text, each left
// out when empty.
func AnswerBlocks(thinking, text string) []Block {
	var out []Block
	if thinking != "" {
		out = append(out, ThinkingBlock(thinking))
	}
	if text != "" {
		out = append(out, TextBlock(text))
	}
	return out
}

// Thinking is what an answer holds of its thinking: its thinking blocks in order,
// the empty ones left out, a separator between.
func Thinking(blocks []Block) string {
	var parts []string
	for _, b := range blocks {
		if b.Type == BlockThinking && b.Text != "" {
			parts = append(parts, b.Text)
		}
	}
	return strings.Join(parts, ThinkingSeparator)
}

// Text is what a reader sees of a message: its text blocks, adjacent ones
// joined as they stand, a blank line where a tool round or a server call falls
// between two. Every other kind is left out. The webview's textOf reads the same
// way, so a report and the transcript split text at the same moments.
func Text(blocks []Block) string {
	var text strings.Builder
	separate := false
	for _, b := range blocks {
		switch {
		case b.isToolBlock() || b.Type == BlockServerUse:
			separate = text.Len() > 0
		case b.Type == BlockText:
			if separate {
				text.WriteString("\n\n")
				separate = false
			}
			text.WriteString(b.Text)
		}
	}
	return text.String()
}

// Prompt is what the model reads of a message on a wire that takes one as a single
// string: its context, notice and text blocks in order, the context in its tags, a blank
// line between. The reader's view is the text blocks alone.
func Prompt(blocks []Block) string {
	var parts []string
	for _, b := range blocks {
		if b.readsAsText() {
			parts = append(parts, b.wireText())
		}
	}
	return strings.Join(parts, "\n\n")
}
