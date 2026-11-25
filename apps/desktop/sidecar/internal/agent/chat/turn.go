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

// The turn: one send's answer as it runs, from the reservation a send takes to the
// settle write, and the recorder the agent loop reports to.
package chat

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/agent/loop"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/apimeta"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/rawjson"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/llm"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/run/session"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/tools"
)

// turn is a chat's one in-flight answer. It is the reservation Send takes, the
// live message every read overlays, and the loop.Recorder its run reports to:
// its own run's calls are the runJournal it embeds, which also names its service
// and chat. msg is read and written under the service's turnsMu; the call records
// are the run goroutine's alone, written by the recorder methods and read by the
// settle that follows them; the rest is set before the goroutine starts and
// read-only after.
type turn struct {
	*runJournal
	ctx    context.Context
	cancel context.CancelFunc
	// done closes when the goroutine returns, settled, released and kicked;
	// Delete joins on it.
	done chan struct{}
	// retrying closes when the settle's first attempt fails, and gone once a
	// delete's write has taken the chat, which ends the retry.
	retrying, gone chan struct{}
	goneOnce       sync.Once
	// settled is whether settle ran, so runTurn settles what the run did not;
	// landed whether its write has; and succeeded whether it landed the run
	// succeeded, which kicks the chat.
	settled, landed, succeeded bool
	// The outcome settle fixes, which every attempt writes whole: the run's
	// status and error, the answer's content, and when it ended.
	status   RunStatus
	errText  string
	answer   rawjson.RawJSON
	at       time.Time
	attempts int
	// checkpointed is when Progress last wrote the live content to the row; the
	// answer's creation until the first.
	checkpointed time.Time
	// msg is the answer as it is now: the empty row a send wrote, then the rounds so
	// far. Its ID is empty until startTurn, so a reserved turn overlays nothing.
	msg ChatMessage
	// clusterID is the chat's stored cluster, read before the run starts, and
	// outsideSandbox its switch, read in the transaction that reserved the turn,
	// so a send's runtime matches the context block it wrote: what its tools, and
	// the subagents it spawns, run with. A switch flipped meanwhile changes the
	// next turn. networkThisTurn is the send's toggle, pinned beside it.
	clusterID       apimeta.ClusterID
	outsideSandbox  bool
	networkThisTurn bool
	// disabledTools is the tools the user turned off, read where the turn was
	// reserved: the turn and its subagents are offered what was on then.
	disabledTools []string
}

// runJournal is one run's journal: the loop's Recorder and Approver, which
// writes each call's rows as the run reports it and keeps them, with the chat it
// is in, the run and the target it runs on. llmCalls is every model call of the
// run, in order, the last the one in flight; toolCalls every tool call. Both are held whole, each set only to what landed,
// so the settle can write the rows again. openTool is the row Approve or
// ToolCallStarted opened and ToolCallFinished has not closed, and toolSeq the
// next call's place in the current reply.
type runJournal struct {
	s      *service
	chatID ChatID
	runID  RunID
	target llm.Target
	// unansweredLimit bounds a wait on the user: a subagent's, since a request
	// in a chat the user is not looking at would hold its slot for good. Zero on
	// a turn's own run, whose wait holds the turn the user sees and can cancel.
	unansweredLimit time.Duration
	// publish shows what the run just wrote, and the status the answer is in
	// unless status is empty.
	publish func(status MessageStatus)
	// agentCallID is the Agent call a subagent's run is under, which each of its
	// rows carries; "" on the turn's own run.
	agentCallID ToolCallID
	llmCalls    []*llmCallEntry
	toolCalls   []*toolCallEntry
	openTool    *toolCallEntry
	toolSeq     int

	// The proxies' requests run on goroutines of their own while the loop waits
	// in the call's Run. journalMu serializes what they change on the journal,
	// and is never held across a wait on the user; askMu holds one ask at a
	// time from its pending row to its end, so a second waits for the first
	// answer while a record lands at once. asking, under journalMu, is an ask
	// waiting, which a record publishes as still waiting.
	journalMu sync.Mutex
	askMu     sync.Mutex
	asking    bool
}

