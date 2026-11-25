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

// Each chat's own directory: its saved results, its background tasks' output and
// its workspace, under the chats' directory and deleted with the chat. Each
// cluster's monitor directory is the same shape under the monitor's directory,
// deleted with the cluster.
package chat

import (
	"log/slog"
	"os"
	"path/filepath"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/apimeta"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/rootdir"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/tools"
)

// entryDir is one entry of a root the service holds open: a chat's directory
// in the chats' root, or a cluster's monitor directory in the monitor's. Each
// root's path is absolute, since a result names its file under it and Read
// takes only an absolute path.
type entryDir struct {
	root *os.Root
	name string
}

var _ tools.ChatDir = entryDir{}

func (s *service) chatDir(id ChatID) entryDir { return entryDir{root: s.chatsRoot, name: string(id)} }

func (s *service) monitorDir(id apimeta.ClusterID) entryDir {
	return entryDir{root: s.monitorRoot, name: string(id)}
}

func (d entryDir) Path() string { return filepath.Join(d.root.Name(), d.name) }

// Root reaches the entry through its root, so a link swapped in for it is
// refused rather than followed.
func (d entryDir) Root(create bool) (*os.Root, error) { return rootdir.Open(d.root, d.name, create) }

// remove removes the entry, a link rather than its target. An entry already
// gone is a removal done. A failure is logged, and the next sweep removes what
// it left.
func (d entryDir) remove() {
	if err := rootdir.RemoveAll(d.root, d.name); err != nil {
		slog.Warn("could not remove a directory; the next sweep removes it", "dir", d.Path(), "err", err)
	}
}
