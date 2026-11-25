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

package bash

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/tools"
)

// A link planted at any level of the tool home is refused, so a command that
// swaps one in reaches no other folder.
func TestALinkInTheToolHomeIsRefused(t *testing.T) {
	for _, level := range []string{"toolhome", "toolhome/xdg", "toolhome/xdg/cache"} {
		t.Run(level, func(t *testing.T) {
			dir := testChatDir(t)
			path := filepath.Join(string(dir), filepath.FromSlash(level))
			require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
			require.NoError(t, os.Symlink(t.TempDir(), path))

			assert.Error(t, makeToolHome(dir))
		})
	}
}

// confiningTool is a tool on the machine's confining sandbox, whose
// forwarder, this test binary, writes its coverage where a run may write.
func confiningTool(t *testing.T) *Tool {
	t.Helper()
	tl := tool(t)
	tl.sandboxer = confining(t)
	tl.env = append(tl.env, "GOCOVERDIR="+childCoverDir)
	tl.extraWritable = []string{childCoverDir}
	return tl
}

// Through the real sandbox, a run writes its own chat's tool home and not
// another chat's.
func TestARunWritesOnlyItsOwnToolHome(t *testing.T) {
	tl := confiningTool(t)
	own, other := testRuntime(t), testRuntime(t)
	require.NoError(t, makeToolHome(other.Dir))
	theirs := filepath.Join(tools.ToolHomePath(other.Dir), "xdg", "cache", "planted")

	text, isError := tl.Run(t.Context(), own, command(`touch "$XDG_CACHE_HOME/mine" && echo "$XDG_CACHE_HOME"; touch '`+theirs+`' 2>/dev/null || echo refused`))

	require.False(t, isError, text)
	assert.Equal(t, filepath.Join(tools.ToolHomePath(own.Dir), "xdg", "cache")+"\nrefused\n", text)
	assert.FileExists(t, filepath.Join(tools.ToolHomePath(own.Dir), "xdg", "cache", "mine"))
	assert.NoFileExists(t, theirs)
}

// helm, where the machine has it, names the tool home and writes there.
func TestHelmWritesItsCache(t *testing.T) {
	tl := confiningTool(t)
	withTool(t, tl, "helm")
	rt := testRuntime(t)

	text, isError := tl.Run(t.Context(), rt, command(`helm env HELM_CACHE_HOME && touch "$(helm env HELM_CACHE_HOME)/x" && echo wrote`))

	require.False(t, isError, text)
	cache := filepath.Join(tools.ToolHomePath(rt.Dir), "helm", "cache")
	assert.Equal(t, cache+"\nwrote\n", text)
	assert.FileExists(t, filepath.Join(cache, "x"))
}
