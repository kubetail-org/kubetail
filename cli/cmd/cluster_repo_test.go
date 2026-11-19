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

package cmd

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClusterRepoCmd_RegistersSubcommands(t *testing.T) {
	for _, name := range []string{"add", "remove", "update"} {
		sub, _, err := clusterRepoCmd.Find([]string{name})
		require.NoError(t, err, name)
		assert.Equal(t, name, sub.Name())
		assert.Equal(t, clusterRepoCmd, sub.Parent())
	}
	assert.Equal(t, clusterCmd, clusterRepoCmd.Parent())
}
