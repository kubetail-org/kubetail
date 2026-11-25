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
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"
)

// FakeChunkDelay paces a hand-run answer so it visibly types itself.
const FakeChunkDelay = 30 * time.Millisecond

// FakeProvider is the provider a run with no key answers on, over the fake
// given. Its first model's efforts are ignored and exist so the picker has a
// second select to show; the second model takes no tools, so a consumer of the
// catalog's word has both arms to test.
func FakeProvider(f *Fake) Provider {
	return Provider{ID: "fake", Label: "Fake", Dialect: DialectFake, fake: f, Catalog: []Model{
		{ID: "fake", Label: "Fake model", Efforts: []string{"low", "high"}, DefaultEffort: "low", ContextWindow: 1_000_000, Tools: true, Cache: CacheAuto},
		{ID: "fake-no-tools", Label: "Fake model (no tools)", ContextWindow: 1_000_000, Cache: CacheAuto},
	}}
}

// thoughts is the thought ahead of each bank entry. Every reply thinks, so a dev
// build draws the disclosure with no key.
var thoughts = []string{
	"Let me count the pods in that namespace.",
	"I should check every replica's readiness.",
	"Let me compare each node's requests with its capacity.",
}

// bank is every reply, taken in order. Fixed, so a test knows which comes next.
var bank = []string{
	"Twelve pods are running in the default namespace.",
	"The deployment rolled out cleanly; every replica is ready.",
	"One node is under pressure: its memory request is near the cap.",
}

// Fake is what the tests and a dev build run on: the next thought and sentence
// of the bank, a word per chunk, with no key and no network. It ignores the
// request's model, effort, system and offer of tools; a reply asks for the calls
// a test staged.
type Fake struct {
	chunkDelay time.Duration

	mu        sync.Mutex
	next      int
	asked     int
	gate      <-chan struct{}
	failAfter int
	failErr   error
	panicMsg  string
	reply     []Chunk
	requests  []Request
	calls     []Block          // staged for the next reply
	queued    [][]Block        // staged for the replies after it, one round each
	repeat    []Block          // staged for every reply
	stops     []string         // staged for the next replies, one each
	uses      []map[string]int // staged for the next replies, one each
	usage     []Usage          // staged for the next replies, one each
	served    []string         // staged for the next replies, one each
	citations []Citation       // staged for the next reply
	callNo    int              // the ids minted so far
	routes    map[string]*Fake // by the prompt a request's first message ends in
	// root is the fake a route was made from, which mints its ids; nil on a root.
	root *Fake
}

// NewFake builds a fake. chunkDelay paces the chunks: ~30ms lets an answer type
// itself by hand, and tests pass 0.
func NewFake(chunkDelay time.Duration) *Fake {
	return &Fake{chunkDelay: chunkDelay}
}

// Stream emits the reply a chunk at a time, the thought then the sentence unless
// SetReply staged one, and returns it as blocks ending end_turn, folded through
// Partial as the agent folds them. Calls staged for this reply ride after its
// blocks, each with its staged id or one minted here, and stop it on tool_use; a
// staged stop reason wins over both. A staged count, report and served model
// ride the response, the fake's own report (fakeUsage) when none is staged, and
// staged citations the last text block's payload. A gate set for this reply holds it
// after the first chunk, or at its end when it has fewer than two, until the gate
// closes or ctx ends; a failure set for it ends the reply after the chunk it was
// told, or at its end when it has no more, with that error; a panic set for it
// panics before the first. Chunks of every kind count, so the first chunk is the
// thought's first word.
func (f *Fake) Stream(ctx context.Context, req Request, emit func(Chunk)) (Response, error) {
	f.mu.Lock()
	f.asked++
	f.requests = append(f.requests, req)
	if route := f.routes[firstText(req)]; route != nil {
		f.mu.Unlock()
		return route.Stream(ctx, req, emit)
	}
	chunks := f.reply
	if chunks == nil {
		chunks = wordChunks(thoughts[f.next%len(thoughts)], bank[f.next%len(bank)])
		f.next++
	}
	gate, failAfter, failErr, panicMsg, delay := f.gate, f.failAfter, f.failErr, f.panicMsg, f.chunkDelay
	staged, citations := f.calls, f.citations
	switch {
	case staged != nil:
	case len(f.queued) > 0:
		staged, f.queued = f.queued[0], f.queued[1:]
	default:
		staged = f.repeat
	}
	var stop string
	if len(f.stops) > 0 {
		stop, f.stops = f.stops[0], f.stops[1:]
	}
	var uses map[string]int
	if len(f.uses) > 0 {
		uses, f.uses = f.uses[0], f.uses[1:]
	}
	usage, counted := fakeUsage(req), len(f.usage) == 0
	if !counted {
		usage, f.usage = f.usage[0], f.usage[1:]
	}
	var served string
	if len(f.served) > 0 {
		served, f.served = f.served[0], f.served[1:]
	}
	f.gate, f.failErr, f.panicMsg, f.reply, f.calls, f.citations = nil, nil, "", nil, nil, nil
	f.mu.Unlock()

	if panicMsg != "" {
		panic(panicMsg)
	}

	var partial Partial
	for i, c := range chunks {
		if failErr != nil && i == failAfter {
			return Response{}, failErr
		}
		if gate != nil && i == 1 {
			if err := hold(ctx, gate); err != nil {
				return Response{}, err
			}
		}
		if err := pace(ctx, delay); err != nil {
			return Response{}, err
		}
		partial.Add(c)
		emit(c)
	}
	// A reply with no chunk left at the failure's point fails at its end, and one
	// with none left at the gate's is held there.
	if failErr != nil {
		return Response{}, failErr
	}
	if gate != nil && len(chunks) < 2 {
		if err := hold(ctx, gate); err != nil {
			return Response{}, err
		}
	}
	blocks, reason := partial.Blocks(), "end_turn"
	if last := lastText(blocks); last >= 0 && citations != nil {
		blocks[last].Payload = fakeCitedPayload(citations)
	}
	if len(staged) > 0 {
		blocks, reason = append(blocks, f.mint(staged)...), StopToolUse
	}
	if stop != "" {
		reason = stop
	}
	if counted {
		usage.OutputTokens = words(blocks)
	}
	return Response{Blocks: blocks, StopReason: reason, Model: served, ServerUses: uses, Usage: usage}, nil
}

