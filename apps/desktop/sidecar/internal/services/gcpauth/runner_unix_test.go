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

package gcpauth

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOutputPastTheBoundIsRefused(t *testing.T) {
	program := filepath.Join(t.TempDir(), "gcloud")
	require.NoError(t, os.WriteFile(program, []byte("#!/bin/sh\nhead -c 70000 /dev/zero\n"), 0o700))

	_, err := runner(program)(t.Context(), false)

	assert.ErrorIs(t, err, errTooLong)
}

// The real helper's run, against programs standing in for gcloud.
func TestTheRunnerReadsGcloudsOutput(t *testing.T) {
	_, err := runner("kubetail-no-such-gcloud")(t.Context(), false)
	assert.ErrorIs(t, err, exec.ErrNotFound)

	out, err := runner("echo")(t.Context(), false)
	require.NoError(t, err)
	assert.Equal(t, "config config-helper --format=json --min-expiry=10m\n", string(out))

	out, err = runner("echo")(t.Context(), true)
	require.NoError(t, err)
	assert.Equal(t, "config config-helper --format=json --min-expiry=10m --force-auth-refresh\n", string(out))

	_, err = runner("false")(t.Context(), false)
	_, isExit := errors.AsType[*exec.ExitError](err)
	assert.True(t, isExit, "%v", err)
}
