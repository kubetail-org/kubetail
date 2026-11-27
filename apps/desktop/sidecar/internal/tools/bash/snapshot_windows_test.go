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

//go:build windows

package bash

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/testutil"
)

// gitBashTool is the tool over Git Bash, whose login shell reads rc from a home
// of the test's own.
func gitBashTool(t *testing.T, rc string) *Tool {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("BASH_ENV", "")
	require.NoError(t, os.WriteFile(filepath.Join(home, ".bash_profile"), []byte(". ~/.bashrc\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(home, ".bashrc"), []byte(rc), 0o600))
	return tool(t)
}

// Git Bash's profile reaches the snapshot through the dump's file, which is gone
// once the launch returns; the snapshot is read-only and holds no shims. The
// timeout outlasts a cold Git Bash on a loaded runner, and a dump that gives up
// all the same fails with its logged reason.
func TestTheSnapshotHoldsTheGitBashProfile(t *testing.T) {
	// CaptureLogs swaps the process's default logger, so this test stays serial.
	logs := testutil.CaptureLogs(t)
	rt := testRuntime(t)
	tl := gitBashTool(t, "alias ll='ls -l'\n")
	tl.snapTimeout = 2 * time.Minute
	tl.takeSnapshot(t.Context())
	require.NotEmpty(t, tl.snapshot, logs.String())
	b, err := os.ReadFile(tl.snapshot)
	require.NoError(t, err)
	assert.True(t, strings.Contains(string(b), "ll="), "missing the alias")
	assert.False(t, strings.Contains(string(b), "__kubetail_refuse"), "Windows has no shims")
	info, err := os.Stat(tl.snapshot)
	require.NoError(t, err)
	assert.Zero(t, info.Mode().Perm()&0o200, "the snapshot is writable")
	left, err := filepath.Glob(filepath.Join(tl.scripts, "cmd-*"))
	require.NoError(t, err)
	assert.Empty(t, left, "the dump's file was left behind")

	text, isError := tl.Run(t.Context(), rt, command("alias ll"))
	assert.False(t, isError, text)
	assert.Contains(t, text, "ls -l")
}

// A dump that fails leaves no file on Windows, where there are no shims to
// write, and a command runs all the same. The shrunk timeout is the assertion:
// only it can end the hanging shell.
func TestAFailedSnapshotLeavesNoFileOnWindows(t *testing.T) {
	// gitBashTool sets $HOME on the process, so this test stays serial.
	rt := testRuntime(t)
	tl := gitBashTool(t, "sleep 300\n")
	tl.snapTimeout = 250 * time.Millisecond
	tl.takeSnapshot(t.Context())
	assert.Empty(t, tl.snapshot)
	text, isError := tl.Run(t.Context(), rt, command("echo ok"))
	assert.False(t, isError)
	assert.Equal(t, "ok\n", text)
}

// A launch that cannot reach a shell fails as no shell: no home to start in, no
// directory or file for the dump, or a shell that will not start or resume.
func TestALaunchWithoutAShellIsNoShell(t *testing.T) {
	// t.Setenv sets the process's environment, so this test stays serial.
	missing := filepath.Join(t.TempDir(), "bash.exe")
	for name, tl := range map[string]*Tool{
		"scripts": {shell: missing, scripts: notADir(t)},
		"script":  {shell: missing, scripts: unwritableDir(t)},
		"shell":   {shell: missing, scripts: t.TempDir()},
	} {
		out, reason, code := tl.launchDump(t.Context())
		assert.Nil(t, out, name)
		assert.Equal(t, "no shell", reason, name)
		assert.Equal(t, -1, code, name)
	}

	_, reason, _ := tool(t).launchDumpWith(t.Context(), failing(failResume))
	assert.Equal(t, "no shell", reason, "no resume")

	t.Setenv("USERPROFILE", "")
	_, reason, _ = (&Tool{shell: missing, scripts: t.TempDir()}).launchDump(t.Context())
	assert.Equal(t, "no shell", reason, "no home")
}

// A dump that runs past the limit before its end marker is cut off.
func TestADumpPastTheLimitIsRefused(t *testing.T) {
	t.Parallel()

	got := readDump(strings.NewReader(strings.Repeat("x", 64)), 16)
	assert.Equal(t, dumpRead{reason: "output limit"}, got)
}
