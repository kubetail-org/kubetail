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

package tools

import (
	"os"
	"path/filepath"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/rootdir"
)

// workspaceName is the workspace's directory under the chat's, and
// toolHomeName the tool home's.
const (
	workspaceName = "workspace"
	toolHomeName  = "toolhome"
)

// WorkspacePath is the chat's workspace: a directory under the chat's, where
// every command starts and whose files last for the rest of the chat. It may not
// exist.
func WorkspacePath(dir ChatDir) string {
	return filepath.Join(dir.Path(), workspaceName)
}

// OpenWorkspace opens the chat's workspace as a root, as openIn opens it. The
// caller closes the root.
func OpenWorkspace(dir ChatDir, create bool) (*os.Root, error) {
	return openIn(dir, workspaceName, create)
}

// ToolHomePath is the chat's tool home, beside its workspace: where a
// sandboxed command's tools keep what they would write under the home, through
// their own variables. It may not exist.
func ToolHomePath(dir ChatDir) string {
	return filepath.Join(dir.Path(), toolHomeName)
}

// OpenToolHome opens the chat's tool home as a root, as openIn opens it. The
// caller closes the root.
func OpenToolHome(dir ChatDir, create bool) (*os.Root, error) {
	return openIn(dir, toolHomeName, create)
}

// openIn opens name under the chat's directory, reached through the chat's root
// alone, as rootdir.Open opens it. The caller closes the root.
func openIn(dir ChatDir, name string, create bool) (*os.Root, error) {
	chat, err := dir.Root(create)
	if err != nil {
		return nil, err
	}
	defer chat.Close()
	return rootdir.Open(chat, name, create)
}
