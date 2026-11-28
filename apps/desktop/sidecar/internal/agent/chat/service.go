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
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/amorey/gobus/conflate"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/appdb"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/apimeta"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/drain"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/lifecycle"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/rootdir"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/sqlstmt"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/version"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/llm"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/run/permissions"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/run/sandbox"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/run/session"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/settings"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/tools"
)

// The named errors. The GraphQL layer maps each to a stable code.
// contextFullText is a failed answer's error when the provider refused to read the
// chat, in the one spelling the transcript draws for it.
const contextFullText = "chat is longer than the model can read"

// usageLimitText is a failed answer's error when the provider refused the request
// at a plan's usage limit.
const usageLimitText = "usage limit reached"

var (
	// ErrBadRequest is a request key or a deleted chat's id that is not a UUID,
	// an empty message, a mode that is neither constant, a model the llm service
	// does not hold or an effort it does not list, or a title that is empty or
	// too long.
	ErrBadRequest = errors.New("chat: bad request")
	// ErrTurnInFlight is a send into a chat whose turn is still running.
	ErrTurnInFlight = errors.New("chat: a turn is already in flight")
	// ErrChatGone is a send or rename naming a chat that does not exist.
	ErrChatGone = errors.New("chat: chat not found")
	// ErrClusterGone is a send into a cluster that does not exist or is marked for
	// deletion.
	ErrClusterGone = errors.New("chat: cluster not found")
	// ErrStopping is work arriving after stop began.
	ErrStopping = errors.New("chat: service is stopping")
	// ErrChatContextFull is a send into a chat longer than the model it named can
	// read.
	ErrChatContextFull = errors.New("chat: " + contextFullText)
	// ErrChatSandboxChanged is a send whose sender saw the chat's switch the
	// other way.
	ErrChatSandboxChanged = errors.New("chat: the chat's sandbox switch changed")
	// ErrChatNetworkChanged is a send whose sender saw the chat's network switch
	// the other way.
	ErrChatNetworkChanged = errors.New("chat: the chat's network switch changed")
	// ErrNoSandbox is a monitor run on a machine where no sandbox confines one.
	ErrNoSandbox = errors.New("chat: no sandbox confines a monitor run")
	// ErrMonitoringOff is a monitor run its caller's admit refused.
	ErrMonitoringOff = errors.New("chat: the cluster is not watched")
	// ErrMonitorInFlight is a monitor run on a cluster whose run is still going.
	ErrMonitorInFlight = errors.New("chat: a monitor run is already in flight")
	// ErrServerChanged is a monitor run on a cluster whose context reaches another
	// server than the one the run is for.
	ErrServerChanged = errors.New("chat: " + serverChangedText)
)

const (
	// sweepRetry paces the chat sweeper after a sweep that failed: no signal follows
	// a failure, so the retry is the sweeper's own.
	sweepRetry = 5 * time.Second
	// writeBackoff is a settle's first wait before it tries again, and
	// maxBackoffSteps how far the doubling goes: a store that stays broken is
	// tried every 4s.
	writeBackoff    = 250 * time.Millisecond
	maxBackoffSteps = 16
	// checkpointEvery paces the writes of an answer's live content to its row,
	// so a crash keeps what the reader saw.
	checkpointEvery = 3 * time.Second
	// strandedReason fails a run a previous process left unfinished.
	strandedReason = "the sidecar stopped before this answer finished"
	// maxTitleLen caps a title, in characters: the one a send derives is cut to
	// it, and a rename past it is refused.
	maxTitleLen = 80
	// lineBreaks are Unicode's mandatory line breaks. A title is one line: a send's
	// is cut at the first, and a rename holding one is refused.
	lineBreaks = "\n\v\f\r\u0085\u2028\u2029"
	// clusterCardTimeout bounds the cluster card's reads on a send: every read is a
	// local file, so a card that takes longer is unavailable rather than a slower send.
	clusterCardTimeout = 2 * time.Second
	// MaxToolCalls is what one turn may ask for, counted per call: enough for a
	// handful of reads and a synthesis, few enough that a loop cannot run away.
	MaxToolCalls = 8
	// MaxSubagentToolCalls is what one subagent may ask for: more than a turn's,
	// since a subagent exists to spend calls whose output the parent would rather
	// not keep. The Agent tool's section states it.
	MaxSubagentToolCalls = 16
	// defaultToolTimeout bounds one call of a tool with no bound of its own: the time a
	// cache read takes.
	defaultToolTimeout = 2 * time.Second
)

// The change-bus keys this service notifies and watches, spelled by appdb.
const chatsKey = appdb.KeyChats

func messagesKey(id ChatID) string { return appdb.MessagesKey(string(id)) }

func grantsKey(id ChatID) string { return appdb.GrantsKey(string(id)) }

// GrantChangeNotifications pings after each write or revoke of a chat's rules commits,
// coalesced. The key is the whole news: re-read on each. Close it when done.
type GrantChangeNotifications = *conflate.Receiver[string, struct{}]

// ClusterCards renders the cluster card: what the model is told about a chat's
// cluster, the one section of a question's context block today. The text is what
// goes on the wire: equal text on consecutive questions is one card.
type ClusterCards interface {
	ClusterCard(ctx context.Context, clusterID apimeta.ClusterID) string
	// ClusterCardAt is the card read at serverUID's server, and false when the
	// cluster's record names another.
	ClusterCardAt(ctx context.Context, clusterID apimeta.ClusterID, serverUID string) (string, bool)
}