// isTurns reports whether this is the turn's own run rather than a subagent's.
func (j *runJournal) isTurns() bool { return j.agentCallID == "" }

// reserveTurn places a new turn in the chat's slot, refusing a chat that already holds
// one. The check and the reservation are one critical section, so two sends cannot
// both pass. The turn's context exists from here, so a cancel before it runs is
// safe.
func (s *service) reserveTurn(chatID ChatID, runID RunID, target llm.Target) (*turn, error) {
	s.turnsMu.Lock()
	defer s.turnsMu.Unlock()
	if s.deleting[chatID] > 0 {
		return nil, ErrChatGone
	}
	if _, held := s.turns[chatID]; held {
		return nil, ErrTurnInFlight
	}
	t := &turn{done: make(chan struct{}), retrying: make(chan struct{}), gone: make(chan struct{})}
	t.runJournal = &runJournal{s: s, chatID: chatID, runID: runID, target: target, publish: t.publishLive}
	t.ctx, t.cancel = context.WithCancel(s.ctx)
	s.turns[chatID] = t
	return t, nil
}

// turnOf is the chat's in-flight turn, or nil.
func (s *service) turnOf(chatID ChatID) *turn {
	s.turnsMu.Lock()
	defer s.turnsMu.Unlock()
	return s.turns[chatID]
}

// guardDelete marks the chat as being deleted and returns the turn it holds, if
// any, in one critical section: no reservation can slip in between.
func (s *service) guardDelete(chatID ChatID) *turn {
	s.turnsMu.Lock()
	defer s.turnsMu.Unlock()
	s.deleting[chatID]++
	return s.turns[chatID]
}

// releaseDelete lifts the guard once the delete's write has returned.
func (s *service) releaseDelete(chatID ChatID) {
	s.turnsMu.Lock()
	defer s.turnsMu.Unlock()
	if s.deleting[chatID]--; s.deleting[chatID] == 0 {
		delete(s.deleting, chatID)
	}
}

// releaseTurn empties the chat's slot, dropping the overlay with it.
func (s *service) releaseTurn(t *turn) {
	s.turnsMu.Lock()
	delete(s.turns, t.chatID)
	s.turnsMu.Unlock()
}

// abandonTurn releases a turn that never ran: a send that failed after reserving.
func (s *service) abandonTurn(t *turn) {
	t.cancel()
	close(t.done)
	s.releaseTurn(t)
}

// startTurn makes the empty answer the live message and starts the goroutine. A
// service that is stopping abandons it: the rows are written, and the next start
// fails the run as stranded.
func (s *service) startTurn(t *turn, msg ChatMessage) {
	s.turnsMu.Lock()
	t.msg = msg
	s.turnsMu.Unlock()
	t.checkpointed = msg.CreatedAt
	if s.enter() != nil {
		s.abandonTurn(t)
		return
	}
	go s.runTurn(t)
}

// runTurn answers one send and ends in a fixed order: settle, release, notify. A
// watcher re-reading on the completion ping must find the settled row, never the
// streaming overlay.
func (s *service) runTurn(t *turn) {
	defer s.wg.Done()
	defer close(t.done)
	defer t.cancel()

	res, err := s.run(t)
	if !t.settled {
		// The run ended before Settled: the history read or the claim failed, a
		// cancel landed first, or the stream panicked.
		t.settle(res, err)
	}
	if err != nil && t.ctx.Err() == nil {
		slog.Warn("chat turn failed", "chat", t.chatID, "err", err)
	}
	s.settleUntilLanded(t)
	s.releaseTurn(t)
	s.notify(messagesKey(t.chatID))
	s.notify(chatsKey)
	// A cancelled or failed turn does not: a Cancel stops the chat.
	if t.succeeded {
		s.kick(t.chatID)
	}
}

