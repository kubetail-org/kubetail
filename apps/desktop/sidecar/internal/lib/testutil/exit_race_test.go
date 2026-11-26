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

//go:build race

package testutil

import (
	"os"
	"os/exec"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const exitHelperEnv = "KUBETAIL_TESTUTIL_EXIT_HELPER"

// TestExitHelperChild is the helper TestExitHelper starts: it ends through
// ExitHelper, after a data race when asked for one.
func TestExitHelperChild(t *testing.T) {
	mode := os.Getenv(exitHelperEnv)
	if mode == "" {
		t.Skip("run by TestExitHelper")
	}
	if mode == "race" {
		n := 0
		done := make(chan struct{})
		go func() { n++; close(done) }()
		n++
		<-done
	}
	ExitHelper(3)
}

// A helper keeps its own exit code, and one that raced still fails with the
// race detector's code and report.
func TestExitHelper(t *testing.T) {
	exe, err := os.Executable()
	require.NoError(t, err)
	for mode, want := range map[string]int{"clean": 3, "race": raceExitCode} {
		cmd := exec.Command(exe, "-test.run=^TestExitHelperChild$")
		cmd.Env = append(os.Environ(), exitHelperEnv+"="+mode)
		out, err := cmd.CombinedOutput()
		var exit *exec.ExitError
		require.ErrorAs(t, err, &exit, "%s: %s", mode, out)
		assert.Equal(t, want, exit.ExitCode(), "%s: %s", mode, out)
		if mode == "race" {
			assert.Contains(t, string(out), "DATA RACE")
		}
	}
}
