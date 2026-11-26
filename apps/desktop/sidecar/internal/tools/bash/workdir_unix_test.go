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
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A workdir is resolved by string work alone, against the workspace or the
// home by mode. Outside the sandbox .. may leave either: a directory is a place,
// not a boundary.
func TestWhereACallStarts(t *testing.T) {
	const home, ws = "/home/ana", "/data/chats/c1/workspace"
	for _, c := range []struct{ workdir, sandboxed, outside string }{
		{"", ws, ws},
		{"~", ws, home},
		{"~/src", ws + "/src", home + "/src"},
		{"~/src/", ws + "/src", home + "/src"},
		{"src/app", ws + "/src/app", ws + "/src/app"},
		{ws, ws, ws},
		{ws + "/./x/../y", ws + "/y", ws + "/y"},
		{"../bob", "", "/data/chats/c1/bob"},
		{"~/..", "", "/home"},
		{"~/../../etc", "", "/etc"},
		{"/tmp/./x/../y", "", "/tmp/y"},
		{"/", "", "/"},
		{"/data/chats/c1/workspaces", "", "/data/chats/c1/workspaces"},
	} {
		got, err := resolveWorkdir(home, ws, c.workdir, false)
		require.NoError(t, err, c.workdir)
		assert.Equal(t, c.outside, got, "outside: %q", c.workdir)

		got, err = resolveWorkdir(home, ws, c.workdir, true)
		if c.sandboxed == "" {
			assert.ErrorIs(t, err, errOutsideWorkspace, "sandboxed: %q", c.workdir)
			continue
		}
		require.NoError(t, err, c.workdir)
		assert.Equal(t, c.sandboxed, got, "sandboxed: %q", c.workdir)
	}

	// ~user names another user's home, which only the shell can look up.
	for _, workdir := range []string{"~ana/x", "~bob", "~+"} {
		for _, sandboxed := range []bool{false, true} {
			_, err := resolveWorkdir(home, ws, workdir, sandboxed)
			assert.ErrorIs(t, err, errInput, workdir)
		}
	}
}

// checkWorkdir refuses what no home can make valid, and nothing else.
func TestCheckWorkdir(t *testing.T) {
	for _, workdir := range []string{"", "~", "~/src", "src", "/tmp"} {
		assert.NoError(t, checkWorkdir(workdir), workdir)
	}
	for _, workdir := range []string{"~ana/x", "~bob", "~+"} {
		assert.ErrorIs(t, checkWorkdir(workdir), errInput, workdir)
	}
}