// run is the agent's run over the chat's history. The stream is third-party code
// on a goroutine of the service's own, so a panic in it fails this turn rather
// than the process.
func (s *service) run(t *turn) (res loop.Result, err error) {
	defer func() {
		if p := recover(); p != nil {
			err = fmt.Errorf("panic: %v", p)
		}
	}()
	history, err := s.history(t)
	if err != nil {
		return loop.Result{}, err
	}
	chat, err := s.chatOf(t)
	if err != nil {
		return loop.Result{}, err
	}
	t.clusterID = chat.ClusterID
	spec := loop.Turn{
		Target: t.target, SystemPrompt: SystemPrompt(), Messages: history, AffinityKey: string(t.chatID),
		Tools: s.boxFor(t.target, t.disabledTools),
		Runtime: tools.Runtime{
			ClusterID: t.clusterID, ChatID: t.chatID, Session: t.session(),
			Dir: s.chatDir(t.chatID), Tasks: s.chatTasks(t.chatID, t.runJournal), Files: s.chatFiles(t.chatID),
			Agent: t, Watches: s.chatWatches(t.chatID, t.runJournal), ActionAsker: actionAsker{j: t.runJournal, run: t.ctx},
		},
		MaxToolCalls: MaxToolCalls, DefaultToolTimeout: defaultToolTimeout,
	}
	return loop.Run(t.ctx, spec, t, t)
}

// session is the turn's session, fixed for the turn.
func (t *turn) session() session.Session {
	return t.s.sessionFor(t.chatID, t.outsideSandbox, t.networkThisTurn)
}

// chatOf is the turn's chat as stored: its cluster, the one its tools reach, since
// the send's cluster is only what a create files under.
func (s *service) chatOf(t *turn) (Chat, error) {
	c, ok, err := getChat(t.ctx, s.store.Stmts(), t.chatID)
	if err != nil {
		return Chat{}, err
	}
	if !ok {
		return Chat{}, ErrChatGone
	}
	return c, nil
}

// boxFor is what a run on target is offered: the tools its list names that the
// target takes and the user has not disabled, from the service's box. An
// always-on tool stays whatever its stored setting says. A model that takes no
// tools is offered none, so the prompt says so and Stream is never handed an
// offer it refuses. The service's box still reads every stored call.
func (s *service) boxFor(target llm.Target, disabled []string) tools.Box {
	disabled = slices.DeleteFunc(slices.Clone(disabled), s.lists.AlwaysOn)
	return s.tools.For(target, s.lists.ToolsFor(target)).WithoutNames(disabled)
}

// history is the chat's stored messages as the model is shown them: every message
// but the answer being written, and none with no blocks, which no wire accepts. An
// answer that failed before its first chunk is such a message, so two questions
// can follow each other; the APIs fold consecutive same-role messages into one. A
// row's tool blocks go only when it settled Complete, since only such a row holds
// a result for every call it asked. An answer names the provider and effort its
// run recorded: who may replay its payloads, and whether it thought.
func (s *service) history(t *turn) ([]llm.Message, error) {
	msgs, err := s.transcript(t.ctx, t.chatID)
	if err != nil {
		return nil, err
	}
	var out []llm.Message
	for _, m := range msgs {
		if m.ID == t.msg.ID {
			continue
		}
		blocks, err := unmarshalBlocks(m.Content)
		if err != nil {
			return nil, err
		}
		if m.Status != StatusComplete {
			blocks = llm.WithoutRounds(blocks)
		}
		if len(blocks) == 0 {
			continue
		}
		out = append(out, llm.Message{
			Role: string(m.Role), Blocks: blocks,
			ProviderID: m.ProviderID, Dialect: m.dialect, Effort: m.Effort,
		})
	}
	return out, nil
}

