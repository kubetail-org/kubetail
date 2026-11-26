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

package testutil

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// A git that fails fails the test, naming what it ran.
func TestGitFailsTheTestWhenGitFails(t *testing.T) {
	f := &fakeTB{}
	Git(f, "", "no-such-command")
	assert.Contains(t, f.failed, "git no-such-command")
}

// A machine with no git leaves the test to RequireSandbox.
func TestGitOriginWithoutGitIsRequireSandboxs(t *testing.T) { // serial: t.Setenv
	t.Setenv("PATH", t.TempDir())
	t.Setenv("KUBETAIL_REQUIRE_SANDBOX", "")
	var sub *testing.T
	t.Run("needs git", func(t *testing.T) {
		sub = t
		GitOrigin(t)
	})
	assert.True(t, sub.Skipped())
}