// ToolLists is the tools a turn on each target is offered, by name, and which
// of them no setting turns off.
type ToolLists interface {
	ToolsFor(t llm.Target) []string
	AlwaysOn(name string) bool
}

// SendSwitches is where a send's commands run. SandboxDisabled and
// NetworkEnabled are the chat's switches as the sender saw them: a created
// chat's sandbox default and no network. NetworkThisTurn gives this turn's
// commands the internet and SandboxDisabledThisTurn runs them outside the
// sandbox, the two never together.
type SendSwitches struct {
	SandboxDisabled         bool
	NetworkEnabled          bool
	NetworkThisTurn         bool
	SandboxDisabledThisTurn bool
}

// Service is what the GraphQL layer calls.
type Service interface {
	lifecycle.StartCloser

	// Send saves a user message and starts the assistant's turn, returning the
	// assistant's empty message once the turn is accepted. A nil chatID creates
	// the chat, in mode and under clusterID; an existing chat keeps the mode and
	// cluster it was made with. model and effort are what this turn runs on.
	// requestID is the client's key for this send, a UUID it minted; a repeat
	// returns the first attempt's message and starts nothing. snap is what the
	// user's open log view showed, nil with none; a replay ignores it.
	Send(ctx context.Context, chatID *ChatID, mode Mode, clusterID apimeta.ClusterID, switches SendSwitches, providerID, modelID, effort, requestID, content string, snap *Snapshot) (ChatMessage, error)
	// Cancel stops the in-flight turn, keeping the partial answer. A no-op when
	// nothing is running.
	Cancel(ctx context.Context, chatID ChatID) error
	// Rename retitles a chat. The title is trimmed, and one empty, over
	// maxTitleLen or holding a line break is refused.
	Rename(ctx context.Context, chatID ChatID, title string) (Chat, error)
	// SetSandboxDisabled is the user's switch: the chat's commands from its next
	// turn run outside the sandbox, or back in it. Leaving the sandbox turns
	// the network switch off. It is not activity, so UpdatedAt stays.
	// ErrBadRequest on a machine with no sandbox.
	SetSandboxDisabled(ctx context.Context, chatID ChatID, disabled bool) (Chat, error)
	// SetNetworkEnabled is the user's network switch: the chat's sandboxed
	// commands reach the internet from their next start, a running turn's
	// included. It is not activity, so UpdatedAt stays. ErrBadRequest turning
	// it on where network is unavailable or while the chat is outside the
	// sandbox.
	SetNetworkEnabled(ctx context.Context, chatID ChatID, enabled bool) (Chat, error)
	// Delete removes a chat, its messages and its directory. Deleting one already gone
	// is not an error; an id that is not a UUID is ErrBadRequest.
	Delete(ctx context.Context, chatID ChatID) error

	Get(ctx context.Context, chatID ChatID) (Chat, bool, error)
	// List is every chat, newest activity first.
	List(ctx context.Context) ([]Chat, error)

	WatchCollection(ctx context.Context) (*Stream[ChatCollectionWatchFrame], error)
	WatchMessageCollection(ctx context.Context, chatID ChatID) (*Stream[ChatMessageCollectionWatchFrame], error)

	// Approve decides a command a turn is waiting on. true when a turn was
	// waiting on it; a decision nothing waits on returns false and changes nothing.
	Approve(ctx context.Context, id ApprovalID, d ApprovalDecision) (bool, error)
	// StopBackgroundTask is the user's stop of the background task call id
	// started. false when no task of that call is running.
	StopBackgroundTask(ctx context.Context, id ToolCallID) (bool, error)

	// FoldersFor is the folders a run of chatID reads, the chat's grants then
	// the always ones, each checked; with no chat, the always ones alone. None
	// on a machine where no sandbox confines a run. A read that fails is an
	// error.
	FoldersFor(ctx context.Context, chatID ChatID) ([]session.Folder, error)
	// FolderGrants is the always grants and, with a chat, the chat's, each with
	// why it fails its check now. Empty on a machine with no sandbox.
	FolderGrants(ctx context.Context, chatID ChatID) (always, chat []FolderGrant, err error)
	// GrantFolder grants path, read or with write read and write, for the chat
	// or with no chat always, once it passes its check, a
	// settings.FolderRefusal otherwise. A grant of a folder already
	// granted in the same place changes that grant's mode and keeps its id.
	GrantFolder(ctx context.Context, chatID ChatID, path string, write bool) error
	// RevokeFolder removes the folder grant id: the chat's, or with no chat an
	// always one. ErrGrantGone for an id that is not one.
	RevokeFolder(ctx context.Context, chatID ChatID, id string) error
	// ChatRules is the chat's own rules; none for a chat with none.
	ChatRules(ctx context.Context, chatID ChatID) ([]permissions.Rule, error)
	// RemoveChatRule removes one of the chat's rules by id and answers the
	// rules left: ErrChatGone or ErrGrantGone for a chat or an id
	// gone.
	RemoveChatRule(ctx context.Context, chatID ChatID, id string) ([]permissions.Rule, error)
	// ListenForGrantChanges notifies of each change to the chat's rules and folder
	// grants. Listen before the first read.
	ListenForGrantChanges(chatID ChatID) GrantChangeNotifications

	// RunMonitor takes one monitor run on the cluster, reading the server
	// serverUID names and no other: a run of target over brief in the monitor's
	// session, settled when it returns, if admit admits it inside the run's insert.
	RunMonitor(ctx context.Context, clusterID apimeta.ClusterID, serverUID string, target llm.Target, brief string, admit Admit) (MonitorResult, error)
}

