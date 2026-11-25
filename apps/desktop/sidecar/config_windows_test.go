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

package main

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// rootsEnv is the environment every config test's roots come from.
var rootsEnv = env{
	"LOCALAPPDATA": `C:\Users\x\AppData\Local`,
	"APPDATA":      `C:\Users\x\AppData\Roaming`,
}

// unreadableRoots names no local data root.
var unreadableRoots = env{"LOCALAPPDATA": ""}

// scratchRoots puts every root a run reads under a scratch folder, so the
// folders it makes are the test's.
func scratchRoots(t *testing.T) env {
	dir := t.TempDir()
	return env{"LOCALAPPDATA": dir, "APPDATA": dir, "USERPROFILE": dir}
}

func TestConfigFromArgsTakesAKubetailPathHoldingDotDot(t *testing.T) {
	dir := `C:\Users\x\link\..\run`
	cfg := mustConfigFromArgs(t, []string{"--runtime-dir", dir}, noEnv)
	require.Equal(t, dir, cfg.Folders.Runtime)
}
