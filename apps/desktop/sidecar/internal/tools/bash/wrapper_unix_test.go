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
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A quoted string reaches the shell as the one word it was, whatever it holds
// but NUL, which argv cannot carry.
func TestQuoteRoundTrips(t *testing.T) {
	var all []byte
	for b := 1; b < 256; b++ {
		all = append(all, byte(b))
	}
	for _, s := range []string{
		"", "plain", "it's", "'", "''", `"`, `\`, "a\nb", "$(id)", "`id`", "${HOME}", "*", "-n",
		"'\\''", "日本語", string(all),
	} {
		out, err := exec.Command("sh", "-c", "printf %s "+quote(s)).Output()
		require.NoError(t, err, "%q", s)
		assert.Equal(t, s, string(out), "%q", s)
	}
}

// shellTool is the tool over the named shell, skipping when the machine has none,
// with a snapshot of the test's own.
func shellTool(t *testing.T, kind, snapshot string) *Tool {
	t.Helper()
	path, err := exec.LookPath(kind)
	if err != nil {
		t.Skip("no " + kind + " on this machine")
	}
	tl := tool(t)
	tl.shell, tl.kind = path, kind
	if snapshot != "" {
		tl.snapshot = filepath.Join(t.TempDir(), "snapshot.sh")
		require.NoError(t, os.WriteFile(tl.snapshot, []byte(snapshot), 0o600))
	}
	return tl
}

// Each command sources the snapshot first and runs through eval with stdin
// closed, under either shell.
func TestTheWrapperSourcesTheSnapshot(t *testing.T) {
	rt := testRuntime(t)
	for _, kind := range []string{"bash", "zsh"} {
		tl := shellTool(t, kind, "greet() { echo hi from $1; }\n")
		text, isError := tl.Run(t.Context(), rt, command(`greet "it's"; read x; echo "[$x]"`))
		assert.False(t, isError, kind)
		assert.Equal(t, "hi from it's\n[]\n", text, kind)
	}
}

// A profile that turns on zsh's extended globbing still leaves bash-style text
// alone: # and ~ are not glob syntax in the command.
func TestZshKeepsBashStyleGlobs(t *testing.T) {
	rt := testRuntime(t)
	tl := shellTool(t, "zsh", "setopt extendedglob\n")
	text, isError := tl.Run(t.Context(), rt, command(`echo a#b x~y`))
	assert.False(t, isError, text)
	assert.Equal(t, "a#b x~y\n", text)
}

// Under zsh a glob matching nothing stays the word it was, and an unquoted
// variable splits into words, both as bash has it.
func TestZshKeepsBashStyleWords(t *testing.T) {
	rt := testRuntime(t)
	tl := shellTool(t, "zsh", "")
	text, isError := tl.Run(t.Context(), rt, command(`echo {.items[*].metadata.name}; x="a b"; for w in $x; do echo "[$w]"; done`))
	assert.False(t, isError, text)
	assert.Equal(t, "{.items[*].metadata.name}\n[a]\n[b]\n", text)
}

// With no snapshot, a command runs all the same.
func TestTheWrapperRunsWithoutASnapshot(t *testing.T) {
	rt := testRuntime(t)
	tl := shellTool(t, "bash", "")
	text, isError := tl.Run(t.Context(), rt, command(`echo ok`))
	assert.False(t, isError)
	assert.Equal(t, "ok\n", text)
}
