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

// File stamps: what a chat's turns have seen of the files they read. Kept in
// memory alone, so a restart forgets them and a tool that needs one asks the
// model to read the file again.
package chat

import "github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/tools"

// chatFiles is one chat's stamps as a turn's tools read and set them.
type chatFiles struct {
	s  *service
	id ChatID
}

var _ tools.FileStamps = chatFiles{}

func (s *service) chatFiles(id ChatID) chatFiles { return chatFiles{s: s, id: id} }

func (c chatFiles) Stamp(path string) (tools.Stamp, bool) {
	c.s.stampsMu.Lock()
	defer c.s.stampsMu.Unlock()
	st, ok := c.s.stamps[c.id][path]
	return st, ok
}

func (c chatFiles) SetStamp(path string, st tools.Stamp) {
	c.s.stampsMu.Lock()
	defer c.s.stampsMu.Unlock()
	if c.s.stamps[c.id] == nil {
		c.s.stamps[c.id] = map[string]tools.Stamp{}
	}
	c.s.stamps[c.id][path] = st
}

// runStamps is what one subagent has seen, apart from its chat's: a stamp says
// the model saw the file, and the parent did not see what the subagent read. The
// subagent's calls run on its goroutine alone, and the set goes with the subagent.
type runStamps map[string]tools.Stamp

var _ tools.FileStamps = runStamps{}

func (r runStamps) Stamp(path string) (tools.Stamp, bool) {
	st, ok := r[path]
	return st, ok
}

func (r runStamps) SetStamp(path string, st tools.Stamp) { r[path] = st }

// dropStamps forgets every stamp of a deleted chat.
func (s *service) dropStamps(id ChatID) {
	s.stampsMu.Lock()
	defer s.stampsMu.Unlock()
	delete(s.stamps, id)
}