// LLMCallStarted opens the round's call row, under the run's own provider and
// effort, and on the turn's own run's first round claims the run too, one
// transaction, before the model is asked; a subagent's run is inserted running. A context already cancelled claims
// nothing, so the run settles cancelled from queued. The write itself is not
// cancellable: it lands or it fails.
func (j *runJournal) LLMCallStarted(ctx context.Context, model string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	call := &llmCallEntry{
		ID: newLLMCallID(), RunID: j.runID, Seq: len(j.llmCalls), ProviderID: j.target.Provider.ID, ModelID: model,
		Effort: j.target.Effort, StartedAt: normalizeTime(j.s.now()),
	}
	err := j.s.store.InTx(context.WithoutCancel(ctx), func(st stmts) error {
		if call.Seq == 0 && j.isTurns() {
			if err := claimRun(ctx, st, j.runID, call.StartedAt); err != nil {
				return err
			}
		}
		return insertLLMCall(ctx, st, *call)
	})
	if err != nil {
		return err
	}
	j.llmCalls = append(j.llmCalls, call)
	j.toolSeq = 0
	return nil
}

// LLMCallFirstChunk stamps the round's first text or thinking chunk.
func (j *runJournal) LLMCallFirstChunk() {
	call := j.llmCalls[len(j.llmCalls)-1]
	call.FirstChunkAt = normalizeTime(j.s.now())
}

// Progress is the answer so far: the live message's content, then a ping so the
// watchers re-read and overlay it. Every checkpointEvery it also writes that
// content to the row, so a crash keeps what the reader saw; the checkpoint
// notifies nothing, since the overlay is what readers see. One that fails is
// skipped: memory is the truth until the settle.
func (t *turn) Progress(blocks []llm.Block) {
	// The chunk's moment is read before a watcher can see it.
	now := t.s.now()
	content, citations := marshalBlocks(llm.WithoutPayloads(blocks)), marshalCitations(llm.Citations(t.msg.dialect, blocks))
	t.s.turnsMu.Lock()
	t.msg.Content, t.msg.Citations = content, citations
	t.s.turnsMu.Unlock()
	t.s.notify(messagesKey(t.chatID))

	if now.Sub(t.checkpointed) < t.s.checkpointEvery {
		return
	}
	t.checkpointed = now
	ctx := context.WithoutCancel(t.ctx)
	if err := t.s.store.InTx(ctx, func(st stmts) error { return writeContent(ctx, st, t.msg.ID, content) }); err != nil {
		slog.Warn("could not checkpoint a chat answer", "chat", t.chatID, "err", err)
	}
}

// LLMCallFinished closes the round's call row with its own outcome, never the turn's:
// a clean reply followed by a failed tool write stays a clean call row. The row
// names the model the provider said it served; a stream that broke off has no
// response, so its row keeps the one it asked for.
func (j *runJournal) LLMCallFinished(ctx context.Context, resp llm.Response, streamErr error) error {
	call := j.llmCalls[len(j.llmCalls)-1]
	call.Error = callError(streamErr)
	call.StopReason, call.ServerUses, call.FinishedAt = resp.StopReason, resp.ServerUses, normalizeTime(j.s.now())
	call.setUsage(resp.Usage)
	if resp.Model != "" {
		call.ModelID = resp.Model
	}
	ctx = context.WithoutCancel(ctx)
	return j.s.store.InTx(ctx, func(st stmts) error {
		if err := closeLLMCall(ctx, st, *call); err != nil {
			return err
		}
		for _, row := range j.toolCalls {
			if row.ByProvider && row.LLMCallID == call.ID {
				if err := upsertToolCall(ctx, st, *row); err != nil {
					return err
				}
			}
		}
		return nil
	})
}

// LLMCallFinished closes the round's row, then gives the live message the stop
// reason the stored read will serve: the run's latest. A subagent's run has no
// live message and uses the journal's alone.
func (t *turn) LLMCallFinished(ctx context.Context, resp llm.Response, streamErr error) error {
	err := t.runJournal.LLMCallFinished(ctx, resp, streamErr)
	if resp.StopReason != "" {
		t.s.turnsMu.Lock()
		t.msg.FinishReason = resp.StopReason
		t.s.turnsMu.Unlock()
	}
	return err
}

