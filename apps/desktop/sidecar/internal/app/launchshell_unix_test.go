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

//go:build unix

package app

import (
	"bytes"
	"context"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/safe"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/run/loginshell"
	"github.com/stretchr/testify/require"
)

// One run of the login shell answers both readers: its environment is set
// and its PATH handed on.
func TestResolveIsRunOnceAtLaunch(t *testing.T) {
	runs := 0
	got := runShell(t.Context(), nil, nil, func(context.Context, loginshell.Start, []string) (loginshell.Result, *loginshell.Fault) {
		runs++
		return loginshell.Result{Path: []string{"/opt/bin", "/usr/bin"}}, nil
	})
	require.Equal(t, 1, runs)
	require.Equal(t, launch{path: []string{"/opt/bin", "/usr/bin"}}, got)

	got = runShell(t.Context(), nil, nil, func(context.Context, loginshell.Start, []string) (loginshell.Result, *loginshell.Fault) {
		return loginshell.Result{}, &loginshell.Fault{Reason: "timeout", ExitCode: -1}
	})
	require.Equal(t, launch{fault: "timeout"}, got)
}

// A key the login shell set is handed back and registered with the redactor,
// and no log line carries it. It is never set on the process, so a credential
// plugin the sidecar spawns does not inherit it.
func TestAKeyFromTheShellIsRedactedAndNeverInherited(t *testing.T) {
	const key = "sk-ant-shell-0123456789"
	t.Cleanup(safe.ResetSecrets)
	t.Setenv("ANTHROPIC_API_KEY", "")
	require.NoError(t, os.Unsetenv("ANTHROPIC_API_KEY"))
	var logged bytes.Buffer
	defer slog.SetDefault(slog.Default())
	slog.SetDefault(slog.New(slog.NewTextHandler(&logged, nil)))

	var asked []string
	got := runShell(t.Context(), nil, []string{"ANTHROPIC_API_KEY"}, func(_ context.Context, _ loginshell.Start, keyVars []string) (loginshell.Result, *loginshell.Fault) {
		asked = keyVars
		return loginshell.Result{
			Path: []string{"/usr/bin"},
			Env:  map[string]string{},
			Keys: map[string]string{"ANTHROPIC_API_KEY": key},
		}, nil
	})

	require.Equal(t, []string{"ANTHROPIC_API_KEY"}, asked)
	require.Equal(t, map[string]string{"ANTHROPIC_API_KEY": key}, got.keys)
	require.NotContains(t, safe.String("key "+key+" here"), key)
	require.NotContains(t, logged.String(), key)
	_, set := os.LookupEnv("ANTHROPIC_API_KEY")
	require.False(t, set, "the key is not on the process")
	out, err := exec.Command("/usr/bin/env").Output()
	require.NoError(t, err)
	require.NotContains(t, string(out), key, "a child does not inherit it")
}

// The launch resolution runs in the sandbox, its output leaving it, so the
// denied-always list is shut to it; a refused policy runs no shell.
func TestTheLaunchResolutionRunsInTheSandbox(t *testing.T) {
	sb := &refusingSandbox{never: []string{"/never"}}
	kubetailDirs := []string{t.TempDir(), t.TempDir(), t.TempDir()}
	tmpDir := filepath.Join(kubetailDirs[1], "tmp")

	got := launchShell(t.Context(), sb, kubetailDirs, tmpDir)

	require.Equal(t, launch{fault: "sandbox refused"}, got)
	require.Len(t, sb.runs, 1)
	p := sb.runs[0].Policy
	require.Equal(t, []string{"/never"}, p.Always.Deny)
	require.Equal(t, kubetailDirs, p.Always.Kubetail)
	require.Len(t, p.Always.Write, 1)
	require.Equal(t, tmpDir, filepath.Dir(p.Always.Write[0]))
	require.Zero(t, p.Network)
}

// stubLaunch makes the login shell's run answer res and f, and counts the
// runs it was asked for. A res with an Env is set process-wide on macOS, so a
// test that stubs one sets each of its names with t.Setenv first.
func stubLaunch(t *testing.T, res loginshell.Result, f *loginshell.Fault) *int {
	t.Helper()
	runs := new(int)
	resolve := resolveShell
	t.Cleanup(func() { resolveShell = resolve })
	resolveShell = func(context.Context, loginshell.Start, []string) (loginshell.Result, *loginshell.Fault) {
		*runs++
		return res, f
	}
	return runs
}
