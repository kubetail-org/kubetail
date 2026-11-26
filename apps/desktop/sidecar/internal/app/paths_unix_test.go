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

//go:build !windows

package app

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Logs linked out of the state keep an entry of their own, as their target.
func TestLogsLinkedOutOfTheStateAreFencedAtTheirTarget(t *testing.T) {
	root := scratch(t)
	n := linuxNamed(root)
	target := filepath.Join(root, "mnt", "logs")
	require.NoError(t, os.MkdirAll(target, 0o700))
	require.NoError(t, os.MkdirAll(n.Layout.State, 0o700))
	symlink(t, target, n.Layout.Logs)

	f, err := ResolveFolders(n)

	require.NoError(t, err)
	assert.Contains(t, pathsOf(Config{Folders: f}).Bash.DeniedDirs, target)
}
