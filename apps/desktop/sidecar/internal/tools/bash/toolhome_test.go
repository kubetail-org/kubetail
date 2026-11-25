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
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/tools"
)

// The tool home is made with every folder its variables name, under the
// chat's directory.
func TestTheToolHomeIsMadeWithItsFolders(t *testing.T) {
	dir := testChatDir(t)

	require.NoError(t, makeToolHome(dir))
	require.NoError(t, makeToolHome(dir), "a second run finds it made")

	for _, v := range toolHomeVars {
		assert.DirExists(t, filepath.Join(tools.ToolHomePath(dir), filepath.FromSlash(v.dir)), v.name)
	}
}

// Two chats get two tool homes, though they share a cluster.
func TestTheToolHomeIsPerChat(t *testing.T) {
	a, b := testRuntime(t), testRuntime(t)
	a.ClusterID, b.ClusterID = "c", "c"

	assert.NotEqual(t, tools.ToolHomePath(a.Dir), tools.ToolHomePath(b.Dir))
}

// gcloud keeps its configuration and logs in the tool home, not the
// workspace.
func TestCloudSDKConfigIsUnderTheToolHome(t *testing.T) {
	assert.Contains(t, toolHomeEnv("/home/t"), "CLOUDSDK_CONFIG=/home/t/gcloud")
}
