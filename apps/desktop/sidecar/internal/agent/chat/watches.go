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

// Log watches: a follow a LogsWatch call opened, held as a background task of the
// chat. The watch reads its own follow; the chat holds its slot and its row, stops
// it, and writes how it ended.
package chat

import (
	"database/sql"
	"encoding/json"
	"os"
	"time"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/tools"
)

// chatWatches is one chat's watches as a run's tools start them, under the call
// open in run.
type chatWatches struct {
	s   *service
	id  ChatID
	run *runJournal
}

var _ tools.Watches = chatWatches{}

func (s *service) chatWatches(id ChatID, j *runJournal) chatWatches {
	return chatWatches{s: s, id: id, run: j}
}

// Start holds w as a task of the call now running in the run.
func (c chatWatches) Start(w tools.Watch) (string, error) {
	if c.run == nil || c.run.openTool == nil {
		return "", errNoOpenCall
	}
	rec := taskRecord{end: func(by string, at time.Time) taskEnd { return endWatch(w.End(by), by, at) }}
	// A watch writes nothing to its file: it is closed, empty, before the watch runs.
	id, _, err := c.s.startTask(c.id, c.run.runID, c.run.openTool.ID, rec, func(f *os.File) (tools.Task, error) {
		return watchTask{w}, f.Close()
	})
	return id, err
}

// watchTask is a watch as a tools.Task: Wait reads the follow to its end, and any
// stop ends it at once, since there is no process to give a grace.
type watchTask struct {
	w tools.Watch
}

func (t watchTask) Wait() tools.Exit {
	t.w.Wait()
	return tools.Exit{}
}

func (t watchTask) Stop(bool) { t.w.Stop() }

// watchResultOf is the result call's watch wrote to its task row, read through
// box (tools.Box.WatchResult). A result the box refuses is none, and one warning.
func watchResultOf(box tools.Box, call toolCallEntry, result string) (tools.WatchResult, bool) {
	r, err := box.WatchResult(call.Name, tools.ContractName(call.Contract), json.RawMessage(result))
	if err != nil {
		warnRefused(call.ID, "result", err)
		return tools.WatchResult{}, false
	}
	return r, true
}

// watchStatuses is each watch end's row status.
var watchStatuses = map[tools.WatchStatus]string{
	tools.WatchMatched: taskMatched, tools.WatchExpired: taskExpired,
	tools.WatchFailed: taskFailed, tools.WatchStopped: taskStopped,
}

// endWatch is how a watch's task ends: its status, who stopped it on a stopped
// row, and its result. It is never written notified, a stop by the model
// included: TaskStop answers before the watch has ended, so only the notice
// carries the lines.
func endWatch(e tools.WatchEnd, by string, at time.Time) taskEnd {
	end := taskEnd{Status: watchStatuses[e.Status], At: at}
	if e.Status == tools.WatchStopped {
		end.StoppedBy = by
	}
	result, _ := json.Marshal(e.Result) // strings, times and slices of them always marshal
	end.Result = sql.NullString{String: string(result), Valid: true}
	return end
}
