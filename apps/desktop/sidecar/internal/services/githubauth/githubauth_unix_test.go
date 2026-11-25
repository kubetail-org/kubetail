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

package githubauth

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeGhAt writes a gh that runs script, and answers its path. A test that
// runs it stays serial: a fork in a parallel test can inherit the script's
// descriptor while it is open for writing, and its exec fails with ETXTBSY.
func fakeGhAt(t *testing.T, script string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "gh")
	require.NoError(t, os.WriteFile(path, []byte("#!/bin/sh\n"+script), 0o700))
	return path
}

// gh is asked for github.com's token with prompts off and no stdin, and its
// answer is what it printed.
func TestGhIsAskedForGitHubsToken(t *testing.T) { // serial: see fakeGhAt
	gh := fakeGhAt(t, `[ "$*" = "auth token --hostname github.com" ] || exit 2
[ "$GH_PROMPT_DISABLED" = 1 ] || exit 3
read -r line && exit 4
echo gho_fromTheFakeGh00001
echo noise >&2
`)
	out, err := ghToken(gh, time.Minute)(t.Context())
	require.NoError(t, err)
	assert.Equal(t, "gho_fromTheFakeGh00001\n", string(out))
}

func TestAGhThatFailsIsAnError(t *testing.T) { // serial: see fakeGhAt
	_, err := ghToken(fakeGhAt(t, "echo 'not logged in' >&2; exit 1\n"), time.Minute)(t.Context())
	assert.Equal(t, "gh failed", failureOf(err))
	_, err = ghToken(filepath.Join(t.TempDir(), "missing"), time.Minute)(t.Context())
	assert.Equal(t, "gh not found", failureOf(err))
}

// A gh that hangs is killed at the bound, which this test shrinks.
func TestAGhPastItsBoundIsKilled(t *testing.T) { // serial: see fakeGhAt
	_, err := ghToken(fakeGhAt(t, "exec sleep 60\n"), 10*time.Millisecond)(t.Context())
	assert.Equal(t, "timed out", failureOf(err))
}
