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

package main

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// rootsEnv is the environment every config test's roots come from.
var rootsEnv = env{"HOME": "/home/x"}

// unreadableRoots names no home.
var unreadableRoots = env{"HOME": ""}

// scratchRoots puts every root a run reads under a scratch folder, so the
// folders it makes are the test's.
func scratchRoots(t *testing.T) env {
	return env{"HOME": t.TempDir()}
}

func TestConfigFromArgsRefusesARuntimeDirHoldingDotDot(t *testing.T) {
	_, err := configFromArgs([]string{"--runtime-dir", "/home/x/link/../kubetail"}, noEnv.get)
	require.ErrorContains(t, err, "--runtime-dir")
}

func TestConfigFromArgsRefusesARootHoldingDotDot(t *testing.T) {
	_, err := configFromArgs(nil, env{"HOME": "/home/x/link/.."}.get)
	require.ErrorContains(t, err, "HOME")
}

func TestConfigFromArgsTakesANameBeginningWithDots(t *testing.T) {
	cfg := mustConfigFromArgs(t, []string{"--runtime-dir", "/home/x/..run"}, noEnv)
	require.Equal(t, "/home/x/..run", cfg.Folders.Runtime)
}
