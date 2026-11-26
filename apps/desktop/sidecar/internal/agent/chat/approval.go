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

// The gate: a gated call's rows, the wait on the user's decision, and the
// delivery of that decision from approvalDecide to the turn that waits on it.
package chat

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/llm"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/run/permissions"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/tools"
)

// Approve is loop.Approver over the run's rows. The waiter is registered before
// anything is written or shown, so a decision cannot arrive at a request nobody
// waits on and no id is decidable ahead of its request. The call's row, its
// approval and the run's flip to waiting_approval land in one transaction; then
// the request is published, and the wait ends on the decision or the run's
// cancel. Only this run flips: a subagent's wait leaves its parent's alone. A
// cancel that wins leaves the approval pending, the record of a question nobody
// answered. Once the decision has committed it stands, and the loop decides what
// a cancel after it means.
//
// A run with an unansweredLimit, a subagent's, is bounded too: past it the agent
// is stopped, as the user's Stop would, and the wait ends on that stop's cancel.
func (j *runJournal) Approve(ctx context.Context, call llm.Block, shown tools.Approval) (bool, error) {
	s := j.s
	w := s.await(newApprovalID(), j.chatID, nil)
	row := j.newToolCall(call)
	row.Status, row.IsMutating, row.Cwd, row.Sandboxed = toolAwaitingApproval, true, shown.Cwd, shown.Sandboxed
	if len(shown.Env) > 0 {
		env, _ := json.Marshal(shown.Env) // coverage:ignore a []string always marshals
		row.Env = string(env)
	}
	a := &approval{ID: w.id, ToolCallID: row.ID, Status: ApprovalPending, CreatedAt: row.CreatedAt}
	if err := j.writeWaiting(ctx, row, *a); err != nil {
		s.forget(w)
		return false, err
	}
	row.Approval = a
	j.toolCalls, j.openTool = append(j.toolCalls, row), row
	j.publish(StatusWaitingApproval)
	s.notify(chatsKey)

	d, err := j.waitDecision(ctx, w)
	if err != nil {
		return false, err
	}
	if err := j.endApproval(ctx, a, d); err != nil {
		return false, err
	}
	return d.status == ApprovalApproved, nil
}

// writeWaiting writes a pending, with row when it is a call's own first row,
// and flips the run to waiting_approval, in one transaction on ctx without its
// cancel.
func (j *runJournal) writeWaiting(ctx context.Context, row *toolCallEntry, a approval) error {
	wctx := context.WithoutCancel(ctx)
	return j.s.store.InTx(wctx, func(st stmts) error {
		if row != nil {
			if err := upsertToolCall(wctx, st, *row); err != nil {
				return err
			}
		}
		if err := upsertApproval(wctx, st, a); err != nil {
			return err
		}
		return flipRun(wctx, st, j.runID, RunWaitingApproval)
	})
}

// waitDecision waits for the user's answer to w, or the error that ended the
// wait first, and stops waiting either way. Past the run's unansweredLimit the
// agent is stopped, as the user's Stop would, and the wait ends on that stop's
// cancel.
func (j *runJournal) waitDecision(ctx context.Context, w *waiter) (decision, error) {
	s := j.s
	defer s.forget(w)
	var unanswered <-chan time.Time
	if j.unansweredLimit > 0 {
		timer := time.NewTimer(j.unansweredLimit)
		defer timer.Stop()
		unanswered = timer.C
	}
	select {
	case d := <-w.decided:
		return d, nil
	case <-ctx.Done():
		return decision{}, ctx.Err()
	case <-unanswered:
		s.forget(w)
		if s.stopTaskOf(j.agentCallID, stoppedByUnanswered) {
			<-ctx.Done()
		}
		return decision{}, context.Canceled
	}
}

// endApproval writes a's end as d and flips the run back to running, in one
// transaction on ctx without its cancel, then publishes. a takes the end only
// once it has landed.
func (j *runJournal) endApproval(ctx context.Context, a *approval, d decision) error {
	ended := *a
	ended.Status, ended.Duration, ended.DecidedAt = d.status, d.duration, nullMillis(normalizeTime(j.s.now()))
	wctx := context.WithoutCancel(ctx)
	err := j.s.store.InTx(wctx, func(st stmts) error {
		if err := upsertApproval(wctx, st, ended); err != nil {
			return err
		}
		return flipRun(wctx, st, j.runID, RunRunning)
	})
	if err != nil {
		return err
	}
	*a = ended
	j.publish(StatusStreaming)
	j.s.notify(chatsKey)
	return nil
}

// actionAsker is a run's journal as the tools.ActionAsker its calls get, bound
// to the run's context, so a wait ends with the run as well as with the
// request.
type actionAsker struct {
	j   *runJournal
	run context.Context
}

func (a actionAsker) Ask(ctx context.Context, r tools.ActionRequest) (tools.Answer, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	defer context.AfterFunc(a.run, cancel)()
	return a.j.askAction(ctx, r)
}

