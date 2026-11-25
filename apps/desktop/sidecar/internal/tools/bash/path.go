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

package bash

import (
	"context"
	"errors"
	"log/slog"
	"path/filepath"
	"strings"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/run/sandbox"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/settings"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/tools"
)

// emptyPath is the PATH of a run that searches no folder. An empty PATH would
// search the working directory, which a run can write, so it names a folder
// no run can make.
const emptyPath = "/nonexistent"

// runPath is the folders a run searches, in order, and the Read rules they
// need: one per folder open does not read already. list is the run's PATH as
// read at its start, each folder checked by its origin (RunCheck). Each
// folder left out gets one log line. It reads the disk.
func runPath(list []settings.PathFolder, open sandbox.FilePolicy, never []string, home string) (folders, reads []string) {
	for _, f := range settings.NewRunCheck(open, never, home).Folders(list) {
		if !f.Searched {
			slog.Info("PATH folder left out of a run", "folder", f.Path, "origin", string(f.Origin), "reason", f.Reason)
			continue
		}
		folders = append(folders, f.Target)
		if !f.Open {
			reads = append(reads, f.Target)
		}
	}
	return folders, reads
}

// joinPath is folders as PATH, or emptyPath with none.
func joinPath(folders []string) string {
	if len(folders) == 0 {
		return emptyPath
	}
	return strings.Join(folders, string(filepath.ListSeparator))
}

// readPath is the run's PATH, read once at its start: none for a tool with no
// pathList. A read that fails, or a setting Kubetail cannot use, fails the run.
func (t *Tool) readPath(ctx context.Context) (settings.RunPath, error) {
	if t.pathList == nil {
		return settings.RunPath{}, nil
	}
	list, err := t.pathList(ctx)
	if refusal, ok := errors.AsType[settings.PathRefusal](err); ok {
		return settings.RunPath{}, refusal
	}
	if err != nil {
		return settings.RunPath{}, errors.New(tools.PathUnread(Name, err).Result)
	}
	return list, nil
}
