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

package main

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Windows has no exec: the arguments and the environment pass through as they are.
func TestScrubPassesThroughOnWindows(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "sk-ant-test-0123456789")
	args, getenv, err := scrubExecEnvironment()
	require.NoError(t, err)
	assert.Equal(t, os.Args, args)
	assert.Equal(t, "sk-ant-test-0123456789", getenv("ANTHROPIC_API_KEY"))
}