var _ Service = (*service)(nil)

type service struct {
	db           *appdb.DB
	store        *sqlstmt.Set[stmtID]
	llmSvc       *llm.Service
	clusterCards ClusterCards
	memories     Memories
	lists        ToolLists
	// sandboxState is whether sandboxed Bash is offered, which the switch needs.
	sandboxState sandbox.State
	// settings holds the modes, the always rules and the folders granted always
	// each session decides by, and the zones a folder is checked against.
	settings *settings.Service
	// cloudTargets is the cloud targets this build has a proxy for, which the
	// Sandbox section names when their switch is on.
	cloudTargets []permissions.Target
	// tools is every tool the app knows, in offer order, and the readers of those
	// this machine cannot offer: each turn is offered what its list names and its
	// target takes, and every stored call is read through it, whether or not a
	// turn offers its tool.
	tools tools.Box
	// chatsRoot is the root every chat's directory is under, and monitorRoot
	// every cluster's monitor directory: held open so no link a command swaps
	// in leads out of either.
	chatsRoot   *os.Root
	monitorRoot *os.Root

	// turns is the one in-flight turn per chat, under turnsMu. The mutex is never
	// held across a database call: the writer has one connection, and a holder
	// that then needed it would deadlock the service.
	turnsMu sync.Mutex
	turns   map[ChatID]*turn
	// deleting counts the deletes of each chat in progress, under turnsMu too. A chat
	// being deleted takes no new turn: the delete joins the one it found, and a turn
	// reserved after that would outlive the rows it writes to.
	deleting map[ChatID]int
	// pending is the waiter of each command put to the user, under turnsMu too.
	pending map[ApprovalID]*waiter
	// ruleWrite writes the rule a Chat or Always answer adds: writeRule, or a
	// test's stand-in.
	ruleWrite func(ctx context.Context, chatID ChatID, rule permissions.Rule, d ApprovalDecision) error
	// tasks is every background task that holds a slot, by chat, under turnsMu too.
	tasks map[ChatID]map[TaskID]*task
	// monitors is the one monitor run per cluster, under turnsMu too.
	monitors map[apimeta.ClusterID]*monitor

	// stamps is what each chat's turns have seen of the files they read, under
	// its own mutex (files.go).
	stampsMu sync.Mutex
	stamps   map[ChatID]map[string]tools.Stamp

	// Shutdown. wg holds every goroutine that outlives its caller: the turns, the
	// task watchers, the sweeper and the watch pumps. They join through enter, which refuses once
	// stopped is closed; stop cancels ctx, closes stopped and waits on wg, and Close
	// then releases the statements they read through.
	wg       sync.WaitGroup
	mu       sync.Mutex // guards stopped against enter
	ctx      context.Context
	cancel   context.CancelFunc
	stopped  chan struct{}
	stopOnce sync.Once

	// Production parameters a test can change, set by newService.
	sweepRetry         time.Duration                                           // paces the sweeper after a failed sweep
	writeBackoff       time.Duration                                           // paces a settle's retry
	settleWrite        func(ctx context.Context, t *turn) error                // the settle's write, so a test can fail or hold it
	checkpointEvery    time.Duration                                           // paces an answer's checkpoints; zero writes every chunk
	deleteWrite        func(ctx context.Context, id ChatID) (bool, error)      // the delete's write, so a test can fail it
	finishWrite        func(ctx context.Context, id TaskID, end taskEnd) error // a task's last row write, so a test can hold it
	unansweredLimit    time.Duration                                           // how long an agent's request waits on the user
	now                func() time.Time
	clusterCardTimeout time.Duration

	// Hooks a test sets to see or step into a moment; nil in production.
	onSwept       chan struct{} // receives after every sweep
	onRecorded    func()        // runs once a task's rows land, before it starts
	onFoldersRead func()        // runs once a notice turn has read the folders, before its transaction
	onSlotsCopied func()        // runs once a monitor sweep has copied the slots, before it reads the live clusters
	onLiveRead    func()        // runs once a monitor sweep has read the live clusters
}

// New builds the service over the app's DB, the directory each chat's files go
// under and the one each cluster's monitor's go under, the providers a send can
// name, the card source, the memories each chat's cluster sees, the box: the
// tools every turn is offered from, which read every stored call, the lists
// that pick a turn's tools from it, whether the machine offers sandboxed Bash,
// the user's settings a session's modes, rules and folders come from, and the
// cloud targets the build has. Nothing runs until Start.
func New(db *appdb.DB, chatsDir, monitorDir string, llmSvc *llm.Service, clusterCards ClusterCards, memories Memories, box tools.Box, lists ToolLists, sandboxState sandbox.State, settingsSvc *settings.Service, cloudTargets []permissions.Target) (Service, error) {
	return newService(db, chatsDir, monitorDir, llmSvc, clusterCards, memories, box, lists, sandboxState, settingsSvc, cloudTargets)
}