// fakeUsage is the fake's report on req, before the reply's words are counted
// as its output. The cache counts are constants, since the fake models no cache
// and a store round trip needs numbers; the input includes them, as every real
// provider's does, so the report always splits into uncached and cached parts.
func fakeUsage(req Request) Usage {
	input := 0
	for _, m := range req.Messages {
		input += words(m.Blocks)
	}
	return Usage{Reported: true, InputTokens: input + 2 + 1, CacheReadTokens: 2, CacheWriteTokens: 1}
}

// words is how many words the blocks' text holds.
func words(blocks []Block) int {
	n := 0
	for _, b := range blocks {
		n += len(strings.Fields(b.Text))
	}
	return n
}

// fakeCited is the fake's own shape for a text block that cites: the fake has
// no provider's shape to keep, so it writes the citations as the app reads them.
type fakeCited struct {
	Citations []Citation `json:"citations"`
}

// fakeCitedPayload is citations as a text block's payload.
func fakeCitedPayload(citations []Citation) json.RawMessage {
	b, _ := json.Marshal(fakeCited{citations}) // strings always marshal
	return b
}

// fakeCitations is the citations the fake's text blocks make, off their payloads.
func fakeCitations(blocks []Block) []Citation {
	var out []Citation
	for _, b := range blocks {
		var cited fakeCited
		if b.Type == BlockText && b.Payload != nil && json.Unmarshal(b.Payload, &cited) == nil {
			out = append(out, cited.Citations...)
		}
	}
	return out
}

// lastText is the index of the last text block, or -1.
func lastText(blocks []Block) int {
	for i, b := range slices.Backward(blocks) {
		if b.Type == BlockText {
			return i
		}
	}
	return -1
}

