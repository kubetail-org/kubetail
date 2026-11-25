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

// Background tasks: what a call started that outlives it — a command's process,
// or an agent. Each is a row, a file in the chat's directory, and a
// watcher that writes how it ended. The service holds every task that holds a
// slot, and stops them with the chat and with the app.
package chat

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/tools"
)

// The most tasks that run at once in one chat, and in the app.
const (
	maxTasksPerChat = 4
	maxTasks        = 16
)

// taskDirName is the directory under a chat's that holds its tasks' files.
const taskDirName = "tasks"

// errNoOpenCall is a task start with no call running: only a call can start
// one, since the row hangs off it.
var errNoOpenCall = errors.New("chat: a task starts only from a running call")

// task is one background task, from its registration until its watcher has
// written its row.
type task struct {
	id         TaskID
	chatID     ChatID
	toolCallID ToolCallID
	// runID is the run whose call started it.
	runID RunID
	// end names how it ended, off its own record; nil for a command, whose end
	// is its exit's (endCommand).
	end func(by string, at time.Time) taskEnd
	// done closes when the task's row is written and its slot released.
	done chan struct{}

	// Under turnsMu. proc is the process, nil until it has started; stoppedBy
	// is who stopped it first, "" until someone does; stopPending is a stop
	// that came before the process existed, and stopNow whether it was to kill
	// at once, which the start applies. reaped is set once Wait has answered:
	// the task still holds its slot while its row is written, but a stop can no
	// longer reach it, so neither the model's nor the user's answers that it did.
	proc                 tools.Task
	stoppedBy            string
	stopPending, stopNow bool
	reaped               bool
}

// chatTasks is one chat's tasks as a run's tools start and stop them. A task's
// file goes in the chat's directory, and it belongs to the call open in
// run; run is nil for tasks no run starts.
type chatTasks struct {
	s   *service
	id  ChatID
	run *runJournal
}

var _ tools.Tasks = chatTasks{}

func (s *service) chatTasks(id ChatID, j *runJournal) chatTasks {
	return chatTasks{s: s, id: id, run: j}
}

// Start starts a task for the call now running in the run.
func (c chatTasks) Start(start func(*os.File) (tools.Task, error)) (string, string, error) {
	if c.run == nil || c.run.openTool == nil {
		return "", "", errNoOpenCall
	}
	return c.s.startTask(c.id, c.run.runID, c.run.openTool.ID, taskRecord{}, start)
}

// Stop stops one of the chat's tasks on the model's word, with the grace. A
// subagent reaches only the tasks its own run's calls started: a stop by the
// model is written notified, since its result told it, and only that model read
// the result, so a subagent stopping another's task, or its own agent, would
// leave the parent waiting for a notice that never comes.
func (c chatTasks) Stop(id string) bool {
	s := c.s
	s.turnsMu.Lock()
	defer s.turnsMu.Unlock()
	tk, ok := s.tasks[c.id][TaskID(id)]
	if !ok || tk.reaped {
		return false
	}
	if c.run != nil && !c.run.isTurns() && tk.runID != c.run.runID {
		return false
	}
	s.stopLocked(tk, stoppedByModel, false)
	return true
}

// taskRecord is what a task's start writes beside its own row, in the same
// transaction, how a start that failed takes it back, and how the task names its
// end when its exit does not; zero for a command.
type taskRecord struct {
	write, takeBack func(context.Context, stmts) error
	end             func(by string, at time.Time) taskEnd
}

// startTask takes a slot for a task the run's call toolCallID starts, makes the
// task's file, writes its row with rec, starts the task and hands it to a
// watcher; a step that fails undoes the ones before it. The slot is one of wg's,
// taken through enter, so stop waits for the watcher that releases it.
func (s *service) startTask(chatID ChatID, runID RunID, toolCallID ToolCallID, rec taskRecord, start func(*os.File) (tools.Task, error)) (string, string, error) {
	if err := s.enter(); err != nil {
		return "", "", err
	}
	dir := s.chatDir(chatID)
	tk := &task{id: newTaskID(), chatID: chatID, runID: runID, toolCallID: toolCallID, end: rec.end, done: make(chan struct{})}
	if err := s.registerTask(tk); err != nil {
		s.wg.Done()
		return "", "", err
	}
	f, path, err := openTaskFile(dir, tk.id)
	if err != nil {
		s.dropTask(tk)
		return "", "", err
	}
	proc, err := s.recordAndStart(tk, path, f, rec, start)
	if err != nil {
		_ = f.Close()
		removeTaskFile(dir, tk.id)
		s.dropTask(tk)
		return "", "", err
	}

	s.turnsMu.Lock()
	tk.proc = proc
	if tk.stopPending {
		proc.Stop(tk.stopNow)
	}
	s.turnsMu.Unlock()
	go s.watchTask(tk, f)
	s.notify(messagesKey(tk.chatID))
	return string(tk.id), path, nil
}