// newService is New returning the concrete type, for tests. A nil memories sends
// no memory section.
func newService(db *appdb.DB, chatsDir, monitorDir string, llmSvc *llm.Service, clusterCards ClusterCards, memories Memories, box tools.Box, lists ToolLists, sandboxState sandbox.State, settingsSvc *settings.Service, cloudTargets []permissions.Target) (*service, error) {
	chatsRoot, err := rootdir.MakeRoot(chatsDir)
	if err != nil {
		return nil, fmt.Errorf("open the chats' directory: %w", err)
	}
	monitorRoot, err := rootdir.MakeRoot(monitorDir)
	if err != nil {
		_ = chatsRoot.Close()
		return nil, fmt.Errorf("open the monitor's directory: %w", err)
	}
	st, err := sqlstmt.Prepare[stmtID](context.Background(), db.Write.DB, db.Read.DB, statements)
	if err != nil {
		_ = chatsRoot.Close()
		_ = monitorRoot.Close()
		return nil, fmt.Errorf("prepare chat statements: %w", err)
	}
	s := &service{
		db:                 db,
		store:              st,
		chatsRoot:          chatsRoot,
		monitorRoot:        monitorRoot,
		llmSvc:             llmSvc,
		clusterCards:       clusterCards,
		memories:           memories,
		tools:              box,
		lists:              lists,
		sandboxState:       sandboxState,
		settings:           settingsSvc,
		cloudTargets:       cloudTargets,
		turns:              map[ChatID]*turn{},
		deleting:           map[ChatID]int{},
		pending:            map[ApprovalID]*waiter{},
		tasks:              map[ChatID]map[TaskID]*task{},
		monitors:           map[apimeta.ClusterID]*monitor{},
		stamps:             map[ChatID]map[string]tools.Stamp{},
		stopped:            make(chan struct{}),
		sweepRetry:         sweepRetry,
		writeBackoff:       writeBackoff,
		checkpointEvery:    checkpointEvery,
		unansweredLimit:    llm.UnansweredLimit,
		now:                time.Now,
		clusterCardTimeout: clusterCardTimeout,
	}
	// Built here rather than in Start, whose ctx bounds startup alone: a watch can
	// reach the service before Start.
	s.ctx, s.cancel = context.WithCancel(context.Background())
	s.deleteWrite = s.deleteRow
	s.settleWrite = s.settleRow
	s.finishWrite = s.finishRow
	s.ruleWrite = s.writeRule
	return s, nil
}

func (s *service) deleteRow(ctx context.Context, id ChatID) (deleted bool, err error) {
	err = s.store.InTx(ctx, func(st stmts) (err error) {
		deleted, err = deleteChat(ctx, st, id)
		return err
	})
	return deleted, err
}

// Start fails the runs a previous process left unfinished and closes their calls,
// marks the tasks it left running lost, starts the chat sweeper, and returns the func that drains the background work.
// ctx bounds startup only. A failed start is the end of the service: it joins the
// watches admitted before it, since nothing else will.
func (s *service) Start(ctx context.Context) (func(context.Context) error, error) {
	now := normalizeTime(s.now())
	var stranded, lost []ChatID
	err := s.store.InTx(ctx, func(st stmts) (err error) {
		// The stranded runs settle without moving their chats: the sends
		// that stranded them already did, and moving them again would put every
		// interrupted chat above ones the user touched since.
		if stranded, err = failStrandedRuns(ctx, st, strandedReason, now); err != nil {
			return err
		}
		if _, err = closeStrandedLLMCalls(ctx, st, now); err != nil {
			return err
		}
		// Lost, not stranded: the process may still be running, and its notice
		// rides the chat's next question. No turn starts at startup.
		if lost, err = markLostTasks(ctx, st, now); err != nil {
			return err
		}
		return closeStrandedToolCalls(ctx, st, now)
	})
	if err != nil {
		return nil, errors.Join(err, s.stop(context.WithoutCancel(ctx)))
	}
	if len(stranded) > 0 {
		slog.Warn("failed chat answers left unfinished by a previous run", "count", len(stranded))
	}
	if len(lost) > 0 {
		slog.Warn("background commands a previous run left running are lost; they may still be running", "count", len(lost))
	}
	// A watch opened before Start read the rows as they were; the pings are what
	// bring it the failed answers and the lost tasks.
	for _, id := range slices.Compact(slices.Sorted(slices.Values(append(stranded, lost...)))) {
		s.notify(messagesKey(id))
	}
	// A stranded run may have been waiting on the user, which the list marks.
	if len(stranded) > 0 {
		s.notify(chatsKey)
	}
	if err := s.startSweeper(); err != nil {
		return nil, err
	}
	return s.stop, nil
}

// stop ends every pump, turn, task and the sweeper, and joins them. Idempotent.
func (s *service) stop(ctx context.Context) error {
	s.stopOnce.Do(func() {
		s.cancel()
		// Under the mutex, so enter cannot check stopped and then Add past the Wait.
		s.mu.Lock()
		close(s.stopped)
		s.mu.Unlock()
		// After stopped closes: a task registers only while it is open, so every
		// task is found here or refused, and the Wait covers each watcher.
		s.stopAllTasks()
	})
	return drain.WithContext(ctx, s.wg.Wait)
}

