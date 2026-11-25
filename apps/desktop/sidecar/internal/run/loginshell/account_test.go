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

package loginshell

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// The account record's shell is taken when it names an executable file.
func TestAccountShellTakesTheRecordsShell(t *testing.T) {
	shell := writeScript(t, "zsh", "#!/bin/sh\nexit 0\n")
	useAccountShell(t, shell)

	got, f := accountShell(t.Context())
	require.Nil(t, f)
	require.Equal(t, shell, got)
}

// A record naming nothing executable falls back to the platform's own shells,
// in order, and to no shell when none is there.
func TestAccountShellFallsBack(t *testing.T) {
	notExecutable := filepath.Join(t.TempDir(), "zsh")
	require.NoError(t, os.WriteFile(notExecutable, nil, 0o600))
	first := filepath.Join(t.TempDir(), "missing")
	second := writeScript(t, "sh", "#!/bin/sh\nexit 0\n")
	defer func(shells []string) { defaultShells = shells }(defaultShells)

	for name, record := range map[string]string{
		"nothing":        "",
		"relative":       "zsh",
		"missing":        filepath.Join(t.TempDir(), "gone"),
		"not executable": notExecutable,
	} {
		t.Run(name, func(t *testing.T) {
			useAccountShell(t, record)
			defaultShells = []string{first, second}
			got, f := accountShell(t.Context())
			require.Nil(t, f)
			require.Equal(t, second, got)

			defaultShells = []string{first}
			_, f = accountShell(t.Context())
			require.NotNil(t, f)
			require.Equal(t, reasonNoShell, f.Reason)
		})
	}
}

// Finding the account's shell can stat a dead network mount, which no context
// interrupts, so the resolution's deadline bounds the wait for it.
func TestResolveBoundsShellDiscovery(t *testing.T) {
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	defer func(find func(context.Context) (string, *Fault)) { findShell = find }(findShell)
	findShell = func(context.Context) (string, *Fault) {
		<-release
		return "", nil
	}

	ctx, cancel := context.WithTimeout(t.Context(), time.Millisecond)
	defer cancel()
	_, f := Resolve(ctx, plainStart, nil)
	require.NotNil(t, f)
	require.Equal(t, reasonTimeout, f.Reason)
}
