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
	"testing"

	"github.com/stretchr/testify/assert"
)

// A key from the shell is added under its provider, and one the launch set wins.
func TestWithShellKeysPrefersTheLaunch(t *testing.T) {
	assert.Nil(t, withShellKeys(nil, nil))
	assert.Equal(t, map[string]string{"anthropic": "sk-ant-shell"},
		withShellKeys(nil, map[string]string{"ANTHROPIC_API_KEY": "sk-ant-shell"}), "a launch with no keys takes the shell's")

	launch := map[string]string{"openai": "sk-openai-launch"}
	got := withShellKeys(launch, map[string]string{
		"ANTHROPIC_API_KEY": "sk-ant-shell",
		"OPENAI_API_KEY":    "sk-openai-shell",
	})

	assert.Equal(t, map[string]string{"anthropic": "sk-ant-shell", "openai": "sk-openai-launch"}, got)
	assert.Equal(t, map[string]string{"openai": "sk-openai-launch"}, launch, "the launch's map is left alone")
}