// enter joins the group stop waits on, ErrStopping once the service is stopping.
// The caller owes the matching wg.Done.
func (s *service) enter() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	select {
	case <-s.stopped:
		return ErrStopping
	default:
		s.wg.Add(1)
		return nil
	}
}

// Close releases the prepared statements and the two roots. It runs after stop
// has joined every reader.
func (s *service) Close() error {
	return errors.Join(s.store.Close(), s.chatsRoot.Close(), s.monitorRoot.Close())
}

// notify tells the watchers of key to re-read. Called after a commit, never inside
// the transaction.
func (s *service) notify(key string) { s.db.Notify(key) }

// Send saves the question and starts its answer. The key is looked up before every
// other check, so a retry of the send that started the running turn is answered
// rather than refused as a second one; on a hit the key is the whole identity.
func (s *service) Send(ctx context.Context, chatID *ChatID, mode Mode, clusterID apimeta.ClusterID, switches SendSwitches, providerID, modelID, effort, requestID, content string, snap *Snapshot) (ChatMessage, error) {
	if err := s.enter(); err != nil {
		return ChatMessage{}, err
	}
	defer s.wg.Done()
	if appdb.ValidateUUID(requestID) != nil {
		return ChatMessage{}, ErrBadRequest
	}
	// Checked whatever chatID is, though only a create reads it: the enum guards the
	// wire and nothing guards a Go caller.
	if mode != ModeChat && mode != ModeDashboard {
		return ChatMessage{}, ErrBadRequest
	}
	content = strings.TrimSpace(content)
	var (
		prior   ChatMessage
		retried bool
	)
	err := s.store.InReadTx(ctx, func(st stmts) (err error) {
		prior, retried, err = s.seenBefore(ctx, st, requestID)
		return err
	})
	if err != nil || retried {
		return prior, err
	}
	if content == "" {
		return ChatMessage{}, ErrBadRequest
	}
	// A send that creates a chat has no call to name.
	if snap != nil && (chatID == nil || snap.check() != nil) {
		return ChatMessage{}, ErrBadRequest
	}
	// After the lookup: a replay is answered by its key whatever the machine
	// offers now.
	if switches.NetworkThisTurn && !s.sandboxState.NetworkAvailable {
		return ChatMessage{}, ErrBadRequest
	}
	// Network is the sandbox's: outside it a command has the machine's, so
	// a turn asking for both asks for nothing the override would honour.
	if switches.SandboxDisabledThisTurn && (!s.sandboxState.Available || switches.NetworkThisTurn) {
		return ChatMessage{}, ErrBadRequest
	}
	// After the lookup, never before: a check ahead of it would refuse a retry of
	// a send that already ran because the catalog moved in between.
	target, err := s.llmSvc.Resolve(providerID, modelID, effort)
	if err != nil {
		return ChatMessage{}, fmt.Errorf("%w: %w", ErrBadRequest, err)
	}
	// Rendered outside the transaction, since its reads are another store's, and
	// compared inside it, beside the rows.
	contextText, err := s.contextText(ctx, chatID, clusterID)
	if err != nil {
		return ChatMessage{}, err
	}
	// Where the turn's commands run as the sender saw it: the transaction
	// refuses a switch or a default that differs, so it is the turn's too.
	outside := switches.SandboxDisabled || switches.SandboxDisabledThisTurn
	// Read outside the transaction too, since checking each reads the disk. A
	// turn outside the sandbox takes none. The cloud platform switches are read
	// here too, so the transaction never holds the writer while it waits for a
	// reader.
	var (
		folders []session.Folder
		cloudOn []string
	)
	if !outside {
		var existing ChatID
		if chatID != nil {
			existing = *chatID
		}
		if folders, err = s.foldersFor(ctx, existing); err != nil {
			return ChatMessage{}, err
		}
		cloudOn = s.cloudTargetsOn(ctx)
	}
	at := normalizeTime(s.now())

	var (
		assistant ChatMessage
		seen      bool
		t         *turn
	)
	err = s.store.InTx(ctx, func(st stmts) (err error) {
		// Again, inside: a retry racing the first attempt.
		if assistant, seen, err = s.seenBefore(ctx, st, requestID); err != nil || seen {
			return err
		}
		// Inside the transaction, so a send and a cluster's mark are serialized.
		c, err := s.checkChat(ctx, st, chatID, clusterID)
		if err != nil {
			return err
		}
		var (
			toolUseID string
			view      tools.LogsViewAction
		)
		if snap != nil {
			var ok bool
			toolUseID, view, ok, err = viewCall(ctx, st, s.tools, *chatID, snap.CallID)
			if err != nil {
				return err
			}
			if !ok {
				return ErrBadRequest
			}
		}
		// Read in the transaction that pins them to the turn, so the turn runs
		// where and with what the sender saw, whichever window switched meanwhile.
		// A create checks the default it copies, which a window can change too.
		if chatID == nil {
			if c.SandboxDisabled, err = s.sandboxDefault(ctx, st, clusterID); err != nil {
				return err
			}
		}
		if c.SandboxDisabled != switches.SandboxDisabled {
			return ErrChatSandboxChanged
		}
		if c.NetworkEnabled != switches.NetworkEnabled {
			return ErrChatNetworkChanged
		}
		off, err := disabledTools(ctx, st)
		if err != nil {
			return err
		}
		// Before anything is written.
		if chatID != nil {
			if err := roomFor(ctx, st, *chatID, target, s.boxFor(target, off)); err != nil {
				return err
			}
		}
		id, err := s.resolveChat(ctx, st, chatID, mode, clusterID, c.SandboxDisabled, content, at)
		if err != nil {
			return err
		}
		// Reserved before the rows go in: two sends on one chat would otherwise both
		// pass the in-flight check and both take the next seq.
		if t, err = s.reserveTurn(id, newRunID(), target); err != nil {
			return err
		}
		// Pinned here, beside the context block that tells the model.
		t.outsideSandbox, t.networkThisTurn, t.disabledTools = outside, switches.NetworkThisTurn, off
		// The chat's id is known only now: a new chat has none when the card is
		// rendered.
		block := s.withWorkspace(contextText, id)
		if snap != nil {
			block = withView(block, toolUseID, view, *snap)
		}
		question, err := questionBlocks(ctx, st, id, s.withSandbox(block, sandboxState{
			outside: outside, networkEnabled: c.NetworkEnabled, networkThisTurn: switches.NetworkThisTurn, folders: folders, cloudTargetsOn: cloudOn,
		}), content)
		if err != nil {
			return err
		}
		if question, err = s.takeNotices(ctx, st, id, question, at); err != nil {
			return err
		}
		assistant, err = s.writeTurnRows(ctx, st, t, requestID, effort, question, at)
		return err
	})
	if err != nil {
		if t != nil {
			s.abandonTurn(t)
		}
		return ChatMessage{}, err
	}
	if seen {
		return assistant, nil // a retry racing the first attempt: its turn is the one running
	}

	s.startTurn(t, assistant)
	s.notify(messagesKey(assistant.ChatID))
	s.notify(chatsKey)
	return assistant, nil
}