// ServerCallSeen keeps a call the provider ran as the round's next row, under
// its tool's name and contract, and publishes it. The app ran nothing, so the
// row has no status and no span. It lands with the round's finish write, and
// again at the settle.
func (j *runJournal) ServerCallSeen(call llm.Block, tool tools.Tool) {
	row := j.newToolCall(call)
	row.Name, row.Contract = tool.Name(), string(tool.ContractName())
	row.ByProvider = true
	j.toolCalls = append(j.toolCalls, row)
	j.toolSeq++
	j.publish("")
}

// ToolCallStarted commits the call's running row before the tool runs, so a command
// that touched the cluster is never absent from the record. A gated call's row is
// the one Approve opened; any other is minted here, with where and how the call
// runs off its approval, which a call that skipped the question still has. Either
// row takes the network the call runs with only now, so a call that asked and
// never ran keeps none. The row takes running and
// started_at only once the write has landed, so a refusal after a failed write
// still says the call never started.
func (j *runJournal) ToolCallStarted(ctx context.Context, call llm.Block, approval tools.Approval) error {
	row := j.openTool
	if row == nil {
		row = j.newToolCall(call)
		row.Cwd, row.Sandboxed = approval.Cwd, approval.Sandboxed
	}
	started := *row
	started.Status, started.StartedAt = toolRunning, nullMillis(normalizeTime(j.s.now()))
	started.Network = approval.Network
	if err := j.writeToolCall(ctx, &started); err != nil {
		return err
	}
	if j.openTool == nil {
		j.toolCalls = append(j.toolCalls, row)
	}
	*row = started
	j.openTool = row
	j.publish("")
	return nil
}

// ToolCallFinished writes the call's row whole: succeeded, or failed with its
// error, and result what the model read either way. A call refused without
// running gets its first row here, with no started_at: the record of a call that
// never ran.
func (j *runJournal) ToolCallFinished(ctx context.Context, call, result llm.Block, shown *tools.Action) error {
	row := j.openTool
	if row == nil {
		row = j.newToolCall(call)
		j.toolCalls = append(j.toolCalls, row)
	}
	j.openTool, j.toolSeq = nil, j.toolSeq+1
	row.Status, row.Result, row.Error = toolSucceeded, result.Text, ""
	if shown != nil {
		b, err := json.Marshal(shown)
		if err != nil { // coverage:ignore an Action is strings, bools and numbers, which always marshal
			return fmt.Errorf("tool call finished: %w", err)
		}
		row.Shown = string(b)
	}
	if result.IsError {
		row.Status, row.Error = toolFailed, toolError(result.Text)
		if code, _ := loop.RefusalOf(result.Text); row.IsMutating && code == loop.CodeDenied {
			row.Status = toolDenied
		}
	}
	row.FinishedAt = nullMillis(normalizeTime(j.s.now()))
	err := j.writeToolCall(ctx, row)
	j.publish("")
	return err
}

// publishLive rebuilds the live message's list from the turn's own calls, sets
// its status unless status is empty, and pings the watchers. The string is new
// each time, so a frame already sent keeps the list it was built with.
func (t *turn) publishLive(status MessageStatus) {
	calls := marshalToolCalls(derefToolCalls(t.toolCalls), t.s.tools)
	t.s.turnsMu.Lock()
	t.msg.ToolCalls = calls
	if status != "" {
		t.msg.Status = status
	}
	t.s.turnsMu.Unlock()
	t.s.notify(messagesKey(t.chatID))
}

// derefToolCalls is the rows as values, for marshalToolCalls.
func derefToolCalls(rows []*toolCallEntry) []toolCallEntry {
	out := make([]toolCallEntry, 0, len(rows))
	for _, r := range rows {
		out = append(out, *r)
	}
	return out
}

// toolError is a failed call's error column, a JSON object: the loop's refusal
// when the text is one (loop.RefusalOf), else toolErrorOwn, since the text itself
// is in result. A tool whose own error text is exactly one of the loop's refusals
// reads as it; bash cannot write one, as each of its error texts begins with an
// exit, timeout, could-not-start or code-unknown line.
func toolError(text string) string {
	if _, ok := loop.RefusalOf(text); ok {
		return text
	}
	return toolErrorOwn
}