func (a actionAsker) Record(ctx context.Context, r tools.ActionRequest, d permissions.Decision, reason string) error {
	return a.j.recordAction(ctx, r, d, reason)
}

// recordAction records an action of a sandboxed command the policy decided
// with nobody asked, against the call the run has open, as askAction records
// one the user decided: written already decided, so nothing waits and the run
// stays running.
func (j *runJournal) recordAction(ctx context.Context, r tools.ActionRequest, d permissions.Decision, reason string) error {
	s := j.s
	var status ApprovalStatus
	switch d {
	case permissions.Allowed:
		status = ApprovalAllowed
	case permissions.Denied:
		status = ApprovalRefused
	default:
		return errNotRecorded
	}
	j.journalMu.Lock()
	defer j.journalMu.Unlock()
	call := j.openTool
	if call == nil {
		return errNoRunningCall
	}
	now := normalizeTime(s.now())
	a := &approval{
		ID: newApprovalID(), ToolCallID: call.ID, Status: status, CreatedAt: now,
		DecidedAt: nullMillis(now), Request: &r, Reason: reason,
	}
	wctx := context.WithoutCancel(ctx)
	if err := s.store.InTx(wctx, func(st stmts) error { return upsertApproval(wctx, st, *a) }); err != nil {
		return err
	}
	call.SandboxActions = append(call.SandboxActions, a)
	if j.asking {
		j.publish(StatusWaitingApproval)
	} else {
		j.publish(StatusStreaming)
	}
	return nil
}

// askAction puts an action of a sandboxed command to the user as a request of
// the call the run has open: calls run one at a time, so it is the one whose
// command sent it. Unlike Approve, every end of the wait writes the approval
// and flips the run back, since the command runs on after it and the run must
// not stay waiting behind it: a wait that ends without a decision writes
// abandoned. The call's row keeps running throughout. An end the store refuses
// answers the command with that error, never the decision: no change goes on a
// decision the record does not hold.
//
// It runs on a proxy's goroutine while the loop waits in the call's Run, which
// returns only once every request it sent has returned (kubeproxy's Wait), so the
// loop and the proxies never touch the journal at once; journalMu keeps the
// proxies' requests apart, and askMu keeps one ask before the user at a time.
func (j *runJournal) askAction(ctx context.Context, r tools.ActionRequest) (tools.Answer, error) {
	j.askMu.Lock()
	defer j.askMu.Unlock()
	a, w, err := j.openAction(ctx, r)
	if err != nil {
		return tools.Answer{}, err
	}
	d, waitErr := j.waitDecision(ctx, w)
	if waitErr != nil {
		d = decision{status: ApprovalAbandoned}
	}
	if err := j.closeAction(ctx, a, d); err != nil {
		return tools.Answer{}, err
	}
	return tools.Answer{Approved: d.status == ApprovalApproved, Duration: d.duration}, waitErr
}

// openAction writes r pending under the open call and publishes the request,
// under journalMu, and answers the approval and the waiter its decision
// arrives on.
func (j *runJournal) openAction(ctx context.Context, r tools.ActionRequest) (*approval, *waiter, error) {
	s := j.s
	j.journalMu.Lock()
	defer j.journalMu.Unlock()
	call := j.openTool
	if call == nil {
		return nil, nil, errNoRunningCall
	}
	w := s.await(newApprovalID(), j.chatID, &r)
	a := &approval{ID: w.id, ToolCallID: call.ID, Status: ApprovalPending, CreatedAt: normalizeTime(s.now()), Request: &r}
	if err := j.writeWaiting(ctx, nil, *a); err != nil {
		s.forget(w)
		return nil, nil, err
	}
	call.SandboxActions = append(call.SandboxActions, a)
	j.asking = true
	j.publish(StatusWaitingApproval)
	s.notify(chatsKey)
	return a, w, nil
}

// closeAction writes a's end as d under journalMu. An end the store refuses
// still comes down, since the command runs on: the journal takes it, and the
// settle writes it whole.
func (j *runJournal) closeAction(ctx context.Context, a *approval, d decision) error {
	j.journalMu.Lock()
	defer j.journalMu.Unlock()
	j.asking = false
	err := j.endApproval(ctx, a, d)
	if err != nil {
		a.Status, a.Duration, a.DecidedAt = d.status, d.duration, nullMillis(normalizeTime(j.s.now()))
		j.publish(StatusStreaming)
		j.s.notify(chatsKey)
	}
	return err
}

// errNoRunningCall is an action asked while no call of the run is running.
var errNoRunningCall = errors.New("chat: an action with no call running")

// errNotRecorded is a record of a decision that is not Allowed or Denied: a
// prompt is asked, never recorded.
var errNotRecorded = errors.New("chat: only an allowed or denied action is recorded")