// contextText is the context for the cluster the send is under — the card, then
// the memory section, to which the send appends the workspace — for the chat's
// stored cluster, never the send's argument, which only a create files under. A
// chat that is gone is ErrChatGone here as it is inside the transaction. The
// timeout is the render's alone: the rows go on the send's own context.
func (s *service) contextText(ctx context.Context, chatID *ChatID, clusterID apimeta.ClusterID) (string, error) {
	if chatID != nil {
		c, ok, err := getChat(ctx, s.store.Stmts(), *chatID)
		if err != nil {
			return "", err
		}
		if !ok {
			return "", ErrChatGone
		}
		clusterID = c.ClusterID
	}
	ctx, cancel := context.WithTimeout(ctx, s.clusterCardTimeout)
	defer cancel()
	return s.withMemory(ctx, s.clusterCards.ClusterCard(ctx, clusterID), clusterID), nil
}

// questionBlocks is the question as stored: the context block ahead of the text
// when its text differs from the newest one in the chat's record, the text alone
// when not. A new chat holds none, so its first question always carries one.
func questionBlocks(ctx context.Context, st stmts, id ChatID, contextText, text string) ([]llm.Block, error) {
	newest, err := newestContext(ctx, st, id)
	if err != nil {
		return nil, err
	}
	if contextText == newest {
		return []llm.Block{llm.TextBlock(text)}, nil
	}
	return []llm.Block{llm.ContextBlock(contextText), llm.TextBlock(text)}, nil
}

// seenBefore is whether a question already carries the request key, with the
// answer that attempt returned — overlaid, so a retry landing mid-answer carries
// the text so far. The key went with the chat if the chat is gone, so a retry
// then is a fresh send.
func (s *service) seenBefore(ctx context.Context, st stmts, requestID string) (ChatMessage, bool, error) {
	msg, seen, err := answerByRequestKey(ctx, st, requestID, s.tools)
	if err != nil || !seen {
		return ChatMessage{}, false, err
	}
	msgs := []ChatMessage{msg}
	s.forReader(msgs)
	return msgs[0], true, nil
}

// checkChat refuses a send into a chat nobody has and a cluster that is missing or
// marked, and answers the chat: the zero Chat for one the send creates. An
// existing chat's cluster is the chat's own, never the argument's.
func (s *service) checkChat(ctx context.Context, st stmts, chatID *ChatID, clusterID apimeta.ClusterID) (Chat, error) {
	var c Chat
	if chatID != nil {
		found, ok, err := getChat(ctx, st, *chatID)
		if err != nil {
			return Chat{}, err
		}
		if !ok {
			return Chat{}, ErrChatGone
		}
		c, clusterID = found, found.ClusterID
	}
	ok, err := clusterAccepts(ctx, st, clusterID)
	if err != nil {
		return Chat{}, err
	}
	if !ok {
		return Chat{}, ErrClusterGone
	}
	return c, nil
}

// sandboxDefault is the sandbox switch a new chat on cluster starts with: its
// cluster's default, else every cluster's, else sandboxed. A machine with no
// sandbox reads none.
func (s *service) sandboxDefault(ctx context.Context, st stmts, cluster apimeta.ClusterID) (bool, error) {
	if !s.sandboxState.Available {
		return false, nil
	}
	rows, err := st.Query(ctx, stmtSelectSandboxDefault, settings.SandboxDefaultArgs(cluster)...)
	if err != nil {
		return false, fmt.Errorf("sandbox default: %w", err)
	}
	return settings.ScanSandboxDefault(rows, cluster)
}