// newToolCall is the row for a call of the current reply. The caller keeps it in
// toolCalls once it has landed: a row the store refused is written again by the
// refusal that follows, under the same seq, and two rows there would collide.
// Its contract is its tool's in the service's box, which reads every stored
// call, so a call the turn was not offered still records one. Every later write
// keeps it.
func (j *runJournal) newToolCall(call llm.Block) *toolCallEntry {
	return &toolCallEntry{
		ID: newToolCallID(), LLMCallID: j.llmCalls[len(j.llmCalls)-1].ID, Seq: j.toolSeq,
		Name: call.Name, Contract: string(j.s.tools.ContractOf(call.Name)), ToolUseID: call.ID, Arguments: string(call.Input),
		CreatedAt: normalizeTime(j.s.now()), AgentCallID: j.agentCallID,
	}
}

// writeToolCall lands the row as it stands. Not cancellable: it lands or it fails.
func (j *runJournal) writeToolCall(ctx context.Context, row *toolCallEntry) error {
	ctx = context.WithoutCancel(ctx)
	return j.s.store.InTx(ctx, func(st stmts) error { return upsertToolCall(ctx, st, *row) })
}

// Settled is the settle. A write that did not land is the retry's, so it
// reports nothing.
func (t *turn) Settled(_ context.Context, res loop.Result, err error) error {
	t.settle(res, err)
	return nil
}

// runOutcome is the status a stream error settles a run as, and the text stored
// with it: a cancel is cancelled and carries none, a provider that refused to read
// the chat or refused at a usage limit is failed in the transcript's own words, and
// a clean stream succeeded.
func runOutcome(streamErr error) (RunStatus, string) {
	switch {
	case streamErr == nil:
		return RunSucceeded, ""
	case errors.Is(streamErr, context.Canceled), errors.Is(streamErr, context.DeadlineExceeded):
		return RunCancelled, ""
	}
	if pe, ok := errors.AsType[*llm.Error](streamErr); ok {
		switch {
		case pe.ContextFull():
			return RunFailed, contextFullText
		case pe.UsageLimit():
			return RunFailed, usageLimitText
		}
	}
	return RunFailed, streamErr.Error()
}

// callError is a model call's error as its row keeps it: empty for a clean
// stream, cancelled for a cancel, else the stream error's own text. Unlike the
// run's, a context overflow keeps the provider's words; an llm.Error's text never
// holds a response body.
func callError(streamErr error) string {
	switch status, _ := runOutcome(streamErr); status {
	case RunSucceeded:
		return ""
	case RunCancelled:
		return callCancelled
	}
	return streamErr.Error()
}

// settle fixes the turn's outcome and makes the first attempt to write it. A
// cancelled turn is cancelled, any other error is failed with its text, a clean
// stream succeeded. The content is the result's blocks, else the text so far, so
// a turn that broke off keeps what the reader saw. Before the attempt the live
// message shows the answer as it will be stored, still streaming, since the
// stored run is until the row lands.
func (t *turn) settle(res loop.Result, streamErr error) {
	t.settled = true
	if streamErr != nil && t.ctx.Err() != nil {
		// A cancel can reach a store read as the driver's own error
		// ("interrupted (9)"), not as the context's.
		streamErr = t.ctx.Err()
	}
	t.status, t.errText = runOutcome(streamErr)
	t.s.turnsMu.Lock()
	t.answer = t.msg.Content
	t.s.turnsMu.Unlock()
	if len(res.Blocks) > 0 {
		t.answer = marshalBlocks(res.Blocks)
	}
	t.at = normalizeTime(t.s.now())
	t.closeOpen(res.StopReason, streamErr, t.at)
	content, citations := contentForReader(t.answer, t.msg.dialect)
	t.s.turnsMu.Lock()
	t.msg.Content, t.msg.Citations = content, citations
	t.s.turnsMu.Unlock()
	t.publishLive(StatusStreaming)
	t.settleAttempt()
}

