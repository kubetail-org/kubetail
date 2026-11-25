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

package app

import (
	"os"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/testutil"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/run/loginshell"
)

func TestSetShellEnvSetsWhatItIsHanded(t *testing.T) {
	t.Setenv("PATH", "/usr/bin:/bin")
	t.Setenv("AWS_PROFILE", "")

	setShellEnv(map[string]string{"PATH": "/opt/bin:/usr/bin", "AWS_PROFILE": "work"})

	require.Equal(t, "/opt/bin:/usr/bin", os.Getenv("PATH"))
	require.Equal(t, "work", os.Getenv("AWS_PROFILE"))
}

// With no sandbox, macOS still runs the login shell for its environment, and
// says it runs unconfined. Serial: it swaps the default logger.
func TestDarwinRunsTheShellUnconfinedWithNoSandbox(t *testing.T) {
	logs := testutil.CaptureLogs(t)
	runs := stubLaunch(t, loginshell.Result{Path: []string{"/usr/bin"}}, nil)

	got := launchShell(t.Context(), nil, []string{t.TempDir()}, t.TempDir())

	require.Equal(t, 1, *runs)
	require.Equal(t, []string{"/usr/bin"}, got.path)
	require.Contains(t, logs.String(), "login shell runs unconfined")
}