// recordAndStart writes the task's row running, with rec's in the same
// transaction, and starts the task. A start that fails takes the rows back, so
// no row stands for a task that never ran.
func (s *service) recordAndStart(tk *task, path string, f *os.File, rec taskRecord, start func(*os.File) (tools.Task, error)) (tools.Task, error) {
	ctx := s.ctx
	err := s.store.InTx(ctx, func(st stmts) error {
		if rec.write != nil {
			if err := rec.write(ctx, st); err != nil {
				return err
			}
		}
		return insertTask(ctx, st, tk.id, tk.chatID, tk.toolCallID, path, normalizeTime(s.now()))
	})
	if err != nil {
		return nil, err
	}
	if s.onRecorded != nil {
		s.onRecorded()
	}
	proc, err := start(f)
	if err != nil {
		wctx := context.WithoutCancel(ctx)
		derr := s.store.InTx(wctx, func(st stmts) error {
			if rec.takeBack != nil {
				if err := rec.takeBack(wctx, st); err != nil {
					return err
				}
			}
			return deleteTask(wctx, st, tk.id)
		})
		if derr != nil {
			slog.Warn("could not remove a task that never started; the next start marks it lost", "err", derr)
		}
		return nil, err
	}
	return proc, nil
}

// registerTask holds a slot for tk, or refuses: a stopping service, a chat being
// deleted, or a limit. The check of stopped is under turnsMu, the lock stop walks
// the tasks under, so a task is either found by that walk or refused here.
func (s *service) registerTask(tk *task) error {
	s.turnsMu.Lock()
	defer s.turnsMu.Unlock()
	select {
	case <-s.stopped:
		return ErrStopping
	default:
	}
	if s.deleting[tk.chatID] > 0 {
		return ErrChatGone
	}
	if len(s.tasks[tk.chatID]) >= maxTasksPerChat {
		return tools.ErrChatTaskLimit
	}
	n := 0
	for _, chat := range s.tasks {
		n += len(chat)
	}
	if n >= maxTasks {
		return tools.ErrTaskLimit
	}
	if s.tasks[tk.chatID] == nil {
		s.tasks[tk.chatID] = map[TaskID]*task{}
	}
	s.tasks[tk.chatID][tk.id] = tk
	return nil
}

// unregisterTask releases tk's slot.
func (s *service) unregisterTask(tk *task) {
	s.turnsMu.Lock()
	defer s.turnsMu.Unlock()
	delete(s.tasks[tk.chatID], tk.id)
	if len(s.tasks[tk.chatID]) == 0 {
		delete(s.tasks, tk.chatID)
	}
}

// dropTask releases a task that never started: its slot, its done, its wg.
func (s *service) dropTask(tk *task) {
	s.unregisterTask(tk)
	close(tk.done)
	s.wg.Done()
}

// openTaskFile makes the chat's tasks directory and a new file for the task in
// it, through the chat's root, and answers the path a result names it by.
func openTaskFile(dir entryDir, id TaskID) (*os.File, string, error) {
	root, err := dir.Root(true)
	if err != nil {
		return nil, "", err
	}
	defer root.Close()
	if err := root.MkdirAll(taskDirName, 0o700); err != nil {
		return nil, "", err
	}
	name := filepath.Join(taskDirName, string(id)+".output")
	f, err := root.OpenFile(name, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return nil, "", err
	}
	return f, filepath.Join(dir.Path(), name), nil
}

// removeTaskFile removes a task's file, for a start that failed after making it.
func removeTaskFile(dir entryDir, id TaskID) {
	root, err := dir.Root(false)
	if err != nil {
		return
	}
	defer root.Close()
	_ = root.Remove(filepath.Join(taskDirName, string(id)+".output"))
}

// watchTask waits on the task, names how it ended — a command's off its exit,
// ending its file with a line that says so; an agent's off its run, whose loop
// has closed its file — writes its row, and only then releases its slot, so the
// count always matches the rows still running. The row goes on a context
// without the service's cancel: stop cancels it before it stops the tasks.
func (s *service) watchTask(tk *task, f *os.File) {
	defer s.wg.Done()
	exit := tk.proc.Wait()
	s.turnsMu.Lock()
	tk.reaped = true
	by := tk.stoppedBy
	s.turnsMu.Unlock()

	var end taskEnd
	at := normalizeTime(s.now())
	if tk.end != nil {
		end = tk.end(by, at)
	} else {
		end = endCommand(exit, by, at, f)
	}

	if err := s.finishWrite(context.WithoutCancel(s.ctx), tk.id, end); err != nil {
		slog.Warn("could not record how a task ended; the next start marks it lost", "err", err)
	}
	s.unregisterTask(tk)
	s.notify(messagesKey(tk.chatID))
	// An agent's run may have ended waiting on the user, which the list marks.
	s.notify(chatsKey)
	// Before done closes, so whoever waits on the task sees its kick through.
	// startsTurn decides whether the end starts one.
	s.kick(tk.chatID)
	close(tk.done)
}