// resolveChat creates the chat a nil chatID asks for, with no network and
// sandboxDisabled as its switch, else touches the one named: the list sorts by
// updated_at, which moves when an answer starts.
func (s *service) resolveChat(ctx context.Context, st stmts, chatID *ChatID, mode Mode, clusterID apimeta.ClusterID, sandboxDisabled bool, content string, at time.Time) (ChatID, error) {
	if chatID == nil {
		c := Chat{ID: newChatID(), Title: titleFrom(content), Mode: mode, ClusterID: clusterID, SandboxDisabled: sandboxDisabled, CreatedAt: at, UpdatedAt: at}
		return c.ID, insertChat(ctx, st, c)
	}
	return *chatID, touchChat(ctx, st, *chatID, at)
}

// writeTurnRows writes what one accepted send owes, in the order their references
// run: the question carrying the request key, the queued run answering it, and the
// empty answer the run produces. It returns the answer as the read will serve it.
func (s *service) writeTurnRows(ctx context.Context, st stmts, t *turn, requestID, effort string, question []llm.Block, at time.Time) (ChatMessage, error) {
	providerID, modelID := t.target.Provider.ID, t.target.Model.ID
	seq, err := nextSeq(ctx, st, t.chatID)
	if err != nil {
		return ChatMessage{}, err
	}
	user := ChatMessage{
		ID: newMessageID(), ChatID: t.chatID, Seq: seq, Role: RoleUser,
		Content: marshalBlocks(question), Status: StatusComplete, CreatedAt: at, ToolCalls: emptyToolCalls,
		Citations: emptyCitations,
	}
	if err := insertMessage(ctx, st, user, requestID); err != nil {
		return ChatMessage{}, err
	}
	run := agentRun{
		ID: t.runID, ChatID: t.chatID, TriggerMessageID: user.ID,
		ProviderID: providerID, ModelID: modelID, Effort: effort, Dialect: t.target.Provider.Dialect,
		AppVersion: version.Version, CreatedAt: at,
	}
	if err := insertRun(ctx, st, run); err != nil {
		return ChatMessage{}, err
	}
	assistant := ChatMessage{
		ID: newMessageID(), ChatID: t.chatID, Seq: seq + 1, Role: RoleAssistant,
		Content: emptyContent, RunID: t.runID, Status: StatusStreaming, CreatedAt: at,
		ProviderID: providerID, ModelID: modelID, Effort: effort, ToolCalls: emptyToolCalls,
		Citations: emptyCitations, dialect: t.target.Provider.Dialect,
	}
	if err := insertMessage(ctx, st, assistant, ""); err != nil {
		return ChatMessage{}, err
	}
	return assistant, nil
}

// titleFrom is a chat's title until the user renames it: the message's first line,
// trimmed and capped. The cap counts characters, so a multibyte one is never cut
// in half.
func titleFrom(content string) string {
	title := content
	if i := strings.IndexAny(title, lineBreaks); i >= 0 {
		title = title[:i]
	}
	title = strings.TrimSpace(title)
	if utf8.RuneCountInString(title) > maxTitleLen {
		title = string([]rune(title)[:maxTitleLen])
	}
	return title
}

// Cancel stops the chat's turn, if one is running. The turn settles cancelled with
// the text so far.
func (s *service) Cancel(ctx context.Context, chatID ChatID) error {
	if t := s.turnOf(chatID); t != nil {
		t.cancel()
	}
	return nil
}

// Rename retitles a chat and returns the record it committed.
func (s *service) Rename(ctx context.Context, chatID ChatID, title string) (Chat, error) {
	title = strings.TrimSpace(title)
	if title == "" || utf8.RuneCountInString(title) > maxTitleLen || strings.ContainsAny(title, lineBreaks) {
		return Chat{}, ErrBadRequest
	}
	at := normalizeTime(s.now())
	var renamed Chat
	err := s.store.InTx(ctx, func(st stmts) error {
		c, ok, err := renameChat(ctx, st, chatID, title, at)
		if err != nil {
			return err
		}
		if !ok {
			return ErrChatGone
		}
		renamed = c
		return nil
	})
	if err != nil {
		return Chat{}, err
	}
	s.notify(chatsKey)
	return renamed, nil
}

// SetSandboxDisabled writes the chat's switch and returns the record it committed.
func (s *service) SetSandboxDisabled(ctx context.Context, chatID ChatID, disabled bool) (Chat, error) {
	if !s.sandboxState.Available {
		return Chat{}, ErrBadRequest
	}
	var switched Chat
	err := s.store.InTx(ctx, func(st stmts) error {
		c, ok, err := setSandboxDisabled(ctx, st, chatID, disabled)
		if err != nil {
			return err
		}
		if !ok {
			return ErrChatGone
		}
		switched = c
		return nil
	})
	if err != nil {
		return Chat{}, err
	}
	s.notify(chatsKey)
	return switched, nil
}