// settleAttempt writes the outcome settle fixed, once, on a context without the
// turn's cancel, so a cancelled turn still lands. A retry logs its two ends
// alone, so a store that stays broken is two lines: the first failure, which
// closes retrying, and the landing.
func (t *turn) settleAttempt() {
	t.attempts++
	if err := t.s.settleWrite(context.WithoutCancel(t.ctx), t); err != nil {
		if t.attempts == 1 {
			slog.Warn("chat answer did not settle; retrying", "chat", t.chatID, "err", err)
			close(t.retrying)
		}
		return
	}
	t.landed = true
	t.succeeded = t.status == RunSucceeded
	if t.attempts > 1 {
		slog.Info("chat answer settled after retrying", "chat", t.chatID, "attempts", t.attempts)
	}
}

// settleUntilLanded retries the settle until it lands, the service stops, or a
// delete has taken the chat, each wait twice the last up to maxBackoffSteps
// times the first. A stop leaves the row streaming, and the next Start fails it
// as stranded. The slot is held
// meanwhile, so a reader keeps the answer and a send is refused rather than
// started on rows still being written.
func (s *service) settleUntilLanded(t *turn) {
	wait := s.writeBackoff
	for !t.landed {
		select {
		case <-time.After(wait):
		case <-s.ctx.Done():
			return
		case <-t.gone:
			return
		}
		t.settleAttempt()
		wait = min(2*wait, maxBackoffSteps*s.writeBackoff)
	}
}

// settleRow is the settle's one transaction: the content, the run's terminal
// status, every call row of the turn and each approval written whole again
// (writeCalls), and the chat's recency. A subagent's rows are its own
// task's to write. The rows go after the run's status, so a write a trigger
// refused while the run was live is healed here.
func (s *service) settleRow(ctx context.Context, t *turn) error {
	return s.store.InTx(ctx, func(st stmts) error {
		if err := writeContent(ctx, st, t.msg.ID, t.answer); err != nil {
			return err
		}
		if err := settleRun(ctx, st, t.runID, t.status, "", t.errText, t.at); err != nil {
			return err
		}
		if err := t.writeCalls(ctx, st); err != nil {
			return err
		}
		return touchChat(ctx, st, t.chatID, t.at)
	})
}

// closeOpen closes in memory what the recorder never closed, which only a panic
// inside Run leaves: a model call takes stopReason and the stream's error, and a
// tool call fails interrupted.
func (j *runJournal) closeOpen(stopReason string, streamErr error, at time.Time) {
	for _, c := range j.llmCalls {
		if c.FinishedAt.IsZero() {
			c.StopReason, c.Error, c.FinishedAt = stopReason, callError(streamErr), at
		}
	}
	for _, c := range j.toolCalls {
		if !c.ByProvider && !c.FinishedAt.Valid {
			c.Status, c.Error, c.FinishedAt = toolFailed, toolCallInterrupted, nullMillis(at)
		}
	}
}

// writeCalls writes every call row of the run whole again, as it stands, and
// each approval.
func (j *runJournal) writeCalls(ctx context.Context, st stmts) error {
	for _, c := range j.llmCalls {
		if err := closeLLMCall(ctx, st, *c); err != nil {
			return err
		}
	}
	for _, c := range j.toolCalls {
		if err := upsertToolCall(ctx, st, *c); err != nil {
			return err
		}
		if c.Approval != nil {
			if err := upsertApproval(ctx, st, *c.Approval); err != nil {
				return err
			}
		}
		for _, w := range c.SandboxActions {
			if err := upsertApproval(ctx, st, *w); err != nil {
				return err
			}
		}
	}
	return nil
}