// mint gives each staged call with no id the next one, and keeps a staged id. It
// runs once the reply has streamed, so a reply a failure or panic cut off mints
// none and the ids stay in step.
func (f *Fake) mint(staged []Block) []Block {
	if f.root != nil {
		return f.root.mint(staged)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	calls := make([]Block, 0, len(staged))
	for _, c := range staged {
		id := c.ID
		if id == "" {
			f.callNo++
			id = fmt.Sprintf("call-%d", f.callNo)
		}
		call := ToolUseBlock(id, c.Name, c.Input)
		// A staged payload rides out with the call: the readers that strip one are
		// tested through a whole turn.
		call.Payload = c.Payload
		calls = append(calls, call)
	}
	return calls
}

// Route is a fake of its own for the requests whose first message ends in the
// text prompt: a subagent's, whose one message is its brief, or a chat's, whose
// first is its first question. Its staging and its bank are its own, so a test
// stages a subagent's replies apart from its parent's, which ask at the same
// time. The root still logs every request, and mints every call's id.
func (f *Fake) Route(prompt string) *Fake {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.routes == nil {
		f.routes = map[string]*Fake{}
	}
	route := &Fake{chunkDelay: f.chunkDelay, root: f}
	f.routes[prompt] = route
	return route
}

// firstText is the text of the last text block of the request's first message,
// "" when it has none.
func firstText(req Request) string {
	if len(req.Messages) == 0 {
		return ""
	}
	return lastTextOf(req.Messages[0].Blocks)
}

// lastTextOf is the text of the last text block, "" when there is none.
func lastTextOf(blocks []Block) string {
	if i := lastText(blocks); i >= 0 {
		return blocks[i].Text
	}
	return ""
}

// wordChunks is a thought then a sentence, a word per chunk.
func wordChunks(thought, sentence string) []Chunk {
	var out []Chunk
	for _, word := range strings.SplitAfter(thought, " ") {
		out = append(out, Chunk{Kind: ChunkThinking, Text: word})
	}
	for _, word := range strings.SplitAfter(sentence, " ") {
		out = append(out, Chunk{Text: word})
	}
	return out
}

// hold waits for gate to close, or until ctx ends. A value sent on the gate
// panics: it would release the whole rest of the reply, so a test that sent one
// to step a chunk would read a finished answer as a held one.
func hold(ctx context.Context, gate <-chan struct{}) error {
	select {
	case _, open := <-gate:
		if open {
			panic("llm: a value was sent on the fake's gate; close it to release the reply")
		}
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// pace waits delay, or until ctx ends.
func pace(ctx context.Context, delay time.Duration) error {
	if delay == 0 {
		return ctx.Err()
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// SetChunkDelay repaces every reply from here on: a test sets 0, so a reply
// arrives at once.
func (f *Fake) SetChunkDelay(d time.Duration) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.chunkDelay = d
}

// LastRequest is the request the last Stream was handed, zero before the first.
func (f *Fake) LastRequest() Request {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.requests) == 0 {
		return Request{}
	}
	return f.requests[len(f.requests)-1]
}

// Asked is how many times Stream was called.
func (f *Fake) Asked() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.asked
}

// FailAfter makes the next reply fail with err after n chunks, or at its end when
// it has n or fewer.
func (f *Fake) FailAfter(n int, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.failAfter, f.failErr = n, err
}

// SetReply stages the chunks the next reply streams, in the order given, in place
// of the bank's. With none, the reply streams nothing and holds no block.
func (f *Fake) SetReply(chunks ...Chunk) {
	f.mu.Lock()
	defer f.mu.Unlock()
	// Never nil, so Stream tells a reply set to nothing from one not set.
	f.reply = append([]Chunk{}, chunks...)
}

// SetGate holds the next reply after its first chunk, or at its end when it has
// fewer than two, until gate closes. A value sent on gate panics the reply.
func (f *Fake) SetGate(gate <-chan struct{}) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.gate = gate
}

// StagedCall is a call for the fake to ask: the tool and its input, valid JSON.
// The fake mints the id when the call goes out.
func StagedCall(name, input string) Block {
	return ToolUseBlock("", name, json.RawMessage(input))
}

// StagedCallWithID is a staged call under an id of the test's choosing, so a
// reply can repeat one the way a provider must not. The fake mints no id for it.
func StagedCallWithID(id, name, input string) Block {
	return ToolUseBlock(id, name, json.RawMessage(input))
}

// SetToolCalls makes the next reply ask for calls, after its thought and text, and
// stop on tool_use. The reply after answers from the bank again.
func (f *Fake) SetToolCalls(calls ...Block) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = calls
}

// QueueToolCalls makes the replies after the next ask for calls, one round each
// and in order: a turn and the agents it starts share the queue, so each of
// their replies can be staged ahead. A nil round asks for nothing.
func (f *Fake) QueueToolCalls(rounds ...[]Block) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.queued = append(f.queued, rounds...)
}

// Requests is every request Stream was handed, in order.
func (f *Fake) Requests() []Request {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.requests)
}

// RepeatToolCalls makes every reply from now on ask for calls, so a caller's bound
// on rounds can be counted. Nil stops it.
func (f *Fake) RepeatToolCalls(calls ...Block) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.repeat = calls
}

// SetStop ends the next replies on reasons, one each and in order, whatever a
// reply holds: with calls staged it is a call the cap cut off, with none a
// malformed tool_use reply or a paused one.
func (f *Fake) SetStop(reasons ...string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.stops = reasons
}

// SetServerUses stages the counts the next replies report, by tool name, one each
// and in order. The fake reports none otherwise.
func (f *Fake) SetServerUses(uses ...map[string]int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.uses = uses
}

// SetUsage stages the reports the next replies make, one each and in order, in
// place of the fake's own count. A zero Usage is a call that reported none.
func (f *Fake) SetUsage(usage ...Usage) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.usage = usage
}

// SetServedModel stages the model the next replies say they served, one each
// and in order. The fake names none otherwise.
func (f *Fake) SetServedModel(ids ...string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.served = ids
}

// SetCitations stages the citations the next reply's last text block makes, as
// its payload in the fake's own shape (fakeCited).
func (f *Fake) SetCitations(citations ...Citation) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.citations = citations
}

// PanicNext makes the next reply panic with msg before its first chunk, as a
// provider SDK might.
func (f *Fake) PanicNext(msg string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.panicMsg = msg
}