// endCommand is how a command's task ends, off its exit, and ends its file with
// the line that says so.
func endCommand(exit tools.Exit, by string, at time.Time, f *os.File) taskEnd {
	end := taskEnd{Status: taskExited, At: at}
	var line string
	switch {
	case exit.Stopped:
		end.Status, end.StoppedBy, end.Notified = taskStopped, by, by == stoppedByModel
		line = "[stopped]"
	case exit.OK:
		end.ExitCode = sql.NullInt64{Int64: int64(exit.Code), Valid: true}
		line = fmt.Sprintf("[exited with code %d]", exit.Code)
	default:
		line = "[exited; its code could not be read]"
	}
	if err := endTaskFile(f, line); err != nil {
		slog.Warn("could not end a task's output file", "err", err)
	}
	return end
}

// finishRow is finishWrite's production value: the task's end, with what the
// end writes beside it, in one transaction. When that fails it tries once more
// without the run's rows, which rewrite rows whose own writes may have failed,
// so the task and its run still end rather than stay running until the next
// start.
func (s *service) finishRow(ctx context.Context, id TaskID, end taskEnd) error {
	err := s.store.InTx(ctx, func(st stmts) error {
		if end.Rows != nil {
			if err := end.Rows(ctx, st); err != nil {
				return err
			}
		}
		return finishTaskAndRun(ctx, st, id, end)
	})
	if err != nil && end.Rows != nil {
		slog.Warn("could not write an agent's rows as it ended; ending it without them", "err", err)
		err = s.store.InTx(ctx, func(st stmts) error { return finishTaskAndRun(ctx, st, id, end) })
	}
	return err
}

func finishTaskAndRun(ctx context.Context, st stmts, id TaskID, end taskEnd) error {
	if end.Run != nil {
		if err := end.Run(ctx, st); err != nil {
			return err
		}
	}
	return finishTask(ctx, st, id, end)
}

// endTaskFile appends line on a line of its own and closes the file.
func endTaskFile(f *os.File, line string) error {
	info, err := f.Stat()
	if err == nil && info.Size() > 0 {
		last := make([]byte, 1)
		if _, err = f.ReadAt(last, info.Size()-1); err == nil && last[0] != '\n' {
			line = "\n" + line
		}
	}
	if err == nil {
		_, err = f.WriteString(line + "\n")
	}
	return errors.Join(err, f.Close())
}

// StopBackgroundTask is the user's stop of the task a call started, with the
// grace. false when no task of that call is still running.
func (s *service) StopBackgroundTask(_ context.Context, id ToolCallID) (bool, error) {
	return s.stopTaskOf(id, stoppedByUser), nil
}

// stopTaskOf stops the task the call id started, with the grace, recording by;
// false when no task of that call is still running.
func (s *service) stopTaskOf(id ToolCallID, by string) bool {
	s.turnsMu.Lock()
	defer s.turnsMu.Unlock()
	for _, chat := range s.tasks {
		for _, tk := range chat {
			if tk.toolCallID == id {
				if tk.reaped {
					return false
				}
				s.stopLocked(tk, by, false)
				return true
			}
		}
	}
	return false
}

// stopChatTasks stops every task of the chat at once and returns what to wait
// on: each closes once its row is written.
func (s *service) stopChatTasks(chatID ChatID) []chan struct{} {
	s.turnsMu.Lock()
	defer s.turnsMu.Unlock()
	var done []chan struct{}
	for _, tk := range s.tasks[chatID] {
		s.stopLocked(tk, stoppedByUser, true)
		done = append(done, tk.done)
	}
	return done
}

// stopAllTasks stops every task at once, for the service's stop.
func (s *service) stopAllTasks() {
	s.turnsMu.Lock()
	defer s.turnsMu.Unlock()
	for _, chat := range s.tasks {
		for _, tk := range chat {
			s.stopLocked(tk, stoppedByApp, true)
		}
	}
}

// stopLocked records who stopped tk, the first alone, and stops it, or leaves
// the stop for the start to apply when there is no process yet. Under turnsMu.
func (s *service) stopLocked(tk *task, by string, now bool) {
	if tk.stoppedBy == "" {
		tk.stoppedBy = by
	}
	if tk.proc == nil {
		tk.stopPending, tk.stopNow = true, tk.stopNow || now
		return
	}
	tk.proc.Stop(now)
}
