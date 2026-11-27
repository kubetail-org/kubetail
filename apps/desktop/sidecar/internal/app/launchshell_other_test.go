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

//go:build unix && !darwin

package app

import (
	"context"
	"os"
	"testing"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/run/loginshell"
	"github.com/stretchr/testify/require"
)

// Only a macOS GUI launch is handed an environment that lacks what the user's
// shell builds, so nothing is set elsewhere.
func TestSetShellEnvIsANoOpOffDarwin(t *testing.T) {
	// t.Setenv sets the process's environment, so this test stays serial.
	t.Setenv("PATH", "/usr/bin:/bin")

	setShellEnv(map[string]string{"PATH": "/opt/bin"})

	require.Equal(t, "/usr/bin:/bin", os.Getenv("PATH"))
}

// On Linux with no sandbox nothing reads the launch's PATH, so the login shell
// is not run.
func TestLinuxSkipsTheResolutionWithNoSandbox(t *testing.T) {
	// It swaps the package's resolveShell, so this test stays serial.
	resolve := resolveShell
	t.Cleanup(func() { resolveShell = resolve })
	resolveShell = func(context.Context, loginshell.Start, []string) (loginshell.Result, *loginshell.Fault) {
		t.Fatal("the login shell ran")
		return loginshell.Result{}, nil
	}

	require.Equal(t, launch{}, launchShell(t.Context(), nil, []string{t.TempDir()}, t.TempDir()))
}
