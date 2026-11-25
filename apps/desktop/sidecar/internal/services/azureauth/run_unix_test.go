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

package azureauth

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// script writes an executable shell script and answers its path.
func script(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "az")
	require.NoError(t, os.WriteFile(path, []byte("#!/bin/sh\n"+body+"\n"), 0o700))
	return path
}

func found(path string) func(string) (string, error) {
	return func(string) (string, error) { return path, nil }
}

// The runner runs the az it finds with the arguments it is given, answering
// its standard output alone.
func TestTheRunnerRunsTheAzItFinds(t *testing.T) {
	az := script(t, `echo "$@"; echo noise >&2`)
	out, err := commandRunner(found(az), time.Minute)(t.Context(), "account", "list")
	require.NoError(t, err)
	assert.Equal(t, "account list\n", string(out))
}

// An az that is not an absolute path, missing, or past the bound did not
// run to an answer, ErrNotRun; one that exits non-zero answered.
func TestTheRunnerRefusesWhatItCannotTrust(t *testing.T) {
	_, err := commandRunner(found("az"), time.Minute)(t.Context())
	assert.ErrorIs(t, err, ErrNotRun)
	assert.ErrorContains(t, err, "absolute")
	_, err = commandRunner(func(string) (string, error) { return "", os.ErrNotExist }, time.Minute)(t.Context())
	assert.ErrorIs(t, err, ErrNotRun)
	_, err = commandRunner(found(script(t, "exec sleep 60")), 20*time.Millisecond)(context.Background())
	assert.ErrorIs(t, err, ErrNotRun)
	_, err = commandRunner(found(script(t, "exit 3")), time.Minute)(t.Context())
	assert.ErrorContains(t, err, "exit status 3")
	assert.NotErrorIs(t, err, ErrNotRun)
}

// The stamp changes when any of the user's files changes or appears.
func TestTheStampChangesWithEachFile(t *testing.T) {
	dir := t.TempDir()
	stamp := configStamp(func() string { return dir })
	none := stamp()
	profile := filepath.Join(dir, "azureProfile.json")
	require.NoError(t, os.WriteFile(profile, []byte("{}"), 0o600))
	appeared := stamp()
	assert.NotEqual(t, none, appeared)
	assert.Equal(t, appeared, stamp(), "unchanged files are one stamp")

	config := filepath.Join(dir, "config")
	require.NoError(t, os.WriteFile(config, nil, 0o600))
	withConfig := stamp()
	assert.NotEqual(t, appeared, withConfig)
	later := time.Now().Add(time.Hour)
	require.NoError(t, os.Chtimes(config, later, later))
	touched := stamp()
	assert.NotEqual(t, withConfig, touched)

	require.NoError(t, os.WriteFile(filepath.Join(dir, "clouds.config"), nil, 0o600))
	assert.NotEqual(t, touched, stamp(), "clouds.config holds the cloud's tables")
}

// The user's files are in AZURE_CONFIG_DIR, else ~/.azure. It sets the
// process's environment, so it is not parallel.
func TestTheConfigDirIsAzs(t *testing.T) {
	t.Setenv("AZURE_CONFIG_DIR", "/custom/azure")
	assert.Equal(t, "/custom/azure", configDir())
	t.Setenv("AZURE_CONFIG_DIR", "")
	home, err := os.UserHomeDir()
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(home, ".azure"), configDir())
}

func TestNewReadsNothing(t *testing.T) {
	s := New()
	require.NotNil(t, s)
	assert.False(t, s.discovered)
}