// SetNetworkEnabled writes the chat's network switch and returns the record it
// committed. Where network is unavailable nothing turns it on, and neither does
// a chat outside the sandbox, so the switch is on only where it applies and a
// chat back in the sandbox starts without it; turning it off is always accepted.
func (s *service) SetNetworkEnabled(ctx context.Context, chatID ChatID, enabled bool) (Chat, error) {
	if enabled && !s.sandboxState.NetworkAvailable {
		return Chat{}, ErrBadRequest
	}
	var switched Chat
	err := s.store.InTx(ctx, func(st stmts) error {
		if enabled {
			// In the transaction, so a sandbox switch cannot land between the read
			// and the write.
			c, ok, err := getChat(ctx, st, chatID)
			if err != nil {
				return err
			}
			if !ok {
				return ErrChatGone
			}
			if c.SandboxDisabled {
				return ErrBadRequest
			}
		}
		c, ok, err := setNetworkEnabled(ctx, st, chatID, enabled)
		if err != nil {
			return err
		}
		if !ok {
			return ErrChatGone
		}
		switched = c
		return nil
	})
	if err != nil {
		return Chat{}, err
	}
	s.notify(chatsKey)
	return switched, nil
}

// Delete removes the chat's row, and its messages, runs and directory with it.
func (s *service) Delete(ctx context.Context, chatID ChatID) error {
	// The id names the chat's directory to remove, so a path is refused before
	// anything is touched: "<chat>/out.txt" would reach into a live chat's files.
	if appdb.ValidateUUID(string(chatID)) != nil {
		return ErrBadRequest
	}
	// The rows go on a context of the service's own, so stop has to wait for this.
	if err := s.enter(); err != nil {
		return err
	}
	defer s.wg.Done()

	// The write outlives the caller and ends with the service: a client that gave up
	// still asked for the chat to go, and stop must not wait on a write parked
	// behind the one writer connection.
	ctx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	defer cancel()
	defer context.AfterFunc(s.ctx, cancel)()
	// A turn in flight is cancelled and joined first, off the mutex, so its settle
	// lands before the rows go; the guard keeps a send from reserving another until
	// the rows are gone. A settle whose first attempt failed holds no delete: the
	// writer has one connection, so a later attempt either lands before the rows
	// go or finds nothing to write, and the write below ends the retry.
	t := s.guardDelete(chatID)
	if t != nil {
		t.cancel()
		select {
		case <-t.done:
		case <-t.retrying:
		}
	}
	defer s.releaseDelete(chatID)
	// Then its tasks, joined, so every file is closed before the directory goes and
	// no watcher writes after the rows do. The guard refuses a start from here on.
	for _, done := range s.stopChatTasks(chatID) {
		<-done
	}
	deleted, err := s.deleteWrite(ctx, chatID)
	if err != nil {
		return err
	}
	if t != nil {
		t.goneOnce.Do(func() { close(t.gone) })
	}
	// The run has ended, and a settle's retry writes rows alone, so nothing of the
	// sidecar's writes to the directory after.
	s.chatDir(chatID).remove()
	s.dropStamps(chatID)
	s.notify(chatsKey)
	s.notify(messagesKey(chatID))
	// The mirror removes a marked cluster's row only once its chats are gone, and
	// this may have been the last: a row that went is its signal, whoever asked.
	if deleted {
		s.notify(appdb.KeyClusters)
	}
	return nil
}

func (s *service) Get(ctx context.Context, chatID ChatID) (Chat, bool, error) {
	return getChat(ctx, s.store.Stmts(), chatID)
}

func (s *service) List(ctx context.Context) ([]Chat, error) {
	return listChats(ctx, s.store.Stmts())
}

// WatchCollection streams every chat, newest activity first, then every change to the list.
func (s *service) WatchCollection(ctx context.Context) (*Stream[ChatCollectionWatchFrame], error) {
	return newStream(s, ctx, func(ctx context.Context, out chan<- ChatCollectionWatchFrame) error {
		// Listen, then read: a snapshot taken first would miss a commit in between.
		rx := s.db.ListenForChanges(chatsKey)
		defer rx.Close()

		f := newChatFold()
		chats, err := listChats(ctx, s.store.Stmts())
		if err != nil {
			return err
		}
		if !f.Snapshot(ctx, out, chats) {
			return nil
		}
		for {
			// RecvContext, not Chan: the hub closes only after stop has joined the
			// pumps, so a pump on the channel alone would outlive its consumer.
			if _, err := rx.RecvContext(ctx); err != nil {
				return nil
			}
			chats, err := listChats(ctx, s.store.Stmts())
			if err != nil {
				return err
			}
			if !f.Diff(ctx, out, chats) {
				return nil
			}
		}
	}), nil
}

// WatchMessageCollection streams one chat's transcript, then every change to it.
func (s *service) WatchMessageCollection(ctx context.Context, chatID ChatID) (*Stream[ChatMessageCollectionWatchFrame], error) {
	return newStream(s, ctx, func(ctx context.Context, out chan<- ChatMessageCollectionWatchFrame) error {
		rx := s.db.ListenForChanges(messagesKey(chatID))
		defer rx.Close()

		f := newMessageFold()
		msgs, err := s.readMessages(ctx, chatID)
		if err != nil {
			return err
		}
		if !f.Snapshot(ctx, out, msgs) {
			return nil
		}
		for {
			if _, err := rx.RecvContext(ctx); err != nil {
				return nil
			}
			msgs, err := s.readMessages(ctx, chatID)
			if err != nil {
				return err
			}
			if !f.Diff(ctx, out, msgs) {
				return nil
			}
		}
	}), nil
}