// overlay swaps each in-flight answer's live message in for its stored row. Rows
// first, then the mutex only to copy. The live list is the turn's own entries,
// which know nothing of a task's row or of its subagents' calls, so the stored
// list gives it both (withStoredCalls). The live message knows only its own run's
// wait, so the stored row's AwaitingApproval is kept beside it.
func (s *service) overlay(msgs []ChatMessage) {
	type stored struct {
		i     int
		calls rawjson.RawJSON
	}
	var merge []stored
	s.turnsMu.Lock()
	for i, m := range msgs {
		if t, ok := s.turns[m.ChatID]; ok && t.msg.ID == m.ID {
			msgs[i] = t.msg
			msgs[i].AwaitingApproval = t.msg.Status == StatusWaitingApproval || m.AwaitingApproval
			if m.hasStored {
				merge = append(merge, stored{i, m.ToolCalls})
			}
		}
	}
	s.turnsMu.Unlock()
	for _, w := range merge {
		msgs[w.i].ToolCalls = withStoredCalls(msgs[w.i].ToolCalls, w.calls)
		msgs[w.i].hasStored = true
	}
}

// withStoredCalls is the live list with each call's task state taken from the
// stored list by call id, then the stored calls that ran under an Agent call: the
// store is the one source of a subagent's calls, whichever message is live. A
// list that does not parse is left as it is.
func withStoredCalls(live, stored rawjson.RawJSON) rawjson.RawJSON {
	var liveCalls, storedCalls []ToolCall
	if json.Unmarshal([]byte(live), &liveCalls) != nil || json.Unmarshal([]byte(stored), &storedCalls) != nil {
		return live
	}
	tasks := map[ToolCallID]*BackgroundTask{}
	for _, c := range storedCalls {
		tasks[c.ID] = c.Background
	}
	for i := range liveCalls {
		liveCalls[i].Background = tasks[liveCalls[i].ID]
	}
	for _, c := range storedCalls {
		if c.AgentCallID != nil {
			liveCalls = append(liveCalls, c)
		}
	}
	b, err := json.Marshal(liveCalls)
	if err != nil { // coverage:ignore both lists were decoded from JSON, which always marshals back
		return live
	}
	return rawjson.RawJSON(b)
}

// readMessages is a chat's transcript as it is now: the stored rows as a reader
// is shown them. The watch and the query read through here; history reads
// listMessages directly and keeps the payloads for the replay.
func (s *service) readMessages(ctx context.Context, chatID ChatID) ([]ChatMessage, error) {
	msgs, err := s.transcript(ctx, chatID)
	if err != nil {
		return nil, err
	}
	s.forReader(msgs)
	return msgs, nil
}

// transcript is listMessages on the pools, in one read transaction so the messages
// and their tool calls cannot tear. A transaction that could not run is named as
// the read it was for; listMessages names its own failures.
func (s *service) transcript(ctx context.Context, chatID ChatID) ([]ChatMessage, error) {
	var (
		msgs    []ChatMessage
		readErr error
	)
	err := s.store.InReadTx(ctx, func(st stmts) error {
		msgs, readErr = listMessages(ctx, st, chatID, s.tools)
		return readErr
	})
	if readErr != nil {
		return nil, readErr
	}
	if err != nil {
		return nil, fmt.Errorf("list messages: %w", err)
	}
	return msgs, nil
}

// forReader makes stored rows what a reader is shown: without their payloads,
// overlaid with any answer in flight. Every row that leaves the service passes
// through here.
func (s *service) forReader(msgs []ChatMessage) {
	for i := range msgs {
		msgs[i].Content, msgs[i].Citations = contentForReader(msgs[i].Content, msgs[i].dialect)
	}
	s.overlay(msgs)
}

// contentForReader is a row's content as a reader is shown it: without the
// payloads only the provider that wrote them reads, and the citations d reads
// from them first. A row holding none, every row but a signed or cited answer,
// comes back as it stands, since a stream re-reads the transcript on every tick
// and the watch diffs content byte for byte. A row that does not parse is left
// alone: a reader is owed its content.
func contentForReader(content rawjson.RawJSON, d llm.Dialect) (rawjson.RawJSON, rawjson.RawJSON) {
	if !strings.Contains(string(content), `"payload"`) {
		return content, emptyCitations
	}
	blocks, err := unmarshalBlocks(content)
	if err != nil {
		return content, emptyCitations
	}
	return marshalBlocks(llm.WithoutPayloads(blocks)), marshalCitations(llm.Citations(d, blocks))
}