// ApprovalDecision is the user's answer to a request: Once and Deny answer it
// alone; Command also allows the same change for the rest of the command; Chat
// and Always also write an Allow rule for the action's access level and scope.
type ApprovalDecision string

const (
	DecisionOnce    ApprovalDecision = "once"
	DecisionCommand ApprovalDecision = "command"
	DecisionChat    ApprovalDecision = "chat"
	DecisionAlways  ApprovalDecision = "always"
	DecisionDeny    ApprovalDecision = "deny"
)

// decision is how a wait ended: the status the approval records, and for an
// approval how long it holds.
type decision struct {
	status   ApprovalStatus
	duration permissions.Duration
}

// durationOf is how long each approving answer holds.
var durationOf = map[ApprovalDecision]permissions.Duration{
	DecisionOnce:    permissions.DurationOnce,
	DecisionCommand: permissions.DurationCommand,
	DecisionChat:    permissions.DurationChat,
	DecisionAlways:  permissions.DurationAlways,
}

// waiter is one request a turn waits on: its channel of one, buffered, so a
// decision delivered before the turn reaches its select is kept for it; the
// chat it is in; and the action it asks about, nil for a call's own.
// forgotten is set once the turn stops waiting, so a decision claimed
// meanwhile never puts it back.
type waiter struct {
	id        ApprovalID
	decided   chan decision
	chatID    ChatID
	request   *tools.ActionRequest
	forgotten bool
}

// decide is the decision d is on w, or ErrBadRequest for one w does not take:
// a call's own, and an action no rule may allow, take Once and Deny alone. A
// grantable action takes every duration, whatever its target.
func (w *waiter) decide(d ApprovalDecision) (decision, error) {
	grantable := w.request != nil && w.request.Grantable
	switch d {
	case DecisionDeny:
		return decision{status: ApprovalDenied}, nil
	case DecisionOnce:
	case DecisionCommand, DecisionChat, DecisionAlways:
		if !grantable {
			return decision{}, ErrBadRequest
		}
	default:
		return decision{}, ErrBadRequest
	}
	return decision{status: ApprovalApproved, duration: durationOf[d]}, nil
}

// await registers a waiter for id.
func (s *service) await(id ApprovalID, chatID ChatID, request *tools.ActionRequest) *waiter {
	w := &waiter{id: id, decided: make(chan decision, 1), chatID: chatID, request: request}
	s.turnsMu.Lock()
	s.pending[id] = w
	s.turnsMu.Unlock()
	return w
}

// forget is the one way a turn stops waiting on w: its request was never
// shown, or its wait ended. It takes w out if it is still there, and marks it
// forgotten either way.
func (s *service) forget(w *waiter) {
	s.turnsMu.Lock()
	defer s.turnsMu.Unlock()
	if s.pending[w.id] == w {
		delete(s.pending, w.id)
	}
	w.forgotten = true
}

// Approve delivers d to the turn waiting on id, having claimed the waiter
// first, so each approval has one writer and a second answer finds none.
// false is an id nothing waits on: already decided, cancelled, or stranded
// across a restart. true says the decision reached a waiting turn, not that
// it was recorded: a cancel ready in the same moment can still win the turn's
// select. A decision the waiter does not take is ErrBadRequest, and the
// waiter stays. A Chat or Always answer writes its rule once the waiter is
// claimed; a write that fails puts the waiter back unless its turn stopped
// waiting meanwhile, and answers the error, so the user can answer again.
func (s *service) Approve(ctx context.Context, id ApprovalID, d ApprovalDecision) (bool, error) {
	s.turnsMu.Lock()
	w, ok := s.pending[id]
	if !ok {
		s.turnsMu.Unlock()
		return false, nil
	}
	out, err := w.decide(d)
	if err != nil {
		s.turnsMu.Unlock()
		return false, err
	}
	delete(s.pending, id)
	s.turnsMu.Unlock()

	// The rule lands before the decision, so the command's next write finds it,
	// and never under turnsMu, which no file or database write may hold. A
	// Command answer writes none: the proxy keeps the command's rule.
	if d == DecisionChat || d == DecisionAlways {
		if err := s.ruleWrite(ctx, w.chatID, permissions.GrantRule(w.request.Action), d); err != nil {
			s.turnsMu.Lock()
			if !w.forgotten {
				s.pending[id] = w
			}
			s.turnsMu.Unlock()
			return false, err
		}
	}
	w.decided <- out // buffered, and the entry is gone: this send is the only one
	return true, nil
}

// writeRule writes rule as chatID's for Chat, or as an always rule for Always,
// unless the same rule is held there already.
func (s *service) writeRule(ctx context.Context, chatID ChatID, rule permissions.Rule, d ApprovalDecision) error {
	if d == DecisionChat {
		_, err := s.addRule(ctx, chatID, rule)
		return err
	}
	_, err := s.settings.AddRule(ctx, rule)
	return err
}
